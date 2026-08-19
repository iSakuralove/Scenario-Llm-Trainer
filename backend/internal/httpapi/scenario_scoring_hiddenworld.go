package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/domain"
)

// hiddenWorldAggregateScore 是新评分的内部对照结果。
// 旧评分仍负责用户当前复盘口径；此结构只用于并行分布审计。
type hiddenWorldAggregateScore struct {
	Status                  string
	Total                   int
	Efficiency              int
	Accuracy                int
	ClueIndependence        int
	ReasoningDepth          int
	EliminationAbility      int
	SourceTurns             int
	AutonomousFactCount     int
	RuledOutHypothesisCount int
}

func (score hiddenWorldAggregateScore) auditMetadata(legacy *domain.ScenarioScore) map[string]string {
	metadata := map[string]string{
		"status":                     score.Status,
		"hiddenworld_total":          hiddenWorldIntString(score.Total),
		"hiddenworld_efficiency":     hiddenWorldIntString(score.Efficiency),
		"hiddenworld_accuracy":       hiddenWorldIntString(score.Accuracy),
		"clue_independence":          hiddenWorldIntString(score.ClueIndependence),
		"reasoning_depth":            hiddenWorldIntString(score.ReasoningDepth),
		"elimination_ability":        hiddenWorldIntString(score.EliminationAbility),
		"source_turns":               hiddenWorldIntString(score.SourceTurns),
		"autonomous_fact_count":      hiddenWorldIntString(score.AutonomousFactCount),
		"ruled_out_hypothesis_count": hiddenWorldIntString(score.RuledOutHypothesisCount),
	}
	if legacy != nil {
		metadata["legacy_total"] = hiddenWorldIntString(legacy.Total)
		if score.Status == "available" {
			metadata["delta"] = hiddenWorldIntString(score.Total - legacy.Total)
		}
	}
	return metadata
}

func scoreScenarioWithHiddenWorld(
	question *domain.ScenarioQuestion,
	session *domain.ScenarioSession,
	answer string,
	records []domain.ScenarioAgentTurnRecord,
) hiddenWorldAggregateScore {
	if question == nil || question.Content.HiddenWorld == nil || session == nil {
		return hiddenWorldAggregateScore{Status: "insufficient_data"}
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return hiddenWorldAggregateScore{Status: "insufficient_data"}
	}

	var comparison *agentclient.InternalAnswerComparison
	for _, record := range records {
		if strings.TrimSpace(record.Message.UserContent) != answer {
			continue
		}
		var verification agentclient.VerificationResult
		if err := json.Unmarshal(record.InternalVerification, &verification); err != nil || verification.AnswerComparison == nil {
			continue
		}
		candidate := *verification.AnswerComparison
		comparison = &candidate
	}
	if comparison == nil {
		return hiddenWorldAggregateScore{Status: "insufficient_data", SourceTurns: len(records)}
	}

	state := session.LearnerState.Normalized()
	efficiency := 0
	if session.CurrentTurn > 0 {
		efficiency = clampInt(int(math.Round(float64(state.EffectiveTurns) / float64(session.CurrentTurn) * 100)))
	}
	accuracy := clampInt(int(math.Round(
		comparison.ClaimAlignment*50 + comparison.EvidenceCoverage*35 + comparison.SolutionCoverage*15,
	)))
	if comparison.Relation == "target" && !comparison.CompletionAllowed && accuracy > 69 {
		accuracy = 69
	}

	autonomousFacts := countAutonomousFacts(records)
	clueIndependence := 0
	if len(state.EstablishedFacts) > 0 {
		clueIndependence = clampInt(autonomousFacts * 100 / len(state.EstablishedFacts))
	}
	nonTargetHypotheses := 0
	for _, hypothesis := range question.Content.HiddenWorld.Hypotheses {
		if hypothesis.HypothesisID != "H_OTHER" && !hiddenWorldContainsString(question.Content.HiddenWorld.RootCause.AcceptedHypotheses, hypothesis.HypothesisID) {
			nonTargetHypotheses++
		}
	}
	denominator := max(1, nonTargetHypotheses+len(question.Content.HiddenWorld.EvidenceGraph))
	reasoningDepth := clampInt((len(state.RuledOutHypotheses) + autonomousFacts) * 100 / denominator)
	eliminationAbility := 0
	if nonTargetHypotheses > 0 {
		eliminationAbility = clampInt(len(state.RuledOutHypotheses) * 100 / nonTargetHypotheses)
	}
	total := clampInt((efficiency*15 + accuracy*45 + clueIndependence*15 + reasoningDepth*25) / 100)
	return hiddenWorldAggregateScore{
		Status:                  "available",
		Total:                   total,
		Efficiency:              efficiency,
		Accuracy:                accuracy,
		ClueIndependence:        clueIndependence,
		ReasoningDepth:          reasoningDepth,
		EliminationAbility:      eliminationAbility,
		SourceTurns:             len(records),
		AutonomousFactCount:     autonomousFacts,
		RuledOutHypothesisCount: len(state.RuledOutHypotheses),
	}
}

func countAutonomousFacts(records []domain.ScenarioAgentTurnRecord) int {
	previousEvidence := map[string]struct{}{}
	previousFacts := map[string]struct{}{}
	autonomous := 0
	for _, record := range records {
		state := record.SessionSnapshot.LearnerState.Normalized()
		newEvidence := hiddenWorldDifference(state.CollectedEvidence, previousEvidence)
		newFacts := hiddenWorldDifference(state.EstablishedFacts, previousFacts)
		if len(newEvidence) == 0 {
			autonomous += len(newFacts)
		}
		previousEvidence = hiddenWorldStringSet(state.CollectedEvidence)
		previousFacts = hiddenWorldStringSet(state.EstablishedFacts)
	}
	return autonomous
}

func hiddenWorldDifference(values []string, previous map[string]struct{}) []string {
	result := make([]string, 0)
	for _, value := range values {
		if _, ok := previous[value]; !ok {
			result = append(result, value)
		}
	}
	return result
}

func hiddenWorldStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func hiddenWorldContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hiddenWorldIntString(value int) string {
	return fmt.Sprintf("%d", value)
}
