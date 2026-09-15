package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/glm"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestGetGLMCodingPlanRedactsAPIKey(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{GLMCodingPlan: []config.GLMCodingPlanKey{{APIKey: "secret-key", Site: "cn", Organization: "team", ProxyURL: "http://proxy-user:proxy-password@proxy.example.com:8080/private"}}}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/glm-coding-plan", nil)
	h.GetGLMCodingPlan(ctx)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "secret-key") || strings.Contains(recorder.Body.String(), `"api-key":`) || strings.Contains(recorder.Body.String(), "proxy-user") || strings.Contains(recorder.Body.String(), "proxy-password") || strings.Contains(recorder.Body.String(), "/private") {
		t.Fatalf("response leaks secret: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Entries []glmCodingPlanView `json:"glm-coding-plan"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &body); errDecode != nil || len(body.Entries) != 1 || !body.Entries[0].HasAPIKey {
		t.Fatalf("response = %s err=%v", recorder.Body.String(), errDecode)
	}
	if body.Entries[0].ProxyURL != "http://redacted@proxy.example.com:8080" {
		t.Fatalf("proxy URL = %q", body.Entries[0].ProxyURL)
	}
}

func TestGetGLMCodingPlanIncludesQuotaEnvelope(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "glm-runtime",
		Provider: glm.Provider,
		Attributes: map[string]string{
			"config_index": "0",
			"source":       "config:glm-coding-plan[token]",
		},
		Quota: coreauth.QuotaState{
			ObservedAt: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
			Signals: map[string]string{
				"GLM-Quota-Status":          "ready",
				"GLM-Quota-5h-Used-Percent": "12",
			},
		},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatal(errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{GLMCodingPlan: []config.GLMCodingPlanKey{{APIKey: "secret-key", Site: "cn"}}}, manager)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/glm-coding-plan", nil)
	h.GetGLMCodingPlan(ctx)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "secret-key") {
		t.Fatalf("response leaks secret: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Entries []glmCodingPlanView `json:"glm-coding-plan"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &body); errDecode != nil || len(body.Entries) != 1 {
		t.Fatalf("response = %s err=%v", recorder.Body.String(), errDecode)
	}
	if body.Entries[0].AuthIndex == "" {
		t.Fatalf("missing auth_index: %s", recorder.Body.String())
	}
	quotaSignals, ok := body.Entries[0].Quota["signals"].(map[string]any)
	if !ok || quotaSignals["GLM-Quota-Status"] != "ready" || quotaSignals["GLM-Quota-5h-Used-Percent"] != "12" {
		t.Fatalf("quota = %#v", body.Entries[0].Quota)
	}
}

func TestRefreshGLMCodingPlanQuotaRequiresRuntimeAuth(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{GLMCodingPlan: []config.GLMCodingPlanKey{{APIKey: "secret-key", Site: "cn"}}}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/glm-coding-plan/quota?index=0", nil)
	h.RefreshGLMCodingPlanQuota(ctx)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPutPatchDeleteGLMCodingPlan(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	if errWrite := os.WriteFile(configPath, []byte("{}\n"), 0o600); errWrite != nil {
		t.Fatal(errWrite)
	}
	h := NewHandler(&config.Config{}, configPath, nil)

	putRecorder := httptest.NewRecorder()
	putCtx, _ := gin.CreateTestContext(putRecorder)
	putCtx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/glm-coding-plan", strings.NewReader(`[{"api-key":"secret","site":"international","organization":"team"}]`))
	h.PutGLMCodingPlan(putCtx)
	if putRecorder.Code != http.StatusOK || len(h.cfg.GLMCodingPlan) != 1 || h.cfg.GLMCodingPlan[0].APIKey != "secret" {
		t.Fatalf("PUT status=%d body=%s cfg=%#v", putRecorder.Code, putRecorder.Body.String(), h.cfg.GLMCodingPlan)
	}

	patchRecorder := httptest.NewRecorder()
	patchCtx, _ := gin.CreateTestContext(patchRecorder)
	patchCtx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/glm-coding-plan", strings.NewReader(`{"index":0,"value":{"prefix":"team-a","project":"project-a"}}`))
	h.PatchGLMCodingPlan(patchCtx)
	if patchRecorder.Code != http.StatusOK || h.cfg.GLMCodingPlan[0].Prefix != "team-a" || h.cfg.GLMCodingPlan[0].Project != "project-a" || h.cfg.GLMCodingPlan[0].APIKey != "secret" {
		t.Fatalf("PATCH status=%d body=%s cfg=%#v", patchRecorder.Code, patchRecorder.Body.String(), h.cfg.GLMCodingPlan)
	}

	appendRecorder := httptest.NewRecorder()
	appendCtx, _ := gin.CreateTestContext(appendRecorder)
	appendCtx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/glm-coding-plan", strings.NewReader(`{"index":1,"value":{"api-key":"second","site":"cn"}}`))
	h.PatchGLMCodingPlan(appendCtx)
	if appendRecorder.Code != http.StatusOK || len(h.cfg.GLMCodingPlan) != 2 || h.cfg.GLMCodingPlan[0].APIKey != "secret" || h.cfg.GLMCodingPlan[1].APIKey != "second" {
		t.Fatalf("PATCH append status=%d body=%s cfg=%#v", appendRecorder.Code, appendRecorder.Body.String(), h.cfg.GLMCodingPlan)
	}

	for _, index := range []int{1, 0} {
		deleteRecorder := httptest.NewRecorder()
		deleteCtx, _ := gin.CreateTestContext(deleteRecorder)
		deleteCtx.Request = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v0/management/glm-coding-plan?index=%d", index), nil)
		h.DeleteGLMCodingPlan(deleteCtx)
		if deleteRecorder.Code != http.StatusOK {
			t.Fatalf("DELETE index=%d status=%d body=%s cfg=%#v", index, deleteRecorder.Code, deleteRecorder.Body.String(), h.cfg.GLMCodingPlan)
		}
	}
	if len(h.cfg.GLMCodingPlan) != 0 {
		t.Fatalf("DELETE leftover cfg=%#v", h.cfg.GLMCodingPlan)
	}
}

func TestGLMCodingPlanManagementRejectsInvalidWrites(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	for _, body := range []string{`[{"site":"cn"}]`, `[{"api-key":"key","site":"payg"}]`, `[{"api-key":"key","site":"cn","project":"missing-org"}]`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/glm-coding-plan", strings.NewReader(body))
		h.PutGLMCodingPlan(ctx)
		if recorder.Code != http.StatusBadRequest || len(h.cfg.GLMCodingPlan) != 0 {
			t.Fatalf("invalid PUT status=%d body=%s cfg=%#v", recorder.Code, recorder.Body.String(), h.cfg.GLMCodingPlan)
		}
	}
}
