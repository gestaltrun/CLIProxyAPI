package config

import "testing"

func TestParseConfigRejectsInvalidGLMCodingPlan(t *testing.T) {
	for _, payload := range []string{
		"glm-coding-plan:\n  - site: cn\n",
		"glm-coding-plan:\n  - api-key: key\n    site: payg\n",
		"glm-coding-plan:\n  - api-key: key\n    site: cn\n    project: project-only\n",
	} {
		if _, err := ParseConfigBytes([]byte(payload)); err == nil {
			t.Fatalf("ParseConfigBytes accepted invalid GLM config: %s", payload)
		}
	}
}

func TestParseConfigGLMCodingPlan(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte(`
glm-coding-plan:
  - api-key: coding-secret
    site: international
    organization: team-a
    project: project-b
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.GLMCodingPlan) != 1 {
		t.Fatalf("GLMCodingPlan = %#v", cfg.GLMCodingPlan)
	}
	entry := cfg.GLMCodingPlan[0]
	if entry.APIKey != "coding-secret" || entry.Site != "international" || entry.Organization != "team-a" || entry.Project != "project-b" {
		t.Fatalf("entry = %#v", entry)
	}
}
