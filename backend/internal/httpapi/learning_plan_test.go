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
