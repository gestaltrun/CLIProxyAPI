// Package glm applies canonical thinking levels to GLM request payloads.
package glm

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements the GLM Coding Plan thinking policy.
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

func init() {
	thinking.RegisterProvider("glm", &Applier{})
}

// Apply writes the GLM-native reasoning effort for the canonical config.
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}
	effort := glmEffort(config, modelID(modelInfo, body))
	if effort == "" {
		return body, nil
	}
	result, errSet := sjson.SetBytes(body, "reasoning_effort", effort)
	if errSet != nil {
		return body, fmt.Errorf("GLM thinking: set reasoning_effort: %w", errSet)
	}
	return result, nil
}

func modelID(modelInfo *registry.ModelInfo, body []byte) string {
	if modelInfo != nil && strings.TrimSpace(modelInfo.ID) != "" {
		return strings.TrimSpace(modelInfo.ID)
	}
	return strings.TrimSpace(gjson.GetBytes(body, "model").String())
}

func glmEffort(config thinking.ThinkingConfig, model string) string {
	var level thinking.ThinkingLevel
	switch config.Mode {
	case thinking.ModeLevel:
		level = config.Level
	case thinking.ModeBudget:
		converted, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return ""
		}
		level = thinking.ThinkingLevel(converted)
	case thinking.ModeNone:
		level = thinking.LevelLow
	case thinking.ModeAuto:
		level = thinking.LevelHigh
	default:
		return ""
	}
	isGLM53 := strings.EqualFold(strings.TrimSpace(model), "glm-5.3")
	switch level {
	case thinking.LevelXHigh, thinking.LevelMax:
		return "max"
	case thinking.LevelLow:
		if isGLM53 {
			return "low"
		}
		return "high"
	case thinking.LevelMinimal, thinking.LevelMedium, thinking.LevelHigh, thinking.LevelAuto, thinking.LevelNone:
		return "high"
	default:
		return ""
	}
}
