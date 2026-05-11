package ai

import "strings"

const contextVersion = "router-v1"

type ContextStrategy string

const (
	ContextStrategyDirect            ContextStrategy = "direct"
	ContextStrategyRecentWindow      ContextStrategy = "recent_window"
	ContextStrategySummaryPlusRecent ContextStrategy = "summary_plus_recent"
)

type ContextMessage struct {
	Role    string
	Content string
}

type ContextInput struct {
	Task           string
	Strategy       ContextStrategy
	Summary        string
	Messages       []ContextMessage
	CurrentMessage string
	MaxMessages    int
	MaxInputTokens int
	KeyFacts       []string
}

type ContextPlan struct {
	Window           ContextWindow
	RetainedMessages []ContextMessage
}

type ContextManager struct{}

func NewContextManager() ContextManager {
	return ContextManager{}
}

func (ContextManager) Build(input ContextInput) ContextPlan {
	strategy := normalizeContextStrategy(input.Strategy)
	maxMessages := input.MaxMessages
	if maxMessages <= 0 {
		maxMessages = defaultMaxMessages(strategy)
	}
	maxTokens := input.MaxInputTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	messages := append([]ContextMessage{}, input.Messages...)
	if strings.TrimSpace(input.CurrentMessage) != "" {
		messages = append(messages, ContextMessage{Role: "user", Content: strings.TrimSpace(input.CurrentMessage)})
	}
	original := len(messages)
	retained := messages
	if strategy != ContextStrategyDirect && len(retained) > maxMessages {
		retained = append([]ContextMessage{}, retained[len(retained)-maxMessages:]...)
	}
	if strategy == ContextStrategyDirect && len(retained) > maxMessages {
		retained = append([]ContextMessage{}, retained[len(retained)-maxMessages:]...)
	}
	keyFacts := append([]string{}, input.KeyFacts...)
	if strategy == ContextStrategySummaryPlusRecent {
		keyFacts = append(keyFacts, extractSummaryFacts(input.Summary)...)
	}
	window := normalizeContextWindow(ContextWindow{
		Version:              contextVersion,
		Strategy:             string(strategy),
		OriginalMessages:     original,
		RetainedMessages:     len(retained),
		SummaryRetained:      strategy == ContextStrategySummaryPlusRecent && strings.TrimSpace(input.Summary) != "",
		KeyFactsRetained:     dedupeStrings(keyFacts),
		EstimatedInputTokens: estimateContextTokens(input.Summary, retained),
		MaxInputTokens:       maxTokens,
		Compressed:           len(retained) < original || strategy == ContextStrategySummaryPlusRecent && strings.TrimSpace(input.Summary) != "",
	})
	return ContextPlan{Window: window, RetainedMessages: retained}
}

func normalizeContextStrategy(strategy ContextStrategy) ContextStrategy {
	switch strategy {
	case ContextStrategyRecentWindow, ContextStrategySummaryPlusRecent:
		return strategy
	default:
		return ContextStrategyDirect
	}
}

func defaultMaxMessages(strategy ContextStrategy) int {
	switch strategy {
	case ContextStrategySummaryPlusRecent:
		return 6
	case ContextStrategyRecentWindow:
		return 8
	default:
		return 12
	}
}

func extractSummaryFacts(summary string) []string {
	normalized := strings.TrimSpace(summary)
	if normalized == "" {
		return nil
	}
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '；' || r == ';' || r == '\n'
	})
	facts := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(part, "关键事实："))
		part = strings.TrimSpace(strings.TrimPrefix(part, "关键事实:"))
		if part != "" {
			facts = append(facts, part)
		}
	}
	if len(facts) == 0 {
		return []string{normalized}
	}
	return facts
}

func estimateContextTokens(summary string, messages []ContextMessage) int {
	total := estimateTokens(summary)
	for _, message := range messages {
		total += estimateTokens(message.Role)
		total += estimateTokens(message.Content)
	}
	return total
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, normalized)
	}
	return out
}

func scenarioReplyContextInput(req ScenarioReplyRequest) ContextInput {
	messages := make([]ContextMessage, 0, len(req.RecentMessages)*2)
	for _, message := range req.RecentMessages {
		if strings.TrimSpace(message.UserContent) != "" {
			messages = append(messages, ContextMessage{Role: "user", Content: message.UserContent})
		}
		if strings.TrimSpace(message.AssistantContent) != "" {
			messages = append(messages, ContextMessage{Role: "assistant", Content: message.AssistantContent})
		}
	}
	return ContextInput{
		Task:           RouterTaskScenarioReply,
		Strategy:       ContextStrategySummaryPlusRecent,
		Summary:        req.ConversationSummary,
		Messages:       messages,
		MaxMessages:    6,
		MaxInputTokens: 8192,
	}
}

func prepareScenarioReplyRequest(req ScenarioReplyRequest) (ScenarioReplyRequest, ContextWindow) {
	plan := NewContextManager().Build(scenarioReplyContextInput(req))
	req.RecentMessages = retainedScenarioMessages(plan.RetainedMessages)
	return req, plan.Window
}

func retainedScenarioMessages(messages []ContextMessage) []ScenarioContextMessage {
	out := []ScenarioContextMessage{}
	var pendingUser string
	turn := 1
	for _, message := range messages {
		switch message.Role {
		case "user":
			if pendingUser != "" {
				out = append(out, ScenarioContextMessage{TurnNumber: turn, UserContent: pendingUser})
				turn++
			}
			pendingUser = message.Content
		case "assistant":
			out = append(out, ScenarioContextMessage{TurnNumber: turn, UserContent: pendingUser, AssistantContent: message.Content})
			pendingUser = ""
			turn++
		}
	}
	if pendingUser != "" {
		out = append(out, ScenarioContextMessage{TurnNumber: turn, UserContent: pendingUser})
	}
	return out
}
