package ai

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestBuildScenarioVectorDocumentsSplitsOfficialScenarioContent(t *testing.T) {
	question := domain.ScenarioQuestion{
		ID:           "scenario-1",
		Title:        "订单列表查询突然变慢",
		Description:  "筛选条件扩展后接口耗时升高",
		Domain:       "database",
		Difficulty:   "L3",
		ScenarioType: "performance",
		Tags:         []string{"MySQL", "索引"},
		Status:       "active",
		Version:      2,
		Content: domain.ScenarioContent{
			RootCause:         "缺少联合索引导致全表扫描。",
			RootCauseKeywords: []string{"联合索引", "全表扫描"},
			KeyEvidence:       []string{"慢查询 rows_examined 升高", "EXPLAIN type=ALL"},
			StandardProcedure: []string{"查看慢查询日志", "检查执行计划"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{{ClueID: "s1", TriggerKeywords: []string{"慢查询"}, Content: "rows_examined 持续升高", RecommendedNextAsk: "继续看执行计划"}},
				DeepClues:    []domain.Clue{{ClueID: "d1", TriggerKeywords: []string{"EXPLAIN"}, PrerequisiteClues: []string{"s1"}, Content: "type=ALL", RecommendedNextAsk: "收束到索引"}},
				Distractors:  []domain.Clue{{ClueID: "x1", TriggerKeywords: []string{"网络"}, Content: "网络正常", IsDistractor: true}},
			},
		},
	}

	docs := BuildScenarioVectorDocuments(question)

	if len(docs) != 9 {
		t.Fatalf("expected 9 vector docs, got %d: %#v", len(docs), docs)
	}
	counts := map[string]int{}
	for _, doc := range docs {
		counts[doc.DocType]++
		if doc.QuestionID != question.ID || doc.SourceVersion != question.Version || doc.Status != "active" {
			t.Fatalf("unexpected doc identity: %#v", doc)
		}
		if doc.TextHash == "" || doc.DocText == "" {
			t.Fatalf("expected stable hash and text: %#v", doc)
		}
	}
	expected := map[string]int{
		"problem_context": 1,
		"root_cause":      1,
		"evidence":        2,
		"procedure_step":  2,
		"clue":            2,
		"distractor":      1,
	}
	for docType, want := range expected {
		if counts[docType] != want {
			t.Fatalf("expected %s count %d, got %d", docType, want, counts[docType])
		}
	}
}

func TestBuildScenarioVectorDocumentsSkipsNonActiveScenario(t *testing.T) {
	question := domain.ScenarioQuestion{
		ID:          "draft-1",
		Title:       "待审题",
		Description: "还未发布",
		Status:      "pending_review",
		Content:     domain.ScenarioContent{RootCause: "不能入评分索引"},
	}

	if docs := BuildScenarioVectorDocuments(question); len(docs) != 0 {
		t.Fatalf("expected no docs for non-active scenario, got %#v", docs)
	}
}
