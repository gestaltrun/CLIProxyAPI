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
	return ResolveEndpointsForBase(site, "")
}

// ResolveEndpointsForBase verifies that the selected inference base is the official
// Coding Plan base for the selected site before deriving its quota endpoint.
func ResolveEndpointsForBase(site, selectedBaseURL string) (Endpoints, error) {
	var expected Endpoints
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "", SiteCN:
		expected = endpointsForBase("https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/monitor/usage/quota/limit")
	case SiteInternational:
		expected = endpointsForBase("https://api.z.ai/api/coding/paas/v4", "https://api.z.ai/api/monitor/usage/quota/limit")
	default:
		return Endpoints{}, fmt.Errorf("unsupported GLM Coding Plan site %q", site)
	}
	selectedBaseURL = strings.TrimSpace(selectedBaseURL)
	if selectedBaseURL == "" {
		return expected, nil
	}
	selected, errSelected := normalizeCodingBaseURL(selectedBaseURL)
	if errSelected != nil {
		return Endpoints{}, errSelected
	}
	official, _ := normalizeCodingBaseURL(expected.CodingBaseURL)
	if selected != official {
		return Endpoints{}, fmt.Errorf("GLM quota observation is unsupported for non-official Coding Plan base %q", selectedBaseURL)
	}
	return expected, nil
}

func normalizeCodingBaseURL(raw string) (string, error) {
	parsed, errParse := url.Parse(strings.TrimSpace(raw))
	if errParse != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid GLM Coding Plan base %q", raw)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid GLM Coding Plan base %q", raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.RawPath = ""
	return parsed.String(), nil
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
