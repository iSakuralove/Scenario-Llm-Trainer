package httpapi

import (
	"encoding/json"
	"fmt"
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

func TestScenarioMessageReturnsDiagnosticAgentTraceAndAudit(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", demoToken, map[string]string{
		"content": "我想先看日志和发布时间",
	})
	if status != http.StatusOK {
		t.Fatalf("message status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Message      domain.ScenarioMessage `json:"message"`
		ResponseMeta domain.ResponseMeta    `json:"response_meta"`
		Session      interface{}            `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	trace := payload.ResponseMeta.AgentTrace
	if trace == nil {
		t.Fatal("expected agent trace in response_meta")
	}
	if trace.Agent != "diagnostic_agent" || trace.Mode != "server_react" || trace.ToolCount == 0 {
		t.Fatalf("unexpected trace: %+v", trace)
	}
	if len(trace.Steps) == 0 {
		t.Fatalf("expected trace steps: %+v", trace)
	}
	if strings.Contains(payload.Message.AssistantContent, question.Content.RootCause) {
		t.Fatalf("assistant content leaked root cause: %s", payload.Message.AssistantContent)
	}
	for _, step := range question.Content.StandardProcedure {
		if strings.TrimSpace(step) != "" && strings.Contains(payload.Message.AssistantContent, step) {
			t.Fatalf("assistant content leaked standard step %q: %s", step, payload.Message.AssistantContent)
		}
	}

	events := dataStore.ListAuditEvents(20)
	found := false
	for _, event := range events {
		if event.Action == "agent.diagnostic_run" && event.ResourceType == "scenario_session" && event.ResourceID == created.SessionID {
			found = true
			if event.Metadata["agent"] != "diagnostic_agent" || event.Metadata["tool_count"] == "" {
				t.Fatalf("unexpected audit metadata: %+v", event.Metadata)
			}
			if event.Metadata["status"] != "completed" {
				t.Fatalf("expected completed audit status, got %+v", event.Metadata)
			}
		}
	}
	if !found {
		t.Fatalf("expected agent diagnostic audit event, got %+v", events)
	}
}

func TestAgentSummaryAggregatesDiagnosticAuditEvents(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)
	summary := agentSummary([]domain.AuditEvent{
		{
			Action:    "agent.diagnostic_run",
			CreatedAt: now,
			Metadata: map[string]string{
				"agent":            "diagnostic_agent",
				"status":           "failed",
				"safety_rewritten": "true",
			},
		},
		{
			Action:    "agent.diagnostic_run",
			CreatedAt: now.Add(-time.Minute),
			Metadata: map[string]string{
				"agent":            "diagnostic_agent",
				"status":           "completed",
				"safety_rewritten": "false",
			},
		},
		{Action: "ai.error", CreatedAt: now.Add(-2 * time.Minute)},
	})

	if summary["total_recent"] != 2 {
		t.Fatalf("unexpected total_recent: %+v", summary)
	}
	if summary["latest_agent"] != "diagnostic_agent" || summary["latest_run_at"] != now.Format(time.RFC3339) {
		t.Fatalf("unexpected latest metadata: %+v", summary)
	}
	if summary["failed_recent"] != 1 {
		t.Fatalf("unexpected failed_recent: %+v", summary)
	}
	if summary["safety_rewritten_recent"] != 1 {
		t.Fatalf("unexpected safety_rewritten_recent: %+v", summary)
	}
}

func TestAgentSummaryAggregatesPerAgentCounts(t *testing.T) {
	now := time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)
	summary := agentSummary([]domain.AuditEvent{
		{
			Action:    "agent.community_review_run",
			CreatedAt: now,
			Metadata: map[string]string{
				"agent":            "cm_review_agent",
				"status":           "completed",
				"safety_rewritten": "false",
				"flagged":          "true",
			},
		},
		{
			Action:    "agent.interview_run",
			CreatedAt: now.Add(-time.Minute),
			Metadata: map[string]string{
				"agent":            "interview_agent",
				"status":           "failed",
				"safety_rewritten": "true",
				"flagged":          "false",
			},
		},
		{
			Action:    "agent.diagnostic_run",
			CreatedAt: now.Add(-2 * time.Minute),
			Metadata: map[string]string{
				"agent":            "diagnostic_agent",
				"status":           "completed",
				"safety_rewritten": "false",
				"flagged":          "false",
			},
		},
	})

	if summary["total_recent"] != 3 {
		t.Fatalf("unexpected total_recent: %+v", summary)
	}
	if summary["latest_agent"] != "cm_review_agent" {
		t.Fatalf("unexpected latest_agent: %+v", summary)
	}
	if summary["flagged_recent"] != 1 {
		t.Fatalf("unexpected flagged_recent: %+v", summary)
	}
	perAgent, ok := summary["per_agent"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected per_agent slice, got %+v", summary["per_agent"])
	}
	if len(perAgent) != 3 {
		t.Fatalf("expected 3 per_agent rows, got %+v", perAgent)
	}
	first := perAgent[0]
	if first["agent"] != "cm_review_agent" || first["total_recent"] != 1 || first["latest_status"] != "completed" {
		t.Fatalf("unexpected first per_agent row: %+v", first)
	}
}

func TestScenarioMessageAuditMarksSafetyRewriteFromRealRun(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": mustJSON(map[string]string{"reply": "根因是" + question.Content.RootCause})}},
			},
		})
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

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", token, map[string]string{
		"content": "我想先看数据库连接和慢查询日志",
	})
	if status != http.StatusOK {
		t.Fatalf("message status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Message      domain.ScenarioMessage `json:"message"`
		ResponseMeta domain.ResponseMeta    `json:"response_meta"`
	}
	mustDecodeData(t, env, &payload)
	if !payload.ResponseMeta.SafetyRewritten {
		t.Fatalf("expected safety rewrite in response meta: %+v", payload.ResponseMeta)
	}
	if strings.Contains(payload.Message.AssistantContent, question.Content.RootCause) {
		t.Fatalf("assistant content leaked root cause: %s", payload.Message.AssistantContent)
	}

	metadata := findAgentAuditMetadata(t, dataStore.ListAuditEvents(20), created.SessionID)
	if metadata["status"] != "completed" || metadata["safety_rewritten"] != "true" {
		t.Fatalf("expected completed safety rewrite audit metadata, got %+v", metadata)
	}
}

func TestScenarioMessageResponseIncludesSemanticGateMetadata(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var embeddingCalls int
	embeddingProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected embedding path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-jianyi-key" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode embedding request: %v", err)
		}
		embeddingCalls++
		if body.Model == "text-embedding-3-small" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"primary unavailable"}}`))
			return
		}
		data := make([]map[string]interface{}, len(body.Input))
		for i := range body.Input {
			vector := []float64{0, 1}
			if i == 0 || i == 2 {
				vector = []float64{1, 0}
			}
			data[i] = map[string]interface{}{"index": i, "embedding": vector}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer embeddingProvider.Close()

	t.Setenv("JIANYI_API_KEY", "test-jianyi-key")
	t.Setenv("EMBEDDING_API_KEY", "")
	t.Setenv("jeniya_embedding_key", "")
	t.Setenv("EMBEDDING_BASE_URL", embeddingProvider.URL)
	t.Setenv("EMBEDDING_MODEL", "text-embedding-3-small")
	t.Setenv("EMBEDDING_FALLBACK_MODEL", "text-embedding-3-large")
	t.Setenv("EMBEDDING_TIMEOUT_SECONDS", "1")

	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{Provider: ai.ProviderMock}))
	server.stt = MockSTTProvider{}
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", token, map[string]string{
		"content": "我想确认异常开始时间是否和上线窗口重合",
	})
	if status != http.StatusOK {
		t.Fatalf("message status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Message      domain.ScenarioMessage `json:"message"`
		ResponseMeta domain.ResponseMeta    `json:"response_meta"`
	}
	mustDecodeData(t, env, &payload)
	if embeddingCalls != 2 {
		t.Fatalf("expected primary and fallback embedding calls, got %d", embeddingCalls)
	}
	if payload.ResponseMeta.SemanticDecision != "release_clue" || payload.ResponseMeta.MatchedClueID == "" {
		t.Fatalf("expected semantic release metadata: %+v", payload.ResponseMeta)
	}
	if payload.ResponseMeta.EmbeddingModel != "text-embedding-3-large" || !payload.ResponseMeta.EmbeddingFallbackUsed {
		t.Fatalf("expected fallback embedding metadata: %+v", payload.ResponseMeta)
	}
	if payload.Message.ResponseMeta.SemanticDecision != payload.ResponseMeta.SemanticDecision {
		t.Fatalf("message and top-level meta diverged: message=%+v top=%+v", payload.Message.ResponseMeta, payload.ResponseMeta)
	}
	traceText := mustJSON(payload.ResponseMeta.AgentTrace)
	for _, step := range []string{"input_quality_check", "agent_relevance_judge", "embedding_similarity_match"} {
		if !strings.Contains(traceText, step) {
			t.Fatalf("trace missing %s: %s", step, traceText)
		}
	}
	if strings.Contains(fmt.Sprint(payload.ResponseMeta.AgentTrace), question.Content.RootCause) {
		t.Fatalf("trace leaked root cause: %+v", payload.ResponseMeta.AgentTrace)
	}
}

func TestSystemStatusAgentSummaryCountsRealDiagnosticFailures(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	handler := server.Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	user := &domain.User{ID: "user-demo"}
	server.auditDiagnosticAgentRun(nil, user, "session-failed", domain.AgentTrace{
		Agent:     "diagnostic_agent",
		ToolCount: 4,
	}, domain.ResponseMeta{}, "failed", http.ErrAbortHandler)
	metadata := findAgentAuditMetadata(t, dataStore.ListAuditEvents(20), "session-failed")
	if metadata["status"] != "failed" {
		t.Fatalf("expected failed audit metadata, got %+v", metadata)
	}
	if metadata["error"] != "agent run failed" {
		t.Fatalf("expected safe audit error summary, got %+v", metadata)
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("system status=%d message=%s", status, env.Message)
	}
	var payload struct {
		AgentSummary map[string]interface{} `json:"agent_summary"`
	}
	mustDecodeData(t, env, &payload)
	if payload.AgentSummary["failed_recent"].(float64) != 1 {
		t.Fatalf("expected failed_recent=1, got %+v", payload.AgentSummary)
	}
}

func findAgentAuditMetadata(t *testing.T, events []domain.AuditEvent, sessionID string) map[string]string {
	t.Helper()
	for _, event := range events {
		if event.Action == "agent.diagnostic_run" && event.ResourceID == sessionID {
			return event.Metadata
		}
	}
	t.Fatalf("expected agent audit event for session %s, got %+v", sessionID, events)
	return nil
}
