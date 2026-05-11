package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

const (
	ProviderMock             = "mock"
	ProviderOpenAICompatible = "openai_compatible"
	ProviderDeepSeek         = "deepseek"
	ProviderQwen             = "qwen"
	ProviderERNIE            = "ernie"

	defaultDeepSeekBaseURL  = "https://api.deepseek.com"
	defaultDeepSeekModel    = "deepseek-v4-flash"
	scenarioGenerateTimeout = 75 * time.Second
	defaultJianyiBaseURL    = "https://jeniya.top"
	defaultJianyiModel      = "gpt-5.5"
	defaultQwenBaseURL      = "https://dashscope.aliyuncs.com/compatible-mode"
	defaultQwenModel        = "qwen-plus"
	defaultERNIEBaseURL     = "https://qianfan.baidubce.com/v2"
	defaultERNIEModel       = "ernie-4.0-turbo-8k"
)

type Config struct {
	Provider         string
	BaseURL          string
	APIKey           string
	Model            string
	Timeout          time.Duration
	Temperature      float64
	TopP             float64
	TopK             int
	MaxTokens        int
	StreamEnabled    bool
	StreamConfigured bool
	ProviderConfigs  map[string]Config
}

type ProviderInfo struct {
	Provider           string               `json:"provider"`
	Model              string               `json:"model"`
	BaseURL            string               `json:"base_url,omitempty"`
	Fallback           bool                 `json:"fallback"`
	ConfiguredProvider string               `json:"configured_provider,omitempty"`
	ConfiguredModel    string               `json:"configured_model,omitempty"`
	InitError          string               `json:"init_error,omitempty"`
	StreamEnabled      bool                 `json:"stream_enabled"`
	RouterVersion      string               `json:"router_version"`
	Healthy            bool                 `json:"healthy"`
	Health             string               `json:"health"`
	Transport          string               `json:"transport"`
	LastTraceID        string               `json:"last_trace_id,omitempty"`
	LastTask           string               `json:"last_task,omitempty"`
	LastLatencyMS      int64                `json:"last_latency_ms,omitempty"`
	LastErrorType      string               `json:"last_error_type,omitempty"`
	LastError          string               `json:"last_error,omitempty"`
	LastErrorAt        string               `json:"last_error_at,omitempty"`
	LastFallbackReason string               `json:"last_fallback_reason,omitempty"`
	LastFallbackError  string               `json:"last_fallback_error,omitempty"`
	Capability         ProviderCapability   `json:"capability"`
	Telemetry          RouterTelemetry      `json:"telemetry"`
	ProviderPool       ProviderPoolSnapshot `json:"provider_pool"`
}

type CallMeta struct {
	Provider        string `json:"provider"`
	Model           string `json:"model,omitempty"`
	Validated       bool   `json:"validated"`
	FallbackUsed    bool   `json:"fallback_used"`
	SafetyRewritten bool   `json:"safety_rewritten"`
	SafetyBlocked   bool   `json:"safety_blocked"`
	TraceID         string `json:"trace_id,omitempty"`
	Task            string `json:"task,omitempty"`
	ErrorType       string `json:"error_type,omitempty"`
	RawOutput       string `json:"-"`
	LatencyMS       int64  `json:"latency_ms,omitempty"`
}

type ScenarioGenerationRequest struct {
	Domain       string
	Difficulty   string
	ScenarioType string
	Tags         []string
	Constraints  ScenarioGenerationConstraints
	UserID       string
	Nonce        string
}

type ScenarioGenerationConstraints struct {
	Title         string
	Description   string
	TopicScope    []string
	RootCauseHint string
	EvidenceHints []string
	ClueHints     []string
}

func (c ScenarioGenerationConstraints) ActiveFields() []string {
	fields := make([]string, 0, 6)
	if strings.TrimSpace(c.Title) != "" {
		fields = append(fields, "title")
	}
	if strings.TrimSpace(c.Description) != "" {
		fields = append(fields, "description")
	}
	if len(c.TopicScope) > 0 {
		fields = append(fields, "topic_scope")
	}
	if strings.TrimSpace(c.RootCauseHint) != "" {
		fields = append(fields, "root_cause_hint")
	}
	if len(c.EvidenceHints) > 0 {
		fields = append(fields, "evidence_hints")
	}
	if len(c.ClueHints) > 0 {
		fields = append(fields, "clue_hints")
	}
	return fields
}

type CommunityStructureRequest struct {
	Title      string
	RawContent string
	Domain     string
	Tags       []string
}

type ScenarioReplyRequest struct {
	QuestionTitle       string
	UserMessage         string
	ResponseType        string
	AllowedContent      string
	DiagnosticIntent    string
	CoachingAction      string
	DiagnosticFocus     string
	MissingEvidence     []string
	RepeatedWithTurn    int
	ToneStyle           string
	ForbiddenTerms      []string
	HintLevel           int
	IsDistractor        bool
	IsAnswerLeak        bool
	ConversationSummary string
	RecentMessages      []ScenarioContextMessage
}

type ScenarioContextMessage struct {
	TurnNumber       int
	UserContent      string
	AssistantContent string
}

type InterviewFeedbackRequest struct {
	Question   *domain.InterviewQuestion
	Answer     string
	Evaluation domain.InterviewEvaluation
	NeedReport bool
}

type SensitiveCheckRequest struct {
	Field string
	Text  string
}

type InterviewFeedback struct {
	Highlights       []string `json:"highlights"`
	Deficiencies     []string `json:"deficiencies"`
	FollowUpQuestion string   `json:"follow_up_question"`
	FinalReport      string   `json:"final_report"`
}

type Provider interface {
	Info() ProviderInfo
	GenerateScenario(ctx context.Context, req ScenarioGenerationRequest) (domain.ScenarioQuestion, error)
	StructureCommunityPost(ctx context.Context, req CommunityStructureRequest) (domain.ScenarioContent, error)
	StructureCommunityPostStream(ctx context.Context, req CommunityStructureRequest, onDelta func(string)) (domain.ScenarioContent, error)
	RewriteScenarioReply(ctx context.Context, req ScenarioReplyRequest) (string, error)
	RewriteScenarioReplyStream(ctx context.Context, req ScenarioReplyRequest, onDelta func(string)) (string, error)
	GenerateInterviewFeedback(ctx context.Context, req InterviewFeedbackRequest) (InterviewFeedback, error)
	GenerateInterviewFeedbackStream(ctx context.Context, req InterviewFeedbackRequest, onDelta func(string)) (InterviewFeedback, error)
	CheckSensitiveContent(ctx context.Context, req SensitiveCheckRequest) (domain.SensitiveCheckResult, error)
}

type Router struct {
	primary       Provider
	fallback      Provider
	providers     map[string]Provider
	info          ProviderInfo
	primaryInfo   ProviderInfo
	streamEnabled bool
	telemetry     *routerTelemetryStore
	health        *providerHealthStore
	rateLimiter   *providerRateLimiter
}

func ConfigFromEnv() Config {
	timeoutSeconds := parseInt(os.Getenv("LLM_TIMEOUT_SECONDS"), 30)
	temperature := parseFloat(os.Getenv("LLM_TEMPERATURE"), 0.2)
	topP := parseFloat(os.Getenv("LLM_TOP_P"), 0)
	topK := parseInt(os.Getenv("LLM_TOP_K"), 0)
	maxTokens := parseInt(os.Getenv("LLM_MAX_TOKENS"), 0)
	deepSeekKey := strings.TrimSpace(os.Getenv("DEEPSEEK_KEY"))
	jianyiKey := strings.TrimSpace(os.Getenv("JIANYI_API_KEY"))
	qwenKey := strings.TrimSpace(os.Getenv("QWEN_API_KEY"))
	ernieKey := strings.TrimSpace(os.Getenv("ERNIE_API_KEY"))
	cfg := Config{
		Provider:         "",
		BaseURL:          strings.TrimSpace(os.Getenv("LLM_BASE_URL")),
		APIKey:           strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		Model:            strings.TrimSpace(os.Getenv("LLM_MODEL")),
		Timeout:          time.Duration(timeoutSeconds) * time.Second,
		Temperature:      temperature,
		TopP:             topP,
		TopK:             topK,
		MaxTokens:        maxTokens,
		StreamEnabled:    parseBool(os.Getenv("LLM_STREAM_ENABLED"), true),
		StreamConfigured: true,
	}
	providerConfigs := map[string]Config{}
	if deepSeekKey != "" {
		cfg.Provider = ProviderDeepSeek
		cfg.APIKey = deepSeekKey
		cfg.Model = ""
		cfg.BaseURL = ""
		providerConfigs[ProviderDeepSeek] = Config{
			Provider:         ProviderDeepSeek,
			APIKey:           deepSeekKey,
			Timeout:          cfg.Timeout,
			Temperature:      cfg.Temperature,
			TopP:             cfg.TopP,
			TopK:             cfg.TopK,
			MaxTokens:        cfg.MaxTokens,
			StreamEnabled:    cfg.StreamEnabled,
			StreamConfigured: true,
		}
	} else if jianyiKey != "" {
		cfg.Provider = ProviderOpenAICompatible
		cfg.APIKey = jianyiKey
	}
	if jianyiKey != "" {
		providerConfigs[ProviderOpenAICompatible] = Config{
			Provider:         ProviderOpenAICompatible,
			APIKey:           jianyiKey,
			BaseURL:          strings.TrimSpace(os.Getenv("LLM_BASE_URL")),
			Model:            strings.TrimSpace(os.Getenv("LLM_MODEL")),
			Timeout:          cfg.Timeout,
			Temperature:      cfg.Temperature,
			TopP:             cfg.TopP,
			TopK:             cfg.TopK,
			MaxTokens:        cfg.MaxTokens,
			StreamEnabled:    cfg.StreamEnabled,
			StreamConfigured: true,
		}
	}
	if qwenKey != "" {
		providerConfigs[ProviderQwen] = Config{
			Provider:         ProviderQwen,
			APIKey:           qwenKey,
			BaseURL:          strings.TrimSpace(os.Getenv("QWEN_BASE_URL")),
			Model:            strings.TrimSpace(os.Getenv("QWEN_MODEL")),
			Timeout:          cfg.Timeout,
			Temperature:      cfg.Temperature,
			TopP:             cfg.TopP,
			TopK:             cfg.TopK,
			MaxTokens:        cfg.MaxTokens,
			StreamEnabled:    cfg.StreamEnabled,
			StreamConfigured: true,
		}
		if cfg.Provider == "" {
			cfg.Provider = ProviderQwen
			cfg.APIKey = qwenKey
			cfg.BaseURL = strings.TrimSpace(os.Getenv("QWEN_BASE_URL"))
			cfg.Model = strings.TrimSpace(os.Getenv("QWEN_MODEL"))
		}
	}
	if ernieKey != "" {
		providerConfigs[ProviderERNIE] = Config{
			Provider:         ProviderERNIE,
			APIKey:           ernieKey,
			BaseURL:          strings.TrimSpace(os.Getenv("ERNIE_BASE_URL")),
			Model:            strings.TrimSpace(os.Getenv("ERNIE_MODEL")),
			Timeout:          cfg.Timeout,
			Temperature:      cfg.Temperature,
			TopP:             cfg.TopP,
			TopK:             cfg.TopK,
			MaxTokens:        cfg.MaxTokens,
			StreamEnabled:    cfg.StreamEnabled,
			StreamConfigured: true,
		}
		if cfg.Provider == "" {
			cfg.Provider = ProviderERNIE
			cfg.APIKey = ernieKey
			cfg.BaseURL = strings.TrimSpace(os.Getenv("ERNIE_BASE_URL"))
			cfg.Model = strings.TrimSpace(os.Getenv("ERNIE_MODEL"))
		}
	}
	if cfg.APIKey != "" && cfg.BaseURL != "" && strings.TrimSpace(cfg.Provider) == ProviderOpenAICompatible {
		providerConfigs[ProviderOpenAICompatible] = Config{
			Provider:         ProviderOpenAICompatible,
			APIKey:           cfg.APIKey,
			BaseURL:          cfg.BaseURL,
			Model:            cfg.Model,
			Timeout:          cfg.Timeout,
			Temperature:      cfg.Temperature,
			TopP:             cfg.TopP,
			TopK:             cfg.TopK,
			MaxTokens:        cfg.MaxTokens,
			StreamEnabled:    cfg.StreamEnabled,
			StreamConfigured: true,
		}
	}
	if len(providerConfigs) > 0 {
		cfg.ProviderConfigs = providerConfigs
	}
	return NormalizeConfig(cfg)
}

func NormalizeConfig(cfg Config) Config {
	nested := cfg.ProviderConfigs
	cfg.ProviderConfigs = nil
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Temperature <= 0 {
		cfg.Temperature = 0.2
	}
	if cfg.TopP < 0 {
		cfg.TopP = 0
	}
	if cfg.TopK < 0 {
		cfg.TopK = 0
	}
	if cfg.MaxTokens < 0 {
		cfg.MaxTokens = 0
	}
	if !cfg.StreamConfigured {
		cfg.StreamEnabled = true
	}
	if cfg.Provider == "" {
		cfg.Provider = autoProvider(cfg)
	}

	switch cfg.Provider {
	case ProviderDeepSeek:
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultDeepSeekBaseURL
		}
		if cfg.Model == "" {
			cfg.Model = defaultDeepSeekModel
		}
	case ProviderQwen:
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultQwenBaseURL
		}
		if cfg.Model == "" {
			cfg.Model = defaultQwenModel
		}
	case ProviderERNIE:
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultERNIEBaseURL
		}
		if cfg.Model == "" {
			cfg.Model = defaultERNIEModel
		}
	case ProviderOpenAICompatible:
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultJianyiBaseURL
		}
		if cfg.Model == "" {
			cfg.Model = defaultJianyiModel
		}
	case ProviderMock:
		cfg.BaseURL = ""
		cfg.APIKey = ""
		if cfg.Model == "" {
			cfg.Model = ProviderMock
		}
	default:
		cfg.Provider = ProviderMock
		cfg.BaseURL = ""
		cfg.APIKey = ""
		cfg.Model = ProviderMock
	}
	if len(nested) > 0 {
		cfg.ProviderConfigs = make(map[string]Config, len(nested))
		for provider, nestedCfg := range nested {
			nestedCfg.ProviderConfigs = nil
			if strings.TrimSpace(nestedCfg.Provider) == "" {
				nestedCfg.Provider = provider
			}
			cfg.ProviderConfigs[strings.ToLower(strings.TrimSpace(nestedCfg.Provider))] = NormalizeConfig(nestedCfg)
		}
	}
	return cfg
}

func autoProvider(cfg Config) string {
	switch {
	case strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.BaseURL) != "":
		return ProviderOpenAICompatible
	default:
		return ProviderMock
	}
}

func NewRouter(cfg Config) *Router {
	cfg = NormalizeConfig(cfg)
	fallback := NewMockProvider()
	router := &Router{fallback: fallback, providers: map[string]Provider{ProviderMock: fallback}, streamEnabled: cfg.StreamEnabled, telemetry: newRouterTelemetryStore(), health: newProviderHealthStore(), rateLimiter: newProviderRateLimiter(8)}
	router.primaryInfo = ProviderInfo{
		Provider: cfg.Provider,
		Model:    cfg.Model,
		BaseURL:  cfg.BaseURL,
	}
	for providerName, providerCfg := range cfg.ProviderConfigs {
		provider := providerFromConfig(providerCfg)
		if provider != nil {
			router.providers[providerName] = provider
		}
	}
	if cfg.Provider == ProviderMock {
		router.primary = fallback
		router.info = fallback.Info()
		return router
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		router.primary = fallback
		router.info = fallback.Info()
		router.info.Fallback = true
		router.info.ConfiguredProvider = cfg.Provider
		router.info.ConfiguredModel = cfg.Model
		router.info.InitError = "missing api key"
		return router
	}
	router.primary = providerFromConfig(cfg)
	if router.primary == nil {
		router.primary = fallback
	}
	router.providers[cfg.Provider] = router.primary
	router.info = router.primary.Info()
	return router
}

func providerFromConfig(cfg Config) Provider {
	cfg = NormalizeConfig(cfg)
	if cfg.Provider == ProviderMock {
		return NewMockProvider()
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil
	}
	return NewOpenAICompatibleProvider(cfg)
}

func (r *Router) Info() ProviderInfo {
	if r == nil {
		info := NewMockProvider().Info()
		return enrichProviderInfo(info, true, nil)
	}
	r.ensureStatusObservation()
	info := enrichProviderInfo(r.info, r.streamEnabled, r.telemetry)
	info.ProviderPool = r.providerPoolSnapshot(info)
	return info
}

func (r *Router) ObserveStatus() ProviderInfo {
	if r == nil {
		return NewRouter(Config{Provider: ProviderMock}).ObserveStatus()
	}
	r.ensureStatusObservation()
	info := enrichProviderInfo(r.info, r.streamEnabled, r.telemetry)
	info.ProviderPool = r.providerPoolSnapshot(info)
	return info
}

func (r *Router) ensureStatusObservation() {
	if r == nil || r.telemetry == nil {
		return
	}
	if r.telemetry.snapshot().TotalCalls > 0 {
		return
	}
	req := routerRequest(
		RouterTaskStatusCheck,
		withOutputMode(OutputModeStatus),
		withPrompt("router_status"),
	)
	info := r.info
	if strings.TrimSpace(info.Provider) == "" {
		info = NewMockProvider().Info()
	}
	meta := CallMeta{Provider: info.Provider, Validated: true, TraceID: nextRouterTraceID(req.Task), Task: req.Task}
	now := time.Now()
	decision := routerDecision(req, info, meta, now, now, nil)
	decision.Meta = map[string]string{"source": "system_status"}
	r.recordDecision(decision, meta, nil)
}

func (r *Router) GenerateScenario(ctx context.Context, req ScenarioGenerationRequest) (domain.ScenarioQuestion, CallMeta, error) {
	var zero domain.ScenarioQuestion
	routerReq := routerRequest(
		RouterTaskScenarioGenerate,
		withDomain(req.Domain),
		withUserID(req.UserID),
		withSchema(SchemaScenarioQuestion),
		withPrompt("scenario_generate"),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.GenerateScenario(ctx, req)
	}, processWithDomainValidator(routerReq, func(value interface{}) error {
		return ValidateScenarioQuestion(value.(domain.ScenarioQuestion))
	}))
	if err != nil {
		return zero, meta, err
	}
	return value.(domain.ScenarioQuestion), meta, nil
}

func (r *Router) StructureCommunityPost(ctx context.Context, req CommunityStructureRequest) (domain.ScenarioContent, CallMeta, error) {
	var zero domain.ScenarioContent
	routerReq := routerRequest(
		RouterTaskCommunityStructure,
		withDomain(req.Domain),
		withSchema(SchemaScenarioContentPreview),
		withPrompt("community_structure"),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.StructureCommunityPost(ctx, req)
	}, processCommunityStructure(routerReq, req))
	if err != nil {
		return zero, meta, err
	}
	return value.(domain.ScenarioContent), meta, nil
}

func (r *Router) StructureCommunityPostStream(ctx context.Context, req CommunityStructureRequest, onDelta func(string)) (domain.ScenarioContent, CallMeta, error) {
	if r == nil || !r.streamEnabled {
		return r.StructureCommunityPost(ctx, req)
	}
	var zero domain.ScenarioContent
	routerReq := routerRequest(
		RouterTaskCommunityStructure,
		withDomain(req.Domain),
		withSchema(SchemaScenarioContentPreview),
		withPrompt("community_structure"),
		withStream(true),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.StructureCommunityPostStream(ctx, req, onDelta)
	}, processCommunityStructure(routerReq, req))
	if err != nil {
		return zero, meta, err
	}
	return value.(domain.ScenarioContent), meta, nil
}

func (r *Router) RewriteScenarioReply(ctx context.Context, req ScenarioReplyRequest) (string, CallMeta, error) {
	preparedReq, window := prepareScenarioReplyRequest(req)
	routerReq := routerRequest(
		RouterTaskScenarioReply,
		withContextWindow(window),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.RewriteScenarioReply(ctx, preparedReq)
	}, processScenarioReply(routerReq, preparedReq))
	if err != nil {
		return "", meta, err
	}
	return value.(string), meta, nil
}

func (r *Router) RewriteScenarioReplyStream(ctx context.Context, req ScenarioReplyRequest, onDelta func(string)) (string, CallMeta, error) {
	if r == nil || !r.streamEnabled {
		return r.RewriteScenarioReply(ctx, req)
	}
	preparedReq, window := prepareScenarioReplyRequest(req)
	routerReq := routerRequest(
		RouterTaskScenarioReply,
		withStream(true),
		withContextWindow(window),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.RewriteScenarioReplyStream(ctx, preparedReq, nil)
	}, processScenarioReply(routerReq, preparedReq))
	if err != nil {
		return "", meta, err
	}
	reply := value.(string)
	emitMockDelta(onDelta, reply)
	return reply, meta, nil
}

func (r *Router) GenerateInterviewFeedback(ctx context.Context, req InterviewFeedbackRequest) (InterviewFeedback, CallMeta, error) {
	var zero InterviewFeedback
	routerReq := routerRequest(
		RouterTaskInterviewFeedback,
		withSchema(SchemaInterviewFeedback),
		withPrompt("interview_feedback"),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.GenerateInterviewFeedback(ctx, req)
	}, processWithDomainValidator(routerReq, func(value interface{}) error {
		return ValidateInterviewFeedback(value.(InterviewFeedback), req.Evaluation.FollowUpTriggered, req.NeedReport)
	}))
	if err != nil {
		return zero, meta, err
	}
	return value.(InterviewFeedback), meta, nil
}

func (r *Router) GenerateInterviewFeedbackStream(ctx context.Context, req InterviewFeedbackRequest, onDelta func(string)) (InterviewFeedback, CallMeta, error) {
	if r == nil || !r.streamEnabled {
		return r.GenerateInterviewFeedback(ctx, req)
	}
	var zero InterviewFeedback
	routerReq := routerRequest(
		RouterTaskInterviewFeedback,
		withSchema(SchemaInterviewFeedback),
		withPrompt("interview_feedback"),
		withStream(true),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.GenerateInterviewFeedbackStream(ctx, req, onDelta)
	}, processWithDomainValidator(routerReq, func(value interface{}) error {
		return ValidateInterviewFeedback(value.(InterviewFeedback), req.Evaluation.FollowUpTriggered, req.NeedReport)
	}))
	if err != nil {
		return zero, meta, err
	}
	return value.(InterviewFeedback), meta, nil
}

func (r *Router) CheckSensitiveContent(ctx context.Context, req SensitiveCheckRequest) (domain.SensitiveCheckResult, CallMeta, error) {
	var zero domain.SensitiveCheckResult
	routerReq := routerRequest(
		RouterTaskSensitiveCheck,
		withSchema(SchemaSensitiveCheck),
		withPrompt("sensitive_check"),
		withSafetyPolicy(SafetyPolicySensitiveDetection),
	)
	value, meta, err := r.call(ctx, routerReq, func(provider Provider) (interface{}, error) {
		return provider.CheckSensitiveContent(ctx, req)
	}, processWithDomainValidator(routerReq, func(value interface{}) error {
		return ValidateSensitiveCheck(value.(domain.SensitiveCheckResult))
	}))
	if err != nil {
		return zero, meta, err
	}
	return value.(domain.SensitiveCheckResult), meta, nil
}

type outputProcessFunc func(interface{}) (interface{}, OutputProcessResult, error)

func taskModelOverride(req RouterRequest, provider ProviderInfo) string {
	if req.Task == RouterTaskScenarioGenerate && provider.Provider == ProviderDeepSeek {
		return defaultDeepSeekModel
	}
	return strings.TrimSpace(provider.Model)
}

func taskTimeoutOverride(req RouterRequest, provider ProviderInfo, current time.Duration) time.Duration {
	if req.Task == RouterTaskScenarioGenerate && provider.Provider == ProviderDeepSeek && current < scenarioGenerateTimeout {
		return scenarioGenerateTimeout
	}
	return current
}

func isTerminalScenarioGenerateError(err error) bool {
	if err == nil {
		return false
	}
	switch classifyRouterError(err) {
	case "timeout", "validation", "provider", "auth", "rate_limit", "unknown":
		return true
	default:
		return errors.Is(err, context.DeadlineExceeded)
	}
}

func isTerminalTaskError(req RouterRequest, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if req.StrictFailure && req.Task == RouterTaskScenarioGenerate {
		return isTerminalScenarioGenerateError(err)
	}
	return false
}

func providerWithTaskModel(provider Provider, req RouterRequest) Provider {
	if provider == nil {
		return nil
	}
	info := provider.Info()
	overrideModel := taskModelOverride(req, info)
	switch typed := provider.(type) {
	case *OpenAICompatibleProvider:
		overrideTimeout := taskTimeoutOverride(req, info, typed.client.Timeout)
		if (overrideModel == "" || overrideModel == strings.TrimSpace(info.Model)) && overrideTimeout == typed.client.Timeout {
			return provider
		}
		return NewOpenAICompatibleProvider(Config{
			Provider:         typed.name,
			BaseURL:          typed.baseURL,
			APIKey:           typed.apiKey,
			Model:            defaultString(overrideModel, typed.model),
			Timeout:          overrideTimeout,
			Temperature:      typed.temperature,
			TopP:             typed.topP,
			TopK:             typed.topK,
			MaxTokens:        typed.maxTokens,
			StreamEnabled:    typed.stream,
			StreamConfigured: true,
		})
	default:
		return provider
	}
}

func (r *Router) call(ctx context.Context, req RouterRequest, fn func(Provider) (interface{}, error), processors ...outputProcessFunc) (interface{}, CallMeta, error) {
	started := time.Now()
	_ = ctx
	if r == nil || r.primary == nil {
		provider := NewMockProvider()
		traceID := nextRouterTraceID(req.Task)
		value, err := fn(provider)
		value, processResult, processErr := processRouterOutput(req, value, processors...)
		if err == nil {
			err = processErr
		}
		meta := CallMeta{Provider: ProviderMock, Validated: err == nil, TraceID: traceID, Task: req.Task, SafetyRewritten: processResult.Safety.RewriteUsed, SafetyBlocked: processResult.Safety.Blocked}
		decision := routerDecision(req, provider.Info(), meta, started, time.Now(), err)
		applyProcessResultToDecision(&decision, processResult)
		if err != nil {
			meta.ErrorType = classifyRouterError(err)
		}
		meta.LatencyMS = decision.LatencyMS
		return value, meta, err
	}
	traceID := nextRouterTraceID(req.Task)
	attempts := []FallbackAttempt{}
	providers := r.executableProviders(req)
	var firstErr error
	for index, provider := range providers {
		provider = providerWithTaskModel(provider, req)
		info := provider.Info()
		attemptStarted := time.Now()
		release, rateLimit, rateErr := r.rateLimiter.begin(info.Provider)
		if rateErr != nil {
			attempt := fallbackAttempt(info, attemptStarted, time.Now(), rateErr, firstErr)
			attempts = append(attempts, attempt)
			r.recordProviderHealth(info.Provider, rateErr)
			if firstErr == nil {
				firstErr = rateErr
			}
			if index < len(providers)-1 {
				continue
			}
			meta := CallMeta{Provider: info.Provider, Model: info.Model, FallbackUsed: len(attempts) > 1, TraceID: traceID, Task: req.Task, ErrorType: classifyRouterError(firstErr)}
			decision := routerDecision(req, info, meta, started, time.Now(), firstErr)
			attachAttemptsToDecision(&decision, attempts)
			decision.ProviderHealth = r.providerHealth(info.Provider)
			decision.RateLimit = rateLimit
			r.recordDecision(decision, meta, firstErr)
			meta.LatencyMS = decision.LatencyMS
			return nil, meta, firstErr
		}
		value, err := fn(provider)
		release()
		if err == nil {
			var processResult OutputProcessResult
			value, processResult, err = processRouterOutput(req, value, processors...)
			if processResult.Safety.Blocked {
				safetyErr := fmt.Errorf("safety blocked output")
				attempt := fallbackAttempt(info, attemptStarted, time.Now(), safetyErr, firstErr)
				attempts = append(attempts, attempt)
				meta := CallMeta{Provider: info.Provider, Model: info.Model, Validated: false, FallbackUsed: len(attempts) > 1, TraceID: traceID, Task: req.Task, SafetyRewritten: processResult.Safety.RewriteUsed, SafetyBlocked: processResult.Safety.Blocked, ErrorType: "safety_blocked"}
				if firstErr != nil {
					meta.ErrorType = classifyRouterError(firstErr)
				}
				decision := routerDecision(req, info, meta, started, time.Now(), safetyErr)
				applyProcessResultToDecision(&decision, processResult)
				decision.Status = "blocked"
				decision.ErrorType = "safety_blocked"
				decision.ErrorMessage = ""
				attachAttemptsToDecision(&decision, attempts)
				decision.ProviderHealth = r.providerHealth(info.Provider)
				decision.RateLimit = r.rateLimitSnapshot(info.Provider)
				r.recordDecision(decision, meta, safetyErr)
				meta.LatencyMS = decision.LatencyMS
				return value, meta, err
			}
			if err == nil {
				attempt := fallbackAttempt(info, attemptStarted, time.Now(), nil, firstErr)
				attempts = append(attempts, attempt)
				r.recordProviderHealth(info.Provider, nil)
				meta := CallMeta{Provider: info.Provider, Model: info.Model, Validated: true, FallbackUsed: len(attempts) > 1, TraceID: traceID, Task: req.Task, SafetyRewritten: processResult.Safety.RewriteUsed, SafetyBlocked: processResult.Safety.Blocked}
				if firstErr != nil {
					meta.ErrorType = classifyRouterError(firstErr)
				}
				decision := routerDecision(req, info, meta, started, time.Now(), nil)
				applyProcessResultToDecision(&decision, processResult)
				attachAttemptsToDecision(&decision, attempts)
				decision.ProviderHealth = r.providerHealth(info.Provider)
				decision.RateLimit = r.rateLimitSnapshot(info.Provider)
				if meta.ErrorType != "" {
					decision.Meta = map[string]string{
						"fallback_reason": meta.ErrorType,
						"fallback_error":  sanitizeErrorMessage(firstErr.Error()),
					}
				}
				r.recordDecision(decision, meta, nil)
				meta.LatencyMS = decision.LatencyMS
				return value, meta, nil
			}
		}
		if isTerminalTaskError(req, err) {
			attempt := fallbackAttempt(info, attemptStarted, time.Now(), err, firstErr)
			attempts = append(attempts, attempt)
			r.recordProviderHealth(info.Provider, err)
			meta := CallMeta{Provider: info.Provider, Model: info.Model, TraceID: traceID, Task: req.Task, ErrorType: classifyRouterError(err), RawOutput: RawOutputFromError(err)}
			decision := routerDecision(req, info, meta, started, time.Now(), err)
			attachAttemptsToDecision(&decision, attempts)
			decision.ProviderHealth = r.providerHealth(info.Provider)
			decision.RateLimit = r.rateLimitSnapshot(info.Provider)
			r.recordDecision(decision, meta, err)
			meta.LatencyMS = decision.LatencyMS
			return nil, meta, err
		}
		attempt := fallbackAttempt(info, attemptStarted, time.Now(), err, firstErr)
		attempts = append(attempts, attempt)
		r.recordProviderHealth(info.Provider, err)
		if firstErr == nil {
			firstErr = err
		}
	}
	lastInfo := providers[len(providers)-1].Info()
	callErr := fmt.Errorf("llm provider failed")
	meta := CallMeta{Provider: lastInfo.Provider, Model: lastInfo.Model, FallbackUsed: len(attempts) > 1, TraceID: traceID, Task: req.Task, ErrorType: classifyRouterError(firstErr)}
	decision := routerDecision(req, lastInfo, meta, started, time.Now(), firstErr)
	attachAttemptsToDecision(&decision, attempts)
	decision.ProviderHealth = r.providerHealth(lastInfo.Provider)
	decision.RateLimit = r.rateLimitSnapshot(lastInfo.Provider)
	r.recordDecision(decision, meta, callErr)
	meta.LatencyMS = decision.LatencyMS
	return nil, meta, callErr
}

func (r *Router) executableProviders(req RouterRequest) []Provider {
	if r == nil || r.primary == nil {
		return []Provider{NewMockProvider()}
	}
	seen := map[string]bool{}
	providers := []Provider{}
	for _, providerName := range req.FallbackChain {
		providerName = strings.ToLower(strings.TrimSpace(providerName))
		if providerName == "" || seen[providerName] {
			continue
		}
		provider := r.providerByName(providerName)
		if provider == nil {
			continue
		}
		providers = append(providers, provider)
		seen[providerName] = true
	}
	primaryName := strings.TrimSpace(r.primary.Info().Provider)
	if primaryName != "" && !seen[primaryName] {
		providers = append([]Provider{r.primary}, providers...)
		seen[primaryName] = true
	}
	if len(providers) == 0 {
		providers = append(providers, r.primary)
		seen[primaryName] = true
	}
	primaryInfo := r.primary.Info()
	if r.fallback != nil && primaryInfo.Provider != ProviderMock && !seen[ProviderMock] {
		providers = append(providers, r.fallback)
	}
	return providers
}

func (r *Router) PlannedProviderInfo(task string) ProviderInfo {
	req := routerRequest(task)
	providers := r.executableProviders(req)
	if len(providers) == 0 {
		return NewMockProvider().Info()
	}
	provider := providerWithTaskModel(providers[0], req)
	if provider == nil {
		return NewMockProvider().Info()
	}
	return provider.Info()
}

func (r *Router) providerByName(providerName string) Provider {
	if r == nil {
		return nil
	}
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if providerName == "" {
		return nil
	}
	if r.providers != nil {
		if provider := r.providers[providerName]; provider != nil {
			return provider
		}
	}
	if r.primary != nil && r.primary.Info().Provider == providerName {
		return r.primary
	}
	if providerName == ProviderMock && r.fallback != nil {
		return r.fallback
	}
	return nil
}

func attachAttemptsToDecision(decision *RouterDecision, attempts []FallbackAttempt) {
	if decision == nil {
		return
	}
	decision.FallbackAttempts = attempts
	if len(attempts) == 0 {
		return
	}
	chain := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.Provider != "" {
			chain = append(chain, attempt.Provider)
		}
	}
	if len(chain) > 0 {
		decision.FallbackChain = chain
	}
}

func fallbackAttempt(info ProviderInfo, started time.Time, completed time.Time, err error, fallbackReason error) FallbackAttempt {
	attempt := FallbackAttempt{
		Provider:    info.Provider,
		Model:       info.Model,
		Success:     err == nil,
		StartedAt:   started,
		CompletedAt: completed,
		LatencyMS:   completed.Sub(started).Milliseconds(),
	}
	if err != nil {
		attempt.ErrorType = classifyRouterError(err)
		attempt.ErrorMessage = sanitizeErrorMessage(err.Error())
	}
	if fallbackReason != nil {
		attempt.FallbackReason = classifyRouterError(fallbackReason)
	}
	return attempt
}

func processRouterOutput(req RouterRequest, value interface{}, processors ...outputProcessFunc) (interface{}, OutputProcessResult, error) {
	if len(processors) == 0 || processors[0] == nil {
		result, err := NewOutputProcessor().Process(OutputProcessRequest{
			Task:         req.Task,
			Schema:       req.Schema,
			OutputMode:   req.OutputMode,
			DomainValue:  value,
			Stream:       req.Stream,
			SafetyPolicy: req.SafetyPolicy,
		})
		return value, result, err
	}
	return processors[0](value)
}

func processWithDomainValidator(req RouterRequest, validate func(interface{}) error) outputProcessFunc {
	return func(value interface{}) (interface{}, OutputProcessResult, error) {
		result, err := NewOutputProcessor().Process(OutputProcessRequest{
			Task:         req.Task,
			Schema:       req.Schema,
			OutputMode:   req.OutputMode,
			DomainValue:  value,
			Stream:       req.Stream,
			SafetyPolicy: req.SafetyPolicy,
			Validate:     validate,
		})
		return value, result, err
	}
}

func processCommunityStructure(req RouterRequest, structureReq CommunityStructureRequest) outputProcessFunc {
	return func(value interface{}) (interface{}, OutputProcessResult, error) {
		original := value.(domain.ScenarioContent)
		safety := NewSafetyFilter().Apply(SafetyFilterRequest{
			Task:   req.Task,
			Text:   strings.Join(scenarioContentSafetyText(original), "\n"),
			Policy: req.SafetyPolicy,
		})
		content := SanitizeScenarioContentFields(original)
		content = PrepareScenarioContent(content, domain.ScenarioQuestion{
			Title:   structureReq.Title,
			Domain:  structureReq.Domain,
			Content: content,
		})
		result, err := NewOutputProcessor().Process(OutputProcessRequest{
			Task:         req.Task,
			Schema:       req.Schema,
			OutputMode:   req.OutputMode,
			DomainValue:  content,
			Stream:       req.Stream,
			SafetyPolicy: req.SafetyPolicy,
			Validate: func(value interface{}) error {
				return ValidateScenarioContent(value.(domain.ScenarioContent), true)
			},
		})
		result.Safety = safety.toVerdict(req.SafetyPolicy)
		return content, result, err
	}
}

func scenarioContentSafetyText(content domain.ScenarioContent) []string {
	values := []string{content.RootCause, content.ArchitectureDiagram}
	if content.ArchitectureDiagramSpec != nil {
		for _, node := range content.ArchitectureDiagramSpec.Nodes {
			values = append(values, node.Label)
		}
		for _, edge := range content.ArchitectureDiagramSpec.Edges {
			values = append(values, edge.Label)
		}
	}
	values = append(values, content.RootCauseKeywords...)
	values = append(values, content.KeyEvidence...)
	values = append(values, content.StandardProcedure...)
	values = append(values, content.ReferenceLinks...)
	for _, clue := range append(append(content.RevealStrategy.SurfaceClues, content.RevealStrategy.DeepClues...), content.RevealStrategy.Distractors...) {
		values = append(values, clue.Content, clue.RecommendedNextAsk)
		values = append(values, clue.TriggerKeywords...)
	}
	return values
}

func processScenarioReply(req RouterRequest, replyReq ScenarioReplyRequest) outputProcessFunc {
	return func(value interface{}) (interface{}, OutputProcessResult, error) {
		reply, _ := value.(string)
		processorResult, err := NewOutputProcessor().Process(OutputProcessRequest{
			Task:         req.Task,
			Schema:       req.Schema,
			OutputMode:   req.OutputMode,
			RawOutput:    fmt.Sprintf(`{"reply":%q}`, reply),
			Stream:       req.Stream,
			SafetyPolicy: req.SafetyPolicy,
			Validate: func(interface{}) error {
				return ValidateScenarioReply(reply)
			},
		})
		if err != nil {
			return value, processorResult, err
		}
		forbidden := append([]string{}, scenarioReplyForbiddenTerms(replyReq)...)
		safety := NewSafetyFilter().Apply(SafetyFilterRequest{
			Task:           req.Task,
			Text:           reply,
			Policy:         req.SafetyPolicy,
			ForbiddenTerms: forbidden,
		})
		processorResult.Safety = safety.toVerdict(req.SafetyPolicy)
		if safety.RewriteUsed {
			reply = safety.SanitizedPreview
		}
		return reply, processorResult, nil
	}
}

func scenarioReplyForbiddenTerms(req ScenarioReplyRequest) []string {
	terms := append([]string{}, req.ForbiddenTerms...)
	if req.IsAnswerLeak {
		terms = append(terms, req.UserMessage)
	}
	return terms
}

func (result SafetyFilterResult) toVerdict(policy string) SafetyVerdict {
	return SafetyVerdict{
		Policy:      defaultString(policy, SafetyPolicyDefault),
		Status:      defaultString(result.Status, "passed"),
		Detail:      result.Detail,
		Blocked:     result.Blocked,
		RewriteUsed: result.RewriteUsed,
	}
}

func applyProcessResultToDecision(decision *RouterDecision, result OutputProcessResult) {
	if decision == nil {
		return
	}
	if result.Output.ParseStatus != "" {
		decision.Output = result.Output
	}
	if result.Validation.Status != "" {
		decision.Validation = result.Validation
	}
	if result.Safety.Status != "" {
		decision.Safety = result.Safety
	}
}

func (r *Router) recordDecision(decision RouterDecision, meta CallMeta, err error) {
	if r == nil || r.telemetry == nil {
		return
	}
	r.telemetry.record(decision, meta, err)
}

func routerDecision(req RouterRequest, provider ProviderInfo, meta CallMeta, started time.Time, completed time.Time, err error) RouterDecision {
	if completed.IsZero() {
		completed = time.Now()
	}
	status := "ok"
	errorType := ""
	errorMessage := ""
	validation := ValidationResult{Required: req.Schema != "", Schema: req.Schema, Status: "passed"}
	if err != nil {
		status = "failed"
		errorType = classifyRouterError(err)
		errorMessage = sanitizeErrorMessage(err.Error())
		if errorType == "validation" {
			validation.Status = "failed"
			validation.Detail = errorMessage
		}
	}
	if req.Schema == "" {
		validation.Status = "skipped"
	}
	safety := SafetyVerdict{Policy: req.SafetyPolicy, Status: "passed"}
	if meta.SafetyRewritten {
		safety.Status = "rewritten"
		safety.Detail = "输出经过安全重写"
	}
	capability := capabilityForProvider(provider, req.Stream)
	fallbackChain := []string{provider.Provider}
	if meta.FallbackUsed && provider.Provider != ProviderMock {
		fallbackChain = append(fallbackChain, ProviderMock)
	}
	return RouterDecision{
		TraceID:        meta.TraceID,
		Task:           req.Task,
		Provider:       provider.Provider,
		Model:          provider.Model,
		Schema:         req.Schema,
		Prompt:         req.Prompt,
		PromptTemplate: req.PromptTemplate,
		OutputMode:     req.OutputMode,
		Stream:         req.Stream,
		SafetyPolicy:   req.SafetyPolicy,
		FallbackChain:  fallbackChain,
		Context:        normalizeContextWindow(req.Context),
		Capability:     capability,
		Validation:     validation,
		Safety:         safety,
		StartedAt:      started,
		CompletedAt:    completed,
		LatencyMS:      completed.Sub(started).Milliseconds(),
		Status:         status,
		ErrorType:      errorType,
		ErrorMessage:   errorMessage,
	}
}

func (r *Router) recordProviderHealth(provider string, err error) ProviderHealth {
	if r == nil || r.health == nil {
		return ProviderHealth{Provider: provider, Status: "unknown"}
	}
	return r.health.record(provider, err)
}

func (r *Router) providerHealth(provider string) ProviderHealth {
	if r == nil || r.health == nil {
		return ProviderHealth{Provider: provider, Status: "unknown"}
	}
	return r.health.snapshot(provider)
}

func (r *Router) rateLimitSnapshot(provider string) ProviderRateLimit {
	if r == nil || r.rateLimiter == nil {
		return ProviderRateLimit{Provider: provider, Status: "ok"}
	}
	return r.rateLimiter.snapshot(provider)
}

func (r *Router) providerPoolSnapshot(info ProviderInfo) ProviderPoolSnapshot {
	telemetry := RouterTelemetry{}
	if r != nil && r.telemetry != nil {
		telemetry = r.telemetry.snapshot()
	}
	healthByProvider := map[string]ProviderHealth{}
	if r != nil && r.health != nil {
		healthByProvider = r.health.snapshotAll()
	}
	activeProvider := info.Provider
	if telemetry.LastDecision != nil && telemetry.LastDecision.Provider != "" {
		activeProvider = telemetry.LastDecision.Provider
	}
	order := providerFallbackOrder()
	providers := make([]ProviderPoolProvider, 0, len(order))
	degraded := 0
	for _, providerName := range order {
		providerInfo := ProviderInfo{Provider: providerName, Model: providerName}
		if providerName == info.Provider {
			providerInfo = info
		}
		if providerName == ProviderDeepSeek && strings.TrimSpace(providerInfo.Model) == "" {
			providerInfo.Model = defaultDeepSeekModel
		}
		if providerName == ProviderOpenAICompatible && strings.TrimSpace(providerInfo.Model) == "" {
			providerInfo.Model = defaultJianyiModel
		}
		capability := capabilityForProvider(providerInfo, true)
		health := healthByProvider[providerName]
		status := capability.Health
		if health.Status != "" && health.Status != "unknown" {
			status = health.Status
		}
		if status == "degraded" || status == "limited" {
			degraded++
		}
		rateLimit := ProviderRateLimit{Provider: providerName, Status: "ok"}
		if r != nil {
			rateLimit = r.rateLimitSnapshot(providerName)
		}
		if rateLimit.Status == "limited" && status != "degraded" {
			status = "limited"
			degraded++
		}
		providers = append(providers, ProviderPoolProvider{
			Provider:       providerName,
			Model:          capability.Model,
			Health:         status,
			Status:         status,
			Priority:       capability.Priority,
			Enabled:        r.providerByName(providerName) != nil,
			LastCheckedAt:  health.LastCheckedAt,
			LastErrorType:  health.LastErrorType,
			LastError:      health.LastError,
			CallCount:      telemetry.ProviderCalls[providerName],
			FallbackReason: telemetry.LastFallbackReason,
			Capability:     capability,
			RateLimit:      rateLimit,
		})
	}
	return ProviderPoolSnapshot{
		ActiveProvider: activeProvider,
		FallbackOrder:  order,
		DegradedCount:  degraded,
		Providers:      providers,
		RecentAttempts: append([]FallbackAttempt(nil), telemetry.RecentAttempts...),
		UpdatedAt:      telemetry.UpdatedAt,
	}
}

func providerFallbackOrder() []string {
	return []string{ProviderDeepSeek, ProviderQwen, ProviderERNIE, ProviderOpenAICompatible, ProviderMock}
}

func enrichProviderInfo(info ProviderInfo, streamEnabled bool, telemetry *routerTelemetryStore) ProviderInfo {
	info.StreamEnabled = streamEnabled
	info.RouterVersion = "router-v1"
	info.Capability = capabilityForProvider(info, streamEnabled)
	info.Transport = info.Capability.Transport
	info.Health = info.Capability.Health
	info.Healthy = info.Health == "ok" || info.Health == "fallback"
	if telemetry != nil {
		snapshot := telemetry.snapshot()
		info.Telemetry = snapshot
		if snapshot.LastDecision != nil {
			info.LastTraceID = snapshot.LastDecision.TraceID
			info.LastTask = snapshot.LastDecision.Task
			info.LastLatencyMS = snapshot.LastDecision.LatencyMS
		}
		info.LastError = snapshot.LastError
		info.LastErrorType = snapshot.LastErrorType
		if snapshot.LastErrorAt != nil {
			info.LastErrorAt = snapshot.LastErrorAt.Format(time.RFC3339)
		}
		info.LastFallbackReason = snapshot.LastFallbackReason
		info.LastFallbackError = snapshot.LastFallbackError
	}
	return info
}

func parseInt(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseFloat(value string, fallback float64) float64 {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func parseBool(value string, fallback bool) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return fallback
	}
}
