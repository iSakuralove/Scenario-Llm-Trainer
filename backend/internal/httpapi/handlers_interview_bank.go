package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
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
	BatchID      string
	SourceRef    string
	PublishNote  string
	Domain       string
	Category     string
	Difficulty   string
	QuestionRole string
	Status       string
	VectorStatus string
	Tags         []string
	Items        []interviewKnowledgeImportRawItem
}

type interviewKnowledgeImportRawItem struct {
	Index         int
	Raw           map[string]json.RawMessage
	ID            string
	Title         string
	Subject       string
	Domain        string
	Difficulty    string
	Category      string
	QuestionRole  string
	SourceRef     string
	Tags          []string
	Principles    []string
	Pitfalls      []string
	FollowUpPaths []string
	Status        string
	VectorStatus  string
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

func (s *Server) handleAdminInterviewBank(w http.ResponseWriter, r *http.Request, user *domain.User, parts []string) {
	if len(parts) == 1 && parts[0] == "summary" && r.Method == http.MethodGet {
		writeOK(w, s.store.InterviewKnowledgeSummary())
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

func decodeInterviewKnowledgeImportRequest(w http.ResponseWriter, r *http.Request) (interviewKnowledgeImportRequest, bool) {
	var raw map[string]json.RawMessage
	if !decode(w, r, &raw) {
		return interviewKnowledgeImportRequest{}, false
	}
	req := interviewKnowledgeImportRequest{
		BatchID:      rawStringAlias(raw, "batch_id", "batchId"),
		SourceRef:    rawStringAlias(raw, "source_ref", "sourceRef"),
		PublishNote:  rawStringAlias(raw, "publish_note", "publishNote"),
		Domain:       rawStringAlias(raw, "domain"),
		Category:     rawStringAlias(raw, "category"),
		Difficulty:   rawStringAlias(raw, "difficulty"),
		QuestionRole: rawStringAlias(raw, "question_role", "questionRole"),
		Status:       rawStringAlias(raw, "status"),
		VectorStatus: rawStringAlias(raw, "vector_status", "vectorStatus"),
		Tags:         normalizeImportStringList(rawStringSliceAlias(raw, "tags"), true),
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
		Index:         index,
		Raw:           raw,
		ID:            rawStringAlias(raw, "id"),
		Title:         rawStringAlias(raw, "title"),
		Subject:       rawStringAlias(raw, "subject"),
		Domain:        firstNonEmpty(rawStringAlias(raw, "domain"), req.Domain),
		Difficulty:    firstNonEmpty(rawStringAlias(raw, "difficulty"), req.Difficulty),
		Category:      firstNonEmpty(rawStringAlias(raw, "category"), req.Category),
		QuestionRole:  firstNonEmpty(rawStringAlias(raw, "question_role", "questionRole"), req.QuestionRole),
		SourceRef:     firstNonEmpty(rawStringAlias(raw, "source_ref", "sourceRef"), req.SourceRef),
		Tags:          tags,
		Principles:    normalizeImportStringList(rawStringSliceAlias(raw, "principles"), false),
		Pitfalls:      normalizeImportStringList(rawStringSliceAlias(raw, "pitfalls"), false),
		FollowUpPaths: normalizeImportStringList(rawStringSliceAlias(raw, "follow_up_paths", "followUpPaths"), false),
		Status:        firstNonEmpty(rawStringAlias(raw, "status"), req.Status, "published"),
		VectorStatus:  firstNonEmpty(rawStringAlias(raw, "vector_status", "vectorStatus"), req.VectorStatus, "pending"),
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
			ID:            rawItem.ID,
			Title:         rawItem.Title,
			Subject:       rawItem.Subject,
			Domain:        rawItem.Domain,
			Difficulty:    strings.ToUpper(rawItem.Difficulty),
			Category:      rawItem.Category,
			QuestionRole:  rawItem.QuestionRole,
			SourceRef:     rawItem.SourceRef,
			Tags:          rawItem.Tags,
			Principles:    rawItem.Principles,
			Pitfalls:      rawItem.Pitfalls,
			FollowUpPaths: rawItem.FollowUpPaths,
			Status:        rawItem.Status,
			VectorStatus:  rawItem.VectorStatus,
		},
		VersionType: domain.InterviewKnowledgeVersionContentUpdate,
	}
	required := map[string]string{
		"id":            result.Atom.ID,
		"title":         result.Atom.Title,
		"subject":       result.Atom.Subject,
		"domain":        result.Atom.Domain,
		"difficulty":    result.Atom.Difficulty,
		"category":      result.Atom.Category,
		"question_role": result.Atom.QuestionRole,
		"source_ref":    result.Atom.SourceRef,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			result.Errors = append(result.Errors, field+" is required")
		}
	}
	if !validInterviewKnowledgeDifficulty(result.Atom.Difficulty) {
		result.Errors = append(result.Errors, "difficulty must be L1-L5")
	}
	if !interviewKnowledgeAllowedCategories[result.Atom.Category] {
		result.Errors = append(result.Errors, "category is invalid")
	}
	if !interviewKnowledgeAllowedQuestionRoles[result.Atom.QuestionRole] {
		result.Errors = append(result.Errors, "question_role is invalid")
	}
	if !interviewKnowledgeAllowedStatuses[result.Atom.Status] {
		result.Errors = append(result.Errors, "status is invalid")
	}
	if !interviewKnowledgeAllowedVectorStatuses[result.Atom.VectorStatus] {
		result.Errors = append(result.Errors, "vector_status is invalid")
	}
	if len(result.Atom.Principles) < 2 {
		result.Errors = append(result.Errors, "principles must include at least 2 items")
	}
	if len(result.Atom.Pitfalls) < 2 {
		result.Errors = append(result.Errors, "pitfalls must include at least 2 items")
	}
	if len(result.Atom.FollowUpPaths) < 2 {
		result.Errors = append(result.Errors, "follow_up_paths must include at least 2 items")
	}

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
