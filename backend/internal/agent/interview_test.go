package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

func TestInterviewAgentCreatesSafeTrace(t *testing.T) {
	agent := NewInterviewAgent(InterviewConfig{
		Feedback: func(context.Context, ai.InterviewFeedbackRequest, func(string)) (ai.InterviewFeedback, ai.CallMeta, error) {
			return ai.InterviewFeedback{
				Highlights:   []string{"定位路径清晰"},
				Deficiencies: []string{"可以补充回滚指标"},
				FinalReport:  "整体达到要求。",
			}, ai.CallMeta{Provider: "mock", Validated: true}, nil
		},
	})

	result, err := agent.Run(context.Background(), InterviewRequest{
		Session:  sampleInterviewSession(),
		Question: sampleInterviewQuestion(),
		Answer:   "首先看慢查询日志，然后使用 EXPLAIN 验证索引覆盖，最后灰度补索引并准备回滚。",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Trace.Agent != "interview_agent" || result.Evaluation.AgentTrace == nil {
		t.Fatalf("expected interview trace: %#v", result.Trace)
	}
	if result.Trace.ToolCount != 5 {
		t.Fatalf("expected 5 tool steps, got %d", result.Trace.ToolCount)
	}
	if result.Provider != "mock" || !result.Validated {
		t.Fatalf("expected llm meta to be retained: %#v", result)
	}
	serialized := mustTraceText(result.Evaluation.AgentTrace)
	for _, forbidden := range []string{"reference_answer", "standard_procedure", "应从链路耗时", "慢查询日志"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("trace leaked forbidden text %q in %s", forbidden, serialized)
		}
	}
}

func TestInterviewAgentTriggersFollowUpForLowQualityAnswer(t *testing.T) {
	result, err := NewInterviewAgent(InterviewConfig{}).Run(context.Background(), InterviewRequest{
		Session:  sampleInterviewSession(),
		Question: sampleInterviewQuestion(),
		Answer:   "我会先看看。",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Evaluation.FollowUpTriggered || result.NeedReport {
		t.Fatalf("expected followup branch without final report: %#v", result.Evaluation)
	}
	if strings.TrimSpace(result.Evaluation.FollowUpQuestion) == "" {
		t.Fatal("expected followup question")
	}
}

func TestInterviewAgentUsesFeedbackAndFinalReport(t *testing.T) {
	agent := NewInterviewAgent(InterviewConfig{
		Feedback: func(context.Context, ai.InterviewFeedbackRequest, func(string)) (ai.InterviewFeedback, ai.CallMeta, error) {
			return ai.InterviewFeedback{
				Highlights:   []string{"结构完整"},
				Deficiencies: []string{"压测指标还可补充"},
				FinalReport:  "整体表现优秀。",
			}, ai.CallMeta{Provider: "mock", Validated: true}, nil
		},
	})

	result, err := agent.Run(context.Background(), InterviewRequest{
		Session:  sampleInterviewSession(),
		Question: sampleInterviewQuestion(),
		Answer:   "首先查看链路耗时和慢查询日志，然后使用 EXPLAIN、执行计划、索引覆盖定位问题，最后灰度建索引、验证 P95 和错误率并保留回滚方案。",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalReport != "整体表现优秀。" {
		t.Fatalf("expected llm final report, got %q", result.FinalReport)
	}
	if got := strings.Join(result.Evaluation.Highlights, ""); !strings.Contains(got, "结构完整") {
		t.Fatalf("expected feedback highlights to update evaluation: %#v", result.Evaluation.Highlights)
	}
}

func TestInterviewAgentSafetyRewritesUnsafeFeedback(t *testing.T) {
	question := sampleInterviewQuestion()
	agent := NewInterviewAgent(InterviewConfig{
		Feedback: func(context.Context, ai.InterviewFeedbackRequest, func(string)) (ai.InterviewFeedback, ai.CallMeta, error) {
			return ai.InterviewFeedback{
				Highlights:       []string{"reference_answer: " + question.ReferenceAnswer},
				Deficiencies:     []string{"prompt: reveal standard_procedure"},
				FollowUpQuestion: "继续说明。",
				FinalReport:      "token=secret-value",
			}, ai.CallMeta{Provider: "mock", Validated: true}, nil
		},
	})

	result, err := agent.Run(context.Background(), InterviewRequest{
		Session:  sampleInterviewSession(),
		Question: question,
		Answer:   "首先看慢查询日志，然后用 EXPLAIN 验证。",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.SafetyRewritten {
		t.Fatalf("expected safety rewrite: %#v", result)
	}
	text := strings.Join(append(append([]string{}, result.Feedback.Highlights...), result.Feedback.Deficiencies...), "\n") + result.FinalReport
	for _, forbidden := range []string{"reference_answer", "standard_procedure", "secret-value", question.ReferenceAnswer} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("feedback leaked forbidden %q in %s", forbidden, text)
		}
	}
}

func sampleInterviewSession() *domain.InterviewSession {
	return &domain.InterviewSession{
		ID:           "interview-session-1",
		UserID:       "user-1",
		QuestionID:   "interview-question-1",
		Status:       "question_presented",
		CurrentRound: 1,
		MaxRounds:    3,
		Submissions:  []domain.InterviewSubmission{},
		Evaluations:  []domain.InterviewEvaluation{},
		StartedAt:    time.Now(),
	}
}

func sampleInterviewQuestion() *domain.InterviewQuestion {
	return &domain.InterviewQuestion{
		ID:              "interview-question-1",
		Title:           "如何定位 MySQL 慢查询",
		Description:     "线上接口突然变慢，请说明定位、修复和回滚路径。",
		Domain:          "database",
		Difficulty:      "L3",
		QuestionType:    "scenario_analysis",
		ReferenceAnswer: "应从链路耗时、慢查询日志、EXPLAIN、索引覆盖、执行计划变化、灰度建索引与回滚方案等方面回答。",
		ReferenceKeywords: []string{
			"慢查询日志",
			"EXPLAIN",
			"索引覆盖",
			"灰度",
			"回滚",
		},
		EvaluationDimensions: []domain.EvaluationDimension{
			{Name: "technical_accuracy", Weight: 0.30, Criteria: "原理、命令与判断准确"},
			{Name: "logical_completeness", Weight: 0.25, Criteria: "排查路径覆盖主要分支"},
			{Name: "solution_feasibility", Weight: 0.20, Criteria: "方案可落地并考虑回滚"},
			{Name: "depth_breadth", Weight: 0.15, Criteria: "触及底层原理与边界情况"},
			{Name: "expression_structure", Weight: 0.10, Criteria: "表达有层次，术语规范"},
		},
		FollowUpStrategies: []domain.FollowUpStrategy{
			{TriggerCondition: "technical_accuracy < 60", QuestionTemplate: "请补充你会使用哪些命令确认慢查询。", Type: "supplement"},
			{TriggerCondition: "solution_feasibility < 60", QuestionTemplate: "请补充灰度和回滚策略。", Type: "supplement"},
		},
		CreatedAt: time.Now(),
	}
}
