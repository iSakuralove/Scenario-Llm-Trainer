package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/store"
)

func TestAnonymousPasswordResetIsDisabledByDefault(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset", "", map[string]string{
		"identifier":   "demo@example.com",
		"new_password": "demo456",
	})
	if status != http.StatusNotFound {
		t.Fatalf("disabled anonymous password reset status=%d message=%s", status, env.Message)
	}
}

func TestAnonymousPasswordResetCanBeEnabledInMemoryMode(t *testing.T) {
	t.Setenv("ENABLE_ANON_PASSWORD_RESET", "true")

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

func TestAnonymousPasswordResetStaysDisabledForPersistentStore(t *testing.T) {
	t.Setenv("ENABLE_ANON_PASSWORD_RESET", "true")
	if anonymousPasswordResetEnabled(&store.PostgresStore{}) {
		t.Fatal("persistent store must keep anonymous password reset disabled")
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
	var rotated struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		User         interface{} `json:"user"`
	}
	mustDecodeData(t, env, &rotated)
	if rotated.AccessToken == "" {
		t.Fatal("expected rotated access token after password reset")
	}
	if rotated.RefreshToken == "" {
		t.Fatal("expected rotated refresh token after password reset")
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "profile456",
	})
	if status != http.StatusOK {
		t.Fatalf("profile reset login status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/users/me", token, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("old token should be revoked after password reset, status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/users/me", rotated.AccessToken, nil)
	if status != http.StatusOK {
		t.Fatalf("new token should remain valid after password reset, status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": rotated.RefreshToken,
	})
	if status != http.StatusOK {
		t.Fatalf("rotated refresh token should remain valid, status=%d message=%s", status, env.Message)
	}
}

func TestPasswordResetRejectsShortPasswordConsistently(t *testing.T) {
	t.Setenv("ENABLE_ANON_PASSWORD_RESET", "true")
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	for _, tc := range []struct {
		name string
		path string
		auth string
	}{
		{name: "anonymous", path: "/api/v1/auth/password-reset", auth: ""},
		{name: "authenticated", path: "/api/v1/users/me/password", auth: token},
	} {
		status, env := requestJSON(t, handler, http.MethodPost, tc.path, tc.auth, map[string]string{
			"identifier":   "demo@example.com",
			"new_password": "12345",
		})
		if tc.path == "/api/v1/users/me/password" {
			status, env = requestJSON(t, handler, http.MethodPost, tc.path, tc.auth, map[string]string{
				"new_password": "12345",
			})
		}
		if status != http.StatusBadRequest {
			t.Fatalf("%s short password status=%d message=%s", tc.name, status, env.Message)
		}
		if env.Message != "密码至少需要 6 位" {
			t.Fatalf("%s short password message=%q", tc.name, env.Message)
		}
	}
}

func TestRotatedRefreshTokenRejectsOldRefreshToken(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()

	loginStatus, loginEnv := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "demo123",
	})
	if loginStatus != http.StatusOK {
		t.Fatalf("login status=%d message=%s", loginStatus, loginEnv.Message)
	}
	var loginData struct {
		RefreshToken string `json:"refresh_token"`
	}
	mustDecodeData(t, loginEnv, &loginData)
	if loginData.RefreshToken == "" {
		t.Fatal("expected old refresh token")
	}

	accessToken := loginToken(t, handler, "demo", "demo123")
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/users/me/password", accessToken, map[string]string{
		"new_password": "profile456",
	})
	if status != http.StatusOK {
		t.Fatalf("password reset status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": loginData.RefreshToken,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("old refresh token should be revoked after password reset, status=%d message=%s", status, env.Message)
	}
}

func TestLoginLimiterDoesNotBlockSuccessfulDemoLogins(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), newCountingLimiter())
	handler := server.Handler()

	for i := 0; i < 6; i++ {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"identifier": "demo",
			"password":   "demo123",
		})
		if status != http.StatusOK {
			t.Fatalf("successful login attempt %d status=%d message=%s", i+1, status, env.Message)
		}
	}
}

func TestFailedLoginAttemptsAreRateLimited(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), newCountingLimiter())
	handler := server.Handler()

	for i := 0; i < 5; i++ {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"identifier": "demo",
			"password":   "wrong-password",
		})
		if status != http.StatusUnauthorized {
			t.Fatalf("failed login attempt %d status=%d message=%s", i+1, status, env.Message)
		}
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "wrong-password",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected failed login rate limit, status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "demo123",
	})
	if status != http.StatusOK {
		t.Fatalf("correct login should recover from failed-attempt limiter, status=%d message=%s", status, env.Message)
	}
}

type countingLimiter struct {
	mu     sync.Mutex
	counts map[string]int
}

func newCountingLimiter() *countingLimiter {
	return &countingLimiter{counts: map[string]int{}}
}

func (l *countingLimiter) Allow(_ context.Context, key string, limit int, _ time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts[key]++
	return l.counts[key] <= limit
}

func (l *countingLimiter) Enabled() bool {
	return true
}
