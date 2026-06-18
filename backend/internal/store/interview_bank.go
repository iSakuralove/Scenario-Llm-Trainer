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

func prepareInterviewKnowledgeAtomForVersion(atom domain.InterviewKnowledgeAtom, existing *domain.InterviewKnowledgeAtom, nextVersion int, now time.Time) domain.InterviewKnowledgeAtom {
	atom.ID = strings.TrimSpace(atom.ID)
	atom.Title = strings.TrimSpace(atom.Title)
	atom.Subject = strings.TrimSpace(atom.Subject)
	atom.Domain = strings.TrimSpace(atom.Domain)
	atom.Difficulty = strings.TrimSpace(atom.Difficulty)
	atom.Category = strings.TrimSpace(atom.Category)
	atom.QuestionRole = strings.TrimSpace(atom.QuestionRole)
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
		ID:            atom.ID,
		Title:         atom.Title,
		Subject:       atom.Subject,
		Domain:        atom.Domain,
		Difficulty:    atom.Difficulty,
		Category:      atom.Category,
		QuestionRole:  atom.QuestionRole,
		SourceRef:     atom.SourceRef,
		Tags:          append([]string{}, atom.Tags...),
		Principles:    append([]string{}, atom.Principles...),
		Pitfalls:      append([]string{}, atom.Pitfalls...),
		FollowUpPaths: append([]string{}, atom.FollowUpPaths...),
		Status:        atom.Status,
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
