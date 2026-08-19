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

func TestScenarioReplyGuardAllowsRootComponentWhenPubliclyNamed(t *testing.T) {
	world := &domain.HiddenWorld{
		RootCause: domain.RootCause{ID: "RC_PRIVATE", Component: "orders", Description: "private root marker"},
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_PRIVATE", Content: "隐藏配置 idx_orders_v2"},
		},
	}
	publicScenario := &domain.PublicScenario{
		Title:       "订单列表变慢",
		Description: "orders 表的请求响应变慢。",
		Environment: "MySQL orders",
	}
	state := domain.ScenarioLearnerState{}.Normalized()
	if err := validateScenarioReply("可以先围绕 orders 表的公开现象继续观察。", world, publicScenario, state); err != nil {
		t.Fatalf("publicly named root component should be allowed: %v", err)
	}
}

func TestScenarioReplyGuardIgnoresBareSmallIntegersButKeepsPreciseValues(t *testing.T) {
	// 固定题库实测：4 道题里有 3 道把 8 / 10 / 12 / 35 / 45 / 90 列成了禁词，
	// 导师连「10 分钟」都写不出来。裸的一两位整数不携带识别信息，
	// 但三位以上整数和带小数点/千分位的取值仍然指向隐藏内容。
	world := &domain.HiddenWorld{
		RootCause: domain.RootCause{ID: "RC_PRIVATE", Component: "orders", Description: "重建耗时 45 秒"},
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_PRIVATE", Content: "扫描行数 2,400,000，命中率 92%，平均 3.8s"},
		},
	}
	publicScenario := &domain.PublicScenario{Title: "订单列表变慢", Description: "响应变慢。"}
	state := domain.ScenarioLearnerState{}.Normalized()

	for _, reply := range []string{
		"我们先看 3 个方向，每个大概花 10 分钟。",
		"这一步大概需要 45 秒左右，先别急。",
	} {
		if err := validateScenarioReply(reply, world, publicScenario, state); err != nil {
			t.Fatalf("ordinary small number was rejected (%q): %v", reply, err)
		}
	}

	for _, reply := range []string{
		"扫描行数大概是 2,400,000 行。",
		"慢查询平均耗时 3.8 秒。",
	} {
		if err := validateScenarioReply(reply, world, publicScenario, state); err == nil {
			t.Fatalf("precise hidden value leaked through the guard: %q", reply)
		}
	}
}
