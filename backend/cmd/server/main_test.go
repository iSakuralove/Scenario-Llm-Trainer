package main

import "testing"

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
