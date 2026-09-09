package glm

import (
	"context"
	"testing"
)

func TestResolveEndpoints(t *testing.T) {
	tests := []struct {
		site       string
		codingBase string
		quota      string
	}{
		{site: "", codingBase: "https://open.bigmodel.cn/api/coding/paas/v4", quota: "https://open.bigmodel.cn/api/monitor/usage/quota/limit"},
		{site: SiteCN, codingBase: "https://open.bigmodel.cn/api/coding/paas/v4", quota: "https://open.bigmodel.cn/api/monitor/usage/quota/limit"},
		{site: SiteInternational, codingBase: "https://api.z.ai/api/coding/paas/v4", quota: "https://api.z.ai/api/monitor/usage/quota/limit"},
	}
	for _, test := range tests {
		endpoints, err := ResolveEndpoints(test.site)
		if err != nil {
			t.Fatalf("ResolveEndpoints(%q): %v", test.site, err)
		}
		if endpoints.CodingBaseURL != test.codingBase || endpoints.ModelsURL != test.codingBase+"/models" || endpoints.QuotaURL != test.quota {
			t.Fatalf("ResolveEndpoints(%q) = %#v", test.site, endpoints)
		}
	}
	if _, err := ResolveEndpoints("payg"); err == nil {
		t.Fatal("ResolveEndpoints(payg) accepted a non-Coding Plan site")
	}
}

func TestResolveEndpointsForBaseRejectsCustomAndMismatchedBases(t *testing.T) {
	accepted, errAccepted := ResolveEndpointsForBase(SiteCN, "HTTPS://OPEN.BIGMODEL.CN/api/coding/paas/v4/")
	if errAccepted != nil || accepted.QuotaURL == "" {
		t.Fatalf("official normalized base rejected: endpoints=%#v err=%v", accepted, errAccepted)
	}
	for _, test := range []struct {
		site string
		base string
	}{
		{site: SiteCN, base: "https://proxy.example.com/api/coding/paas/v4"},
		{site: SiteCN, base: "https://api.z.ai/api/coding/paas/v4"},
		{site: SiteInternational, base: "https://api.z.ai/v1"},
		{site: SiteInternational, base: "https://api.z.ai/api/coding/paas/v4?target=other"},
	} {
		if _, err := ResolveEndpointsForBase(test.site, test.base); err == nil {
			t.Fatalf("ResolveEndpointsForBase(%q, %q) accepted unsupported base", test.site, test.base)
		}
	}
}

func TestBuildQuotaRequestUsesRawAuthorization(t *testing.T) {
	endpoints, _ := ResolveEndpoints(SiteCN)
	req, err := BuildQuotaRequest(context.Background(), endpoints, "secret", "org", "project")
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Query().Get("type") != "2" {
		t.Fatalf("type = %q", req.URL.Query().Get("type"))
	}
	if got := req.Header.Get("Authorization"); got != "secret" {
		t.Fatalf("Authorization = %q, want raw API key", got)
	}
	if req.Header.Get("bigmodel-organization") != "org" || req.Header.Get("bigmodel-project") != "project" {
		t.Fatalf("team headers = %#v", req.Header)
	}
}
