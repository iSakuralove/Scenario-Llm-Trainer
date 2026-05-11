package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommunityPostSchemaMigratesModerationSummaryColumn(t *testing.T) {
	required := "ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS moderation_summary JSONB;"
	if !strings.Contains(LegacyCompatibilitySQL, required) {
		t.Fatalf("legacy community_posts migration must add moderation_summary column; missing %q", required)
	}
}

func TestCommunityPostDockerInitSchemaIncludesModerationColumns(t *testing.T) {
	root := filepath.Join("..", "..", "migrations", "001_schema.sql")
	content, err := os.ReadFile(root)
	if err != nil {
		t.Fatalf("read docker init schema: %v", err)
	}
	schema := string(content)
	for _, required := range []string{
		"forked_from_scenario_id TEXT",
		"moderation_summary JSONB",
		"sensitive_check JSONB DEFAULT '{}'",
		"updated_at TIMESTAMPTZ DEFAULT NOW()",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("docker init schema must include community_posts column %q", required)
		}
	}
}

func TestAIConfigSchemaIncludesProviderSamplingColumns(t *testing.T) {
	for _, required := range []string{
		"temperature DOUBLE PRECISION DEFAULT 0.2",
		"top_p DOUBLE PRECISION DEFAULT 0",
		"top_k INT DEFAULT 0",
		"max_tokens INT DEFAULT 0",
	} {
		if !strings.Contains(SchemaSQL, required) {
			t.Fatalf("ai_config schema must include %q", required)
		}
		if !strings.Contains(LegacyCompatibilitySQL, "ALTER TABLE IF EXISTS ai_config ADD COLUMN IF NOT EXISTS") {
			t.Fatalf("legacy migration must include ai_config column backfill statements")
		}
	}
}

func TestAIJobsSchemaIncludesModelColumn(t *testing.T) {
	required := "model VARCHAR(100)"
	if !strings.Contains(SchemaSQL, required) {
		t.Fatalf("ai_jobs schema must include %q", required)
	}
	if !strings.Contains(LegacyCompatibilitySQL, "ALTER TABLE IF EXISTS ai_jobs ADD COLUMN IF NOT EXISTS model VARCHAR(100);") {
		t.Fatalf("legacy migration must backfill ai_jobs model column")
	}
}

func TestDockerInitSchemaIncludesVectorDocuments(t *testing.T) {
	root := filepath.Join("..", "..", "migrations", "001_schema.sql")
	content, err := os.ReadFile(root)
	if err != nil {
		t.Fatalf("read docker init schema: %v", err)
	}
	schema := string(content)
	for _, required := range []string{
		"CREATE EXTENSION IF NOT EXISTS vector;",
		"CREATE TABLE IF NOT EXISTS scenario_vector_documents",
		"embedding vector",
		"scenario_vector_documents_embedding_hnsw",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("docker init schema must include vector fragment %q", required)
		}
	}
}
