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

func TestContextManagerKeepsSummaryFactsRecentMessagesAndCurrentQuestion(t *testing.T) {
	manager := NewContextManager()
	messages := []ContextMessage{
		{Role: "user", Content: "第 1 轮：先看服务状态"},
		{Role: "assistant", Content: "服务状态正常"},
		{Role: "user", Content: "第 2 轮：看数据库连接"},
		{Role: "assistant", Content: "连接池使用率升高"},
		{Role: "user", Content: "第 3 轮：看慢查询"},
		{Role: "assistant", Content: "慢查询没有明显增加"},
		{Role: "user", Content: "第 4 轮：看等待队列"},
		{Role: "assistant", Content: "等待队列持续增长"},
	}

	plan := manager.Build(ContextInput{
		Task:           RouterTaskScenarioReply,
		Strategy:       ContextStrategySummaryPlusRecent,
		Summary:        "关键事实：连接池使用率升高；等待队列持续增长",
		Messages:       messages,
		CurrentMessage: "是否可以判断是连接池耗尽？",
		MaxMessages:    4,
		MaxInputTokens: 512,
	})

	if plan.Window.Strategy != string(ContextStrategySummaryPlusRecent) {
		t.Fatalf("expected summary_plus_recent strategy, got %+v", plan.Window)
	}
	if !plan.Window.SummaryRetained || !plan.Window.Compressed {
		t.Fatalf("expected summary to be retained and compressed, got %+v", plan.Window)
	}
	if plan.Window.OriginalMessages != len(messages)+1 {
		t.Fatalf("expected original messages to include current question, got %+v", plan.Window)
	}
	if plan.Window.RetainedMessages != 4 {
		t.Fatalf("expected recent window to retain 4 messages, got %+v", plan.Window)
	}
	if len(plan.RetainedMessages) != 4 {
		t.Fatalf("expected 4 retained messages, got %+v", plan.RetainedMessages)
	}
	if plan.RetainedMessages[len(plan.RetainedMessages)-1].Content != "是否可以判断是连接池耗尽？" {
		t.Fatalf("expected current question to be retained last, got %+v", plan.RetainedMessages)
	}
	if len(plan.Window.KeyFactsRetained) == 0 {
		t.Fatalf("expected summary key facts to be retained, got %+v", plan.Window)
	}
}

func TestContextManagerDirectStrategyDoesNotTrimShortContext(t *testing.T) {
	manager := NewContextManager()
	messages := []ContextMessage{
		{Role: "system", Content: "只释放已允许线索"},
		{Role: "user", Content: "查看日志"},
	}

	plan := manager.Build(ContextInput{
		Task:           RouterTaskScenarioReply,
		Strategy:       ContextStrategyDirect,
		Messages:       messages,
		MaxMessages:    8,
		MaxInputTokens: 1024,
	})

	if plan.Window.Strategy != string(ContextStrategyDirect) {
		t.Fatalf("expected direct strategy, got %+v", plan.Window)
	}
	if plan.Window.Compressed || plan.Window.OriginalMessages != 2 || plan.Window.RetainedMessages != 2 {
		t.Fatalf("short direct context should not be trimmed, got %+v", plan.Window)
	}
	if len(plan.RetainedMessages) != len(messages) {
		t.Fatalf("expected all messages retained, got %+v", plan.RetainedMessages)
	}
}

func TestTaskPolicyRegistryBindsCoreTasks(t *testing.T) {
	for _, task := range []string{
		RouterTaskScenarioGenerate,
		RouterTaskScenarioReply,
		RouterTaskCommunityStructure,
		RouterTaskInterviewFeedback,
	} {
		policy, ok := LookupTaskPolicy(task)
		if !ok {
			t.Fatalf("expected policy for %s", task)
		}
		if policy.PromptName == "" || policy.PromptVersion == "" || policy.SchemaName == "" {
			t.Fatalf("expected prompt and schema binding for %s: %+v", task, policy)
		}
		if policy.OutputMode == "" || policy.SafetyPolicy == "" || policy.ContextStrategy == "" {
			t.Fatalf("expected output, safety and context policy for %s: %+v", task, policy)
		}
	}
}

func TestRouterDecisionRecordsPolicyPromptSchemaAndContext(t *testing.T) {
	router := NewRouter(Config{Provider: ProviderMock, StreamEnabled: true, StreamConfigured: true})
	_, meta, err := router.RewriteScenarioReplyStream(context.Background(), ScenarioReplyRequest{
		QuestionTitle:       "连接池排查",
		UserMessage:         "继续看等待队列",
		AllowedContent:      "等待队列持续增长。",
		ConversationSummary: "关键事实：连接池使用率升高",
		RecentMessages: []ScenarioContextMessage{
			{TurnNumber: 1, UserContent: "看连接池", AssistantContent: "连接池使用率升高。"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Task != RouterTaskScenarioReply {
		t.Fatalf("expected scenario reply meta, got %+v", meta)
	}

	info := router.Info()
	decision := info.Telemetry.RecentDecisions[0]
	if decision.PromptTemplate.Name != "scenario_reply" || decision.PromptTemplate.Version == "" {
		t.Fatalf("expected prompt template metadata, got %+v", decision)
	}
	if decision.Schema != SchemaScenarioReply || decision.Validation.Schema != SchemaScenarioReply {
		t.Fatalf("expected schema metadata, got %+v", decision)
	}
	if decision.Context.Strategy != string(ContextStrategySummaryPlusRecent) || !decision.Context.SummaryRetained {
		t.Fatalf("expected scenario reply context policy, got %+v", decision.Context)
	}
	if len(decision.FallbackChain) != 1 || decision.FallbackChain[0] != ProviderMock {
		t.Fatalf("mock decision should expose actual call chain only, got %+v", decision.FallbackChain)
	}
}

func TestScenarioReplyContextPolicyTrimsPromptInput(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) < 2 {
			t.Fatalf("expected user prompt message, got %+v", body.Messages)
		}
		prompt = body.Messages[1].Content
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"reply\":\"等待队列持续增长，可以继续核对连接池上限。\"}"}}]}`))
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
	_, _, err := router.RewriteScenarioReply(context.Background(), ScenarioReplyRequest{
		QuestionTitle:       "连接池排查",
		UserMessage:         "是否可以判断是连接池耗尽？",
		AllowedContent:      "等待队列持续增长。",
		ConversationSummary: "关键事实：连接池使用率升高；等待队列持续增长",
		RecentMessages: []ScenarioContextMessage{
			{TurnNumber: 1, UserContent: "legacy user 1", AssistantContent: "legacy assistant 1"},
			{TurnNumber: 2, UserContent: "legacy user 2", AssistantContent: "legacy assistant 2"},
			{TurnNumber: 3, UserContent: "recent user 3", AssistantContent: "recent assistant 3"},
			{TurnNumber: 4, UserContent: "recent user 4", AssistantContent: "recent assistant 4"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "legacy user 1") || strings.Contains(prompt, "legacy assistant 1") {
		t.Fatalf("expected early context to be trimmed from prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "recent user 4") || !strings.Contains(prompt, "是否可以判断是连接池耗尽？") {
		t.Fatalf("expected recent context and current question in prompt, got %s", prompt)
	}
}
