package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/store"
)

func TestDefaultCORSAllowsFrontendDevOrigin(t *testing.T) {
	handler := NewServerForTests(store.NewMemoryStore(auth.HashPassword), auth.NewManager("test-secret", time.Hour)).Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected healthz to allow default dev origin, status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("expected allow origin header for localhost dev server, got %q", got)
	}
}

func TestDefaultCORSRejectsUnknownOrigin(t *testing.T) {
	handler := NewServerForTests(store.NewMemoryStore(auth.HashPassword), auth.NewManager("test-secret", time.Hour)).Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected unknown origin to be rejected, status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow origin header for unknown origin, got %q", got)
	}
}

func TestConfiguredCORSAllowsExplicitOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://review.example.com,https://dashboard.example.com")
	handler := NewServerForTests(store.NewMemoryStore(auth.HashPassword), auth.NewManager("test-secret", time.Hour)).Handler()

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "https://review.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected configured origin preflight to succeed, status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://review.example.com" {
		t.Fatalf("expected configured allow origin header, got %q", got)
	}
}

func TestSystemAIPublicStatusOmitsSensitiveProviderFields(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	server.setLLMRouter(ai.NewRouter(ai.Config{
		Provider: ai.ProviderOpenAICompatible,
		BaseURL:  "https://internal-router.example.com",
		APIKey:   "unit-test-key",
		Model:    "gpt-5.5",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/ai", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected public system ai endpoint to succeed, status=%d body=%s", rr.Code, rr.Body.String())
	}
	var env testEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	var raw map[string]any
	mustDecodeData(t, env, &raw)
	if raw["provider"] == "" || raw["model"] == "" {
		t.Fatalf("expected public ai status to retain provider/model, got %#v", raw)
	}
	for _, forbidden := range []string{"base_url", "init_error", "last_error", "last_trace_id", "telemetry", "provider_pool"} {
		if _, ok := raw[forbidden]; ok {
			t.Fatalf("public ai status must omit %q, got %#v", forbidden, raw[forbidden])
		}
	}
}
