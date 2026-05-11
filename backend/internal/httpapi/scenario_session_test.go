package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestScenarioSessionDetailReturnsSessionAndMessages(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
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
		"content": "先看慢查询日志和最近变更。",
	})
	if status != http.StatusOK {
		t.Fatalf("send message status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/sessions/"+created.SessionID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Session  scenarioSessionResponse  `json:"session"`
		Messages []domain.ScenarioMessage `json:"messages"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Session.ID != created.SessionID {
		t.Fatalf("unexpected session id: %+v", payload.Session)
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("expected one message, got %+v", payload.Messages)
	}
	if payload.Session.QuestionSnapshot.Content.RootCause != "" {
		t.Fatalf("detail payload must not expose root cause: %+v", payload.Session.QuestionSnapshot)
	}
	if strings.TrimSpace(payload.Session.QuestionSnapshot.Content.ArchitectureDiagram) == "" {
		t.Fatalf("expected public question snapshot in detail payload: %+v", payload.Session.QuestionSnapshot)
	}
}

func TestScenarioSessionDetailRepairsInvalidMermaidSnapshot(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]
	question.Content.ArchitectureDiagram = "graph TD\nA[内网客户端] --> B[内网DNS递归器]\nB --> C[公网根/顶级域]\nB --> D[上游权威服务器]\nD --> E[错误IP(无服务)]"
	question.Content.DiagramStatus = "validated"
	dataStore.AddScenario(question)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)
	stored, ok := dataStore.GetScenarioSession(created.SessionID)
	if !ok {
		t.Fatal("missing persisted session")
	}
	if stored.QuestionSnapshot.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected persisted snapshot to be repaired before storage, got %+v", stored.QuestionSnapshot.Content)
	}
	if strings.Contains(stored.QuestionSnapshot.Content.ArchitectureDiagram, "错误IP(无服务)") {
		t.Fatalf("invalid mermaid persisted in session snapshot: %q", stored.QuestionSnapshot.Content.ArchitectureDiagram)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/sessions/"+created.SessionID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Session scenarioSessionResponse `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Session.QuestionSnapshot.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected repaired snapshot fallback status, got %+v", payload.Session.QuestionSnapshot.Content)
	}
	if strings.Contains(payload.Session.QuestionSnapshot.Content.ArchitectureDiagram, "错误IP(无服务)") {
		t.Fatalf("invalid mermaid leaked into session snapshot: %q", payload.Session.QuestionSnapshot.Content.ArchitectureDiagram)
	}
}

func TestScenarioSessionDetailRepairsLegacyInvalidMermaidSnapshot(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
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

	session, ok := dataStore.GetScenarioSession(created.SessionID)
	if !ok {
		t.Fatal("missing persisted session")
	}
	session.QuestionSnapshot.Content.ArchitectureDiagramSpec = nil
	session.QuestionSnapshot.Content.ArchitectureDiagram = "graph TD\nA[内网客户端] --> B[内网DNS递归器]\nB --> D[上游权威服务器]\nD --> E[错误IP(无服务)]B --> F[正常权威服务器]"
	session.QuestionSnapshot.Content.DiagramStatus = "validated"
	dataStore.SaveScenarioSession(session)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/sessions/"+created.SessionID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Session scenarioSessionResponse `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Session.QuestionSnapshot.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected legacy snapshot to be repaired at response time, got %+v", payload.Session.QuestionSnapshot.Content)
	}
	if strings.Contains(payload.Session.QuestionSnapshot.Content.ArchitectureDiagram, "错误IP(无服务)") ||
		strings.Contains(payload.Session.QuestionSnapshot.Content.ArchitectureDiagram, "]B -->") {
		t.Fatalf("legacy invalid mermaid leaked into detail payload: %q", payload.Session.QuestionSnapshot.Content.ArchitectureDiagram)
	}
}

func TestScenarioSessionDetailReturnsNotFoundForOtherUser(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	ownerToken := loginToken(t, handler, "demo", "demo123")
	otherToken := loginToken(t, handler, "admin", "admin123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", ownerToken, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/sessions/"+created.SessionID, otherToken, nil)
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected not found for other user, status=%d env=%+v", status, env)
	}
	if env.Message != "session not found" {
		t.Fatalf("unexpected not found message: %q", env.Message)
	}
}

func TestScenarioSessionDetailExpiresIdleSessionAndReturnsAbandoned(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
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

	session, ok := dataStore.GetScenarioSession(created.SessionID)
	if !ok {
		t.Fatal("missing scenario session")
	}
	session.LastActiveAt = time.Now().Add(-31 * time.Minute)
	dataStore.SaveScenarioSession(session)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/sessions/"+created.SessionID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Session scenarioSessionResponse `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Session.Status != "abandoned" {
		t.Fatalf("expected abandoned status after idle expiration, got %+v", payload.Session)
	}
	updated, ok := dataStore.GetScenarioSession(created.SessionID)
	if !ok || updated.Status != "abandoned" || updated.EndedAt == nil {
		t.Fatalf("expected persisted abandoned session, got %+v", updated)
	}
}
