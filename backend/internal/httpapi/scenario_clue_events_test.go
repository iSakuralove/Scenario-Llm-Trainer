package httpapi

import (
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
	if clue.ClueID != "E_NEW" || clue.Content.ContentType != "clue" || clue.Content.DisplayVariant != "clue" {
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
