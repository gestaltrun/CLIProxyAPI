package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatGLMUsesCodingEndpointBearerAndEffort(t *testing.T) {
	var captured *http.Request
	var body []byte
	serverTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		body, _ = io.ReadAll(req.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-test","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
		}, nil
	})
	auth := &cliproxyauth.Auth{Provider: "glm", Attributes: map[string]string{
		"base_url": "https://open.bigmodel.cn/api/coding/paas/v4",
		"api_key":  "secret",
	}}
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(serverTransport))
	executor := NewOpenAICompatExecutor("glm", nil)
	_, err := executor.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "glm-5.3",
		Payload: []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"low"}`),
		Format:  sdktranslator.FromString("openai"),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.URL.String() != "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions" {
		t.Fatalf("request URL = %#v", captured)
	}
	if captured.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("Authorization = %q", captured.Header.Get("Authorization"))
	}
	if got := gjson.GetBytes(body, "reasoning_effort").String(); got != "low" {
		t.Fatalf("reasoning_effort = %q body=%s", got, body)
	}
}
