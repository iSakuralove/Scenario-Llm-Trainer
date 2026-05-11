package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"situational-teaching/backend/internal/ai"
)

type PostgresVectorStore struct {
	pool *pgxpool.Pool
}

func NewPostgresVectorStore(pool *pgxpool.Pool) *PostgresVectorStore {
	return &PostgresVectorStore{pool: pool}
}

func (s *PostgresVectorStore) UpsertDocuments(ctx context.Context, docs []ai.ScenarioVectorDocument) error {
	if s == nil || s.pool == nil {
		return nil
	}
	for _, doc := range docs {
		id := vectorDocID(doc)
		vectorLiteral := vectorLiteral(doc.Vector)
		metadata := metadataJSON(doc.Metadata)
		var err error
		if vectorLiteral == "" {
			_, err = s.pool.Exec(ctx, `
				INSERT INTO scenario_vector_documents
				    (id, question_id, source_version, doc_type, doc_key, doc_text, text_hash, metadata, embedding_model, embedding_dim, embedding, status, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL,$11,NOW())
				ON CONFLICT (question_id, source_version, doc_type, doc_key) DO UPDATE SET
				    doc_text = EXCLUDED.doc_text,
				    text_hash = EXCLUDED.text_hash,
				    metadata = EXCLUDED.metadata,
				    embedding_model = EXCLUDED.embedding_model,
				    embedding_dim = EXCLUDED.embedding_dim,
				    embedding = EXCLUDED.embedding,
				    status = EXCLUDED.status,
				    updated_at = NOW()
			`, id, doc.QuestionID, doc.SourceVersion, doc.DocType, doc.DocKey, doc.DocText, doc.TextHash,
				metadata, doc.EmbeddingModel, doc.EmbeddingDim, doc.Status)
		} else {
			_, err = s.pool.Exec(ctx, `
				INSERT INTO scenario_vector_documents
				    (id, question_id, source_version, doc_type, doc_key, doc_text, text_hash, metadata, embedding_model, embedding_dim, embedding, status, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::vector,$12,NOW())
				ON CONFLICT (question_id, source_version, doc_type, doc_key) DO UPDATE SET
				    doc_text = EXCLUDED.doc_text,
				    text_hash = EXCLUDED.text_hash,
				    metadata = EXCLUDED.metadata,
				    embedding_model = EXCLUDED.embedding_model,
				    embedding_dim = EXCLUDED.embedding_dim,
				    embedding = EXCLUDED.embedding,
				    status = EXCLUDED.status,
				    updated_at = NOW()
			`, id, doc.QuestionID, doc.SourceVersion, doc.DocType, doc.DocKey, doc.DocText, doc.TextHash,
				metadata, doc.EmbeddingModel, doc.EmbeddingDim, vectorLiteral, doc.Status)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresVectorStore) Search(ctx context.Context, query VectorSearchQuery) ([]VectorSearchResult, error) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	if len(query.Vector) == 0 {
		return s.searchByText(ctx, query, limit)
	}
	args := []interface{}{vectorLiteral(query.Vector), limit}
	where := []string{"embedding IS NOT NULL", "COALESCE(status, 'active') = 'active'"}
	if query.QuestionID != "" {
		args = append(args, query.QuestionID)
		where = append(where, fmt.Sprintf("question_id = $%d", len(args)))
	}
	if len(query.DocTypes) > 0 {
		args = append(args, query.DocTypes)
		where = append(where, fmt.Sprintf("doc_type = ANY($%d)", len(args)))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT question_id, source_version, doc_type, doc_key, doc_text, text_hash,
		       COALESCE(metadata, '{}'::jsonb), COALESCE(embedding_model, ''), COALESCE(embedding_dim, 0),
		       COALESCE(status, 'active'), 1 - (embedding <=> $1::vector) AS score
		FROM scenario_vector_documents
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []VectorSearchResult{}
	for rows.Next() {
		var doc ai.ScenarioVectorDocument
		var metadataBytes []byte
		var score float64
		if err := rows.Scan(&doc.QuestionID, &doc.SourceVersion, &doc.DocType, &doc.DocKey, &doc.DocText, &doc.TextHash, &metadataBytes, &doc.EmbeddingModel, &doc.EmbeddingDim, &doc.Status, &score); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadataBytes, &doc.Metadata)
		results = append(results, VectorSearchResult{Document: doc, Score: score})
	}
	return results, rows.Err()
}

func (s *PostgresVectorStore) searchByText(ctx context.Context, query VectorSearchQuery, limit int) ([]VectorSearchResult, error) {
	if strings.TrimSpace(query.Text) == "" {
		return nil, nil
	}
	candidateLimit := int(math.Max(float64(limit*8), 64))
	args := []interface{}{candidateLimit}
	where := []string{"COALESCE(status, 'active') = 'active'"}
	if query.QuestionID != "" {
		args = append(args, query.QuestionID)
		where = append(where, fmt.Sprintf("question_id = $%d", len(args)))
	}
	if len(query.DocTypes) > 0 {
		args = append(args, query.DocTypes)
		where = append(where, fmt.Sprintf("doc_type = ANY($%d)", len(args)))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT question_id, source_version, doc_type, doc_key, doc_text, text_hash,
		       COALESCE(metadata, '{}'::jsonb), COALESCE(embedding_model, ''), COALESCE(embedding_dim, 0),
		       COALESCE(status, 'active')
		FROM scenario_vector_documents
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY updated_at DESC
		LIMIT $1
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []VectorSearchResult{}
	for rows.Next() {
		var doc ai.ScenarioVectorDocument
		var metadataBytes []byte
		if err := rows.Scan(&doc.QuestionID, &doc.SourceVersion, &doc.DocType, &doc.DocKey, &doc.DocText, &doc.TextHash, &metadataBytes, &doc.EmbeddingModel, &doc.EmbeddingDim, &doc.Status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadataBytes, &doc.Metadata)
		score := vectorDocumentScore(query, doc)
		if score <= 0 {
			continue
		}
		results = append(results, VectorSearchResult{Document: doc, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortVectorResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *PostgresVectorStore) DeleteByQuestion(ctx context.Context, questionID string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM scenario_vector_documents WHERE question_id = $1`, questionID)
	return err
}

func (s *PostgresVectorStore) RebuildScenarioIndex(ctx context.Context, docs []ai.ScenarioVectorDocument) error {
	if len(docs) == 0 {
		return nil
	}
	if err := s.DeleteByQuestion(ctx, docs[0].QuestionID); err != nil {
		return err
	}
	return s.UpsertDocuments(ctx, docs)
}

func vectorLiteral(vector []float64) string {
	if len(vector) == 0 {
		return ""
	}
	parts := make([]string, 0, len(vector))
	for _, value := range vector {
		parts = append(parts, strconv.FormatFloat(value, 'f', -1, 64))
	}
	return "[" + strings.Join(parts, ",") + "]"
}
