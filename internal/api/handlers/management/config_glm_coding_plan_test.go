package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
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

	deleteRecorder := httptest.NewRecorder()
	deleteCtx, _ := gin.CreateTestContext(deleteRecorder)
	deleteCtx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/glm-coding-plan?index=0", nil)
	h.DeleteGLMCodingPlan(deleteCtx)
	if deleteRecorder.Code != http.StatusOK || len(h.cfg.GLMCodingPlan) != 0 {
		t.Fatalf("DELETE status=%d body=%s cfg=%#v", deleteRecorder.Code, deleteRecorder.Body.String(), h.cfg.GLMCodingPlan)
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
