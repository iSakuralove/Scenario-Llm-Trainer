package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// ScenarioLearnerState 是 Go 持有的排查工坊权威学习状态。
// Python 只能提出变更建议，最终状态必须由 Go 审批后写入。
type ScenarioLearnerState struct {
	CollectedEvidence  []string `json:"collected_evidence"`
	RuledOutHypotheses []string `json:"ruled_out_hypotheses"`
	CurrentHypothesis  string   `json:"current_hypothesis,omitempty"`
	EstablishedFacts   []string `json:"established_facts"`
	ActionsTaken       []string `json:"actions_taken"`
	CurrentFocus       string   `json:"current_focus"`
	EffectiveTurns     int      `json:"effective_turns"`
	StalledTurns       int      `json:"stalled_turns"`
	RecentOpenings     []string `json:"recent_openings"`
}

type ScenarioPublicReasoningSummary struct {
	Stage string `json:"stage"`
	Text  string `json:"text"`
}

type ScenarioPublicObservation struct {
	Action     string `json:"action"`
	Result     string `json:"result"`
	IsNegative bool   `json:"is_negative"`
}

type ScenarioPublicAnswerComparison struct {
	Tool          string   `json:"tool"`
	Status        string   `json:"status"`
	UserPoints    []string `json:"user_points"`
	SupportStatus string   `json:"support_status"`
	NextAction    string   `json:"next_action"`
}

type ScenarioToolEventPayload struct {
	Name              string                          `json:"name"`
	RedactedArguments map[string]string               `json:"redacted_arguments"`
	DurationMS        int                             `json:"duration_ms"`
	Result            *ScenarioPublicAnswerComparison `json:"result,omitempty"`
}

type ScenarioRunEvent struct {
	RequestID   string                          `json:"request_id"`
	Sequence    int                             `json:"sequence"`
	Kind        string                          `json:"kind"`
	Status      string                          `json:"status"`
	Text        string                          `json:"text,omitempty"`
	Summary     string                          `json:"summary,omitempty"`
	Reasoning   *ScenarioPublicReasoningSummary `json:"reasoning,omitempty"`
	Observation *ScenarioPublicObservation      `json:"observation,omitempty"`
	Tool        *ScenarioToolEventPayload       `json:"tool,omitempty"`
	ErrorCode   string                          `json:"error_code,omitempty"`
}

func (state ScenarioLearnerState) Normalized() ScenarioLearnerState {
	if state.CollectedEvidence == nil {
		state.CollectedEvidence = []string{}
	}
	if state.RuledOutHypotheses == nil {
		state.RuledOutHypotheses = []string{}
	}
	if state.EstablishedFacts == nil {
		state.EstablishedFacts = []string{}
	}
	if state.ActionsTaken == nil {
		state.ActionsTaken = []string{}
	}
	if state.RecentOpenings == nil {
		state.RecentOpenings = []string{}
	}
	return state
}

type ScenarioAgentTurnRecord struct {
	SessionID            string          `json:"session_id"`
	RequestID            string          `json:"request_id"`
	RequestFingerprint   string          `json:"request_fingerprint"`
	ExpectedRevision     int             `json:"expected_revision"`
	CommittedRevision    int             `json:"committed_revision"`
	Message              ScenarioMessage `json:"message"`
	SessionSnapshot      ScenarioSession `json:"session_snapshot"`
	PublicTrace          json.RawMessage `json:"public_trace"`
	InternalVerification json.RawMessage `json:"internal_verification"`
	InternalAudit        json.RawMessage `json:"internal_audit"`
	ApprovalAudit        json.RawMessage `json:"approval_audit"`
	CreatedAt            time.Time       `json:"created_at"`
}

type ScenarioAgentTurnCommit struct {
	SessionID            string
	RequestID            string
	RequestFingerprint   string
	ExpectedRevision     int
	Message              ScenarioMessage
	NextSession          ScenarioSession
	PublicTrace          json.RawMessage
	InternalVerification json.RawMessage
	InternalAudit        json.RawMessage
	ApprovalAudit        json.RawMessage
}

type ScenarioAgentTurnCommitResult struct {
	Record   ScenarioAgentTurnRecord
	Replayed bool
}

type ScenarioRevisionConflictError struct {
	Expected int
	Current  int
}

func (e ScenarioRevisionConflictError) Error() string {
	return fmt.Sprintf("scenario state revision conflict: expected %d, current %d", e.Expected, e.Current)
}

type ScenarioRequestConflictError struct {
	RequestID string
}

func (e ScenarioRequestConflictError) Error() string {
	return fmt.Sprintf("scenario request_id %q was already used for a different request", e.RequestID)
}
