package ai

type TaskPolicy struct {
	Task            string          `json:"task"`
	PromptName      string          `json:"prompt_name"`
	PromptVersion   string          `json:"prompt_version"`
	SchemaName      string          `json:"schema_name,omitempty"`
	OutputMode      string          `json:"output_mode"`
	StreamAllowed   bool            `json:"stream_allowed"`
	SafetyPolicy    string          `json:"safety_policy"`
	ContextStrategy ContextStrategy `json:"context_strategy"`
	FallbackChain   []string        `json:"fallback_chain"`
	StrictFailure   bool            `json:"strict_failure"`
}

const promptVersionV1 = "1.0.0"

var taskPolicies = map[string]TaskPolicy{
	RouterTaskScenarioGenerate: {
		Task:            RouterTaskScenarioGenerate,
		PromptName:      "scenario_generate",
		PromptVersion:   promptVersionV1,
		SchemaName:      SchemaScenarioQuestion,
		OutputMode:      OutputModeJSON,
		StreamAllowed:   false,
		SafetyPolicy:    SafetyPolicyDefault,
		ContextStrategy: ContextStrategyDirect,
		FallbackChain:   []string{ProviderDeepSeek, ProviderOpenAICompatible, ProviderMock},
		StrictFailure:   true,
	},
	RouterTaskScenarioReply: {
		Task:            RouterTaskScenarioReply,
		PromptName:      "scenario_reply",
		PromptVersion:   promptVersionV1,
		SchemaName:      SchemaScenarioReply,
		OutputMode:      OutputModeJSON,
		StreamAllowed:   true,
		SafetyPolicy:    SafetyPolicyDefault,
		ContextStrategy: ContextStrategySummaryPlusRecent,
		FallbackChain:   []string{ProviderDeepSeek, ProviderOpenAICompatible, ProviderMock},
		StrictFailure:   false,
	},
	RouterTaskCommunityStructure: {
		Task:            RouterTaskCommunityStructure,
		PromptName:      "community_structure",
		PromptVersion:   promptVersionV1,
		SchemaName:      SchemaScenarioContentPreview,
		OutputMode:      OutputModeJSON,
		StreamAllowed:   true,
		SafetyPolicy:    SafetyPolicyDefault,
		ContextStrategy: ContextStrategyDirect,
		FallbackChain:   []string{ProviderDeepSeek, ProviderOpenAICompatible, ProviderMock},
		StrictFailure:   false,
	},
	RouterTaskInterviewFeedback: {
		Task:            RouterTaskInterviewFeedback,
		PromptName:      "interview_feedback",
		PromptVersion:   promptVersionV1,
		SchemaName:      SchemaInterviewFeedback,
		OutputMode:      OutputModeJSON,
		StreamAllowed:   true,
		SafetyPolicy:    SafetyPolicyDefault,
		ContextStrategy: ContextStrategyRecentWindow,
		FallbackChain:   []string{ProviderDeepSeek, ProviderOpenAICompatible, ProviderMock},
		StrictFailure:   false,
	},
	RouterTaskSensitiveCheck: {
		Task:            RouterTaskSensitiveCheck,
		PromptName:      "sensitive_check",
		PromptVersion:   promptVersionV1,
		SchemaName:      SchemaSensitiveCheck,
		OutputMode:      OutputModeJSON,
		StreamAllowed:   false,
		SafetyPolicy:    SafetyPolicySensitiveDetection,
		ContextStrategy: ContextStrategyDirect,
		FallbackChain:   []string{ProviderDeepSeek, ProviderOpenAICompatible, ProviderMock},
		StrictFailure:   false,
	},
	RouterTaskStatusCheck: {
		Task:            RouterTaskStatusCheck,
		PromptName:      "router_status",
		PromptVersion:   promptVersionV1,
		OutputMode:      OutputModeStatus,
		StreamAllowed:   false,
		SafetyPolicy:    SafetyPolicyDefault,
		ContextStrategy: ContextStrategyDirect,
		FallbackChain:   []string{ProviderMock},
		StrictFailure:   false,
	},
}

func LookupTaskPolicy(task string) (TaskPolicy, bool) {
	policy, ok := taskPolicies[task]
	if !ok {
		return TaskPolicy{}, false
	}
	policy.FallbackChain = append([]string{}, policy.FallbackChain...)
	return policy, true
}

func policyForTask(task string) TaskPolicy {
	if policy, ok := LookupTaskPolicy(task); ok {
		return policy
	}
	return TaskPolicy{
		Task:            task,
		PromptVersion:   promptVersionV1,
		OutputMode:      OutputModeJSON,
		StreamAllowed:   true,
		SafetyPolicy:    SafetyPolicyDefault,
		ContextStrategy: ContextStrategyDirect,
		FallbackChain:   []string{ProviderMock},
		StrictFailure:   false,
	}
}

func promptTemplateForPolicy(policy TaskPolicy) PromptTemplate {
	return PromptTemplate{
		Name:      policy.PromptName,
		Version:   policy.PromptVersion,
		Task:      policy.Task,
		Schema:    policy.SchemaName,
		ManagedBy: "router_policy",
	}
}
