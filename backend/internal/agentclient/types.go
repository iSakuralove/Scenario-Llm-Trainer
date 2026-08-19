package agentclient

import "situational-teaching/backend/internal/domain"

const ContractVersion = domain.HiddenWorldContractVersion

type TurnRequest struct {
	ContractVersion string                `json:"contract_version"`
	RequestID       string                `json:"request_id"`
	SessionID       string                `json:"session_id"`
	StateRevision   int                   `json:"state_revision"`
	PublicScenario  domain.PublicScenario `json:"public_scenario"`
	HiddenWorld     domain.HiddenWorld    `json:"hidden_world"`
	LearnerState    LearnerState          `json:"learner_state"`
	Transcript      []Turn                `json:"transcript"`
	UserMessage     string                `json:"user_message"`
	Budget          Budget                `json:"budget"`
}

type LearnerState struct {
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

type Turn struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	TurnNumber int    `json:"turn_number"`
}

type Budget struct {
	DeadlineMS  int `json:"deadline_ms"`
	MaxReleases int `json:"max_releases"`
}

type TurnResult struct {
	ContractVersion      string             `json:"contract_version"`
	RequestID            string             `json:"request_id"`
	ExpectedRevision     int                `json:"expected_revision"`
	Reply                string             `json:"reply"`
	TurnAnalysis         TurnAnalysis       `json:"turn_analysis"`
	Proposals            []Proposal         `json:"proposals"`
	PublicTrace          []PublicTraceEvent `json:"public_trace"`
	InternalVerification VerificationResult `json:"internal_verification"`
	InternalAudit        AuditTrace         `json:"internal_audit"`
}

type TurnAnalysis struct {
	Actions               []string `json:"actions"`
	HypothesisID          string   `json:"hypothesis_id"`
	HypothesisRaw         string   `json:"hypothesis_raw"`
	MadeClaim             bool     `json:"made_claim"`
	ContainsAnswerAttempt bool     `json:"contains_answer_attempt"`
	AnswerAttemptText     string   `json:"answer_attempt_text"`
	EstablishedFacts      []string `json:"established_facts"`
	IsStuck               bool     `json:"is_stuck"`
	IsNoise               bool     `json:"is_noise"`
	StudentAffect         string   `json:"student_affect"`
	Confidence            float64  `json:"confidence"`
}

type Proposal struct {
	Kind         string `json:"kind"`
	EvidenceID   string `json:"evidence_id"`
	HypothesisID string `json:"hypothesis_id"`
	Fact         string `json:"fact"`
	Action       string `json:"action"`
	Focus        string `json:"focus"`
	Value        int    `json:"value"`
	Text         string `json:"text"`
}

type PublicTraceEvent struct {
	Sequence   int                     `json:"sequence"`
	Kind       string                  `json:"kind"`
	Status     string                  `json:"status"`
	Summary    string                  `json:"summary"`
	Text       string                  `json:"text"`
	Reasoning  *PublicReasoningSummary `json:"reasoning,omitempty"`
	Tool       *ToolEventPayload       `json:"tool,omitempty"`
	ToolName   string                  `json:"tool_name"`
	DurationMS int                     `json:"duration_ms"`
}

type PublicReasoningSummary struct {
	Stage string `json:"stage"`
	Text  string `json:"text"`
}

type ToolEventPayload struct {
	Name              string                  `json:"name"`
	RedactedArguments map[string]string       `json:"redacted_arguments"`
	DurationMS        int                     `json:"duration_ms"`
	Result            *PublicAnswerComparison `json:"result,omitempty"`
}

type PublicAnswerComparison struct {
	Tool          string   `json:"tool"`
	Status        string   `json:"status"`
	UserPoints    []string `json:"user_points"`
	SupportStatus string   `json:"support_status"`
	NextAction    string   `json:"next_action"`
}

type VerificationResult struct {
	Relation          string                    `json:"relation"`
	Coverage          float64                   `json:"coverage"`
	CompletionAllowed bool                      `json:"completion_allowed"`
	RuledOutThisTurn  []string                  `json:"ruled_out_this_turn"`
	AnswerComparison  *InternalAnswerComparison `json:"answer_comparison,omitempty"`
}

type InternalAnswerComparison struct {
	AnswerAttemptID             string   `json:"answer_attempt_id"`
	Relation                    string   `json:"relation"`
	ClaimAlignment              float64  `json:"claim_alignment"`
	EvidenceCoverage            float64  `json:"evidence_coverage"`
	BestEvidenceSet             []string `json:"best_evidence_set"`
	MissingEvidence             []string `json:"missing_evidence"`
	Contradictions              []string `json:"contradictions"`
	SolutionCoverage            float64  `json:"solution_coverage"`
	MissingSolutionRequirements []string `json:"missing_solution_requirements"`
	CompletionAllowed           bool     `json:"completion_allowed"`
	UserPoints                  []string `json:"user_points"`
}

type AuditTrace struct {
	ReasonCodes     []string `json:"reason_codes"`
	MentorRationale string   `json:"mentor_rationale"`
	GuardRetries    int      `json:"guard_retries"`
	InterpreterMS   int      `json:"interpreter_ms"`
	MentorMS        int      `json:"mentor_ms"`
	RulesVersion    string   `json:"rules_version"`
}
