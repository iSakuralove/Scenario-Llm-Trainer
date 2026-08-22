package domain

import (
	"strings"
	"time"
)

const (
	RoleStudent    = "student"
	RoleInstructor = "instructor"
	RoleAdmin      = "admin"
)

const (
	AIJobKindScenarioGeneration = "scenario_generation"

	AIJobStatusQueued    = "queued"
	AIJobStatusRunning   = "running"
	AIJobStatusCompleted = "completed"
	AIJobStatusFailed    = "failed"
	AIJobStatusCanceled  = "canceled"
)

func ValidRole(role string) bool {
	switch role {
	case RoleStudent, RoleInstructor, RoleAdmin:
		return true
	default:
		return false
	}
}

type User struct {
	ID           string      `json:"id"`
	Username     string      `json:"username"`
	Email        string      `json:"email"`
	PasswordHash string      `json:"-"`
	TokenVersion int         `json:"-"`
	Role         string      `json:"role"`
	Profile      UserProfile `json:"profile"`
	CreatedAt    time.Time   `json:"created_at"`
}

type UserProfile struct {
	TargetLevel           string           `json:"target_level"`
	TargetRole            string           `json:"target_role,omitempty"`
	PreferredDomains      []string         `json:"preferred_domains"`
	ResumeSummary         string           `json:"resume_summary,omitempty"`
	ProjectSummary        string           `json:"project_summary,omitempty"`
	ResumeDocuments       []ResumeDocument `json:"resume_documents,omitempty"`
	ManualResumeUpdatedAt *time.Time       `json:"manual_resume_updated_at,omitempty"`
	CapabilityRadar       map[string]int   `json:"capability_radar"`
	WeakPoints            []WeakPoint      `json:"weak_points"`
	TotalStats            TotalStats       `json:"total_stats"`
	CheckinDates          []string         `json:"checkin_dates,omitempty"`
	LastCheckinDate       string           `json:"last_checkin_date,omitempty"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

type ResumeDocument struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	SourceType    string    `json:"source_type"`
	Format        string    `json:"format"`
	AssetID       string    `json:"asset_id,omitempty"`
	ContentURL    string    `json:"content_url,omitempty"`
	Content       string    `json:"content,omitempty"`
	ExtractedText string    `json:"extracted_text"`
	ParseStatus   string    `json:"parse_status"`
	QualityStatus string    `json:"quality_status"`
	QualityReason string    `json:"quality_reason,omitempty"`
	Editable      bool      `json:"editable"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type WeakPoint struct {
	Domain             string   `json:"domain"`
	Topic              string   `json:"topic"`
	LastScore          int      `json:"last_score"`
	SuggestedQuestions []string `json:"suggested_questions"`
}

type TotalStats struct {
	ScenariosSolved int `json:"scenarios_solved"`
	InterviewsTaken int `json:"interviews_taken"`
	AverageScore    int `json:"average_score"`
	StreakDays      int `json:"streak_days"`
}

type LearningPlan struct {
	GeneratedAt     time.Time                `json:"generated_at"`
	Summary         string                   `json:"summary"`
	TargetLevel     string                   `json:"target_level"`
	FocusDomains    []string                 `json:"focus_domains"`
	DomainInsights  []LearningDomainInsight  `json:"domain_insights"`
	Recommendations []LearningRecommendation `json:"recommendations"`
	ReviewPlan      []ReviewPlanItem         `json:"review_plan"`
}

type LearningDomainInsight struct {
	Domain         string `json:"domain"`
	Score          int    `json:"score"`
	Level          string `json:"level"`
	Trend          string `json:"trend"`
	CompletedCount int    `json:"completed_count"`
	LastScore      int    `json:"last_score,omitempty"`
	Reason         string `json:"reason"`
}

type LearningRecommendation struct {
	ID          string                `json:"id"`
	Kind        string                `json:"kind"`
	Domain      string                `json:"domain"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Difficulty  string                `json:"difficulty"`
	Priority    int                   `json:"priority"`
	Reason      string                `json:"reason"`
	ActionLabel string                `json:"action_label"`
	ActionPath  string                `json:"action_path"`
	Question    *ScenarioQuestionView `json:"question,omitempty"`
}

type ReviewPlanItem struct {
	DayLabel         string   `json:"day_label"`
	Domain           string   `json:"domain"`
	Focus            string   `json:"focus"`
	Actions          []string `json:"actions"`
	EstimatedMinutes int      `json:"estimated_minutes"`
	TargetScore      int      `json:"target_score"`
	QuestionIDs      []string `json:"question_ids"`
	SourceKind       string   `json:"source_kind,omitempty"`
	SourceID         string   `json:"source_id,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

type ReviewCalendar struct {
	GeneratedAt  time.Time        `json:"generated_at"`
	CheckinDates []string         `json:"checkin_dates"`
	StreakDays   int              `json:"streak_days"`
	TodayChecked bool             `json:"today_checked"`
	Today        string           `json:"today"`
	ReviewPlan   []ReviewPlanItem `json:"review_plan"`
	FocusDomains []string         `json:"focus_domains"`
	NextAction   string           `json:"next_action"`
}

type CheckinResult struct {
	CheckedIn        bool   `json:"checked_in"`
	AlreadyCheckedIn bool   `json:"already_checked_in"`
	CheckinDate      string `json:"checkin_date"`
	StreakDays       int    `json:"streak_days"`
	NextAction       string `json:"next_action"`
}

type ScenarioQuestion struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Domain       string          `json:"domain"`
	Difficulty   string          `json:"difficulty"`
	ScenarioType string          `json:"scenario_type"`
	Tags         []string        `json:"tags"`
	Content      ScenarioContent `json:"content"`
	Status       string          `json:"status"`
	Source       string          `json:"source"`
	CreatedBy    string          `json:"created_by"`
	Version      int             `json:"version"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ScenarioQuestionView struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Domain       string          `json:"domain"`
	Difficulty   string          `json:"difficulty"`
	ScenarioType string          `json:"scenario_type"`
	Tags         []string        `json:"tags"`
	Content      ScenarioContent `json:"content"`
	Status       string          `json:"status"`
	Source       string          `json:"source"`
	CreatedBy    string          `json:"created_by"`
	Version      int             `json:"version"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	IsSanitized  bool            `json:"is_sanitized"`
}

type ScenarioContent struct {
	ModelVersion     string          `json:"model_version,omitempty"`
	ContractVersion  string          `json:"contract_version,omitempty"`
	ContractChecksum string          `json:"contract_checksum,omitempty"`
	PublicScenario   *PublicScenario `json:"public_scenario,omitempty"`
	HiddenWorld      *HiddenWorld    `json:"hidden_world,omitempty"`
	// 以下字段仅用于旧题目迁移期。hiddenworld.v1 题目以
	// PublicScenario + HiddenWorld 为唯一权威内容。
	RootCause               string               `json:"root_cause,omitempty"`
	RootCauseKeywords       []string             `json:"root_cause_keywords,omitempty"`
	KeyEvidence             []string             `json:"key_evidence,omitempty"`
	StandardProcedure       []string             `json:"standard_procedure,omitempty"`
	RevealStrategy          RevealStrategy       `json:"reveal_strategy"`
	ArchitectureDiagram     string               `json:"architecture_diagram"`
	ArchitectureDiagramSpec *ScenarioDiagramSpec `json:"architecture_diagram_spec,omitempty"`
	DiagramStatus           string               `json:"diagram_status,omitempty"`
	DiagramWarnings         []string             `json:"diagram_warnings,omitempty"`
	ReferenceLinks          []string             `json:"reference_links"`
}

type PublicScenario struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Environment         string   `json:"environment,omitempty"`
	InitialSymptoms     []string `json:"initial_symptoms"`
	ArchitectureDiagram string   `json:"architecture_diagram"`
	// ArchitectureDiagramSpec 是生成模型输出的结构化架构描述；
	// 后端用 BuildMermaidFromSpec 确定性渲染成 ArchitectureDiagram，
	// 避免 LLM 手写 Mermaid 引入语法错误或注入风险。
	ArchitectureDiagramSpec *ScenarioDiagramSpec `json:"architecture_diagram_spec,omitempty"`
}

type HiddenWorld struct {
	RootCause RootCause `json:"root_cause"`
	// CanonicalAnswer 是 V2 题目的独立持久化权威答案；旧 v1 题目为 nil，
	// 仅做读取兼容。Go 只透传给 Python Runtime 消费，绝不下发前端。
	CanonicalAnswer     *CanonicalAnswer    `json:"canonical_answer,omitempty"`
	DiagnosticRelations []string            `json:"diagnostic_relations,omitempty"`
	Hypotheses          []Hypothesis        `json:"hypotheses"`
	EvidenceGraph       []EvidenceNode      `json:"evidence_graph"`
	Observations        []Observation       `json:"observations"`
	VirtualTools        []VirtualTool       `json:"virtual_tools,omitempty"`
	SolutionRubric      SolutionRubric      `json:"solution_rubric"`
	MisconceptionRules  []MisconceptionRule `json:"misconception_rules"`
	TeachingModel       *TeachingModel      `json:"teaching_model,omitempty"`
}

// CanonicalAnswer 与 agent/src/hiddenworld/contracts/answer.py 的 CanonicalAnswer
// 逐字段一致。唯一性与引用一致性由 Python ScenarioContractValidator 在生成/加载时
// 校验；Go 持有同一份快照用于透传与审计，不自行解读。
type CanonicalAnswer struct {
	CanonicalConclusion     string   `json:"canonical_conclusion"`
	RootCauseID             string   `json:"root_cause_id"`
	RequiredEvidenceIDs     []string `json:"required_evidence_ids"`
	RequiredCausalRelations []string `json:"required_causal_relations"`
	AcceptedEquivalents     []string `json:"accepted_equivalents"`
	SolutionRequirements    []string `json:"solution_requirements"`
	DirectTrigger           string   `json:"direct_trigger,omitempty"`
	LatentIssues            []string `json:"latent_issues,omitempty"`
	Phenomenon              string   `json:"phenomenon,omitempty"`
	DerivedRisks            []string `json:"derived_risks,omitempty"`
	AnswerVersion           string   `json:"answer_version"`
}

// TeachingModel 只描述教学表达、概念目录和证据可用性。
// 其中安全字段可投影给 ScenarioAgent；提示阶梯仍由 Runtime 按状态选择。
type TeachingModel struct {
	MentorPersona             MentorPersona              `json:"mentor_persona"`
	Concepts                  []ConceptDefinition        `json:"concepts"`
	EvidenceAvailabilityRules []EvidenceAvailabilityRule `json:"evidence_availability_rules"`
	HintLadder                []HintStep                 `json:"hint_ladder"`
}

type MentorPersona struct {
	StyleName         string  `json:"style_name"`
	Tone              string  `json:"tone"`
	Detail            string  `json:"detail"`
	Directness        float64 `json:"directness"`
	Humor             float64 `json:"humor"`
	TimelineFocus     float64 `json:"timeline_focus"`
	QuestionFrequency float64 `json:"question_frequency"`
}

type ConceptDefinition struct {
	ConceptID string   `json:"concept_id"`
	Label     string   `json:"label"`
	Summary   string   `json:"summary"`
	Aliases   []string `json:"aliases"`
}

type EvidenceAvailabilityRule struct {
	RequestPatterns []string `json:"request_patterns"`
	Availability    string   `json:"availability"`
	PublicMessage   string   `json:"public_message"`
	ActionIDs       []string `json:"action_ids"`
}

type HintStep struct {
	Level          int      `json:"level"`
	PublicHint     string   `json:"public_hint"`
	FocusActionIDs []string `json:"focus_action_ids"`
}

// VirtualTool 描述题目自带的只读模拟查询入口。查询文本仅用于意图匹配，
// 后端与 Agent 都不得执行真实 SQL、Shell、HTTP 或外部 API。
type VirtualTool struct {
	ToolID             string   `json:"tool_id"`
	Kind               string   `json:"kind"`
	Target             string   `json:"target"`
	Aliases            []string `json:"aliases"`
	QueryPatterns      []string `json:"query_patterns"`
	RedactedParameters []string `json:"redacted_parameters"`
	SimulatedOutput    string   `json:"simulated_output"`
	ObservationAction  string   `json:"observation_action"`
	EvidenceIDs        []string `json:"evidence_ids"`
}

type RootCause struct {
	ID                     string     `json:"id"`
	Category               string     `json:"category"`
	Component              string     `json:"component"`
	Description            string     `json:"description"`
	SufficientEvidenceSets [][]string `json:"sufficient_evidence_sets"`
	AcceptedHypotheses     []string   `json:"accepted_hypotheses"`
	SolutionRequirements   []string   `json:"solution_requirements"`
}

type Hypothesis struct {
	HypothesisID string `json:"hypothesis_id"`
	Label        string `json:"label"`
}

type EvidenceNode struct {
	EvidenceID     string   `json:"evidence_id"`
	Content        string   `json:"content"`
	Category       string   `json:"category"`
	Prerequisites  []string `json:"prerequisites"`
	ObtainedBy     []string `json:"obtained_by"`
	ClueImportance string   `json:"clue_importance,omitempty"`
	PublicTitle    string   `json:"public_title,omitempty"`
	DiagnosticRole string   `json:"diagnostic_role,omitempty"`
}

type Observation struct {
	Action                  string   `json:"action"`
	Result                  string   `json:"result"`
	IsNegative              bool     `json:"is_negative"`
	YieldsEvidence          []string `json:"yields_evidence"`
	RulesOut                []string `json:"rules_out"`
	UnmetPrerequisiteResult string   `json:"unmet_prerequisite_result"`
}

type SolutionRubric struct {
	RequiredActions   []string `json:"required_actions"`
	VerificationSteps []string `json:"verification_steps"`
	RollbackNotes     []string `json:"rollback_notes"`
}

type MisconceptionRule struct {
	MisconceptionID   string   `json:"misconception_id"`
	PatternHypotheses []string `json:"pattern_hypotheses"`
	WhyWrong          string   `json:"why_wrong"`
}

type ScenarioDiagramSpec struct {
	Direction string                `json:"direction"`
	Nodes     []ScenarioDiagramNode `json:"nodes"`
	Edges     []ScenarioDiagramEdge `json:"edges"`
}

type ScenarioDiagramNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type ScenarioDiagramEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
	Style string `json:"style,omitempty"`
}

type RevealStrategy struct {
	SurfaceClues []Clue `json:"surface_clues"`
	DeepClues    []Clue `json:"deep_clues"`
	Distractors  []Clue `json:"distractors"`
}

type Clue struct {
	ClueID             string   `json:"clue_id"`
	TriggerKeywords    []string `json:"trigger_keywords"`
	PrerequisiteClues  []string `json:"prerequisite_clues,omitempty"`
	Content            string   `json:"content"`
	IsDistractor       bool     `json:"is_distractor"`
	RecommendedNextAsk string   `json:"recommended_next_ask,omitempty"`
}

type ScenarioSession struct {
	ID                  string               `json:"id"`
	UserID              string               `json:"user_id"`
	QuestionID          string               `json:"question_id"`
	Status              string               `json:"status"`
	CurrentTurn         int                  `json:"current_turn"`
	MaxTurns            int                  `json:"max_turns"`
	RevealedClueIDs     []string             `json:"revealed_clue_ids"`
	UserAnswer          string               `json:"user_answer,omitempty"`
	EvaluationResult    *ScenarioEvaluation  `json:"evaluation_result,omitempty"`
	Score               *ScenarioScore       `json:"score,omitempty"`
	QuestionSnapshot    ScenarioQuestion     `json:"question_snapshot"`
	StateRevision       int                  `json:"state_revision"`
	LearnerState        ScenarioLearnerState `json:"learner_state"`
	ConversationSummary string               `json:"conversation_summary,omitempty"`
	StartedAt           time.Time            `json:"started_at"`
	LastActiveAt        time.Time            `json:"last_active_at"`
	EndedAt             *time.Time           `json:"ended_at,omitempty"`
}

type ScenarioMessage struct {
	ID               string       `json:"id"`
	SessionID        string       `json:"session_id"`
	TurnNumber       int          `json:"turn_number"`
	Role             string       `json:"role"`
	UserContent      string       `json:"user_content"`
	AssistantContent string       `json:"assistant_content"`
	ResponseMeta     ResponseMeta `json:"response_meta"`
	CreatedAt        time.Time    `json:"created_at"`
}

type ResponseMeta struct {
	ResponseType       string                      `json:"response_type"`
	RequestID          string                      `json:"request_id,omitempty"`
	Revision           int                         `json:"revision"`
	RunEvents          []ScenarioRunEvent          `json:"run_events,omitempty"`
	TeachingProjection *ScenarioTeachingProjection `json:"teaching_projection,omitempty"`
}

type AgentTrace struct {
	RunID      string      `json:"run_id"`
	Agent      string      `json:"agent"`
	Mode       string      `json:"mode"`
	Steps      []AgentStep `json:"steps"`
	ToolCount  int         `json:"tool_count"`
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt time.Time   `json:"finished_at"`
}

type AgentStep struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Status    string            `json:"status"`
	Summary   string            `json:"summary"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at"`
}

type ScenarioEvaluation struct {
	IsCorrect         bool                   `json:"is_correct"`
	MatchDegree       int                    `json:"match_degree"`
	MissingPoints     []string               `json:"missing_points"`
	StandardProcedure []string               `json:"standard_procedure"`
	ScoringReport     *ScenarioScoringReport `json:"scoring_report,omitempty"`
}

type ScenarioScore struct {
	Efficiency int `json:"efficiency"`
	Accuracy   int `json:"accuracy"`
	ClueUsage  int `json:"clue_usage"`
	Total      int `json:"total"`
}

type ScenarioScoringReport struct {
	OverallScore           int                       `json:"overall_score"`
	RootCauseSimilarity    int                       `json:"root_cause_similarity"`
	EvidenceChainScore     int                       `json:"evidence_chain_score"`
	ProcedureCoverageScore int                       `json:"procedure_coverage_score"`
	ClueUsageScore         int                       `json:"clue_usage_score"`
	ReasoningDepthScore    int                       `json:"reasoning_depth_score"`
	EfficiencyScore        int                       `json:"efficiency_score"`
	MatchedDocuments       []ScenarioMatchedDocument `json:"matched_documents"`
	EvidenceEvents         []ScenarioEvidenceEvent   `json:"evidence_events"`
	Penalties              []string                  `json:"penalties"`
	ScoreExplanation       string                    `json:"score_explanation"`
}

type ScenarioMatchedDocument struct {
	DocType string  `json:"doc_type"`
	DocKey  string  `json:"doc_key"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

type ScenarioEvidenceEvent struct {
	TurnNumber  int     `json:"turn_number"`
	EventType   string  `json:"event_type"`
	Text        string  `json:"text"`
	BestDocType string  `json:"best_doc_type,omitempty"`
	BestDocKey  string  `json:"best_doc_key,omitempty"`
	Score       float64 `json:"score"`
}

type InterviewQuestion struct {
	ID                   string                `json:"id"`
	Title                string                `json:"title"`
	Description          string                `json:"description"`
	Domain               string                `json:"domain"`
	Difficulty           string                `json:"difficulty"`
	QuestionType         string                `json:"question_type"`
	ReferenceAnswer      string                `json:"reference_answer,omitempty"`
	ReferenceKeywords    []string              `json:"reference_keywords,omitempty"`
	EvaluationDimensions []EvaluationDimension `json:"evaluation_dimensions"`
	FollowUpStrategies   []FollowUpStrategy    `json:"follow_up_strategies"`
	CreatedAt            time.Time             `json:"created_at"`
}

type EvaluationDimension struct {
	Name     string  `json:"name"`
	Weight   float64 `json:"weight"`
	Criteria string  `json:"criteria"`
}

type FollowUpStrategy struct {
	TriggerCondition string `json:"trigger_condition"`
	QuestionTemplate string `json:"question_template"`
	Type             string `json:"type"`
}

type InterviewSession struct {
	ID                    string                                `json:"id"`
	UserID                string                                `json:"user_id"`
	QuestionID            string                                `json:"question_id"`
	Mode                  string                                `json:"mode,omitempty"`
	ResumeDocumentIDs     []string                              `json:"resume_document_ids,omitempty"`
	CandidateContext      string                                `json:"-"`
	Status                string                                `json:"status"`
	CurrentRound          int                                   `json:"current_round"`
	MaxRounds             int                                   `json:"max_rounds"`
	SmartClose            bool                                  `json:"smart_close"`
	EndReason             string                                `json:"end_reason,omitempty"`
	DifficultyLevel       string                                `json:"difficulty_level,omitempty"`
	FocusAreas            []string                              `json:"focus_areas,omitempty"`
	SetupNotes            string                                `json:"setup_notes,omitempty"`
	Submissions           []InterviewSubmission                 `json:"submissions"`
	Evaluations           []InterviewEvaluation                 `json:"evaluations"`
	FollowUpQuestion      string                                `json:"follow_up_question,omitempty"`
	FinalScore            int                                   `json:"final_score,omitempty"`
	FinalReport           string                                `json:"final_report,omitempty"`
	QuestionSnapshot      InterviewQuestionSnapshot             `json:"question_snapshot,omitempty"`
	SelectedAtomSnapshots []InterviewKnowledgeAtomLightSnapshot `json:"selected_atom_snapshots,omitempty"`
	StartedAt             time.Time                             `json:"started_at"`
	EndedAt               *time.Time                            `json:"ended_at,omitempty"`
}

type InterviewSubmission struct {
	Round           int                 `json:"round"`
	Content         string              `json:"content"`
	Type            string              `json:"type"`
	Source          string              `json:"source,omitempty"`
	QualityFlag     string              `json:"quality_flag,omitempty"`
	AssetID         string              `json:"asset_id,omitempty"`
	AssetURL        string              `json:"asset_url,omitempty"`
	Asset           *Asset              `json:"asset,omitempty"`
	Transcript      string              `json:"transcript,omitempty"`
	DurationSeconds int                 `json:"duration_seconds,omitempty"`
	VoiceQuality    *VoiceQualityResult `json:"voice_quality,omitempty"`
	SubmittedAt     time.Time           `json:"submitted_at"`
}

type InterviewEvaluation struct {
	Round             int            `json:"round"`
	TotalScore        int            `json:"total_score"`
	DimensionScores   map[string]int `json:"dimension_scores"`
	IsPassed          bool           `json:"is_passed"`
	Highlights        []string       `json:"highlights"`
	Deficiencies      []string       `json:"deficiencies"`
	FollowUpTriggered bool           `json:"follow_up_triggered"`
	FollowUpQuestion  string         `json:"follow_up_question,omitempty"`
	FollowUpType      string         `json:"follow_up_type,omitempty"`
	FollowUpSubject   string         `json:"follow_up_subject,omitempty"`
	FallbackUsed      bool           `json:"fallback_used,omitempty"`
	RetrievedSubjects []string       `json:"retrieved_subjects,omitempty"`
	AgentTrace        *AgentTrace    `json:"agent_trace,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

type CommunityPost struct {
	ID                      string               `json:"id"`
	UserID                  string               `json:"user_id"`
	AuthorUsername          string               `json:"author_username,omitempty"`
	Title                   string               `json:"title"`
	RawContent              string               `json:"raw_content"`
	Domain                  string               `json:"domain"`
	Tags                    []string             `json:"tags"`
	ForkedFromScenarioID    string               `json:"forked_from_scenario_id,omitempty"`
	AIStructuredContent     ScenarioContent      `json:"ai_structured_content"`
	EditedStructuredContent *ScenarioContent     `json:"edited_structured_content,omitempty"`
	ModerationSummary       *ModerationSummary   `json:"moderation_summary,omitempty"`
	ReviewHistory           []ReviewHistoryItem  `json:"review_history"`
	SensitiveCheck          SensitiveCheckResult `json:"sensitive_check"`
	ConvertedQuestionID     string               `json:"converted_question_id,omitempty"`
	Status                  string               `json:"status"`
	ReviewedBy              string               `json:"reviewed_by,omitempty"`
	ReviewedAt              *time.Time           `json:"reviewed_at,omitempty"`
	ReviewNote              string               `json:"review_note,omitempty"`
	FinalizedBy             string               `json:"finalized_by,omitempty"`
	FinalizedAt             *time.Time           `json:"finalized_at,omitempty"`
	FinalNote               string               `json:"final_note,omitempty"`
	CreatedAt               time.Time            `json:"created_at"`
	UpdatedAt               time.Time            `json:"updated_at"`
}

type ModerationSummary struct {
	AgentTrace     *AgentTrace `json:"agent_trace,omitempty"`
	Status         string      `json:"status,omitempty"`
	RiskLevel      string      `json:"risk_level,omitempty"`
	Recommendation string      `json:"recommendation,omitempty"`
	SafeSummary    string      `json:"safe_summary,omitempty"`
	SafeRiskNote   string      `json:"safe_risk_note,omitempty"`
	SafeActionHint string      `json:"safe_action_hint,omitempty"`
	SafeLabels     []string    `json:"safe_labels,omitempty"`
	SuggestedNote  string      `json:"suggested_note,omitempty"`
	Reasons        []string    `json:"reasons,omitempty"`
	Flagged        bool        `json:"flagged,omitempty"`
	UpdatedAt      time.Time   `json:"updated_at,omitempty"`
}

type ReviewHistoryItem struct {
	ID         string           `json:"id"`
	ActorID    string           `json:"actor_id"`
	Action     string           `json:"action"`
	FromStatus string           `json:"from_status,omitempty"`
	ToStatus   string           `json:"to_status"`
	Note       string           `json:"note,omitempty"`
	Content    *ScenarioContent `json:"content,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
}

type AIJob struct {
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	Kind             string     `json:"kind"`
	Status           string     `json:"status"`
	Stage            string     `json:"stage"`
	Progress         int        `json:"progress"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	ValidationErrors []string   `json:"validation_errors,omitempty"`
	Provider         string     `json:"provider,omitempty"`
	Model            string     `json:"model,omitempty"`
	Validated        bool       `json:"validated"`
	FallbackUsed     bool       `json:"fallback_used"`
	FallbackEvents   []string   `json:"fallback_events,omitempty"`
	ResultQuestionID string     `json:"result_question_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// AppendFallbackEvent 记录一条回退轨迹（如 "deepseek:auth → glm"），供前端展示 provider 切换过程。
func (j *AIJob) AppendFallbackEvent(event string) {
	if strings.TrimSpace(event) == "" {
		return
	}
	if len(j.FallbackEvents) >= 8 {
		j.FallbackEvents = j.FallbackEvents[1:]
	}
	j.FallbackEvents = append(j.FallbackEvents, event)
}

type Asset struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Kind       string    `json:"kind"`
	Filename   string    `json:"filename"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	StorageKey string    `json:"storage_key"`
	URL        string    `json:"url"`
	ContentURL string    `json:"content_url,omitempty"`
	Checksum   string    `json:"checksum,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type VoiceQualityResult struct {
	DetectedLanguage      string                 `json:"detected_language"`
	STTConfidence         float64                `json:"stt_confidence"`
	TopicRelevanceScore   int                    `json:"topic_relevance_score"`
	KeywordHits           []string               `json:"keyword_hits"`
	TranscriptSuggestions []TranscriptSuggestion `json:"transcript_suggestions,omitempty"`
	Reasons               []string               `json:"reasons"`
	Status                string                 `json:"status"`
}

type TranscriptSuggestion struct {
	Original  string `json:"original"`
	Suggested string `json:"suggested"`
	Reason    string `json:"reason,omitempty"`
}

type InterviewAnswerValidation struct {
	Valid   bool               `json:"valid"`
	Message string             `json:"message,omitempty"`
	Quality VoiceQualityResult `json:"quality"`
}

type PromptTemplate struct {
	Name         string    `json:"name"`
	Task         string    `json:"task"`
	Default      string    `json:"default"`
	Content      string    `json:"content"`
	RenderEngine string    `json:"render_engine,omitempty"`
	UpdatedBy    string    `json:"updated_by,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	IsModified   bool      `json:"is_modified"`
	Validator    string    `json:"validator"`
}

type AIConfig struct {
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	BaseURL       string    `json:"base_url,omitempty"`
	Temperature   float64   `json:"temperature"`
	TopP          float64   `json:"top_p"`
	TopK          int       `json:"top_k"`
	MaxTokens     int       `json:"max_tokens"`
	StreamEnabled bool      `json:"stream_enabled"`
	FallbackModel string    `json:"fallback_model"`
	UpdatedBy     string    `json:"updated_by,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SensitiveFinding struct {
	Type            string  `json:"type"`
	Field           string  `json:"field"`
	Excerpt         string  `json:"excerpt"`
	Severity        string  `json:"severity"`
	Suggestion      string  `json:"suggestion"`
	Source          string  `json:"source,omitempty"`
	Confidence      float64 `json:"confidence,omitempty"`
	RedactedExcerpt string  `json:"redacted_excerpt,omitempty"`
}

type SensitiveCheckResult struct {
	Status       string             `json:"status"`
	Sanitized    bool               `json:"sanitized"`
	Findings     []SensitiveFinding `json:"findings"`
	CheckedAt    time.Time          `json:"checked_at"`
	Source       string             `json:"source,omitempty"`
	RiskLevel    string             `json:"risk_level,omitempty"`
	Blocked      bool               `json:"blocked,omitempty"`
	FallbackUsed bool               `json:"fallback_used,omitempty"`
	Summary      string             `json:"summary,omitempty"`
}

type AuditEvent struct {
	ID           string            `json:"id"`
	ActorID      string            `json:"actor_id,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	IPAddress    string            `json:"ip_address,omitempty"`
	UserAgent    string            `json:"user_agent,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"created_at"`
}
