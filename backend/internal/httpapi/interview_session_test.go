package httpapi

import (
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestCreateInterviewSessionValidatesRequiredTrackFields(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	cases := []struct {
		name string
		body map[string]string
	}{
		{name: "missing domain", body: map[string]string{"difficulty": "L3", "question_type": "scenario_analysis"}},
		{name: "empty domain", body: map[string]string{"domain": " ", "difficulty": "L3", "question_type": "scenario_analysis"}},
		{name: "missing difficulty", body: map[string]string{"domain": "database", "question_type": "scenario_analysis"}},
		{name: "empty difficulty", body: map[string]string{"domain": "database", "difficulty": " ", "question_type": "scenario_analysis"}},
		{name: "missing question type", body: map[string]string{"domain": "database", "difficulty": "L3"}},
		{name: "empty question type", body: map[string]string{"domain": "database", "difficulty": "L3", "question_type": " "}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, tc.body)
			if status != http.StatusBadRequest || env.Code != http.StatusBadRequest {
				t.Fatalf("expected missing track field rejection, status=%d env=%+v", status, env)
			}
			if env.Message != "domain, difficulty and question_type are required" {
				t.Fatalf("unexpected validation message: %q", env.Message)
			}
		})
	}
}

func TestCreateInterviewSessionReturnsNotFoundForUnsupportedTrack(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "security",
		"difficulty":    "L5",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected unsupported track not found, status=%d env=%+v", status, env)
	}
	if env.Message != "interview question not found" {
		t.Fatalf("unexpected not found message: %q", env.Message)
	}
}

func TestCreateInterviewSessionReturnsSelectedTrackQuestion(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("create interview status=%d env=%+v", status, env)
	}
	var payload struct {
		Question domain.InterviewQuestion `json:"question"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Question.Domain != "database" || payload.Question.Difficulty != "L3" || payload.Question.QuestionType != "scenario_analysis" {
		t.Fatalf("unexpected question track: %+v", payload.Question)
	}
}

func TestCreateInterviewSessionSupportsExpandedLaunchTracks(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	cases := []struct {
		domain       string
		difficulty   string
		questionType string
	}{
		{domain: "security", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "devops", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "backend", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "distributed", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "cloud-native", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "mq-cache", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "observability", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "performance", difficulty: "L4", questionType: "scenario_analysis"},
		{domain: "architecture", difficulty: "L5", questionType: "principle"},
	}

	for _, tc := range cases {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
			"domain":        tc.domain,
			"difficulty":    tc.difficulty,
			"question_type": tc.questionType,
		})
		if status != http.StatusOK || env.Code != http.StatusOK {
			t.Fatalf("expected interview session for %s/%s/%s, status=%d env=%+v", tc.domain, tc.difficulty, tc.questionType, status, env)
		}
		var payload struct {
			Question domain.InterviewQuestion `json:"question"`
		}
		mustDecodeData(t, env, &payload)
		if payload.Question.Domain != tc.domain || payload.Question.Difficulty != tc.difficulty || payload.Question.QuestionType != tc.questionType {
			t.Fatalf("unexpected question track for %s/%s/%s: %+v", tc.domain, tc.difficulty, tc.questionType, payload.Question)
		}
	}
}

func TestInterviewSessionDetailReturnsSessionAndQuestion(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string                  `json:"session_id"`
		Question  domain.InterviewQuestion `json:"question"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/sessions/"+created.SessionID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Session  domain.InterviewSession  `json:"session"`
		Question domain.InterviewQuestion `json:"question"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Session.ID != created.SessionID {
		t.Fatalf("unexpected session payload: %+v", payload.Session)
	}
	if payload.Question.ID != created.Question.ID {
		t.Fatalf("unexpected question payload: %+v", payload.Question)
	}
	if payload.Question.ReferenceAnswer != "" || len(payload.Question.ReferenceKeywords) != 0 {
		t.Fatalf("question detail must not expose references to student: %+v", payload.Question)
	}
}

func TestInterviewSessionDetailReturnsNotFoundForOtherUser(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	ownerToken := loginToken(t, handler, "demo", "demo123")
	otherToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", ownerToken, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/sessions/"+created.SessionID, otherToken, nil)
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected not found for other user, status=%d env=%+v", status, env)
	}
	if env.Message != "interview session not found" {
		t.Fatalf("unexpected not found message: %q", env.Message)
	}
}

func TestDeleteInterviewSessionRemovesOwnHistoryRecord(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodDelete, "/api/v1/interviews/sessions/"+created.SessionID, token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("delete interview status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/sessions/"+created.SessionID, token, nil)
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected deleted session not found, status=%d env=%+v", status, env)
	}
}

func TestDeleteInterviewSessionRejectsOtherUser(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	ownerToken := loginToken(t, handler, "demo", "demo123")
	otherToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", ownerToken, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodDelete, "/api/v1/interviews/sessions/"+created.SessionID, otherToken, nil)
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected delete not found for other user, status=%d env=%+v", status, env)
	}
}
