package main

import (
	"context"
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
	secret := getenv("JWT_SECRET", "dev-secret-change-me")
	databaseURL := os.Getenv("DATABASE_URL")
	redisURL := os.Getenv("REDIS_URL")
	storeMode := getenv("STORE_MODE", "")
	ctx := context.Background()
	storeConfig, err := resolveStoreConfig(storeMode, databaseURL)
	if err != nil {
		log.Fatalf("invalid store configuration: %v", err)
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

	log.Printf("MVP API listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, server.Handler()))
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
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
