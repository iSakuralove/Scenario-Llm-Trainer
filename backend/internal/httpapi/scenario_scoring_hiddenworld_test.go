package httpapi

import (
	"encoding/json"
	"testing"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestHiddenWorldAggregateScoreUsesPersistedVerificationAndState(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	session, err := dataStore.CreateScenarioSession("user-demo", question.ID)
	if err != nil {
		t.Fatal(err)
	}
	session.CurrentTurn = 2
	session.LearnerState.EffectiveTurns = 2
	session.LearnerState.EstablishedFacts = []string{"fact-from-observation", "fact-derived"}
	session.LearnerState.RuledOutHypotheses = []string{"H_POOL"}
	answer := "根因是索引变更"

	verification, err := json.Marshal(agentclient.VerificationResult{
		Relation:          "target",
		Coverage:          1,
		CompletionAllowed: true,
		AnswerComparison: &agentclient.InternalAnswerComparison{
			AnswerAttemptID:   "request-2:answer",
			Relation:          "target",
			ClaimAlignment:    1,
			EvidenceCoverage:  1,
			SolutionCoverage:  1,
			CompletionAllowed: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	records := []domain.ScenarioAgentTurnRecord{
		{
			Message: domain.ScenarioMessage{UserContent: "先看公开观察"},
			SessionSnapshot: domain.ScenarioSession{LearnerState: domain.ScenarioLearnerState{
				CollectedEvidence: []string{"E_SLOW_QUERY"},
				EstablishedFacts:  []string{"fact-from-observation"},
			}},
			InternalVerification: verification,
		},
		{
			Message: domain.ScenarioMessage{UserContent: answer},
			SessionSnapshot: domain.ScenarioSession{LearnerState: domain.ScenarioLearnerState{
				CollectedEvidence:  []string{"E_SLOW_QUERY"},
				EstablishedFacts:   []string{"fact-from-observation", "fact-derived"},
				RuledOutHypotheses: []string{"H_POOL"},
			}},
			InternalVerification: verification,
		},
	}

	score := scoreScenarioWithHiddenWorld(&question, session, answer, records)
	if score.Status != "available" || score.Total != 71 || score.Efficiency != 100 || score.Accuracy != 100 {
		t.Fatalf("unexpected aggregate score: %+v", score)
	}
	if score.ClueIndependence != 50 || score.AutonomousFactCount != 1 {
		t.Fatalf("expected autonomous fact ratio, got %+v", score)
	}
	if score.EliminationAbility <= 0 || score.ReasoningDepth <= 0 {
		t.Fatalf("expected elimination and reasoning dimensions, got %+v", score)
	}
}

func TestHiddenWorldAggregateScoreRejectsUnmatchedFinalAnswer(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	session, err := dataStore.CreateScenarioSession("user-demo", question.ID)
	if err != nil {
		t.Fatal(err)
	}
	score := scoreScenarioWithHiddenWorld(
		&question,
		session,
		"没有执行过答案对比的最终答案",
		[]domain.ScenarioAgentTurnRecord{{Message: domain.ScenarioMessage{UserContent: "其他轮次"}}},
	)
	if score.Status != "insufficient_data" {
		t.Fatalf("expected explicit insufficient data, got %+v", score)
	}
}

func TestHiddenWorldAggregateScoreCapsUnsupportedTargetClaim(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	session, err := dataStore.CreateScenarioSession("user-demo", question.ID)
	if err != nil {
		t.Fatal(err)
	}
	session.CurrentTurn = 1
	session.LearnerState.EffectiveTurns = 1
	answer := "我猜是索引问题"
	verification, err := json.Marshal(agentclient.VerificationResult{
		Relation: "target",
		Coverage: 0.9,
		AnswerComparison: &agentclient.InternalAnswerComparison{
			AnswerAttemptID:   "request-guess:answer",
			Relation:          "target",
			ClaimAlignment:    1,
			EvidenceCoverage:  0.9,
			SolutionCoverage:  1,
			CompletionAllowed: false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreScenarioWithHiddenWorld(
		&question,
		session,
		answer,
		[]domain.ScenarioAgentTurnRecord{{
			Message:              domain.ScenarioMessage{UserContent: answer},
			InternalVerification: verification,
		}},
	)
	if score.Status != "available" || score.Accuracy != 69 {
		t.Fatalf("unsupported target claim must stay below the completion threshold: %+v", score)
	}
	metadata := score.auditMetadata(&domain.ScenarioScore{Total: 60})
	for _, forbidden := range []string{"answer", "relation", "root_cause", "claim_alignment"} {
		if _, ok := metadata[forbidden]; ok {
			t.Fatalf("private field %q leaked into scoring audit: %+v", forbidden, metadata)
		}
	}
}
