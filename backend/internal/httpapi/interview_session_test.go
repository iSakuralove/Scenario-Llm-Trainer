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
