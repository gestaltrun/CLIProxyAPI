package cliproxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/glm"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestFetchGLMModelsForAuthUsesBearerAndDynamicCatalog(t *testing.T) {
	var request *http.Request
	service := &Service{
		glmResolveEndpoints: func(string) (glm.Endpoints, error) {
			return glm.Endpoints{ModelsURL: "https://fixture.invalid/models"}, nil
		},
		glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client {
			return &http.Client{Transport: serviceRoundTripper(func(req *http.Request) (*http.Response, error) {
				request = req
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"glm-5.3"},{"id":"glm-4.7"},{"id":"glm-5.3"}]}`))}, nil
			})}
		},
	}
	auth := &coreauth.Auth{ID: "glm-a", Provider: glm.Provider, Attributes: map[string]string{"api_key": "secret", "glm_site": glm.SiteCN}}
	models, err := service.fetchGLMModelsForAuth(context.Background(), auth)
	if err != nil {
		t.Fatal(err)
	}
	if request == nil || request.URL.String() != "https://fixture.invalid/models" || request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("request = %#v", request)
	}
	if len(models) != 2 || models[0].ID != "glm-4.7" || models[1].ID != "glm-5.3" {
		t.Fatalf("models = %#v", models)
	}
}

func TestGLMQuotaSignalsExposeStaleSnapshotWithoutSecrets(t *testing.T) {
	signals := glmQuotaSignals(glm.QuotaSnapshot{
		Status:           "stale",
		CredentialValid:  false,
		LastSuccessfulAt: time.Unix(10, 0),
		Windows:          []glm.QuotaWindow{{Window: glm.WindowFiveHour, UsedPercent: 51, ResetAt: time.Unix(20, 0)}},
		Error:            "authentication failed",
	})
	if signals["GLM-Quota-Status"] != "stale" || signals["GLM-Quota-5h-Used-Percent"] != "51" {
		t.Fatalf("signals = %#v", signals)
	}
	for key, value := range signals {
		if strings.Contains(strings.ToLower(key), "api-key") || strings.Contains(value, "secret") {
			t.Fatalf("secret-shaped signal: %s=%q", key, value)
		}
	}
}

type serviceRoundTripper func(*http.Request) (*http.Response, error)

func (f serviceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
