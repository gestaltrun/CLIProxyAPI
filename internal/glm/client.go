package glm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const MaxAcquisitionBodyBytes int64 = 1 << 20

// BuildQuotaRequest constructs a personal or team Coding Plan quota request.
func BuildQuotaRequest(ctx context.Context, endpoint Endpoints, apiKey, organization, project string) (*http.Request, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("GLM quota request requires an API key")
	}
	target, errTarget := TeamQuotaURL(endpoint.QuotaURL, organization)
	if errTarget != nil {
		return nil, errTarget
	}
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if errRequest != nil {
		return nil, fmt.Errorf("build GLM quota request: %w", errRequest)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)
	if organization = strings.TrimSpace(organization); organization != "" {
		req.Header.Set("bigmodel-organization", organization)
		if project = strings.TrimSpace(project); project != "" {
			req.Header.Set("bigmodel-project", project)
		}
	}
	return req, nil
}

// ProbeQuota obtains one bounded Coding Plan quota observation.
func ProbeQuota(ctx context.Context, client *http.Client, endpoint Endpoints, apiKey, organization, project string, previous QuotaSnapshot, now time.Time) QuotaSnapshot {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := previous
	result.ObservedAt = now.UTC()
	req, errRequest := BuildQuotaRequest(ctx, endpoint, apiKey, organization, project)
	if errRequest != nil {
		result.Status = "error"
		result.Error = errRequest.Error()
		return result
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("GLM quota request failed: %v", errDo)
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, MaxAcquisitionBodyBytes+1))
	if errRead != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("read GLM quota response: %v", errRead)
		return result
	}
	if int64(len(body)) > MaxAcquisitionBodyBytes {
		result.Status = "error"
		result.Error = fmt.Sprintf("GLM quota response exceeds %d bytes", MaxAcquisitionBodyBytes)
		return result
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Status = "stale"
		result.CredentialValid = false
		result.Error = fmt.Sprintf("GLM quota authentication failed with HTTP %d", resp.StatusCode)
		return result
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		result.Status = "error"
		result.Error = fmt.Sprintf("GLM quota request returned HTTP %d", resp.StatusCode)
		return result
	}
	planLevel, windows, errParse := ParseQuotaResponse(body)
	if errParse != nil {
		result.Status = "error"
		result.Error = errParse.Error()
		return result
	}
	result.Status = "ready"
	result.CredentialValid = true
	result.LastSuccessfulAt = now.UTC()
	result.PlanLevel = planLevel
	result.Windows = windows
	result.Error = ""
	return result
}
