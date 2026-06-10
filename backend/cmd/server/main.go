package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/httpapi"
	"situational-teaching/backend/internal/ratelimit"
	"situational-teaching/backend/internal/store"
)

func main() {
	port := getenv("PORT", "8080")
	databaseURL := os.Getenv("DATABASE_URL")
	redisURL := os.Getenv("REDIS_URL")
	storeMode := getenv("STORE_MODE", "")
	ctx := context.Background()
	storeConfig, err := resolveStoreConfig(storeMode, databaseURL)
	if err != nil {
		log.Fatalf("invalid store configuration: %v", err)
	}

	secret, err := resolveJWTSecret(os.Getenv("JWT_SECRET"), storeConfig.Persistent)
	if err != nil {
		log.Fatalf("invalid JWT configuration: %v", err)
	}

	authManager := auth.NewManager(secret, 24*time.Hour)
	var dataStore store.Store
	if storeConfig.Mode == "memory" {
		log.Printf("using in-memory store; generated scenarios and AI jobs are not persistent")
		dataStore = store.NewMemoryStore(auth.HashPassword)
	} else {
		log.Printf("using postgres store")
		postgresStore, err := store.NewPostgresStore(ctx, storeConfig.DatabaseURL, auth.HashPassword)
		if err != nil {
			log.Fatalf("failed to initialize postgres store: %v", err)
		}
		defer postgresStore.Close()
		dataStore = postgresStore
	}

	var limiter ratelimit.Limiter = ratelimit.NewNoopLimiter()
	if redisURL != "" {
		redisLimiter, err := ratelimit.NewRedisLimiter(ctx, redisURL)
		if err != nil {
			log.Printf("redis unavailable, rate limiting disabled: %v", err)
		} else {
			log.Printf("redis rate limiting enabled")
			defer redisLimiter.Close()
			limiter = redisLimiter
		}
	}

	llmRouter := ai.NewRouter(ai.ConfigFromEnv())
	llmInfo := llmRouter.Info()
	if llmInfo.Fallback {
		log.Printf("LLM provider: %s fallback=true configured_provider=%s configured_model=%s reason=%s", llmInfo.Provider, llmInfo.ConfiguredProvider, llmInfo.ConfiguredModel, llmInfo.InitError)
	} else {
		log.Printf("LLM provider: %s model=%s base_url=%s", llmInfo.Provider, llmInfo.Model, llmInfo.BaseURL)
	}

	server := httpapi.NewServer(dataStore, authManager, limiter, llmRouter)

	httpServer := buildHTTPServer(":"+port, server.Handler())

	log.Printf("MVP API listening on :%s", port)
	log.Fatal(httpServer.ListenAndServe())
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

var weakJWTSecrets = map[string]bool{
	"dev-secret-change-me":           true,
	"local-dev-secret":               true,
	"local-dev-secret-please-change": true,
}

// resolveJWTSecret enforces a strong signing secret in persistent (production)
// mode and falls back to an ephemeral random secret in memory/dev mode so the
// insecure default can never sign real tokens.
func resolveJWTSecret(secret string, persistent bool) (string, error) {
	secret = strings.TrimSpace(secret)
	if persistent {
		switch {
		case secret == "":
			return "", fmt.Errorf("JWT_SECRET is required when STORE_MODE=postgres; set it to a long random string")
		case weakJWTSecrets[secret]:
			return "", fmt.Errorf("JWT_SECRET must not use an insecure default value; set a long random string")
		case len(secret) < 16:
			return "", fmt.Errorf("JWT_SECRET must be at least 16 characters, got %d", len(secret))
		}
		return secret, nil
	}
	if secret == "" || weakJWTSecrets[secret] {
		generated, err := randomSecret(32)
		if err != nil {
			return "", err
		}
		log.Printf("JWT_SECRET not set or insecure; generated an ephemeral dev secret (sessions reset on restart). Set JWT_SECRET for stable tokens.")
		return generated, nil
	}
	return secret, nil
}

func randomSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func buildHTTPServer(addr string, handler http.Handler) *http.Server {
	// No WriteTimeout: several endpoints stream Server-Sent Events and a
	// write deadline would sever long-lived responses. ReadTimeout is still
	// capped so slow request bodies cannot hold connections indefinitely;
	// uploads are separately bounded to 20 MiB and JSON bodies to 4 MiB.
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

type storeConfig struct {
	Mode        string
	DatabaseURL string
	Persistent  bool
}

func resolveStoreConfig(storeMode, databaseURL string) (storeConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(storeMode))
	dsn := strings.TrimSpace(databaseURL)
	if mode == "" {
		mode = "postgres"
	}
	switch mode {
	case "memory":
		return storeConfig{Mode: "memory", DatabaseURL: dsn, Persistent: false}, nil
	case "postgres", "persistent":
		if dsn == "" {
			return storeConfig{}, fmt.Errorf("DATABASE_URL is required when STORE_MODE=%s; set STORE_MODE=memory only for temporary local data", mode)
		}
		return storeConfig{Mode: "postgres", DatabaseURL: dsn, Persistent: true}, nil
	default:
		return storeConfig{}, fmt.Errorf("STORE_MODE must be postgres or memory, got %q", storeMode)
	}
}
