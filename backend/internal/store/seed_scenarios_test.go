package store

import (
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestSeedScenariosIncludeStableCompleteDiagnosticQuestions(t *testing.T) {
	items := seedDiagnosticScenarios(time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC))
	if len(items) < 4 {
		t.Fatalf("expected at least 4 fixed diagnostic seed scenarios, got %d", len(items))
	}

	expectedIDs := map[string]bool{
		"scenario-db-index":          false,
		"scenario-network-timeout":   false,
		"scenario-k8s-io-throttle":   false,
		"scenario-cache-key-release": false,
	}

	for _, item := range items {
		if _, ok := expectedIDs[item.ID]; ok {
			expectedIDs[item.ID] = true
		}
		if item.Status != "active" {
			t.Fatalf("seed scenario %s should be active, got %q", item.ID, item.Status)
		}
		if item.Source != "seed" {
			t.Fatalf("seed scenario %s should use source=seed, got %q", item.ID, item.Source)
		}
		if item.CreatedBy != "user-admin" {
			t.Fatalf("seed scenario %s should be owned by user-admin, got %q", item.ID, item.CreatedBy)
		}
		if item.Content.RootCause == "" || len(item.Content.KeyEvidence) < 3 || len(item.Content.StandardProcedure) < 5 {
			t.Fatalf("seed scenario %s has incomplete answer content: %+v", item.ID, item.Content)
		}
		if len(item.Content.RevealStrategy.SurfaceClues) < 2 {
			t.Fatalf("seed scenario %s should have at least 2 surface clues", item.ID)
		}
		if len(item.Content.RevealStrategy.DeepClues) < 2 {
			t.Fatalf("seed scenario %s should have at least 2 deep clues", item.ID)
		}
		if len(item.Content.RevealStrategy.Distractors) < 1 {
			t.Fatalf("seed scenario %s should have at least 1 distractor", item.ID)
		}
		knownIDs := clueIDs(item.Content.RevealStrategy.SurfaceClues)
		assertSeedClueIntegrity(t, item.ID, item.Content.RevealStrategy.SurfaceClues, nil, nil)
		assertSeedClueIntegrity(t, item.ID, item.Content.RevealStrategy.DeepClues, knownIDs, knownIDs)
		mergeClueIDs(knownIDs, item.Content.RevealStrategy.DeepClues)
		assertSeedClueIntegrity(t, item.ID, item.Content.RevealStrategy.Distractors, nil, nil)
	}

	for id, found := range expectedIDs {
		if !found {
			t.Fatalf("missing fixed seed scenario %s", id)
		}
	}
}

func assertSeedClueIntegrity(t *testing.T, scenarioID string, clues []domain.Clue, knownPrerequisites map[string]bool, updateKnown map[string]bool) {
	t.Helper()
	for _, clue := range clues {
		if clue.ClueID == "" {
			t.Fatalf("seed scenario %s has clue without id: %+v", scenarioID, clue)
		}
		if len(clue.TriggerKeywords) < 2 {
			t.Fatalf("seed scenario %s clue %s should have natural trigger keywords, got %+v", scenarioID, clue.ClueID, clue.TriggerKeywords)
		}
		if clue.Content == "" {
			t.Fatalf("seed scenario %s clue %s has empty content", scenarioID, clue.ClueID)
		}
		for _, prerequisite := range clue.PrerequisiteClues {
			if knownPrerequisites == nil || !knownPrerequisites[prerequisite] {
				t.Fatalf("seed scenario %s clue %s references missing prerequisite %s", scenarioID, clue.ClueID, prerequisite)
			}
		}
		if updateKnown != nil {
			updateKnown[clue.ClueID] = true
		}
	}
}

func clueIDs(clues []domain.Clue) map[string]bool {
	ids := map[string]bool{}
	for _, clue := range clues {
		ids[clue.ClueID] = true
	}
	return ids
}

func mergeClueIDs(dst map[string]bool, clues []domain.Clue) {
	for _, clue := range clues {
		dst[clue.ClueID] = true
	}
}
