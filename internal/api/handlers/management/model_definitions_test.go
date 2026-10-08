package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
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

func TestModelDefinitionsWithoutClientCatalogUseRegistryWindows(t *testing.T) {
	for _, channel := range []string{"claude", "xai", "kimi", "glm"} {
		entries := getModelDefinitions(t, channel)
		checked := 0
		for _, model := range registry.GetStaticModelDefinitionsByChannel(channel) {
			if model == nil || model.ContextLength <= 0 {
				continue
			}
			checked++
			entry := entries[model.ID]
			wantMax := model.ContextLength
			if model.MaxContextWindow > wantMax {
				wantMax = model.MaxContextWindow
			}
			if entry["context_window"] != float64(model.ContextLength) || entry["max_context_window"] != float64(wantMax) {
				t.Errorf("%s/%s: context_window=%#v max_context_window=%#v, want %d / %d", channel, model.ID, entry["context_window"], entry["max_context_window"], model.ContextLength, wantMax)
			}
		}
		if checked == 0 {
			t.Fatalf("%s definitions carry no context_length", channel)
		}
	}
}

func TestModelDefinitionsReportCorrectedCapabilities(t *testing.T) {
	type want struct {
		contextWindow  float64
		maxContext     float64
		maxCompletion  float64
		input          string
		levels         string
		defaultLevel   string
		omitDefault    bool
		skipCompletion bool
	}
	cases := []struct {
		channel string
		id      string
		want    want
	}{
		{"glm", "glm-5.3", want{contextWindow: 1000000, maxCompletion: 131072, input: "text", levels: "low,high,max", defaultLevel: "max"}},
		{"glm", "glm-5.3-flash", want{contextWindow: 1000000, maxCompletion: 131072, input: "text,image", levels: "low,high,max", defaultLevel: "max"}},
		{"glm", "glm-5.2", want{contextWindow: 1000000, maxCompletion: 131072, input: "text", levels: "high,max", defaultLevel: "max"}},
		{"glm", "glm-5.1", want{contextWindow: 1000000, maxCompletion: 131072, input: "text", levels: "high,max", defaultLevel: "max"}},
		{"glm", "glm-5-turbo", want{contextWindow: 202752, maxCompletion: 131072, input: "text", levels: "high,max", defaultLevel: "max"}},
		{"claude", "claude-sonnet-4-6", want{contextWindow: 200000, maxContext: 1000000, maxCompletion: 128000, input: "text,image", levels: "low,medium,high,max", defaultLevel: "high"}},
		{"claude", "claude-opus-4-6", want{contextWindow: 200000, maxContext: 1000000, maxCompletion: 128000, input: "text,image", levels: "low,medium,high,max", defaultLevel: "high"}},
		{"claude", "claude-opus-4-7", want{contextWindow: 1000000, maxCompletion: 128000, input: "text,image", levels: "low,medium,high,xhigh,max", defaultLevel: "high"}},
		{"claude", "claude-opus-5-5", want{contextWindow: 1000000, maxCompletion: 128000, input: "text,image", levels: "low,medium,high,xhigh,max", defaultLevel: "medium"}},
		{"claude", "claude-opus-4-5-20251101", want{contextWindow: 200000, maxCompletion: 64000, input: "text,image", omitDefault: true}},
		{"xai", "grok-4.20-0309-reasoning", want{contextWindow: 1000000, maxCompletion: 65536, input: "text,image", omitDefault: true}},
		{"xai", "grok-4.20-0309-non-reasoning", want{contextWindow: 1000000, maxCompletion: 65536, input: "text,image", omitDefault: true}},
		{"xai", "grok-4.20-multi-agent-0309", want{contextWindow: 1000000, maxCompletion: 65536, input: "text,image", levels: "low,medium,high", omitDefault: true}},
		{"xai", "grok-4.3", want{contextWindow: 1000000, maxCompletion: 65536, input: "text,image", levels: "none,low,medium,high,xhigh", defaultLevel: "low"}},
		{"xai", "grok-4.7", want{contextWindow: 500000, maxCompletion: 500000, input: "text,image", levels: "low,medium,high,xhigh", defaultLevel: "high"}},
		{"antigravity", "gemini-3.1-flash-image", want{contextWindow: 131072, maxCompletion: 32768, input: "text,image", levels: "minimal,high", omitDefault: true}},
		{"kimi", "kimi-k2.7-code", want{contextWindow: 1048576, maxCompletion: 65536, input: "text,image,video", levels: "low,high,max", defaultLevel: "max"}},
		{"kimi", "kimi-k3", want{contextWindow: 1048576, maxCompletion: 65536, input: "text,image,video", levels: "low,high,max", defaultLevel: "high"}},
		{"kimi", "kimi-k2.7-code-highspeed", want{contextWindow: 262144, maxCompletion: 65536, input: "text,image,video", levels: "low,high", omitDefault: true}},
		{"codex", "gpt-6-astra", want{contextWindow: 272000, maxContext: 872000, maxCompletion: 128000, input: "text,image", defaultLevel: "medium"}},
		{"codex", "gpt-5.6-sol", want{contextWindow: 272000, maxContext: 872000, skipCompletion: true, input: "text,image", defaultLevel: "low"}},
	}
	byChannel := map[string]map[string]map[string]any{}
	for _, tc := range cases {
		t.Run(tc.channel+"/"+tc.id, func(t *testing.T) {
			if byChannel[tc.channel] == nil {
				byChannel[tc.channel] = getModelDefinitions(t, tc.channel)
			}
			model, ok := byChannel[tc.channel][tc.id]
			if !ok {
				t.Fatalf("%s definitions omit %s", tc.channel, tc.id)
			}
			if got := model["context_window"]; got != tc.want.contextWindow {
				t.Errorf("context_window = %#v, want %v", got, tc.want.contextWindow)
			}
			wantMax := tc.want.maxContext
			if wantMax == 0 {
				wantMax = tc.want.contextWindow
			}
			if got := model["max_context_window"]; got != wantMax {
				t.Errorf("max_context_window = %#v, want %v", got, wantMax)
			}
			if !tc.want.skipCompletion {
				if got := model["max_completion_tokens"]; got != tc.want.maxCompletion {
					t.Errorf("max_completion_tokens = %#v, want %v", got, tc.want.maxCompletion)
				}
			}
			if got := joinStrings(model["supportedInputModalities"]); got != tc.want.input {
				t.Errorf("supportedInputModalities = %s, want %s", got, tc.want.input)
			}
			if tc.want.levels != "" {
				thinking, _ := model["thinking"].(map[string]any)
				if got := joinStrings(thinking["levels"]); got != tc.want.levels {
					t.Errorf("thinking.levels = %s, want %s", got, tc.want.levels)
				}
			}
			got, present := model["default_reasoning_level"]
			if tc.want.omitDefault {
				if present {
					t.Errorf("default_reasoning_level = %#v, want omitted", got)
				}
			} else if got != tc.want.defaultLevel {
				t.Errorf("default_reasoning_level = %#v, want %q", got, tc.want.defaultLevel)
			}
		})
	}
}

func joinStrings(value any) string {
	items, _ := value.([]any)
	parts := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		parts = append(parts, text)
	}
	return strings.Join(parts, ",")
}
