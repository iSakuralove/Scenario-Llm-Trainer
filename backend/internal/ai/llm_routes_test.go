package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLLMRoutesFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llm_routes.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadLLMRoutesInterpolatesEnvAndValidates(t *testing.T) {
	t.Setenv("TEST_GLM_KEY", "sk-glm-test")
	path := writeLLMRoutesFile(t, `
providers:
  - name: glm-official
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${TEST_GLM_KEY}
    model: glm-4.7
    extra_headers:
      X-Custom: yes
  - name: deepseek-official
    base_url: https://api.deepseek.com
    api_key: ${MISSING_KEY}
    model: deepseek-v4-flash
`)
	routes, err := LoadLLMRoutes(path)
	if err != nil {
		t.Fatalf("load routes: %v", err)
	}
	// key 未定义的 deepseek 条目被跳过，只保留 glm-official。
	if len(routes) != 1 {
		t.Fatalf("expected 1 usable route (empty-key skipped), got %d: %+v", len(routes), routes)
	}
	if routes[0].APIKey != "sk-glm-test" || routes[0].Provider != "glm-official" {
		t.Fatalf("first route mismatch: %+v", routes[0])
	}
	if routes[0].ExtraHeaders["X-Custom"] != "yes" {
		t.Fatalf("extra headers not interpolated: %+v", routes[0].ExtraHeaders)
	}
}

func TestLoadLLMRoutesFillsVendorDefaultsAndRejectsUnknown(t *testing.T) {
	path := writeLLMRoutesFile(t, `
providers:
  - name: glm-official
    api_key: sk-x
  - name: minimax-official
    api_key: sk-y
    model: MiniMax-M9
`)
	routes, err := LoadLLMRoutes(path)
	if err != nil {
		t.Fatalf("load routes: %v", err)
	}
	if routes[0].BaseURL != defaultRouteGLMBaseURL || routes[0].Model != defaultRouteGLMModel {
		t.Fatalf("glm defaults not filled: %+v", routes[0])
	}
	if routes[1].BaseURL != defaultRouteMiniMaxBaseURL || routes[1].Model != "MiniMax-M9" {
		t.Fatalf("minimax defaults/model mismatch: %+v", routes[1])
	}

	unknownPath := writeLLMRoutesFile(t, `
providers:
  - name: broken
    base_url: https://example.com/v1
    api_key: sk-x
`)
	if _, err := LoadLLMRoutes(unknownPath); err == nil {
		t.Fatal("expected validation error for unknown site without model")
	}
}

func TestRouterFallsThroughOrderedRoutes(t *testing.T) {
	var calls []string
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "first")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "second")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\\"ok\\":true}"}}]}`))
	}))
	defer second.Close()

	cfg := configFromLLMRoutes([]Config{
		{Provider: "route-first", BaseURL: first.URL, APIKey: "sk-1", Model: "m-1"},
		{Provider: "route-second", BaseURL: second.URL, APIKey: "sk-2", Model: "m-2"},
	})
	cfg.Timeout = 5 * time.Second
	router := NewRouter(cfg)
	req := SensitiveCheckRequest{Field: "unit-test", Text: "hello"}
	_, meta, err := router.CheckSensitiveContent(context.Background(), req)
	if err != nil {
		t.Fatalf("ordered fallback should succeed on second route: %v (meta=%+v)", err, meta)
	}
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("expected call order first->second, got %v", calls)
	}
	if !meta.FallbackUsed {
		t.Fatalf("meta should record fallback usage: %+v", meta)
	}
}

func TestConfigFromEnvUsesRoutesFile(t *testing.T) {
	t.Setenv("TEST_ROUTE_KEY", "sk-route")
	path := writeLLMRoutesFile(t, `
providers:
  - name: glm-official
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${TEST_ROUTE_KEY}
    model: glm-4.7
`)
	t.Setenv("LLM_ROUTES_FILE", path)
	cfg := ConfigFromEnv()
	if cfg.Provider != "glm-official" {
		t.Fatalf("primary provider should come from routes file, got %q", cfg.Provider)
	}
	if len(cfg.OrderedChain) != 1 || cfg.OrderedChain[0] != "glm-official" {
		t.Fatalf("ordered chain mismatch: %v", cfg.OrderedChain)
	}
	if _, ok := cfg.ProviderConfigs["glm-official"]; !ok {
		t.Fatalf("provider configs missing route entry: %+v", cfg.ProviderConfigs)
	}
}

func TestLoadLLMRoutesMissingFileReturnsNil(t *testing.T) {
	routes, err := LoadLLMRoutes(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("missing file should be a soft miss: %v", err)
	}
	if routes != nil {
		t.Fatalf("missing file should return nil routes, got %+v", routes)
	}
}
