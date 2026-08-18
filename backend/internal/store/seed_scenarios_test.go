package store

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestGeneratedFixedHiddenWorldAssetMatchesAgentSource(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve seed_scenarios_test.go path")
	}
	sourcePath := filepath.Join(
		filepath.Dir(testFile),
		"..", "..", "..",
		"agent", "src", "hiddenworld", "bank", "fixed", "hw-db-index-001.json",
	)
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read Agent fixed question source: %v", err)
	}
	generated, err := fixedHiddenWorldAssets.ReadFile("fixed_hiddenworld/hw-db-index-001.json")
	if err != nil {
		t.Fatalf("read generated Go fixed question asset: %v", err)
	}
	want := append(bytes.TrimSpace(source), '\n')
	if !bytes.Equal(generated, want) {
		t.Fatal("generated fixed question drifted from Agent source; run go generate ./internal/store")
	}
}

func TestSeedScenariosLoadTheFixedHiddenWorldBank(t *testing.T) {
	items := seedDiagnosticScenarios(time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC))
	if len(items) != 1 {
		t.Fatalf("expected the one landed fixed HiddenWorld question, got %d", len(items))
	}

	item := items[0]
	if item.ID != "hw-db-index-001" {
		t.Fatalf("unexpected fixed question id %q", item.ID)
	}
	if item.Status != "active" || item.Source != "fixed_hiddenworld" || item.Version != 1 {
		t.Fatalf("unexpected fixed question metadata: %+v", item)
	}
	if item.CreatedBy != "user-admin" {
		t.Fatalf("fixed question should be owned by user-admin, got %q", item.CreatedBy)
	}
	if item.Content.ModelVersion != "hiddenworld.v1" {
		t.Fatalf("unexpected model version %q", item.Content.ModelVersion)
	}
	if item.Content.PublicScenario == nil || item.Content.HiddenWorld == nil {
		t.Fatalf("fixed question must contain public_scenario + hidden_world: %+v", item.Content)
	}
	if item.Title != item.Content.PublicScenario.Title || item.Description != item.Content.PublicScenario.Description {
		t.Fatalf("outer directory title/description must mirror public_scenario: %+v", item)
	}
	if item.Content.RootCause != "" || len(item.Content.KeyEvidence) != 0 || len(item.Content.StandardProcedure) != 0 {
		t.Fatalf("hiddenworld.v1 seed retained legacy answer fields: %+v", item.Content)
	}
	if len(item.Content.HiddenWorld.Hypotheses) < 4 || len(item.Content.HiddenWorld.EvidenceGraph) < 6 {
		t.Fatalf("fixed question is below the agreed minimum scale: %+v", item.Content.HiddenWorld)
	}
	if len(item.Content.HiddenWorld.RootCause.SufficientEvidenceSets) < 2 {
		t.Fatalf("fixed question must keep both sufficient evidence paths: %+v", item.Content.HiddenWorld.RootCause)
	}
	if !containsHypothesis(item.Content.HiddenWorld.Hypotheses, "H_OTHER") {
		t.Fatal("fixed question must include H_OTHER")
	}
	if !hasNegativeObservation(item.Content.HiddenWorld.Observations) {
		t.Fatal("fixed question must include a negative observation")
	}
}

func containsHypothesis(items []domain.Hypothesis, id string) bool {
	for _, item := range items {
		if item.HypothesisID == id {
			return true
		}
	}
	return false
}

func hasNegativeObservation(items []domain.Observation) bool {
	for _, item := range items {
		if item.IsNegative {
			return true
		}
	}
	return false
}
