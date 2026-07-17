package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/store"
)

func TestBuildPasswordResetMailIncludesClickableHTMLAlternative(t *testing.T) {
	link := "http://localhost:5173/reset-password?token=test-token"
	built, err := buildPasswordResetMail("sender@example.com", "student@example.com", link)
	if err != nil {
		t.Fatalf("build password reset mail: %v", err)
	}
	if built.envelopeFrom != "sender@example.com" || built.envelopeTo != "student@example.com" {
		t.Fatalf("unexpected envelope addresses: from=%q to=%q", built.envelopeFrom, built.envelopeTo)
	}

	message, err := mail.ReadMessage(bytes.NewReader(built.data))
	if err != nil {
		t.Fatalf("parse password reset mail: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse mail content type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("content type=%q, want multipart/alternative", mediaType)
	}

	parts := multipart.NewReader(message.Body, params["boundary"])
	var htmlBody string
	for {
		part, partErr := parts.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			t.Fatalf("read mail part: %v", partErr)
		}
		if strings.HasPrefix(part.Header.Get("Content-Type"), "text/html") {
			content, readErr := io.ReadAll(part)
			if readErr != nil {
				t.Fatalf("read HTML mail part: %v", readErr)
			}
			htmlBody = string(content)
		}
	}
	if !strings.Contains(htmlBody, `href="`+link+`"`) {
		t.Fatalf("HTML mail does not contain clickable reset link: %q", htmlBody)
	}
	if !strings.Contains(htmlBody, "10 分钟") {
		t.Fatalf("HTML mail does not explain expiry: %q", htmlBody)
	}
}

func TestBuildPasswordResetMailRejectsHeaderInjection(t *testing.T) {
	_, err := buildPasswordResetMail(
		"sender@example.com",
		"student@example.com\r\nBcc: attacker@example.com",
		"http://localhost:5173/reset-password?token=test-token",
	)
	if err == nil {
		t.Fatal("expected injected recipient header to be rejected")
	}
}

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

// TestPasswordResetVerifyEndpoint 覆盖邮件 token 链路的三态：有效、10 分钟后过期、
// 以及一次性失效（改密后 TokenVersion 递增导致旧链接失效）。此前该链路无测试覆盖。
func TestPasswordResetVerifyEndpoint(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	authManager := auth.NewManager("test-secret", time.Hour)
	server := NewServerForTests(dataStore, authManager)
	handler := server.Handler()

	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok {
		t.Fatal("demo user should exist")
	}

	// 有效 token → 校验通过。
	validToken, err := authManager.SignWithVersion(user.ID, user.Role, "password_reset", 10*time.Minute, user.TokenVersion)
	if err != nil {
		t.Fatalf("sign valid reset token: %v", err)
	}
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset/verify", "", map[string]string{
		"token": validToken,
	})
	if status != http.StatusOK {
		t.Fatalf("valid token verify status=%d message=%s", status, env.Message)
	}

	// 过期 token → 校验失败。
	expiredToken, err := authManager.SignWithVersion(user.ID, user.Role, "password_reset", -time.Minute, user.TokenVersion)
	if err != nil {
		t.Fatalf("sign expired reset token: %v", err)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset/verify", "", map[string]string{
		"token": expiredToken,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expired token verify status=%d message=%s", status, env.Message)
	}

	// 空 token → 校验失败。
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset/verify", "", map[string]string{
		"token": "",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("empty token verify status=%d message=%s", status, env.Message)
	}

	// 用一个有效 token 完成改密后，TokenVersion 递增，旧的（同版本）token 失效。
	onetimeToken, err := authManager.SignWithVersion(user.ID, user.Role, "password_reset", 10*time.Minute, user.TokenVersion)
	if err != nil {
		t.Fatalf("sign one-time reset token: %v", err)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset", "", map[string]string{
		"token":        onetimeToken,
		"new_password": "demo456",
	})
	if status != http.StatusOK {
		t.Fatalf("password reset status=%d message=%s", status, env.Message)
	}
	// 改密后再次校验同一 token → 已使用，失败。
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/auth/password-reset/verify", "", map[string]string{
		"token": onetimeToken,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("reused token verify status=%d message=%s", status, env.Message)
	}
}
