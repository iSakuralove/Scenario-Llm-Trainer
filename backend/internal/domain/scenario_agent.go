package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// ScenarioLearnerState 是 Go 持有的排查工坊权威学习状态。
// Python 只能提出变更建议，最终状态必须由 Go 审批后写入。
type ScenarioLearnerState struct {
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
	ExplanationPreferences ScenarioExplanationPreferences `json:"explanation_preferences"`
	HintLevel              int                            `json:"hint_level"`
	LastHint               string                         `json:"last_hint,omitempty"`
	// RepairStatus 是仅供教学导航的三值抽象；不保存内部 solution coverage。
	RepairStatus string `json:"repair_status"`
}

// ScenarioTeachingProjection 是学生侧可见的教学状态投影。
//
// 它只保留“导师如何承接本轮”和“解释深度如何调整”的安全摘要，不能替代
// LearnerState，也不携带原始情绪、假设 ID、答案比较、证据 ID 或内部裁判结果。
type ScenarioTeachingProjection struct {
	TeachingState      string                    `json:"teaching_state"`
	ProgressAssessment string                    `json:"progress_assessment"`
	DirectionStatus    string                    `json:"direction_status"`
	DetailLevel        string                    `json:"detail_level"`
	Focus              string                    `json:"focus,omitempty"`
	Mastery            ScenarioMasteryProjection `json:"mastery"`
}

type ScenarioMasteryProjection struct {
	Concepts        []ScenarioMasteryItem `json:"concepts,omitempty"`
	Skills          []ScenarioMasteryItem `json:"skills,omitempty"`
	ConceptsCovered int                   `json:"concepts_covered"`
	ConceptsTotal   int                   `json:"concepts_total"`
	SkillsCovered   int                   `json:"skills_covered"`
	SkillsTotal     int                   `json:"skills_total"`
}

type ScenarioMasteryItem struct {
	Label  string  `json:"label"`
	Level  int     `json:"level"`
	Weight float64 `json:"weight"`
}

type ScenarioExplanationPreferences struct {
	Detail     string `json:"detail"`
	Analogy    string `json:"analogy"`
	Directness string `json:"directness"`
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
	Tool              string   `json:"tool"`
	Status            string   `json:"status"`
	UserPoints        []string `json:"user_points"`
	ConclusionStatus  string   `json:"conclusion_status"`
	EvidenceStatus    string   `json:"evidence_status"`
	CausalStatus      string   `json:"causal_status"`
	MissingDimensions []string `json:"missing_dimensions"`
	Contradictions    []string `json:"contradictions"`
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
	// V2 外层字段。SchemaVersion 为空表示存量 v1 事件（只读兼容），
	// 新写入一律 hiddenworld.v2；StateRevision 描述事件所属业务状态版本。
	SchemaVersion string                   `json:"schema_version,omitempty"`
	StateRevision int                      `json:"state_revision,omitempty"`
	Payload       *ScenarioRunEventPayload `json:"payload,omitempty"`
}

// ScenarioRunEventSchemaV2 是 Go SSE 出口与落库事件的协议版本。
// v1 事件不带 schema_version，前端 LegacyEventAdapter 按缺失值识别。
const ScenarioRunEventSchemaV2 = "hiddenworld.v2"

// ToolCallState 取值：任务/工具调用的生命周期状态，由 task_upserted 承载。
const (
	ScenarioTaskPending     = "pending"
	ScenarioTaskRunning     = "running"
	ScenarioTaskCompleted   = "completed"
	ScenarioTaskFailed      = "failed"
	ScenarioTaskUnsupported = "unsupported"
	ScenarioTaskRejected    = "rejected"
	ScenarioTaskExpired     = "expired"
)

// ScenarioRunEventPayload 是 V2 判别联合的 payload 容器。
// Go 是唯一生成者：每种 kind 只填对应子对象，validateScenarioRunEventV2
// 负责检查 kind 与 payload 的对应关系，禁止 kitchen-sink 填法下发。
type ScenarioRunEventPayload struct {
	// turn_started
	TurnID      string `json:"turn_id,omitempty"`
	TaskSummary string `json:"task_summary,omitempty"`
	// task_upserted
	Task *ScenarioTaskPayload `json:"task,omitempty"`
	// tool_result
	ToolResult *ScenarioToolResultPayload `json:"tool_result,omitempty"`
	// clue_published
	Clue *ScenarioCluePayload `json:"clue,omitempty"`
	// hint_published
	Hint *ScenarioHintPayload `json:"hint,omitempty"`
	// assistant_delta：phase = understanding | replying
	Phase              string `json:"phase,omitempty"`
	MarkdownReadyDelta string `json:"markdown_ready_delta,omitempty"`
	// turn_completed
	NextActions []ScenarioAllowedAction `json:"next_actions,omitempty"`
	// turn_failed
	ErrorCode string `json:"error_code,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

// ScenarioTaskPayload 是 task_upserted 的负载：工具调用生命周期状态。
type ScenarioTaskPayload struct {
	TaskID    string `json:"task_id"`
	CallID    string `json:"call_id,omitempty"`
	Round     int    `json:"round,omitempty"`
	Title     string `json:"title"`
	State     string `json:"state"`
	ToolRef   string `json:"tool_ref,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// ScenarioToolResultPayload 是 tool_result 的负载：只表达执行终态。
// pending/running/rejected 等未执行状态不产生 tool_result，走 task_upserted。
type ScenarioToolResultPayload struct {
	CallID       string                 `json:"call_id"`
	ToolID       string                 `json:"tool_id"`
	ToolKind     string                 `json:"tool_kind"`
	Round        int                    `json:"round,omitempty"`
	ResultStatus string                 `json:"result_status"`
	DurationMS   int                    `json:"duration_ms"`
	Content      *ScenarioPublicContent `json:"content,omitempty"`
	ErrorCode    string                 `json:"error_code,omitempty"`
}

// ScenarioPublicContent 是 observation / clue 的统一外发内容层，
// markdown_ready 是前端唯一渲染源。
type ScenarioPublicContent struct {
	ContentType    string                        `json:"content_type"`
	MarkdownReady  string                        `json:"markdown_ready"`
	DisplayVariant string                        `json:"display_variant,omitempty"`
	Meta           *ScenarioPublicContentMeta    `json:"meta,omitempty"`
	Details        *ScenarioPublicContentDetails `json:"details,omitempty"`
}

// ScenarioPublicContentDetails 是工具卡可展开的固定安全 JSON。
// 只包含公开工具身份、执行终态、耗时、来源和公开摘要；不允许把参数、
// 授权标识、证据 ID、答案比较或内部错误细节塞进这个对象。
type ScenarioPublicContentDetails struct {
	ToolID       string `json:"tool_id"`
	ToolKind     string `json:"tool_kind"`
	ResultStatus string `json:"result_status"`
	DurationMS   int    `json:"duration_ms"`
	SourceKind   string `json:"source_kind,omitempty"`
	SourceLabel  string `json:"source_label,omitempty"`
	Summary      string `json:"summary,omitempty"`
}

type ScenarioPublicContentMeta struct {
	ToolKind    string `json:"tool_kind,omitempty"`
	IsNegative  bool   `json:"is_negative,omitempty"`
	SourceKind  string `json:"source_kind,omitempty"`
	SourceLabel string `json:"source_label,omitempty"`
	Title       string `json:"title,omitempty"`
}

// ScenarioCluePayload 是 clue_published 的负载。
type ScenarioCluePayload struct {
	ClueID    string                     `json:"clue_id"`
	Content   ScenarioPublicContent      `json:"content"`
	Dimension *ScenarioTeachingDimension `json:"dimension,omitempty"`
}

// ScenarioHintPayload 与线索分离。提示帮助学生缩小范围，但不会计入学生独立发现的证据。
type ScenarioHintPayload struct {
	HintID  string                `json:"hint_id"`
	Level   int                   `json:"level"`
	Content ScenarioPublicContent `json:"content"`
}

// ScenarioTeachingDimension 与 agentclient.TeachingDimensionRef 同构，
// 是 TeachingNavigation 与 missing_dimensions 的共同维度引用。
type ScenarioTeachingDimension struct {
	DimensionID string `json:"dimension_id"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	HintLevel   string `json:"hint_level"`
}

// ScenarioAllowedAction 是 turn_completed 下发的结构化动作，
// 前端渲染 QuickActions；点击后以 StructuredUserAction 回传。
type ScenarioAllowedAction struct {
	ActionID       string `json:"action_id"`
	CatalogVersion string `json:"catalog_version"`
	ToolKind       string `json:"tool_kind"`
	Title          string `json:"title"`
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
	if state.ConceptMastery == nil {
		state.ConceptMastery = map[string]int{}
	}
	if state.SkillMastery == nil {
		state.SkillMastery = map[string]int{}
	}
	if state.ExplanationPreferences.Detail == "" {
		state.ExplanationPreferences.Detail = "balanced"
	}
	if state.ExplanationPreferences.Analogy == "" {
		state.ExplanationPreferences.Analogy = "medium"
	}
	if state.ExplanationPreferences.Directness == "" {
		state.ExplanationPreferences.Directness = "medium"
	}
	if state.HintLevel < 0 {
		state.HintLevel = 0
	}
	if state.HintLevel > 4 {
		state.HintLevel = 4
	}
	if state.RepairStatus != "partial" && state.RepairStatus != "sufficient" {
		state.RepairStatus = "none"
	}
	return state
}

type ScenarioSessionTransition struct {
	SessionID        string
	ExpectedRevision int
	NextSession      ScenarioSession
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
