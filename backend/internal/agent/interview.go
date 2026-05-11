package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

type InterviewFeedbackFunc func(context.Context, ai.InterviewFeedbackRequest, func(string)) (ai.InterviewFeedback, ai.CallMeta, error)
type InterviewScoringFunc func(*domain.InterviewQuestion, string, int, int) domain.InterviewEvaluation

type InterviewConfig struct {
	Feedback InterviewFeedbackFunc
	Score    InterviewScoringFunc
	NewRunID func() string
	Now      func() time.Time
}

type InterviewRequest struct {
	Session  *domain.InterviewSession
	Question *domain.InterviewQuestion
	Answer   string
	OnStage  func(step, message string)
	OnDelta  func(string)
}

type InterviewResult struct {
	Evaluation      domain.InterviewEvaluation
	Feedback        ai.InterviewFeedback
	FinalReport     string
	NeedReport      bool
	Trace           domain.AgentTrace
	Provider        string
	Validated       bool
	FallbackUsed    bool
	SafetyRewritten bool
}

type InterviewAgent struct {
	config InterviewConfig
}

func NewInterviewAgent(config InterviewConfig) *InterviewAgent {
	return &InterviewAgent{config: config}
}

func (a *InterviewAgent) Run(ctx context.Context, req InterviewRequest) (InterviewResult, error) {
	if req.Session == nil {
		return InterviewResult{}, fmt.Errorf("session is required")
	}
	if req.Question == nil {
		return InterviewResult{}, fmt.Errorf("question is required")
	}
	answer := strings.TrimSpace(req.Answer)
	if answer == "" {
		return InterviewResult{}, fmt.Errorf("answer is required")
	}
	runtime := NewRuntime(RuntimeConfig{
		Agent:          "interview_agent",
		Mode:           "server_react",
		ForbiddenTerms: interviewForbiddenTraceTerms(req.Question),
		NewRunID:       a.config.NewRunID,
		Now:            a.config.Now,
	})
	state := interviewState{
		session:  req.Session,
		question: req.Question,
		answer:   answer,
		round:    req.Session.CurrentRound,
		maxRound: req.Session.MaxRounds,
	}
	if state.round <= 0 {
		state.round = len(req.Session.Evaluations) + 1
	}
	if state.maxRound <= 0 {
		state.maxRound = 1
	}
	steps := []Step{
		a.analyzeAnswerIntentStep(req, &state),
		a.evaluateDimensionsStep(req, &state),
		a.decideFollowUpStep(req, &state),
		a.generateFeedbackStep(req, &state),
		a.safetyRewriteStep(req, &state),
	}
	trace, err := runtime.Execute(ctx, steps)
	state.evaluation.AgentTrace = &trace
	return InterviewResult{
		Evaluation:      state.evaluation,
		Feedback:        state.feedback,
		FinalReport:     state.finalReport,
		NeedReport:      state.needReport,
		Trace:           trace,
		Provider:        state.meta.Provider,
		Validated:       state.meta.Validated,
		FallbackUsed:    state.meta.FallbackUsed,
		SafetyRewritten: state.meta.SafetyRewritten,
	}, err
}

type interviewState struct {
	session     *domain.InterviewSession
	question    *domain.InterviewQuestion
	answer      string
	round       int
	maxRound    int
	evaluation  domain.InterviewEvaluation
	feedback    ai.InterviewFeedback
	finalReport string
	needReport  bool
	meta        ai.CallMeta
}

func (a *InterviewAgent) analyzeAnswerIntentStep(req InterviewRequest, state *interviewState) Step {
	return Step{
		Name: "analyze_answer_intent",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_intent", "正在分析你的作答意图")
			return ToolResult{
				Summary: "已确认作答文本可进入面试评分",
				Metadata: map[string]string{
					"round":     fmt.Sprintf("%d", state.round),
					"max_round": fmt.Sprintf("%d", state.maxRound),
				},
			}, nil
		},
	}
}

func (a *InterviewAgent) evaluateDimensionsStep(req InterviewRequest, state *interviewState) Step {
	return Step{
		Name: "evaluate_dimensions",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_eval", "正在执行评分维度检查")
			score := a.config.Score
			if score == nil {
				score = EvaluateInterview
			}
			state.evaluation = score(state.question, state.answer, state.round, state.maxRound)
			if state.evaluation.CreatedAt.IsZero() {
				state.evaluation.CreatedAt = interviewNow(a.config.Now)
			}
			return ToolResult{
				Summary: "已完成五维评分与通过判断",
				Metadata: map[string]string{
					"total_score": fmt.Sprintf("%d", state.evaluation.TotalScore),
					"is_passed":   fmt.Sprintf("%t", state.evaluation.IsPassed),
				},
			}, nil
		},
	}
}

func (a *InterviewAgent) decideFollowUpStep(req InterviewRequest, state *interviewState) Step {
	return Step{
		Name: "decide_follow_up",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_followup", "正在判断是否需要追问")
			if state.evaluation.FollowUpTriggered && strings.TrimSpace(state.evaluation.FollowUpQuestion) == "" {
				state.evaluation.FollowUpQuestion = "请补充说明你的关键判断依据、验证路径和风险控制。"
				state.evaluation.FollowUpType = firstNonEmpty(state.evaluation.FollowUpType, "supplement")
			}
			state.needReport = !(state.evaluation.FollowUpTriggered && state.round < state.maxRound)
			return ToolResult{
				Summary: "已确定本轮追问或报告分支",
				Metadata: map[string]string{
					"follow_up":   fmt.Sprintf("%t", state.evaluation.FollowUpTriggered),
					"need_report": fmt.Sprintf("%t", state.needReport),
				},
			}, nil
		},
	}
}

func (a *InterviewAgent) generateFeedbackStep(req InterviewRequest, state *interviewState) Step {
	return Step{
		Name: "generate_feedback",
		Kind: "tool",
		Run: func(ctx context.Context, _ *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_reply", "正在生成面试反馈")
			feedback := defaultInterviewFeedback(state.evaluation, state.needReport)
			meta := ai.CallMeta{Provider: "deterministic", Validated: true}
			if a.config.Feedback == nil {
				state.meta = meta
				state.feedback = feedback
				state.finalReport = feedback.FinalReport
				return ToolResult{Summary: "未配置模型反馈，已使用确定性反馈", Metadata: map[string]string{"provider": meta.Provider}}, nil
			}
			llmFeedback, llmMeta, err := a.config.Feedback(ctx, ai.InterviewFeedbackRequest{
				Question:   state.question,
				Answer:     state.answer,
				Evaluation: state.evaluation,
				NeedReport: state.needReport,
			}, nil)
			if err != nil {
				meta.FallbackUsed = true
				state.meta = meta
				state.feedback = feedback
				state.finalReport = feedback.FinalReport
				return ToolResult{Summary: "模型反馈失败，已回退为确定性反馈", Metadata: map[string]string{"fallback_used": "true"}}, nil
			}
			state.meta = llmMeta
			state.feedback = mergeInterviewFeedback(state.evaluation, llmFeedback, state.needReport)
			state.finalReport = state.feedback.FinalReport
			return ToolResult{
				Summary:  "面试反馈已完成模型改写",
				Metadata: map[string]string{"provider": firstNonEmpty(llmMeta.Provider, "unknown")},
			}, nil
		},
	}
}

func (a *InterviewAgent) safetyRewriteStep(req InterviewRequest, state *interviewState) Step {
	return Step{
		Name: "safety_rewrite",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_safety", "正在检查反馈安全性")
			combined := strings.Join([]string{
				strings.Join(state.feedback.Highlights, "\n"),
				strings.Join(state.feedback.Deficiencies, "\n"),
				state.feedback.FollowUpQuestion,
				state.feedback.FinalReport,
			}, "\n")
			_, rewritten := ai.SafetyRewrite(combined, interviewForbiddenFeedbackTerms(state.question))
			if rewritten || containsInterviewInternalTerm(combined) {
				state.feedback = defaultInterviewFeedback(state.evaluation, state.needReport)
				state.feedback.Deficiencies = append([]string{"反馈中包含不适合直接展示的内容，已改为安全摘要。"}, state.evaluation.Deficiencies...)
				state.meta.SafetyRewritten = true
				applyFeedbackToEvaluation(&state.evaluation, state.feedback, state.needReport)
				state.finalReport = state.feedback.FinalReport
				return ToolResult{Summary: "反馈触发安全重写，已替换为安全摘要", Metadata: map[string]string{"safety_rewritten": "true"}}, nil
			}
			applyFeedbackToEvaluation(&state.evaluation, state.feedback, state.needReport)
			state.finalReport = state.feedback.FinalReport
			return ToolResult{Summary: "反馈通过安全检查", Metadata: map[string]string{"safety_rewritten": "false"}}, nil
		},
	}
}

func EvaluateInterview(question *domain.InterviewQuestion, answer string, round, maxRounds int) domain.InterviewEvaluation {
	if question == nil {
		question = &domain.InterviewQuestion{}
	}
	keywordScore := ai.RootCauseMatch(answer, question.ReferenceAnswer, question.ReferenceKeywords)
	if keywordScore < 35 && len(strings.TrimSpace(answer)) > 80 {
		keywordScore = 45
	}
	dimensions := map[string]int{
		"technical_accuracy":   clampInt(keywordScore+5, 0, 100),
		"logical_completeness": clampInt(keywordScore+scoreIfInterview(answer, []string{"首先", "然后", "最后", "定位", "验证", "对比"}, 18), 0, 100),
		"solution_feasibility": clampInt(keywordScore+scoreIfInterview(answer, []string{"灰度", "回滚", "降级", "恢复", "验证"}, 18), 0, 100),
		"depth_breadth":        clampInt(keywordScore+scoreIfInterview(answer, []string{"原理", "机制", "底层", "执行计划", "缓存", "内核"}, 16), 0, 100),
		"expression_structure": clampInt(50+minInt(len([]rune(answer))/10, 40), 0, 100),
	}
	total := 0
	for _, dimension := range question.EvaluationDimensions {
		total += int(float64(dimensions[dimension.Name]) * dimension.Weight)
	}
	if total == 0 {
		total = keywordScore
	}
	followUpTriggered := false
	followUpQuestion := ""
	followUpType := ""
	for _, dimension := range question.EvaluationDimensions {
		if dimension.Weight > 0.2 && dimensions[dimension.Name] < 60 && round < maxRounds {
			followUpTriggered = true
			followUpType = "supplement"
			followUpQuestion = "请补充说明：" + dimension.Criteria
			for _, strategy := range question.FollowUpStrategies {
				if strings.Contains(strategy.TriggerCondition, dimension.Name) {
					followUpQuestion = strategy.QuestionTemplate
					followUpType = strategy.Type
					break
				}
			}
			break
		}
	}
	if !followUpTriggered && total > 85 && round == 1 && round < maxRounds {
		followUpTriggered = true
		followUpType = "pressure"
		followUpQuestion = "如果线上只给你 5 分钟恢复服务，你会删减哪些步骤并保留哪些关键验证？"
	}

	highlights := []string{"回答覆盖了核心定位方向。"}
	deficiencies := []string{}
	if dimensions["solution_feasibility"] < 70 {
		deficiencies = append(deficiencies, "落地方案与回滚策略还不充分。")
	}
	if dimensions["depth_breadth"] < 70 {
		deficiencies = append(deficiencies, "底层原理解释可以更深入。")
	}
	if len(deficiencies) == 0 {
		deficiencies = append(deficiencies, "可以进一步量化验证指标。")
	}

	return domain.InterviewEvaluation{
		Round:             round,
		TotalScore:        total,
		DimensionScores:   dimensions,
		IsPassed:          total >= 60,
		Highlights:        highlights,
		Deficiencies:      deficiencies,
		FollowUpTriggered: followUpTriggered,
		FollowUpQuestion:  followUpQuestion,
		FollowUpType:      followUpType,
		CreatedAt:         time.Now(),
	}
}

func defaultInterviewFeedback(evaluation domain.InterviewEvaluation, needReport bool) ai.InterviewFeedback {
	feedback := ai.InterviewFeedback{
		Highlights:       append([]string{}, evaluation.Highlights...),
		Deficiencies:     append([]string{}, evaluation.Deficiencies...),
		FollowUpQuestion: evaluation.FollowUpQuestion,
	}
	if len(feedback.Highlights) == 0 {
		feedback.Highlights = []string{"本轮回答已完成基础评估。"}
	}
	if len(feedback.Deficiencies) == 0 {
		feedback.Deficiencies = []string{"建议继续补充关键依据、验证路径和风险控制。"}
	}
	if needReport {
		feedback.FinalReport = ai.DefaultInterviewReport(evaluation)
	}
	return feedback
}

func mergeInterviewFeedback(evaluation domain.InterviewEvaluation, feedback ai.InterviewFeedback, needReport bool) ai.InterviewFeedback {
	if len(feedback.Highlights) == 0 {
		feedback.Highlights = append([]string{}, evaluation.Highlights...)
	}
	if len(feedback.Deficiencies) == 0 {
		feedback.Deficiencies = append([]string{}, evaluation.Deficiencies...)
	}
	if evaluation.FollowUpTriggered && strings.TrimSpace(feedback.FollowUpQuestion) == "" {
		feedback.FollowUpQuestion = evaluation.FollowUpQuestion
	}
	if needReport && strings.TrimSpace(feedback.FinalReport) == "" {
		feedback.FinalReport = ai.DefaultInterviewReport(evaluation)
	}
	return feedback
}

func applyFeedbackToEvaluation(evaluation *domain.InterviewEvaluation, feedback ai.InterviewFeedback, needReport bool) {
	if evaluation == nil {
		return
	}
	if len(feedback.Highlights) > 0 {
		evaluation.Highlights = append([]string{}, feedback.Highlights...)
	}
	if len(feedback.Deficiencies) > 0 {
		evaluation.Deficiencies = append([]string{}, feedback.Deficiencies...)
	}
	if evaluation.FollowUpTriggered && strings.TrimSpace(feedback.FollowUpQuestion) != "" {
		evaluation.FollowUpQuestion = feedback.FollowUpQuestion
	}
	if needReport && strings.TrimSpace(feedback.FinalReport) == "" {
		feedback.FinalReport = ai.DefaultInterviewReport(*evaluation)
	}
}

func interviewForbiddenTraceTerms(question *domain.InterviewQuestion) []string {
	if question == nil {
		return nil
	}
	terms := []string{question.ReferenceAnswer, "reference_answer", "standard_procedure", "prompt"}
	terms = append(terms, question.ReferenceKeywords...)
	return terms
}

func interviewForbiddenFeedbackTerms(question *domain.InterviewQuestion) []string {
	if question == nil {
		return []string{"reference_answer", "standard_procedure", "prompt"}
	}
	return []string{question.ReferenceAnswer, "reference_answer", "standard_procedure", "prompt"}
}

func containsInterviewInternalTerm(text string) bool {
	normalized := strings.ToLower(text)
	for _, term := range []string{"reference_answer", "standard_procedure", "tool_args", "tool_call", "prompt:", "system prompt", "api_key", "token="} {
		if strings.Contains(normalized, term) {
			return true
		}
	}
	return false
}

func interviewNow(now func() time.Time) time.Time {
	if now != nil {
		return now()
	}
	return time.Now()
}

func scoreIfInterview(text string, keywords []string, score int) int {
	for _, keyword := range keywords {
		if strings.Contains(strings.ToLower(text), strings.ToLower(keyword)) {
			return score
		}
	}
	return 0
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
