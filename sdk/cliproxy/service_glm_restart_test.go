package cliproxy

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/glm"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// On restart the core manager registers models for auths loaded by the file token store
// before the watcher synthesizes GLM attributes, so discovery must read the auth file fields.
func TestGLMModelDiscoveryUsesStoreLoadedAuthFile(t *testing.T) {
	dir := t.TempDir()
	payload := `{"type":"glm","api_key":"fixture-key","site":"International","organization":"org-1","project":"proj-1"}`
	if err := os.WriteFile(filepath.Join(dir, "glm-restart.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	store := sdkAuth.NewFileTokenStore()
	store.SetBaseDir(dir)
	auths, errList := store.List(context.Background())
	if errList != nil || len(auths) != 1 {
		t.Fatalf("store.List = %d auths, err %v", len(auths), errList)
	}
	auth := auths[0]
	if auth.Provider != glm.Provider || auth.Attributes["api_key"] != "" {
		t.Fatalf("store-loaded auth provider=%q attributes=%v; test expects the pre-synthesis shape", auth.Provider, auth.Attributes)
	}

	var gotSite, gotAuthorization string
	service := &Service{
		glmResolveEndpoints: func(site, _ string) (glm.Endpoints, error) {
			gotSite = site
			return glm.Endpoints{ModelsURL: "https://fixture.invalid/models"}, nil
		},
		glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client {
			return &http.Client{Transport: serviceRoundTripper(func(req *http.Request) (*http.Response, error) {
				gotAuthorization = req.Header.Get("Authorization")
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"glm-5.3"}]}`))}, nil
			})}
		},
	}
	models, errModels := service.fetchGLMModelsForAuth(context.Background(), auth)
	if errModels != nil {
		t.Fatalf("discovery for store-loaded auth failed: %v", errModels)
	}
	if len(models) != 1 || models[0].ID != "glm-5.3" {
		t.Fatalf("models = %#v", models)
	}
	if gotAuthorization != "Bearer fixture-key" || gotSite != glm.SiteInternational {
		t.Fatalf("authorization=%q site=%q", gotAuthorization, gotSite)
	}

	credentials, ok := glmCredentialsForAuth(auth)
	if !ok || credentials.organization != "org-1" || credentials.project != "proj-1" {
		t.Fatalf("credentials = %#v ok=%v", credentials, ok)
	}
}

func TestGLMModelDiscoveryMissingKeyIsConfigurationError(t *testing.T) {
	service := &Service{}
	auth := &coreauth.Auth{ID: "glm-empty", Provider: glm.Provider, Attributes: map[string]string{}, Metadata: map[string]any{"type": "glm"}}
	_, err := service.fetchGLMModelsForAuth(context.Background(), auth)
	if err == nil || classifyGLMModelDiscoveryError(err) != "configuration" {
		t.Fatalf("err=%v class=%q, want configuration", err, classifyGLMModelDiscoveryError(err))
	}
}
