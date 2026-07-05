package store

import (
	"sort"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

const (
	interviewRetrievalLogQueryMaxRunes    = 500
	interviewRetrievalLogErrorMaxRunes    = 300
	interviewRetrievalLogListDefaultLimit = 50
	interviewRetrievalLogListMaxLimit     = 200
	interviewRetrievalAnalyticsDefault    = 500
	interviewRetrievalAnalyticsMax        = 1000
)

type retrievalAtomLookup func(string) (domain.InterviewKnowledgeAtom, bool)
type retrievalSessionLookup func(string) (domain.InterviewQuestionSnapshot, bool)

type retrievalCombination struct {
	Domain     string
	Category   string
	Difficulty string
}

func prepareInterviewRetrievalLogForSave(log domain.InterviewRetrievalLog, now time.Time) domain.InterviewRetrievalLog {
	log.ID = strings.TrimSpace(log.ID)
	if log.ID == "" {
		log.ID = NewID()
	}
	log.SessionID = strings.TrimSpace(log.SessionID)
	if log.Round <= 0 {
		log.Round = 1
	}
	log.QueryText = truncateStringRunes(strings.TrimSpace(log.QueryText), interviewRetrievalLogQueryMaxRunes)
	log.ErrorMessage = truncateStringRunes(strings.TrimSpace(log.ErrorMessage), interviewRetrievalLogErrorMaxRunes)
	log.MatchedAtoms = cloneInterviewKnowledgeAtomLightSnapshots(log.MatchedAtoms)
	if log.CreatedAt.IsZero() {
		log.CreatedAt = now
	}
	return log
}

func cloneInterviewRetrievalLog(log domain.InterviewRetrievalLog) domain.InterviewRetrievalLog {
	log.MatchedAtoms = cloneInterviewKnowledgeAtomLightSnapshots(log.MatchedAtoms)
	return log
}

func normalizeRetrievalLogListLimit(limit int) int {
	return normalizeRetrievalLogLimit(limit, interviewRetrievalLogListDefaultLimit, interviewRetrievalLogListMaxLimit)
}

func normalizeRetrievalAnalyticsLimit(limit int) int {
	return normalizeRetrievalLogLimit(limit, interviewRetrievalAnalyticsDefault, interviewRetrievalAnalyticsMax)
}

func normalizeRetrievalLogLimit(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func retrievalLogNeedsContextFilter(filter domain.InterviewRetrievalLogFilter) bool {
	return strings.TrimSpace(filter.Domain) != "" || strings.TrimSpace(filter.Category) != "" || strings.TrimSpace(filter.Difficulty) != ""
}

func interviewRetrievalLogMatchesFilter(log domain.InterviewRetrievalLog, filter domain.InterviewRetrievalLogFilter, atomByID retrievalAtomLookup, sessionByID retrievalSessionLookup) bool {
	if filter.FallbackUsed != nil && log.FallbackUsed != *filter.FallbackUsed {
		return false
	}
	if !retrievalLogNeedsContextFilter(filter) {
		return true
	}
	for _, combo := range interviewRetrievalLogCombinations(log, atomByID, sessionByID) {
		if retrievalCombinationMatchesFilter(combo, filter) {
			return true
		}
	}
	return false
}

func retrievalCombinationMatchesFilter(combo retrievalCombination, filter domain.InterviewRetrievalLogFilter) bool {
	if !matchesTrimmedFilter(combo.Domain, filter.Domain) {
		return false
	}
	if !matchesTrimmedFilter(combo.Category, filter.Category) {
		return false
	}
	return matchesTrimmedFilter(combo.Difficulty, filter.Difficulty)
}

func interviewRetrievalLogCombinations(log domain.InterviewRetrievalLog, atomByID retrievalAtomLookup, sessionByID retrievalSessionLookup) []retrievalCombination {
	seen := map[string]bool{}
	out := []retrievalCombination{}
	add := func(combo retrievalCombination) {
		combo.Domain = strings.TrimSpace(combo.Domain)
		combo.Category = strings.TrimSpace(combo.Category)
		combo.Difficulty = strings.TrimSpace(combo.Difficulty)
		if combo.Domain == "" && combo.Category == "" && combo.Difficulty == "" {
			return
		}
		key := retrievalCombinationKey(combo)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, combo)
	}
	for _, matched := range log.MatchedAtoms {
		combo := retrievalCombination{
			Domain:   matched.Domain,
			Category: matched.Category,
		}
		if atomByID != nil {
			if atom, ok := atomByID(matched.AtomID); ok {
				combo.Domain = firstNonEmptyString(atom.Domain, combo.Domain)
				combo.Category = firstNonEmptyString(atom.Category, combo.Category)
				combo.Difficulty = atom.Difficulty
			}
		}
		add(combo)
	}
	if sessionByID != nil {
		if snapshot, ok := sessionByID(log.SessionID); ok {
			add(retrievalCombination{
				Domain:     snapshot.Domain,
				Category:   snapshot.Category,
				Difficulty: snapshot.Difficulty,
			})
		}
	}
	return out
}

func interviewRetrievalAnalytics(logs []domain.InterviewRetrievalLog, atoms []domain.InterviewKnowledgeAtom, filter domain.InterviewRetrievalLogFilter, atomByID retrievalAtomLookup, sessionByID retrievalSessionLookup) domain.InterviewRetrievalAnalytics {
	analytics := domain.InterviewRetrievalAnalytics{
		TopHitAtoms:          []domain.InterviewRetrievalAtomHit{},
		LowHitAtoms:          []domain.InterviewRetrievalAtomHit{},
		FallbackCombinations: []domain.InterviewRetrievalFallbackCombination{},
		RecentFallbacks:      []domain.InterviewRetrievalLog{},
	}
	analytics.TotalLogs = len(logs)
	hits := map[string]domain.InterviewRetrievalAtomHit{}
	fallbacks := map[string]domain.InterviewRetrievalFallbackCombination{}
	for _, log := range logs {
		if log.FallbackUsed {
			analytics.FallbackLogs++
			if len(analytics.RecentFallbacks) < 5 {
				analytics.RecentFallbacks = append(analytics.RecentFallbacks, cloneInterviewRetrievalLog(log))
			}
			combo := fallbackCombinationFromLog(log, sessionByID)
			key := retrievalCombinationKey(combo)
			item := fallbacks[key]
			if item.Count == 0 {
				item.Domain = combo.Domain
				item.Category = combo.Category
				item.Difficulty = combo.Difficulty
			}
			item.Count++
			if item.LastSeenAt == nil || log.CreatedAt.After(*item.LastSeenAt) {
				seenAt := log.CreatedAt
				item.LastSeenAt = &seenAt
				item.RecentReason = log.ErrorMessage
			}
			fallbacks[key] = item
		}
		if len(log.MatchedAtoms) > 0 && !log.FallbackUsed {
			analytics.HitLogs++
		}
		for _, matched := range log.MatchedAtoms {
			atomID := strings.TrimSpace(matched.AtomID)
			if atomID == "" {
				continue
			}
			hit := hits[atomID]
			if hit.AtomID == "" {
				hit = retrievalAtomHitFromSnapshot(matched)
				if atomByID != nil {
					if atom, ok := atomByID(atomID); ok {
						hit = retrievalAtomHitFromAtom(atom)
					}
				}
			}
			hit.HitCount++
			if hit.LastHitAt == nil || log.CreatedAt.After(*hit.LastHitAt) {
				hitAt := log.CreatedAt
				hit.LastHitAt = &hitAt
			}
			hits[atomID] = hit
		}
	}
	if analytics.TotalLogs > 0 {
		analytics.HitRate = float64(analytics.HitLogs) / float64(analytics.TotalLogs)
		analytics.FallbackRate = float64(analytics.FallbackLogs) / float64(analytics.TotalLogs)
	}
	analytics.TopHitAtoms = sortedTopRetrievalHits(hits, 8)
	analytics.LowHitAtoms = sortedLowRetrievalHits(atoms, hits, filter, 8)
	analytics.FallbackCombinations = sortedFallbackCombinations(fallbacks, 8)
	return analytics
}

func retrievalAtomHitFromSnapshot(snapshot domain.InterviewKnowledgeAtomLightSnapshot) domain.InterviewRetrievalAtomHit {
	return domain.InterviewRetrievalAtomHit{
		AtomID:   snapshot.AtomID,
		Version:  snapshot.Version,
		Title:    snapshot.Title,
		Subject:  snapshot.Subject,
		Domain:   snapshot.Domain,
		Category: snapshot.Category,
	}
}

func retrievalAtomHitFromAtom(atom domain.InterviewKnowledgeAtom) domain.InterviewRetrievalAtomHit {
	return domain.InterviewRetrievalAtomHit{
		AtomID:       atom.ID,
		Version:      maxIntStore(atom.CurrentVersion, 1),
		Title:        atom.Title,
		Subject:      atom.Subject,
		Domain:       atom.Domain,
		Category:     atom.Category,
		Difficulty:   atom.Difficulty,
		QuestionRole: atom.QuestionRole,
	}
}

func fallbackCombinationFromLog(log domain.InterviewRetrievalLog, sessionByID retrievalSessionLookup) retrievalCombination {
	if sessionByID != nil {
		if snapshot, ok := sessionByID(log.SessionID); ok {
			return retrievalCombination{
				Domain:     strings.TrimSpace(snapshot.Domain),
				Category:   strings.TrimSpace(snapshot.Category),
				Difficulty: strings.TrimSpace(snapshot.Difficulty),
			}
		}
	}
	combos := interviewRetrievalLogCombinations(log, nil, nil)
	if len(combos) > 0 {
		return combos[0]
	}
	return retrievalCombination{}
}

func sortedTopRetrievalHits(hits map[string]domain.InterviewRetrievalAtomHit, limit int) []domain.InterviewRetrievalAtomHit {
	items := make([]domain.InterviewRetrievalAtomHit, 0, len(hits))
	for _, hit := range hits {
		items = append(items, hit)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].HitCount != items[j].HitCount {
			return items[i].HitCount > items[j].HitCount
		}
		if items[i].LastHitAt != nil && items[j].LastHitAt != nil && !items[i].LastHitAt.Equal(*items[j].LastHitAt) {
			return items[i].LastHitAt.After(*items[j].LastHitAt)
		}
		return items[i].AtomID < items[j].AtomID
	})
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func sortedLowRetrievalHits(atoms []domain.InterviewKnowledgeAtom, hits map[string]domain.InterviewRetrievalAtomHit, filter domain.InterviewRetrievalLogFilter, limit int) []domain.InterviewRetrievalAtomHit {
	items := []domain.InterviewRetrievalAtomHit{}
	for _, atom := range atoms {
		if !retrievalAtomEligibleForLowHit(atom, filter) {
			continue
		}
		hit := retrievalAtomHitFromAtom(atom)
		if existing, ok := hits[atom.ID]; ok {
			hit.HitCount = existing.HitCount
			hit.LastHitAt = existing.LastHitAt
		}
		items = append(items, hit)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].HitCount != items[j].HitCount {
			return items[i].HitCount < items[j].HitCount
		}
		if items[i].LastHitAt == nil && items[j].LastHitAt != nil {
			return true
		}
		if items[i].LastHitAt != nil && items[j].LastHitAt == nil {
			return false
		}
		if items[i].LastHitAt != nil && items[j].LastHitAt != nil && !items[i].LastHitAt.Equal(*items[j].LastHitAt) {
			return items[i].LastHitAt.Before(*items[j].LastHitAt)
		}
		return items[i].AtomID < items[j].AtomID
	})
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func retrievalAtomEligibleForLowHit(atom domain.InterviewKnowledgeAtom, filter domain.InterviewRetrievalLogFilter) bool {
	if !strings.EqualFold(strings.TrimSpace(atom.Status), "published") {
		return false
	}
	role := strings.TrimSpace(atom.QuestionRole)
	if role != "followup" && role != "mixed" {
		return false
	}
	return retrievalCombinationMatchesFilter(retrievalCombination{
		Domain:     atom.Domain,
		Category:   atom.Category,
		Difficulty: atom.Difficulty,
	}, filter)
}

func sortedFallbackCombinations(items map[string]domain.InterviewRetrievalFallbackCombination, limit int) []domain.InterviewRetrievalFallbackCombination {
	out := make([]domain.InterviewRetrievalFallbackCombination, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].LastSeenAt != nil && out[j].LastSeenAt != nil && !out[i].LastSeenAt.Equal(*out[j].LastSeenAt) {
			return out[i].LastSeenAt.After(*out[j].LastSeenAt)
		}
		return retrievalFallbackCombinationSortKey(out[i]) < retrievalFallbackCombinationSortKey(out[j])
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func retrievalFallbackCombinationSortKey(item domain.InterviewRetrievalFallbackCombination) string {
	return item.Domain + "|" + item.Category + "|" + item.Difficulty
}

func retrievalCombinationKey(combo retrievalCombination) string {
	return strings.TrimSpace(combo.Domain) + "|" + strings.TrimSpace(combo.Category) + "|" + strings.TrimSpace(combo.Difficulty)
}

func truncateStringRunes(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	if maxRunes <= 0 {
		return ""
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func maxIntStore(left, right int) int {
	if left > right {
		return left
	}
	return right
}
