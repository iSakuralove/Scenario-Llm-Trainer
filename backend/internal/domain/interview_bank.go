package domain

import "time"

const (
	InterviewKnowledgeVersionContentUpdate   = "content_update"
	InterviewKnowledgeVersionDuplicateImport = "duplicate_import"
	InterviewKnowledgeVersionManualEdit      = "manual_edit"
	InterviewKnowledgeVersionArchive         = "archive"
	InterviewKnowledgeVersionRestoreArchived = "restore_archived"
)

func ValidInterviewKnowledgeVersionType(versionType string) bool {
	switch versionType {
	case InterviewKnowledgeVersionContentUpdate,
		InterviewKnowledgeVersionDuplicateImport,
		InterviewKnowledgeVersionManualEdit,
		InterviewKnowledgeVersionArchive,
		InterviewKnowledgeVersionRestoreArchived:
		return true
	default:
		return false
	}
}

type InterviewKnowledgeAtom struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Subject        string     `json:"subject"`
	Domain         string     `json:"domain"`
	Difficulty     string     `json:"difficulty"`
	Category       string     `json:"category"`
	QuestionRole   string     `json:"question_role"`
	SourceRef      string     `json:"source_ref"`
	Tags           []string   `json:"tags"`
	Principles     []string   `json:"principles"`
	Pitfalls       []string   `json:"pitfalls"`
	FollowUpPaths  []string   `json:"follow_up_paths"`
	Status         string     `json:"status"`
	CurrentVersion int        `json:"current_version"`
	VectorStatus   string     `json:"vector_status"`
	LastIndexedAt  *time.Time `json:"last_indexed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type InterviewKnowledgeAtomSnapshot struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Subject       string   `json:"subject"`
	Domain        string   `json:"domain"`
	Difficulty    string   `json:"difficulty"`
	Category      string   `json:"category"`
	QuestionRole  string   `json:"question_role"`
	SourceRef     string   `json:"sourceRef"`
	Tags          []string `json:"tags"`
	Principles    []string `json:"principles"`
	Pitfalls      []string `json:"pitfalls"`
	FollowUpPaths []string `json:"followUpPaths"`
	Status        string   `json:"status"`
}

type InterviewKnowledgeAtomVersion struct {
	ID              string                         `json:"id"`
	AtomID          string                         `json:"atom_id"`
	Version         int                            `json:"version"`
	VersionType     string                         `json:"version_type"`
	AdminID         string                         `json:"admin_id,omitempty"`
	ChangeNote      string                         `json:"change_note,omitempty"`
	Snapshot        InterviewKnowledgeAtomSnapshot `json:"snapshot"`
	DiffSummary     map[string]interface{}         `json:"diff_summary"`
	NoContentChange bool                           `json:"no_content_change"`
	CreatedAt       time.Time                      `json:"created_at"`
}

type InterviewKnowledgeBatch struct {
	ID               string                 `json:"id"`
	SourceRef        string                 `json:"source_ref"`
	Status           string                 `json:"status"`
	Mode             string                 `json:"mode"`
	AtomCount        int                    `json:"atom_count"`
	ValidationReport map[string]interface{} `json:"validation_report"`
	PublishNote      string                 `json:"publish_note,omitempty"`
	AdminID          string                 `json:"admin_id,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type InterviewKnowledgeAtomFilter struct {
	Status       string
	Domain       string
	Difficulty   string
	Category     string
	QuestionRole string
	VectorStatus string
}

type InterviewKnowledgeSummary struct {
	TotalAtoms           int        `json:"total_atoms"`
	PublishedAtoms       int        `json:"published_atoms"`
	DraftAtoms           int        `json:"draft_atoms"`
	ArchivedAtoms        int        `json:"archived_atoms"`
	VectorPendingAtoms   int        `json:"vector_pending_atoms"`
	VectorIndexedAtoms   int        `json:"vector_indexed_atoms"`
	VectorFailedAtoms    int        `json:"vector_failed_atoms"`
	BatchCount           int        `json:"batch_count"`
	OpenCombinationCount int        `json:"open_combination_count"`
	LastImportedAt       *time.Time `json:"last_imported_at,omitempty"`
	LastEditedAt         *time.Time `json:"last_edited_at,omitempty"`
}

const (
	InterviewBankOpsActionTypeFillGap       = "fill_gap"
	InterviewBankOpsActionTypeFixAtom       = "fix_atom"
	InterviewBankOpsActionTypeRebuildIndex  = "rebuild_index"
	InterviewBankOpsActionTypeReviewArchive = "review_archive"
	InterviewBankOpsActionTypeObserve       = "observe"

	InterviewBankOpsActionSourceRetrievalAnalytics = "retrieval_analytics"
	InterviewBankOpsActionSourceRetrievalLog       = "retrieval_log"
	InterviewBankOpsActionSourceHealthDiagnostic   = "health_diagnostic"
	InterviewBankOpsActionSourceIndexStatus        = "index_status"
	InterviewBankOpsActionSourceManual             = "manual"

	InterviewBankOpsActionStatusOpen       = "open"
	InterviewBankOpsActionStatusInProgress = "in_progress"
	InterviewBankOpsActionStatusWatching   = "watching"
	InterviewBankOpsActionStatusResolved   = "resolved"
	InterviewBankOpsActionStatusDismissed  = "dismissed"
	InterviewBankOpsActionStatusReopened   = "reopened"
)

func ValidInterviewBankOpsActionType(value string) bool {
	switch value {
	case InterviewBankOpsActionTypeFillGap,
		InterviewBankOpsActionTypeFixAtom,
		InterviewBankOpsActionTypeRebuildIndex,
		InterviewBankOpsActionTypeReviewArchive,
		InterviewBankOpsActionTypeObserve:
		return true
	default:
		return false
	}
}

func ValidInterviewBankOpsActionSource(value string) bool {
	switch value {
	case InterviewBankOpsActionSourceRetrievalAnalytics,
		InterviewBankOpsActionSourceRetrievalLog,
		InterviewBankOpsActionSourceHealthDiagnostic,
		InterviewBankOpsActionSourceIndexStatus,
		InterviewBankOpsActionSourceManual:
		return true
	default:
		return false
	}
}

func ValidInterviewBankOpsActionStatus(value string) bool {
	switch value {
	case InterviewBankOpsActionStatusOpen,
		InterviewBankOpsActionStatusInProgress,
		InterviewBankOpsActionStatusWatching,
		InterviewBankOpsActionStatusResolved,
		InterviewBankOpsActionStatusDismissed,
		InterviewBankOpsActionStatusReopened:
		return true
	default:
		return false
	}
}

func ValidInterviewBankOpsActionPriority(value string) bool {
	switch value {
	case "P0", "P1", "P2", "P3":
		return true
	default:
		return false
	}
}

type InterviewBankOpsAction struct {
	ID         string                 `json:"id"`
	ActionType string                 `json:"action_type"`
	Status     string                 `json:"status"`
	Priority   string                 `json:"priority"`
	Source     string                 `json:"source"`
	DedupeKey  string                 `json:"dedupe_key"`
	Title      string                 `json:"title"`
	Reason     string                 `json:"reason"`
	Domain     string                 `json:"domain,omitempty"`
	Category   string                 `json:"category,omitempty"`
	Difficulty string                 `json:"difficulty,omitempty"`
	AtomID     string                 `json:"atom_id,omitempty"`
	Evidence   map[string]interface{} `json:"evidence"`
	CreatedBy  string                 `json:"created_by,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

type InterviewBankOpsActionFilter struct {
	Status     string
	ActionType string
	Priority   string
	Source     string
	Domain     string
	Category   string
	Difficulty string
	AtomID     string
	Limit      int
}

type InterviewKnowledgeAtomLightSnapshot struct {
	AtomID   string `json:"atom_id"`
	Version  int    `json:"version"`
	Title    string `json:"title"`
	Subject  string `json:"subject"`
	Domain   string `json:"domain"`
	Category string `json:"category,omitempty"`
}

type InterviewQuestionSnapshot struct {
	ID                   string                `json:"id"`
	Version              int                   `json:"version"`
	Title                string                `json:"title"`
	Subject              string                `json:"subject"`
	Description          string                `json:"description,omitempty"`
	Domain               string                `json:"domain"`
	Difficulty           string                `json:"difficulty"`
	Category             string                `json:"category,omitempty"`
	QuestionRole         string                `json:"question_role"`
	QuestionType         string                `json:"question_type,omitempty"`
	QuestionSource       string                `json:"question_source,omitempty"`
	SourceRef            string                `json:"source_ref,omitempty"`
	Tags                 []string              `json:"tags,omitempty"`
	Principles           []string              `json:"principles,omitempty"`
	Pitfalls             []string              `json:"pitfalls,omitempty"`
	FollowUpPaths        []string              `json:"follow_up_paths,omitempty"`
	ReferenceKeywords    []string              `json:"reference_keywords,omitempty"`
	EvaluationDimensions []EvaluationDimension `json:"evaluation_dimensions,omitempty"`
	FollowUpStrategies   []FollowUpStrategy    `json:"follow_up_strategies,omitempty"`
}

type InterviewRetrievalLog struct {
	ID           string                                `json:"id"`
	SessionID    string                                `json:"session_id"`
	Round        int                                   `json:"round"`
	QueryText    string                                `json:"query_text"`
	MatchedAtoms []InterviewKnowledgeAtomLightSnapshot `json:"matched_atoms"`
	FallbackUsed bool                                  `json:"fallback_used"`
	ErrorMessage string                                `json:"error_message,omitempty"`
	CreatedAt    time.Time                             `json:"created_at"`
}

type InterviewRetrievalLogFilter struct {
	Domain       string
	Category     string
	Difficulty   string
	FallbackUsed *bool
	Limit        int
}

type InterviewRetrievalAtomHit struct {
	AtomID       string     `json:"atom_id"`
	Version      int        `json:"version"`
	Title        string     `json:"title"`
	Subject      string     `json:"subject"`
	Domain       string     `json:"domain"`
	Category     string     `json:"category"`
	Difficulty   string     `json:"difficulty"`
	QuestionRole string     `json:"question_role"`
	HitCount     int        `json:"hit_count"`
	LastHitAt    *time.Time `json:"last_hit_at,omitempty"`
}

type InterviewRetrievalFallbackCombination struct {
	Domain       string     `json:"domain"`
	Category     string     `json:"category"`
	Difficulty   string     `json:"difficulty"`
	Count        int        `json:"count"`
	RecentReason string     `json:"recent_reason,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
}

type InterviewRetrievalAnalytics struct {
	TotalLogs            int                                     `json:"total_logs"`
	HitLogs              int                                     `json:"hit_logs"`
	FallbackLogs         int                                     `json:"fallback_logs"`
	HitRate              float64                                 `json:"hit_rate"`
	FallbackRate         float64                                 `json:"fallback_rate"`
	TopHitAtoms          []InterviewRetrievalAtomHit             `json:"top_hit_atoms"`
	LowHitAtoms          []InterviewRetrievalAtomHit             `json:"low_hit_atoms"`
	FallbackCombinations []InterviewRetrievalFallbackCombination `json:"fallback_combinations"`
	RecentFallbacks      []InterviewRetrievalLog                 `json:"recent_fallbacks"`
}
