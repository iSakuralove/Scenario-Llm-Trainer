package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/store"
)

func TestLoginAcceptsLegacyPasswordHashes(t *testing.T) {
	dataStore := store.NewMemoryStore(legacyPasswordHashForTest)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": "demo",
		"password":   "demo123",
	})
	if status != http.StatusOK {
		t.Fatalf("legacy password login status=%d message=%s", status, env.Message)
	}
	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok {
		t.Fatal("expected demo user to exist after login")
	}
	if !strings.HasPrefix(user.PasswordHash, "$2") {
		t.Fatalf("expected legacy hash to be upgraded to bcrypt, got %q", user.PasswordHash)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()

	for _, candidate := range []string{"12345", "   "} {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
			"username": "candidate-" + strings.ReplaceAll(candidate, " ", "space"),
			"email":    "candidate@example.com",
			"password": candidate,
		})
		if status != http.StatusBadRequest {
			t.Fatalf("register short password status=%d message=%s", status, env.Message)
		}
		if env.Message != "密码至少需要 6 位" {
			t.Fatalf("register short password message=%q", env.Message)
		}
	}
}

func legacyPasswordHashForTest(password string) string {
	sum := sha256.Sum256([]byte("mvp-salt:" + password))
	return hex.EncodeToString(sum[:])
}
