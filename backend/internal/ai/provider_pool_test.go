package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderCapabilityRegistryIncludesPriorityAndTasks(t *testing.T) {
	for _, provider := range []string{ProviderDeepSeek, ProviderQwen, ProviderERNIE, ProviderOpenAICompatible, ProviderMock} {
		info := ProviderInfo{Provider: provider, Model: provider}
		capability := capabilityForProvider(info, true)
		if capability.Provider != provider {
			t.Fatalf("expected provider %s, got %+v", provider, capability)
		}
		if capability.Priority <= 0 {
			t.Fatalf("expected priority for %s, got %+v", provider, capability)
		}
		if capability.MaxTokens <= 0 || capability.CostTier == "" {
			t.Fatalf("expected max tokens and cost tier for %s, got %+v", provider, capability)
		}
		if !capability.SupportsJSON {
			t.Fatalf("expected json support for %s, got %+v", provider, capability)
		}
		if provider != ProviderMock && !capability.TopK {
			t.Fatalf("expected top_k support for %s, got %+v", provider, capability)
		}
		if len(capability.SupportedTasks) == 0 {
			t.Fatalf("expected supported tasks for %s, got %+v", provider, capability)
		}
	}
}

func TestRouterProviderPoolSnapshotTracksHealthAndFallbackAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"not json"}}]}`))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		APIKey:   "test-key",
		Model:    "fake-model",
		Timeout:  time.Second,
	})
	_, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err == nil {
		t.Fatalf("expected strict failure for scenario_generate invalid json, got meta=%+v", meta)
	}
	if meta.FallbackUsed || meta.Provider == ProviderMock {
		t.Fatalf("scenario_generate invalid json must not fall back to mock, got %+v", meta)
	}

	info := router.Info()
	pool := info.ProviderPool
	if pool.ActiveProvider != ProviderOpenAICompatible {
		t.Fatalf("expected active provider to stay on openai-compatible failure, got %+v", pool)
	}
	if len(pool.FallbackOrder) < 2 || pool.FallbackOrder[0] != ProviderDeepSeek || pool.FallbackOrder[len(pool.FallbackOrder)-1] != ProviderMock {
		t.Fatalf("expected task fallback order, got %+v", pool.FallbackOrder)
	}
	if pool.DegradedCount == 0 {
		t.Fatalf("expected degraded provider count, got %+v", pool)
	}
	if len(pool.RecentAttempts) != 1 {
		t.Fatalf("expected only failing primary attempt under strict failure, got %+v", pool.RecentAttempts)
	}
	if pool.RecentAttempts[0].Provider != ProviderOpenAICompatible || pool.RecentAttempts[0].Success {
		t.Fatalf("expected failed primary attempt first, got %+v", pool.RecentAttempts)
	}
	if got := pool.ProviderByName(ProviderOpenAICompatible); got == nil || got.Health != "degraded" || got.LastErrorType != "validation" {
		t.Fatalf("expected degraded openai-compatible provider, got %+v", got)
	}
	if got := pool.ProviderByName(ProviderMock); got == nil || got.CallCount != 0 {
		t.Fatalf("expected mock provider to remain unused, got %+v", got)
	}
}

func TestRouterRateLimitReturnsExplainableErrorWithoutSuccessCount(t *testing.T) {
	router := NewRouter(Config{Provider: ProviderMock})
	router.rateLimiter = newProviderRateLimiter(0)

	_, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if meta.ErrorType != "rate_limit" {
		t.Fatalf("expected rate limit meta, got %+v err=%v", meta, err)
	}
	info := router.Info()
	if info.Telemetry.SuccessfulCalls != 0 {
		t.Fatalf("rate limited call should not count as success, got %+v", info.Telemetry)
	}
	if info.ProviderPool.ProviderByName(ProviderMock).RateLimit.Status != "limited" {
		t.Fatalf("expected limited provider pool status, got %+v", info.ProviderPool)
	}
}

func TestRouterExecutesConfiguredFallbackChainBeforeMock(t *testing.T) {
	deepSeekCalls := 0
	deepSeek := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deepSeekCalls++
		http.Error(w, "deepseek unavailable", http.StatusBadGateway)
	}))
	defer deepSeek.Close()
	compatibleCalls := 0
	compatible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compatibleCalls++
		_, _ = w.Write([]byte(openAICompatibleCommunityStructureResponse()))
	}))
	defer compatible.Close()

	router := NewRouter(Config{
		Provider: ProviderDeepSeek,
		BaseURL:  deepSeek.URL,
		APIKey:   "deepseek-key",
		Model:    "deepseek-test",
		Timeout:  time.Second,
		ProviderConfigs: map[string]Config{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible,
				BaseURL:  compatible.URL,
				APIKey:   "compatible-key",
				Model:    "compatible-test",
				Timeout:  time.Second,
			},
		},
	})
	content, meta, err := router.StructureCommunityPost(context.Background(), CommunityStructureRequest{
		Title:      "缓存命中率异常下降",
		RawContent: "发布后缓存 key 规则变化，数据库读请求升高。",
		Domain:     "database",
		Tags:       []string{"缓存", "变更"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(content.RootCause) == "" {
		t.Fatalf("expected compatible provider structured content, got %+v", content)
	}
	if deepSeekCalls != 1 || compatibleCalls != 1 {
		t.Fatalf("expected deepseek then compatible calls, deepseek=%d compatible=%d", deepSeekCalls, compatibleCalls)
	}
	if !meta.FallbackUsed || meta.Provider != ProviderOpenAICompatible || meta.ErrorType != "provider" {
		t.Fatalf("expected openai-compatible fallback meta, got %+v", meta)
	}
	attempts := router.Info().ProviderPool.RecentAttempts
	if len(attempts) < 2 || attempts[0].Provider != ProviderDeepSeek || attempts[0].Success || attempts[1].Provider != ProviderOpenAICompatible || !attempts[1].Success {
		t.Fatalf("expected deepseek failure then compatible success, got %+v", attempts)
	}
}

func TestRouterFallsBackWhenProviderRateLimited(t *testing.T) {
	router := NewRouter(Config{Provider: ProviderDeepSeek, APIKey: "deepseek-key"})
	router.rateLimiter = newProviderRateLimiter(1)
	router.rateLimiter.inFlight[ProviderDeepSeek] = 1

	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err != nil {
		t.Fatal(err)
	}
	if question.Title == "" || meta.Provider != ProviderMock || !meta.FallbackUsed || meta.ErrorType != "rate_limit" {
		t.Fatalf("expected mock fallback after rate limit, question=%+v meta=%+v", question, meta)
	}
	info := router.Info()
	if info.Telemetry.SuccessfulCalls != 1 || info.Telemetry.FallbackCalls != 1 {
		t.Fatalf("expected one successful fallback call, got %+v", info.Telemetry)
	}
	attempts := info.ProviderPool.RecentAttempts
	if len(attempts) < 2 || attempts[0].Provider != ProviderDeepSeek || attempts[0].ErrorType != "rate_limit" || attempts[1].Provider != ProviderMock || !attempts[1].Success {
		t.Fatalf("expected rate limited deepseek then mock success, got %+v", attempts)
	}
}

func TestRouterSafetyBlockedDoesNotCountAsSuccess(t *testing.T) {
	router := &Router{
		primary:       unsafeReplyProvider{},
		info:          ProviderInfo{Provider: "unsafe", Model: "unsafe-model"},
		streamEnabled: true,
		telemetry:     newRouterTelemetryStore(),
		health:        newProviderHealthStore(),
		rateLimiter:   newProviderRateLimiter(8),
	}
	reply, meta, err := router.RewriteScenarioReply(context.Background(), ScenarioReplyRequest{
		AllowedContent: "可以提示继续观察连接池指标。",
		UserMessage:    "数据库连接池耗尽导致排队",
		IsAnswerLeak:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply, "数据库连接池耗尽导致排队") || !meta.SafetyBlocked {
		t.Fatalf("expected blocked sanitized reply, reply=%q meta=%+v", reply, meta)
	}
	info := router.Info()
	if info.Telemetry.SuccessfulCalls != 0 || info.Telemetry.FailedCalls != 1 {
		t.Fatalf("safety blocked output should not count as success, got %+v", info.Telemetry)
	}
	decision := info.Telemetry.RecentDecisions[0]
	if decision.Status != "blocked" || decision.ErrorType != "safety_blocked" || !decision.Safety.Blocked {
		t.Fatalf("expected blocked telemetry decision, got %+v", decision)
	}
	if provider := info.ProviderPool.ProviderByName("unsafe"); provider != nil && (provider.Health == "degraded" || provider.LastErrorType != "") {
		t.Fatalf("safety blocked output should not degrade provider health, got %+v", provider)
	}
}

func TestProviderPoolSnapshotMarksUnconfiguredProvidersDisabled(t *testing.T) {
	router := NewRouter(Config{Provider: ProviderDeepSeek, APIKey: "deepseek-key"})
	pool := router.Info().ProviderPool

	if len(pool.FallbackOrder) < 5 {
		t.Fatalf("expected expanded fallback order, got %+v", pool.FallbackOrder)
	}
	if got := pool.ProviderByName(ProviderQwen); got == nil || got.Enabled {
		t.Fatalf("expected qwen to exist but be disabled, got %+v", got)
	}
	if got := pool.ProviderByName(ProviderERNIE); got == nil || got.Enabled {
		t.Fatalf("expected ernie to exist but be disabled, got %+v", got)
	}
	if got := pool.ProviderByName(ProviderOpenAICompatible); got == nil || got.Enabled {
		t.Fatalf("expected openai-compatible to exist but be disabled, got %+v", got)
	}
}

func openAICompatibleScenarioResponse(title string) string {
	content := openAICompatibleScenarioJSON(title)
	return `{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(content) + `}}]}`
}

func openAICompatibleCommunityStructureResponse() string {
	content := `{"root_cause":"缓存 key 规则变化导致命中率下降。","root_cause_keywords":["缓存","命中率"],"key_evidence":["命中率下降","数据库读请求升高"],"standard_procedure":["对比发布前后 key 规则","核对数据库读流量"],"architecture_diagram":"","architecture_diagram_spec":{"direction":"TD","nodes":[{"id":"App","label":"应用服务"},{"id":"Cache","label":"缓存层"},{"id":"DB","label":"数据库"}],"edges":[{"from":"App","to":"Cache"},{"from":"Cache","to":"DB","label":"回源"}]},"reference_links":["缓存预热"],"reveal_strategy":{"surface_clues":[{"clue_id":"c1","trigger_keywords":["缓存"],"content":"缓存命中率明显下降。","is_distractor":false}],"deep_clues":[{"clue_id":"c2","trigger_keywords":["回源"],"content":"数据库读流量与回源同时升高。","is_distractor":false}],"distractors":[]}}`
	return `{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(content) + `}}]}`
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
