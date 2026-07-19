package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

var interviewKnowledgeAllowedCategories = map[string]bool{
	"java":          true,
	"database":      true,
	"cache":         true,
	"middleware":    true,
	"system_design": true,
	"frontend":      true,
	"ai_llm":        true,
	"hr_soft_skill": true,
}

var interviewKnowledgeAllowedQuestionRoles = map[string]bool{
	"opening":  true,
	"followup": true,
	"mixed":    true,
}

var interviewKnowledgeAllowedQuestionTypes = map[string]bool{
	"principle":       true,
	"troubleshooting": true,
	"architecture":    true,
	"behavioral":      true,
}

var interviewKnowledgeAllowedStatuses = map[string]bool{
	"draft":     true,
	"published": true,
	"archived":  true,
}

var interviewKnowledgeAllowedVectorStatuses = map[string]bool{
	"pending": true,
	"indexed": true,
	"failed":  true,
}

type interviewKnowledgeImportRequest struct {
	BatchID         string
	SourceRef       string
	PublishNote     string
	Domain          string
	Category        string
	Difficulty      string
	QuestionRole    string
	QuestionType    string
	OpeningQuestion string
	StableCode      string
	Status          string
	VectorStatus    string
	Tags            []string
	Items           []interviewKnowledgeImportRawItem
}

type interviewKnowledgeImportRawItem struct {
	Index           int
	Raw             map[string]json.RawMessage
	ID              string
	Title           string
	Subject         string
	Domain          string
	Difficulty      string
	Category        string
	QuestionRole    string
	QuestionType    string
	OpeningQuestion string
	StableCode      string
	SourceRef       string
	Tags            []string
	Principles      []string
	Pitfalls        []string
	FollowUpPaths   []string
	Status          string
	VectorStatus    string
}

type interviewKnowledgeImportItemResult struct {
	Index           int      `json:"index"`
	ID              string   `json:"id,omitempty"`
	Title           string   `json:"title,omitempty"`
	Action          string   `json:"action"`
	ExistingVersion int      `json:"existing_version,omitempty"`
	Errors          []string `json:"errors"`
	Warnings        []string `json:"warnings"`

	Atom        domain.InterviewKnowledgeAtom `json:"-"`
	VersionType string                        `json:"-"`
}

type interviewKnowledgeImportReport struct {
	BatchID   string                               `json:"batch_id"`
	SourceRef string                               `json:"source_ref"`
	Summary   map[string]int                       `json:"summary"`
	Errors    []string                             `json:"errors"`
	Warnings  []string                             `json:"warnings"`
	Results   []interviewKnowledgeImportItemResult `json:"results"`
}

type interviewKnowledgeIndexRebuildRequest struct {
	AtomIDs      []string `json:"atom_ids"`
	VectorStatus string   `json:"vector_status"`
	Limit        int      `json:"limit"`
}

type interviewKnowledgeIndexRebuildResult struct {
	AtomID         string `json:"atom_id"`
	Status         string `json:"status"`
	DocCount       int    `json:"doc_count,omitempty"`
	EmbeddingModel string `json:"embedding_model,omitempty"`
	Error          string `json:"error,omitempty"`
}

type interviewKnowledgeIndexRebuildResponse struct {
	Total   int                                    `json:"total"`
	Indexed int                                    `json:"indexed"`
	Failed  int                                    `json:"failed"`
	Skipped int                                    `json:"skipped"`
	Results []interviewKnowledgeIndexRebuildResult `json:"results"`
}

type interviewKnowledgeAtomUpdateRequest struct {
	BaseVersion     int      `json:"base_version"`
	ChangeNote      string   `json:"change_note"`
	Title           string   `json:"title"`
	Subject         string   `json:"subject"`
	Domain          string   `json:"domain"`
	Difficulty      string   `json:"difficulty"`
	Category        string   `json:"category"`
	QuestionRole    string   `json:"question_role"`
	QuestionType    string   `json:"question_type"`
	OpeningQuestion string   `json:"opening_question"`
	StableCode      string   `json:"stable_code"`
	SourceRef       string   `json:"source_ref"`
	Tags            []string `json:"tags"`
	Principles      []string `json:"principles"`
	Pitfalls        []string `json:"pitfalls"`
	FollowUpPaths   []string `json:"follow_up_paths"`
}

type interviewKnowledgeAtomArchiveRequest struct {
	Reason string `json:"reason"`
}

type interviewBankOpsActionCreateRequest struct {
	ActionType string                 `json:"action_type"`
	Priority   string                 `json:"priority"`
	Title      string                 `json:"title"`
	Reason     string                 `json:"reason"`
	Domain     string                 `json:"domain"`
	Category   string                 `json:"category"`
	Difficulty string                 `json:"difficulty"`
	AtomID     string                 `json:"atom_id"`
	Evidence   map[string]interface{} `json:"evidence"`
}

type interviewBankOpsActionUpdateRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type interviewKnowledgeHealthSummary struct {
	TotalAtoms          int `json:"total_atoms"`
	PublishedAtoms      int `json:"published_atoms"`
	DraftAtoms          int `json:"draft_atoms"`
	ArchivedAtoms       int `json:"archived_atoms"`
	VectorPendingAtoms  int `json:"vector_pending_atoms"`
	VectorIndexedAtoms  int `json:"vector_indexed_atoms"`
	VectorFailedAtoms   int `json:"vector_failed_atoms"`
	OpenCombinations    int `json:"open_combinations"`
	WarningCombinations int `json:"warning_combinations"`
	BlockedCombinations int `json:"blocked_combinations"`
}

type interviewKnowledgeHealthCombination struct {
	Domain               string   `json:"domain"`
	Category             string   `json:"category"`
	Difficulty           string   `json:"difficulty"`
	TotalCount           int      `json:"total_count"`
	PublishedCount       int      `json:"published_count"`
	DraftCount           int      `json:"draft_count"`
	ArchivedCount        int      `json:"archived_count"`
	OpeningCount         int      `json:"opening_count"`
	FollowupCount        int      `json:"followup_count"`
	MixedCount           int      `json:"mixed_count"`
	IndexedFollowupCount int      `json:"indexed_followup_count"`
	PendingCount         int      `json:"pending_count"`
	FailedCount          int      `json:"failed_count"`
	Status               string   `json:"status"`
	Reasons              []string `json:"reasons"`
	Actions              []string `json:"actions"`
}

type interviewKnowledgeHealthResponse struct {
	Summary      interviewKnowledgeHealthSummary       `json:"summary"`
	Combinations []interviewKnowledgeHealthCombination `json:"combinations"`
}

type interviewKnowledgeRetrievalPreviewRequest struct {
	Domain     string `json:"domain"`
	Category   string `json:"category"`
	Difficulty string `json:"difficulty"`
	Query      string `json:"query"`
	Answer     string `json:"answer"`
	Text       string `json:"text"`
	Limit      int    `json:"limit"`
}

type interviewKnowledgeRetrievalPreviewDiagnostics struct {
	Domain               string         `json:"domain"`
	Category             string         `json:"category"`
	Difficulty           string         `json:"difficulty"`
	Query                string         `json:"query"`
	CandidateCount       int            `json:"candidate_count"`
	PublishedCandidates  int            `json:"published_candidates"`
	IndexedCandidates    int            `json:"indexed_candidates"`
	PendingCandidates    int            `json:"pending_candidates"`
	FailedCandidates     int            `json:"failed_candidates"`
	ArchivedCandidates   int            `json:"archived_candidates"`
	VectorStoreAvailable bool           `json:"vector_store_available"`
	EmbeddingAvailable   bool           `json:"embedding_available"`
	SearchLimit          int            `json:"search_limit"`
	FilterCounts         map[string]int `json:"filter_counts"`
}

type interviewKnowledgeRetrievalPreviewResult struct {
	AtomID       string  `json:"atom_id"`
	Version      int     `json:"version"`
	Title        string  `json:"title"`
	Subject      string  `json:"subject"`
	Domain       string  `json:"domain"`
	Category     string  `json:"category"`
	Difficulty   string  `json:"difficulty"`
	QuestionRole string  `json:"question_role"`
	Score        float64 `json:"score"`
	DocType      string  `json:"doc_type"`
	DocKey       string  `json:"doc_key"`
	Snippet      string  `json:"snippet"`
}

type interviewKnowledgeRetrievalPreviewResponse struct {
	MatchedCount   int                                           `json:"matched_count"`
	FallbackUsed   bool                                          `json:"fallback_used"`
	FallbackReason string                                        `json:"fallback_reason,omitempty"`
	Results        []interviewKnowledgeRetrievalPreviewResult    `json:"results"`
	Diagnostics    interviewKnowledgeRetrievalPreviewDiagnostics `json:"diagnostics"`
}

const interviewKnowledgeExpectedEmbeddingDim = 1536

func (s *Server) handleAdminInterviewBank(w http.ResponseWriter, r *http.Request, user *domain.User, parts []string) {
	if len(parts) == 1 && parts[0] == "summary" && r.Method == http.MethodGet {
		writeOK(w, s.store.InterviewKnowledgeSummary())
		return
	}
	if len(parts) == 1 && parts[0] == "health" && r.Method == http.MethodGet {
		writeOK(w, s.interviewKnowledgeHealth())
		return
	}
	if len(parts) == 1 && parts[0] == "atoms" && r.Method == http.MethodGet {
		filter := domain.InterviewKnowledgeAtomFilter{
			Status:       r.URL.Query().Get("status"),
			Domain:       r.URL.Query().Get("domain"),
			Difficulty:   r.URL.Query().Get("difficulty"),
			Category:     r.URL.Query().Get("category"),
			QuestionRole: r.URL.Query().Get("question_role"),
			VectorStatus: r.URL.Query().Get("vector_status"),
		}
		items := s.store.ListInterviewKnowledgeAtoms(filter)
		writeOK(w, map[string]interface{}{
			"list":    paginate(items, r),
			"total":   len(items),
			"filters": filter,
		})
		return
	}
	if len(parts) == 2 && parts[0] == "atoms" && r.Method == http.MethodGet {
		atom, ok := s.store.GetInterviewKnowledgeAtom(parts[1])
		if !ok {
			writeError(w, http.StatusNotFound, "interview knowledge atom not found")
			return
		}
		writeOK(w, map[string]interface{}{"atom": atom})
		return
	}
	if len(parts) == 3 && parts[0] == "atoms" && parts[2] == "versions" && r.Method == http.MethodGet {
		if _, ok := s.store.GetInterviewKnowledgeAtom(parts[1]); !ok {
			writeError(w, http.StatusNotFound, "interview knowledge atom not found")
			return
		}
		writeOK(w, map[string]interface{}{"list": s.store.ListInterviewKnowledgeAtomVersions(parts[1])})
		return
	}
	if len(parts) == 2 && parts[0] == "atoms" && r.Method == http.MethodPatch {
		var req interviewKnowledgeAtomUpdateRequest
		if !decode(w, r, &req) {
			return
		}
		atom, version, err := s.updateInterviewKnowledgeAtom(parts[1], req, user)
		if err != nil {
			status := http.StatusBadRequest
			if err.Error() == "interview knowledge atom not found" {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_atom_edit", "interview_knowledge_atom", atom.ID, map[string]string{
			"version":       strconv.Itoa(atom.CurrentVersion),
			"vector_status": atom.VectorStatus,
			"no_change":     strconv.FormatBool(version.NoContentChange),
		})
		writeOK(w, map[string]interface{}{"atom": atom, "version": version})
		return
	}
	if len(parts) == 3 && parts[0] == "atoms" && parts[2] == "archive" && r.Method == http.MethodPost {
		var req interviewKnowledgeAtomArchiveRequest
		if !decode(w, r, &req) {
			return
		}
		atom, version, err := s.archiveInterviewKnowledgeAtom(r.Context(), parts[1], req, user)
		if err != nil {
			status := http.StatusBadRequest
			if err.Error() == "interview knowledge atom not found" {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_atom_archive", "interview_knowledge_atom", atom.ID, map[string]string{
			"version":       strconv.Itoa(atom.CurrentVersion),
			"vector_status": atom.VectorStatus,
		})
		writeOK(w, map[string]interface{}{"atom": atom, "version": version})
		return
	}
	if len(parts) == 3 && parts[0] == "atoms" && parts[2] == "restore" && r.Method == http.MethodPost {
		atom, version, err := s.restoreInterviewKnowledgeAtom(parts[1], user)
		if err != nil {
			status := http.StatusBadRequest
			if err.Error() == "interview knowledge atom not found" {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_atom_restore", "interview_knowledge_atom", atom.ID, map[string]string{
			"version":       strconv.Itoa(atom.CurrentVersion),
			"vector_status": atom.VectorStatus,
		})
		writeOK(w, map[string]interface{}{"atom": atom, "version": version})
		return
	}
	if len(parts) == 1 && parts[0] == "batches" && r.Method == http.MethodGet {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 30
		}
		if limit > 100 {
			limit = 100
		}
		writeOK(w, map[string]interface{}{"list": s.store.ListInterviewKnowledgeBatches(limit)})
		return
	}
	if len(parts) == 2 && parts[0] == "index" && parts[1] == "rebuild" && r.Method == http.MethodPost {
		var req interviewKnowledgeIndexRebuildRequest
		if !decode(w, r, &req) {
			return
		}
		response, err := s.rebuildInterviewKnowledgeIndex(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_index_rebuild", "interview_knowledge_atom", "bulk", map[string]string{
			"total":   strconv.Itoa(response.Total),
			"indexed": strconv.Itoa(response.Indexed),
			"failed":  strconv.Itoa(response.Failed),
			"skipped": strconv.Itoa(response.Skipped),
		})
		writeOK(w, response)
		return
	}
	if len(parts) == 1 && parts[0] == "ops-actions" && r.Method == http.MethodGet {
		filter := parseInterviewBankOpsActionFilter(r)
		items := s.store.ListInterviewBankOpsActions(filter)
		writeOK(w, map[string]interface{}{"list": items, "total": len(items), "filters": filter})
		return
	}
	if len(parts) == 2 && parts[0] == "ops-actions" && r.Method == http.MethodGet {
		detail, ok := s.interviewBankOpsActionDetail(parts[1])
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeOK(w, detail)
		return
	}
	if len(parts) == 2 && parts[0] == "ops-actions" && r.Method == http.MethodPatch {
		var req interviewBankOpsActionUpdateRequest
		if !decode(w, r, &req) {
			return
		}
		action, historyEntry, err := s.store.UpdateInterviewBankOpsActionStatus(parts[1], req.Status, req.Note, user.ID)
		if err != nil {
			status := http.StatusBadRequest
			if err.Error() == "interview bank ops action not found" {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_ops_action_status_update", "interview_bank_ops_action", action.ID, map[string]string{
			"from_status": historyEntry.FromStatus,
			"to_status":   historyEntry.ToStatus,
		})
		writeOK(w, map[string]interface{}{"action": action, "history_entry": historyEntry})
		return
	}
	if len(parts) == 3 && parts[0] == "ops-actions" && parts[1] == "candidates" && parts[2] == "save" && r.Method == http.MethodPost {
		var req domain.InterviewBankOpsActionCandidateSaveRequest
		if !decode(w, r, &req) {
			return
		}
		response, err := s.saveInterviewBankOpsActionCandidates(req, user)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_ops_action_candidate_save", "interview_bank_ops_action", "bulk", map[string]string{
			"saved":            strconv.Itoa(response.Saved),
			"skipped_existing": strconv.Itoa(response.SkippedExisting),
		})
		writeOK(w, response)
		return
	}
	if len(parts) == 2 && parts[0] == "ops-actions" && parts[1] == "candidates" && r.Method == http.MethodPost {
		var req domain.InterviewBankOpsActionCandidateRequest
		if !decode(w, r, &req) {
			return
		}
		response, err := s.generateInterviewBankOpsActionCandidates(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, response)
		return
	}
	if len(parts) == 1 && parts[0] == "ops-actions" && r.Method == http.MethodPost {
		var req interviewBankOpsActionCreateRequest
		if !decode(w, r, &req) {
			return
		}
		action, err := s.store.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: req.ActionType,
			Priority:   req.Priority,
			Source:     domain.InterviewBankOpsActionSourceManual,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Title:      req.Title,
			Reason:     req.Reason,
			Domain:     req.Domain,
			Category:   req.Category,
			Difficulty: req.Difficulty,
			AtomID:     req.AtomID,
			Evidence:   req.Evidence,
			CreatedBy:  user.ID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, user, "admin.interview_bank_ops_action_create", "interview_bank_ops_action", action.ID, map[string]string{
			"action_type": action.ActionType,
			"priority":    action.Priority,
			"source":      action.Source,
		})
		writeOK(w, map[string]interface{}{"action": action})
		return
	}
	if len(parts) == 1 && parts[0] == "retrieval-logs" && r.Method == http.MethodGet {
		filter, err := parseInterviewRetrievalLogFilter(r, 50, 200)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		items := s.store.ListInterviewRetrievalLogs(filter)
		writeOK(w, map[string]interface{}{"list": items, "total": len(items)})
		return
	}
	if len(parts) == 1 && parts[0] == "retrieval-analytics" && r.Method == http.MethodGet {
		filter, err := parseInterviewRetrievalLogFilter(r, 500, 1000)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, s.store.InterviewRetrievalAnalytics(filter))
		return
	}
	if len(parts) == 1 && parts[0] == "retrieval-preview" && r.Method == http.MethodPost {
		var req interviewKnowledgeRetrievalPreviewRequest
		if !decode(w, r, &req) {
			return
		}
		response, err := s.previewInterviewKnowledgeRetrieval(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, response)
		return
	}
	if len(parts) == 2 && parts[0] == "import" && parts[1] == "validate" && r.Method == http.MethodPost {
		req, ok := decodeInterviewKnowledgeImportRequest(w, r)
		if !ok {
			return
		}
		report := s.previewInterviewKnowledgeImport(req)
		writeOK(w, report)
		return
	}
	if len(parts) == 2 && parts[0] == "import" && parts[1] == "publish" && r.Method == http.MethodPost {
		req, ok := decodeInterviewKnowledgeImportRequest(w, r)
		if !ok {
			return
		}
		report := s.previewInterviewKnowledgeImport(req)
		if report.Summary["error_count"] > 0 {
			writeErrorWithData(w, http.StatusBadRequest, "interview knowledge import validation failed", report)
			return
		}
		published := 0
		for i := range report.Results {
			result := &report.Results[i]
			saved, _, err := s.store.SaveInterviewKnowledgeAtomVersioned(result.Atom, result.VersionType, user.ID, "admin import")
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				report.Errors = append(report.Errors, fmt.Sprintf("items[%d]: %s", result.Index, err.Error()))
				continue
			}
			result.Atom = saved
			published++
		}
		report.Summary["published_count"] = published
		status := "published"
		if published != len(report.Results) {
			status = "partially_published"
		}
		batch := s.store.SaveInterviewKnowledgeBatch(domain.InterviewKnowledgeBatch{
			ID:               report.BatchID,
			SourceRef:        report.SourceRef,
			Status:           status,
			Mode:             "publish",
			AtomCount:        published,
			ValidationReport: interviewKnowledgeReportMap(report),
			PublishNote:      req.PublishNote,
			AdminID:          user.ID,
		})
		s.audit(r, user, "admin.interview_bank_import_publish", "interview_knowledge_batch", batch.ID, map[string]string{
			"atom_count":        strconv.Itoa(published),
			"duplicate_count":   strconv.Itoa(report.Summary["duplicate_count"]),
			"vector_rebuild":    "false",
			"vector_failed_mvp": "filter_only",
		})
		writeOK(w, map[string]interface{}{"report": report, "batch": batch})
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func parseInterviewRetrievalLogFilter(r *http.Request, defaultLimit, maxLimit int) (domain.InterviewRetrievalLogFilter, error) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	filter := domain.InterviewRetrievalLogFilter{
		Domain:     r.URL.Query().Get("domain"),
		Category:   r.URL.Query().Get("category"),
		Difficulty: r.URL.Query().Get("difficulty"),
		Limit:      limit,
	}
	fallbackValue := strings.TrimSpace(r.URL.Query().Get("fallback_used"))
	if fallbackValue != "" {
		parsed, err := strconv.ParseBool(fallbackValue)
		if err != nil {
			return domain.InterviewRetrievalLogFilter{}, fmt.Errorf("fallback_used must be true or false")
		}
		filter.FallbackUsed = &parsed
	}
	return filter, nil
}

func parseInterviewBankOpsActionFilter(r *http.Request) domain.InterviewBankOpsActionFilter {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	actionType := r.URL.Query().Get("type")
	if actionType == "" {
		actionType = r.URL.Query().Get("action_type")
	}
	return domain.InterviewBankOpsActionFilter{
		Status:     r.URL.Query().Get("status"),
		ActionType: actionType,
		Priority:   r.URL.Query().Get("priority"),
		Source:     r.URL.Query().Get("source"),
		Domain:     r.URL.Query().Get("domain"),
		Category:   r.URL.Query().Get("category"),
		Difficulty: r.URL.Query().Get("difficulty"),
		AtomID:     r.URL.Query().Get("atom_id"),
		Limit:      limit,
	}
}

func (s *Server) generateInterviewBankOpsActionCandidates(req domain.InterviewBankOpsActionCandidateRequest) (domain.InterviewBankOpsActionCandidateResponse, error) {
	policy, err := normalizeInterviewBankOpsActionCandidatePolicy(req)
	if err != nil {
		return domain.InterviewBankOpsActionCandidateResponse{}, err
	}
	response := domain.InterviewBankOpsActionCandidateResponse{
		List:   []domain.InterviewBankOpsActionCandidate{},
		Policy: policy,
	}
	seen := map[string]bool{}
	active := s.activeInterviewBankOpsActionKeys()
	add := func(candidate domain.InterviewBankOpsActionCandidate) {
		candidate.DedupeKey = strings.TrimSpace(candidate.DedupeKey)
		if candidate.DedupeKey == "" || seen[candidate.DedupeKey] {
			return
		}
		seen[candidate.DedupeKey] = true
		if active[candidate.DedupeKey] {
			response.SkippedExisting++
			return
		}
		if candidate.CandidateKey == "" {
			candidate.CandidateKey = candidate.Source + "|" + candidate.DedupeKey
		}
		if candidate.Evidence == nil {
			candidate.Evidence = map[string]interface{}{}
		}
		response.List = append(response.List, candidate)
	}
	for _, source := range policy.Sources {
		switch source {
		case domain.InterviewBankOpsActionSourceHealthDiagnostic:
			for _, combo := range s.interviewKnowledgeHealth().Combinations {
				if !interviewKnowledgeHealthCombinationMatchesCandidateRequest(combo, req) {
					continue
				}
				actionType := ""
				priority := ""
				title := ""
				switch combo.Status {
				case "blocked":
					actionType = domain.InterviewBankOpsActionTypeFillGap
					priority = "P0"
					title = fmt.Sprintf("补齐 %s/%s/%s 题库资源", combo.Domain, combo.Category, combo.Difficulty)
				case "warning":
					actionType = domain.InterviewBankOpsActionTypeRebuildIndex
					priority = "P2"
					if combo.FailedCount > 0 {
						priority = "P1"
					}
					title = fmt.Sprintf("重建 %s/%s/%s 组合题库索引", combo.Domain, combo.Category, combo.Difficulty)
				default:
					continue
				}
				action := domain.InterviewBankOpsAction{
					ActionType: actionType,
					Domain:     combo.Domain,
					Category:   combo.Category,
					Difficulty: combo.Difficulty,
				}
				add(domain.InterviewBankOpsActionCandidate{
					ActionType: actionType,
					Priority:   priority,
					Source:     domain.InterviewBankOpsActionSourceHealthDiagnostic,
					DedupeKey:  domain.InterviewBankOpsActionDedupeKey(action),
					Title:      title,
					Reason:     strings.Join(combo.Reasons, "；"),
					Domain:     combo.Domain,
					Category:   combo.Category,
					Difficulty: combo.Difficulty,
					Evidence: map[string]interface{}{
						"status":                 combo.Status,
						"reasons":                append([]string{}, combo.Reasons...),
						"actions":                append([]string{}, combo.Actions...),
						"opening_count":          combo.OpeningCount,
						"followup_count":         combo.FollowupCount,
						"indexed_followup_count": combo.IndexedFollowupCount,
						"published_count":        combo.PublishedCount,
					},
				})
			}
		case domain.InterviewBankOpsActionSourceIndexStatus:
			for _, atom := range s.store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{}) {
				if !interviewKnowledgeAtomMatchesCandidateRequest(atom, req) {
					continue
				}
				if strings.TrimSpace(atom.Status) != "published" {
					continue
				}
				vectorStatus := strings.TrimSpace(atom.VectorStatus)
				priority := ""
				switch vectorStatus {
				case "failed":
					priority = "P1"
				case "pending":
					priority = "P2"
				default:
					continue
				}
				action := domain.InterviewBankOpsAction{
					ActionType: domain.InterviewBankOpsActionTypeRebuildIndex,
					AtomID:     strings.TrimSpace(atom.ID),
				}
				add(domain.InterviewBankOpsActionCandidate{
					ActionType: domain.InterviewBankOpsActionTypeRebuildIndex,
					Priority:   priority,
					Source:     domain.InterviewBankOpsActionSourceIndexStatus,
					DedupeKey:  domain.InterviewBankOpsActionDedupeKey(action),
					Title:      "重建题库索引：" + strings.TrimSpace(atom.Title),
					Reason:     fmt.Sprintf("已发布题目索引状态为 %s，可能影响后续追问检索。", vectorStatus),
					Domain:     strings.TrimSpace(atom.Domain),
					Category:   strings.TrimSpace(atom.Category),
					Difficulty: strings.ToUpper(strings.TrimSpace(atom.Difficulty)),
					AtomID:     strings.TrimSpace(atom.ID),
					Evidence: map[string]interface{}{
						"atom_id":         strings.TrimSpace(atom.ID),
						"title":           strings.TrimSpace(atom.Title),
						"subject":         strings.TrimSpace(atom.Subject),
						"domain":          strings.TrimSpace(atom.Domain),
						"category":        strings.TrimSpace(atom.Category),
						"difficulty":      strings.ToUpper(strings.TrimSpace(atom.Difficulty)),
						"question_role":   strings.TrimSpace(atom.QuestionRole),
						"vector_status":   vectorStatus,
						"current_version": atom.CurrentVersion,
					},
				})
			}
		case domain.InterviewBankOpsActionSourceRetrievalAnalytics:
			analytics := s.store.InterviewRetrievalAnalytics(domain.InterviewRetrievalLogFilter{
				Domain:     strings.TrimSpace(req.Domain),
				Category:   strings.TrimSpace(req.Category),
				Difficulty: strings.ToUpper(strings.TrimSpace(req.Difficulty)),
				Limit:      policy.Limit,
			})
			for _, combo := range analytics.FallbackCombinations {
				domainName := strings.TrimSpace(combo.Domain)
				category := strings.TrimSpace(combo.Category)
				difficulty := strings.ToUpper(strings.TrimSpace(combo.Difficulty))
				if domainName == "" || category == "" || difficulty == "" {
					continue
				}
				priority := "P1"
				if combo.Count >= 3 {
					priority = "P0"
				}
				reason := fmt.Sprintf("真实面试检索在该组合回退 %d 次，需要补齐可命中的追问资源。", combo.Count)
				if recentReason := truncateText(combo.RecentReason, 160); recentReason != "" {
					reason += " 最近原因：" + recentReason
				}
				action := domain.InterviewBankOpsAction{
					ActionType: domain.InterviewBankOpsActionTypeFillGap,
					Domain:     domainName,
					Category:   category,
					Difficulty: difficulty,
				}
				add(domain.InterviewBankOpsActionCandidate{
					ActionType: domain.InterviewBankOpsActionTypeFillGap,
					Priority:   priority,
					Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
					DedupeKey:  domain.InterviewBankOpsActionDedupeKey(action),
					Title:      fmt.Sprintf("补齐真实回退组合 %s/%s/%s 题库资源", domainName, category, difficulty),
					Reason:     reason,
					Domain:     domainName,
					Category:   category,
					Difficulty: difficulty,
					Evidence: map[string]interface{}{
						"fallback_count":              combo.Count,
						"recent_reason":               truncateText(combo.RecentReason, 160),
						"last_seen_at":                combo.LastSeenAt,
						"analytics_window_total_logs": analytics.TotalLogs,
						"fallback_rate":               analytics.FallbackRate,
					},
				})
			}
			for _, hit := range analytics.LowHitAtoms {
				if hit.HitCount != 0 {
					continue
				}
				atomID := strings.TrimSpace(hit.AtomID)
				if atomID == "" {
					continue
				}
				action := domain.InterviewBankOpsAction{
					ActionType: domain.InterviewBankOpsActionTypeObserve,
					AtomID:     atomID,
				}
				domainName := strings.TrimSpace(hit.Domain)
				category := strings.TrimSpace(hit.Category)
				difficulty := strings.ToUpper(strings.TrimSpace(hit.Difficulty))
				add(domain.InterviewBankOpsActionCandidate{
					ActionType: domain.InterviewBankOpsActionTypeObserve,
					Priority:   "P3",
					Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
					DedupeKey:  domain.InterviewBankOpsActionDedupeKey(action),
					Title:      "观察真实检索零命中题目：" + strings.TrimSpace(hit.Title),
					Reason:     "真实检索窗口内该已发布追问资源暂无命中，先记录观察，不自动归档或改题。",
					Domain:     domainName,
					Category:   category,
					Difficulty: difficulty,
					AtomID:     atomID,
					Evidence: map[string]interface{}{
						"atom_id":                     atomID,
						"title":                       strings.TrimSpace(hit.Title),
						"subject":                     strings.TrimSpace(hit.Subject),
						"domain":                      domainName,
						"category":                    category,
						"difficulty":                  difficulty,
						"question_role":               strings.TrimSpace(hit.QuestionRole),
						"hit_count":                   hit.HitCount,
						"last_hit_at":                 hit.LastHitAt,
						"analytics_window_total_logs": analytics.TotalLogs,
					},
				})
			}
		}
	}
	if len(response.List) > policy.Limit {
		response.List = response.List[:policy.Limit]
	}
	response.Total = len(response.List)
	return response, nil
}

func (s *Server) saveInterviewBankOpsActionCandidates(req domain.InterviewBankOpsActionCandidateSaveRequest, user *domain.User) (domain.InterviewBankOpsActionCandidateSaveResponse, error) {
	if len(req.Candidates) == 0 {
		return domain.InterviewBankOpsActionCandidateSaveResponse{}, fmt.Errorf("candidates is required")
	}
	if len(req.Candidates) > 50 {
		return domain.InterviewBankOpsActionCandidateSaveResponse{}, fmt.Errorf("candidates limit exceeded")
	}
	response := domain.InterviewBankOpsActionCandidateSaveResponse{
		List: []domain.InterviewBankOpsAction{},
	}
	active := s.activeInterviewBankOpsActionKeys()
	seen := map[string]bool{}
	actions := []domain.InterviewBankOpsAction{}
	for _, candidate := range req.Candidates {
		action, err := interviewBankOpsActionFromCandidate(candidate, user.ID)
		if err != nil {
			return domain.InterviewBankOpsActionCandidateSaveResponse{}, err
		}
		if active[action.DedupeKey] || seen[action.DedupeKey] {
			response.SkippedExisting++
			continue
		}
		seen[action.DedupeKey] = true
		actions = append(actions, action)
	}
	for _, action := range actions {
		saved, err := s.store.CreateInterviewBankOpsAction(action)
		if err != nil {
			return domain.InterviewBankOpsActionCandidateSaveResponse{}, err
		}
		response.List = append(response.List, saved)
	}
	response.Saved = len(response.List)
	response.Total = len(response.List)
	return response, nil
}

func interviewBankOpsActionFromCandidate(candidate domain.InterviewBankOpsActionCandidate, adminID string) (domain.InterviewBankOpsAction, error) {
	source := strings.TrimSpace(candidate.Source)
	switch source {
	case domain.InterviewBankOpsActionSourceHealthDiagnostic,
		domain.InterviewBankOpsActionSourceIndexStatus,
		domain.InterviewBankOpsActionSourceRetrievalAnalytics:
	default:
		return domain.InterviewBankOpsAction{}, fmt.Errorf("candidate source is invalid")
	}
	dedupeKey := strings.TrimSpace(candidate.DedupeKey)
	if dedupeKey == "" {
		return domain.InterviewBankOpsAction{}, fmt.Errorf("candidate dedupe_key is required")
	}
	actionType := strings.TrimSpace(candidate.ActionType)
	if !domain.ValidInterviewBankOpsActionType(actionType) {
		return domain.InterviewBankOpsAction{}, fmt.Errorf("invalid action_type")
	}
	priority := strings.ToUpper(strings.TrimSpace(candidate.Priority))
	if !domain.ValidInterviewBankOpsActionPriority(priority) {
		return domain.InterviewBankOpsAction{}, fmt.Errorf("invalid priority")
	}
	title := strings.TrimSpace(candidate.Title)
	if title == "" {
		return domain.InterviewBankOpsAction{}, fmt.Errorf("title is required")
	}
	reason := strings.TrimSpace(candidate.Reason)
	if reason == "" {
		return domain.InterviewBankOpsAction{}, fmt.Errorf("reason is required")
	}
	action := domain.InterviewBankOpsAction{
		ActionType: actionType,
		Status:     domain.InterviewBankOpsActionStatusOpen,
		Priority:   priority,
		Source:     source,
		DedupeKey:  dedupeKey,
		Title:      title,
		Reason:     reason,
		Domain:     strings.TrimSpace(candidate.Domain),
		Category:   strings.TrimSpace(candidate.Category),
		Difficulty: strings.ToUpper(strings.TrimSpace(candidate.Difficulty)),
		AtomID:     strings.TrimSpace(candidate.AtomID),
		Evidence:   candidate.Evidence,
		CreatedBy:  strings.TrimSpace(adminID),
	}
	if action.AtomID == "" && (action.Domain == "" || action.Category == "" || action.Difficulty == "") {
		return domain.InterviewBankOpsAction{}, fmt.Errorf("target scope is required")
	}
	if action.Evidence == nil {
		action.Evidence = map[string]interface{}{}
	}
	return action, nil
}

func normalizeInterviewBankOpsActionCandidatePolicy(req domain.InterviewBankOpsActionCandidateRequest) (domain.InterviewBankOpsActionCandidatePolicy, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	sources := []string{}
	if len(req.Sources) == 0 {
		sources = []string{domain.InterviewBankOpsActionSourceHealthDiagnostic, domain.InterviewBankOpsActionSourceIndexStatus, domain.InterviewBankOpsActionSourceRetrievalAnalytics}
	} else {
		seen := map[string]bool{}
		for _, source := range req.Sources {
			source = strings.TrimSpace(source)
			if source == "" || seen[source] {
				continue
			}
			if source != domain.InterviewBankOpsActionSourceHealthDiagnostic && source != domain.InterviewBankOpsActionSourceIndexStatus && source != domain.InterviewBankOpsActionSourceRetrievalAnalytics {
				return domain.InterviewBankOpsActionCandidatePolicy{}, fmt.Errorf("source is invalid")
			}
			seen[source] = true
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return domain.InterviewBankOpsActionCandidatePolicy{}, fmt.Errorf("source is required")
	}
	return domain.InterviewBankOpsActionCandidatePolicy{Sources: sources, Limit: limit}, nil
}

func (s *Server) activeInterviewBankOpsActionKeys() map[string]bool {
	active := map[string]bool{}
	for _, status := range []string{
		domain.InterviewBankOpsActionStatusOpen,
		domain.InterviewBankOpsActionStatusInProgress,
		domain.InterviewBankOpsActionStatusWatching,
		domain.InterviewBankOpsActionStatusReopened,
	} {
		for _, action := range s.store.ListInterviewBankOpsActions(domain.InterviewBankOpsActionFilter{Status: status, Limit: 200}) {
			if strings.TrimSpace(action.DedupeKey) != "" {
				active[action.DedupeKey] = true
			}
		}
	}
	return active
}

func (s *Server) interviewBankOpsActionDetail(actionID string) (domain.InterviewBankOpsActionDetail, bool) {
	action, ok := s.store.GetInterviewBankOpsAction(actionID)
	if !ok || action == nil {
		return domain.InterviewBankOpsActionDetail{}, false
	}
	detail := domain.InterviewBankOpsActionDetail{
		Action:      *action,
		History:     s.store.ListInterviewBankOpsActionHistory(actionID),
		StaleReason: "",
	}
	if strings.TrimSpace(action.AtomID) == "" {
		return detail, true
	}
	atom, ok := s.store.GetInterviewKnowledgeAtom(action.AtomID)
	if !ok || atom == nil {
		detail.Stale = true
		detail.StaleReason = "关联 atom 不存在"
		return detail, true
	}
	detail.AtomContext = &domain.InterviewBankOpsActionAtomContext{
		ID:             atom.ID,
		Title:          atom.Title,
		Status:         atom.Status,
		VectorStatus:   atom.VectorStatus,
		CurrentVersion: atom.CurrentVersion,
		UpdatedAt:      atom.UpdatedAt,
	}
	if strings.TrimSpace(atom.Status) == "archived" {
		detail.Stale = true
		detail.StaleReason = "关联 atom 已归档"
	}
	return detail, true
}

func interviewKnowledgeHealthCombinationMatchesCandidateRequest(combo interviewKnowledgeHealthCombination, req domain.InterviewBankOpsActionCandidateRequest) bool {
	if req.Domain != "" && !strings.EqualFold(combo.Domain, strings.TrimSpace(req.Domain)) {
		return false
	}
	if req.Category != "" && !strings.EqualFold(combo.Category, strings.TrimSpace(req.Category)) {
		return false
	}
	if req.Difficulty != "" && !strings.EqualFold(combo.Difficulty, strings.ToUpper(strings.TrimSpace(req.Difficulty))) {
		return false
	}
	return true
}

func interviewKnowledgeAtomMatchesCandidateRequest(atom domain.InterviewKnowledgeAtom, req domain.InterviewBankOpsActionCandidateRequest) bool {
	if req.Domain != "" && !strings.EqualFold(strings.TrimSpace(atom.Domain), strings.TrimSpace(req.Domain)) {
		return false
	}
	if req.Category != "" && !strings.EqualFold(strings.TrimSpace(atom.Category), strings.TrimSpace(req.Category)) {
		return false
	}
	if req.Difficulty != "" && !strings.EqualFold(strings.TrimSpace(atom.Difficulty), strings.ToUpper(strings.TrimSpace(req.Difficulty))) {
		return false
	}
	return true
}

func (s *Server) updateInterviewKnowledgeAtom(atomID string, req interviewKnowledgeAtomUpdateRequest, user *domain.User) (domain.InterviewKnowledgeAtom, domain.InterviewKnowledgeAtomVersion, error) {
	changeNote := strings.TrimSpace(req.ChangeNote)
	if req.BaseVersion <= 0 {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("base_version is required")
	}
	if changeNote == "" {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("change_note is required")
	}
	current, ok := s.store.GetInterviewKnowledgeAtom(atomID)
	if !ok {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("interview knowledge atom not found")
	}
	if req.BaseVersion != current.CurrentVersion {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("版本已更新，请刷新后重试")
	}

	updated := *current
	updated.Title = strings.TrimSpace(req.Title)
	updated.Subject = strings.TrimSpace(req.Subject)
	updated.Domain = strings.TrimSpace(req.Domain)
	updated.Difficulty = strings.ToUpper(strings.TrimSpace(req.Difficulty))
	updated.Category = strings.TrimSpace(req.Category)
	updated.QuestionRole = strings.TrimSpace(req.QuestionRole)
	updated.QuestionType = strings.TrimSpace(req.QuestionType)
	updated.OpeningQuestion = strings.TrimSpace(req.OpeningQuestion)
	if strings.TrimSpace(current.StableCode) == "" {
		updated.StableCode = strings.ToUpper(strings.TrimSpace(req.StableCode))
	} else {
		updated.StableCode = current.StableCode
	}
	updated.SourceRef = strings.TrimSpace(req.SourceRef)
	updated.Tags = normalizeImportStringList(req.Tags, true)
	updated.Principles = normalizeImportStringList(req.Principles, false)
	updated.Pitfalls = normalizeImportStringList(req.Pitfalls, false)
	updated.FollowUpPaths = normalizeImportStringList(req.FollowUpPaths, false)
	updated.Status = current.Status
	// 内容变更后旧向量不再可信，等待管理员手动重建索引。
	updated.VectorStatus = "pending"

	if errors := validateInterviewKnowledgeAtomFields(updated); len(errors) > 0 {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return s.store.SaveInterviewKnowledgeAtomVersioned(updated, domain.InterviewKnowledgeVersionManualEdit, user.ID, changeNote)
}

func (s *Server) archiveInterviewKnowledgeAtom(ctx context.Context, atomID string, req interviewKnowledgeAtomArchiveRequest, user *domain.User) (domain.InterviewKnowledgeAtom, domain.InterviewKnowledgeAtomVersion, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("reason is required")
	}
	current, ok := s.store.GetInterviewKnowledgeAtom(atomID)
	if !ok {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("interview knowledge atom not found")
	}
	if strings.TrimSpace(current.Status) == "archived" {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("interview knowledge atom is already archived")
	}
	archived := *current
	archived.Status = "archived"
	// 归档题目不再可检索，旧向量会在保存后清理；状态同步改为 pending 避免管理端误判为可用索引。
	archived.VectorStatus = "pending"
	saved, version, err := s.store.SaveInterviewKnowledgeAtomVersioned(archived, domain.InterviewKnowledgeVersionArchive, user.ID, reason)
	if err != nil {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, err
	}
	if vectorStore := interviewKnowledgeVectorStore(s.store); vectorStore != nil {
		_ = vectorStore.DeleteInterviewKnowledgeByAtom(ctx, saved.ID)
	}
	return saved, version, nil
}

func (s *Server) restoreInterviewKnowledgeAtom(atomID string, user *domain.User) (domain.InterviewKnowledgeAtom, domain.InterviewKnowledgeAtomVersion, error) {
	current, ok := s.store.GetInterviewKnowledgeAtom(atomID)
	if !ok {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("interview knowledge atom not found")
	}
	if strings.TrimSpace(current.Status) != "archived" {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("only archived interview knowledge atoms can be restored")
	}
	restored := *current
	restored.Status = "published"
	restored.VectorStatus = "pending"
	if errors := validateInterviewKnowledgeAtomFields(restored); len(errors) > 0 {
		return domain.InterviewKnowledgeAtom{}, domain.InterviewKnowledgeAtomVersion{}, fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return s.store.SaveInterviewKnowledgeAtomVersioned(restored, domain.InterviewKnowledgeVersionRestoreArchived, user.ID, "恢复归档")
}

func (s *Server) interviewKnowledgeHealth() interviewKnowledgeHealthResponse {
	items := s.store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{})
	groups := map[string]*interviewKnowledgeHealthCombination{}
	response := interviewKnowledgeHealthResponse{
		Summary: interviewKnowledgeHealthSummary{TotalAtoms: len(items)},
	}
	for _, atom := range items {
		response.Summary.TotalAtoms = len(items)
		switch strings.TrimSpace(atom.Status) {
		case "published":
			response.Summary.PublishedAtoms++
		case "archived":
			response.Summary.ArchivedAtoms++
		default:
			response.Summary.DraftAtoms++
		}
		switch strings.TrimSpace(atom.VectorStatus) {
		case "indexed":
			response.Summary.VectorIndexedAtoms++
		case "failed":
			response.Summary.VectorFailedAtoms++
		default:
			response.Summary.VectorPendingAtoms++
		}

		domainName := strings.TrimSpace(atom.Domain)
		category := strings.TrimSpace(atom.Category)
		difficulty := strings.ToUpper(strings.TrimSpace(atom.Difficulty))
		if domainName == "" && category == "" && difficulty == "" {
			continue
		}
		key := strings.Join([]string{domainName, category, difficulty}, "|")
		group := groups[key]
		if group == nil {
			group = &interviewKnowledgeHealthCombination{
				Domain:     domainName,
				Category:   category,
				Difficulty: difficulty,
				Reasons:    []string{},
				Actions:    []string{},
			}
			groups[key] = group
		}
		group.TotalCount++
		switch strings.TrimSpace(atom.Status) {
		case "published":
			group.PublishedCount++
			role := strings.TrimSpace(atom.QuestionRole)
			if role == "opening" || role == "mixed" {
				group.OpeningCount++
			}
			if role == "followup" || role == "mixed" {
				group.FollowupCount++
				if atom.VectorStatus == "indexed" {
					group.IndexedFollowupCount++
				}
			}
			if role == "mixed" {
				group.MixedCount++
			}
			switch strings.TrimSpace(atom.VectorStatus) {
			case "failed":
				group.FailedCount++
			case "pending", "":
				group.PendingCount++
			}
		case "archived":
			group.ArchivedCount++
		default:
			group.DraftCount++
		}
	}

	response.Combinations = make([]interviewKnowledgeHealthCombination, 0, len(groups))
	for _, group := range groups {
		finalizeInterviewKnowledgeHealthCombination(group)
		switch group.Status {
		case "open":
			response.Summary.OpenCombinations++
		case "warning":
			response.Summary.WarningCombinations++
		default:
			response.Summary.BlockedCombinations++
		}
		response.Combinations = append(response.Combinations, *group)
	}
	sort.Slice(response.Combinations, func(i, j int) bool {
		left := response.Combinations[i]
		right := response.Combinations[j]
		if left.Status != right.Status {
			return interviewKnowledgeHealthStatusRank(left.Status) < interviewKnowledgeHealthStatusRank(right.Status)
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Difficulty != right.Difficulty {
			return left.Difficulty < right.Difficulty
		}
		return left.Domain < right.Domain
	})
	return response
}

func finalizeInterviewKnowledgeHealthCombination(group *interviewKnowledgeHealthCombination) {
	if group == nil {
		return
	}
	reasons := []string{}
	actions := []string{}
	if group.OpeningCount == 0 {
		reasons = append(reasons, "开场题不足")
		actions = append(actions, "补充 opening 或 mixed 题目")
	}
	if group.FollowupCount == 0 {
		reasons = append(reasons, "追问题不足")
		actions = append(actions, "补充 followup 或 mixed 题目")
	}
	if group.FollowupCount > 0 && group.IndexedFollowupCount == 0 {
		reasons = append(reasons, "追问索引未就绪")
		actions = append(actions, "重建该组合追问索引")
	}
	if len(reasons) > 0 {
		group.Status = "blocked"
		group.Reasons = uniqueStrings(reasons)
		group.Actions = uniqueStrings(actions)
		return
	}
	if group.FailedCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 条已发布题索引失败", group.FailedCount))
		actions = append(actions, "筛选 failed 并重建索引")
	}
	if group.PendingCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 条已发布题待索引", group.PendingCount))
		actions = append(actions, "重建 pending 索引")
	}
	if len(reasons) > 0 {
		group.Status = "warning"
		group.Reasons = uniqueStrings(reasons)
		group.Actions = uniqueStrings(actions)
		return
	}
	group.Status = "open"
	group.Reasons = []string{"组合可用于开场与追问检索"}
	group.Actions = []string{}
}

func interviewKnowledgeHealthStatusRank(status string) int {
	switch status {
	case "blocked":
		return 0
	case "warning":
		return 1
	case "open":
		return 2
	default:
		return 3
	}
}

func (s *Server) previewInterviewKnowledgeRetrieval(ctx context.Context, req interviewKnowledgeRetrievalPreviewRequest) (interviewKnowledgeRetrievalPreviewResponse, error) {
	req.Domain = strings.TrimSpace(req.Domain)
	req.Category = strings.TrimSpace(req.Category)
	req.Difficulty = strings.ToUpper(strings.TrimSpace(req.Difficulty))
	queryText := strings.TrimSpace(firstNonEmpty(req.Query, req.Answer, req.Text))
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	if req.Category == "" {
		return interviewKnowledgeRetrievalPreviewResponse{}, fmt.Errorf("category is required")
	}
	if !interviewKnowledgeAllowedCategories[req.Category] {
		return interviewKnowledgeRetrievalPreviewResponse{}, fmt.Errorf("category is invalid")
	}
	if !validInterviewKnowledgeDifficulty(req.Difficulty) {
		return interviewKnowledgeRetrievalPreviewResponse{}, fmt.Errorf("difficulty must be L1-L5")
	}
	if queryText == "" {
		return interviewKnowledgeRetrievalPreviewResponse{}, fmt.Errorf("query is required")
	}

	vectorStore := interviewKnowledgeVectorStore(s.store)
	diagnostics := s.interviewKnowledgeRetrievalPreviewDiagnostics(req, queryText, limit, vectorStore != nil, s.embedding != nil)
	response := interviewKnowledgeRetrievalPreviewResponse{
		Results:     []interviewKnowledgeRetrievalPreviewResult{},
		Diagnostics: diagnostics,
	}
	if vectorStore == nil {
		response.FallbackUsed = true
		response.FallbackReason = "题库检索不可用"
		return response, nil
	}
	if diagnostics.IndexedCandidates == 0 {
		response.FallbackUsed = true
		response.FallbackReason = "当前组合没有已索引追问资源"
		return response, nil
	}
	vector, embeddingIssue := s.embeddingVectorForInterviewPreview(ctx, queryText)
	if embeddingIssue != "" {
		response.FallbackUsed = true
		response.FallbackReason = embeddingIssue
		response.Diagnostics.EmbeddingAvailable = false
		return response, nil
	}
	results, err := vectorStore.SearchInterviewKnowledge(ctx, store.InterviewKnowledgeVectorSearchQuery{
		Domain:        req.Domain,
		Category:      req.Category,
		Difficulty:    req.Difficulty,
		QuestionRoles: []string{"followup", "mixed"},
		Vector:        vector,
		Limit:         limit * 8,
	})
	if err != nil {
		response.FallbackUsed = true
		response.FallbackReason = "题库检索失败：" + truncateText(err.Error(), 120)
		return response, nil
	}
	response.Results = s.interviewKnowledgeRetrievalPreviewResults(results, req, limit)
	response.MatchedCount = len(response.Results)
	if response.MatchedCount == 0 {
		response.FallbackUsed = true
		response.FallbackReason = "未命中可用题库追问原子"
	}
	return response, nil
}

func (s *Server) interviewKnowledgeRetrievalPreviewDiagnostics(req interviewKnowledgeRetrievalPreviewRequest, queryText string, limit int, vectorStoreAvailable, embeddingAvailable bool) interviewKnowledgeRetrievalPreviewDiagnostics {
	diagnostics := interviewKnowledgeRetrievalPreviewDiagnostics{
		Domain:               req.Domain,
		Category:             req.Category,
		Difficulty:           req.Difficulty,
		Query:                queryText,
		VectorStoreAvailable: vectorStoreAvailable,
		EmbeddingAvailable:   embeddingAvailable,
		SearchLimit:          limit,
		FilterCounts:         map[string]int{},
	}
	for _, atom := range s.store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{}) {
		if !interviewKnowledgeAtomMatchesPreviewCombo(atom, req) {
			continue
		}
		role := strings.TrimSpace(atom.QuestionRole)
		if role != "followup" && role != "mixed" {
			diagnostics.FilterCounts["non_followup_role"]++
			continue
		}
		diagnostics.CandidateCount++
		switch strings.TrimSpace(atom.Status) {
		case "published":
			diagnostics.PublishedCandidates++
			switch strings.TrimSpace(atom.VectorStatus) {
			case "indexed":
				diagnostics.IndexedCandidates++
			case "failed":
				diagnostics.FailedCandidates++
			default:
				diagnostics.PendingCandidates++
			}
		case "archived":
			diagnostics.ArchivedCandidates++
			diagnostics.FilterCounts["archived"]++
		default:
			diagnostics.FilterCounts["draft"]++
		}
	}
	return diagnostics
}

func interviewKnowledgeAtomMatchesPreviewCombo(atom domain.InterviewKnowledgeAtom, req interviewKnowledgeRetrievalPreviewRequest) bool {
	if req.Domain != "" && !strings.EqualFold(strings.TrimSpace(atom.Domain), req.Domain) {
		return false
	}
	if req.Category != "" && !strings.EqualFold(strings.TrimSpace(atom.Category), req.Category) {
		return false
	}
	if req.Difficulty != "" && !strings.EqualFold(strings.TrimSpace(atom.Difficulty), req.Difficulty) {
		return false
	}
	return true
}

func (s *Server) embeddingVectorForInterviewPreview(ctx context.Context, queryText string) ([]float64, string) {
	if s.embedding == nil {
		return nil, "embedding client is not configured"
	}
	result, err := s.embedding.Embed(ctx, []string{queryText})
	if err != nil {
		return nil, "embedding 生成失败：" + truncateText(err.Error(), 120)
	}
	if len(result.Vectors) != 1 || len(result.Vectors[0]) != interviewKnowledgeExpectedEmbeddingDim {
		return nil, "embedding 维度不匹配"
	}
	return append([]float64{}, result.Vectors[0]...), ""
}

func (s *Server) interviewKnowledgeRetrievalPreviewResults(results []store.InterviewKnowledgeVectorSearchResult, req interviewKnowledgeRetrievalPreviewRequest, limit int) []interviewKnowledgeRetrievalPreviewResult {
	out := []interviewKnowledgeRetrievalPreviewResult{}
	seenAtoms := map[string]bool{}
	for _, result := range results {
		atomID := strings.TrimSpace(result.Document.AtomID)
		if atomID == "" || seenAtoms[atomID] {
			continue
		}
		atom, ok := s.store.GetInterviewKnowledgeAtom(atomID)
		if !ok || !interviewKnowledgeAtomMatchesPreviewCombo(*atom, req) {
			continue
		}
		if atom.Status != "published" || atom.VectorStatus != "indexed" {
			continue
		}
		if atom.QuestionRole != "followup" && atom.QuestionRole != "mixed" {
			continue
		}
		seenAtoms[atomID] = true
		out = append(out, interviewKnowledgeRetrievalPreviewResult{
			AtomID:       atom.ID,
			Version:      maxInt(atom.CurrentVersion, 1),
			Title:        atom.Title,
			Subject:      atom.Subject,
			Domain:       atom.Domain,
			Category:     atom.Category,
			Difficulty:   atom.Difficulty,
			QuestionRole: atom.QuestionRole,
			Score:        result.Score,
			DocType:      result.Document.DocType,
			DocKey:       result.Document.DocKey,
			Snippet:      truncateText(result.Document.DocText, 160),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Server) rebuildInterviewKnowledgeIndex(ctx context.Context, req interviewKnowledgeIndexRebuildRequest) (interviewKnowledgeIndexRebuildResponse, error) {
	req.VectorStatus = firstNonEmpty(strings.TrimSpace(req.VectorStatus), "pending_failed")
	if !validInterviewKnowledgeRebuildStatus(req.VectorStatus) {
		return interviewKnowledgeIndexRebuildResponse{}, fmt.Errorf("vector_status is invalid")
	}
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 50
	}

	vectorStore := interviewKnowledgeVectorStore(s.store)
	if vectorStore == nil {
		return interviewKnowledgeIndexRebuildResponse{}, fmt.Errorf("vector store is unavailable")
	}

	atomIDs := normalizeInterviewKnowledgeRebuildIDs(req.AtomIDs, limit)
	response := interviewKnowledgeIndexRebuildResponse{Results: []interviewKnowledgeIndexRebuildResult{}}
	if len(atomIDs) > 0 {
		for _, atomID := range atomIDs {
			atom, ok := s.store.GetInterviewKnowledgeAtom(atomID)
			if !ok {
				response.addResult(interviewKnowledgeIndexRebuildResult{
					AtomID: atomID,
					Status: "failed",
					Error:  "interview knowledge atom not found",
				})
				continue
			}
			response.addResult(s.rebuildInterviewKnowledgeAtomIndex(ctx, vectorStore, *atom))
		}
		return response, nil
	}

	candidates := s.interviewKnowledgeRebuildCandidates(req.VectorStatus, limit)
	for _, atom := range candidates {
		response.addResult(s.rebuildInterviewKnowledgeAtomIndex(ctx, vectorStore, atom))
	}
	return response, nil
}

func (s *Server) interviewKnowledgeRebuildCandidates(vectorStatus string, limit int) []domain.InterviewKnowledgeAtom {
	items := s.store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{})
	candidates := []domain.InterviewKnowledgeAtom{}
	for _, atom := range items {
		status := strings.TrimSpace(atom.VectorStatus)
		if vectorStatus == "pending_failed" && status != "pending" && status != "failed" {
			continue
		}
		if vectorStatus != "pending_failed" && status != vectorStatus {
			continue
		}
		candidates = append(candidates, atom)
		if len(candidates) >= limit {
			break
		}
	}
	return candidates
}

func (s *Server) rebuildInterviewKnowledgeAtomIndex(ctx context.Context, vectorStore store.VectorStore, atom domain.InterviewKnowledgeAtom) interviewKnowledgeIndexRebuildResult {
	result := interviewKnowledgeIndexRebuildResult{AtomID: atom.ID}
	if strings.TrimSpace(atom.Status) != "published" {
		_ = vectorStore.DeleteInterviewKnowledgeByAtom(ctx, atom.ID)
		result.Status = "skipped"
		result.Error = "only published atoms are indexed"
		return result
	}
	docs := ai.BuildInterviewKnowledgeVectorDocuments(atom)
	if len(docs) == 0 {
		return s.failInterviewKnowledgeAtomIndex(atom.ID, "no vector documents to index")
	}
	if s.embedding == nil {
		return s.failInterviewKnowledgeAtomIndex(atom.ID, "embedding client is not configured")
	}
	texts := make([]string, 0, len(docs))
	for _, doc := range docs {
		texts = append(texts, doc.DocText)
	}
	embedding, err := s.embedding.Embed(ctx, texts)
	if err != nil {
		return s.failInterviewKnowledgeAtomIndex(atom.ID, err.Error())
	}
	if len(embedding.Vectors) != len(docs) {
		return s.failInterviewKnowledgeAtomIndex(atom.ID, "embedding response count mismatch")
	}
	for i := range docs {
		if len(embedding.Vectors[i]) != interviewKnowledgeExpectedEmbeddingDim {
			return s.failInterviewKnowledgeAtomIndex(atom.ID, "embedding dimension mismatch")
		}
		docs[i].Vector = append([]float64{}, embedding.Vectors[i]...)
		docs[i].EmbeddingModel = embedding.Model
		docs[i].EmbeddingDim = len(embedding.Vectors[i])
	}
	if err := vectorStore.RebuildInterviewKnowledgeIndex(ctx, docs); err != nil {
		return s.failInterviewKnowledgeAtomIndex(atom.ID, err.Error())
	}
	indexedAt := time.Now()
	if _, err := s.store.UpdateInterviewKnowledgeAtomIndexStatus(atom.ID, "indexed", &indexedAt); err != nil {
		return s.failInterviewKnowledgeAtomIndex(atom.ID, err.Error())
	}
	result.Status = "indexed"
	result.DocCount = len(docs)
	result.EmbeddingModel = embedding.Model
	return result
}

func (s *Server) failInterviewKnowledgeAtomIndex(atomID, message string) interviewKnowledgeIndexRebuildResult {
	if _, err := s.store.UpdateInterviewKnowledgeAtomIndexStatus(atomID, "failed", nil); err != nil {
		message = strings.TrimSpace(message + "; status update failed: " + err.Error())
	}
	return interviewKnowledgeIndexRebuildResult{
		AtomID: atomID,
		Status: "failed",
		Error:  truncateText(message, 180),
	}
}

func (r *interviewKnowledgeIndexRebuildResponse) addResult(result interviewKnowledgeIndexRebuildResult) {
	r.Total++
	switch result.Status {
	case "indexed":
		r.Indexed++
	case "skipped":
		r.Skipped++
	default:
		r.Failed++
	}
	r.Results = append(r.Results, result)
}

func interviewKnowledgeVectorStore(dataStore store.Store) store.VectorStore {
	provider, ok := dataStore.(store.VectorStoreProvider)
	if !ok {
		return nil
	}
	return provider.VectorStore()
}

func validInterviewKnowledgeRebuildStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "pending", "failed", "pending_failed":
		return true
	default:
		return false
	}
}

func normalizeInterviewKnowledgeRebuildIDs(ids []string, limit int) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func decodeInterviewKnowledgeImportRequest(w http.ResponseWriter, r *http.Request) (interviewKnowledgeImportRequest, bool) {
	var raw map[string]json.RawMessage
	if !decode(w, r, &raw) {
		return interviewKnowledgeImportRequest{}, false
	}
	req := interviewKnowledgeImportRequest{
		BatchID:         rawStringAlias(raw, "batch_id", "batchId"),
		SourceRef:       rawStringAlias(raw, "source_ref", "sourceRef"),
		PublishNote:     rawStringAlias(raw, "publish_note", "publishNote"),
		Domain:          rawStringAlias(raw, "domain"),
		Category:        rawStringAlias(raw, "category"),
		Difficulty:      rawStringAlias(raw, "difficulty"),
		QuestionRole:    rawStringAlias(raw, "question_role", "questionRole"),
		QuestionType:    rawStringAlias(raw, "question_type", "questionType"),
		OpeningQuestion: rawStringAlias(raw, "opening_question", "openingQuestion"),
		StableCode:      strings.ToUpper(rawStringAlias(raw, "stable_code", "stableCode")),
		Status:          rawStringAlias(raw, "status"),
		VectorStatus:    rawStringAlias(raw, "vector_status", "vectorStatus"),
		Tags:            normalizeImportStringList(rawStringSliceAlias(raw, "tags"), true),
	}
	itemRaw, ok := rawAlias(raw, "items", "atoms")
	if ok {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(itemRaw, &items); err == nil {
			req.Items = make([]interviewKnowledgeImportRawItem, 0, len(items))
			for i, item := range items {
				req.Items = append(req.Items, parseInterviewKnowledgeImportItem(i, item, req))
			}
		}
	}
	return req, true
}

func parseInterviewKnowledgeImportItem(index int, raw map[string]json.RawMessage, req interviewKnowledgeImportRequest) interviewKnowledgeImportRawItem {
	itemTags := normalizeImportStringList(rawStringSliceAlias(raw, "tags"), true)
	tags := uniqueStrings(append(append([]string{}, req.Tags...), itemTags...))
	return interviewKnowledgeImportRawItem{
		Index:           index,
		Raw:             raw,
		ID:              rawStringAlias(raw, "id"),
		Title:           rawStringAlias(raw, "title"),
		Subject:         rawStringAlias(raw, "subject"),
		Domain:          firstNonEmpty(rawStringAlias(raw, "domain"), req.Domain),
		Difficulty:      firstNonEmpty(rawStringAlias(raw, "difficulty"), req.Difficulty),
		Category:        firstNonEmpty(rawStringAlias(raw, "category"), req.Category),
		QuestionRole:    firstNonEmpty(rawStringAlias(raw, "question_role", "questionRole"), req.QuestionRole),
		QuestionType:    firstNonEmpty(rawStringAlias(raw, "question_type", "questionType"), req.QuestionType),
		OpeningQuestion: firstNonEmpty(rawStringAlias(raw, "opening_question", "openingQuestion"), req.OpeningQuestion),
		StableCode:      strings.ToUpper(firstNonEmpty(rawStringAlias(raw, "stable_code", "stableCode"), req.StableCode)),
		SourceRef:       firstNonEmpty(rawStringAlias(raw, "source_ref", "sourceRef"), req.SourceRef),
		Tags:            tags,
		Principles:      normalizeImportStringList(rawStringSliceAlias(raw, "principles"), false),
		Pitfalls:        normalizeImportStringList(rawStringSliceAlias(raw, "pitfalls"), false),
		FollowUpPaths:   normalizeImportStringList(rawStringSliceAlias(raw, "follow_up_paths", "followUpPaths"), false),
		Status:          firstNonEmpty(rawStringAlias(raw, "status"), req.Status, "published"),
		VectorStatus:    firstNonEmpty(rawStringAlias(raw, "vector_status", "vectorStatus"), req.VectorStatus, "pending"),
	}
}

func (s *Server) previewInterviewKnowledgeImport(req interviewKnowledgeImportRequest) interviewKnowledgeImportReport {
	report := interviewKnowledgeImportReport{
		BatchID:   firstNonEmpty(req.BatchID, "batch-"+strconv.FormatInt(time.Now().UnixNano(), 10)),
		SourceRef: req.SourceRef,
		Summary: map[string]int{
			"total":           len(req.Items),
			"valid_count":     0,
			"error_count":     0,
			"warning_count":   0,
			"create_count":    0,
			"update_count":    0,
			"duplicate_count": 0,
		},
		Errors:   []string{},
		Warnings: []string{},
		Results:  []interviewKnowledgeImportItemResult{},
	}
	if len(req.Items) == 0 {
		report.Errors = append(report.Errors, "items is required")
		report.Summary["error_count"] = 1
		return report
	}

	seen := map[string]int{}
	for _, rawItem := range req.Items {
		result := s.validateInterviewKnowledgeImportItem(rawItem)
		if result.ID != "" {
			if previousIndex, ok := seen[result.ID]; ok {
				result.Errors = append(result.Errors, fmt.Sprintf("duplicate id in batch, first seen at index %d", previousIndex))
			} else {
				seen[result.ID] = rawItem.Index
			}
		}
		if len(result.Errors) == 0 {
			report.Summary["valid_count"]++
			switch result.Action {
			case "create":
				report.Summary["create_count"]++
			case "update":
				report.Summary["update_count"]++
			case "duplicate_import":
				report.Summary["duplicate_count"]++
			}
		}
		if len(result.Errors) > 0 {
			result.Action = "invalid"
			report.Summary["error_count"] += len(result.Errors)
			for _, err := range result.Errors {
				report.Errors = append(report.Errors, fmt.Sprintf("items[%d]: %s", result.Index, err))
			}
		}
		if len(result.Warnings) > 0 {
			report.Summary["warning_count"] += len(result.Warnings)
			for _, warning := range result.Warnings {
				report.Warnings = append(report.Warnings, fmt.Sprintf("items[%d]: %s", result.Index, warning))
			}
		}
		report.Results = append(report.Results, result)
	}
	return report
}

func (s *Server) validateInterviewKnowledgeImportItem(rawItem interviewKnowledgeImportRawItem) interviewKnowledgeImportItemResult {
	result := interviewKnowledgeImportItemResult{
		Index:    rawItem.Index,
		ID:       rawItem.ID,
		Title:    rawItem.Title,
		Action:   "create",
		Errors:   []string{},
		Warnings: []string{},
		Atom: domain.InterviewKnowledgeAtom{
			ID:              rawItem.ID,
			Title:           rawItem.Title,
			Subject:         rawItem.Subject,
			Domain:          rawItem.Domain,
			Difficulty:      strings.ToUpper(rawItem.Difficulty),
			Category:        rawItem.Category,
			QuestionRole:    rawItem.QuestionRole,
			QuestionType:    rawItem.QuestionType,
			OpeningQuestion: rawItem.OpeningQuestion,
			StableCode:      rawItem.StableCode,
			SourceRef:       rawItem.SourceRef,
			Tags:            rawItem.Tags,
			Principles:      rawItem.Principles,
			Pitfalls:        rawItem.Pitfalls,
			FollowUpPaths:   rawItem.FollowUpPaths,
			Status:          rawItem.Status,
			VectorStatus:    rawItem.VectorStatus,
		},
		VersionType: domain.InterviewKnowledgeVersionContentUpdate,
	}
	result.Errors = append(result.Errors, validateInterviewKnowledgeAtomFields(result.Atom)...)

	if existing, ok := s.store.GetInterviewKnowledgeAtom(result.Atom.ID); ok {
		result.ExistingVersion = existing.CurrentVersion
		if interviewKnowledgeAtomsSameContent(*existing, result.Atom) {
			result.Action = "duplicate_import"
			result.VersionType = domain.InterviewKnowledgeVersionDuplicateImport
		} else {
			result.Action = "update"
			result.VersionType = domain.InterviewKnowledgeVersionContentUpdate
		}
	}
	return result
}

func validateInterviewKnowledgeAtomFields(atom domain.InterviewKnowledgeAtom) []string {
	errors := []string{}
	required := map[string]string{
		"id":            atom.ID,
		"title":         atom.Title,
		"subject":       atom.Subject,
		"domain":        atom.Domain,
		"difficulty":    atom.Difficulty,
		"category":      atom.Category,
		"question_role": atom.QuestionRole,
		"source_ref":    atom.SourceRef,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			errors = append(errors, field+" is required")
		}
	}
	if !validInterviewKnowledgeDifficulty(atom.Difficulty) {
		errors = append(errors, "difficulty must be L1-L5")
	}
	if !interviewKnowledgeAllowedCategories[atom.Category] {
		errors = append(errors, "category is invalid")
	}
	if !interviewKnowledgeAllowedQuestionRoles[atom.QuestionRole] {
		errors = append(errors, "question_role is invalid")
	}
	if atom.QuestionRole == "opening" || atom.QuestionRole == "mixed" {
		if strings.TrimSpace(atom.OpeningQuestion) == "" {
			errors = append(errors, "opening_question is required for opening or mixed items")
		} else if len([]rune(strings.TrimSpace(atom.OpeningQuestion))) > 50 {
			errors = append(errors, "opening_question must be at most 50 characters")
		}
		if !interviewKnowledgeAllowedQuestionTypes[strings.TrimSpace(atom.QuestionType)] {
			errors = append(errors, "question_type is invalid")
		}
		if !validInterviewStableCode(atom.StableCode) {
			errors = append(errors, "stable_code must use DOMAIN-001 format")
		}
	}
	if !interviewKnowledgeAllowedStatuses[atom.Status] {
		errors = append(errors, "status is invalid")
	}
	if !interviewKnowledgeAllowedVectorStatuses[atom.VectorStatus] {
		errors = append(errors, "vector_status is invalid")
	}
	if len(atom.Principles) < 2 {
		errors = append(errors, "principles must include at least 2 items")
	}
	if len(atom.Pitfalls) < 2 {
		errors = append(errors, "pitfalls must include at least 2 items")
	}
	if len(atom.FollowUpPaths) < 2 {
		errors = append(errors, "follow_up_paths must include at least 2 items")
	}
	return errors
}

func validInterviewStableCode(value string) bool {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(value)), "-")
	if len(parts) != 2 || len(parts[0]) < 1 || len(parts[0]) > 8 || len(parts[1]) < 3 {
		return false
	}
	for _, r := range parts[0] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validInterviewKnowledgeDifficulty(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "L1", "L2", "L3", "L4", "L5":
		return true
	default:
		return false
	}
}

func interviewKnowledgeAtomsSameContent(left, right domain.InterviewKnowledgeAtom) bool {
	return left.Title == right.Title &&
		left.Subject == right.Subject &&
		left.Domain == right.Domain &&
		left.Difficulty == right.Difficulty &&
		left.Category == right.Category &&
		left.QuestionRole == right.QuestionRole &&
		left.QuestionType == right.QuestionType &&
		left.OpeningQuestion == right.OpeningQuestion &&
		left.StableCode == right.StableCode &&
		left.SourceRef == right.SourceRef &&
		left.Status == right.Status &&
		reflect.DeepEqual(left.Tags, right.Tags) &&
		reflect.DeepEqual(left.Principles, right.Principles) &&
		reflect.DeepEqual(left.Pitfalls, right.Pitfalls) &&
		reflect.DeepEqual(left.FollowUpPaths, right.FollowUpPaths)
}

func rawAlias(raw map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func rawStringAlias(raw map[string]json.RawMessage, keys ...string) string {
	value, ok := rawAlias(raw, keys...)
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var generic interface{}
	if err := json.Unmarshal(value, &generic); err == nil && generic != nil {
		return strings.TrimSpace(fmt.Sprint(generic))
	}
	return ""
}

func rawStringSliceAlias(raw map[string]json.RawMessage, keys ...string) []string {
	value, ok := rawAlias(raw, keys...)
	if !ok {
		return []string{}
	}
	var items []string
	if err := json.Unmarshal(value, &items); err == nil {
		return items
	}
	var genericItems []interface{}
	if err := json.Unmarshal(value, &genericItems); err == nil {
		items = make([]string, 0, len(genericItems))
		for _, item := range genericItems {
			if item != nil {
				items = append(items, fmt.Sprint(item))
			}
		}
		return items
	}
	text := rawStringAlias(raw, keys...)
	if text == "" {
		return []string{}
	}
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';' || r == '；'
	})
}

func normalizeImportStringList(items []string, dedupe bool) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if dedupe {
			key := strings.ToLower(item)
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, item)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func interviewKnowledgeReportMap(report interviewKnowledgeImportReport) map[string]interface{} {
	data, err := json.Marshal(report)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func writeErrorWithData(w http.ResponseWriter, status int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Code: status, Message: message, Data: data})
}
