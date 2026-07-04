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

func TestUserSchemaIncludesTokenVersion(t *testing.T) {
	required := "token_version INT DEFAULT 0"
	if !strings.Contains(SchemaSQL, required) {
		t.Fatalf("user schema must include %q", required)
	}
	if !strings.Contains(LegacyCompatibilitySQL, "ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS token_version INT DEFAULT 0;") {
		t.Fatal("legacy migration must backfill users.token_version")
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
		"token_version INT DEFAULT 0",
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
		"CREATE TABLE IF NOT EXISTS interview_knowledge_vector_documents",
		"embedding vector(1536)",
		"scenario_vector_documents_embedding_hnsw",
		"interview_knowledge_vector_documents_embedding_hnsw",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("docker init schema must include vector fragment %q", required)
		}
	}
}

func TestInterviewKnowledgeBankSchemaIncludesVersionGovernance(t *testing.T) {
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS interview_knowledge_atoms",
		"current_version INT DEFAULT 1",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_atom_versions",
		"version_type VARCHAR(32) NOT NULL CHECK",
		"'archive'",
		"snapshot JSONB NOT NULL",
		"diff_summary JSONB DEFAULT '{}'",
		"no_content_change BOOLEAN DEFAULT FALSE",
		"interview_knowledge_atom_versions_atom_version_idx",
		"interview_knowledge_atom_versions_type_created_idx",
		"interview_knowledge_atom_versions_admin_created_idx",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_batches",
		"CREATE TABLE IF NOT EXISTS interview_retrieval_logs",
		"question_snapshot JSONB DEFAULT '{}'",
		"selected_atom_snapshots JSONB DEFAULT '[]'",
	} {
		if !strings.Contains(SchemaSQL, required) {
			t.Fatalf("runtime schema must include interview knowledge bank fragment %q", required)
		}
	}
}

func TestDockerInitSchemaIncludesInterviewKnowledgeBank(t *testing.T) {
	root := filepath.Join("..", "..", "migrations", "001_schema.sql")
	content, err := os.ReadFile(root)
	if err != nil {
		t.Fatalf("read docker init schema: %v", err)
	}
	schema := string(content)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS interview_knowledge_atoms",
		"current_version INT DEFAULT 1",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_atom_versions",
		"'archive'",
		"interview_knowledge_atom_versions_atom_version_idx",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_batches",
		"CREATE TABLE IF NOT EXISTS interview_retrieval_logs",
		"question_snapshot JSONB DEFAULT '{}'",
		"selected_atom_snapshots JSONB DEFAULT '[]'",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("docker init schema must include interview knowledge bank fragment %q", required)
		}
	}
}

func TestLegacyCompatibilitySQLBackfillsInterviewSessionSnapshots(t *testing.T) {
	for _, required := range []string{
		"ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS question_snapshot JSONB DEFAULT '{}';",
		"ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS selected_atom_snapshots JSONB DEFAULT '[]';",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_atoms",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_atom_versions",
		"interview_knowledge_atom_versions_version_type_check",
		"CREATE TABLE IF NOT EXISTS interview_knowledge_batches",
	} {
		if !strings.Contains(LegacyCompatibilitySQL, required) {
			t.Fatalf("legacy migration must include interview knowledge bank fragment %q", required)
		}
	}
}
