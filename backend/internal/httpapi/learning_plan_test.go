package httpapi

import (
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestLearningPlanDashboardCalendarAndCheckin(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/users/me/dashboard", token, nil)
	if status != http.StatusOK {
		t.Fatalf("dashboard status=%d message=%s", status, env.Message)
	}
	var dashboard struct {
		LearningPlan   domain.LearningPlan   `json:"learning_plan"`
		ReviewCalendar domain.ReviewCalendar `json:"review_calendar"`
		WeakPoints     []domain.WeakPoint    `json:"weak_points"`
	}
	mustDecodeData(t, env, &dashboard)
	if len(dashboard.LearningPlan.DomainInsights) == 0 {
		t.Fatal("expected learning domain insights")
	}
	if len(dashboard.LearningPlan.Recommendations) == 0 {
		t.Fatal("expected learning recommendations")
	}
	if len(dashboard.ReviewCalendar.ReviewPlan) != 3 {
		t.Fatalf("expected three review plan items, got %d", len(dashboard.ReviewCalendar.ReviewPlan))
	}
	if dashboard.ReviewCalendar.StreakDays != 0 || dashboard.ReviewCalendar.TodayChecked {
		t.Fatalf("expected fresh demo account to have no checkin streak, got %+v", dashboard.ReviewCalendar)
	}
	if dashboard.ReviewCalendar.NextAction == "" {
		t.Fatal("expected review calendar next action")
	}
	if len(dashboard.WeakPoints) == 0 {
		t.Fatal("expected weak points")
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/users/me/checkin", token, nil)
	if status != http.StatusOK {
		t.Fatalf("checkin status=%d message=%s", status, env.Message)
	}
	var first struct {
		Checkin domain.CheckinResult `json:"checkin"`
		User    domain.User          `json:"user"`
	}
	mustDecodeData(t, env, &first)
	if first.Checkin.AlreadyCheckedIn || first.Checkin.StreakDays != 1 {
		t.Fatalf("unexpected first checkin: %+v", first.Checkin)
	}
	if first.User.Profile.LastCheckinDate == "" || len(first.User.Profile.CheckinDates) == 0 {
		t.Fatalf("expected persisted checkin profile, got %+v", first.User.Profile)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/users/me/checkin", token, nil)
	if status != http.StatusOK {
		t.Fatalf("repeat checkin status=%d message=%s", status, env.Message)
	}
	var second struct {
		Checkin domain.CheckinResult `json:"checkin"`
		User    domain.User          `json:"user"`
	}
	mustDecodeData(t, env, &second)
	if !second.Checkin.AlreadyCheckedIn {
		t.Fatalf("expected idempotent repeat checkin, got %+v", second.Checkin)
	}
	if second.Checkin.StreakDays != first.Checkin.StreakDays {
		t.Fatalf("repeat checkin changed streak: before=%d after=%d", first.Checkin.StreakDays, second.Checkin.StreakDays)
	}
	if len(second.User.Profile.CheckinDates) != len(first.User.Profile.CheckinDates) {
		t.Fatalf("repeat checkin duplicated date: before=%v after=%v", first.User.Profile.CheckinDates, second.User.Profile.CheckinDates)
	}
}

func TestDashboardLearningPlanIncludesInterviewRetrainingLoop(t *testing.T) {
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
	session.FinalReport = "需要继续补强。"
	session.QuestionSnapshot.Subject = "缓存穿透"
	session.Evaluations = []domain.InterviewEvaluation{
		{
			Round:             1,
			TotalScore:        58,
			DimensionScores:   map[string]int{"technical_accuracy": 55, "logical_completeness": 69},
			FollowUpSubject:   "缓存穿透",
			FollowUpType:      "deepen",
			RetrievedSubjects: []string{"缓存穿透"},
			CreatedAt:         time.Now(),
		},
		{
			Round:           2,
			TotalScore:      70,
			DimensionScores: map[string]int{"solution_feasibility": 64},
			FollowUpSubject: "回滚验证",
			FollowUpType:    "fallback_rule_only",
			FallbackUsed:    true,
			CreatedAt:       time.Now(),
		},
	}
	dataStore.SaveInterviewSession(session)

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/users/me/dashboard", token, nil)
	if status != http.StatusOK {
		t.Fatalf("dashboard status=%d message=%s", status, env.Message)
	}
	var dashboard struct {
		LearningPlan struct {
			Recommendations []domain.LearningRecommendation `json:"recommendations"`
		} `json:"learning_plan"`
		ReviewCalendar struct {
			ReviewPlan []domain.ReviewPlanItem `json:"review_plan"`
		} `json:"review_calendar"`
	}
	mustDecodeData(t, env, &dashboard)

	foundInterviewRecommendation := false
	for _, item := range dashboard.LearningPlan.Recommendations {
		if item.Kind != "interview" {
			continue
		}
		foundInterviewRecommendation = true
		if item.ActionPath != "/interviews" || item.Reason == "" {
			t.Fatalf("unexpected interview recommendation: %+v", item)
		}
	}
	if !foundInterviewRecommendation {
		t.Fatalf("expected interview recommendation in learning plan, got %+v", dashboard.LearningPlan.Recommendations)
	}

	foundInterviewReviewItem := false
	for _, item := range dashboard.ReviewCalendar.ReviewPlan {
		if item.SourceKind != "interview_retraining" {
			continue
		}
		foundInterviewReviewItem = true
		if item.Reason == "" || len(item.Actions) == 0 {
			t.Fatalf("unexpected interview review plan item: %+v", item)
		}
	}
	if !foundInterviewReviewItem {
		t.Fatalf("expected interview retraining review item, got %+v", dashboard.ReviewCalendar.ReviewPlan)
	}
}
