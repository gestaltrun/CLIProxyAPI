// Package glm implements provider-specific GLM Coding Plan protocol behavior.
package glm

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	// Provider is the auth and executor provider key for GLM Coding Plan.
	Provider = "glm"

	SiteCN            = "cn"
	SiteInternational = "international"
)

// Endpoints contains the official upstream endpoints for one GLM site.
type Endpoints struct {
	CodingBaseURL string
	ModelsURL     string
	QuotaURL      string
}

// ResolveEndpoints returns the official endpoint set for a GLM Coding Plan site.
func ResolveEndpoints(site string) (Endpoints, error) {
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "", SiteCN:
		return endpointsForBase("https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/monitor/usage/quota/limit"), nil
	case SiteInternational:
		return endpointsForBase("https://api.z.ai/api/coding/paas/v4", "https://api.z.ai/api/monitor/usage/quota/limit"), nil
	default:
		return Endpoints{}, fmt.Errorf("unsupported GLM Coding Plan site %q", site)
	}
}

func endpointsForBase(codingBaseURL, quotaURL string) Endpoints {
	return Endpoints{
		CodingBaseURL: strings.TrimRight(codingBaseURL, "/"),
		ModelsURL:     strings.TrimRight(codingBaseURL, "/") + "/models",
		QuotaURL:      quotaURL,
	}
}

// TeamQuotaURL adds the official team-plan selector to the quota endpoint.
func TeamQuotaURL(endpoint string, organization string) (string, error) {
	if strings.TrimSpace(organization) == "" {
		return endpoint, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse GLM quota endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("type", "2")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
