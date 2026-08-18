package store

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

var errInterviewKnowledgeAtomIDRequired = errors.New("interview knowledge atom id is required")
var errInterviewKnowledgeAtomNotFound = errors.New("interview knowledge atom not found")
var errInterviewBankOpsActionNotFound = errors.New("interview bank ops action not found")

func prepareInterviewKnowledgeAtomForVersion(atom domain.InterviewKnowledgeAtom, existing *domain.InterviewKnowledgeAtom, nextVersion int, now time.Time) domain.InterviewKnowledgeAtom {
	atom.ID = strings.TrimSpace(atom.ID)
	atom.Title = strings.TrimSpace(atom.Title)
	atom.Subject = strings.TrimSpace(atom.Subject)
	atom.Domain = strings.TrimSpace(atom.Domain)
	atom.Difficulty = strings.TrimSpace(atom.Difficulty)
	atom.Category = strings.TrimSpace(atom.Category)
	atom.QuestionRole = strings.TrimSpace(atom.QuestionRole)
	atom.QuestionType = strings.TrimSpace(atom.QuestionType)
	atom.OpeningQuestion = strings.TrimSpace(atom.OpeningQuestion)
	atom.StableCode = strings.ToUpper(strings.TrimSpace(atom.StableCode))
	atom.SourceRef = strings.TrimSpace(atom.SourceRef)
	atom.Status = strings.TrimSpace(atom.Status)
	atom.VectorStatus = strings.TrimSpace(atom.VectorStatus)
	atom.Tags = normalizeStringList(atom.Tags)
	atom.Principles = normalizeStringList(atom.Principles)
	atom.Pitfalls = normalizeStringList(atom.Pitfalls)
	atom.FollowUpPaths = normalizeStringList(atom.FollowUpPaths)

	if existing != nil {
		if atom.Status == "" {
			atom.Status = existing.Status
		}
		if atom.VectorStatus == "" {
			atom.VectorStatus = existing.VectorStatus
		}
		atom.CreatedAt = existing.CreatedAt
		if atom.LastIndexedAt == nil {
			atom.LastIndexedAt = existing.LastIndexedAt
		}
	} else if atom.CreatedAt.IsZero() {
		atom.CreatedAt = now
	}
	if atom.Status == "" {
		atom.Status = "draft"
	}
	if atom.VectorStatus == "" {
		atom.VectorStatus = "pending"
	}
	atom.UpdatedAt = now
	atom.CurrentVersion = nextVersion
	return atom
}

func buildInterviewKnowledgeAtomVersion(atom domain.InterviewKnowledgeAtom, previous *domain.InterviewKnowledgeAtom, versionType, adminID, changeNote string, now time.Time) domain.InterviewKnowledgeAtomVersion {
	version := domain.InterviewKnowledgeAtomVersion{
		ID:          NewID(),
		AtomID:      atom.ID,
		Version:     atom.CurrentVersion,
		VersionType: versionType,
		AdminID:     strings.TrimSpace(adminID),
		ChangeNote:  strings.TrimSpace(changeNote),
		Snapshot:    interviewKnowledgeAtomSnapshot(atom),
		CreatedAt:   now,
	}
	if previous == nil {
		version.NoContentChange = false
		version.DiffSummary = map[string]interface{}{"created": true}
		return version
	}
	previousSnapshot := interviewKnowledgeAtomSnapshot(*previous)
	version.NoContentChange = reflect.DeepEqual(previousSnapshot, version.Snapshot)
	version.DiffSummary = interviewKnowledgeDiffSummary(previousSnapshot, version.Snapshot)
	return version
}

func interviewKnowledgeAtomSnapshot(atom domain.InterviewKnowledgeAtom) domain.InterviewKnowledgeAtomSnapshot {
	return domain.InterviewKnowledgeAtomSnapshot{
		ID:              atom.ID,
		Title:           atom.Title,
		Subject:         atom.Subject,
		Domain:          atom.Domain,
		Difficulty:      atom.Difficulty,
		Category:        atom.Category,
		QuestionRole:    atom.QuestionRole,
		QuestionType:    atom.QuestionType,
		OpeningQuestion: atom.OpeningQuestion,
		StableCode:      atom.StableCode,
		SourceRef:       atom.SourceRef,
		Tags:            append([]string{}, atom.Tags...),
		Principles:      append([]string{}, atom.Principles...),
		Pitfalls:        append([]string{}, atom.Pitfalls...),
		FollowUpPaths:   append([]string{}, atom.FollowUpPaths...),
		Status:          atom.Status,
	}
}

func interviewKnowledgeDiffSummary(previous, next domain.InterviewKnowledgeAtomSnapshot) map[string]interface{} {
	changed := []string{}
	if previous.ID != next.ID {
		changed = append(changed, "id")
	}
	if previous.Title != next.Title {
		changed = append(changed, "title")
	}
	if previous.Subject != next.Subject {
		changed = append(changed, "subject")
	}
	if previous.Domain != next.Domain {
		changed = append(changed, "domain")
	}
	if previous.Difficulty != next.Difficulty {
		changed = append(changed, "difficulty")
	}
	if previous.Category != next.Category {
		changed = append(changed, "category")
	}
	if previous.QuestionRole != next.QuestionRole {
		changed = append(changed, "question_role")
	}
	if previous.QuestionType != next.QuestionType {
		changed = append(changed, "question_type")
	}
	if previous.OpeningQuestion != next.OpeningQuestion {
		changed = append(changed, "opening_question")
	}
	if previous.StableCode != next.StableCode {
		changed = append(changed, "stable_code")
	}
	if previous.SourceRef != next.SourceRef {
		changed = append(changed, "sourceRef")
	}
	if !reflect.DeepEqual(previous.Tags, next.Tags) {
		changed = append(changed, "tags")
	}
	if !reflect.DeepEqual(previous.Principles, next.Principles) {
		changed = append(changed, "principles")
	}
	if !reflect.DeepEqual(previous.Pitfalls, next.Pitfalls) {
		changed = append(changed, "pitfalls")
	}
	if !reflect.DeepEqual(previous.FollowUpPaths, next.FollowUpPaths) {
		changed = append(changed, "followUpPaths")
	}
	if previous.Status != next.Status {
		changed = append(changed, "status")
	}
	return map[string]interface{}{"fields_changed": changed}
}

func validateInterviewKnowledgeAtomSave(atom domain.InterviewKnowledgeAtom, versionType string) error {
	if strings.TrimSpace(atom.ID) == "" {
		return errInterviewKnowledgeAtomIDRequired
	}
	if !domain.ValidInterviewKnowledgeVersionType(versionType) {
		return errors.New("invalid interview knowledge version type")
	}
	return nil
}

func prepareInterviewKnowledgeAtomIndexStatus(atom domain.InterviewKnowledgeAtom, vectorStatus string, lastIndexedAt *time.Time, now time.Time) (domain.InterviewKnowledgeAtom, error) {
	vectorStatus = strings.TrimSpace(vectorStatus)
	if !validInterviewKnowledgeVectorStatus(vectorStatus) {
		return domain.InterviewKnowledgeAtom{}, errors.New("invalid interview knowledge vector status")
	}
	atom.VectorStatus = vectorStatus
	if vectorStatus == "indexed" {
		if lastIndexedAt == nil {
			lastIndexedAt = &now
		}
		indexedAt := *lastIndexedAt
		atom.LastIndexedAt = &indexedAt
	}
	atom.UpdatedAt = now
	return atom, nil
}

func validInterviewKnowledgeVectorStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "pending", "indexed", "failed":
		return true
	default:
		return false
	}
}

func normalizeStringList(items []string) []string {
	if items == nil {
		return []string{}
	}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			normalized = append(normalized, item)
		}
	}
	return normalized
}

func cloneInterviewKnowledgeAtom(atom *domain.InterviewKnowledgeAtom) *domain.InterviewKnowledgeAtom {
	if atom == nil {
		return nil
	}
	copy := *atom
	copy.Tags = append([]string{}, atom.Tags...)
	copy.Principles = append([]string{}, atom.Principles...)
	copy.Pitfalls = append([]string{}, atom.Pitfalls...)
	copy.FollowUpPaths = append([]string{}, atom.FollowUpPaths...)
	if atom.LastIndexedAt != nil {
		lastIndexedAt := *atom.LastIndexedAt
		copy.LastIndexedAt = &lastIndexedAt
	}
	return &copy
}

func cloneInterviewKnowledgeAtomVersion(version domain.InterviewKnowledgeAtomVersion) domain.InterviewKnowledgeAtomVersion {
	version.Snapshot.Tags = append([]string{}, version.Snapshot.Tags...)
	version.Snapshot.Principles = append([]string{}, version.Snapshot.Principles...)
	version.Snapshot.Pitfalls = append([]string{}, version.Snapshot.Pitfalls...)
	version.Snapshot.FollowUpPaths = append([]string{}, version.Snapshot.FollowUpPaths...)
	version.DiffSummary = cloneJSONMap(version.DiffSummary)
	return version
}

func cloneInterviewKnowledgeBatch(batch *domain.InterviewKnowledgeBatch) *domain.InterviewKnowledgeBatch {
	if batch == nil {
		return nil
	}
	copy := *batch
	copy.ValidationReport = cloneJSONMap(batch.ValidationReport)
	return &copy
}

func prepareInterviewBankOpsActionForCreate(action domain.InterviewBankOpsAction, now time.Time) (domain.InterviewBankOpsAction, error) {
	action.ID = strings.TrimSpace(action.ID)
	if action.ID == "" {
		action.ID = NewID()
	}
	action.ActionType = strings.TrimSpace(action.ActionType)
	action.Status = strings.TrimSpace(action.Status)
	action.Priority = strings.ToUpper(strings.TrimSpace(action.Priority))
	action.Source = strings.TrimSpace(action.Source)
	action.DedupeKey = strings.TrimSpace(action.DedupeKey)
	action.Title = strings.TrimSpace(action.Title)
	action.Reason = strings.TrimSpace(action.Reason)
	action.Domain = strings.TrimSpace(action.Domain)
	action.Category = strings.TrimSpace(action.Category)
	action.Difficulty = strings.ToUpper(strings.TrimSpace(action.Difficulty))
	action.AtomID = strings.TrimSpace(action.AtomID)
	action.CreatedBy = strings.TrimSpace(action.CreatedBy)
	if action.Status == "" {
		action.Status = domain.InterviewBankOpsActionStatusOpen
	}
	if action.Source == "" {
		action.Source = domain.InterviewBankOpsActionSourceManual
	}
	if action.Evidence == nil {
		action.Evidence = map[string]interface{}{}
	} else {
		action.Evidence = cloneJSONMap(action.Evidence)
	}
	if action.CreatedAt.IsZero() {
		action.CreatedAt = now
	}
	action.UpdatedAt = now
	if action.DedupeKey == "" {
		action.DedupeKey = domain.InterviewBankOpsActionDedupeKey(action)
	}
	if err := validateInterviewBankOpsAction(action); err != nil {
		return domain.InterviewBankOpsAction{}, err
	}
	return action, nil
}

func validateInterviewBankOpsAction(action domain.InterviewBankOpsAction) error {
	if action.Title == "" {
		return errors.New("title is required")
	}
	if action.Reason == "" {
		return errors.New("reason is required")
	}
	if !domain.ValidInterviewBankOpsActionType(action.ActionType) {
		return errors.New("invalid action_type")
	}
	if !domain.ValidInterviewBankOpsActionSource(action.Source) {
		return errors.New("invalid source")
	}
	if !domain.ValidInterviewBankOpsActionStatus(action.Status) {
		return errors.New("invalid status")
	}
	if !domain.ValidInterviewBankOpsActionPriority(action.Priority) {
		return errors.New("invalid priority")
	}
	if action.AtomID == "" && (action.Domain == "" || action.Category == "" || action.Difficulty == "") {
		return errors.New("target scope is required")
	}
	if action.DedupeKey == "" {
		return errors.New("dedupe_key is required")
	}
	return nil
}

func isActiveInterviewBankOpsActionStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.InterviewBankOpsActionStatusOpen,
		domain.InterviewBankOpsActionStatusInProgress,
		domain.InterviewBankOpsActionStatusWatching,
		domain.InterviewBankOpsActionStatusReopened:
		return true
	default:
		return false
	}
}

func cloneInterviewBankOpsAction(action *domain.InterviewBankOpsAction) *domain.InterviewBankOpsAction {
	if action == nil {
		return nil
	}
	copy := *action
	copy.Evidence = cloneJSONMap(action.Evidence)
	return &copy
}

func prepareInterviewBankOpsActionStatusTransition(current domain.InterviewBankOpsAction, nextStatus, note, adminID string, now time.Time) (domain.InterviewBankOpsAction, domain.InterviewBankOpsActionHistoryEntry, error) {
	nextStatus = strings.TrimSpace(nextStatus)
	note = strings.TrimSpace(note)
	adminID = strings.TrimSpace(adminID)
	if !domain.ValidInterviewBankOpsActionStatus(nextStatus) {
		return domain.InterviewBankOpsAction{}, domain.InterviewBankOpsActionHistoryEntry{}, errors.New("invalid status")
	}
	if current.Status == nextStatus {
		return domain.InterviewBankOpsAction{}, domain.InterviewBankOpsActionHistoryEntry{}, errors.New("status is unchanged")
	}
	if (nextStatus == domain.InterviewBankOpsActionStatusResolved || nextStatus == domain.InterviewBankOpsActionStatusDismissed) && note == "" {
		return domain.InterviewBankOpsAction{}, domain.InterviewBankOpsActionHistoryEntry{}, errors.New("note is required for resolved or dismissed")
	}
	if nextStatus == domain.InterviewBankOpsActionStatusReopened &&
		current.Status != domain.InterviewBankOpsActionStatusResolved &&
		current.Status != domain.InterviewBankOpsActionStatusDismissed {
		return domain.InterviewBankOpsAction{}, domain.InterviewBankOpsActionHistoryEntry{}, errors.New("reopened status requires resolved or dismissed action")
	}
	updated := *cloneInterviewBankOpsAction(&current)
	updated.Status = nextStatus
	updated.UpdatedAt = now
	historyEntry := domain.InterviewBankOpsActionHistoryEntry{
		ID:         NewID(),
		ActionID:   current.ID,
		FromStatus: current.Status,
		ToStatus:   nextStatus,
		Note:       note,
		CreatedBy:  adminID,
		CreatedAt:  now,
	}
	return updated, historyEntry, nil
}

func cloneInterviewBankOpsActionHistoryEntry(item domain.InterviewBankOpsActionHistoryEntry) domain.InterviewBankOpsActionHistoryEntry {
	return item
}

func sortInterviewBankOpsActionHistory(items []domain.InterviewBankOpsActionHistoryEntry) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].EntryIndex != items[j].EntryIndex {
			return items[i].EntryIndex > items[j].EntryIndex
		}
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}

func interviewBankOpsActionMatchesFilter(action domain.InterviewBankOpsAction, filter domain.InterviewBankOpsActionFilter) bool {
	if !matchesTrimmedFilter(action.Status, filter.Status) {
		return false
	}
	if !matchesTrimmedFilter(action.ActionType, filter.ActionType) {
		return false
	}
	if !matchesTrimmedFilter(action.Priority, filter.Priority) {
		return false
	}
	if !matchesTrimmedFilter(action.Source, filter.Source) {
		return false
	}
	if !matchesTrimmedFilter(action.Domain, filter.Domain) {
		return false
	}
	if !matchesTrimmedFilter(action.Category, filter.Category) {
		return false
	}
	if !matchesTrimmedFilter(action.Difficulty, filter.Difficulty) {
		return false
	}
	return matchesTrimmedFilter(action.AtomID, filter.AtomID)
}

func cloneInterviewKnowledgeAtomLightSnapshots(items []domain.InterviewKnowledgeAtomLightSnapshot) []domain.InterviewKnowledgeAtomLightSnapshot {
	if items == nil {
		return []domain.InterviewKnowledgeAtomLightSnapshot{}
	}
	return append([]domain.InterviewKnowledgeAtomLightSnapshot{}, items...)
}

func cloneJSONMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = cloneJSONValue(value)
	}
	return output
}

func cloneJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneJSONMap(typed)
	case []interface{}:
		copy := make([]interface{}, len(typed))
		for i, item := range typed {
			copy[i] = cloneJSONValue(item)
		}
		return copy
	case []string:
		return append([]string{}, typed...)
	default:
		return typed
	}
}

func sortInterviewKnowledgeVersions(items []domain.InterviewKnowledgeAtomVersion) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].Version > items[j].Version
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}

func interviewKnowledgeAtomMatchesFilter(atom domain.InterviewKnowledgeAtom, filter domain.InterviewKnowledgeAtomFilter) bool {
	if !matchesTrimmedFilter(atom.Status, filter.Status) {
		return false
	}
	if !matchesTrimmedFilter(atom.Domain, filter.Domain) {
		return false
	}
	if !matchesTrimmedFilter(atom.Difficulty, filter.Difficulty) {
		return false
	}
	if !matchesTrimmedFilter(atom.Category, filter.Category) {
		return false
	}
	if !matchesTrimmedFilter(atom.QuestionRole, filter.QuestionRole) {
		return false
	}
	if !matchesTrimmedFilter(atom.VectorStatus, filter.VectorStatus) {
		return false
	}
	return interviewKnowledgeAtomMatchesQuery(atom, filter.Query)
}

func interviewKnowledgeAtomMatchesQuery(atom domain.InterviewKnowledgeAtom, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	fields := []string{
		atom.ID,
		atom.Title,
		atom.Subject,
		atom.Domain,
		atom.Category,
		atom.SourceRef,
		strings.Join(atom.Tags, " "),
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), query) {
			return true
		}
	}
	return false
}

func matchesTrimmedFilter(value, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value), filter)
}

func interviewKnowledgeSummary(atoms []domain.InterviewKnowledgeAtom, batches []domain.InterviewKnowledgeBatch) domain.InterviewKnowledgeSummary {
	summary := domain.InterviewKnowledgeSummary{TotalAtoms: len(atoms), BatchCount: len(batches)}
	combinations := map[string]struct{}{}
	for _, atom := range atoms {
		switch strings.TrimSpace(atom.Status) {
		case "published":
			summary.PublishedAtoms++
			if atom.Category != "" && atom.Difficulty != "" {
				combinations[atom.Category+"|"+atom.Difficulty] = struct{}{}
			}
		case "archived":
			summary.ArchivedAtoms++
		default:
			summary.DraftAtoms++
		}
		switch strings.TrimSpace(atom.VectorStatus) {
		case "indexed":
			summary.VectorIndexedAtoms++
		case "failed":
			summary.VectorFailedAtoms++
		default:
			summary.VectorPendingAtoms++
		}
		if summary.LastEditedAt == nil || atom.UpdatedAt.After(*summary.LastEditedAt) {
			updatedAt := atom.UpdatedAt
			summary.LastEditedAt = &updatedAt
		}
	}
	for _, batch := range batches {
		if summary.LastImportedAt == nil || batch.CreatedAt.After(*summary.LastImportedAt) {
			createdAt := batch.CreatedAt
			summary.LastImportedAt = &createdAt
		}
	}
	summary.OpenCombinationCount = len(combinations)
	return summary
}
