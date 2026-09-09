package glm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	WindowFiveHour = "5h"
	WindowWeekly   = "weekly"
)

// QuotaWindow is one observed Coding Plan utilization window.
type QuotaWindow struct {
	Window      string
	UsedPercent float64
	ResetAt     time.Time
}

// QuotaSnapshot is the management-safe state of the latest quota observation.
type QuotaSnapshot struct {
	Status           string
	CredentialValid  bool
	ObservedAt       time.Time
	LastSuccessfulAt time.Time
	PlanLevel        string
	Windows          []QuotaWindow
	Error            string
}

// ParseQuotaResponse parses a successful GLM quota response without depending on source-project types.
func ParseQuotaResponse(body []byte) (string, []QuotaWindow, error) {
	var envelope struct {
		Success *bool  `json:"success"`
		Message string `json:"msg"`
		Data    struct {
			Level  string `json:"level"`
			Limits []struct {
				Type          string          `json:"type"`
				Unit          json.RawMessage `json:"unit"`
				Percentage    json.RawMessage `json:"percentage"`
				NextResetTime json.RawMessage `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return "", nil, fmt.Errorf("decode GLM quota response: %w", err)
	}
	if envelope.Success != nil && !*envelope.Success {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = "upstream rejected quota request"
		}
		return "", nil, fmt.Errorf("GLM quota response: %s", message)
	}

	tokenEntries := make([]quotaEntry, 0, len(envelope.Data.Limits))
	creditEntries := make([]quotaEntry, 0, len(envelope.Data.Limits))
	for _, item := range envelope.Data.Limits {
		kind := strings.ToUpper(strings.TrimSpace(item.Type))
		if kind != "TOKENS_LIMIT" && kind != "CREDIT_LIMIT" {
			continue
		}
		percentage, ok := parseNumber(item.Percentage)
		if !ok {
			continue
		}
		entry := quotaEntry{
			unit:        parseInteger(item.Unit),
			utilization: percentage,
			resetAt:     parseResetTime(item.NextResetTime),
		}
		if kind == "TOKENS_LIMIT" {
			tokenEntries = append(tokenEntries, entry)
		} else {
			creditEntries = append(creditEntries, entry)
		}
	}
	entries := tokenEntries
	if len(entries) == 0 {
		entries = creditEntries
	}
	return strings.TrimSpace(envelope.Data.Level), classifyQuotaEntries(entries), nil
}

type quotaEntry struct {
	unit        int64
	utilization float64
	resetAt     time.Time
}

func classifyQuotaEntries(entries []quotaEntry) []QuotaWindow {
	var fiveHour, weekly *quotaEntry
	unclassified := make([]quotaEntry, 0, len(entries))
	for i := range entries {
		entry := entries[i]
		switch entry.unit {
		case 3:
			if fiveHour == nil {
				copyEntry := entry
				fiveHour = &copyEntry
			} else {
				unclassified = append(unclassified, entry)
			}
		case 6:
			if weekly == nil {
				copyEntry := entry
				weekly = &copyEntry
			} else {
				unclassified = append(unclassified, entry)
			}
		default:
			unclassified = append(unclassified, entry)
		}
	}
	sort.SliceStable(unclassified, func(i, j int) bool {
		left, right := unclassified[i].resetAt, unclassified[j].resetAt
		if left.IsZero() != right.IsZero() {
			return left.IsZero()
		}
		return left.Before(right)
	})
	for i := range unclassified {
		entry := unclassified[i]
		if fiveHour == nil {
			copyEntry := entry
			fiveHour = &copyEntry
			continue
		}
		if weekly == nil {
			copyEntry := entry
			weekly = &copyEntry
		}
	}
	out := make([]QuotaWindow, 0, 2)
	if fiveHour != nil {
		out = append(out, QuotaWindow{Window: WindowFiveHour, UsedPercent: fiveHour.utilization, ResetAt: fiveHour.resetAt})
	}
	if weekly != nil {
		out = append(out, QuotaWindow{Window: WindowWeekly, UsedPercent: weekly.utilization, ResetAt: weekly.resetAt})
	}
	return out
}

func parseNumber(raw json.RawMessage) (float64, bool) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, false
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, false
		}
		value = strings.TrimSpace(text)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

func parseInteger(raw json.RawMessage) int64 {
	value, ok := parseNumber(raw)
	if !ok {
		return 0
	}
	return int64(value)
}

func parseResetTime(raw json.RawMessage) time.Time {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return time.Time{}
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return time.Time{}
		}
		text = strings.TrimSpace(text)
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed.UTC()
			}
		}
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return unixTimestamp(parsed)
		}
		return time.Time{}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return unixTimestamp(parsed)
}

func unixTimestamp(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value < 1_000_000_000_000 {
		return time.Unix(value, 0).UTC()
	}
	return time.UnixMilli(value).UTC()
}
