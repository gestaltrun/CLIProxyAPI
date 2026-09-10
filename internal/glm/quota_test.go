package glm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestParseQuotaResponseUsesUnitsAndTokenLimits(t *testing.T) {
	body := []byte(`{"success":true,"data":{"level":"pro","limits":[{"type":"CREDIT_LIMIT","unit":3,"percentage":99},{"type":"TOKENS_LIMIT","unit":6,"percentage":"27","nextResetTime":2000000000},{"type":"TOKENS_LIMIT","unit":3,"percentage":12,"nextResetTime":2000000000000}]}}`)
	level, windows, err := ParseQuotaResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if level != "pro" || len(windows) != 2 {
		t.Fatalf("level=%q windows=%#v", level, windows)
	}
	if windows[0].Window != WindowFiveHour || windows[0].UsedPercent != 12 {
		t.Fatalf("five-hour = %#v", windows[0])
	}
	if windows[1].Window != WindowWeekly || windows[1].UsedPercent != 27 {
		t.Fatalf("weekly = %#v", windows[1])
	}
}

func TestParseQuotaResponseFallsBackToCreditOnly(t *testing.T) {
	_, windows, err := ParseQuotaResponse([]byte(`{"success":true,"data":{"limits":[{"type":"CREDIT_LIMIT","percentage":"41"}]}}`))
	if err != nil || len(windows) != 1 || windows[0].UsedPercent != 41 {
		t.Fatalf("windows=%#v err=%v", windows, err)
	}
}

func TestProbeQuotaPreservesPreviousSnapshotOnUnauthorized(t *testing.T) {
	previous := QuotaSnapshot{
		Status:           "ready",
		CredentialValid:  true,
		LastSuccessfulAt: time.Unix(10, 0),
		Windows:          []QuotaWindow{{Window: WindowFiveHour, UsedPercent: 50}},
	}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("quota request used Bearer authentication")
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	endpoints, _ := ResolveEndpoints(SiteCN)
	got := ProbeQuota(context.Background(), client, endpoints, "secret", "", "", previous, time.Unix(20, 0))
	if got.Status != "stale" || got.CredentialValid || !got.LastSuccessfulAt.Equal(previous.LastSuccessfulAt) || len(got.Windows) != 1 {
		t.Fatalf("snapshot = %#v", got)
	}
}
