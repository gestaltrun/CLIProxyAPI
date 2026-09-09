package cliproxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/glm"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestFetchGLMModelsForAuthUsesBearerAndDynamicCatalog(t *testing.T) {
	var request *http.Request
	service := &Service{
		glmResolveEndpoints: func(string, string) (glm.Endpoints, error) {
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

func TestRefreshGLMQuotaCustomBaseDoesNotCreateHTTPClient(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: "glm-custom", Provider: glm.Provider, Status: coreauth.StatusActive, Attributes: map[string]string{
		"api_key":  "secret",
		"glm_site": glm.SiteCN,
		"base_url": "https://proxy.example.com/api/coding/paas/v4",
	}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	clientCalls := 0
	service := &Service{
		coreManager: manager,
		glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client {
			clientCalls++
			return &http.Client{}
		},
	}
	service.refreshGLMQuotaForAuth(context.Background(), auth)
	if clientCalls != 0 {
		t.Fatalf("HTTP client calls = %d, want 0 for custom base", clientCalls)
	}
}

func TestFetchGLMModelsCustomBaseDoesNotCreateHTTPClient(t *testing.T) {
	clientCalls := 0
	service := &Service{glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client {
		clientCalls++
		return &http.Client{}
	}}
	auth := &coreauth.Auth{ID: "glm-custom", Provider: glm.Provider, Attributes: map[string]string{
		"api_key":  "secret",
		"glm_site": glm.SiteCN,
		"base_url": "https://proxy.example.com/api/coding/paas/v4",
	}}
	if _, err := service.fetchGLMModelsForAuth(context.Background(), auth); err == nil {
		t.Fatal("custom base model discovery succeeded")
	}
	if clientCalls != 0 {
		t.Fatalf("HTTP client calls = %d, want 0 for custom base", clientCalls)
	}
}

func TestConcurrentGLMQuotaRefreshesCoalesceByAuth(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: "glm-coalesce", Provider: glm.Provider, Status: coreauth.StatusActive, Attributes: map[string]string{
		"api_key":  "secret",
		"glm_site": glm.SiteCN,
		"base_url": "https://open.bigmodel.cn/api/coding/paas/v4",
	}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	entered := make(chan struct{})
	secondAtFlight := make(chan struct{})
	release := make(chan struct{})
	flightEntries := 0
	var flightMu sync.Mutex
	calls := 0
	var callsMu sync.Mutex
	service := &Service{
		coreManager: manager,
		glmQuotaBeforeFlight: func(string) {
			flightMu.Lock()
			flightEntries++
			if flightEntries == 2 {
				close(secondAtFlight)
			}
			flightMu.Unlock()
		},
		glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client { return &http.Client{} },
		glmQuotaProbe: func(_ context.Context, _ *http.Client, _ glm.Endpoints, _, _, _ string, _ glm.QuotaSnapshot, now time.Time) glm.QuotaSnapshot {
			callsMu.Lock()
			calls++
			if calls == 1 {
				close(entered)
			}
			callsMu.Unlock()
			<-release
			return glm.QuotaSnapshot{Status: "ready", CredentialValid: true, ObservedAt: now}
		},
	}
	done := make(chan struct{}, 2)
	go func() { service.refreshGLMQuotaForAuth(context.Background(), auth); done <- struct{}{} }()
	<-entered
	go func() { service.refreshGLMQuotaForAuth(context.Background(), auth); done <- struct{}{} }()
	<-secondAtFlight
	close(release)
	<-done
	<-done
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 {
		t.Fatalf("quota probe calls = %d, want 1", calls)
	}
}

func TestStartStopGLMQuotaPollingCancelsWorker(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	_, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: "glm-a", Provider: glm.Provider, Status: coreauth.StatusActive, Attributes: map[string]string{"api_key": "secret", "glm_site": glm.SiteCN}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	entered := make(chan struct{})
	service := &Service{
		coreManager: manager,
		glmResolveEndpoints: func(string, string) (glm.Endpoints, error) {
			return glm.Endpoints{QuotaURL: "https://fixture.invalid/quota"}, nil
		},
		glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client { return &http.Client{} },
		glmQuotaProbe: func(ctx context.Context, _ *http.Client, _ glm.Endpoints, _, _, _ string, previous glm.QuotaSnapshot, _ time.Time) glm.QuotaSnapshot {
			close(entered)
			<-ctx.Done()
			return previous
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	service.startGLMQuotaPolling(ctx)
	<-entered
	cancel()
	service.stopGLMQuotaPolling()
}

func TestGLMQuotaObservationPreservesConcurrentAuthConfiguration(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	registered, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: "glm-race", Provider: glm.Provider, Status: coreauth.StatusActive, Attributes: map[string]string{
		"api_key":          "secret",
		"glm_site":         glm.SiteCN,
		"base_url":         "https://open.bigmodel.cn/api/coding/paas/v4",
		"glm_organization": "old-org",
	}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	service := &Service{
		coreManager:   manager,
		glmHTTPClient: func(context.Context, *coreauth.Auth, time.Duration) *http.Client { return &http.Client{} },
		glmQuotaProbe: func(_ context.Context, _ *http.Client, _ glm.Endpoints, _, _, _ string, _ glm.QuotaSnapshot, now time.Time) glm.QuotaSnapshot {
			close(entered)
			<-release
			return glm.QuotaSnapshot{Status: "ready", CredentialValid: true, ObservedAt: now, LastSuccessfulAt: now}
		},
	}
	done := make(chan struct{})
	go func() {
		service.refreshGLMQuotaForAuth(context.Background(), registered)
		close(done)
	}()
	<-entered
	latest, _ := manager.GetByID(registered.ID)
	latest.Attributes["glm_organization"] = "new-org"
	latest.Attributes[coreauth.AttributeWeight] = "9"
	if _, errUpdate := manager.Update(context.Background(), latest); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	close(release)
	<-done
	final, _ := manager.GetByID(registered.ID)
	if final.Attributes["glm_organization"] != "new-org" || final.Attributes[coreauth.AttributeWeight] != "9" {
		t.Fatalf("concurrent attributes overwritten: %#v", final.Attributes)
	}
	if final.Quota.Signals["GLM-Quota-Status"] != "ready" {
		t.Fatalf("quota signals missing: %#v", final.Quota.Signals)
	}
}

func TestApplyGLMQuotaAvailabilityUsesOnlyCredentialFailureAndActualExhaustion(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &coreauth.Auth{Status: coreauth.StatusActive}
	applyGLMQuotaAvailability(auth, glm.QuotaSnapshot{Status: "ready", CredentialValid: true, ObservedAt: now, Windows: []glm.QuotaWindow{{Window: glm.WindowFiveHour, UsedPercent: 99.9, ResetAt: now.Add(time.Hour)}}})
	if auth.Unavailable {
		t.Fatal("sub-100 utilization blocked auth")
	}
	applyGLMQuotaAvailability(auth, glm.QuotaSnapshot{Status: "ready", CredentialValid: true, ObservedAt: now, Windows: []glm.QuotaWindow{{Window: glm.WindowFiveHour, UsedPercent: 100, ResetAt: now.Add(time.Hour)}}})
	if !auth.Unavailable || !auth.NextRetryAfter.Equal(now.Add(time.Hour)) {
		t.Fatalf("exhausted auth = %#v", auth)
	}
	applyGLMQuotaAvailability(auth, glm.QuotaSnapshot{Status: "ready", CredentialValid: true, ObservedAt: now.Add(2 * time.Hour)})
	if auth.Unavailable || auth.Status != coreauth.StatusActive {
		t.Fatalf("recovered auth = %#v", auth)
	}
	applyGLMQuotaAvailability(auth, glm.QuotaSnapshot{Status: "stale", CredentialValid: false, ObservedAt: now})
	if !auth.Unavailable || auth.Status != coreauth.StatusError || !auth.NextRetryAfter.IsZero() {
		t.Fatalf("rejected auth = %#v", auth)
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
