package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestConfigFromEnvDeepSeekDefault(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("JIANYI_API_KEY", "")
	t.Setenv("DEEPSEEK_KEY", "deepseek-test-key")

	cfg := ConfigFromEnv()
	if cfg.Provider != ProviderDeepSeek {
		t.Fatalf("expected deepseek, got %s", cfg.Provider)
	}
	if cfg.BaseURL != defaultDeepSeekBaseURL {
		t.Fatalf("expected deepseek base url, got %s", cfg.BaseURL)
	}
	if cfg.Model != defaultDeepSeekModel {
		t.Fatalf("expected deepseek default model, got %s", cfg.Model)
	}
	if cfg.APIKey != "deepseek-test-key" {
		t.Fatal("expected DEEPSEEK_KEY to be used")
	}
}

func TestRenderPromptSupportsGoTemplateAndJinja2(t *testing.T) {
	if !jinja2Available() {
		t.Skip("jinja2 is not available in current python environment")
	}

	goTemplateName := "unit-go-template"
	ClearPromptOverride(goTemplateName)
	if err := SetPromptOverride(goTemplateName, "go_template", "专业域：{{ .Domain }}"); err != nil {
		t.Fatal(err)
	}
	defer ClearPromptOverride(goTemplateName)

	goRendered, err := renderPrompt(goTemplateName, map[string]string{"Domain": "database"})
	if err != nil {
		t.Fatal(err)
	}
	if goRendered != "专业域：database" {
		t.Fatalf("unexpected go template render %q", goRendered)
	}

	jinjaTemplateName := "unit-jinja-template"
	ClearPromptOverride(jinjaTemplateName)
	if err := SetPromptOverride(jinjaTemplateName, "jinja2", "专业域：{{ Domain }}"); err != nil {
		t.Fatal(err)
	}
	defer ClearPromptOverride(jinjaTemplateName)

	jinjaRendered, err := renderPrompt(jinjaTemplateName, map[string]string{"Domain": "database"})
	if err != nil {
		t.Fatal(err)
	}
	if jinjaRendered != "专业域：database" {
		t.Fatalf("unexpected jinja render %q", jinjaRendered)
	}
}

func TestSetPromptOverrideRejectsInvalidJinja2Syntax(t *testing.T) {
	if !jinja2Available() {
		t.Skip("jinja2 is not available in current python environment")
	}
	if err := SetPromptOverride("invalid-jinja", "jinja2", "{{ Domain "); err == nil {
		t.Fatal("expected invalid jinja2 template to fail")
	}
}

func TestRenderPromptJinja2FailsOnMissingVariables(t *testing.T) {
	if !jinja2Available() {
		t.Skip("jinja2 is not available in current python environment")
	}
	name := "missing-jinja-vars"
	ClearPromptOverride(name)
	if err := SetPromptOverride(name, "jinja2", "专业域：{{ Domain }}"); err != nil {
		t.Fatal(err)
	}
	defer ClearPromptOverride(name)

	if _, err := renderPrompt(name, map[string]string{}); err == nil {
		t.Fatal("expected missing jinja2 variables to fail")
	}
}

func TestPromptCanSwitchBackToGoTemplate(t *testing.T) {
	name := "switch-back-template"
	ClearPromptOverride(name)
	defer ClearPromptOverride(name)

	if err := SetPromptOverride(name, "go_template", "专业域：{{ .Domain }}"); err != nil {
		t.Fatal(err)
	}
	rendered, err := renderPrompt(name, map[string]string{"Domain": "database"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "专业域：database" {
		t.Fatalf("unexpected go render %q", rendered)
	}
}

func TestConfigFromEnvJianyiCompatible(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("JIANYI_API_KEY", "jianyi-test-key")
	t.Setenv("DEEPSEEK_KEY", "")

	cfg := ConfigFromEnv()
	if cfg.Provider != ProviderOpenAICompatible {
		t.Fatalf("expected openai compatible, got %s", cfg.Provider)
	}
	if cfg.BaseURL != defaultJianyiBaseURL {
		t.Fatalf("expected jianyi base url, got %s", cfg.BaseURL)
	}
	if cfg.Model != defaultJianyiModel {
		t.Fatalf("expected jianyi default model, got %s", cfg.Model)
	}
	if cfg.APIKey != "jianyi-test-key" {
		t.Fatal("expected JIANYI_API_KEY to be used")
	}
}

func TestConfigFromEnvRegistersQwenAndERNIEProviders(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("JIANYI_API_KEY", "")
	t.Setenv("DEEPSEEK_KEY", "")
	t.Setenv("QWEN_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode")
	t.Setenv("QWEN_API_KEY", "qwen-test-key")
	t.Setenv("QWEN_MODEL", "qwen-max")
	t.Setenv("ERNIE_BASE_URL", "https://qianfan.baidubce.com/v2")
	t.Setenv("ERNIE_API_KEY", "ernie-test-key")
	t.Setenv("ERNIE_MODEL", "ernie-4.0-turbo-8k")
	t.Setenv("LLM_TOP_P", "0.85")
	t.Setenv("LLM_TOP_K", "32")
	t.Setenv("LLM_MAX_TOKENS", "4096")

	cfg := ConfigFromEnv()
	if cfg.Provider != ProviderQwen {
		t.Fatalf("expected qwen to become default provider when deepseek/jianyi missing, got %+v", cfg)
	}
	if cfg.TopP != 0.85 || cfg.TopK != 32 || cfg.MaxTokens != 4096 {
		t.Fatalf("expected sampling params from env, got %+v", cfg)
	}
	if len(cfg.ProviderConfigs) != 2 {
		t.Fatalf("expected qwen and ernie provider configs, got %+v", cfg.ProviderConfigs)
	}
	if qwen := cfg.ProviderConfigs[ProviderQwen]; qwen.Provider != ProviderQwen || qwen.APIKey != "qwen-test-key" || qwen.Model != "qwen-max" {
		t.Fatalf("unexpected qwen config %+v", qwen)
	}
	if ernie := cfg.ProviderConfigs[ProviderERNIE]; ernie.Provider != ProviderERNIE || ernie.APIKey != "ernie-test-key" || ernie.Model != "ernie-4.0-turbo-8k" {
		t.Fatalf("unexpected ernie config %+v", ernie)
	}
}

func TestConfigFromEnvDeepSeekWinsOverJianyi(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "https://jeniya.top")
	t.Setenv("LLM_API_KEY", "stale-compatible-key")
	t.Setenv("LLM_MODEL", "gpt-5.5")
	t.Setenv("JIANYI_API_KEY", "jianyi-test-key")
	t.Setenv("DEEPSEEK_KEY", "deepseek-test-key")

	cfg := ConfigFromEnv()
	if cfg.Provider != ProviderDeepSeek {
		t.Fatalf("expected deepseek to win, got %s", cfg.Provider)
	}
	if cfg.BaseURL != defaultDeepSeekBaseURL {
		t.Fatalf("expected deepseek base url, got %s", cfg.BaseURL)
	}
	if cfg.Model != defaultDeepSeekModel {
		t.Fatalf("expected deepseek model, got %s", cfg.Model)
	}
	if cfg.APIKey != "deepseek-test-key" {
		t.Fatal("expected DEEPSEEK_KEY to override stale compatible key")
	}
}

func TestGenerateScenarioUsesDeepSeekV4FlashEvenWhenGlobalModelIsDifferent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != defaultDeepSeekModel {
			t.Fatalf("expected scenario generation to use %s, got %s", defaultDeepSeekModel, req.Model)
		}
		_, _ = w.Write([]byte(openAICompatibleScenarioResponse("deepseek v4 flash scenario")))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider: ProviderDeepSeek,
		BaseURL:  server.URL,
		APIKey:   "deepseek-key",
		Model:    "deepseek-chat",
		Timeout:  time.Second,
	})

	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provider != ProviderDeepSeek {
		t.Fatalf("expected deepseek provider, got %+v", meta)
	}
	if meta.Model != defaultDeepSeekModel {
		t.Fatalf("expected meta model %s, got %+v", defaultDeepSeekModel, meta)
	}
	if question.Title != "deepseek v4 flash scenario" {
		t.Fatalf("unexpected question %+v", question)
	}
}

func TestGenerateScenarioDoesNotForceStreamWhenGlobalStreamEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != defaultDeepSeekModel {
			t.Fatalf("expected scenario generation to use %s, got %s", defaultDeepSeekModel, req.Model)
		}
		if req.Stream {
			t.Fatalf("scenario generation should use non-stream JSON request, got stream=true")
		}
		_, _ = w.Write([]byte(openAICompatibleScenarioResponse("non stream deepseek scenario")))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider:         ProviderDeepSeek,
		BaseURL:          server.URL,
		APIKey:           "deepseek-key",
		Model:            defaultDeepSeekModel,
		Timeout:          time.Second,
		StreamEnabled:    true,
		StreamConfigured: true,
	})

	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provider != ProviderDeepSeek || meta.FallbackUsed {
		t.Fatalf("unexpected meta %+v", meta)
	}
	if question.Title != "non stream deepseek scenario" {
		t.Fatalf("unexpected question %+v", question)
	}
}

func TestMissingKeyFallsBackToMock(t *testing.T) {
	t.Setenv("DEEPSEEK_KEY", "")
	router := NewRouter(Config{Provider: ProviderDeepSeek, Model: defaultDeepSeekModel, BaseURL: defaultDeepSeekBaseURL})
	info := router.Info()
	if info.Provider != ProviderMock || !info.Fallback {
		t.Fatalf("expected mock fallback, got %+v", info)
	}
	if info.ConfiguredProvider != ProviderDeepSeek {
		t.Fatalf("expected configured deepseek, got %+v", info)
	}
}

func TestValidateScenarioQuestionRejectsInvalidShape(t *testing.T) {
	question, err := NewMockProvider().GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err != nil {
		t.Fatal(err)
	}
	question.Difficulty = "L9"
	if err := ValidateScenarioQuestion(question); err == nil {
		t.Fatal("expected invalid difficulty to fail")
	}
}

func TestMockProviderUsesNonceForVariants(t *testing.T) {
	provider := NewMockProvider()
	titles := map[string]bool{}
	for i := 0; i < 12; i++ {
		question, err := provider.GenerateScenario(context.Background(), ScenarioGenerationRequest{
			Domain:       "database",
			Difficulty:   "L2",
			ScenarioType: "troubleshooting",
			Nonce:        string(rune('a' + i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		titles[question.Title] = true
	}
	if len(titles) < 2 {
		t.Fatalf("expected multiple mock variants, got %v", titles)
	}
}

func TestSafetyRewriteMasksSensitiveAndRootCause(t *testing.T) {
	text, rewritten := SafetyRewrite("数据库地址 10.0.0.1 password=abc", nil)
	if !rewritten || text == "数据库地址 10.0.0.1 password=abc" {
		t.Fatal("expected sensitive content to be rewritten")
	}
	if strings.Contains(text, "abc") {
		t.Fatalf("expected password value to be fully sanitized, got %q", text)
	}
	text, rewritten = SafetyRewrite("根因是连接池耗尽导致数据库慢", []string{"连接池耗尽导致数据库慢"})
	if !rewritten || text == "" {
		t.Fatal("expected root cause leak to be rewritten")
	}
}

func TestSanitizeMasksCredentialValues(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		blocked []string
	}{
		{name: "plain password", input: "password=123456", blocked: []string{"123456"}},
		{name: "partially sanitized password", input: "password=[已脱敏]123456", blocked: []string{"123456"}},
		{name: "api key", input: "api_key=sk-stage24-secret", blocked: []string{"sk-stage24-secret", "stage24"}},
		{name: "token colon", input: "token: real-token-value", blocked: []string{"real-token-value"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.input)
			for _, value := range tt.blocked {
				if strings.Contains(got, value) {
					t.Fatalf("expected %q to be removed from %q", value, got)
				}
			}
			if !strings.Contains(got, "已脱敏") {
				t.Fatalf("expected sanitized marker in %q", got)
			}
		})
	}
}

func TestSanitizeFieldsMasksChineseCredentialsAndOrganizations(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		blocked []string
	}{
		{name: "chinese password and email", input: "密码为12345asdfasd@123qq.com", blocked: []string{"12345asdfasd", "123qq.com"}},
		{name: "trailing partially redacted api key", input: "API KEY=[已脱敏];df'hww@@", blocked: []string{"df'hww@@"}},
		{name: "organization name", input: "马哥教育真实案例", blocked: []string{"马哥教育"}},
		{name: "mixed scenario text", input: "马哥教育真实案例 密码为12345asdfasd@123qq.com,API KEY=[已脱敏];df'hww@@", blocked: []string{"马哥教育", "12345asdfasd", "123qq.com", "df'hww@@"}},
		{name: "natural language password reset", input: "js++公司案例 有位员工A忘记了后台管理密码，找了上层B设置成了alsjkgdlaz124@qq_，然后上层B问他有没有AI KEY，他说有的，sl-saklsdhglasdghl;asgz", blocked: []string{"js++公司", "alsjkgdlaz124", "qq_", "sl-saklsdhglasdghl"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFields(tt.input)
			for _, value := range tt.blocked {
				if strings.Contains(got, value) {
					t.Fatalf("expected %q to be removed from %q", value, got)
				}
			}
			if !strings.Contains(got, "【") {
				t.Fatalf("expected field placeholder in %q", got)
			}
		})
	}
}

func TestOpenAICompatibleScenarioRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var req struct {
			Model       string   `json:"model"`
			Temperature *float64 `json:"temperature"`
			TopP        *float64 `json:"top_p"`
			TopK        *int     `json:"top_k"`
			MaxTokens   *int     `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "fake-model" {
			t.Fatalf("unexpected model %s", req.Model)
		}
		if req.Temperature == nil || *req.Temperature != 0.2 {
			t.Fatalf("expected temperature to be forwarded, got %+v", req)
		}
		if req.TopP == nil || *req.TopP != 0.75 {
			t.Fatalf("expected top_p to be forwarded, got %+v", req)
		}
		if req.TopK == nil || *req.TopK != 16 {
			t.Fatalf("expected top_k to be forwarded, got %+v", req)
		}
		if req.MaxTokens == nil || *req.MaxTokens != 2048 {
			t.Fatalf("expected max_tokens to be forwarded, got %+v", req)
		}
		_, _ = w.Write([]byte(openAICompatibleScenarioResponse("连接池耗尽导致查询变慢")))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider:    ProviderOpenAICompatible,
		BaseURL:     server.URL,
		APIKey:      "test-key",
		Model:       "fake-model",
		Timeout:     time.Second,
		Temperature: 0.2,
		TopP:        0.75,
		TopK:        16,
		MaxTokens:   2048,
	})
	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provider != ProviderOpenAICompatible || meta.FallbackUsed {
		t.Fatalf("unexpected meta %+v", meta)
	}
	if question.Title == "" || question.Content.RootCause == "" {
		t.Fatalf("unexpected question %+v", question)
	}
}

func TestNewOpenAICompatibleProviderPreservesProviderName(t *testing.T) {
	for _, provider := range []string{ProviderDeepSeek, ProviderQwen, ProviderERNIE, ProviderOpenAICompatible} {
		item := NewOpenAICompatibleProvider(Config{
			Provider:      provider,
			BaseURL:       "https://example.com",
			APIKey:        "test-key",
			Model:         "test-model",
			Timeout:       time.Second,
			Temperature:   0.2,
			TopP:          0.9,
			TopK:          8,
			MaxTokens:     1024,
			StreamEnabled: true,
		})
		if item.Info().Provider != provider {
			t.Fatalf("expected provider name %s, got %+v", provider, item.Info())
		}
	}
}

func jinja2Available() bool {
	return exec.Command("python", "-c", "import jinja2").Run() == nil
}

func TestOpenAICompatibleInterviewFeedbackStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !req.Stream {
			t.Fatal("expected stream request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"highlights\\\":[\\\"定位路径清晰\\\"],\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\\\"deficiencies\\\":[\\\"回滚验证不足\\\"],\\\"follow_up_question\\\":\\\"\\\",\\\"final_report\\\":\\\"整体达到要求\\\"}\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider:         ProviderOpenAICompatible,
		BaseURL:          server.URL,
		APIKey:           "test-key",
		Model:            "fake-model",
		Timeout:          time.Second,
		StreamEnabled:    true,
		StreamConfigured: true,
	})
	var streamed strings.Builder
	feedback, meta, err := router.GenerateInterviewFeedbackStream(context.Background(), InterviewFeedbackRequest{
		Answer: "先定位慢查询再验证索引。",
		Evaluation: domain.InterviewEvaluation{
			TotalScore:        88,
			FollowUpTriggered: false,
		},
		NeedReport: true,
	}, func(chunk string) {
		streamed.WriteString(chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provider != ProviderOpenAICompatible || meta.FallbackUsed {
		t.Fatalf("unexpected meta %+v", meta)
	}
	if feedback.FinalReport == "" || !strings.Contains(streamed.String(), "final_report") {
		t.Fatalf("unexpected streamed feedback=%+v raw=%s", feedback, streamed.String())
	}
}

func TestScenarioGenerationFailsOnInvalidJSON(t *testing.T) {
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
	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err == nil {
		t.Fatalf("expected invalid JSON to fail scenario generation, got question=%+v meta=%+v", question, meta)
	}
	if meta.Provider == ProviderMock || meta.FallbackUsed {
		t.Fatalf("scenario generation invalid JSON must not fall back to mock, got %+v", meta)
	}
	if meta.ErrorType != "validation" {
		t.Fatalf("expected validation error type, got %+v", meta)
	}
}

func TestScenarioGenerationDoesNotFallbackToMockAfterInvalidJSON(t *testing.T) {
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
	question, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
	})
	if err == nil {
		t.Fatalf("expected scenario generation invalid JSON error, got question=%+v meta=%+v", question, meta)
	}
	if meta.Provider == ProviderMock || meta.FallbackUsed {
		t.Fatalf("scenario generation invalid JSON must not fall back to mock, got %+v", meta)
	}
	if meta.ErrorType != "validation" {
		t.Fatalf("expected validation error type, got %+v", meta)
	}
}

func TestCommunityStructureStillFallsBackToMockAfterInvalidJSON(t *testing.T) {
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
	content, meta, err := router.StructureCommunityPost(context.Background(), CommunityStructureRequest{
		Title:      "缓存命中率异常下降",
		RawContent: "发布后缓存 key 规则变化，数据库读请求升高。",
		Domain:     "database",
		Tags:       []string{"缓存", "变更"},
	})
	if err != nil {
		t.Fatalf("community structure should keep fallback behavior, got err=%v meta=%+v", err, meta)
	}
	if meta.Provider != ProviderMock || !meta.FallbackUsed {
		t.Fatalf("expected mock fallback for community structure invalid JSON, got %+v", meta)
	}
	if strings.TrimSpace(content.RootCause) == "" {
		t.Fatalf("expected fallback content, got %+v", content)
	}
}

func TestScenarioGenerationDeepSeekUsesExtendedTimeout(t *testing.T) {
	provider := NewOpenAICompatibleProvider(Config{
		Provider: ProviderDeepSeek,
		BaseURL:  "https://example.test",
		APIKey:   "deepseek-key",
		Model:    "deepseek-chat",
		Timeout:  30 * time.Second,
	})

	wrapped, ok := providerWithTaskModel(provider, routerRequest(RouterTaskScenarioGenerate)).(*OpenAICompatibleProvider)
	if !ok {
		t.Fatalf("expected OpenAI compatible provider, got %T", wrapped)
	}
	if wrapped.model != defaultDeepSeekModel {
		t.Fatalf("expected scenario generation model %q, got %q", defaultDeepSeekModel, wrapped.model)
	}
	if wrapped.client.Timeout <= 30*time.Second {
		t.Fatalf("scenario generation DeepSeek timeout must exceed 30s, got %s", wrapped.client.Timeout)
	}
}

func TestRouterDoesNotFallbackToMockAfterScenarioGenerationTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte(openAICompatibleScenarioResponse("slow scenario")))
	}))
	defer server.Close()

	router := NewRouter(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  server.URL,
		APIKey:   "test-key",
		Model:    "fake-model",
		Timeout:  time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	question, meta, err := router.GenerateScenario(ctx, ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err == nil {
		t.Fatalf("expected scenario generation timeout error, got question=%+v meta=%+v", question, meta)
	}
	if meta.Provider == ProviderMock || meta.FallbackUsed {
		t.Fatalf("timeout must not be converted to mock fallback success, got %+v", meta)
	}
	if meta.ErrorType != "timeout" {
		t.Fatalf("expected timeout error type, got %+v", meta)
	}
}

func TestRouterInfoExposesDecisionTelemetry(t *testing.T) {
	router := NewRouter(Config{Provider: ProviderMock, StreamEnabled: true, StreamConfigured: true})
	_, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		UserID:       "u-router",
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.TraceID == "" || meta.Task != RouterTaskScenarioGenerate {
		t.Fatalf("expected trace metadata, got %+v", meta)
	}
	info := router.Info()
	if info.RouterVersion != "router-v1" {
		t.Fatalf("expected router version, got %+v", info)
	}
	if !info.StreamEnabled {
		t.Fatalf("expected stream enabled in info %+v", info)
	}
	if info.Capability.Provider != ProviderMock || !info.Capability.SupportsJSON {
		t.Fatalf("unexpected capability %+v", info.Capability)
	}
	if info.Telemetry.TotalCalls != 1 || info.Telemetry.SuccessfulCalls != 1 {
		t.Fatalf("unexpected telemetry %+v", info.Telemetry)
	}
	if info.LastTraceID != meta.TraceID || info.LastTask != RouterTaskScenarioGenerate {
		t.Fatalf("unexpected last trace info %+v meta=%+v", info, meta)
	}
	if len(info.Telemetry.RecentDecisions) != 1 {
		t.Fatalf("expected one recent decision, got %+v", info.Telemetry.RecentDecisions)
	}
	decision := info.Telemetry.RecentDecisions[0]
	if decision.Schema != SchemaScenarioQuestion || decision.Validation.Status != "passed" {
		t.Fatalf("unexpected decision validation %+v", decision)
	}
	if decision.Context.Version != "router-v1" {
		t.Fatalf("expected normalized context %+v", decision.Context)
	}
}

func TestRouterTelemetryRecordsFallbackDecision(t *testing.T) {
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
		t.Fatalf("expected strict failure meta, got %+v", meta)
	}
	if meta.FallbackUsed || meta.Provider == ProviderMock {
		t.Fatalf("scenario_generate invalid json must not report mock fallback meta, got %+v", meta)
	}
	info := router.Info()
	if info.Telemetry.FallbackCalls != 0 {
		t.Fatalf("expected no fallback telemetry for strict failure, got %+v", info.Telemetry)
	}
	if info.Telemetry.RecentDecisions[0].Provider != ProviderOpenAICompatible {
		t.Fatalf("expected failed primary provider decision, got %+v", info.Telemetry.RecentDecisions[0])
	}
	if info.Telemetry.RecentDecisions[0].Status != "failed" {
		t.Fatalf("strict failure should be recorded as failed, got %+v", info.Telemetry.RecentDecisions[0])
	}
	if info.Telemetry.RecentDecisions[0].ErrorType != "validation" {
		t.Fatalf("expected validation error classified, got %+v", info.Telemetry.RecentDecisions[0])
	}
}

func TestRouterStatusObservationDoesNotCountAsModelCall(t *testing.T) {
	router := NewRouter(Config{Provider: ProviderMock, StreamEnabled: true, StreamConfigured: true})

	info := router.Info()
	if info.LastTask != RouterTaskStatusCheck {
		t.Fatalf("expected status decision, got %+v", info)
	}
	if info.Telemetry.TotalCalls != 0 || info.Telemetry.SuccessfulCalls != 0 {
		t.Fatalf("status observation should not count as model call, got %+v", info.Telemetry)
	}
	if len(info.Telemetry.RecentDecisions) != 1 || info.Telemetry.RecentDecisions[0].Task != RouterTaskStatusCheck {
		t.Fatalf("expected visible status decision, got %+v", info.Telemetry.RecentDecisions)
	}

	_, meta, err := router.GenerateScenario(context.Background(), ScenarioGenerationRequest{Domain: "database", Difficulty: "L2", ScenarioType: "troubleshooting"})
	if err != nil {
		t.Fatal(err)
	}
	info = router.Info()
	if meta.Task != RouterTaskScenarioGenerate || info.Telemetry.TotalCalls != 1 {
		t.Fatalf("expected one real model call after generation, info=%+v meta=%+v", info.Telemetry, meta)
	}
	if info.Telemetry.TaskCalls[RouterTaskStatusCheck] != 0 {
		t.Fatalf("status check should not be included in task calls: %+v", info.Telemetry.TaskCalls)
	}
	if info.Telemetry.TaskCalls[RouterTaskScenarioGenerate] != 1 {
		t.Fatalf("expected scenario generation task call: %+v", info.Telemetry.TaskCalls)
	}
}

func TestMockProviderUsesScenarioConstraints(t *testing.T) {
	provider := NewMockProvider()
	question, err := provider.GenerateScenario(context.Background(), ScenarioGenerationRequest{
		Domain:       "database",
		Difficulty:   "L3",
		ScenarioType: "troubleshooting",
		Tags:         []string{"AI生成", "database"},
		UserID:       "user-demo",
		Constraints: ScenarioGenerationConstraints{
			Title:         "约束标题",
			Description:   "约束描述",
			TopicScope:    []string{"主从复制", "读流量"},
			RootCauseHint: "从库延迟",
			EvidenceHints: []string{"证据一"},
			ClueHints:     []string{"线索一"},
		},
	})
	if err != nil {
		t.Fatalf("generate scenario with constraints: %v", err)
	}
	if question.Title != "约束标题" {
		t.Fatalf("expected constrained title, got %q", question.Title)
	}
	if !strings.Contains(question.Description, "约束描述") || !strings.Contains(question.Description, "主从复制") {
		t.Fatalf("expected constrained description, got %q", question.Description)
	}
	if question.Content.RootCause == "" || !strings.Contains(question.Content.RootCause, "从库延迟") {
		t.Fatalf("expected constrained root cause, got %q", question.Content.RootCause)
	}
	if len(question.Content.KeyEvidence) == 0 || question.Content.KeyEvidence[0] != "证据一" {
		t.Fatalf("expected evidence hint to be prepended, got %+v", question.Content.KeyEvidence)
	}
	if len(question.Content.RevealStrategy.SurfaceClues) == 0 || question.Content.RevealStrategy.SurfaceClues[0].Content != "线索一" {
		t.Fatalf("expected clue hint to be added, got %+v", question.Content.RevealStrategy.SurfaceClues)
	}
}
