package domain

import "time"

const (
	InterviewKnowledgeVersionContentUpdate   = "content_update"
	InterviewKnowledgeVersionDuplicateImport = "duplicate_import"
	InterviewKnowledgeVersionManualEdit      = "manual_edit"
	InterviewKnowledgeVersionRestoreArchived = "restore_archived"
)

func ValidInterviewKnowledgeVersionType(versionType string) bool {
	switch versionType {
	case InterviewKnowledgeVersionContentUpdate,
		InterviewKnowledgeVersionDuplicateImport,
		InterviewKnowledgeVersionManualEdit,
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

type InterviewKnowledgeAtomLightSnapshot struct {
	AtomID   string `json:"atom_id"`
	Version  int    `json:"version"`
	Title    string `json:"title"`
	Subject  string `json:"subject"`
	Domain   string `json:"domain"`
	Category string `json:"category,omitempty"`
}

type InterviewQuestionSnapshot struct {
	ID           string `json:"id"`
	Version      int    `json:"version"`
	Title        string `json:"title"`
	Subject      string `json:"subject"`
	Domain       string `json:"domain"`
	Difficulty   string `json:"difficulty"`
	QuestionRole string `json:"question_role"`
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
