package config

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/glm"
)

// ValidateGLMCodingPlan rejects unusable credentials at the configuration boundary.
func (cfg *Config) ValidateGLMCodingPlan() error {
	if cfg == nil {
		return nil
	}
	for index := range cfg.GLMCodingPlan {
		entry := &cfg.GLMCodingPlan[index]
		if strings.TrimSpace(entry.APIKey) == "" {
			return fmt.Errorf("glm-coding-plan[%d].api-key is required", index)
		}
		endpoints, errEndpoints := glm.ResolveEndpointsForBase(entry.Site, "")
		if errEndpoints != nil {
			return fmt.Errorf("glm-coding-plan[%d].site: %w", index, errEndpoints)
		}
		entry.Site = strings.ToLower(strings.TrimSpace(entry.Site))
		if entry.Site == "" {
			entry.Site = glm.SiteCN
		}
		_ = endpoints
		entry.APIKey = strings.TrimSpace(entry.APIKey)
		entry.Prefix = strings.TrimSpace(entry.Prefix)
		entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
		entry.Organization = strings.TrimSpace(entry.Organization)
		entry.Project = strings.TrimSpace(entry.Project)
		if entry.Project != "" && entry.Organization == "" {
			return fmt.Errorf("glm-coding-plan[%d].organization is required when project is set", index)
		}
	}
	return nil
}
