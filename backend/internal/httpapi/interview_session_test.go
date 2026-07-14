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

func TestCreateInterviewSessionRejectsUnsupportedFocusAreas(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
		"focus_areas":   []string{"technical_accuracy", "unknown_focus"},
	})
	if status != http.StatusBadRequest || env.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported focus area rejection, status=%d env=%+v", status, env)
	}
	if env.Message != "focus_areas contains unsupported value: unknown_focus" {
		t.Fatalf("unexpected validation message: %q", env.Message)
	}
}

func TestInterviewLaunchpadReturnsOpenTracksFromAvailableQuestions(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		Summary struct {
			OpenTrackCount     int  `json:"open_track_count"`
			PublishedAtomCount int  `json:"published_atom_count"`
			IndexedAtomCount   int  `json:"indexed_atom_count"`
			FallbackMode       bool `json:"fallback_mode"`
		} `json:"summary"`
		OpenTracks []struct {
			ID                string `json:"id"`
			Domain            string `json:"domain"`
			Difficulty        string `json:"difficulty"`
			QuestionType      string `json:"question_type"`
			AvailabilityState string `json:"availability_state"`
		} `json:"open_tracks"`
		FallbackMode bool `json:"fallback_mode"`
	}
	mustDecodeData(t, env, &payload)
	if !payload.FallbackMode || !payload.Summary.FallbackMode {
		t.Fatalf("expected compatibility launchpad mode: %+v", payload)
	}
	if payload.Summary.PublishedAtomCount != 0 || payload.Summary.IndexedAtomCount != 0 {
		t.Fatalf("compatibility launchpad must not claim atom statistics: %+v", payload.Summary)
	}
	if payload.Summary.OpenTrackCount != len(payload.OpenTracks) || len(payload.OpenTracks) == 0 {
		t.Fatalf("unexpected open track count: %+v", payload)
	}
	for _, track := range payload.OpenTracks {
		if track.Domain == "" || track.Difficulty == "" || track.QuestionType == "" {
			t.Fatalf("track must include launch parameters: %+v", track)
		}
		if track.AvailabilityState != "available" {
			t.Fatalf("unexpected track availability: %+v", track)
		}
		if _, ok := dataStore.FindInterviewQuestion(track.Domain, track.Difficulty, track.QuestionType); !ok {
			t.Fatalf("launchpad returned non-startable track: %+v", track)
		}
	}
}

func TestInterviewLaunchpadIncludesRecommendedTracksAndRecentSessions(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}
	question, ok := dataStore.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok || question == nil {
		t.Fatal("expected database L3 scenario question")
	}
	session := dataStore.CreateInterviewSession(user.ID, question)
	if session == nil {
		t.Fatal("expected created interview session")
	}
	session.Status = "follow_up_1_presented"
	dataStore.SaveInterviewSession(session)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		RecommendedTracks []struct {
			ID         string `json:"id"`
			Domain     string `json:"domain"`
			Difficulty string `json:"difficulty"`
			Reason     string `json:"reason"`
			SourceKind string `json:"source_kind"`
		} `json:"recommended_tracks"`
		RecentSessions []struct {
			ID            string `json:"id"`
			Status        string `json:"status"`
			Domain        string `json:"domain"`
			Difficulty    string `json:"difficulty"`
			QuestionTitle string `json:"question_title"`
		} `json:"recent_sessions"`
	}
	mustDecodeData(t, env, &payload)
	if len(payload.RecentSessions) == 0 {
		t.Fatalf("expected recent sessions in launchpad, got %+v", payload)
	}
	if payload.RecentSessions[0].ID != session.ID || payload.RecentSessions[0].Status != "follow_up_1_presented" {
		t.Fatalf("unexpected recent session payload: %+v", payload.RecentSessions[0])
	}
	if len(payload.RecommendedTracks) == 0 {
		t.Fatalf("expected recommended tracks in launchpad, got %+v", payload)
	}
	first := payload.RecommendedTracks[0]
	if first.Domain != "database" || first.Difficulty != "L3" {
		t.Fatalf("expected unfinished session track to be recommended first, got %+v", first)
	}
	if first.Reason == "" || first.SourceKind == "" {
		t.Fatalf("expected recommendation reason/source_kind, got %+v", first)
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

func TestCreateInterviewSessionPersistsSessionInputs(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]interface{}{
		"domain":           "database",
		"difficulty":       "L3",
		"question_type":    "scenario_analysis",
		"difficulty_level": "challenge",
		"focus_areas":      []string{"technical_accuracy", "technical_accuracy", "solution_feasibility"},
		"setup_notes":      "简历摘要：做过慢查询治理。",
	})
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("create interview status=%d env=%+v", status, env)
	}
	var payload struct {
		Session domain.InterviewSession `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Session.DifficultyLevel != "challenge" {
		t.Fatalf("expected difficulty_level persisted, got %+v", payload.Session)
	}
	if got := payload.Session.FocusAreas; len(got) != 2 || got[0] != "technical_accuracy" || got[1] != "solution_feasibility" {
		t.Fatalf("expected normalized focus areas, got %+v", got)
	}
	if payload.Session.SetupNotes != "简历摘要：做过慢查询治理。" {
		t.Fatalf("expected setup notes persisted, got %q", payload.Session.SetupNotes)
	}
}

func TestCreateInterviewSessionPrefersPublishedOpeningAtom(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-opening-cache", "published", "indexed")
	atom.Title = "缓存击穿开场题"
	atom.Subject = "缓存击穿"
	atom.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "开场题"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "cache",
		"difficulty":    "L3",
		"question_type": "principle",
	})
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("create interview status=%d env=%+v", status, env)
	}
	var payload struct {
		Question domain.InterviewQuestion `json:"question"`
		Session  domain.InterviewSession  `json:"session"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Question.ID != "interview-knowledge:atom-opening-cache:v1" {
		t.Fatalf("expected synthetic interview knowledge question, got %+v", payload.Question)
	}
	if payload.Question.ReferenceAnswer != "" || len(payload.Question.ReferenceKeywords) != 0 {
		t.Fatalf("student question must not expose atom references: %+v", payload.Question)
	}
	if payload.Session.QuestionSnapshot.QuestionSource != "interview_knowledge" || payload.Session.QuestionSnapshot.Subject != "缓存击穿" {
		t.Fatalf("expected interview knowledge snapshot, got %+v", payload.Session.QuestionSnapshot)
	}
	if len(payload.Session.SelectedAtomSnapshots) != 0 {
		t.Fatalf("session view must not expose selected atom snapshots: %+v", payload.Session.SelectedAtomSnapshots)
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

func TestInterviewLaunchpadAtomTracksIncludeTags(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-launchpad-java-tags", "published", "indexed")
	atom.Category = "java"
	atom.Domain = "java"
	atom.QuestionRole = "opening"
	atom.Tags = []string{"collections", "hashmap", "java"}
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "launchpad tags"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		FallbackMode bool `json:"fallback_mode"`
		OpenTracks   []struct {
			Domain string   `json:"domain"`
			Tags   []string `json:"tags"`
		} `json:"open_tracks"`
	}
	mustDecodeData(t, env, &payload)
	if payload.FallbackMode {
		t.Fatalf("expected atom-backed launchpad mode, got %+v", payload)
	}
	if len(payload.OpenTracks) == 0 {
		t.Fatalf("expected atom-backed tracks, got %+v", payload)
	}
	found := false
	for _, track := range payload.OpenTracks {
		if track.Domain != "java" {
			continue
		}
		found = true
		if len(track.Tags) == 0 {
			t.Fatalf("expected java track tags, got %+v", track)
		}
	}
	if !found {
		t.Fatalf("expected java launchpad track, got %+v", payload.OpenTracks)
	}
}

func TestInterviewLaunchpadSummaryStateForRetrievalDegraded(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-launchpad-degraded", "published", "pending")
	atom.Category = "cache"
	atom.Domain = "cache"
	atom.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "launchpad degraded"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		FallbackMode bool `json:"fallback_mode"`
		Summary      struct {
			State              string `json:"state"`
			PublishedAtomCount int    `json:"published_atom_count"`
			IndexedAtomCount   int    `json:"indexed_atom_count"`
		} `json:"summary"`
	}
	mustDecodeData(t, env, &payload)
	if payload.FallbackMode {
		t.Fatalf("expected atom-backed launchpad mode, got %+v", payload)
	}
	if payload.Summary.State != "retrieval_degraded" {
		t.Fatalf("expected retrieval_degraded state, got %+v", payload.Summary)
	}
	if payload.Summary.PublishedAtomCount == 0 || payload.Summary.IndexedAtomCount != 0 {
		t.Fatalf("unexpected degraded summary counts: %+v", payload.Summary)
	}
}

func TestInterviewLaunchpadRecommendsTrackFromWeakDimension(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}
	question, ok := dataStore.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok || question == nil {
		t.Fatal("expected database L3 interview question")
	}
	session := dataStore.CreateInterviewSession(user.ID, question)
	if session == nil {
		t.Fatal("expected created interview session")
	}
	session.Status = "final_evaluated"
	session.FinalScore = 58
	session.QuestionSnapshot.Domain = "database"
	session.QuestionSnapshot.Difficulty = "L3"
	session.QuestionSnapshot.Title = "数据库索引与慢查询"
	session.Evaluations = []domain.InterviewEvaluation{
		{
			Round:           1,
			TotalScore:      58,
			DimensionScores: map[string]int{"technical_accuracy": 55, "logical_completeness": 78, "solution_feasibility": 80},
			CreatedAt:       time.Now(),
		},
	}
	dataStore.SaveInterviewSession(session)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		RecommendedTracks []struct {
			Domain     string `json:"domain"`
			Difficulty string `json:"difficulty"`
			Reason     string `json:"reason"`
			SourceKind string `json:"source_kind"`
		} `json:"recommended_tracks"`
	}
	mustDecodeData(t, env, &payload)
	if len(payload.RecommendedTracks) == 0 {
		t.Fatalf("expected recommended tracks, got %+v", payload)
	}
	found := false
	for _, item := range payload.RecommendedTracks {
		if item.SourceKind != "weak_dimension" {
			continue
		}
		found = true
		if item.Domain != "database" || item.Difficulty != "L3" {
			t.Fatalf("unexpected weak-dimension recommendation target: %+v", item)
		}
		if !strings.Contains(item.Reason, "技术准确性") {
			t.Fatalf("expected weak-dimension reason to mention 技术准确性, got %+v", item)
		}
	}
	if !found {
		t.Fatalf("expected weak_dimension recommendation, got %+v", payload.RecommendedTracks)
	}
}

func TestInterviewLaunchpadRecentSessionsIncludeWeakDimensionSummary(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}
	question, ok := dataStore.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok || question == nil {
		t.Fatal("expected database L3 interview question")
	}
	session := dataStore.CreateInterviewSession(user.ID, question)
	if session == nil {
		t.Fatal("expected created interview session")
	}
	session.Status = "final_evaluated"
	session.FinalScore = 64
	session.QuestionSnapshot.Domain = "database"
	session.QuestionSnapshot.Difficulty = "L3"
	session.QuestionSnapshot.Title = "缓存击穿与回退"
	session.Evaluations = []domain.InterviewEvaluation{
		{
			Round:           1,
			TotalScore:      64,
			DimensionScores: map[string]int{"solution_feasibility": 61, "technical_accuracy": 75},
			CreatedAt:       time.Now(),
		},
	}
	dataStore.SaveInterviewSession(session)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		RecentSessions []struct {
			ID            string `json:"id"`
			WeakDimension string `json:"weak_dimension"`
			WeakScore     int    `json:"weak_score"`
		} `json:"recent_sessions"`
	}
	mustDecodeData(t, env, &payload)
	if len(payload.RecentSessions) == 0 {
		t.Fatalf("expected recent sessions, got %+v", payload)
	}
	if payload.RecentSessions[0].ID != session.ID || payload.RecentSessions[0].WeakDimension != "方案可落地性" || payload.RecentSessions[0].WeakScore != 61 {
		t.Fatalf("unexpected recent session weak summary: %+v", payload.RecentSessions[0])
	}
}

func TestInterviewLaunchpadRecommendsRecentlyUpdatedTrack(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}
	profile := user.Profile
	profile.PreferredDomains = []string{}
	if _, err := dataStore.SaveUserProfile(user.ID, profile); err != nil {
		t.Fatal(err)
	}
	atom := validInterviewBankAtomForRebuild("atom-launchpad-fresh-cache", "published", "indexed")
	atom.Category = "cache"
	atom.Domain = "cache"
	atom.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "fresh launchpad content"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		RecommendedTracks []struct {
			Domain     string `json:"domain"`
			Reason     string `json:"reason"`
			SourceKind string `json:"source_kind"`
		} `json:"recommended_tracks"`
	}
	mustDecodeData(t, env, &payload)
	found := false
	for _, item := range payload.RecommendedTracks {
		if item.SourceKind != "fresh_content" {
			continue
		}
		found = true
		if item.Domain != "cache" {
			t.Fatalf("unexpected fresh-content domain: %+v", item)
		}
		if !strings.Contains(item.Reason, "最近更新") && !strings.Contains(item.Reason, "新发布") {
			t.Fatalf("expected fresh-content reason to mention recent update, got %+v", item)
		}
	}
	if !found {
		t.Fatalf("expected fresh_content recommendation, got %+v", payload.RecommendedTracks)
	}
}

func TestInterviewLaunchpadRecommendsHabitualTrack(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}
	profile := user.Profile
	profile.PreferredDomains = []string{}
	if _, err := dataStore.SaveUserProfile(user.ID, profile); err != nil {
		t.Fatal(err)
	}

	question, ok := dataStore.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok || question == nil {
		t.Fatal("expected database L3 interview question")
	}
	first := dataStore.CreateInterviewSession(user.ID, question)
	second := dataStore.CreateInterviewSession(user.ID, question)
	if first == nil || second == nil {
		t.Fatal("expected created interview sessions")
	}
	first.Status = "final_evaluated"
	first.FinalScore = 82
	first.QuestionSnapshot.Domain = "database"
	first.QuestionSnapshot.Difficulty = "L3"
	first.StartedAt = time.Now().Add(-2 * time.Hour)
	second.Status = "final_evaluated"
	second.FinalScore = 84
	second.QuestionSnapshot.Domain = "database"
	second.QuestionSnapshot.Difficulty = "L3"
	second.StartedAt = time.Now().Add(-30 * time.Minute)
	dataStore.SaveInterviewSession(first)
	dataStore.SaveInterviewSession(second)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}
	var payload struct {
		RecommendedTracks []struct {
			Domain     string `json:"domain"`
			Difficulty string `json:"difficulty"`
			Reason     string `json:"reason"`
			SourceKind string `json:"source_kind"`
		} `json:"recommended_tracks"`
	}
	mustDecodeData(t, env, &payload)
	found := false
	for _, item := range payload.RecommendedTracks {
		if item.SourceKind != "habitual_track" {
			continue
		}
		found = true
		if item.Domain != "database" || item.Difficulty != "L3" {
			t.Fatalf("unexpected habitual-track recommendation target: %+v", item)
		}
		if !strings.Contains(item.Reason, "最常练") && !strings.Contains(item.Reason, "常用") {
			t.Fatalf("expected habitual-track reason to mention usage habit, got %+v", item)
		}
	}
	if !found {
		t.Fatalf("expected habitual_track recommendation, got %+v", payload.RecommendedTracks)
	}
}

func TestInterviewLaunchpadCoverageStatsSummarizeCompletedTrackCoverage(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}

	databaseAtom := validInterviewBankAtomForRebuild("atom-launchpad-coverage-database", "published", "indexed")
	databaseAtom.Category = "database"
	databaseAtom.Domain = "database"
	databaseAtom.Difficulty = "L3"
	databaseAtom.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(databaseAtom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "coverage database"); err != nil {
		t.Fatal(err)
	}

	cacheAtom := validInterviewBankAtomForRebuild("atom-launchpad-coverage-cache", "published", "indexed")
	cacheAtom.Category = "cache"
	cacheAtom.Domain = "cache"
	cacheAtom.Difficulty = "L2"
	cacheAtom.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(cacheAtom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "coverage cache"); err != nil {
		t.Fatal(err)
	}

	question, ok := dataStore.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok || question == nil {
		t.Fatal("expected database L3 interview question")
	}
	session := dataStore.CreateInterviewSession(user.ID, question)
	if session == nil {
		t.Fatal("expected created interview session")
	}
	session.Status = "final_evaluated"
	session.FinalScore = 72
	session.QuestionSnapshot.Domain = "database"
	session.QuestionSnapshot.Difficulty = "L3"
	session.QuestionSnapshot.Subject = "MySQL 索引下推"
	session.Evaluations = []domain.InterviewEvaluation{
		{
			Round:             1,
			TotalScore:        72,
			DimensionScores:   map[string]int{"technical_accuracy": 70, "logical_completeness": 74},
			FollowUpSubject:   "MySQL 索引下推",
			FollowUpType:      "deepen",
			RetrievedSubjects: []string{"MySQL 索引下推"},
			CreatedAt:         time.Now(),
		},
		{
			Round:             2,
			TotalScore:        68,
			DimensionScores:   map[string]int{"solution_feasibility": 68},
			FollowUpSubject:   "慢查询定位",
			FollowUpType:      "broaden",
			RetrievedSubjects: []string{"慢查询定位"},
			CreatedAt:         time.Now(),
		},
	}
	dataStore.SaveInterviewSession(session)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/launchpad", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("launchpad status=%d env=%+v", status, env)
	}

	var payload struct {
		CoverageStats struct {
			TotalOpenTracks      int      `json:"total_open_tracks"`
			PracticedOpenTracks  int      `json:"practiced_open_tracks"`
			CoveragePercent      int      `json:"coverage_percent"`
			CompletedSessions    int      `json:"completed_sessions"`
			PracticedDomains     []string `json:"practiced_domains"`
			PracticedDifficulties []string `json:"practiced_difficulties"`
			SubjectCount         int      `json:"subject_count"`
			TopSubjects          []string `json:"top_subjects"`
			UncoveredTrackIDs    []string `json:"uncovered_track_ids"`
		} `json:"coverage_stats"`
	}
	mustDecodeData(t, env, &payload)

	stats := payload.CoverageStats
	if stats.TotalOpenTracks != 2 || stats.PracticedOpenTracks != 1 {
		t.Fatalf("unexpected launchpad coverage track counts: %+v", stats)
	}
	if stats.CoveragePercent != 50 {
		t.Fatalf("expected 50 percent launchpad coverage, got %+v", stats)
	}
	if stats.CompletedSessions != 1 {
		t.Fatalf("expected one completed session, got %+v", stats)
	}
	if len(stats.PracticedDomains) != 1 || stats.PracticedDomains[0] != "database" {
		t.Fatalf("unexpected practiced domains: %+v", stats)
	}
	if len(stats.PracticedDifficulties) != 1 || stats.PracticedDifficulties[0] != "L3" {
		t.Fatalf("unexpected practiced difficulties: %+v", stats)
	}
	if stats.SubjectCount != 2 {
		t.Fatalf("expected two covered subjects, got %+v", stats)
	}
	if len(stats.TopSubjects) != 2 || stats.TopSubjects[0] != "MySQL 索引下推" {
		t.Fatalf("unexpected top subjects: %+v", stats)
	}
	if len(stats.UncoveredTrackIDs) != 1 || stats.UncoveredTrackIDs[0] != "interview-bank-cache-l2" {
		t.Fatalf("unexpected uncovered tracks: %+v", stats)
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
		SessionID string                   `json:"session_id"`
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

func TestInterviewReportReturnsRetrievalSummaryWithoutAtomInternals(t *testing.T) {
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
	session, ok := dataStore.GetInterviewSession(created.SessionID)
	if !ok {
		t.Fatal("expected created session")
	}
	session.Status = "final_evaluated"
	session.FinalScore = 82
	session.FinalReport = "整体达到要求。"
	session.QuestionSnapshot.Subject = "慢查询定位"
	session.SelectedAtomSnapshots = []domain.InterviewKnowledgeAtomLightSnapshot{{
		AtomID:  "atom-internal",
		Title:   "管理端内部标题",
		Subject: "索引治理",
	}}
	session.Evaluations = []domain.InterviewEvaluation{
		{Round: 1, TotalScore: 58, FollowUpType: "deepen", FollowUpSubject: "索引治理", RetrievedSubjects: []string{"索引治理"}, CreatedAt: time.Now()},
		{Round: 2, TotalScore: 82, FollowUpType: "fallback_rule_only", FollowUpSubject: "回滚验证", FallbackUsed: true, CreatedAt: time.Now()},
	}
	dataStore.SaveInterviewSession(session)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/sessions/"+created.SessionID+"/report", token, nil)
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("report status=%d env=%+v", status, env)
	}
	var report struct {
		RetrievalSummary struct {
			SummaryText    string `json:"summary_text"`
			HitRounds      int    `json:"hit_rounds"`
			FallbackRounds int    `json:"fallback_rounds"`
			SubjectCount   int    `json:"subject_count"`
			Coverage       []struct {
				Subject        string   `json:"subject"`
				RoundCount     int      `json:"round_count"`
				HitCount       int      `json:"hit_count"`
				FallbackCount  int      `json:"fallback_count"`
				AverageScore   int      `json:"average_score"`
				LowestScore    int      `json:"lowest_score"`
				WeakDimensions []string `json:"weak_dimensions"`
			} `json:"coverage"`
			RetrainingSuggestions []struct {
				Subject      string   `json:"subject"`
				Priority     int      `json:"priority"`
				Actions      []string `json:"actions"`
				SourceRounds []int    `json:"source_rounds"`
			} `json:"retraining_suggestions"`
			Rounds []struct {
				Round        int    `json:"round"`
				Subject      string `json:"subject"`
				FallbackUsed bool   `json:"fallback_used"`
				FollowUpType string `json:"follow_up_type"`
			} `json:"rounds"`
		} `json:"retrieval_summary"`
	}
	mustDecodeData(t, env, &report)
	if report.RetrievalSummary.SubjectCount != 2 || report.RetrievalSummary.HitRounds != 1 || report.RetrievalSummary.FallbackRounds != 1 {
		t.Fatalf("unexpected retrieval summary: %+v", report.RetrievalSummary)
	}
	if len(report.RetrievalSummary.Rounds) != 2 || report.RetrievalSummary.Rounds[0].Subject != "索引治理" || !report.RetrievalSummary.Rounds[1].FallbackUsed {
		t.Fatalf("unexpected round summaries: %+v", report.RetrievalSummary.Rounds)
	}
	if len(report.RetrievalSummary.Coverage) != 2 || report.RetrievalSummary.Coverage[0].Subject != "回滚验证" || report.RetrievalSummary.Coverage[1].Subject != "索引治理" {
		t.Fatalf("unexpected knowledge coverage: %+v", report.RetrievalSummary.Coverage)
	}
	if len(report.RetrievalSummary.RetrainingSuggestions) == 0 {
		t.Fatalf("expected retraining suggestions: %+v", report.RetrievalSummary)
	}
	raw := string(env.Data)
	for _, forbidden := range []string{"selected_atom_snapshots", "管理端内部标题", "principles", "follow_up_paths", "query_text"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("report must not expose %s in %s", forbidden, raw)
		}
	}
}

func TestBuildInterviewReportRetrievalSummaryKnowledgeCoverage(t *testing.T) {
	session := &domain.InterviewSession{
		QuestionSnapshot: domain.InterviewQuestionSnapshot{Subject: "缓存基础"},
		Evaluations: []domain.InterviewEvaluation{
			{
				Round:             1,
				TotalScore:        58,
				DimensionScores:   map[string]int{"technical_accuracy": 55, "logical_completeness": 69, "expression_structure": 88},
				FollowUpSubject:   "缓存穿透",
				FollowUpType:      "deepen",
				RetrievedSubjects: []string{"缓存穿透"},
				CreatedAt:         time.Now(),
			},
			{
				Round:             2,
				TotalScore:        82,
				DimensionScores:   map[string]int{"technical_accuracy": 85, "logical_completeness": 80},
				FollowUpSubject:   "缓存穿透",
				FollowUpType:      "supplement",
				RetrievedSubjects: []string{"缓存穿透"},
				CreatedAt:         time.Now(),
			},
			{
				Round:           3,
				TotalScore:      70,
				DimensionScores: map[string]int{"solution_feasibility": 64},
				FollowUpSubject: "回滚验证",
				FollowUpType:    "fallback_rule_only",
				FallbackUsed:    true,
				CreatedAt:       time.Now(),
			},
		},
	}

	summary := buildInterviewReportRetrievalSummary(session)
	if summary.SubjectCount != 2 || summary.HitRounds != 2 || summary.FallbackRounds != 1 {
		t.Fatalf("unexpected summary counters: %+v", summary)
	}
	if len(summary.Coverage) != 2 {
		t.Fatalf("expected 2 coverage items, got %+v", summary.Coverage)
	}
	cacheCoverage := summary.Coverage[0]
	if cacheCoverage.Subject != "缓存穿透" || cacheCoverage.RoundCount != 2 || cacheCoverage.HitCount != 2 || cacheCoverage.AverageScore != 70 || cacheCoverage.LowestScore != 58 {
		t.Fatalf("unexpected cache coverage: %+v", cacheCoverage)
	}
	if got := strings.Join(cacheCoverage.WeakDimensions, "、"); got != "技术准确性、逻辑完整性" {
		t.Fatalf("unexpected weak dimensions: %q", got)
	}
	if len(summary.RetrainingSuggestions) < 2 {
		t.Fatalf("expected low score and fallback suggestions: %+v", summary.RetrainingSuggestions)
	}
	var cacheSuggestion *interviewReportRetrainingSuggestion
	for i := range summary.RetrainingSuggestions {
		if summary.RetrainingSuggestions[i].Subject == "缓存穿透" {
			cacheSuggestion = &summary.RetrainingSuggestions[i]
			break
		}
	}
	if cacheSuggestion == nil || cacheSuggestion.Priority != 1 || len(cacheSuggestion.SourceRounds) != 2 || cacheSuggestion.SourceRounds[0] != 1 || cacheSuggestion.SourceRounds[1] != 2 {
		t.Fatalf("unexpected cache suggestion: %+v", summary.RetrainingSuggestions)
	}
}

func TestBuildInterviewReportRetrievalSummaryEmptySession(t *testing.T) {
	summary := buildInterviewReportRetrievalSummary(&domain.InterviewSession{})
	if summary.SummaryText != "本场暂无追问检索记录。" {
		t.Fatalf("unexpected empty summary text: %q", summary.SummaryText)
	}
	if len(summary.Coverage) != 0 || len(summary.RetrainingSuggestions) != 0 {
		t.Fatalf("empty report must not create coverage or suggestions: %+v", summary)
	}
}
