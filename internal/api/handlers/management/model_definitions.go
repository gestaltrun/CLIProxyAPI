package management

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	codexmodels "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/models"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

// GetStaticModelDefinitions returns static model metadata for a given channel.
// Channel is provided via path param (:channel) or query param (?channel=...).
//
// Every model carries context_window (the default context budget) and max_context_window
// (the vendor-documented largest context) when either is known. These come from ModelInfo
// ContextLength / MaxContextWindow, not from MaxContextLength (a config override used only
// for Codex catalog generation). Codex models take both from the Codex client catalog,
// falling back to context_length for a missing default. Other models report context_length
// as context_window and the registry max_context_window, or context_length when the
// registry has no separate maximum.
//
// default_reasoning_level is the Thinking level the upstream applies when a request sets none.
// Codex models take it from the Codex client catalog when the level is one of the model's
// Thinking levels; other models report the registry value, which is set only where official
// documentation states the default. Models without a known default omit the field.
func (h *Handler) GetStaticModelDefinitions(c *gin.Context) {
	channel := strings.TrimSpace(c.Param("channel"))
	if channel == "" {
		channel = strings.TrimSpace(c.Query("channel"))
	}
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel is required"})
		return
	}

	models := registry.GetStaticModelDefinitionsByChannel(channel)
	if models == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown channel", "channel": channel})
		return
	}

	normalized := strings.ToLower(channel)
	entries := make([]map[string]any, 0, len(models))
	for _, model := range models {
		entry, err := modelDefinitionEntry(model, normalized == "codex")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode model definitions"})
			return
		}
		if entry != nil {
			entries = append(entries, entry)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"channel": normalized,
		"models":  entries,
	})
}

func modelDefinitionEntry(model *registry.ModelInfo, codexChannel bool) (map[string]any, error) {
	if model == nil {
		return nil, nil
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	var entry map[string]any
	if err = json.Unmarshal(raw, &entry); err != nil {
		return nil, err
	}
	contextWindow, maxContextWindow := model.ContextLength, model.ContextLength
	if model.MaxContextWindow > 0 {
		maxContextWindow = model.MaxContextWindow
	}
	// Codex windows come from the client catalog that already emits
	// context_window / max_context_window. Do not copy those numbers into
	// models.json; other channels use registry ContextLength / MaxContextWindow.
	if codexChannel {
		if clientWindow, clientMax, ok := codexmodels.ClientContextWindows(model.ID); ok {
			if clientWindow > 0 {
				contextWindow = clientWindow
			}
			maxContextWindow = clientMax
		}
	}
	if maxContextWindow < contextWindow {
		maxContextWindow = contextWindow
	}
	if codexChannel {
		if level := codexmodels.ClientDefaultReasoningLevel(model.ID); level != "" && modelSupportsLevel(model, level) {
			entry["default_reasoning_level"] = level
		}
	}
	if contextWindow > 0 {
		entry["context_window"] = contextWindow
	}
	if maxContextWindow > 0 {
		entry["max_context_window"] = maxContextWindow
	}
	return entry, nil
}

func modelSupportsLevel(model *registry.ModelInfo, level string) bool {
	if model == nil || model.Thinking == nil {
		return false
	}
	for _, candidate := range model.Thinking.Levels {
		if strings.EqualFold(candidate, level) {
			return true
		}
	}
	return false
}
