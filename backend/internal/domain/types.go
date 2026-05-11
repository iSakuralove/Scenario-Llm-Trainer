package domain

import "time"

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
	Role         string      `json:"role"`
	Profile      UserProfile `json:"profile"`
	CreatedAt    time.Time   `json:"created_at"`
}

type UserProfile struct {
	TargetLevel      string         `json:"target_level"`
	PreferredDomains []string       `json:"preferred_domains"`
	CapabilityRadar  map[string]int `json:"capability_radar"`
	WeakPoints       []WeakPoint    `json:"weak_points"`
	TotalStats       TotalStats     `json:"total_stats"`
	CheckinDates     []string       `json:"checkin_dates,omitempty"`
	LastCheckinDate  string         `json:"last_checkin_date,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at"`
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
	ID                  string              `json:"id"`
	UserID              string              `json:"user_id"`
	QuestionID          string              `json:"question_id"`
	Status              string              `json:"status"`
	CurrentTurn         int                 `json:"current_turn"`
	MaxTurns            int                 `json:"max_turns"`
	RevealedClueIDs     []string            `json:"revealed_clue_ids"`
	UserAnswer          string              `json:"user_answer,omitempty"`
	EvaluationResult    *ScenarioEvaluation `json:"evaluation_result,omitempty"`
	Score               *ScenarioScore      `json:"score,omitempty"`
	QuestionSnapshot    ScenarioQuestion    `json:"question_snapshot"`
	HintLevel           int                 `json:"hint_level"`
	NoNewClueStreak     int                 `json:"no_new_clue_streak"`
	ConversationSummary string              `json:"conversation_summary,omitempty"`
	StartedAt           time.Time           `json:"started_at"`
	LastActiveAt        time.Time           `json:"last_active_at"`
	EndedAt             *time.Time          `json:"ended_at,omitempty"`
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
	ResponseType          string      `json:"response_type"`
	RevealedClueID        string      `json:"revealed_clue_id,omitempty"`
	HintLevel             int         `json:"hint_level"`
	IsAnswerLeak          bool        `json:"is_answer_leak"`
	IsDistractor          bool        `json:"is_distractor"`
	IsSanitized           bool        `json:"is_sanitized"`
	Provider              string      `json:"provider,omitempty"`
	Validated             bool        `json:"validated,omitempty"`
	FallbackUsed          bool        `json:"fallback_used,omitempty"`
	SafetyRewritten       bool        `json:"safety_rewritten,omitempty"`
	SemanticDecision      string      `json:"semantic_decision,omitempty"`
	InputQuality          string      `json:"input_quality,omitempty"`
	AgentIntent           string      `json:"agent_intent,omitempty"`
	RootSimilarity        float64     `json:"root_similarity,omitempty"`
	ClueSimilarity        float64     `json:"clue_similarity,omitempty"`
	MatchedClueID         string      `json:"matched_clue_id,omitempty"`
	EmbeddingModel        string      `json:"embedding_model,omitempty"`
	EmbeddingFallbackUsed bool        `json:"embedding_fallback_used,omitempty"`
	AgentTrace            *AgentTrace `json:"agent_trace,omitempty"`
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
	ID               string                `json:"id"`
	UserID           string                `json:"user_id"`
	QuestionID       string                `json:"question_id"`
	Status           string                `json:"status"`
	CurrentRound     int                   `json:"current_round"`
	MaxRounds        int                   `json:"max_rounds"`
	Submissions      []InterviewSubmission `json:"submissions"`
	Evaluations      []InterviewEvaluation `json:"evaluations"`
	FollowUpQuestion string                `json:"follow_up_question,omitempty"`
	FinalScore       int                   `json:"final_score,omitempty"`
	FinalReport      string                `json:"final_report,omitempty"`
	StartedAt        time.Time             `json:"started_at"`
	EndedAt          *time.Time            `json:"ended_at,omitempty"`
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
	Provider         string     `json:"provider,omitempty"`
	Model            string     `json:"model,omitempty"`
	Validated        bool       `json:"validated"`
	FallbackUsed     bool       `json:"fallback_used"`
	ResultQuestionID string     `json:"result_question_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
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
