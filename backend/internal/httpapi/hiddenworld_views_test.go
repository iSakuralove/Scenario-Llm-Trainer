package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestStudentScenarioDetailOnlyReturnsPublicHiddenWorldProjection(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	question := dataStore.AddScenario(domain.ScenarioQuestion{
		ID:           "hw-public-projection-test",
		Title:        "订单列表查询变慢",
		Description:  "请逐步定位查询退化原因。",
		Domain:       "database",
		Difficulty:   "L3",
		ScenarioType: "performance",
		Tags:         []string{"MySQL", "索引"},
		Status:       "active",
		Source:       "fixed_hiddenworld",
		CreatedBy:    "user-admin",
		Version:      1,
		Content: domain.ScenarioContent{
			ModelVersion: "hiddenworld.v1",
			PublicScenario: &domain.PublicScenario{
				Title:               "订单列表查询变慢",
				Description:         "接口耗时从 200ms 上升到 4s。",
				Environment:         "Go 服务连接 MySQL。",
				InitialSymptoms:     []string{"应用 CPU 正常", "数据库连接数稳定"},
				ArchitectureDiagram: "graph TD\nA[API] --> B[(MySQL)]",
			},
			HiddenWorld: &domain.HiddenWorld{
				RootCause: domain.RootCause{
					ID:                     "RC_INDEX_DROPPED",
					Category:               "data",
					Component:              "mysql.orders",
					Description:            "绝不能出现在学生响应中的联合索引缺失",
					SufficientEvidenceSets: [][]string{{"E_EXPLAIN"}},
					AcceptedHypotheses:     []string{"H_INDEX"},
				},
				Hypotheses: []domain.Hypothesis{
					{HypothesisID: "H_INDEX", Label: "索引问题"},
					{HypothesisID: "H_OTHER", Label: "其他原因"},
				},
				EvidenceGraph: []domain.EvidenceNode{
					{EvidenceID: "E_EXPLAIN", Content: "type=ALL、key=NULL", Category: "data", ObtainedBy: []string{"inspect:data.explain"}},
				},
				Observations: []domain.Observation{
					{Action: "inspect:data.explain", Result: "执行计划显示全表扫描", YieldsEvidence: []string{"E_EXPLAIN"}},
				},
				SolutionRubric: domain.SolutionRubric{RequiredActions: []string{"补建联合索引"}},
			},
		},
	})

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+question.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("scenario detail status=%d message=%s", status, env.Message)
	}

	raw := string(env.Data)
	if !strings.Contains(raw, `"model_version":"hiddenworld.v1"`) || !strings.Contains(raw, `"public_scenario"`) {
		t.Fatalf("student response should contain hiddenworld.v1 public scenario: %s", raw)
	}
	for _, forbidden := range []string{"hidden_world", "RC_INDEX_DROPPED", "联合索引缺失", "E_EXPLAIN", "H_INDEX"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("student response leaked %q: %s", forbidden, raw)
		}
	}

	var view domain.ScenarioQuestionView
	mustDecodeData(t, env, &view)
	if view.Content.PublicScenario == nil {
		t.Fatalf("student response lost public_scenario: %+v", view.Content)
	}
	if view.Content.HiddenWorld != nil {
		t.Fatalf("student response exposed hidden_world: %+v", view.Content.HiddenWorld)
	}
	stored, ok := dataStore.GetScenario(question.ID)
	if !ok {
		t.Fatal("stored scenario disappeared after rendering public view")
	}
	if len(stored.Content.KeyEvidence) != 1 || stored.Content.KeyEvidence[0] != "type=ALL、key=NULL" {
		t.Fatalf("rendering public view mutated private compatibility evidence: %+v", stored.Content.KeyEvidence)
	}
}

func TestHiddenWorldCreatorCannotReadPrivateWorldThroughDetailEndpoint(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.AddScenario(domain.ScenarioQuestion{
		ID: "hw-creator-private-test", Title: "生成题", Description: "公开题面", Domain: "database", Difficulty: "L2",
		ScenarioType: "troubleshooting", Tags: []string{"AI生成"}, Status: "active", Source: "llm_generated", CreatedBy: "user-demo", Version: 1,
		Content: domain.ScenarioContent{
			ModelVersion:   domain.HiddenWorldContractVersion,
			PublicScenario: &domain.PublicScenario{Title: "生成题", Description: "公开题面", InitialSymptoms: []string{}, ArchitectureDiagram: ""},
			HiddenWorld:    &domain.HiddenWorld{RootCause: domain.RootCause{ID: "RC_PRIVATE", Category: "behavior", Component: "service", Description: "私有根因"}},
		},
	})
	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+question.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("scenario detail status=%d message=%s", status, env.Message)
	}
	raw := string(env.Data)
	if strings.Contains(raw, "hidden_world") || strings.Contains(raw, "RC_PRIVATE") || strings.Contains(raw, "私有根因") {
		t.Fatalf("creator should not receive hidden world: %s", raw)
	}
}
