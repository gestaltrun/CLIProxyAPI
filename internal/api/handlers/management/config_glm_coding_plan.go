package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/glm"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

const glmQuotaRefreshTimeout = 30 * time.Second

type glmCodingPlanView struct {
	Index        int    `json:"index"`
	HasAPIKey    bool   `json:"has-api-key"`
	Site         string `json:"site"`
	Priority     int    `json:"priority,omitempty"`
	Weight       *int   `json:"weight,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	ProxyURL     string `json:"proxy-url,omitempty"`
	Organization string `json:"organization,omitempty"`
	Project      string `json:"project,omitempty"`
	AuthIndex    string `json:"auth_index,omitempty"`
	Quota        gin.H  `json:"quota,omitempty"`
}

func glmCodingPlanViews(entries []config.GLMCodingPlanKey, auths []*coreauth.Auth) []glmCodingPlanView {
	out := make([]glmCodingPlanView, 0, len(entries))
	for index := range entries {
		entry := entries[index]
		view := glmCodingPlanView{
			Index:        index,
			HasAPIKey:    strings.TrimSpace(entry.APIKey) != "",
			Site:         entry.Site,
			Priority:     entry.Priority,
			Weight:       entry.Weight,
			Prefix:       entry.Prefix,
			ProxyURL:     proxyutil.Redact(entry.ProxyURL),
			Organization: entry.Organization,
			Project:      entry.Project,
		}
		if auth := matchingGLMCodingPlanAuth(auths, index); auth != nil {
			view.AuthIndex = auth.EnsureIndex()
			view.Quota = quotaObservationPayloadForProvider(auth.Provider, auth.Quota)
		}
		out = append(out, view)
	}
	return out
}

func matchingGLMCodingPlanAuth(auths []*coreauth.Auth, index int) *coreauth.Auth {
	wanted := strconv.Itoa(index)
	for _, auth := range auths {
		if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), glm.Provider) {
			continue
		}
		if strings.TrimSpace(authAttribute(auth, "config_index")) == wanted {
			return auth
		}
	}
	return nil
}

func glmAuthsFromManager(manager *coreauth.Manager) []*coreauth.Auth {
	if manager == nil {
		return nil
	}
	return manager.List()
}

// GetGLMCodingPlan returns non-secret GLM Coding Plan configuration and quota envelopes.
func (h *Handler) GetGLMCodingPlan(c *gin.Context) {
	h.mu.Lock()
	entries := append([]config.GLMCodingPlanKey(nil), h.cfg.GLMCodingPlan...)
	manager := h.authManager
	h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"glm-coding-plan": glmCodingPlanViews(entries, glmAuthsFromManager(manager))})
}

// RefreshGLMCodingPlanQuota probes one GLM Coding Plan credential and returns its quota envelope.
func (h *Handler) RefreshGLMCodingPlanQuota(c *gin.Context) {
	var index int
	if _, errScan := fmt.Sscanf(strings.TrimSpace(c.Query("index")), "%d", &index); errScan != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index is required"})
		return
	}
	h.mu.Lock()
	if index < 0 || index >= len(h.cfg.GLMCodingPlan) {
		h.mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}
	entry := h.cfg.GLMCodingPlan[index]
	manager := h.authManager
	cfg := h.cfg
	h.mu.Unlock()
	auth := matchingGLMCodingPlanAuth(glmAuthsFromManager(manager), index)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "runtime GLM credential is unavailable"})
		return
	}
	endpoints, errEndpoints := glm.ResolveEndpointsForBase(entry.Site, authAttribute(auth, "base_url"))
	if errEndpoints != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errEndpoints.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), glmQuotaRefreshTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "GLM quota refresh panicked"})
		}
	}()
	if manager == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "runtime GLM credential is unavailable"})
		return
	}
	client := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, glmQuotaRefreshTimeout)
	snapshot := glm.ProbeQuota(ctx, client, endpoints, entry.APIKey, entry.Organization, entry.Project, previousGLMQuotaSnapshot(auth.Quota), time.Now().UTC())
	if _, errUpdate := manager.UpdateRuntimeObservation(coreauth.WithSkipPersist(ctx), auth.ID, func(updated *coreauth.Auth) {
		updated.Quota.ObservedAt = snapshot.ObservedAt
		updated.Quota.Signals = snapshot.Signals()
	}); errUpdate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record GLM quota observation"})
		return
	}
	updated, _ := manager.GetByID(auth.ID)
	if updated == nil {
		updated = auth
		updated.Quota.ObservedAt = snapshot.ObservedAt
		updated.Quota.Signals = snapshot.Signals()
	}
	c.JSON(http.StatusOK, gin.H{
		"index":      index,
		"auth_index": updated.EnsureIndex(),
		"quota":      quotaObservationPayloadForProvider(updated.Provider, updated.Quota),
	})
}

func previousGLMQuotaSnapshot(quota coreauth.QuotaState) glm.QuotaSnapshot {
	signals := quota.Signals
	if signals == nil {
		signals = map[string]string{}
	}
	windows := make([]glm.QuotaWindow, 0, 2)
	if used, ok := parseGLMUsedPercent(signals["GLM-Quota-5h-Used-Percent"]); ok {
		windows = append(windows, glm.QuotaWindow{Window: glm.WindowFiveHour, UsedPercent: used, ResetAt: parseGLMTime(signals["GLM-Quota-5h-Reset-At"])})
	}
	if used, ok := parseGLMUsedPercent(signals["GLM-Quota-Weekly-Used-Percent"]); ok {
		windows = append(windows, glm.QuotaWindow{Window: glm.WindowWeekly, UsedPercent: used, ResetAt: parseGLMTime(signals["GLM-Quota-Weekly-Reset-At"])})
	}
	return glm.QuotaSnapshot{
		ObservedAt:       quota.ObservedAt,
		LastSuccessfulAt: parseGLMTime(signals["GLM-Quota-Last-Success-At"]),
		Status:           strings.TrimSpace(signals["GLM-Quota-Status"]),
		CredentialValid:  strings.TrimSpace(signals["GLM-Credential-Valid"]) != "false",
		PlanLevel:        strings.TrimSpace(signals["GLM-Plan-Level"]),
		Windows:          windows,
		Error:            strings.TrimSpace(signals["GLM-Quota-Error"]),
	}
}

func parseGLMUsedPercent(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed, err == nil
}

func parseGLMTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// PutGLMCodingPlan replaces the GLM Coding Plan credential list.
func (h *Handler) PutGLMCodingPlan(c *gin.Context) {
	data, errRead := c.GetRawData()
	if errRead != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	var entries []config.GLMCodingPlanKey
	if errDecode := json.Unmarshal(data, &entries); errDecode != nil {
		var body struct {
			Items []config.GLMCodingPlanKey `json:"items"`
		}
		if errObject := json.Unmarshal(data, &body); errObject != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		entries = body.Items
	}
	candidate := &config.Config{GLMCodingPlan: append([]config.GLMCodingPlanKey(nil), entries...)}
	if errValidate := candidate.ValidateGLMCodingPlan(); errValidate != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
		return
	}
	if errWeight := candidate.ValidateCredentialWeights(); errWeight != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errWeight.Error()})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.GLMCodingPlan = candidate.GLMCodingPlan
	h.persistLocked(c)
}

type glmCodingPlanPatch struct {
	APIKey       *string         `json:"api-key"`
	Site         *string         `json:"site"`
	Priority     *int            `json:"priority"`
	Weight       json.RawMessage `json:"weight"`
	Prefix       *string         `json:"prefix"`
	ProxyURL     *string         `json:"proxy-url"`
	Organization *string         `json:"organization"`
	Project      *string         `json:"project"`
}

// PatchGLMCodingPlan updates one GLM Coding Plan credential by index.
func (h *Handler) PatchGLMCodingPlan(c *gin.Context) {
	var body struct {
		Index *int                `json:"index"`
		Value *glmCodingPlanPatch `json:"value"`
	}
	if errBind := c.ShouldBindJSON(&body); errBind != nil || body.Index == nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index and value are required"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if *body.Index < 0 || *body.Index > len(h.cfg.GLMCodingPlan) {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}
	appending := *body.Index == len(h.cfg.GLMCodingPlan)
	entry := config.GLMCodingPlanKey{}
	if !appending {
		entry = h.cfg.GLMCodingPlan[*body.Index]
	}
	if body.Value.APIKey != nil {
		entry.APIKey = strings.TrimSpace(*body.Value.APIKey)
	}
	if body.Value.Site != nil {
		entry.Site = strings.TrimSpace(*body.Value.Site)
	}
	if body.Value.Priority != nil {
		entry.Priority = *body.Value.Priority
	}
	if len(body.Value.Weight) > 0 {
		weight, errWeight := parseCredentialWeightPatch(body.Value.Weight)
		if errWeight != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errWeight.Error()})
			return
		}
		entry.Weight = weight
	}
	if body.Value.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*body.Value.Prefix)
	}
	if body.Value.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*body.Value.ProxyURL)
	}
	if body.Value.Organization != nil {
		entry.Organization = strings.TrimSpace(*body.Value.Organization)
	}
	if body.Value.Project != nil {
		entry.Project = strings.TrimSpace(*body.Value.Project)
	}
	candidateEntries := append([]config.GLMCodingPlanKey(nil), h.cfg.GLMCodingPlan...)
	if appending {
		candidateEntries = append(candidateEntries, entry)
	} else {
		candidateEntries[*body.Index] = entry
	}
	candidate := &config.Config{GLMCodingPlan: candidateEntries}
	if errValidate := candidate.ValidateGLMCodingPlan(); errValidate != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errValidate.Error()})
		return
	}
	if errWeight := candidate.ValidateCredentialWeights(); errWeight != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errWeight.Error()})
		return
	}
	h.cfg.GLMCodingPlan = candidate.GLMCodingPlan
	h.persistLocked(c)
}

// DeleteGLMCodingPlan deletes one GLM Coding Plan credential by index.
func (h *Handler) DeleteGLMCodingPlan(c *gin.Context) {
	var index int
	if _, errScan := fmt.Sscanf(strings.TrimSpace(c.Query("index")), "%d", &index); errScan != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index is required"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if index < 0 || index >= len(h.cfg.GLMCodingPlan) {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}
	h.cfg.GLMCodingPlan = append(h.cfg.GLMCodingPlan[:index], h.cfg.GLMCodingPlan[index+1:]...)
	h.persistLocked(c)
}
