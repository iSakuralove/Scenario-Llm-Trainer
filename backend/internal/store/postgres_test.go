package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"situational-teaching/backend/internal/auth"
)

func TestPromptTemplateListSelectIncludesRenderEngine(t *testing.T) {
	query := strings.ToLower(promptTemplateListSelectSQL)
	if !strings.Contains(query, "render_engine") {
		t.Fatal("prompt template list query must include render_engine")
	}
	if strings.Index(query, "render_engine") < strings.Index(query, "content") {
		t.Fatal("render_engine must be selected after content to match scanner order")
	}
}

func TestPostgresSeedAdminConfigRefreshesPromptDefaults(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_TEST_URL")
	if databaseURL == "" {
		t.Skip("POSTGRES_TEST_URL is not set")
	}

	ctx := context.Background()
	store, err := NewPostgresStore(ctx, databaseURL, auth.HashPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.pool.Exec(ctx, `
		UPDATE prompt_templates
		SET default_content = '短默认', content = '短默认', render_engine = 'jinja2'
		WHERE name = 'scenario_generate'
	`); err != nil {
		t.Fatal(err)
	}
	if err := store.seedAdminConfig(ctx); err != nil {
		t.Fatal(err)
	}
	template, ok := store.GetPromptTemplate("scenario_generate")
	if !ok {
		t.Fatal("expected scenario_generate prompt")
	}
	for _, token := range []string{`"architecture_diagram_spec"`, `"reveal_strategy"`} {
		if !strings.Contains(template.Default, token) || !strings.Contains(template.Content, token) {
			t.Fatalf("expected seed to refresh structured prompt token %s, got %+v", token, template)
		}
	}
	if template.RenderEngine != "go_template" || template.IsModified {
		t.Fatalf("expected refreshed prompt to be unmodified go_template, got %+v", template)
	}
}

func TestLegacyCompatibilitySQLKeepsPromptTemplateAndAIConfigColumns(t *testing.T) {
	legacy := strings.ToLower(LegacyCompatibilitySQL)
	requiredFragments := []string{
		"create table if not exists prompt_templates",
		"render_engine text default 'go_template'",
		"create table if not exists ai_config",
		"temperature double precision default 0.2",
		"top_p double precision default 0",
		"top_k int default 0",
		"max_tokens int default 0",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(legacy, fragment) {
			t.Fatalf("legacy compatibility sql must include %q", fragment)
		}
	}
}

func TestPostgresStoreCRUD(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_TEST_URL")
	if databaseURL == "" {
		t.Skip("POSTGRES_TEST_URL is not set")
	}

	ctx := context.Background()
	store, err := NewPostgresStore(ctx, databaseURL, auth.HashPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	user, err := store.CreateUser("unit-"+NewID(), "unit-"+NewID()+"@example.com", auth.HashPassword("secret"))
	if err != nil {
		t.Fatal(err)
	}
	found, ok := store.GetUser(user.ID)
	if !ok || found.Username != user.Username {
		t.Fatalf("expected user %s, got %+v", user.ID, found)
	}

	scenarios := store.ListScenarios("", "", "")
	if len(scenarios) == 0 {
		t.Fatal("expected seeded scenarios")
	}
	session, err := store.CreateScenarioSession(user.ID, scenarios[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetScenarioSession(session.ID); !ok {
		t.Fatal("expected persisted scenario session")
	}
}
