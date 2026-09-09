package cliproxy

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/glm"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const glmQuotaPollInterval = 10 * time.Minute

type glmQuotaSnapshot = glm.QuotaSnapshot

type glmEndpointResolver func(string, string) (glm.Endpoints, error)
type glmHTTPClientFactory func(context.Context, *coreauth.Auth, time.Duration) *http.Client

func (s *Service) resolveGLMEndpoints(site, selectedBaseURL string) (glm.Endpoints, error) {
	if s != nil && s.glmResolveEndpoints != nil {
		return s.glmResolveEndpoints(site, selectedBaseURL)
	}
	return glm.ResolveEndpointsForBase(site, selectedBaseURL)
}

func (s *Service) newGLMHTTPClient(ctx context.Context, auth *coreauth.Auth, timeout time.Duration) *http.Client {
	if s != nil && s.glmHTTPClient != nil {
		return s.glmHTTPClient(ctx, auth, timeout)
	}
	return helps.NewProxyAwareHTTPClient(ctx, s.cfg, auth, timeout)
}

func (s *Service) startGLMQuotaPolling(parent context.Context) {
	if s == nil || s.coreManager == nil {
		return
	}
	s.glmQuotaMu.Lock()
	if s.glmQuotaCancel != nil {
		s.glmQuotaMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.glmQuotaCancel = cancel
	s.glmQuotaDone = done
	if s.glmQuotaSnapshots == nil {
		s.glmQuotaSnapshots = make(map[string]glmQuotaSnapshot)
	}
	s.glmQuotaMu.Unlock()
	go func() {
		defer close(done)
		s.refreshGLMQuota(ctx)
		ticker := time.NewTicker(glmQuotaPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshGLMQuota(ctx)
			}
		}
	}()
}

func (s *Service) stopGLMQuotaPolling() {
	if s == nil {
		return
	}
	s.glmQuotaMu.Lock()
	cancel := s.glmQuotaCancel
	done := s.glmQuotaDone
	s.glmQuotaCancel = nil
	s.glmQuotaDone = nil
	s.glmQuotaMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) refreshGLMQuota(ctx context.Context) {
	for _, auth := range s.coreManager.List() {
		if auth == nil || auth.Disabled || !strings.EqualFold(strings.TrimSpace(auth.Provider), glm.Provider) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		s.refreshGLMQuotaForAuth(ctx, auth)
	}
}

func (s *Service) refreshGLMQuotaForAuth(ctx context.Context, auth *coreauth.Auth) {
	if auth == nil || auth.Attributes == nil {
		return
	}
	endpoints, errEndpoints := s.resolveGLMEndpoints(auth.Attributes["glm_site"], auth.Attributes["base_url"])
	if errEndpoints != nil {
		log.WithError(errEndpoints).Warn("GLM quota endpoint resolution failed")
		return
	}
	s.glmQuotaMu.Lock()
	previous := s.glmQuotaSnapshots[auth.ID]
	s.glmQuotaMu.Unlock()
	client := s.newGLMHTTPClient(ctx, auth, 0)
	if s.glmQuotaBeforeFlight != nil {
		s.glmQuotaBeforeFlight(auth.ID)
	}
	value, errFlight, _ := s.glmQuotaFlight.Do(auth.ID, func() (any, error) {
		probe := glm.ProbeQuota
		if s.glmQuotaProbe != nil {
			probe = s.glmQuotaProbe
		}
		return probe(
			ctx,
			client,
			endpoints,
			auth.Attributes["api_key"],
			auth.Attributes["glm_organization"],
			auth.Attributes["glm_project"],
			previous,
			time.Now().UTC(),
		), nil
	})
	if errFlight != nil {
		return
	}
	snapshot := value.(glm.QuotaSnapshot)
	s.glmQuotaMu.Lock()
	if s.glmQuotaSnapshots == nil {
		s.glmQuotaSnapshots = make(map[string]glmQuotaSnapshot)
	}
	s.glmQuotaSnapshots[auth.ID] = snapshot
	s.glmQuotaMu.Unlock()
	if _, errUpdate := s.coreManager.UpdateRuntimeObservation(coreauth.WithSkipPersist(ctx), auth.ID, func(updated *coreauth.Auth) {
		updated.Quota.ObservedAt = snapshot.ObservedAt
		updated.Quota.Signals = glmQuotaSignals(snapshot)
		applyGLMQuotaAvailability(updated, snapshot)
	}); errUpdate != nil {
		log.WithError(errUpdate).Warn("GLM quota observation update failed")
	}
}

func applyGLMQuotaAvailability(auth *coreauth.Auth, snapshot glmQuotaSnapshot) {
	if auth == nil || auth.Disabled || auth.Status == coreauth.StatusDisabled {
		return
	}
	if snapshot.Status == "stale" && !snapshot.CredentialValid {
		auth.Status = coreauth.StatusError
		auth.StatusMessage = "GLM Coding Plan credential rejected by quota endpoint"
		auth.Unavailable = true
		auth.NextRetryAfter = time.Time{}
		return
	}
	if snapshot.Status != "ready" {
		return
	}
	exhaustedUntil := time.Time{}
	for _, window := range snapshot.Windows {
		if window.UsedPercent < 100 || !window.ResetAt.After(snapshot.ObservedAt) {
			continue
		}
		if exhaustedUntil.IsZero() || window.ResetAt.After(exhaustedUntil) {
			exhaustedUntil = window.ResetAt
		}
	}
	if !exhaustedUntil.IsZero() {
		auth.Status = coreauth.StatusError
		auth.StatusMessage = "GLM Coding Plan quota exhausted"
		auth.Unavailable = true
		auth.NextRetryAfter = exhaustedUntil
		return
	}
	if strings.HasPrefix(auth.StatusMessage, "GLM Coding Plan ") {
		auth.Status = coreauth.StatusActive
		auth.StatusMessage = ""
		auth.Unavailable = false
		auth.NextRetryAfter = time.Time{}
	}
}

func glmQuotaSignals(snapshot glmQuotaSnapshot) map[string]string {
	signals := map[string]string{
		"GLM-Quota-Status":          snapshot.Status,
		"GLM-Credential-Valid":      strconv.FormatBool(snapshot.CredentialValid),
		"GLM-Quota-Last-Success-At": formatGLMQuotaTime(snapshot.LastSuccessfulAt),
	}
	if snapshot.PlanLevel != "" {
		signals["GLM-Plan-Level"] = snapshot.PlanLevel
	}
	for _, window := range snapshot.Windows {
		prefix := "GLM-Quota-5h"
		if window.Window == glm.WindowWeekly {
			prefix = "GLM-Quota-Weekly"
		}
		signals[prefix+"-Used-Percent"] = strconv.FormatFloat(window.UsedPercent, 'f', -1, 64)
		if !window.ResetAt.IsZero() {
			signals[prefix+"-Reset-At"] = window.ResetAt.UTC().Format(time.RFC3339)
		}
	}
	if snapshot.Error != "" {
		signals["GLM-Quota-Error"] = snapshot.Error
	}
	return signals
}

func formatGLMQuotaTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
