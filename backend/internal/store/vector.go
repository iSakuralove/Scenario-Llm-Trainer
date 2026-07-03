package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"situational-teaching/backend/internal/ai"
)

const VectorSchemaSQL = `
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS scenario_vector_documents (
    id TEXT PRIMARY KEY,
    question_id TEXT NOT NULL,
    source_version INT NOT NULL,
    doc_type TEXT NOT NULL,
    doc_key TEXT NOT NULL,
    doc_text TEXT NOT NULL,
    text_hash TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    embedding_model TEXT,
    embedding_dim INT,
    embedding vector(1536),
    status TEXT DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(question_id, source_version, doc_type, doc_key)
);

ALTER TABLE scenario_vector_documents
    ALTER COLUMN embedding TYPE vector(1536)
    USING embedding::vector(1536);

CREATE INDEX IF NOT EXISTS scenario_vector_documents_question_idx
    ON scenario_vector_documents(question_id, doc_type, status);

CREATE INDEX IF NOT EXISTS scenario_vector_documents_embedding_hnsw
    ON scenario_vector_documents USING hnsw (embedding vector_cosine_ops);

CREATE TABLE IF NOT EXISTS interview_knowledge_vector_documents (
    id TEXT PRIMARY KEY,
    atom_id TEXT NOT NULL REFERENCES interview_knowledge_atoms(id) ON DELETE CASCADE,
    atom_version INT NOT NULL,
    doc_type TEXT NOT NULL,
    doc_key TEXT NOT NULL,
    doc_text TEXT NOT NULL,
    text_hash TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    embedding_model TEXT,
    embedding_dim INT,
    embedding vector(1536),
    status TEXT DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(atom_id, atom_version, doc_type, doc_key)
);

ALTER TABLE interview_knowledge_vector_documents
    ALTER COLUMN embedding TYPE vector(1536)
    USING embedding::vector(1536);

CREATE INDEX IF NOT EXISTS interview_knowledge_vector_documents_atom_idx
    ON interview_knowledge_vector_documents(atom_id, doc_type, status);

CREATE INDEX IF NOT EXISTS interview_knowledge_vector_documents_embedding_hnsw
    ON interview_knowledge_vector_documents USING hnsw (embedding vector_cosine_ops);
`

type VectorStore interface {
	UpsertDocuments(context.Context, []ai.ScenarioVectorDocument) error
	UpsertInterviewKnowledgeDocuments(context.Context, []ai.InterviewKnowledgeVectorDocument) error
	Search(context.Context, VectorSearchQuery) ([]VectorSearchResult, error)
	SearchInterviewKnowledge(context.Context, InterviewKnowledgeVectorSearchQuery) ([]InterviewKnowledgeVectorSearchResult, error)
	DeleteByQuestion(context.Context, string) error
	DeleteInterviewKnowledgeByAtom(context.Context, string) error
	RebuildScenarioIndex(context.Context, []ai.ScenarioVectorDocument) error
	RebuildInterviewKnowledgeIndex(context.Context, []ai.InterviewKnowledgeVectorDocument) error
}

type VectorSearchQuery struct {
	QuestionID string
	DocTypes   []string
	Text       string
	Vector     []float64
	Limit      int
}

type VectorSearchResult struct {
	Document ai.ScenarioVectorDocument
	Score    float64
}

type InterviewKnowledgeVectorSearchQuery struct {
	Category      string
	Difficulty    string
	QuestionRoles []string
	Text          string
	Vector        []float64
	Limit         int
}

type InterviewKnowledgeVectorSearchResult struct {
	Document ai.InterviewKnowledgeVectorDocument
	Score    float64
}

type MemoryVectorStore struct {
	mu            sync.RWMutex
	docs          map[string]ai.ScenarioVectorDocument
	knowledgeDocs map[string]ai.InterviewKnowledgeVectorDocument
}

func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{
		docs:          map[string]ai.ScenarioVectorDocument{},
		knowledgeDocs: map[string]ai.InterviewKnowledgeVectorDocument{},
	}
}

func (s *MemoryVectorStore) UpsertDocuments(_ context.Context, docs []ai.ScenarioVectorDocument) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.docs == nil {
		s.docs = map[string]ai.ScenarioVectorDocument{}
	}
	for _, doc := range docs {
		if strings.TrimSpace(doc.QuestionID) == "" || strings.TrimSpace(doc.DocType) == "" || strings.TrimSpace(doc.DocKey) == "" {
			continue
		}
		s.docs[vectorDocID(doc)] = cloneVectorDocument(doc)
	}
	return nil
}

func (s *MemoryVectorStore) UpsertInterviewKnowledgeDocuments(_ context.Context, docs []ai.InterviewKnowledgeVectorDocument) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.knowledgeDocs == nil {
		s.knowledgeDocs = map[string]ai.InterviewKnowledgeVectorDocument{}
	}
	for _, doc := range docs {
		if strings.TrimSpace(doc.AtomID) == "" || strings.TrimSpace(doc.DocType) == "" || strings.TrimSpace(doc.DocKey) == "" {
			continue
		}
		s.knowledgeDocs[interviewKnowledgeVectorDocID(doc)] = cloneInterviewKnowledgeVectorDocument(doc)
	}
	return nil
}

func (s *MemoryVectorStore) Search(_ context.Context, query VectorSearchQuery) ([]VectorSearchResult, error) {
	if s == nil {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	docTypes := map[string]bool{}
	for _, docType := range query.DocTypes {
		if strings.TrimSpace(docType) != "" {
			docTypes[docType] = true
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := []VectorSearchResult{}
	for _, doc := range s.docs {
		if query.QuestionID != "" && doc.QuestionID != query.QuestionID {
			continue
		}
		if len(docTypes) > 0 && !docTypes[doc.DocType] {
			continue
		}
		score := vectorDocumentScore(query, doc)
		if score <= 0 {
			continue
		}
		results = append(results, VectorSearchResult{Document: cloneVectorDocument(doc), Score: score})
	}
	sortVectorResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *MemoryVectorStore) SearchInterviewKnowledge(_ context.Context, query InterviewKnowledgeVectorSearchQuery) ([]InterviewKnowledgeVectorSearchResult, error) {
	if s == nil {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	roleSet := map[string]bool{}
	for _, role := range query.QuestionRoles {
		if trimmed := strings.TrimSpace(role); trimmed != "" {
			roleSet[trimmed] = true
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := []InterviewKnowledgeVectorSearchResult{}
	for _, doc := range s.knowledgeDocs {
		if query.Category != "" && doc.Metadata["category"] != query.Category {
			continue
		}
		if query.Difficulty != "" && doc.Metadata["difficulty"] != query.Difficulty {
			continue
		}
		if len(roleSet) > 0 && !roleSet[doc.Metadata["question_role"]] {
			continue
		}
		score := interviewKnowledgeVectorDocumentScore(query, doc)
		if score <= 0 {
			continue
		}
		results = append(results, InterviewKnowledgeVectorSearchResult{
			Document: cloneInterviewKnowledgeVectorDocument(doc),
			Score:    score,
		})
	}
	sortInterviewKnowledgeVectorResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func sortVectorResults(results []VectorSearchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Document.DocKey < results[j].Document.DocKey
		}
		return results[i].Score > results[j].Score
	})
}

func sortInterviewKnowledgeVectorResults(results []InterviewKnowledgeVectorSearchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Document.DocKey < results[j].Document.DocKey
		}
		return results[i].Score > results[j].Score
	})
}

func (s *MemoryVectorStore) DeleteByQuestion(_ context.Context, questionID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, doc := range s.docs {
		if doc.QuestionID == questionID {
			delete(s.docs, id)
		}
	}
	return nil
}

func (s *MemoryVectorStore) DeleteInterviewKnowledgeByAtom(_ context.Context, atomID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, doc := range s.knowledgeDocs {
		if doc.AtomID == atomID {
			delete(s.knowledgeDocs, id)
		}
	}
	return nil
}

func (s *MemoryVectorStore) RebuildScenarioIndex(ctx context.Context, docs []ai.ScenarioVectorDocument) error {
	if len(docs) == 0 {
		return nil
	}
	if err := s.DeleteByQuestion(ctx, docs[0].QuestionID); err != nil {
		return err
	}
	return s.UpsertDocuments(ctx, docs)
}

func (s *MemoryVectorStore) RebuildInterviewKnowledgeIndex(ctx context.Context, docs []ai.InterviewKnowledgeVectorDocument) error {
	if len(docs) == 0 {
		return nil
	}
	if err := s.DeleteInterviewKnowledgeByAtom(ctx, docs[0].AtomID); err != nil {
		return err
	}
	return s.UpsertInterviewKnowledgeDocuments(ctx, docs)
}

func vectorDocumentScore(query VectorSearchQuery, doc ai.ScenarioVectorDocument) float64 {
	if len(query.Vector) > 0 && len(doc.Vector) == len(query.Vector) {
		return ai.CosineSimilarity(query.Vector, doc.Vector)
	}
	if strings.TrimSpace(query.Text) != "" {
		if strings.Contains(strings.ToLower(doc.DocText), strings.ToLower(strings.TrimSpace(query.Text))) ||
			strings.Contains(strings.ToLower(strings.TrimSpace(query.Text)), strings.ToLower(doc.DocText)) {
			return 1
		}
		return ai.Similarity(query.Text, doc.DocText)
	}
	return 0
}

func interviewKnowledgeVectorDocumentScore(query InterviewKnowledgeVectorSearchQuery, doc ai.InterviewKnowledgeVectorDocument) float64 {
	if len(query.Vector) > 0 && len(doc.Vector) == len(query.Vector) {
		return ai.CosineSimilarity(query.Vector, doc.Vector)
	}
	if strings.TrimSpace(query.Text) != "" {
		lowerText := strings.ToLower(strings.TrimSpace(query.Text))
		lowerDoc := strings.ToLower(doc.DocText)
		if strings.Contains(lowerDoc, lowerText) || strings.Contains(lowerText, lowerDoc) {
			return 1
		}
		return ai.Similarity(query.Text, doc.DocText)
	}
	return 0
}

func vectorDocID(doc ai.ScenarioVectorDocument) string {
	return strings.Join([]string{doc.QuestionID, fmt.Sprintf("%d", doc.SourceVersion), doc.DocType, doc.DocKey}, "|")
}

func interviewKnowledgeVectorDocID(doc ai.InterviewKnowledgeVectorDocument) string {
	return strings.Join([]string{doc.AtomID, fmt.Sprintf("%d", doc.AtomVersion), doc.DocType, doc.DocKey}, "|")
}

func cloneVectorDocument(doc ai.ScenarioVectorDocument) ai.ScenarioVectorDocument {
	if doc.Metadata != nil {
		copied := map[string]string{}
		for k, v := range doc.Metadata {
			copied[k] = v
		}
		doc.Metadata = copied
	}
	if doc.Vector != nil {
		doc.Vector = append([]float64{}, doc.Vector...)
	}
	return doc
}

func cloneInterviewKnowledgeVectorDocument(doc ai.InterviewKnowledgeVectorDocument) ai.InterviewKnowledgeVectorDocument {
	if doc.Metadata != nil {
		copied := map[string]string{}
		for k, v := range doc.Metadata {
			copied[k] = v
		}
		doc.Metadata = copied
	}
	if doc.Vector != nil {
		doc.Vector = append([]float64{}, doc.Vector...)
	}
	return doc
}

func metadataJSON(metadata map[string]string) []byte {
	if metadata == nil {
		return []byte("{}")
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return []byte("{}")
	}
	return data
}
