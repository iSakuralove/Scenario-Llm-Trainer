package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResolveStoreConfigDefaultsToPostgres(t *testing.T) {
	cfg, err := resolveStoreConfig("", "postgres://teaching:teaching@localhost:5432/teaching_mvp?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "postgres" || !cfg.Persistent {
		t.Fatalf("expected default postgres persistence, got %+v", cfg)
	}
}

func TestResolveStoreConfigRequiresDatabaseURLForPersistentMode(t *testing.T) {
	_, err := resolveStoreConfig("postgres", "")
	if err == nil {
		t.Fatal("expected postgres mode without DATABASE_URL to fail")
	}
}

func TestResolveStoreConfigAllowsExplicitMemoryMode(t *testing.T) {
	cfg, err := resolveStoreConfig("memory", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "memory" || cfg.Persistent {
		t.Fatalf("expected explicit memory mode to be non-persistent, got %+v", cfg)
	}
}

func TestResolveJWTSecretRejectsWeakPersistentSecrets(t *testing.T) {
	for _, secret := range []string{"", "dev-secret-change-me", "local-dev-secret", "local-dev-secret-please-change", "short"} {
		_, err := resolveJWTSecret(secret, true)
		if err == nil {
			t.Fatalf("expected persistent JWT_SECRET %q to be rejected", secret)
		}
	}
}

func TestResolveJWTSecretGeneratesEphemeralSecretForMemoryMode(t *testing.T) {
	secret, err := resolveJWTSecret("", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 64 || strings.Trim(secret, "0123456789abcdef") != "" {
		t.Fatalf("expected generated 32-byte hex secret, got %q", secret)
	}

	secret, err = resolveJWTSecret("local-dev-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if secret == "local-dev-secret" || len(secret) != 64 {
		t.Fatalf("expected weak memory-mode secret to be replaced, got %q", secret)
	}
}

func TestBuildHTTPServerSetsReadTimeoutsWithoutWriteTimeout(t *testing.T) {
	server := buildHTTPServer(":8080", http.NotFoundHandler())
	if server.ReadTimeout != 60*time.Second {
		t.Fatalf("expected read timeout 60s, got %s", server.ReadTimeout)
	}
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("expected read header timeout 10s, got %s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != 120*time.Second {
		t.Fatalf("expected idle timeout 120s, got %s", server.IdleTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("expected write timeout disabled for SSE, got %s", server.WriteTimeout)
	}
}
