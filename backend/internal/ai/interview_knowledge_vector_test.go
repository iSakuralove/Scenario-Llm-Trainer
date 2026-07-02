package ai

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestBuildInterviewKnowledgeVectorDocuments(t *testing.T) {
	atom := sampleInterviewKnowledgeVectorAtom()
	docs := BuildInterviewKnowledgeVectorDocuments(atom)
	if len(docs) != 7 {
		t.Fatalf("expected overview plus six detail docs, got %d: %+v", len(docs), docs)
	}
	if docs[0].DocType != InterviewKnowledgeVectorDocOverview || docs[0].DocKey != "overview" {
		t.Fatalf("expected overview doc first, got %+v", docs[0])
	}
	if docs[1].DocType != InterviewKnowledgeVectorDocPrinciple || docs[3].DocType != InterviewKnowledgeVectorDocPitfall || docs[5].DocType != InterviewKnowledgeVectorDocFollowUp {
		t.Fatalf("unexpected doc type order: %+v", docs)
	}
	for _, doc := range docs {
		if doc.AtomID != atom.ID || doc.AtomVersion != atom.CurrentVersion {
			t.Fatalf("unexpected atom identity in doc: %+v", doc)
		}
		if doc.DocText == "" || doc.TextHash == "" || doc.Status != "active" {
			t.Fatalf("expected normalized active doc with hash: %+v", doc)
		}
		if doc.Metadata["domain"] != atom.Domain || doc.Metadata["category"] != atom.Category {
			t.Fatalf("expected base metadata in doc: %+v", doc.Metadata)
		}
	}
	if docs[1].Metadata["index"] != "1" || docs[6].Metadata["index"] != "2" {
		t.Fatalf("expected detail document indexes, got first=%+v last=%+v", docs[1].Metadata, docs[6].Metadata)
	}
}

func TestBuildInterviewKnowledgeVectorDocumentsSkipsNonPublished(t *testing.T) {
	atom := sampleInterviewKnowledgeVectorAtom()
	atom.Status = "draft"
	if docs := BuildInterviewKnowledgeVectorDocuments(atom); len(docs) != 0 {
		t.Fatalf("draft atom must not be indexed: %+v", docs)
	}
	atom.Status = "published"
	atom.ID = ""
	if docs := BuildInterviewKnowledgeVectorDocuments(atom); len(docs) != 0 {
		t.Fatalf("atom without id must not be indexed: %+v", docs)
	}
}

func sampleInterviewKnowledgeVectorAtom() domain.InterviewKnowledgeAtom {
	return domain.InterviewKnowledgeAtom{
		ID:             "atom-vector-cache",
		Title:          "缓存击穿治理",
		Subject:        "缓存击穿治理",
		Domain:         "backend",
		Difficulty:     "L3",
		Category:       "cache",
		QuestionRole:   "mixed",
		SourceRef:      "fixture/cache-breakdown",
		Tags:           []string{"cache", "hot-key"},
		Principles:     []string{"说明互斥锁或 singleflight 控制并发回源", "说明热点 key 预热和过期时间抖动"},
		Pitfalls:       []string{"只说加缓存但不处理失效瞬间并发", "忽略数据库被瞬时流量打满的风险"},
		FollowUpPaths:  []string{"追问缓存雪崩和缓存穿透的差异", "追问本地缓存与分布式缓存的一致性取舍"},
		Status:         "published",
		CurrentVersion: 3,
	}
}
