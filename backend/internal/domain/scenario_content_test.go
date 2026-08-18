package domain

import (
	"encoding/json"
	"testing"
)

func TestHiddenWorldScenarioJSONOmitsLegacyCompatibilityFields(t *testing.T) {
	content := hiddenWorldContentForTest()
	content.RootCause = "仅内存兼容字段"
	content.KeyEvidence = []string{"仅内存证据"}
	content.RevealStrategy = RevealStrategy{SurfaceClues: []Clue{{ClueID: "legacy"}}}

	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"root_cause", "key_evidence", "reveal_strategy", "architecture_diagram"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("hiddenworld.v1 JSON retained legacy field %q: %s", forbidden, raw)
		}
	}
	if _, exists := payload["public_scenario"]; !exists {
		t.Fatalf("hiddenworld.v1 JSON lost public_scenario: %s", raw)
	}
	if _, exists := payload["hidden_world"]; !exists {
		t.Fatalf("hiddenworld.v1 JSON lost hidden_world: %s", raw)
	}
}

func TestHiddenWorldScenarioJSONRestoresLegacyCompatibilityInMemory(t *testing.T) {
	raw, err := json.Marshal(hiddenWorldContentForTest())
	if err != nil {
		t.Fatal(err)
	}
	var content ScenarioContent
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatal(err)
	}
	if content.RootCause == "" || len(content.KeyEvidence) == 0 || len(content.StandardProcedure) == 0 {
		t.Fatalf("expected in-memory compatibility projection, got %+v", content)
	}
	if len(content.RevealStrategy.SurfaceClues)+len(content.RevealStrategy.DeepClues) == 0 {
		t.Fatalf("expected evidence graph to project into legacy clues: %+v", content.RevealStrategy)
	}
}

func hiddenWorldContentForTest() ScenarioContent {
	return ScenarioContent{
		ModelVersion: "hiddenworld.v1",
		PublicScenario: &PublicScenario{
			Title:           "查询变慢",
			Description:     "延迟上升",
			InitialSymptoms: []string{"CPU 正常"},
		},
		HiddenWorld: &HiddenWorld{
			RootCause: RootCause{
				ID:                     "RC_INDEX",
				Category:               "data",
				Component:              "mysql.orders",
				Description:            "联合索引缺失",
				SufficientEvidenceSets: [][]string{{"E_EXPLAIN"}},
				AcceptedHypotheses:     []string{"H_INDEX"},
			},
			Hypotheses: []Hypothesis{
				{HypothesisID: "H_INDEX", Label: "索引问题"},
				{HypothesisID: "H_OTHER", Label: "其他原因"},
			},
			EvidenceGraph: []EvidenceNode{
				{EvidenceID: "E_LOG", Content: "慢查询日志 rows_examined 升高", Category: "logs", ObtainedBy: []string{"inspect:logs.slow_query"}},
				{EvidenceID: "E_EXPLAIN", Content: "执行计划 type=ALL", Category: "data", Prerequisites: []string{"E_LOG"}, ObtainedBy: []string{"inspect:data.explain"}},
			},
			Observations: []Observation{
				{Action: "inspect:logs.slow_query", Result: "rows_examined 升高", YieldsEvidence: []string{"E_LOG"}},
				{Action: "inspect:data.explain", Result: "type=ALL", YieldsEvidence: []string{"E_EXPLAIN"}},
			},
			SolutionRubric: SolutionRubric{
				RequiredActions:   []string{"补建联合索引"},
				VerificationSteps: []string{"重新检查执行计划"},
			},
		},
	}
}
