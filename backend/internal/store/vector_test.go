package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
)

func TestMemoryVectorStoreSearchAndDeleteByQuestion(t *testing.T) {
	vectorStore := NewMemoryVectorStore()
	docs := []ai.ScenarioVectorDocument{
		{QuestionID: "q1", SourceVersion: 1, DocType: ai.VectorDocRootCause, DocKey: "root", DocText: "联合索引缺失导致全表扫描", TextHash: "h1", Vector: []float64{1, 0}, Status: "active"},
		{QuestionID: "q1", SourceVersion: 1, DocType: ai.VectorDocDistractor, DocKey: "distractor:x1", DocText: "网络正常", TextHash: "h2", Vector: []float64{0, 1}, Status: "active"},
	}

	if err := vectorStore.UpsertDocuments(context.Background(), docs); err != nil {
		t.Fatalf("upsert docs: %v", err)
	}
	results, err := vectorStore.Search(context.Background(), VectorSearchQuery{
		QuestionID: "q1",
		Vector:     []float64{0.9, 0.1},
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("search docs: %v", err)
	}
	if len(results) != 2 || results[0].Document.DocType != ai.VectorDocRootCause || results[0].Score <= results[1].Score {
		t.Fatalf("unexpected search results: %#v", results)
	}

	if err := vectorStore.DeleteByQuestion(context.Background(), "q1"); err != nil {
		t.Fatalf("delete docs: %v", err)
	}
	results, err = vectorStore.Search(context.Background(), VectorSearchQuery{QuestionID: "q1", Vector: []float64{1, 0}, Limit: 2})
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results after delete, got %#v", results)
	}
}

func TestMemoryVectorStoreEmptyTextSearchReturnsNoResults(t *testing.T) {
	vectorStore := NewMemoryVectorStore()
	if err := vectorStore.UpsertDocuments(context.Background(), []ai.ScenarioVectorDocument{
		{QuestionID: "q1", SourceVersion: 1, DocType: ai.VectorDocEvidence, DocKey: "evidence:1", DocText: "关键证据", TextHash: "h1", Status: "active"},
	}); err != nil {
		t.Fatalf("upsert docs: %v", err)
	}
	results, err := vectorStore.Search(context.Background(), VectorSearchQuery{QuestionID: "q1", Text: " ", Limit: 5})
	if err != nil {
		t.Fatalf("empty text search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("empty text search must not return pseudo matches: %#v", results)
	}
}

func TestMemoryStoreIndexesOnlyActiveScenarios(t *testing.T) {
	dataStore := NewMemoryStore(auth.HashPassword)
	vectorStore := NewMemoryVectorStore()
	dataStore.SetVectorStore(vectorStore)

	active := domain.ScenarioQuestion{
		ID:          "active-vector-test",
		Title:       "正式题",
		Description: "正式题描述",
		Domain:      "database",
		Difficulty:  "L2",
		Status:      "active",
		Version:     1,
		CreatedAt:   time.Now().Add(-time.Hour),
		Content: domain.ScenarioContent{
			RootCause:         "缓存 key 变更导致回源风暴",
			RootCauseKeywords: []string{"缓存", "回源"},
			KeyEvidence:       []string{"回源流量上升"},
		},
	}
	draft := active
	draft.ID = "draft-vector-test"
	draft.Status = "pending_review"

	dataStore.AddScenario(active)
	dataStore.AddScenario(draft)

	results, err := vectorStore.Search(context.Background(), VectorSearchQuery{
		QuestionID: active.ID,
		Text:       "回源流量上升",
		DocTypes:   []string{ai.VectorDocRootCause},
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("search active docs: %v", err)
	}
	if len(results) != 1 || results[0].Document.QuestionID != active.ID {
		t.Fatalf("expected active scenario indexed, got %#v", results)
	}

	draftResults, err := vectorStore.Search(context.Background(), VectorSearchQuery{
		QuestionID: draft.ID,
		Text:       "回源流量上升",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("search draft docs: %v", err)
	}
	if len(draftResults) != 0 {
		t.Fatalf("draft scenario must not be indexed: %#v", draftResults)
	}

	active.Status = "archived"
	archived := dataStore.AddScenario(active)
	if archived.CreatedAt.IsZero() || !archived.CreatedAt.Equal(active.CreatedAt) {
		t.Fatalf("updating scenario status should preserve created_at, got %+v want %v", archived.CreatedAt, active.CreatedAt)
	}
	archivedResults, err := vectorStore.Search(context.Background(), VectorSearchQuery{
		QuestionID: active.ID,
		Text:       "回源流量上升",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("search archived docs: %v", err)
	}
	if len(archivedResults) != 0 {
		t.Fatalf("archived scenario must be removed from vector index: %#v", archivedResults)
	}
}

func TestMemoryStoreExposesDefaultVectorStoreForSeedScenarios(t *testing.T) {
	dataStore := NewMemoryStore(auth.HashPassword)
	vectorStore := dataStore.VectorStore()
	if vectorStore == nil {
		t.Fatal("expected default memory vector store")
	}
	question := dataStore.ListScenarios("database", "", "")[0]
	results, err := vectorStore.Search(context.Background(), VectorSearchQuery{
		QuestionID: question.ID,
		Text:       question.Content.RootCause,
		DocTypes:   []string{ai.VectorDocRootCause},
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("search seed docs: %v", err)
	}
	if len(results) != 1 || results[0].Document.DocType != ai.VectorDocRootCause {
		t.Fatalf("expected seeded active scenario vector doc, got %#v", results)
	}
}

func TestVectorSchemaIncludesPgvectorTableAndDegradesWhenExtensionUnavailable(t *testing.T) {
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS scenario_vector_documents",
		"embedding vector",
		"embedding_dim INT",
		"metadata JSONB",
		"CREATE INDEX IF NOT EXISTS scenario_vector_documents_embedding_hnsw",
	} {
		if !strings.Contains(VectorSchemaSQL, required) {
			t.Fatalf("vector schema must include %q", required)
		}
	}
}
