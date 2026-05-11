package ai

import "strings"

type providerCapabilityDefinition struct {
	Transport         string
	CostTier          string
	MaxTokens         int
	Priority          int
	SupportsStreaming bool
	SupportsJSON      bool
	SupportsTools     bool
	Temperature       bool
	TopP              bool
	TopK              bool
}

var providerCapabilityRegistry = map[string]providerCapabilityDefinition{
	ProviderDeepSeek: {
		Transport:         "openai-compatible",
		CostTier:          "low",
		MaxTokens:         8192,
		Priority:          10,
		SupportsStreaming: true,
		SupportsJSON:      true,
		Temperature:       true,
		TopP:              true,
		TopK:              true,
	},
	ProviderQwen: {
		Transport:         "openai-compatible",
		CostTier:          "standard",
		MaxTokens:         8192,
		Priority:          20,
		SupportsStreaming: true,
		SupportsJSON:      true,
		Temperature:       true,
		TopP:              true,
		TopK:              true,
	},
	ProviderERNIE: {
		Transport:         "openai-compatible",
		CostTier:          "standard",
		MaxTokens:         8192,
		Priority:          30,
		SupportsStreaming: true,
		SupportsJSON:      true,
		Temperature:       true,
		TopP:              true,
		TopK:              true,
	},
	ProviderOpenAICompatible: {
		Transport:         "openai-compatible",
		CostTier:          "configured",
		MaxTokens:         8192,
		Priority:          40,
		SupportsStreaming: true,
		SupportsJSON:      true,
		Temperature:       true,
		TopP:              true,
		TopK:              true,
	},
	ProviderMock: {
		Transport:         "in-process",
		CostTier:          "free",
		MaxTokens:         4096,
		Priority:          99,
		SupportsStreaming: true,
		SupportsJSON:      true,
	},
}

func capabilityForProvider(info ProviderInfo, _ bool) ProviderCapability {
	provider := strings.TrimSpace(info.Provider)
	if provider == "" {
		provider = ProviderMock
	}
	def, ok := providerCapabilityRegistry[provider]
	if !ok {
		def = providerCapabilityDefinition{
			Transport:         "custom",
			CostTier:          "standard",
			MaxTokens:         8192,
			Priority:          50,
			SupportsStreaming: true,
			SupportsJSON:      true,
			Temperature:       true,
			TopP:              true,
			TopK:              true,
		}
	}
	health := "ok"
	if info.Fallback {
		health = "fallback"
	}
	if info.InitError != "" || info.LastError != "" {
		health = "degraded"
	}
	return ProviderCapability{
		Provider:          provider,
		Model:             info.Model,
		Transport:         def.Transport,
		SupportsStreaming: def.SupportsStreaming,
		SupportsJSON:      def.SupportsJSON,
		SupportsTools:     def.SupportsTools,
		Temperature:       def.Temperature,
		TopP:              def.TopP,
		TopK:              def.TopK,
		MaxTokens:         def.MaxTokens,
		CostTier:          def.CostTier,
		Health:            health,
		Priority:          def.Priority,
		SupportedTasks: []string{
			RouterTaskScenarioGenerate,
			RouterTaskCommunityStructure,
			RouterTaskScenarioReply,
			RouterTaskInterviewFeedback,
			RouterTaskSensitiveCheck,
			RouterTaskStatusCheck,
		},
	}
}
