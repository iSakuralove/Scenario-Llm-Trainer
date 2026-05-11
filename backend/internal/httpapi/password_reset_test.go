package httpapi

import (
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/store"
)

func TestPasswordResetAllowsLoginWithNewPassword(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset", "", map[string]string{
		"identifier":   "demo@example.com",
		"new_password": "demo456",
	})
	if status != http.StatusOK {
		t.Fatalf("password reset status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "demo456",
	})
	if status != http.StatusOK {
		t.Fatalf("new password login status=%d message=%s", status, env.Message)
	}

	status, _ = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "demo123",
	})
	if status == http.StatusOK {
		t.Fatal("old password should not login after reset")
	}
}

func TestAuthenticatedPasswordResetUsesCurrentUser(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/users/me/password", token, map[string]string{
		"new_password": "profile456",
	})
	if status != http.StatusOK {
		t.Fatalf("profile password reset status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "profile456",
	})
	if status != http.StatusOK {
		t.Fatalf("profile reset login status=%d message=%s", status, env.Message)
	}
}
