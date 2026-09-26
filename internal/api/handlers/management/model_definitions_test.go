package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func getModelDefinitions(t *testing.T, channel string) map[string]map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/model-definitions/"+channel, nil)
	ctx.Params = gin.Params{{Key: "channel", Value: channel}}
	(&Handler{}).GetStaticModelDefinitions(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := make(map[string]map[string]any, len(payload.Models))
	for _, model := range payload.Models {
		id, _ := model["id"].(string)
		byID[id] = model
	}
	return byID
}

func TestModelDefinitionsCodexReportDefaultAndMaximumContext(t *testing.T) {
	models := getModelDefinitions(t, "codex")
	astra, ok := models["gpt-6-astra"]
	if !ok {
		t.Fatal("codex definitions omit gpt-6-astra")
	}
	if got := astra["context_window"]; got != float64(272000) {
		t.Fatalf("context_window = %#v, want 272000", got)
	}
	if got := astra["max_context_window"]; got != float64(872000) {
		t.Fatalf("max_context_window = %#v, want 872000", got)
	}
	if got := astra["max_completion_tokens"]; got != float64(128000) {
		t.Fatalf("max_completion_tokens = %#v, want 128000", got)
	}
}

func TestModelDefinitionsWithoutClientCatalogRepeatContextLength(t *testing.T) {
	checked := 0
	for id, model := range getModelDefinitions(t, "claude") {
		length, ok := model["context_length"].(float64)
		if !ok || length <= 0 {
			continue
		}
		checked++
		if model["context_window"] != length || model["max_context_window"] != length {
			t.Fatalf("%s: context_window=%#v max_context_window=%#v, want both %v", id, model["context_window"], model["max_context_window"], length)
		}
	}
	if checked == 0 {
		t.Fatal("claude definitions carry no context_length")
	}
}

func TestModelDefinitionsGLMReportContextWindows(t *testing.T) {
	models := getModelDefinitions(t, "glm")
	model, ok := models["glm-5.3"]
	if !ok {
		t.Fatal("glm definitions omit glm-5.3")
	}
	if model["context_window"] != float64(202752) || model["max_context_window"] != float64(202752) {
		t.Fatalf("context_window=%#v max_context_window=%#v, want both 202752", model["context_window"], model["max_context_window"])
	}
}
