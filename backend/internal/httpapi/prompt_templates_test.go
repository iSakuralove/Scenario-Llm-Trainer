package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestAdminRejectsUnstructuredScenarioGeneratePrompt(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	before, ok := dataStore.GetPromptTemplate("scenario_generate")
	if !ok {
		t.Fatal("expected scenario_generate prompt template")
	}

	status, env := requestJSON(t, handler, http.MethodPut, "/api/v1/admin/prompts/scenario_generate", adminToken, map[string]string{
		"content": "请输出符合 scenario_question validator 的 JSON 结构。",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected short scenario prompt to return 400, got status=%d message=%s", status, env.Message)
	}
	if !strings.Contains(env.Message, "结构化") {
		t.Fatalf("expected structured prompt validation message, got %q", env.Message)
	}
	after, ok := dataStore.GetPromptTemplate("scenario_generate")
	if !ok {
		t.Fatal("expected scenario_generate prompt template after rejection")
	}
	if after.Content != before.Content || after.IsModified {
		t.Fatalf("rejected prompt must not be persisted, before=%+v after=%+v", before, after)
	}
}

func TestSeededScenarioGeneratePromptUsesStructuredDefault(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	template, ok := dataStore.GetPromptTemplate("scenario_generate")
	if !ok {
		t.Fatal("expected scenario_generate prompt template")
	}
	for _, token := range []string{`"architecture_diagram_spec"`, `"reveal_strategy"`, `"root_cause_keywords"`} {
		if !strings.Contains(template.Default, token) || !strings.Contains(template.Content, token) {
			t.Fatalf("expected seeded scenario_generate prompt to contain %s, got %+v", token, template)
		}
	}
	if template.IsModified || template.RenderEngine != ai.PromptRenderEngineGoTemplate {
		t.Fatalf("expected seeded scenario_generate prompt to be default go_template, got %+v", template)
	}
}

func TestApplyPromptOverridesSkipsInvalidScenarioGenerateOverride(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	template, ok := dataStore.GetPromptTemplate("scenario_generate")
	if !ok {
		t.Fatal("expected scenario_generate prompt template")
	}
	template.Content = "请生成一道情景题。"
	template.RenderEngine = "go_template"
	template.UpdatedBy = "user-admin"
	template.IsModified = true
	dataStore.PromptTemplates["scenario_generate"] = template

	_ = NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))

	events := dataStore.ListAuditEvents(10)
	for _, event := range events {
		if event.Action != "ai.prompt_override_error" || event.ResourceID != "scenario_generate" {
			continue
		}
		if !strings.Contains(event.Metadata["error"], "结构化") {
			t.Fatalf("expected structured prompt error metadata, got %+v", event.Metadata)
		}
		return
	}
	t.Fatalf("expected prompt override error audit, got %+v", events)
}

func TestScenarioGenerationFailureAuditsSanitizedRawOutputPreview(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"not json 10.2.3.4 password=Secret123"}}]}`))
	}))
	defer provider.Close()
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{
		Provider: ai.ProviderOpenAICompatible,
		BaseURL:  provider.URL,
		APIKey:   "test-key",
		Model:    "fake-model",
		Timeout:  time.Second,
	}))
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)
	waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusFailed)

	events := dataStore.ListAuditEvents(5)
	var metadata map[string]string
	for _, event := range events {
		if event.Action == "scenario.generate.failed" && event.ResourceID == created.Job.ID {
			metadata = event.Metadata
			break
		}
	}
	if metadata == nil {
		t.Fatalf("expected scenario generation failed audit, got %+v", events)
	}
	if metadata["job_id"] != created.Job.ID || metadata["provider"] != ai.ProviderOpenAICompatible || metadata["stage"] != "validating_output" {
		t.Fatalf("unexpected failed audit metadata: %+v", metadata)
	}
	preview := metadata["raw_output_preview"]
	if preview == "" {
		t.Fatalf("expected raw output preview in failed audit metadata: %+v", metadata)
	}
	if strings.Contains(preview, "10.2.3.4") || strings.Contains(preview, "Secret123") {
		t.Fatalf("raw output preview must be sanitized, got %q", preview)
	}
}
