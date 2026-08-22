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
	ConversationSummary string            `json:"conversation_summary"`
	UserMessage     string                `json:"user_message"`
	// StructuredUserAction 是 QuickAction 点击产生的一等用户动作；
	// Python Runtime 只把它签发成 UserActionAuthorization，Agent 自身
	// 的 tool_call 不能充当授权。与自然语言共用 request_id / state_revision。
	StructuredUserAction *StructuredUserAction `json:"structured_user_action,omitempty"`
	Budget               Budget                `json:"budget"`
}

// StructuredUserAction 与 agent/src/hiddenworld/contracts/authorization.py
// 的 StructuredUserAction 逐字段一致；Python 侧 extra="forbid"。
type StructuredUserAction struct {
	ActionID        string `json:"action_id"`
	CatalogVersion  string `json:"catalog_version"`
	StateRevision   int    `json:"state_revision"`
	NormalizedScope string `json:"normalized_scope,omitempty"`
}

// UserActionAuthorization 是 Python Runtime 签发的观察授权投影。
// Agent 只能消费授权引用，不能创建或扩大授权。
type UserActionAuthorization struct {
	Source          string `json:"source"`
	ActionRef       string `json:"action_ref"`
	ToolKind        string `json:"tool_kind"`
	NormalizedScope string `json:"normalized_scope"`
	StateRevision   int    `json:"state_revision"`
	AuthorizationID string `json:"authorization_id"`
}

// AuthorizedActionRef 是进入 AgentContext 的安全授权引用。
type AuthorizedActionRef struct {
	AuthorizationID string `json:"authorization_id"`
	ActionRef       string `json:"action_ref"`
	ToolKind        string `json:"tool_kind"`
	NormalizedScope string `json:"normalized_scope"`
}

// TeachingDimensionRef 与 agent/src/hiddenworld/contracts/dimensions.py 一致：
// 题目定义的安全教学维度，category 取值受 Runtime 白名单约束。
type TeachingDimensionRef struct {
	DimensionID string `json:"dimension_id"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	HintLevel   string `json:"hint_level"`
}

type LearnerState struct {
	CollectedEvidence      []string                       `json:"collected_evidence"`
	RuledOutHypotheses     []string                       `json:"ruled_out_hypotheses"`
	CurrentHypothesis      string                         `json:"current_hypothesis,omitempty"`
	EstablishedFacts       []string                       `json:"established_facts"`
	ActionsTaken           []string                       `json:"actions_taken"`
	CurrentFocus           string                         `json:"current_focus"`
	EffectiveTurns         int                            `json:"effective_turns"`
	StalledTurns           int                            `json:"stalled_turns"`
	RecentOpenings         []string                       `json:"recent_openings"`
	ConceptMastery         map[string]int                 `json:"concept_mastery"`
	SkillMastery           map[string]int                 `json:"skill_mastery"`
	ExplanationPreferences ExplanationPreferences         `json:"explanation_preferences"`
	HintLevel              int                            `json:"hint_level"`
	LastHint               string                         `json:"last_hint"`
	RepairStatus           string                         `json:"repair_status"`
}

type ExplanationPreferences struct {
	Detail     string `json:"detail"`
	Analogy    string `json:"analogy"`
	Directness string `json:"directness"`
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
	ContractVersion  string       `json:"contract_version"`
	RequestID        string       `json:"request_id"`
	ExpectedRevision int          `json:"expected_revision"`
	Reply            string       `json:"reply"`
	TurnAnalysis     TurnAnalysis `json:"turn_analysis"`
	// TurnAssessment/TeachingDecision 是 Python Agent 的结构化语义输出。
	// 它们只用于校验与展示，不能替代 Go 持有的状态归约与提议审批。
	TurnAssessment       *TurnAssessment    `json:"turn_assessment"`
	TeachingDecision     *TeachingDecision  `json:"teaching_decision"`
	GuidanceState        GuidanceState      `json:"guidance_state"`
	TurnControl          TurnControl        `json:"turn_control"`
	Proposals            []Proposal         `json:"proposals"`
	PublicTrace          []PublicTraceEvent `json:"public_trace"`
	InternalVerification VerificationResult `json:"internal_verification"`
	InternalAudit        AuditTrace         `json:"internal_audit"`
}

type TurnAssessment struct {
	Intent                string   `json:"intent"`
	UserGoal              string   `json:"user_goal"`
	RequestedAction       string   `json:"requested_action"`
	RequestedActionRaw    string   `json:"requested_action_raw"`
	ClarificationTarget   string   `json:"clarification_target"`
	ActionMatchStatus     string   `json:"action_match_status"`
	Actions               []string `json:"actions"`
	HypothesisID          string   `json:"hypothesis_id"`
	HypothesisRaw         string   `json:"hypothesis_raw"`
	ClaimType             string   `json:"claim_type"`
	MadeClaim             bool     `json:"made_claim"`
	ContainsAnswerAttempt bool     `json:"contains_answer_attempt"`
	AnswerAttemptText     string   `json:"answer_attempt_text"`
	EstablishedFacts      []string `json:"established_facts"`
	ProgressAssessment    string   `json:"progress_assessment"`
	IsStuck               bool     `json:"is_stuck"`
	IsOffTopic            bool     `json:"is_off_topic"`
	IsNoise               bool     `json:"is_noise"`
	StudentAffect         string   `json:"student_affect"`
	Confidence            float64  `json:"confidence"`
	HumorLevel            string         `json:"humor_level"`
	FrustrationLevel      string         `json:"frustration_level"`
	ConfusionLevel        string         `json:"confusion_level"`
	ConfidenceLevel       string         `json:"confidence_level"`
	UrgencyLevel          string         `json:"urgency_level"`
	RandomInvestigation   bool           `json:"random_investigation"`
	ConceptMasterySignals map[string]int `json:"concept_mastery_signals"`
	SkillMasterySignals   map[string]int `json:"skill_mastery_signals"`
	PreferenceSignals     map[string]string `json:"preference_signals"`
}

type TeachingDecision struct {
	TeachingState         string `json:"teaching_state"`
	Strategy              string `json:"strategy"`
	PrimaryTask           string `json:"primary_task"`
	GuidanceDirection     string `json:"guidance_direction"`
	ReplyPolicy           string `json:"reply_policy"`
	AllowExplicitNextStep bool   `json:"allow_explicit_next_step"`
	AllowRuledOutScope    bool   `json:"allow_ruled_out_scope"`
}

type GuidanceState struct {
	TeachingState      string                 `json:"teaching_state"`
	ProgressAssessment string                 `json:"progress_assessment"`
	Navigation         []TeachingDimensionRef `json:"navigation"`
	StalledTurns       int                    `json:"stalled_turns"`
	CurrentFocus       string                 `json:"current_focus"`
	RepairStatus       string                 `json:"repair_status"`
}

type TurnControl struct {
	Terminal          bool     `json:"terminal"`
	CompletionAllowed bool     `json:"completion_allowed"`
	CompletionReady   bool     `json:"completion_ready"`
	AllowedActionIDs  []string `json:"allowed_action_ids"`
}

type TurnAnalysis struct {
	// PublicSummary 是 Python 侧 TurnAnalysis 的第一个字段，驱动
	// reasoning_summary_delta 流式推理摘要。Go 不消费它，但**必须声明**——
	// client.go 对最终 result 事件用 DisallowUnknownFields()，漏一个字段
	// 整轮会以 agent_invalid_response 失败，而且失败发生在正文流完之后，
	// 表现为"明明响应成功却被判无效"。
	PublicSummary         string   `json:"public_summary"`
	Intent                string   `json:"intent"`
	RequestedActionRaw    string   `json:"requested_action_raw"`
	ClarificationTarget   string   `json:"clarification_target"`
	ActionMatchStatus     string   `json:"action_match_status"`
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
	Kind            string `json:"kind"`
	EvidenceID      string `json:"evidence_id"`
	HypothesisID    string `json:"hypothesis_id"`
	Fact            string `json:"fact"`
	Action          string `json:"action"`
	Focus           string `json:"focus"`
	Value           int    `json:"value"`
	Text            string `json:"text"`
	ConceptID       string `json:"concept_id"`
	SkillID         string `json:"skill_id"`
	PreferenceKey   string `json:"preference_key"`
	PreferenceValue string `json:"preference_value"`
}

type PublicTraceEvent struct {
	Sequence    int                     `json:"sequence"`
	Kind        string                  `json:"kind"`
	Status      string                  `json:"status"`
	Summary     string                  `json:"summary"`
	Text        string                  `json:"text"`
	Reasoning   *PublicReasoningSummary `json:"reasoning,omitempty"`
	Observation *PublicObservation      `json:"observation,omitempty"`
	Tool        *ToolEventPayload       `json:"tool,omitempty"`
	ToolName    string                  `json:"tool_name"`
	DurationMS  int                     `json:"duration_ms"`
}

type PublicObservation struct {
	Action     string `json:"action"`
	Result     string `json:"result"`
	IsNegative bool   `json:"is_negative"`
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
	Tool              string   `json:"tool"`
	Status            string   `json:"status"`
	UserPoints        []string `json:"user_points"`
	ConclusionStatus  string   `json:"conclusion_status"`
	EvidenceStatus    string   `json:"evidence_status"`
	CausalStatus      string   `json:"causal_status"`
	MissingDimensions []string `json:"missing_dimensions"`
	Contradictions    []string `json:"contradictions"`
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
