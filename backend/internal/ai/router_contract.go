package ai

import (
	"strings"
	"time"
)

const (
	RouterTaskScenarioGenerate     = "scenario_generate"
	RouterTaskCommunityStructure   = "community_structure"
	RouterTaskScenarioReply        = "scenario_reply"
	RouterTaskInterviewFeedback    = "interview_feedback"
	RouterTaskSensitiveCheck       = "sensitive_check"
	RouterTaskStatusCheck          = "router_status"
	OutputModeJSON                 = "json"
	OutputModeText                 = "text"
	OutputModeStatus               = "status"
	SafetyPolicyDefault            = "default"
	SafetyPolicySensitiveDetection = "sensitive_detection"
)

type RouterRequest struct {
	Task            string         `json:"task"`
	Domain          string         `json:"domain,omitempty"`
	UserID          string         `json:"user_id,omitempty"`
	OutputMode      string         `json:"output_mode"`
	Schema          string         `json:"schema,omitempty"`
	Prompt          string         `json:"prompt,omitempty"`
	PromptTemplate  PromptTemplate `json:"prompt_template"`
	Stream          bool           `json:"stream"`
	StreamPreferred bool           `json:"stream_preferred"`
	SafetyPolicy    string         `json:"safety_policy"`
	Context         ContextWindow  `json:"context"`
	ContextInput    ContextInput   `json:"-"`
	PolicyVersion   string         `json:"policy_version,omitempty"`
	FallbackChain   []string       `json:"fallback_chain,omitempty"`
	StrictFailure   bool           `json:"strict_failure,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

type RouterDecision struct {
	TraceID          string             `json:"trace_id"`
	Task             string             `json:"task"`
	Provider         string             `json:"provider"`
	Model            string             `json:"model"`
	Schema           string             `json:"schema,omitempty"`
	Prompt           string             `json:"prompt,omitempty"`
	PromptTemplate   PromptTemplate     `json:"prompt_template"`
	OutputMode       string             `json:"output_mode"`
	Stream           bool               `json:"stream"`
	SafetyPolicy     string             `json:"safety_policy"`
	FallbackChain    []string           `json:"fallback_chain"`
	FallbackAttempts []FallbackAttempt  `json:"fallback_attempts,omitempty"`
	Context          ContextWindow      `json:"context"`
	Capability       ProviderCapability `json:"capability"`
	ProviderHealth   ProviderHealth     `json:"provider_health,omitempty"`
	RateLimit        ProviderRateLimit  `json:"rate_limit,omitempty"`
	Output           OutputTelemetry    `json:"output"`
	Validation       ValidationResult   `json:"validation"`
	Safety           SafetyVerdict      `json:"safety"`
	StartedAt        time.Time          `json:"started_at"`
	CompletedAt      time.Time          `json:"completed_at,omitempty"`
	LatencyMS        int64              `json:"latency_ms,omitempty"`
	Status           string             `json:"status"`
	ErrorType        string             `json:"error_type,omitempty"`
	ErrorMessage     string             `json:"error_message,omitempty"`
	Meta             map[string]string  `json:"meta,omitempty"`
}

type OutputTelemetry struct {
	ParseStatus string `json:"parse_status"`
	RepairUsed  bool   `json:"repair_used"`
}

type ProviderCapability struct {
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	Transport         string   `json:"transport"`
	SupportsStreaming bool     `json:"supports_streaming"`
	SupportsJSON      bool     `json:"supports_json"`
	SupportsTools     bool     `json:"supports_tools"`
	Temperature       bool     `json:"temperature"`
	TopP              bool     `json:"top_p"`
	TopK              bool     `json:"top_k"`
	MaxTokens         int      `json:"max_tokens"`
	CostTier          string   `json:"cost_tier"`
	Health            string   `json:"health"`
	Priority          int      `json:"priority"`
	SupportedTasks    []string `json:"supported_tasks"`
}

type ProviderPoolSnapshot struct {
	ActiveProvider string                 `json:"active_provider"`
	FallbackOrder  []string               `json:"fallback_order"`
	DegradedCount  int                    `json:"degraded_count"`
	Providers      []ProviderPoolProvider `json:"providers"`
	RecentAttempts []FallbackAttempt      `json:"recent_attempts,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

func (p ProviderPoolSnapshot) ProviderByName(provider string) *ProviderPoolProvider {
	for i := range p.Providers {
		if p.Providers[i].Provider == provider {
			return &p.Providers[i]
		}
	}
	return nil
}

type ProviderPoolProvider struct {
	Provider       string             `json:"provider"`
	Model          string             `json:"model"`
	Health         string             `json:"health"`
	Status         string             `json:"status"`
	Priority       int                `json:"priority"`
	Enabled        bool               `json:"enabled"`
	LastCheckedAt  time.Time          `json:"last_checked_at,omitempty"`
	LastErrorType  string             `json:"last_error_type,omitempty"`
	LastError      string             `json:"last_error,omitempty"`
	CallCount      int64              `json:"call_count"`
	FallbackReason string             `json:"fallback_reason,omitempty"`
	Capability     ProviderCapability `json:"capability"`
	RateLimit      ProviderRateLimit  `json:"rate_limit"`
}

type ProviderHealth struct {
	Provider            string    `json:"provider,omitempty"`
	Status              string    `json:"status,omitempty"`
	LastCheckedAt       time.Time `json:"last_checked_at,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures,omitempty"`
	LastErrorType       string    `json:"last_error_type,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
}

type ProviderRateLimit struct {
	Provider string `json:"provider,omitempty"`
	Status   string `json:"status,omitempty"`
	Limit    int64  `json:"limit,omitempty"`
	InFlight int64  `json:"in_flight,omitempty"`
}

type FallbackAttempt struct {
	Provider       string    `json:"provider"`
	Model          string    `json:"model,omitempty"`
	Success        bool      `json:"success"`
	ErrorType      string    `json:"error_type,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	FallbackReason string    `json:"fallback_reason,omitempty"`
	LatencyMS      int64     `json:"latency_ms,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
}

type ContextWindow struct {
	Version              string   `json:"version"`
	Strategy             string   `json:"strategy"`
	OriginalMessages     int      `json:"original_messages"`
	RetainedMessages     int      `json:"retained_messages"`
	SummaryRetained      bool     `json:"summary_retained"`
	KeyFactsRetained     []string `json:"key_facts_retained,omitempty"`
	EstimatedInputTokens int      `json:"estimated_input_tokens"`
	MaxInputTokens       int      `json:"max_input_tokens"`
	Compressed           bool     `json:"compressed"`
}

type PromptTemplate struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Task      string `json:"task"`
	Schema    string `json:"schema,omitempty"`
	ManagedBy string `json:"managed_by"`
}

type ValidationResult struct {
	Required bool   `json:"required"`
	Schema   string `json:"schema,omitempty"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

type SafetyVerdict struct {
	Policy      string `json:"policy"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Blocked     bool   `json:"blocked"`
	RewriteUsed bool   `json:"rewrite_used,omitempty"`
}

func routerRequest(task string, options ...func(*RouterRequest)) RouterRequest {
	req := RouterRequest{
		Task:         task,
		OutputMode:   OutputModeJSON,
		SafetyPolicy: SafetyPolicyDefault,
		Context: ContextWindow{
			Version:        contextVersion,
			Strategy:       string(ContextStrategyDirect),
			MaxInputTokens: 8192,
		},
		CreatedAt: time.Now(),
	}
	applyTaskPolicy(&req, policyForTask(task))
	for _, option := range options {
		option(&req)
	}
	if strings.TrimSpace(req.OutputMode) == "" {
		req.OutputMode = OutputModeJSON
	}
	if strings.TrimSpace(req.SafetyPolicy) == "" {
		req.SafetyPolicy = SafetyPolicyDefault
	}
	if strings.TrimSpace(req.PromptTemplate.Name) == "" && strings.TrimSpace(req.Prompt) != "" {
		req.PromptTemplate = PromptTemplate{Name: req.Prompt, Version: promptVersionV1, Task: req.Task, Schema: req.Schema, ManagedBy: "router_request"}
	}
	return req
}

func applyTaskPolicy(req *RouterRequest, policy TaskPolicy) {
	if req == nil {
		return
	}
	req.OutputMode = policy.OutputMode
	req.Schema = policy.SchemaName
	req.Prompt = policy.PromptName
	req.PromptTemplate = promptTemplateForPolicy(policy)
	req.SafetyPolicy = policy.SafetyPolicy
	req.PolicyVersion = policy.PromptVersion
	req.StreamPreferred = policy.StreamAllowed
	req.FallbackChain = append([]string{}, policy.FallbackChain...)
	req.StrictFailure = policy.StrictFailure
	req.Context = normalizeContextWindow(ContextWindow{
		Version:        contextVersion,
		Strategy:       string(policy.ContextStrategy),
		MaxInputTokens: 8192,
	})
}

func withDomain(domain string) func(*RouterRequest) {
	return func(req *RouterRequest) { req.Domain = strings.TrimSpace(domain) }
}

func withUserID(userID string) func(*RouterRequest) {
	return func(req *RouterRequest) { req.UserID = strings.TrimSpace(userID) }
}

func withSchema(schema string) func(*RouterRequest) {
	return func(req *RouterRequest) { req.Schema = strings.TrimSpace(schema) }
}

func withPrompt(prompt string) func(*RouterRequest) {
	return func(req *RouterRequest) {
		req.Prompt = strings.TrimSpace(prompt)
		if req.Prompt != "" {
			req.PromptTemplate.Name = req.Prompt
		}
	}
}

func withStream(stream bool) func(*RouterRequest) {
	return func(req *RouterRequest) {
		req.Stream = stream
		req.StreamPreferred = stream
	}
}

func withOutputMode(outputMode string) func(*RouterRequest) {
	return func(req *RouterRequest) { req.OutputMode = strings.TrimSpace(outputMode) }
}

func withSafetyPolicy(policy string) func(*RouterRequest) {
	return func(req *RouterRequest) { req.SafetyPolicy = strings.TrimSpace(policy) }
}

func withContextWindow(window ContextWindow) func(*RouterRequest) {
	return func(req *RouterRequest) { req.Context = normalizeContextWindow(window) }
}

func normalizeContextWindow(window ContextWindow) ContextWindow {
	if window.Version == "" {
		window.Version = contextVersion
	}
	if window.Strategy == "" {
		window.Strategy = string(ContextStrategyDirect)
	}
	if window.MaxInputTokens <= 0 {
		window.MaxInputTokens = 8192
	}
	window.Compressed = window.Compressed || window.SummaryRetained || window.OriginalMessages > window.RetainedMessages && window.RetainedMessages > 0
	return window
}

func scenarioContextWindow(summary string, messages []ScenarioContextMessage) ContextWindow {
	return NewContextManager().Build(scenarioReplyContextInput(ScenarioReplyRequest{
		ConversationSummary: summary,
		RecentMessages:      messages,
	})).Window
}

func estimateScenarioMessagesTokens(messages []ScenarioContextMessage) int {
	total := 0
	for _, message := range messages {
		total += estimateTokens(message.UserContent)
		total += estimateTokens(message.AssistantContent)
	}
	return total
}

func estimateTokens(text string) int {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return 0
	}
	return (len(runes) + 3) / 4
}
