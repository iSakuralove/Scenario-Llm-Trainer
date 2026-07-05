package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestMemorySaveInterviewKnowledgeAtomVersionedCreatesAndAdvancesVersions(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	atom := sampleInterviewKnowledgeAtom("atom-java-hashmap")

	saved, version, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "首次导入")
	if err != nil {
		t.Fatal(err)
	}
	if saved.CurrentVersion != 1 || version.Version != 1 {
		t.Fatalf("expected first version to be 1, got atom=%d version=%d", saved.CurrentVersion, version.Version)
	}

	saved, duplicateVersion, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionDuplicateImport, "admin-1", "重复导入留痕")
	if err != nil {
		t.Fatal(err)
	}
	if saved.CurrentVersion != 2 || duplicateVersion.Version != 2 {
		t.Fatalf("expected duplicate import to advance version, got atom=%d version=%d", saved.CurrentVersion, duplicateVersion.Version)
	}
	if !duplicateVersion.NoContentChange {
		t.Fatal("expected duplicate import with same content to mark no_content_change")
	}

	versions := store.ListInterviewKnowledgeAtomVersions(atom.ID)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected versions ordered by newest first, got %d then %d", versions[0].Version, versions[1].Version)
	}
}

func TestInterviewKnowledgeArchiveVersionTypeIsValid(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	atom := sampleInterviewKnowledgeAtom("atom-archive-version")
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "首次导入"); err != nil {
		t.Fatal(err)
	}
	atom.Status = "archived"
	saved, version, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionArchive, "admin-1", "过期归档")
	if err != nil {
		t.Fatalf("archive version type should be valid: %v", err)
	}
	if saved.Status != "archived" || saved.CurrentVersion != 2 || version.VersionType != domain.InterviewKnowledgeVersionArchive {
		t.Fatalf("unexpected archive save result atom=%+v version=%+v", saved, version)
	}
}

func TestInterviewKnowledgeAtomSnapshotExcludesRuntimeIndexFields(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	atom := sampleInterviewKnowledgeAtom("atom-database-index")
	atom.VectorStatus = "indexed"

	_, version, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionManualEdit, "admin-1", "补充标签")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(version.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := string(raw)
	for _, forbidden := range []string{"vector_status", "vectorStatus", "last_indexed_at", "lastIndexedAt"} {
		if strings.Contains(snapshot, forbidden) {
			t.Fatalf("snapshot must not include runtime index field %q: %s", forbidden, snapshot)
		}
	}
	for _, required := range []string{"sourceRef", "followUpPaths", "principles", "pitfalls"} {
		if !strings.Contains(snapshot, required) {
			t.Fatalf("snapshot must include standardized field %q: %s", required, snapshot)
		}
	}
}

func TestMemoryInterviewKnowledgeAtomReturnsClones(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	atom := sampleInterviewKnowledgeAtom("atom-cache-expire")
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "首次导入"); err != nil {
		t.Fatal(err)
	}

	found, ok := store.GetInterviewKnowledgeAtom(atom.ID)
	if !ok {
		t.Fatal("expected atom")
	}
	found.Tags[0] = "mutated"
	again, ok := store.GetInterviewKnowledgeAtom(atom.ID)
	if !ok {
		t.Fatal("expected atom")
	}
	if again.Tags[0] == "mutated" {
		t.Fatal("expected stored atom to be protected by clone")
	}

	versions := store.ListInterviewKnowledgeAtomVersions(atom.ID)
	versions[0].Snapshot.Principles[0] = "mutated"
	againVersions := store.ListInterviewKnowledgeAtomVersions(atom.ID)
	if againVersions[0].Snapshot.Principles[0] == "mutated" {
		t.Fatal("expected stored version snapshot to be protected by clone")
	}
}

func TestMemoryListInterviewKnowledgeAtomsFiltersVectorStatus(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	failed := sampleInterviewKnowledgeAtom("atom-vector-failed")
	failed.VectorStatus = "failed"
	indexed := sampleInterviewKnowledgeAtom("atom-vector-indexed")
	indexed.Category = "database"
	indexed.VectorStatus = "indexed"
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(failed, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "失败索引样例"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(indexed, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "已索引样例"); err != nil {
		t.Fatal(err)
	}

	items := store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{VectorStatus: "failed"})
	if len(items) != 1 || items[0].ID != failed.ID {
		t.Fatalf("expected only failed vector atom, got %+v", items)
	}
	items = store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{Category: "database", VectorStatus: "indexed"})
	if len(items) != 1 || items[0].ID != indexed.ID {
		t.Fatalf("expected indexed database atom, got %+v", items)
	}
}

func TestMemoryUpdateInterviewKnowledgeAtomIndexStatusDoesNotCreateVersion(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	atom := sampleInterviewKnowledgeAtom("atom-index-runtime-status")
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "首次导入"); err != nil {
		t.Fatal(err)
	}
	indexedAt := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	indexed, err := store.UpdateInterviewKnowledgeAtomIndexStatus(atom.ID, "indexed", &indexedAt)
	if err != nil {
		t.Fatalf("update indexed status: %v", err)
	}
	if indexed.VectorStatus != "indexed" || indexed.LastIndexedAt == nil || !indexed.LastIndexedAt.Equal(indexedAt) {
		t.Fatalf("unexpected indexed atom: %+v", indexed)
	}
	if versions := store.ListInterviewKnowledgeAtomVersions(atom.ID); len(versions) != 1 {
		t.Fatalf("runtime index status update must not create version, got %+v", versions)
	}

	failed, err := store.UpdateInterviewKnowledgeAtomIndexStatus(atom.ID, "failed", nil)
	if err != nil {
		t.Fatalf("update failed status: %v", err)
	}
	if failed.VectorStatus != "failed" || failed.LastIndexedAt == nil || !failed.LastIndexedAt.Equal(indexedAt) {
		t.Fatalf("failed status must preserve last_indexed_at, got %+v", failed)
	}
	if _, err := store.UpdateInterviewKnowledgeAtomIndexStatus(atom.ID, "broken", nil); err == nil {
		t.Fatal("expected invalid vector status error")
	}
	if _, err := store.UpdateInterviewKnowledgeAtomIndexStatus("missing-atom", "failed", nil); err != errInterviewKnowledgeAtomNotFound {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestMemoryInterviewKnowledgeBatchSummaryAndClone(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	atom := sampleInterviewKnowledgeAtom("atom-summary")
	atom.VectorStatus = "failed"
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "摘要样例"); err != nil {
		t.Fatal(err)
	}
	batch := store.SaveInterviewKnowledgeBatch(domain.InterviewKnowledgeBatch{
		ID:        "batch-summary",
		SourceRef: "fixture",
		Status:    "published",
		Mode:      "publish",
		AtomCount: 1,
		ValidationReport: map[string]interface{}{
			"summary": map[string]interface{}{"total": float64(1)},
		},
		AdminID: "admin-1",
	})
	batch.ValidationReport["summary"].(map[string]interface{})["total"] = float64(99)

	batches := store.ListInterviewKnowledgeBatches(10)
	if len(batches) != 1 {
		t.Fatalf("expected one batch, got %+v", batches)
	}
	if batches[0].ValidationReport["summary"].(map[string]interface{})["total"] != float64(1) {
		t.Fatalf("batch report should be cloned on save/list, got %+v", batches[0].ValidationReport)
	}
	batches[0].ValidationReport["summary"].(map[string]interface{})["total"] = float64(88)
	again := store.ListInterviewKnowledgeBatches(10)
	if again[0].ValidationReport["summary"].(map[string]interface{})["total"] != float64(1) {
		t.Fatalf("batch report should be cloned on list, got %+v", again[0].ValidationReport)
	}

	summary := store.InterviewKnowledgeSummary()
	if summary.TotalAtoms != 1 || summary.BatchCount != 1 || summary.VectorFailedAtoms != 1 {
		t.Fatalf("unexpected interview knowledge summary: %+v", summary)
	}
	if summary.LastImportedAt == nil || summary.LastEditedAt == nil || summary.OpenCombinationCount != 1 {
		t.Fatalf("expected imported/edited timestamps and one open combination, got %+v", summary)
	}
}

func TestMemoryInterviewRetrievalLogsFilterAnalyticsAndClone(t *testing.T) {
	store := NewMemoryStore(func(password string) string { return "hash:" + password })
	hitAtom := sampleInterviewKnowledgeAtom("atom-retrieval-hit")
	hitAtom.QuestionRole = "followup"
	hitAtom.VectorStatus = "indexed"
	lowAtom := sampleInterviewKnowledgeAtom("atom-retrieval-low")
	lowAtom.QuestionRole = "followup"
	lowAtom.VectorStatus = "indexed"
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(hitAtom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "命中样例"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveInterviewKnowledgeAtomVersioned(lowAtom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "低命中样例"); err != nil {
		t.Fatal(err)
	}
	store.InterviewSessions["session-retrieval"] = &domain.InterviewSession{
		ID: "session-retrieval",
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "java",
			Difficulty: "L3",
		},
	}

	saved := store.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID: "session-retrieval",
		Round:     2,
		QueryText: strings.Repeat("问", 520),
		MatchedAtoms: []domain.InterviewKnowledgeAtomLightSnapshot{{
			AtomID:   hitAtom.ID,
			Version:  1,
			Title:    hitAtom.Title,
			Subject:  hitAtom.Subject,
			Domain:   hitAtom.Domain,
			Category: hitAtom.Category,
		}},
		CreatedAt: time.Now().Add(-time.Minute),
	})
	if len([]rune(saved.QueryText)) != 500 {
		t.Fatalf("expected query text truncated to 500 runes, got %d", len([]rune(saved.QueryText)))
	}
	store.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID:    "session-retrieval",
		Round:        3,
		QueryText:    "没有命中",
		FallbackUsed: true,
		ErrorMessage: "未命中可用题库追问原子，继续使用规则追问。",
		CreatedAt:    time.Now(),
	})

	fallbackOnly := true
	logs := store.ListInterviewRetrievalLogs(domain.InterviewRetrievalLogFilter{
		Category:     "java",
		Difficulty:   "L3",
		FallbackUsed: &fallbackOnly,
		Limit:        10,
	})
	if len(logs) != 1 || !logs[0].FallbackUsed {
		t.Fatalf("expected one fallback log, got %+v", logs)
	}
	logs[0].MatchedAtoms = append(logs[0].MatchedAtoms, domain.InterviewKnowledgeAtomLightSnapshot{AtomID: "mutated"})
	again := store.ListInterviewRetrievalLogs(domain.InterviewRetrievalLogFilter{Limit: 10})
	if len(again) != 2 || len(again[0].MatchedAtoms) != 0 {
		t.Fatalf("retrieval logs must be cloned on list, got %+v", again)
	}

	analytics := store.InterviewRetrievalAnalytics(domain.InterviewRetrievalLogFilter{
		Category:   "java",
		Difficulty: "L3",
		Limit:      10,
	})
	if analytics.TotalLogs != 2 || analytics.HitLogs != 1 || analytics.FallbackLogs != 1 || analytics.HitRate != 0.5 || analytics.FallbackRate != 0.5 {
		t.Fatalf("unexpected analytics counters: %+v", analytics)
	}
	if len(analytics.TopHitAtoms) != 1 || analytics.TopHitAtoms[0].AtomID != hitAtom.ID || analytics.TopHitAtoms[0].HitCount != 1 {
		t.Fatalf("unexpected top hit atoms: %+v", analytics.TopHitAtoms)
	}
	if len(analytics.LowHitAtoms) == 0 || analytics.LowHitAtoms[0].AtomID != lowAtom.ID || analytics.LowHitAtoms[0].HitCount != 0 {
		t.Fatalf("expected low hit atom first, got %+v", analytics.LowHitAtoms)
	}
	if len(analytics.FallbackCombinations) != 1 || analytics.FallbackCombinations[0].Count != 1 || analytics.FallbackCombinations[0].Category != "java" {
		t.Fatalf("unexpected fallback combinations: %+v", analytics.FallbackCombinations)
	}
	if len(analytics.RecentFallbacks) != 1 || analytics.RecentFallbacks[0].ErrorMessage == "" {
		t.Fatalf("expected recent fallback reason, got %+v", analytics.RecentFallbacks)
	}
}

func sampleInterviewKnowledgeAtom(id string) domain.InterviewKnowledgeAtom {
	return domain.InterviewKnowledgeAtom{
		ID:            id,
		Title:         "HashMap 扩容机制",
		Subject:       "HashMap 扩容机制",
		Domain:        "backend",
		Difficulty:    "L3",
		Category:      "java",
		QuestionRole:  "mixed",
		SourceRef:     "manual-curation",
		Tags:          []string{"java", "hashmap"},
		Principles:    []string{"说明负载因子与扩容阈值", "说明数组迁移和链表拆分过程"},
		Pitfalls:      []string{"只背容量翻倍但不解释触发条件", "忽略并发修改风险"},
		FollowUpPaths: []string{"追问 resize 对性能的影响", "追问 HashMap 与 ConcurrentHashMap 的区别"},
		Status:        "published",
	}
}
