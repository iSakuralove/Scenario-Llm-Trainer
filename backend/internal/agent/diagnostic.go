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
		context:  buildDiagnosticContext(content, question, req.Session, req.Messages),
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
	context          DiagnosticContext
	meta             domain.ResponseMeta
	assistantContent string
	decisionMade     bool
	semantic         SemanticDecision
}

type DiagnosticContext struct {
	LastIntent       string
	DiagnosticFocus  string
	RepeatStreak     int
	OffTrackStreak   int
	GuessStreak      int
	EvidenceCoverage int
	RepeatedTurn     int
}

func buildDiagnosticContext(content string, question *domain.ScenarioQuestion, session *domain.ScenarioSession, messages []domain.ScenarioMessage) DiagnosticContext {
	ctx := DiagnosticContext{
		LastIntent:       classifyAgentIntent(content),
		DiagnosticFocus:  diagnosticFocus(content),
		EvidenceCoverage: len(sessionRevealedIDs(session)),
	}
	for i := len(messages) - 1; i >= 0; i-- {
		previous := strings.TrimSpace(messages[i].UserContent)
		if previous == "" {
			continue
		}
		intent := classifyAgentIntent(previous)
		switch intent {
		case agentIntentOffTrack, agentIntentNoise, agentIntentChattyOffTopic:
			ctx.OffTrackStreak++
		case agentIntentAnswerGuess, agentIntentHypothesis:
			ctx.GuessStreak++
		}
		if ctx.RepeatedTurn == 0 && isRepeatedProbe(content, previous, question) {
			ctx.RepeatedTurn = messages[i].TurnNumber
			ctx.RepeatStreak++
		}
		if ctx.OffTrackStreak+ctx.GuessStreak+ctx.RepeatStreak == 0 {
			break
		}
	}
	if ctx.RepeatedTurn > 0 {
		ctx.LastIntent = agentIntentRepeatProbe
	}
	return ctx
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
				state.assistantContent = "要求用户改成一个可验证的排查动作，示例范围仅限日志、指标、变更、配置、影响范围。"
				state.decisionMade = true
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
			intent := state.context.LastIntent
			if intent == "" {
				intent = classifyAgentIntent(state.content)
			}
			state.meta.AgentIntent = intent
			return ToolResult{
				Summary:  "已完成提问意图分类",
				Metadata: map[string]string{"agent_intent": intent, "diagnostic_focus": state.context.DiagnosticFocus},
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
				state.assistantContent = "要求用户先补证据链，围绕日志、指标、变更或验证结果说明判断依据，不能确认根因。"
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
				state.assistantContent = "要求用户先补证据链，围绕日志、指标、变更或验证结果说明判断依据，不能确认根因。"
				state.decisionMade = true
			case semanticDecisionReleaseClue:
				state.releaseClue(decision.MatchedClue)
			case semanticDecisionGuidedRedirect:
				if clue, ok := nextSurfaceGuidanceClue(state.question.Content.RevealStrategy, state.session.RevealedClueIDs); ok {
					state.releaseClue(clue)
					state.meta.SemanticDecision = semanticDecisionGuidedRedirect
					state.meta.MatchedClueID = clue.ClueID
					state.assistantContent = "提示用户已接近关键线索，并释放一条基础观察：" + clue.Content
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
			switch state.meta.AgentIntent {
			case agentIntentRelevant, agentIntentEvidenceProbe, agentIntentBroadProbe:
			default:
				return ToolResult{Summary: "当前意图不允许直接释放线索", Metadata: map[string]string{"skipped": "true", "agent_intent": state.meta.AgentIntent}}, nil
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
		state.assistantContent = "说明该方向可先排除，并给出可排除观察：" + clue.Content
		return
	}
	state.meta.ResponseType = "partial"
	state.assistantContent = "说明用户命中了有效线索，并释放线索内容：" + clue.Content
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
		Name: "route_response_policy",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_policy", "正在选择排查引导策略")
			if state.decisionMade {
				return ToolResult{Summary: "已有线索或安全决策，提示等级保持不变", Metadata: map[string]string{"skipped": "true"}}, nil
			}
			state.meta.ResponseType = "redirect"
			decision := semanticDecisionGuidedRedirect
			action := "suggest_next_dimension"
			hint := ai.NextHint(state.question.Content.RevealStrategy, state.session.RevealedClueIDs)
			switch {
			case state.context.RepeatedTurn > 0 || state.meta.AgentIntent == agentIntentRepeatProbe:
				decision = semanticDecisionRepeatRedirect
				action = "repeat_redirect"
				state.assistantContent = "提醒该方向已经覆盖过，并建议换到配置、指标或依赖链路等新视角。"
			case state.meta.AgentIntent == agentIntentAnswerGuess || state.meta.AgentIntent == agentIntentHypothesis:
				decision = semanticDecisionAskEvidence
				action = "ask_for_evidence"
				state.meta.ResponseType = "insufficient"
				state.assistantContent = "要求用户补完整证据链，不能直接确认该判断。"
			case state.meta.AgentIntent == agentIntentChattyOffTopic:
				decision = semanticDecisionHumorousRedirect
				action = "humorous_redirect"
				state.assistantContent = "温和打断聊天式偏题，拉回主线，并要求给出可验证的排查观察点。"
			case state.meta.AgentIntent == agentIntentOffTrack:
				decision = semanticDecisionRequestRephrase
				action = "request_rephrase"
				state.assistantContent = "指出当前方向跑偏，要求重述为一个可验证的排查动作。"
			case state.meta.AgentIntent == agentIntentBroadProbe:
				decision = semanticDecisionNarrowScope
				action = "narrow_scope"
				state.session.NoNewClueStreak++
				if state.session.NoNewClueStreak >= 3 && state.session.HintLevel < 3 {
					state.session.HintLevel++
					state.session.NoNewClueStreak = 0
				}
				state.assistantContent = "认可方向基本合理，但要求把范围收窄到日志、指标、变更、配置或依赖链路中的一个具体观察点。"
			default:
				state.session.NoNewClueStreak++
				if state.session.NoNewClueStreak >= 3 && state.session.HintLevel < 3 {
					state.session.HintLevel++
					state.session.NoNewClueStreak = 0
				}
				switch state.session.HintLevel {
				case 1:
					state.assistantContent = "说明暂未解锁新线索，但鼓励继续从日志、指标、变更、配置或依赖链路等角度推进。"
				case 2:
					state.assistantContent = "说明仍未解锁新线索，并要求把排查动作说得更具体，例如哪类日志、哪个指标或哪次变更。"
				default:
					state.assistantContent = "给出一条方向提示：" + hint
				}
			}
			state.meta.HintLevel = state.session.HintLevel
			if decision != "" {
				state.meta.SemanticDecision = decision
			}
			state.decisionMade = true
			return ToolResult{
				Summary: "已根据排查行为选择内部引导策略",
				Metadata: map[string]string{
					"decision":         firstNonEmpty(decision, "hint_redirect"),
					"action":           action,
					"hint_level":       fmt.Sprintf("%d", state.session.HintLevel),
					"agent_intent":     state.meta.AgentIntent,
					"diagnostic_focus": state.context.DiagnosticFocus,
					"repeat_turn":      fmt.Sprintf("%d", state.context.RepeatedTurn),
				},
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
				DiagnosticIntent:    state.meta.AgentIntent,
				CoachingAction:      firstNonEmpty(state.meta.SemanticDecision, state.meta.ResponseType),
				DiagnosticFocus:     state.context.DiagnosticFocus,
				RepeatedWithTurn:    state.context.RepeatedTurn,
				ToneStyle:           "playful-guide",
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

func diagnosticFocus(input string) string {
	lower := strings.ToLower(input)
	switch {
	case containsAnyLower(lower, []string{"日志", "log", "slow log"}):
		return "logs"
	case containsAnyLower(lower, []string{"指标", "监控", "metric", "cpu", "内存", "延迟", "耗时"}):
		return "metrics"
	case containsAnyLower(lower, []string{"发布", "变更", "release", "deploy"}):
		return "changes"
	case containsAnyLower(lower, []string{"配置", "config"}):
		return "config"
	case containsAnyLower(lower, []string{"数据库", "连接", "sql", "explain", "慢查询"}):
		return "database"
	case containsAnyLower(lower, []string{"缓存", "cache"}):
		return "cache"
	case containsAnyLower(lower, []string{"网络", "依赖", "链路", "network"}):
		return "dependency"
	default:
		return "general"
	}
}

func isRepeatedProbe(current, previous string, question *domain.ScenarioQuestion) bool {
	current = strings.TrimSpace(current)
	previous = strings.TrimSpace(previous)
	if current == "" || previous == "" {
		return false
	}
	if ai.Similarity(current, previous) >= 0.62 {
		return true
	}
	currentLower := strings.ToLower(current)
	if containsAnyLower(currentLower, []string{"还是", "继续问", "刚才", "刚刚", "上一个", "同样", "依然"}) &&
		diagnosticFocus(current) != "general" &&
		diagnosticFocus(current) == diagnosticFocus(previous) {
		return true
	}
	if question != nil {
		for _, clue := range append(append(question.Content.RevealStrategy.SurfaceClues, question.Content.RevealStrategy.DeepClues...), question.Content.RevealStrategy.Distractors...) {
			if ai.ContainsAny(current, clue.TriggerKeywords) && ai.ContainsAny(previous, clue.TriggerKeywords) {
				return true
			}
		}
	}
	return false
}
