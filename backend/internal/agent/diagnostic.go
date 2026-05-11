package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

type ReplyRewriteFunc func(context.Context, ai.ScenarioReplyRequest, func(string)) (string, ai.CallMeta, error)

type DiagnosticConfig struct {
	Rewrite      ReplyRewriteFunc
	SemanticGate *SemanticGate
	NewRunID     func() string
	Now          func() time.Time
}

type DiagnosticRequest struct {
	Session        *domain.ScenarioSession
	Question       *domain.ScenarioQuestion
	UserMessage    string
	Messages       []domain.ScenarioMessage
	RecentMessages []ai.ScenarioContextMessage
	SummaryBuilder func(string, *domain.ScenarioQuestion, []domain.ScenarioMessage) string
	OnStage        func(step, message string)
	OnDelta        func(string)
}

type DiagnosticResult struct {
	AssistantContent string
	Meta             domain.ResponseMeta
	Trace            domain.AgentTrace
}

type DiagnosticAgent struct {
	config DiagnosticConfig
}

func NewDiagnosticAgent(config DiagnosticConfig) *DiagnosticAgent {
	return &DiagnosticAgent{config: config}
}

func (a *DiagnosticAgent) Run(ctx context.Context, req DiagnosticRequest) (DiagnosticResult, error) {
	if req.Session == nil {
		return DiagnosticResult{}, fmt.Errorf("session is required")
	}
	question := req.Question
	if question == nil {
		question = &req.Session.QuestionSnapshot
	}
	if question == nil {
		return DiagnosticResult{}, fmt.Errorf("question is required")
	}
	content := strings.TrimSpace(req.UserMessage)
	if content == "" {
		return DiagnosticResult{}, fmt.Errorf("content is required")
	}
	runtime := NewRuntime(RuntimeConfig{
		Agent:          "diagnostic_agent",
		Mode:           "server_react",
		ForbiddenTerms: diagnosticForbiddenTerms(question),
		NewRunID:       a.config.NewRunID,
		Now:            a.config.Now,
	})
	state := diagnosticState{
		session:  req.Session,
		question: question,
		content:  content,
		meta:     domain.ResponseMeta{HintLevel: req.Session.HintLevel},
	}
	steps := []Step{
		a.inputQualityCheckStep(req, &state),
		a.agentRelevanceJudgeStep(req, &state),
		a.detectRootCauseLeakStep(req, &state),
		a.embeddingSimilarityMatchStep(req, &state),
		a.findTriggeredClueStep(req, &state),
		a.computeHintStep(req, &state),
		a.buildContextSummaryStep(req, &state),
		a.rewriteTeachingReplyStep(req, &state),
		a.safetyRewriteStep(req, &state),
	}
	trace, err := runtime.Execute(ctx, steps)
	state.meta.AgentTrace = &trace
	return DiagnosticResult{
		AssistantContent: state.assistantContent,
		Meta:             state.meta,
		Trace:            trace,
	}, err
}

func diagnosticForbiddenTerms(question *domain.ScenarioQuestion) []string {
	if question == nil {
		return nil
	}
	content := question.Content
	terms := []string{content.RootCause}
	terms = append(terms, content.RootCauseKeywords...)
	terms = append(terms, content.KeyEvidence...)
	terms = append(terms, content.StandardProcedure...)
	clues := []domain.Clue{}
	clues = append(clues, content.RevealStrategy.SurfaceClues...)
	clues = append(clues, content.RevealStrategy.DeepClues...)
	clues = append(clues, content.RevealStrategy.Distractors...)
	for _, clue := range clues {
		terms = append(terms, clue.Content, clue.RecommendedNextAsk)
	}
	return terms
}

type diagnosticState struct {
	session          *domain.ScenarioSession
	question         *domain.ScenarioQuestion
	content          string
	meta             domain.ResponseMeta
	assistantContent string
	decisionMade     bool
	semantic         SemanticDecision
}

func (a *DiagnosticAgent) inputQualityCheckStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "input_quality_check",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_input", "正在检查提问质量")
			quality := classifyInputQuality(state.content)
			state.meta.InputQuality = quality
			if quality == inputQualityNoise {
				state.meta.SemanticDecision = semanticDecisionRejectNoise
				state.meta.AgentIntent = agentIntentNoise
				state.meta.ResponseType = "redirect"
				state.assistantContent = "这条输入还不足以作为排查问题。请提出一个具体的观察点，例如日志、指标、变更、配置或影响范围。"
				state.decisionMade = true
				state.session.NoNewClueStreak++
				state.meta.HintLevel = state.session.HintLevel
				return ToolResult{
					Summary:  "识别为无意义或过短输入，拒绝释放线索",
					Metadata: map[string]string{"input_quality": quality, "decision": semanticDecisionRejectNoise},
				}, nil
			}
			return ToolResult{
				Summary:  "输入可进入语义判断",
				Metadata: map[string]string{"input_quality": quality},
			}, nil
		},
	}
}

func (a *DiagnosticAgent) agentRelevanceJudgeStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "agent_relevance_judge",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_intent", "正在判断提问是否属于有效排查")
			if state.decisionMade {
				return ToolResult{Summary: "已有输入质量决策，跳过相关性判断", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			intent := classifyAgentIntent(state.content)
			state.meta.AgentIntent = intent
			return ToolResult{
				Summary:  "已完成提问意图分类",
				Metadata: map[string]string{"agent_intent": intent},
			}, nil
		},
	}
}

func (a *DiagnosticAgent) detectRootCauseLeakStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "detect_root_cause_leak",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_policy", "正在检查是否会泄露根因")
			match := ai.RootCauseMatch(state.content, state.question.Content.RootCause, state.question.Content.RootCauseKeywords)
			if match >= 85 {
				state.meta.ResponseType = "insufficient"
				state.meta.IsAnswerLeak = true
				state.meta.IsSanitized = true
				state.meta.SemanticDecision = semanticDecisionBlockGuess
				state.meta.RootSimilarity = float64(match) / 100
				state.assistantContent = "目前信息尚不足以直接确认该结论。请先说明你的判断依据，并继续收集能支撑根因的证据。"
				state.session.NoNewClueStreak++
				state.decisionMade = true
				state.meta.HintLevel = state.session.HintLevel
				return ToolResult{
					Summary:  "已识别提前猜测结论，转为证据引导",
					Metadata: map[string]string{"decision": "answer_guess_blocked", "leak_detected": "true"},
				}, nil
			}
			return ToolResult{
				Summary:  "未发现直接结论泄露风险",
				Metadata: map[string]string{"leak_detected": "false"},
			}, nil
		},
	}
}

func (a *DiagnosticAgent) embeddingSimilarityMatchStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "embedding_similarity_match",
		Kind: "tool",
		Run: func(ctx context.Context, _ *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_embedding", "正在进行向量相似度匹配")
			if state.decisionMade {
				return ToolResult{Summary: "已有前置决策，跳过向量匹配", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			if a.config.SemanticGate == nil {
				return ToolResult{Summary: "未配置向量网关，继续使用关键词线索匹配", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			decision := a.config.SemanticGate.Evaluate(ctx, state.question, state.session, state.content)
			state.semantic = decision
			applySemanticMeta(&state.meta, decision)
			switch decision.Decision {
			case semanticDecisionBlockGuess:
				state.meta.ResponseType = "insufficient"
				state.meta.IsAnswerLeak = true
				state.meta.IsSanitized = true
				state.assistantContent = "目前信息尚不足以直接确认该结论。请先说明你的判断依据，并继续收集能支撑根因的证据。"
				state.session.NoNewClueStreak++
				state.decisionMade = true
			case semanticDecisionReleaseClue:
				state.releaseClue(decision.MatchedClue)
			case semanticDecisionGuidedRedirect:
				if clue, ok := nextSurfaceGuidanceClue(state.question.Content.RevealStrategy, state.session.RevealedClueIDs); ok {
					state.releaseClue(clue)
					state.meta.SemanticDecision = semanticDecisionGuidedRedirect
					state.meta.MatchedClueID = clue.ClueID
					state.assistantContent = "这个方向还需要更多证据支撑。先补一条基础观察：" + clue.Content
				}
			}
			metadata := map[string]string{
				"decision":        firstNonEmpty(decision.Decision, semanticDecisionNone),
				"input_quality":   decision.InputQuality,
				"agent_intent":    decision.AgentIntent,
				"root_similarity": formatScore(decision.RootSimilarity),
				"clue_similarity": formatScore(decision.ClueSimilarity),
			}
			if decision.MatchedClueID != "" {
				metadata["matched_clue_id"] = decision.MatchedClueID
			}
			if decision.EmbeddingModel != "" {
				metadata["embedding_model"] = decision.EmbeddingModel
				metadata["embedding_fallback_used"] = fmt.Sprintf("%t", decision.EmbeddingFallbackUsed)
			}
			if decision.FallbackUsed {
				metadata["local_fallback_used"] = "true"
			}
			return ToolResult{Summary: "已完成向量语义匹配", Metadata: metadata}, nil
		},
	}
}

func (a *DiagnosticAgent) findTriggeredClueStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "find_triggered_clue",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_clue", "正在匹配可释放线索")
			if state.decisionMade {
				return ToolResult{Summary: "已有安全策略决策，跳过线索释放", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			clue, found := ai.FindTriggeredClue(state.question.Content.RevealStrategy, state.content, state.session.RevealedClueIDs)
			if !found {
				return ToolResult{Summary: "未命中新线索", Metadata: map[string]string{"clue_found": "false"}}, nil
			}
			state.releaseClue(clue)
			if clue.IsDistractor {
				return ToolResult{
					Summary:  "命中干扰线索并引导排除",
					Metadata: map[string]string{"decision": "distractor", "clue_id": clue.ClueID},
				}, nil
			}
			return ToolResult{
				Summary:  "命中可释放线索",
				Metadata: map[string]string{"decision": "clue_revealed", "clue_id": clue.ClueID},
			}, nil
		},
	}
}

func (state *diagnosticState) releaseClue(clue domain.Clue) {
	if clue.ClueID == "" {
		return
	}
	state.session.RevealedClueIDs = append(state.session.RevealedClueIDs, clue.ClueID)
	state.session.NoNewClueStreak = 0
	state.meta.RevealedClueID = clue.ClueID
	state.meta.MatchedClueID = firstNonEmpty(state.meta.MatchedClueID, clue.ClueID)
	state.meta.IsDistractor = clue.IsDistractor
	state.meta.HintLevel = state.session.HintLevel
	state.decisionMade = true
	if clue.IsDistractor {
		state.meta.ResponseType = "redirect"
		state.assistantContent = "这个方向可以排除：" + clue.Content
		return
	}
	state.meta.ResponseType = "partial"
	state.assistantContent = "你抓住了关键因素，继续沿这个方向验证。你获得了一条有效线索：" + clue.Content
}

func applySemanticMeta(meta *domain.ResponseMeta, decision SemanticDecision) {
	if meta == nil {
		return
	}
	if decision.Decision != "" && decision.Decision != semanticDecisionNone {
		meta.SemanticDecision = decision.Decision
	}
	if decision.InputQuality != "" {
		meta.InputQuality = decision.InputQuality
	}
	if decision.AgentIntent != "" {
		meta.AgentIntent = decision.AgentIntent
	}
	if decision.RootSimilarity > 0 {
		meta.RootSimilarity = decision.RootSimilarity
	}
	if decision.ClueSimilarity > 0 {
		meta.ClueSimilarity = decision.ClueSimilarity
	}
	if decision.MatchedClueID != "" {
		meta.MatchedClueID = decision.MatchedClueID
	}
	if decision.EmbeddingModel != "" {
		meta.EmbeddingModel = decision.EmbeddingModel
		meta.EmbeddingFallbackUsed = decision.EmbeddingFallbackUsed
	}
}

func nextSurfaceGuidanceClue(strategy domain.RevealStrategy, revealed []string) (domain.Clue, bool) {
	revealedSet := map[string]bool{}
	for _, clueID := range revealed {
		revealedSet[clueID] = true
	}
	for _, clue := range strategy.SurfaceClues {
		if !revealedSet[clue.ClueID] {
			return clue, true
		}
	}
	return domain.Clue{}, false
}

func (a *DiagnosticAgent) computeHintStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "compute_hint",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_hint", "正在判断是否需要升级提示")
			if state.decisionMade {
				return ToolResult{Summary: "已有线索或安全决策，提示等级保持不变", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			state.session.NoNewClueStreak++
			state.meta.ResponseType = "redirect"
			hint := ai.NextHint(state.question.Content.RevealStrategy, state.session.RevealedClueIDs)
			if state.session.NoNewClueStreak >= 3 && state.session.HintLevel < 3 {
				state.session.HintLevel++
				state.session.NoNewClueStreak = 0
			}
			state.meta.HintLevel = state.session.HintLevel
			switch state.session.HintLevel {
			case 1:
				state.assistantContent = "暂未发现新的可释放线索。你可以换一个排查维度继续提问。"
			case 2:
				state.assistantContent = "暂未发现新的可释放线索。建议更具体地询问运行指标、日志或最近变更。"
			default:
				state.assistantContent = "方向提示：" + hint
			}
			state.decisionMade = true
			return ToolResult{
				Summary:  "未命中新线索，已更新提示等级",
				Metadata: map[string]string{"decision": "hint_redirect", "hint_level": fmt.Sprintf("%d", state.session.HintLevel)},
			}, nil
		},
	}
}

func (a *DiagnosticAgent) buildContextSummaryStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "build_context_summary",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			if state.session.CurrentTurn < 10 {
				return ToolResult{Summary: "当前轮次未达到上下文压缩阈值", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			if req.SummaryBuilder != nil {
				state.session.ConversationSummary = req.SummaryBuilder(state.session.ConversationSummary, state.question, req.Messages)
			} else {
				state.session.ConversationSummary = buildSafeConversationSummary(state.session.ConversationSummary, len(req.RecentMessages))
			}
			return ToolResult{Summary: "已生成安全上下文摘要", Metadata: map[string]string{"summary_updated": "true"}}, nil
		},
	}
}

func (a *DiagnosticAgent) rewriteTeachingReplyStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "rewrite_teaching_reply",
		Kind: "tool",
		Run: func(ctx context.Context, _ *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_reply", "正在生成教学化回复")
			if a.config.Rewrite == nil {
				return ToolResult{Summary: "未配置模型改写，使用确定性回复", Metadata: map[string]string{"provider": "none"}}, nil
			}
			rewriteReq := ai.ScenarioReplyRequest{
				QuestionTitle:       state.question.Title,
				UserMessage:         state.content,
				ResponseType:        state.meta.ResponseType,
				AllowedContent:      state.assistantContent,
				ForbiddenTerms:      diagnosticRouterForbiddenTerms(state.question),
				HintLevel:           state.session.HintLevel,
				IsDistractor:        state.meta.IsDistractor,
				IsAnswerLeak:        state.meta.IsAnswerLeak,
				ConversationSummary: state.session.ConversationSummary,
				RecentMessages:      req.RecentMessages,
			}
			content, llmMeta, err := a.config.Rewrite(ctx, rewriteReq, nil)
			if err != nil {
				return ToolResult{Summary: "模型改写失败，保留确定性回复"}, err
			}
			if strings.TrimSpace(content) != "" {
				state.assistantContent = content
			}
			state.meta.Provider = llmMeta.Provider
			state.meta.Validated = llmMeta.Validated
			state.meta.FallbackUsed = llmMeta.FallbackUsed
			state.meta.SafetyRewritten = llmMeta.SafetyRewritten
			state.meta.IsSanitized = state.meta.IsSanitized || llmMeta.SafetyRewritten
			return ToolResult{Summary: "回复已完成模型改写", Metadata: map[string]string{"provider": llmMeta.Provider}}, nil
		},
	}
}

func diagnosticRouterForbiddenTerms(question *domain.ScenarioQuestion) []string {
	if question == nil || strings.TrimSpace(question.Content.RootCause) == "" {
		return nil
	}
	return []string{question.Content.RootCause}
}

func (a *DiagnosticAgent) safetyRewriteStep(req DiagnosticRequest, state *diagnosticState) Step {
	return Step{
		Name: "safety_rewrite",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			forbidden := append([]string{state.question.Content.RootCause}, state.question.Content.StandardProcedure...)
			if safeContent, rewritten := ai.SafetyRewrite(state.assistantContent, forbidden); rewritten {
				state.assistantContent = safeContent
				state.meta.IsSanitized = true
				state.meta.SafetyRewritten = true
				return ToolResult{Summary: "回复触发安全重写", Metadata: map[string]string{"safety_rewritten": "true"}}, nil
			}
			return ToolResult{Summary: "回复通过安全检查", Metadata: map[string]string{"safety_rewritten": "false"}}, nil
		},
	}
}

func emitStage(onStage func(step, message string), step, message string) {
	if onStage != nil {
		onStage(step, message)
	}
}

func buildSafeConversationSummary(existing string, recentCount int) string {
	if strings.TrimSpace(existing) != "" {
		return strings.TrimSpace(existing) + "；已继续压缩最近对话上下文。"
	}
	if recentCount == 0 {
		return "已触发长对话上下文摘要。"
	}
	return fmt.Sprintf("已触发长对话上下文摘要，纳入最近 %d 条消息的安全摘要。", recentCount)
}
