package httpapi

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestScenarioReplyGuardUsesEnglishBoundariesAndBlocksActualEntity(t *testing.T) {
	world := &domain.HiddenWorld{
		RootCause: domain.RootCause{ID: "RC_AI", Component: "ai", Description: "private index idx_orders_v2"},
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_PRIVATE", Content: "内部路径 /srv/orders/private.log 与 orders表"},
		},
	}
	state := domain.ScenarioLearnerState{}.Normalized()

	for _, safe := range []string{
		"Please explain the available observations.",
		"先根据公开现象继续排查，不急着确认结论。",
	} {
		if err := validateScenarioReply(safe, world, nil, state); err != nil {
			t.Fatalf("safe reply was rejected: %q: %v", safe, err)
		}
	}
	for _, leaked := range []string{
		"检查 ai 的配置。",
		"直接看 idx_orders_v2。",
		"打开 /srv/orders/private.log。",
		"问题就在 orders表。",
	} {
		if err := validateScenarioReply(leaked, world, nil, state); err == nil {
			t.Fatalf("leaked entity was not rejected: %q", leaked)
		}
	}
}

func TestScenarioReplyGuardAllowsNewlyReleasedEvidence(t *testing.T) {
	world := &domain.HiddenWorld{
		RootCause: domain.RootCause{ID: "RC_PRIVATE", Component: "orders", Description: "private root marker"},
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_RELEASED", Content: "公开后可见 idx_orders_v2"},
		},
	}
	state := domain.ScenarioLearnerState{CollectedEvidence: []string{"E_RELEASED"}}.Normalized()
	if err := validateScenarioReply("本轮公开观察提到了 idx_orders_v2。", world, nil, state); err != nil {
		t.Fatalf("released evidence should be allowed: %v", err)
	}
}

func TestScenarioReplyGuardAllowsPublicScenarioFactsAndBlocksHiddenNumbers(t *testing.T) {
	world := &domain.HiddenWorld{
		RootCause: domain.RootCause{ID: "RC_PRIVATE", Component: "orders", Description: "private root marker"},
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_SLOW", Content: "慢查询平均耗时 3.8s，占比 92%"},
			{EvidenceID: "E_NETWORK", Content: "依赖 P99 为 8ms"},
		},
	}
	publicScenario := &domain.PublicScenario{
		Title:           "接口变慢",
		Description:     "接口 P99 从 120ms 涨到约 4s。",
		InitialSymptoms: []string{"错误率没有上升"},
	}
	state := domain.ScenarioLearnerState{}.Normalized()
	if err := validateScenarioReply("接口 P99 涨到 4s，错误率没有上升。", world, publicScenario, state); err != nil {
		t.Fatalf("public scenario facts should be allowed: %v", err)
	}
	if err := validateScenarioReply("慢查询平均耗时 3.8s。", world, publicScenario, state); err == nil {
		t.Fatal("unreleased hidden numeric fact was not rejected")
	}
}
