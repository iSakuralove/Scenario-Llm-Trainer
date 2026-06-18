package store

import (
	"encoding/json"
	"strings"
	"testing"

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
