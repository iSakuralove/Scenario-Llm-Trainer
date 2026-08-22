package httpapi

import (
	"strings"
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestScenarioReleasedClueEventsOnlyPublishNewApprovedEvidence(t *testing.T) {
	world := &domain.HiddenWorld{
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_OLD", Content: "旧线索", Category: "logs"},
			{EvidenceID: "E_NEW", Content: "网关超时从 10s 变为 3s", Category: "config"},
		},
	}
	previous := domain.ScenarioLearnerState{CollectedEvidence: []string{"E_OLD"}}
	next := domain.ScenarioLearnerState{CollectedEvidence: []string{"E_OLD", "E_NEW"}}

	events := scenarioReleasedClueEvents("request-clue", 7, previous, next, world)
	if len(events) != 1 {
		t.Fatalf("expected one new clue event, got %+v", events)
	}
	event := events[0]
	if event.Kind != "clue_published" || event.SchemaVersion != domain.ScenarioRunEventSchemaV2 || event.StateRevision != 7 {
		t.Fatalf("unexpected clue event envelope: %+v", event)
	}
	if event.Payload == nil || event.Payload.Clue == nil {
		t.Fatalf("missing clue payload: %+v", event)
	}
	clue := event.Payload.Clue
	// clue_id 是不透明公钥：不回传内部 evidence_id，只承诺稳定派生。
	if !strings.HasPrefix(clue.ClueID, "clue_") || clue.Content.ContentType != "clue" || clue.Content.DisplayVariant != "clue" {
		t.Fatalf("unexpected clue projection: %+v", clue)
	}
	if clue.Content.MarkdownReady != "网关超时从 10s 变为 3s" || clue.Content.Meta == nil || clue.Content.Meta.ToolKind != "config" {
		t.Fatalf("unexpected clue content: %+v", clue.Content)
	}
}

func TestScenarioReleasedClueEventsIgnoreUnknownOrPreviouslyCollectedEvidence(t *testing.T) {
	world := &domain.HiddenWorld{
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_OLD", Content: "旧线索", Category: "logs"},
		},
	}
	previous := domain.ScenarioLearnerState{CollectedEvidence: []string{"E_OLD"}}
	next := domain.ScenarioLearnerState{CollectedEvidence: []string{"E_OLD", "E_UNKNOWN"}}

	if events := scenarioReleasedClueEvents("request-clue", 7, previous, next, world); len(events) != 0 {
		t.Fatalf("unexpected clue events for non-public evidence: %+v", events)
	}
}

func TestScenarioFillCurrentFocusUsesLatestApprovedEvidence(t *testing.T) {
	world := &domain.HiddenWorld{
		EvidenceGraph: []domain.EvidenceNode{
			{EvidenceID: "E_LOG", Content: "日志观察", Category: "logs"},
			{EvidenceID: "E_CONFIG", Content: "配置观察", Category: "config"},
		},
	}
	state := domain.ScenarioLearnerState{CollectedEvidence: []string{"E_LOG", "E_CONFIG"}}
	filled := scenarioFillCurrentFocus(state, world)
	if filled.CurrentFocus != "config" {
		t.Fatalf("expected latest approved evidence category as focus, got %+v", filled)
	}

	state.CurrentFocus = "metrics"
	kept := scenarioFillCurrentFocus(state, world)
	if kept.CurrentFocus != "metrics" {
		t.Fatalf("explicit focus should remain unchanged, got %+v", kept)
	}
}

func TestSafeScenarioHypothesisLabelNeverPublishesHiddenEntity(t *testing.T) {
	world := &domain.HiddenWorld{
		RootCause: domain.RootCause{Description: "内部根因 root-secret"},
		Hypotheses: []domain.Hypothesis{
			{HypothesisID: "H_PUBLIC", Label: "网关等待时间过短"},
			{HypothesisID: "H_PRIVATE", Label: "root-secret"},
		},
		EvidenceGraph: []domain.EvidenceNode{{EvidenceID: "E_PRIVATE", Content: "隐藏证据 hidden-marker", Category: "config"}},
	}
	publicScenario := &domain.PublicScenario{Title: "公开题目", Description: "公开现象"}

	publicSession := &domain.ScenarioSession{
		QuestionSnapshot: domain.ScenarioQuestion{Content: domain.ScenarioContent{PublicScenario: publicScenario, HiddenWorld: world}},
		LearnerState:     domain.ScenarioLearnerState{CurrentHypothesis: "H_PUBLIC"},
	}
	if label := safeScenarioHypothesisLabel(publicSession); label != "网关等待时间过短" {
		t.Fatalf("expected safe hypothesis label, got %q", label)
	}

	privateSession := &domain.ScenarioSession{
		QuestionSnapshot: domain.ScenarioQuestion{Content: domain.ScenarioContent{PublicScenario: publicScenario, HiddenWorld: world}},
		LearnerState:     domain.ScenarioLearnerState{CurrentHypothesis: "H_PRIVATE"},
	}
	if label := safeScenarioHypothesisLabel(privateSession); label != "" {
		t.Fatalf("hidden hypothesis label must stay private, got %q", label)
	}
}
