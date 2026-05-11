package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

const (
	semanticDecisionNone             = "none"
	semanticDecisionBlockGuess       = "block_guess"
	semanticDecisionReleaseClue      = "release_clue"
	semanticDecisionGuidedRedirect   = "guided_redirect"
	semanticDecisionRejectNoise      = "reject_noise"
	semanticDecisionAskEvidence      = "ask_for_evidence"
	semanticDecisionNarrowScope      = "narrow_scope"
	semanticDecisionRepeatRedirect   = "repeat_redirect"
	semanticDecisionRequestRephrase  = "request_rephrase"
	semanticDecisionHumorousRedirect = "humorous_redirect"

	inputQualityNoise  = "noise"
	inputQualityUsable = "usable"

	agentIntentNoise           = "noise"
	agentIntentRelevant        = "relevant"
	agentIntentOffTrack        = "off_track"
	agentIntentBroadProbe      = "broad_probe"
	agentIntentEvidenceProbe   = "evidence_probe"
	agentIntentHypothesis      = "hypothesis"
	agentIntentAnswerGuess     = "answer_guess"
	agentIntentRepeatProbe     = "repeat_probe"
	agentIntentFinalAnswerLike = "final_answer_like"
	agentIntentChattyOffTopic  = "chatty_off_topic"

	rootSimilarityThreshold = 0.86
	clueSimilarityThreshold = 0.78
)

var shortLatinCommandPattern = regexp.MustCompile(`(?i)^\s*(test|hello|hi|ok|give me a line|line|foo|bar|asdf|qwer|123|abc)\s*$`)

type SemanticGateConfig struct {
	Embedding               ai.EmbeddingClient
	RootSimilarityThreshold float64
	ClueSimilarityThreshold float64
}

type SemanticGate struct {
	config SemanticGateConfig
}

type SemanticDecision struct {
	Decision              string
	InputQuality          string
	AgentIntent           string
	RootSimilarity        float64
	ClueSimilarity        float64
	MatchedClue           domain.Clue
	MatchedClueID         string
	EmbeddingModel        string
	EmbeddingFallbackUsed bool
	FallbackUsed          bool
	Reason                string
}

type semanticCandidate struct {
	clue domain.Clue
	text string
}

func NewSemanticGate(config SemanticGateConfig) *SemanticGate {
	if config.RootSimilarityThreshold <= 0 {
		config.RootSimilarityThreshold = rootSimilarityThreshold
	}
	if config.ClueSimilarityThreshold <= 0 {
		config.ClueSimilarityThreshold = clueSimilarityThreshold
	}
	return &SemanticGate{config: config}
}

func (g *SemanticGate) Evaluate(ctx context.Context, question *domain.ScenarioQuestion, session *domain.ScenarioSession, input string) SemanticDecision {
	input = strings.TrimSpace(input)
	quality := classifyInputQuality(input)
	if quality == inputQualityNoise {
		return SemanticDecision{Decision: semanticDecisionRejectNoise, InputQuality: quality, AgentIntent: agentIntentNoise, Reason: "noise_input"}
	}
	intent := classifyAgentIntent(input)
	decision := SemanticDecision{Decision: semanticDecisionNone, InputQuality: quality, AgentIntent: intent}
	if question == nil {
		return decision
	}
	if localScore := ai.RootCauseMatch(input, question.Content.RootCause, question.Content.RootCauseKeywords); localScore >= 85 {
		decision.Decision = semanticDecisionBlockGuess
		decision.RootSimilarity = float64(localScore) / 100
		decision.Reason = "keyword_root_match"
		return decision
	}

	candidates := releasableSemanticCandidates(question.Content.RevealStrategy, sessionRevealedIDs(session))
	texts := []string{input, question.Content.RootCause}
	for _, candidate := range candidates {
		texts = append(texts, candidate.text)
	}
	if g != nil && g.config.Embedding != nil {
		result, err := g.config.Embedding.Embed(ctx, texts)
		if err == nil && len(result.Vectors) == len(texts) {
			decision.EmbeddingModel = result.Model
			decision.EmbeddingFallbackUsed = result.FallbackUsed
			decision.RootSimilarity = ai.CosineSimilarity(result.Vectors[0], result.Vectors[1])
			bestIndex, bestScore := bestClueVectorMatch(result.Vectors, candidates)
			decision.ClueSimilarity = bestScore
			if bestIndex >= 0 {
				decision.MatchedClue = candidates[bestIndex].clue
				decision.MatchedClueID = candidates[bestIndex].clue.ClueID
			}
			return g.decideFromScores(decision)
		}
		decision.FallbackUsed = true
	}

	decision.RootSimilarity = ai.Similarity(input, question.Content.RootCause)
	bestClue, bestScore := bestLocalClueMatch(input, candidates)
	decision.ClueSimilarity = bestScore
	if bestClue.ClueID != "" {
		decision.MatchedClue = bestClue
		decision.MatchedClueID = bestClue.ClueID
	}
	return g.decideFromScores(decision)
}

func (g *SemanticGate) decideFromScores(decision SemanticDecision) SemanticDecision {
	rootThreshold := rootSimilarityThreshold
	clueThreshold := clueSimilarityThreshold
	if g != nil {
		rootThreshold = g.config.RootSimilarityThreshold
		clueThreshold = g.config.ClueSimilarityThreshold
	}
	switch {
	case decision.RootSimilarity >= rootThreshold:
		decision.Decision = semanticDecisionBlockGuess
		decision.Reason = "embedding_root_match"
	case (decision.AgentIntent == agentIntentRelevant || decision.AgentIntent == agentIntentEvidenceProbe) &&
		decision.MatchedClueID != "" && decision.ClueSimilarity >= clueThreshold:
		decision.Decision = semanticDecisionReleaseClue
		decision.Reason = "embedding_clue_match"
	case (decision.AgentIntent == agentIntentRelevant || decision.AgentIntent == agentIntentBroadProbe) &&
		decision.MatchedClueID != "":
		decision.Decision = semanticDecisionGuidedRedirect
		decision.Reason = "frontier_guidance"
	case decision.AgentIntent == agentIntentRelevant || decision.AgentIntent == agentIntentBroadProbe:
		decision.Decision = semanticDecisionGuidedRedirect
		decision.Reason = "frontier_guidance"
	default:
		decision.Decision = semanticDecisionNone
	}
	return decision
}

func classifyInputQuality(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || len([]rune(trimmed)) < 2 {
		return inputQualityNoise
	}
	if shortLatinCommandPattern.MatchString(trimmed) {
		return inputQualityNoise
	}
	noiseTerms := []string{"哈哈", "呵呵", "随便", "给答案", "答案呢", "不知道", "无所谓", "乱问", "什么意思", "不懂", "不明白", "没明白", "看不懂", "听不懂", "说什么"}
	if containsAnyLower(trimmed, noiseTerms) && len([]rune(trimmed)) <= 12 {
		return inputQualityNoise
	}
	var letters, han, digits, symbols int
	for _, r := range trimmed {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
			letters++
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		case !unicode.IsSpace(r):
			symbols++
		}
	}
	if letters == 0 && digits+symbols > 0 {
		return inputQualityNoise
	}
	if han == 0 && letters > 0 && len(strings.Fields(trimmed)) <= 5 &&
		!containsAnyLower(trimmed, []string{"log", "metric", "database", "cache", "network", "cpu", "error", "latency", "timeout"}) {
		return inputQualityNoise
	}
	return inputQualityUsable
}

func classifyAgentIntent(input string) string {
	lower := strings.ToLower(input)
	if isChattyOffTopic(lower) {
		return agentIntentChattyOffTopic
	}
	if containsAnyLower(lower, []string{"重复", "还是", "继续问", "刚才", "刚刚", "上一个", "同样", "依然"}) {
		return agentIntentRepeatProbe
	}
	if containsAnyLower(lower, []string{"日志", "指标", "监控", "发布", "变更", "配置", "回滚", "数据库", "缓存", "网络", "cpu", "错误", "延迟", "连接", "队列", "告警", "影响", "链路", "slow log", "explain", "开始时间", "发布时间", "上线", "窗口"}) {
		if containsAnyLower(lower, []string{"具体", "哪条", "哪个", "哪一", "哪几个", "哪个日志", "哪个指标", "哪次变更", "哪条链路", "哪项配置", "是否重合"}) {
			return agentIntentEvidenceProbe
		}
		if containsAnyLower(lower, []string{"先看", "先从", "整体", "大概", "先检查", "先排查", "先确认", "访问路径", "趋势", "变化"}) {
			return agentIntentBroadProbe
		}
		return agentIntentRelevant
	}
	if containsAnyLower(lower, []string{"是不是", "是否是", "应该是", "就是", "根因是", "一定是", "肯定是"}) {
		return agentIntentAnswerGuess
	}
	if containsAnyLower(lower, []string{"我猜", "我怀疑", "推测", "假设", "可能是"}) {
		return agentIntentHypothesis
	}
	if strings.Contains(input, "排查") || strings.Contains(input, "分析") || strings.Contains(input, "确认") || strings.Contains(input, "看") || strings.Contains(input, "先") {
		if containsAnyLower(lower, []string{"整体", "先从", "先看", "先确认", "大概"}) {
			return agentIntentBroadProbe
		}
		return agentIntentOffTrack
	}
	return agentIntentOffTrack
}

func isChattyOffTopic(input string) bool {
	chattyTerms := []string{"哈哈", "笑死", "逗", "玩", "聊天", "别装", "给我线索", "不给线索", "openai", "gpt", "ai", "机器人", "对手"}
	if !containsAnyLower(input, chattyTerms) {
		return false
	}
	diagnosticTerms := []string{"日志", "指标", "监控", "发布", "变更", "配置", "回滚", "数据库", "缓存", "网络", "cpu", "错误", "延迟", "连接", "队列", "告警", "影响", "链路", "排查", "分析", "确认"}
	return !containsAnyLower(input, diagnosticTerms)
}

func containsAnyLower(input string, terms []string) bool {
	lower := strings.ToLower(input)
	for _, term := range terms {
		if strings.TrimSpace(term) != "" && strings.Contains(lower, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func releasableSemanticCandidates(strategy domain.RevealStrategy, revealed []string) []semanticCandidate {
	revealedSet := map[string]bool{}
	for _, clueID := range revealed {
		revealedSet[clueID] = true
	}
	candidates := []semanticCandidate{}
	appendCandidate := func(clue domain.Clue) {
		if revealedSet[clue.ClueID] {
			return
		}
		for _, prerequisite := range clue.PrerequisiteClues {
			if !revealedSet[prerequisite] {
				return
			}
		}
		text := strings.Join(append([]string{clue.Content, clue.RecommendedNextAsk}, clue.TriggerKeywords...), " ")
		candidates = append(candidates, semanticCandidate{clue: clue, text: text})
	}
	for _, clue := range strategy.SurfaceClues {
		appendCandidate(clue)
	}
	for _, clue := range strategy.DeepClues {
		appendCandidate(clue)
	}
	for _, clue := range strategy.Distractors {
		appendCandidate(clue)
	}
	return candidates
}

func bestClueVectorMatch(vectors [][]float64, candidates []semanticCandidate) (int, float64) {
	bestIndex := -1
	bestScore := 0.0
	for i := range candidates {
		vectorIndex := i + 2
		if vectorIndex >= len(vectors) {
			break
		}
		score := ai.CosineSimilarity(vectors[0], vectors[vectorIndex])
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}
	return bestIndex, bestScore
}

func bestLocalClueMatch(input string, candidates []semanticCandidate) (domain.Clue, float64) {
	var best domain.Clue
	bestScore := 0.0
	for _, candidate := range candidates {
		score := ai.Similarity(input, candidate.text)
		if ai.ContainsAny(input, candidate.clue.TriggerKeywords) && score < clueSimilarityThreshold {
			score = clueSimilarityThreshold
		}
		if score > bestScore {
			bestScore = score
			best = candidate.clue
		}
	}
	return best, bestScore
}

func sessionRevealedIDs(session *domain.ScenarioSession) []string {
	if session == nil {
		return nil
	}
	return session.RevealedClueIDs
}

func formatScore(score float64) string {
	return fmt.Sprintf("%.2f", score)
}
