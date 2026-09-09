package glm

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func TestGLMEffortMatrix(t *testing.T) {
	tests := []struct {
		model string
		level thinking.ThinkingLevel
		want  string
	}{
		{model: "glm-5.3", level: thinking.LevelLow, want: "low"},
		{model: "glm-5.3", level: thinking.LevelMedium, want: "high"},
		{model: "glm-5.3", level: thinking.LevelXHigh, want: "max"},
		{model: "glm-4.7", level: thinking.LevelLow, want: "high"},
		{model: "glm-4.7", level: thinking.LevelHigh, want: "high"},
		{model: "glm-4.7", level: thinking.LevelMax, want: "max"},
	}
	applier := &Applier{}
	for _, test := range tests {
		body, err := applier.Apply([]byte(`{"model":"`+test.model+`"}`), thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: test.level}, &registry.ModelInfo{ID: test.model, UserDefined: true})
		if err != nil {
			t.Fatalf("Apply(%s,%s): %v", test.model, test.level, err)
		}
		if got := gjson.GetBytes(body, "reasoning_effort").String(); got != test.want {
			t.Fatalf("Apply(%s,%s) effort=%q want=%q", test.model, test.level, got, test.want)
		}
	}
}
