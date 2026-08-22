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
	ids := []string{"hw-db-index-001", "hw-network-vip-001", "hw-k8s-io-001", "hw-cache-key-001"}
	for _, id := range ids {
		sourcePath := filepath.Join(
			filepath.Dir(testFile),
			"..", "..", "..",
			"agent", "src", "hiddenworld", "bank", "fixed", id+".json",
		)
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read Agent fixed question source %s: %v", id, err)
		}
		generated, err := fixedHiddenWorldAssets.ReadFile("fixed_hiddenworld/" + id + ".json")
		if err != nil {
			t.Fatalf("read generated Go fixed question asset %s: %v", id, err)
		}
		want := append(bytes.TrimSpace(source), '\n')
		if !bytes.Equal(generated, want) {
			t.Fatalf("generated fixed question %s drifted from Agent source; run go generate ./internal/store", id)
		}
	}
}

func TestGeneratedScenarioV3AssetsMatchAgentSource(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve seed_scenarios_test.go path")
	}
	ids := []string{"hw-db-index-001", "hw-network-vip-001", "hw-k8s-io-001", "hw-cache-key-001"}
	for _, id := range ids {
		sourcePath := filepath.Join(
			filepath.Dir(testFile),
			"..", "..", "..",
			"agent", "src", "hiddenworld", "bank", "fixed_v3", id+".json",
		)
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read Agent scenario.v3 source %s: %v", id, err)
		}
		generated, err := fixedScenarioV3Assets.ReadFile("fixed_scenario_v3/" + id + ".json")
		if err != nil {
			t.Fatalf("read generated scenario.v3 asset %s: %v", id, err)
		}
		want := append(bytes.TrimSpace(source), '\n')
		if !bytes.Equal(generated, want) {
			t.Fatalf("generated scenario.v3 asset %s drifted from Agent source; run go generate ./internal/store", id)
		}
	}
}

func TestSeedScenariosLoadTheFixedHiddenWorldBank(t *testing.T) {
	items := seedDiagnosticScenarios(time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC))
	if len(items) != 4 {
		t.Fatalf("expected four fixed HiddenWorld questions, got %d", len(items))
	}
	wantIDs := map[string]bool{
		"hw-db-index-001": true, "hw-network-vip-001": true,
		"hw-k8s-io-001": true, "hw-cache-key-001": true,
	}
	for _, item := range items {
		if !wantIDs[item.ID] {
			t.Fatalf("unexpected fixed question id %q", item.ID)
		}
		if item.Status != "active" || item.Source != "fixed_hiddenworld" || item.Version != 1 {
			t.Fatalf("unexpected fixed question metadata: %+v", item)
		}
		if item.ScenarioType != "troubleshooting" && item.ScenarioType != "performance" {
			t.Fatalf("fixed HiddenWorld question must use a supported troubleshooting type, got %q for %s", item.ScenarioType, item.ID)
		}
		if item.CreatedBy != "user-admin" || item.Content.ModelVersion != "hiddenworld.v1" || item.Content.ContractVersion != domain.ScenarioV3ContractVersion {
			t.Fatalf("unexpected fixed question ownership/version: %+v", item)
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
		if len(item.Content.HiddenWorld.RootCause.SufficientEvidenceSets) < 2 || !containsHypothesis(item.Content.HiddenWorld.Hypotheses, "H_OTHER") || !hasNegativeObservation(item.Content.HiddenWorld.Observations) {
			t.Fatalf("fixed question misses required HiddenWorld scale: %+v", item.Content.HiddenWorld)
		}
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
