package httpapi

import (
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestMentorEndpointReturnsSummaryAndCoverage(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok || user == nil {
		t.Fatal("expected demo user")
	}
	profile := user.Profile
	profile.TargetRole = "后端开发工程师"
	profile.ResumeSummary = "做过慢查询治理"
	if _, err := dataStore.SaveUserProfile(user.ID, profile); err != nil {
		t.Fatal(err)
	}
	atom := validInterviewBankAtomForRebuild("mentor-database-track", "published", "indexed")
	atom.Category = "database"
	atom.Domain = "database"
	atom.Difficulty = "L3"
	atom.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "mentor launchpad track"); err != nil {
		t.Fatal(err)
	}
	question, ok := dataStore.FindInterviewQuestion("database", "L3", "scenario_analysis")
	if !ok || question == nil {
		t.Fatal("expected database interview question")
	}
	session := dataStore.CreateInterviewSession(user.ID, question)
	session.Status = "final_evaluated"
	session.FinalScore = 78
	session.QuestionSnapshot.Domain = "database"
	session.QuestionSnapshot.Difficulty = "L3"
	session.QuestionSnapshot.Subject = "慢查询定位"
	session.Evaluations = []domain.InterviewEvaluation{{
		Round:             1,
		TotalScore:        78,
		DimensionScores:   map[string]int{"technical_accuracy": 72, "logical_completeness": 80},
		FollowUpSubject:   "慢查询定位",
		RetrievedSubjects: []string{"慢查询定位"},
		CreatedAt:         time.Now(),
	}}
	dataStore.SaveInterviewSession(session)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/users/me/mentor", token, nil)
	if status != http.StatusOK {
		t.Fatalf("mentor status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Overview   string `json:"overview"`
		Strengths  []string `json:"strengths"`
		Weaknesses []string `json:"weaknesses"`
		Risks      []struct {
			Title string `json:"title"`
		} `json:"risks"`
		Actions []struct {
			Title string `json:"title"`
		} `json:"actions"`
		Coverage struct {
			CompletedSessions int      `json:"completed_sessions"`
			TopSubjects       []string `json:"top_subjects"`
		} `json:"coverage"`
		Profile struct {
			TargetRole       string `json:"target_role"`
			HasResumeSummary bool   `json:"has_resume_summary"`
		} `json:"profile"`
		SampleReady bool `json:"sample_ready"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Overview == "" || len(payload.Strengths) == 0 || len(payload.Weaknesses) == 0 {
		t.Fatalf("unexpected mentor diagnosis payload: %+v", payload)
	}
	if len(payload.Actions) == 0 {
		t.Fatalf("expected mentor actions, got %+v", payload)
	}
	if payload.Coverage.CompletedSessions != 1 || len(payload.Coverage.TopSubjects) == 0 {
		t.Fatalf("unexpected mentor coverage: %+v", payload.Coverage)
	}
	if payload.Profile.TargetRole != "后端开发工程师" || !payload.Profile.HasResumeSummary {
		t.Fatalf("unexpected mentor profile payload: %+v", payload.Profile)
	}
	if !payload.SampleReady {
		t.Fatalf("expected sample_ready=true, got %+v", payload)
	}
}
