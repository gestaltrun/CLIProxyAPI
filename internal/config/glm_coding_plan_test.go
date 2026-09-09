package config

import "testing"

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
