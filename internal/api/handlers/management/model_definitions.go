package management

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	codexmodels "github.com/router-for-me/CLIProxyAPI/v7/internal/client/codex/models"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// GetStaticModelDefinitions returns static model metadata for a given channel.
// Channel is provided via path param (:channel) or query param (?channel=...).
//
// Every model carries context_window (the default context budget) and max_context_window
// (the largest context the model accepts) when either is known. Codex models take both from
// the Codex client catalog, falling back to context_length for a missing default; other models
// report context_length for both.
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
	if contextWindow > 0 {
		entry["context_window"] = contextWindow
	}
	if maxContextWindow > 0 {
		entry["max_context_window"] = maxContextWindow
	}
	return entry, nil
}
