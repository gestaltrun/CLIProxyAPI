package management

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

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
}

func glmCodingPlanViews(entries []config.GLMCodingPlanKey) []glmCodingPlanView {
	out := make([]glmCodingPlanView, 0, len(entries))
	for index := range entries {
		entry := entries[index]
		out = append(out, glmCodingPlanView{
			Index:        index,
			HasAPIKey:    strings.TrimSpace(entry.APIKey) != "",
			Site:         entry.Site,
			Priority:     entry.Priority,
			Weight:       entry.Weight,
			Prefix:       entry.Prefix,
			ProxyURL:     entry.ProxyURL,
			Organization: entry.Organization,
			Project:      entry.Project,
		})
	}
	return out
}

// GetGLMCodingPlan returns non-secret GLM Coding Plan configuration.
func (h *Handler) GetGLMCodingPlan(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"glm-coding-plan": glmCodingPlanViews(h.cfg.GLMCodingPlan)})
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
	if *body.Index < 0 || *body.Index >= len(h.cfg.GLMCodingPlan) {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}
	entry := h.cfg.GLMCodingPlan[*body.Index]
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
	candidateEntries[*body.Index] = entry
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
