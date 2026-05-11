package ai

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestRootCauseMatchUsesKeywords(t *testing.T) {
	score := RootCauseMatch("我判断是联合索引缺失导致慢查询全表扫描", "订单表缺少联合索引，导致全表扫描", []string{"联合索引", "慢查询", "全表扫描"})
	if score < 85 {
		t.Fatalf("expected high score, got %d", score)
	}
}

func TestDeepClueRequiresPrerequisite(t *testing.T) {
	strategy := domain.RevealStrategy{
		DeepClues: []domain.Clue{
			{ClueID: "c2", TriggerKeywords: []string{"执行计划"}, PrerequisiteClues: []string{"c1"}, Content: "type=ALL"},
		},
	}
	if _, ok := FindTriggeredClue(strategy, "看执行计划", nil); ok {
		t.Fatal("deep clue should not be released without prerequisite")
	}
	if clue, ok := FindTriggeredClue(strategy, "看执行计划", []string{"c1"}); !ok || clue.ClueID != "c2" {
		t.Fatal("deep clue should be released after prerequisite")
	}
}
