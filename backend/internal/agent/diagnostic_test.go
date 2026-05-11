package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

func TestDiagnosticAgentBlocksRootCauseGuess(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{Rewrite: echoRewrite})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我觉得根因就是连接池耗尽导致数据库慢",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Meta.IsAnswerLeak || !result.Meta.IsSanitized {
		t.Fatalf("expected answer leak to be sanitized: %#v", result.Meta)
	}
	if result.Meta.ResponseType != "insufficient" {
		t.Fatalf("expected insufficient response, got %q", result.Meta.ResponseType)
	}
	if len(session.RevealedClueIDs) != 0 {
		t.Fatalf("root cause guess should not reveal clues: %#v", session.RevealedClueIDs)
	}
	if !(strings.Contains(result.AssistantContent, "证据链") || strings.Contains(result.AssistantContent, "判断依据")) {
		t.Fatalf("expected structured anti-guess content, got %q", result.AssistantContent)
	}
	if result.Meta.AgentTrace == nil || result.Meta.AgentTrace.Agent != "diagnostic_agent" {
		t.Fatalf("expected diagnostic trace: %#v", result.Meta.AgentTrace)
	}
}

func TestDiagnosticAgentRevealsSurfaceClue(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{Rewrite: echoRewrite})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我想先看日志和发布时间",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.ResponseType != "partial" {
		t.Fatalf("expected partial response, got %q", result.Meta.ResponseType)
	}
	if result.Meta.RevealedClueID != "c1" {
		t.Fatalf("expected c1, got %q", result.Meta.RevealedClueID)
	}
	if len(session.RevealedClueIDs) != 1 || session.RevealedClueIDs[0] != "c1" {
		t.Fatalf("session was not updated: %#v", session.RevealedClueIDs)
	}
	if !(strings.Contains(result.AssistantContent, "释放线索内容") || strings.Contains(result.AssistantContent, "命中")) {
		t.Fatalf("unexpected assistant content: %q", result.AssistantContent)
	}
	if strings.Contains(result.AssistantContent, question.Content.RootCause) {
		t.Fatalf("assistant content leaked root cause: %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentEscalatesHintAfterRepeatedBroadMisses(t *testing.T) {
	session := sampleSession()
	session.NoNewClueStreak = 2
	session.HintLevel = 1
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{Rewrite: echoRewrite})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我想先看异常前后的访问路径变化和波动趋势",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.HintLevel != 2 || result.Meta.HintLevel != 2 {
		t.Fatalf("expected hint level 2, session=%d meta=%d", session.HintLevel, result.Meta.HintLevel)
	}
	if session.NoNewClueStreak != 0 {
		t.Fatalf("expected streak reset, got %d", session.NoNewClueStreak)
	}
	if !(strings.Contains(result.AssistantContent, "具体观察点") || strings.Contains(result.AssistantContent, "方向基本合理")) {
		t.Fatalf("expected structured hint escalation content, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentMarksDistractor(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{Rewrite: echoRewrite})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "是不是 CPU 或网络有问题",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Meta.IsDistractor || result.Meta.ResponseType != "redirect" {
		t.Fatalf("expected distractor redirect: %#v", result.Meta)
	}
	if result.Meta.RevealedClueID != "d1" {
		t.Fatalf("expected d1, got %q", result.Meta.RevealedClueID)
	}
	if !(strings.Contains(result.AssistantContent, "可排除观察") || strings.Contains(result.AssistantContent, "排除")) {
		t.Fatalf("expected structured distractor content, got %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentSafetyRewritesLLMLeak(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: func(context.Context, ai.ScenarioReplyRequest, func(string)) (string, ai.CallMeta, error) {
			return "根因是连接池耗尽导致数据库慢，标准步骤是直接扩容。", ai.CallMeta{Provider: "mock", Validated: true}, nil
		},
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我想先看日志和发布时间",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Meta.SafetyRewritten || !result.Meta.IsSanitized {
		t.Fatalf("expected safety rewrite: %#v", result.Meta)
	}
	if strings.Contains(result.AssistantContent, question.Content.RootCause) {
		t.Fatalf("assistant content leaked root cause: %q", result.AssistantContent)
	}
}

func TestDiagnosticAgentDoesNotForwardUnsafeRewriteDelta(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	rewriteDeltaWasNil := false
	externalDeltaCalled := false
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: func(_ context.Context, _ ai.ScenarioReplyRequest, delta func(string)) (string, ai.CallMeta, error) {
			rewriteDeltaWasNil = delta == nil
			if delta != nil {
				delta("根因是" + question.Content.RootCause)
			}
			return "根因是" + question.Content.RootCause, ai.CallMeta{Provider: "mock", Validated: true}, nil
		},
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我想先看日志和发布时间",
		OnDelta: func(string) {
			externalDeltaCalled = true
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rewriteDeltaWasNil {
		t.Fatal("rewrite delta should not be forwarded before safety rewrite")
	}
	if externalDeltaCalled {
		t.Fatal("external delta callback should not receive unsafe rewrite chunks")
	}
	if !result.Meta.SafetyRewritten {
		t.Fatalf("expected final reply to be safety rewritten: %#v", result.Meta)
	}
}

func TestDiagnosticAgentTraceUsesQuestionForbiddenTerms(t *testing.T) {
	session := sampleSession()
	question := sampleQuestion()
	question.Content.RootCause = "索引缺失导致慢查询"
	question.Content.RootCauseKeywords = []string{"索引缺失"}
	question.Content.StandardProcedure = []string{"使用 EXPLAIN 验证执行计划"}
	question.Content.KeyEvidence = []string{"慢查询日志显示全表扫描"}
	question.Content.RevealStrategy.SurfaceClues[0].Content = "慢查询日志显示全表扫描"
	session.QuestionSnapshot = *question
	agent := NewDiagnosticAgent(DiagnosticConfig{
		Rewrite: func(context.Context, ai.ScenarioReplyRequest, func(string)) (string, ai.CallMeta, error) {
			return "保留确定性回复", ai.CallMeta{Provider: "mock", Validated: true}, nil
		},
	})

	result, err := agent.Run(context.Background(), DiagnosticRequest{
		Session:     session,
		Question:    question,
		UserMessage: "我想先看日志和发布时间",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Meta.AgentTrace == nil {
		t.Fatal("expected agent trace")
	}
	serialized := mustTraceText(result.Meta.AgentTrace)
	for _, forbidden := range []string{"索引缺失导致慢查询", "索引缺失", "使用 EXPLAIN 验证执行计划", "慢查询日志显示全表扫描"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("trace leaked %q in %s", forbidden, serialized)
		}
	}
}

func echoRewrite(_ context.Context, req ai.ScenarioReplyRequest, _ func(string)) (string, ai.CallMeta, error) {
	return req.AllowedContent, ai.CallMeta{Provider: "mock", Validated: true}, nil
}

func mustTraceText(trace *domain.AgentTrace) string {
	var builder strings.Builder
	builder.WriteString(trace.RunID)
	builder.WriteString(trace.Agent)
	builder.WriteString(trace.Mode)
	for _, step := range trace.Steps {
		builder.WriteString(step.Name)
		builder.WriteString(step.Kind)
		builder.WriteString(step.Status)
		builder.WriteString(step.Summary)
		for key, value := range step.Metadata {
			builder.WriteString(key)
			builder.WriteString(value)
		}
	}
	return builder.String()
}

func sampleSession() *domain.ScenarioSession {
	return &domain.ScenarioSession{
		ID:               "session-1",
		UserID:           "user-1",
		QuestionID:       "question-1",
		Status:           "active",
		CurrentTurn:      1,
		MaxTurns:         50,
		RevealedClueIDs:  []string{},
		HintLevel:        1,
		NoNewClueStreak:  0,
		StartedAt:        time.Now(),
		LastActiveAt:     time.Now(),
		QuestionSnapshot: *sampleQuestion(),
	}
}

func sampleQuestion() *domain.ScenarioQuestion {
	return &domain.ScenarioQuestion{
		ID:          "question-1",
		Title:       "数据库慢查询排查",
		Description: "一次发布后数据库响应变慢。",
		Domain:      "backend",
		Difficulty:  "medium",
		Content: domain.ScenarioContent{
			RootCause:         "连接池耗尽导致数据库慢",
			RootCauseKeywords: []string{"连接池耗尽", "数据库慢"},
			StandardProcedure: []string{"查看连接池指标", "回滚异常配置"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{
					{ClueID: "c1", TriggerKeywords: []string{"日志", "发布", "时间"}, Content: "异常开始时间与一次配置发布高度重合。", RecommendedNextAsk: "继续询问配置变更。"},
				},
				DeepClues: []domain.Clue{
					{ClueID: "c2", TriggerKeywords: []string{"配置", "回滚"}, PrerequisiteClues: []string{"c1"}, Content: "回滚连接池配置后错误率下降。", RecommendedNextAsk: "可以提交根因判断。"},
				},
				Distractors: []domain.Clue{
					{ClueID: "d1", TriggerKeywords: []string{"CPU", "网络"}, Content: "CPU 和网络指标正常。", IsDistractor: true},
				},
			},
		},
	}
}
