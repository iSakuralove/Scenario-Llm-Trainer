package ai

import (
	"strings"
	"testing"
)

func TestSetPromptOverrideRejectsUnstructuredScenarioGeneratePrompt(t *testing.T) {
	ClearPromptOverride("scenario_generate")
	t.Cleanup(func() {
		ClearPromptOverride("scenario_generate")
	})

	err := SetPromptOverride("scenario_generate", PromptRenderEngineGoTemplate, "请生成一道情景题。")
	if err == nil {
		t.Fatal("expected unstructured scenario_generate prompt to be rejected")
	}

	rendered, renderErr := renderPrompt("scenario_generate", map[string]interface{}{
		"Domain":       "database",
		"Difficulty":   "L2",
		"ScenarioType": "troubleshooting",
		"Nonce":        "unit-test",
		"TagsText":     "数据库",
	})
	if renderErr != nil {
		t.Fatal(renderErr)
	}
	for _, token := range []string{`"architecture_diagram_spec"`, `"reveal_strategy"`, `"root_cause_keywords"`} {
		if !strings.Contains(rendered, token) {
			t.Fatalf("expected built-in structured prompt to contain %s, got %s", token, rendered)
		}
	}
}
