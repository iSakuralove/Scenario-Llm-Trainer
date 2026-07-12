package httpapi

import (
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestProfileUpdatePersistsTargetRole(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPut, "/api/v1/users/me/profile", token, map[string]any{
		"target_level":      "senior",
		"target_role":       "后端开发工程师",
		"preferred_domains": []string{"database", "cache"},
		"resume_summary":    "做过慢查询治理",
		"project_summary":   "做过缓存一致性改造",
	})
	if status != http.StatusOK {
		t.Fatalf("profile update status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Profile.TargetRole != "后端开发工程师" {
		t.Fatalf("expected target_role to persist, got %+v", updated.Profile)
	}
	if updated.Profile.TargetLevel != "senior" {
		t.Fatalf("expected target_level to persist, got %+v", updated.Profile)
	}
}
