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
type InterviewScoringFunc func(*domain.InterviewQuestion, string, int, int, string) domain.InterviewEvaluation
type InterviewRetrievalFunc func(context.Context, InterviewRetrievalRequest) (InterviewRetrievalResult, error)

type InterviewConfig struct {
	Feedback InterviewFeedbackFunc
	Score    InterviewScoringFunc
	Retrieve InterviewRetrievalFunc
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
	EndReason       string
	Trace           domain.AgentTrace
	Provider        string
	Validated       bool
	FallbackUsed    bool
	SafetyRewritten bool
}

type InterviewRetrievalRequest struct {
	Session    *domain.InterviewSession
	Question   *domain.InterviewQuestion
	Answer     string
	Evaluation domain.InterviewEvaluation
}

type InterviewRetrievalResult struct {
	FollowUpQuestion  string
	FollowUpType      string
	FollowUpSubject   string
	FallbackUsed      bool
	RetrievedSubjects []string
	MatchedAtoms      []domain.InterviewKnowledgeAtomLightSnapshot
	SummaryText       string
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
		a.retrieveFollowUpStep(req, &state),
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
		EndReason:       state.endReason,
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
	endReason   string
	meta        ai.CallMeta
	retrieval   InterviewRetrievalResult
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
			difficultyLevel := ""
			if state.session != nil {
				difficultyLevel = state.session.DifficultyLevel
			}
			state.evaluation = score(state.question, state.answer, state.round, state.maxRound, difficultyLevel)
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

func (a *InterviewAgent) retrieveFollowUpStep(req InterviewRequest, state *interviewState) Step {
	return Step{
		Name: "retrieve_followup_context",
		Kind: "tool",
		Run: func(ctx context.Context, _ *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_retrieval", "正在检索题库上下文并规划追问方向")
			if a.config.Retrieve == nil {
				state.retrieval = InterviewRetrievalResult{
					FallbackUsed: true,
					SummaryText:  "未配置题库检索，继续使用规则追问。",
				}
				return ToolResult{
					Summary: "未配置题库检索，已保持规则追问链路",
					Metadata: map[string]string{
						"fallback_used": "true",
					},
				}, nil
			}
			result, err := a.config.Retrieve(ctx, InterviewRetrievalRequest{
				Session:    state.session,
				Question:   state.question,
				Answer:     state.answer,
				Evaluation: state.evaluation,
			})
			if err != nil {
				state.retrieval = InterviewRetrievalResult{
					FallbackUsed: true,
					SummaryText:  "题库检索失败，继续使用规则追问。",
				}
				return ToolResult{
					Summary: "题库检索失败，已回退规则追问",
					Metadata: map[string]string{
						"fallback_used": "true",
					},
				}, nil
			}
			state.retrieval = result
			if state.retrieval.SummaryText == "" {
				state.retrieval.SummaryText = "已完成题库追问规划。"
			}
			return ToolResult{
				Summary: state.retrieval.SummaryText,
				Metadata: map[string]string{
					"fallback_used": fmt.Sprintf("%t", state.retrieval.FallbackUsed),
					"subject":       state.retrieval.FollowUpSubject,
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
			state.endReason = applyInterviewSmartClose(state)
			if state.evaluation.FollowUpTriggered && strings.TrimSpace(state.evaluation.FollowUpQuestion) == "" {
				state.evaluation.FollowUpQuestion = defaultSinglePointFollowUp(state.session)
				state.evaluation.FollowUpType = firstNonEmpty(state.evaluation.FollowUpType, "supplement")
			}
			state.evaluation.FollowUpSubject = state.retrieval.FollowUpSubject
			state.evaluation.FallbackUsed = state.retrieval.FallbackUsed
			state.evaluation.RetrievedSubjects = append([]string{}, state.retrieval.RetrievedSubjects...)
			if state.evaluation.FollowUpTriggered {
				if strings.TrimSpace(state.retrieval.FollowUpQuestion) != "" {
					state.evaluation.FollowUpQuestion = state.retrieval.FollowUpQuestion
				}
				if strings.TrimSpace(state.retrieval.FollowUpType) != "" {
					state.evaluation.FollowUpType = state.retrieval.FollowUpType
				}
				if len(state.retrieval.MatchedAtoms) > 0 {
					state.session.SelectedAtomSnapshots = append(state.session.SelectedAtomSnapshots, state.retrieval.MatchedAtoms...)
				}
			}
			state.needReport = !(state.evaluation.FollowUpTriggered && state.round < state.maxRound)
			if state.needReport && state.endReason == "" {
				if state.round >= state.maxRound {
					state.endReason = "max_rounds"
				} else {
					state.endReason = "completed"
				}
			}
			return ToolResult{
				Summary: "已确定本轮追问或报告分支",
				Metadata: map[string]string{
					"follow_up":   fmt.Sprintf("%t", state.evaluation.FollowUpTriggered),
					"need_report": fmt.Sprintf("%t", state.needReport),
					"end_reason":  state.endReason,
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
				Question:         state.question,
				Answer:           state.answer,
				Evaluation:       state.evaluation,
				NeedReport:       state.needReport,
				DifficultyLevel:  state.session.DifficultyLevel,
				FocusAreas:       append([]string{}, state.session.FocusAreas...),
				SetupNotes:       interviewSetupContext(state.session),
				RetrievalSummary: state.retrieval.SummaryText,
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

// interviewResumeContextLimit 限制每轮送进模型的简历字数。
// 简历原文会随选中份数线性增长，而它每一轮都要重发一次：
// 不截断既会推高每轮成本和延迟，也等于把完整个人信息反复交给第三方模型。
const interviewResumeContextLimit = 1200

func interviewSetupContext(session *domain.InterviewSession) string {
	if session == nil {
		return ""
	}
	parts := []string{strings.TrimSpace(session.SetupNotes)}
	if session.Mode == "resume_deep_dive" {
		if resume := truncateRunes(session.CandidateContext, interviewResumeContextLimit); resume != "" {
			parts = append(parts, "候选人简历上下文：\n"+resume)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func truncateRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…（简历较长，已截断）"
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

func EvaluateInterview(question *domain.InterviewQuestion, answer string, round, maxRounds int, difficultyLevel string) domain.InterviewEvaluation {
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
			followUpQuestion = singlePointFollowUpForCriteria(dimension.Criteria)
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
	// 仅 challenge 在首轮高分时加压；foundation/standard 保持循序渐进，不强行压边界题。
	if !followUpTriggered && total > 85 && round == 1 && round < maxRounds && strings.TrimSpace(difficultyLevel) == "challenge" {
		followUpTriggered = true
		followUpType = "pressure"
		followUpQuestion = "如果线上只给你 5 分钟恢复服务，你会先保留哪一个关键验证？"
	}
	// 第 1 轮默认不直接收束：答得尚可时也给一轮循序渐进深挖（除非已达上限）。
	if !followUpTriggered && round == 1 && round < maxRounds {
		followUpTriggered = true
		followUpType = "deepen"
		followUpQuestion = "基于你刚才说的，最关键的那个点你是怎么验证的？"
	}

	highlights := []string{"回答覆盖了核心定位方向。"}
	deficiencies := []string{}
	if dimensions["solution_feasibility"] < 70 {
		deficiencies = append(deficiencies, "落地方案还可以更具体。")
	}
	if dimensions["depth_breadth"] < 70 {
		deficiencies = append(deficiencies, "底层原理解释可以更深入。")
	}
	if len(deficiencies) == 0 {
		deficiencies = append(deficiencies, "可以再补一个可验证的观测指标。")
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

func applyInterviewSmartClose(state *interviewState) string {
	if state == nil {
		return ""
	}
	if state.round >= state.maxRound {
		state.evaluation.FollowUpTriggered = false
		return "max_rounds"
	}
	if state.session == nil || !state.session.SmartClose {
		return ""
	}
	// 连续低信息：当前 + 历史尾部连续低分/过短作答达到 2 轮则收束。
	if interviewLowInfoAnswer(state.answer, state.evaluation.TotalScore) {
		consecutive := 1
		for i := len(state.session.Evaluations) - 1; i >= 0; i-- {
			prev := state.session.Evaluations[i]
			if prev.TotalScore < 45 {
				consecutive++
				continue
			}
			break
		}
		if consecutive >= 2 {
			state.evaluation.FollowUpTriggered = false
			return "low_info"
		}
	}
	// 质量收束：至少完成 2 轮作答后，整体与各维均达标则提前结束（第 1 轮不因高分收束）。
	if state.round >= 2 && state.evaluation.TotalScore >= 80 && interviewMinDimensionScore(state.evaluation.DimensionScores) >= 55 {
		state.evaluation.FollowUpTriggered = false
		return "quality"
	}
	return ""
}

func InterviewEndNotice(reason string, completed, maxRounds int) map[string]string {
	message := ""
	switch strings.TrimSpace(reason) {
	case "low_info":
		message = "连续两轮回答信息偏少，本场已提前结束。请先看本轮反馈，随后进入报告页。"
	case "quality":
		message = "本场关键点已覆盖得比较充分，面试官决定提前收束。请先看本轮反馈，随后进入报告页。"
	case "max_rounds":
		message = fmt.Sprintf("已达到本场上限 %d 轮，即将进入报告页。", maxRounds)
	case "completed":
		message = "本场面试已结束，即将进入报告页。"
	default:
		if completed > 0 && maxRounds > 0 && completed < maxRounds {
			message = fmt.Sprintf("本场共完成 %d/%d 轮，已提前结束。请先看本轮反馈，随后进入报告页。", completed, maxRounds)
		} else {
			message = "本场面试已结束，即将进入报告页。"
		}
	}
	return map[string]string{
		"reason":  reason,
		"message": message,
	}
}

// interviewLowInfoAnswer 判定「信息偏少」：必须同时低分且过短。
// 只看长度会把简短但说到点子上的高分作答误判为敷衍，从而提前结束一场本该继续的面试。
func interviewLowInfoAnswer(answer string, totalScore int) bool {
	if totalScore >= 45 {
		return false
	}
	return len([]rune(strings.TrimSpace(answer))) < 80
}

func interviewMinDimensionScore(scores map[string]int) int {
	if len(scores) == 0 {
		return 0
	}
	minScore := 100
	for _, score := range scores {
		if score < minScore {
			minScore = score
		}
	}
	return minScore
}

func singlePointFollowUpForCriteria(criteria string) string {
	criteria = strings.TrimSpace(criteria)
	if criteria == "" {
		return "你刚才最关键的那个判断，依据是什么？"
	}
	// 取 criteria 第一分句，避免把整段检查清单塞进追问。
	for _, sep := range []string{"，", ",", "、", "；", ";"} {
		if idx := strings.Index(criteria, sep); idx > 0 {
			criteria = strings.TrimSpace(criteria[:idx])
			break
		}
	}
	return "能否再具体一点：" + criteria + "？"
}

func defaultSinglePointFollowUp(session *domain.InterviewSession) string {
	if session != nil && strings.TrimSpace(session.DifficultyLevel) == "challenge" {
		return "如果这里判断错了，线上最坏会怎样？"
	}
	return "你刚才最关键的那个判断，依据是什么？"
}

func AverageInterviewScore(evaluations []domain.InterviewEvaluation) int {
	if len(evaluations) == 0 {
		return 0
	}
	sum := 0
	for _, item := range evaluations {
		sum += item.TotalScore
	}
	return sum / len(evaluations)
}

func InterviewEarlyEndNote(completed, maxRounds int) string {
	if maxRounds <= 0 || completed <= 0 || completed >= maxRounds {
		return ""
	}
	return fmt.Sprintf("本场共完成 %d/%d 轮作答，已提前结束。", completed, maxRounds)
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
		feedback.Deficiencies = []string{"建议再补一个可验证的关键步骤。"}
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
