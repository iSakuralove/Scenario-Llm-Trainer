package httpapi

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

type scenarioScoringInput struct {
	Question    *domain.ScenarioQuestion
	Messages    []domain.ScenarioMessage
	Answer      string
	RevealedIDs []string
	CurrentTurn int
	VectorStore store.VectorStore
}

func scoreScenarioWithEvidenceChain(input scenarioScoringInput) (*domain.ScenarioScore, *domain.ScenarioScoringReport, []string) {
	question := input.Question
	if question == nil {
		return &domain.ScenarioScore{}, &domain.ScenarioScoringReport{}, nil
	}
	root := ai.RootCauseMatch(input.Answer, question.Content.RootCause, question.Content.RootCauseKeywords)
	structuredMissing := scenarioCanonicalMissingDimensions(input.Answer, question.Content.HiddenWorld)
	root = scenarioStructuredRootMatchFloor(root, question.Content.HiddenWorld, structuredMissing)
	events := extractScenarioEvidenceEvents(input.Messages, input.Answer)
	docs := ai.BuildScenarioVectorDocuments(*question)
	matches, evidenceScore, procedureScore, distractorHits := scoreEvidenceEvents(events, docs)
	if vectorMatches, vectorEvidenceScore, vectorProcedureScore, vectorDistractorHits, ok := scoreEvidenceEventsWithVectorStore(context.Background(), input.VectorStore, question.ID, events, docs); ok {
		matches = dedupeMatchedDocuments(append(vectorMatches, matches...), 8)
		evidenceScore = max(evidenceScore, vectorEvidenceScore)
		procedureScore = max(procedureScore, vectorProcedureScore)
		distractorHits = max(distractorHits, vectorDistractorHits)
	}
	validClues := countValidRevealed(question.Content.RevealStrategy, input.RevealedIDs)
	totalValid := len(question.Content.RevealStrategy.SurfaceClues) + len(question.Content.RevealStrategy.DeepClues)
	clueUsage := 0
	if totalValid > 0 {
		clueUsage = validClues * 100 / totalValid
	}
	efficiency := 100 - max(0, input.CurrentTurn-8)*3
	if efficiency < 40 {
		efficiency = 40
	}
	reasoningDepth := reasoningDepthScore(events, evidenceScore, procedureScore)
	penalties := []string{}
	guessWithoutEvidence := root >= 60 && evidenceScore < 35
	if guessWithoutEvidence {
		penalties = append(penalties, "根因相似度较高，但会话中缺少可追溯证据链，按猜答案处理。")
	}
	if distractorHits > 0 {
		penalties = append(penalties, "排查过程中命中干扰路径，扣除部分过程分。")
	}
	accuracy := clampInt((root*65 + evidenceScore*20 + procedureScore*15) / 100)
	if guessWithoutEvidence && accuracy > 69 {
		accuracy = 69
	}
	if containsScenarioString(structuredMissing, "直接触发") {
		if accuracy > 59 {
			accuracy = 59
		}
		penalties = append(penalties, "结论未说明本次事故的直接触发变化，潜在问题不能代替完整事故结论。")
	} else if containsScenarioString(structuredMissing, "潜在问题") || containsScenarioString(structuredMissing, "现象解释") {
		if accuracy > 69 {
			accuracy = 69
		}
		penalties = append(penalties, "结论没有同时说明潜在问题与表层现象，因果链仍不完整。")
	}
	if containsScenarioString(structuredMissing, "衍生风险") {
		penalties = append(penalties, "结论未覆盖由超时和重试带来的业务风险。")
	}
	total := (efficiency*15 + accuracy*45 + clueUsage*15 + reasoningDepth*25) / 100
	if len(penalties) > 0 {
		total -= 10 + distractorHits*5
	}
	total = clampInt(total)
	score := &domain.ScenarioScore{
		Efficiency: efficiency,
		Accuracy:   accuracy,
		ClueUsage:  clueUsage,
		Total:      total,
	}
	report := &domain.ScenarioScoringReport{
		OverallScore:           total,
		RootCauseSimilarity:    root,
		EvidenceChainScore:     evidenceScore,
		ProcedureCoverageScore: procedureScore,
		ClueUsageScore:         clueUsage,
		ReasoningDepthScore:    reasoningDepth,
		EfficiencyScore:        efficiency,
		MatchedDocuments:       matches,
		EvidenceEvents:         events,
		Penalties:              penalties,
		ScoreExplanation:       scenarioScoreExplanation(root, evidenceScore, procedureScore, clueUsage, reasoningDepth, penalties),
	}
	missing := append(missingRootKeywords(input.Answer, question.Content.RootCauseKeywords), structuredMissing...)
	missing = uniqueScenarioStrings(missing, 6)
	return score, report, missing
}

func scenarioStructuredRootMatchFloor(current int, world *domain.HiddenWorld, missing []string) int {
	if world == nil || world.CanonicalAnswer == nil {
		return current
	}
	canonical := world.CanonicalAnswer
	criticalDimensions := 0
	if strings.TrimSpace(canonical.DirectTrigger) != "" {
		criticalDimensions++
	}
	if len(canonical.LatentIssues) > 0 {
		criticalDimensions++
	}
	if strings.TrimSpace(canonical.Phenomenon) != "" {
		criticalDimensions++
	}
	if criticalDimensions < 2 || containsScenarioString(missing, "直接触发") ||
		containsScenarioString(missing, "潜在问题") || containsScenarioString(missing, "现象解释") {
		return current
	}
	floor := 84
	if len(canonical.DerivedRisks) > 0 && !containsScenarioString(missing, "衍生风险") {
		floor = 90
	}
	if current < floor {
		return floor
	}
	return current
}

func scenarioCanonicalMissingDimensions(answer string, world *domain.HiddenWorld) []string {
	if world == nil || world.CanonicalAnswer == nil {
		return nil
	}
	canonical := world.CanonicalAnswer
	missing := []string{}
	if strings.TrimSpace(canonical.DirectTrigger) != "" && !scenarioCanonicalDimensionCovered(answer, canonical.DirectTrigger) {
		missing = append(missing, "直接触发")
	}
	if len(canonical.LatentIssues) > 0 && !scenarioCanonicalAnyDimensionCovered(answer, canonical.LatentIssues) {
		missing = append(missing, "潜在问题")
	}
	if strings.TrimSpace(canonical.Phenomenon) != "" && !scenarioCanonicalDimensionCovered(answer, canonical.Phenomenon) {
		missing = append(missing, "现象解释")
	}
	if len(canonical.DerivedRisks) > 0 && !scenarioCanonicalAnyDimensionCovered(answer, canonical.DerivedRisks) {
		missing = append(missing, "衍生风险")
	}
	return missing
}

func scenarioCanonicalAnyDimensionCovered(answer string, expected []string) bool {
	for _, item := range expected {
		if scenarioCanonicalDimensionCovered(answer, item) {
			return true
		}
	}
	return false
}

func scenarioCanonicalDimensionCovered(answer, expected string) bool {
	answer = strings.TrimSpace(answer)
	expected = strings.TrimSpace(expected)
	if answer == "" || expected == "" {
		return false
	}
	if strings.Contains(compactScenarioScoringText(answer), compactScenarioScoringText(expected)) {
		return true
	}
	semanticGroups := [][]string{
		{"网关", "gateway", "vip"},
		{"响应超时", "response_timeout", "timeout", "超时"},
		{"缩短", "改为", "改成", "调整", "变成", "→", "->", "由"},
		{"订单库", "数据库", "db", "mysql"},
		{"锁等待", "数据库锁", "db lock", "lock_wait", "lock wait", "锁"},
		{"长尾", "慢请求", "3～5", "3-5"},
		{"504", "gateway timeout"},
		{"nginx", "callback", "回调服务", "后端"},
		{"200", "稍后完成", "最终完成", "处理成功", "返回成功", "成功"},
		{"重试", "retry", "重复回调"},
		{"幂等", "idempotent", "idempotency", "重复处理"},
	}
	requiredGroups := 0
	matchedGroups := 0
	for _, group := range semanticGroups {
		if !containsScoringTerm(strings.ToLower(expected), group) {
			continue
		}
		requiredGroups++
		if containsScoringTerm(strings.ToLower(answer), group) {
			matchedGroups++
		}
	}
	if requiredGroups > 0 {
		return matchedGroups == requiredGroups
	}

	tokens := meaningfulVectorTokens(expected)
	if len(tokens) == 0 {
		return false
	}
	hits := 0
	for _, token := range tokens {
		if ai.ContainsAny(answer, []string{token}) {
			hits++
		}
	}
	return hits >= max(1, (len(tokens)+1)/2)
}

func compactScenarioScoringText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
}

func containsScenarioString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uniqueScenarioStrings(values []string, limit int) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

func extractScenarioEvidenceEvents(messages []domain.ScenarioMessage, answer string) []domain.ScenarioEvidenceEvent {
	events := []domain.ScenarioEvidenceEvent{}
	for _, message := range messages {
		text := strings.TrimSpace(message.UserContent)
		if text == "" {
			continue
		}
		events = append(events, domain.ScenarioEvidenceEvent{
			TurnNumber: message.TurnNumber,
			EventType:  classifyEvidenceEvent(text),
			Text:       text,
		})
	}
	if strings.TrimSpace(answer) != "" {
		events = append(events, domain.ScenarioEvidenceEvent{
			TurnNumber: len(messages) + 1,
			EventType:  "root_cause_claim",
			Text:       strings.TrimSpace(answer),
		})
	}
	return events
}

func classifyEvidenceEvent(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsScoringTerm(lower, []string{"根因", "导致", "因为", "所以", "判断", "是不是"}):
		return "root_cause_claim"
	case containsScoringTerm(lower, []string{"验证", "确认", "检查", "查看", "看", "核对", "explain", "日志", "指标", "监控"}):
		return "verification_action"
	case containsScoringTerm(lower, []string{"可能", "怀疑", "假设", "猜测"}):
		return "hypothesis"
	case containsScoringTerm(lower, []string{"test", "give me a line", "hello", "asdf"}):
		return "noise"
	default:
		return "observation_request"
	}
}

func scoreEvidenceEvents(events []domain.ScenarioEvidenceEvent, docs []ai.ScenarioVectorDocument) ([]domain.ScenarioMatchedDocument, int, int, int) {
	evidenceCoverage := map[string]float64{}
	procedureCoverage := map[string]float64{}
	matches := []domain.ScenarioMatchedDocument{}
	distractorHits := 0
	for eventIndex := range events {
		bestScore := 0.0
		var best ai.ScenarioVectorDocument
		for _, doc := range docs {
			if doc.DocType == ai.VectorDocProblemContext {
				continue
			}
			if events[eventIndex].TurnNumber == 0 || (events[eventIndex].EventType == "root_cause_claim" && eventIndex == len(events)-1) {
				if doc.DocType != ai.VectorDocRootCause {
					continue
				}
			}
			score := ai.Similarity(events[eventIndex].Text, doc.DocText)
			keywordHit := keywordVectorHit(events[eventIndex].Text, doc)
			if keywordHit && score < 0.82 {
				score = 0.82
			}
			if doc.DocType == ai.VectorDocDistractor && !keywordHit {
				score = 0
			}
			if score >= 0.35 {
				switch doc.DocType {
				case ai.VectorDocEvidence, ai.VectorDocClue:
					if doc.DocType == ai.VectorDocClue && doc.Metadata["is_distractor"] == "true" {
						distractorHits++
						continue
					}
					if score > evidenceCoverage[doc.DocKey] {
						evidenceCoverage[doc.DocKey] = score
					}
				case ai.VectorDocProcedureStep:
					if score > procedureCoverage[doc.DocKey] {
						procedureCoverage[doc.DocKey] = score
					}
				case ai.VectorDocDistractor:
					distractorHits++
				}
				matches = append(matches, domain.ScenarioMatchedDocument{
					DocType: doc.DocType,
					DocKey:  doc.DocKey,
					Snippet: truncateScoringSnippet(doc.DocText, 120),
					Score:   roundScore(score),
				})
			}
			if score > bestScore {
				bestScore = score
				best = doc
			}
		}
		events[eventIndex].Score = roundScore(bestScore)
		events[eventIndex].BestDocType = best.DocType
		events[eventIndex].BestDocKey = best.DocKey
	}
	return dedupeMatchedDocuments(matches, 8), coverageScore(evidenceCoverage, countDocs(docs, ai.VectorDocEvidence)+countDocs(docs, ai.VectorDocClue)), coverageScore(procedureCoverage, countDocs(docs, ai.VectorDocProcedureStep)), distractorHits
}

func scoreEvidenceEventsWithVectorStore(ctx context.Context, vectorStore store.VectorStore, questionID string, events []domain.ScenarioEvidenceEvent, docs []ai.ScenarioVectorDocument) ([]domain.ScenarioMatchedDocument, int, int, int, bool) {
	if vectorStore == nil || strings.TrimSpace(questionID) == "" {
		return nil, 0, 0, 0, false
	}
	evidenceCoverage := map[string]float64{}
	procedureCoverage := map[string]float64{}
	matches := []domain.ScenarioMatchedDocument{}
	distractorHits := 0
	usedVectorStore := false
	for eventIndex := range events {
		if strings.TrimSpace(events[eventIndex].Text) == "" {
			continue
		}
		results, err := vectorStore.Search(ctx, store.VectorSearchQuery{
			QuestionID: questionID,
			DocTypes:   scoringDocTypesForEvent(events[eventIndex], eventIndex, len(events)),
			Text:       events[eventIndex].Text,
			Limit:      16,
		})
		if err != nil {
			return nil, 0, 0, 0, false
		}
		if len(results) == 0 {
			continue
		}
		usedVectorStore = true
		bestScore := 0.0
		var best ai.ScenarioVectorDocument
		for _, result := range results {
			doc := result.Document
			if doc.DocType == ai.VectorDocProblemContext {
				continue
			}
			score := result.Score
			keywordHit := keywordVectorHit(events[eventIndex].Text, doc)
			if keywordHit && score < 0.82 {
				score = 0.82
			}
			if doc.DocType == ai.VectorDocDistractor && !keywordHit {
				score = 0
			}
			if score >= 0.35 {
				switch doc.DocType {
				case ai.VectorDocEvidence, ai.VectorDocClue:
					if doc.DocType == ai.VectorDocClue && doc.Metadata["is_distractor"] == "true" {
						distractorHits++
						continue
					}
					if score > evidenceCoverage[doc.DocKey] {
						evidenceCoverage[doc.DocKey] = score
					}
				case ai.VectorDocProcedureStep:
					if score > procedureCoverage[doc.DocKey] {
						procedureCoverage[doc.DocKey] = score
					}
				case ai.VectorDocDistractor:
					distractorHits++
				}
				matches = append(matches, domain.ScenarioMatchedDocument{
					DocType: doc.DocType,
					DocKey:  doc.DocKey,
					Snippet: truncateScoringSnippet(doc.DocText, 120),
					Score:   roundScore(score),
				})
			}
			if score > bestScore {
				bestScore = score
				best = doc
			}
		}
		events[eventIndex].Score = roundScore(bestScore)
		events[eventIndex].BestDocType = best.DocType
		events[eventIndex].BestDocKey = best.DocKey
	}
	if !usedVectorStore {
		return nil, 0, 0, 0, false
	}
	return dedupeMatchedDocuments(matches, 8), coverageScore(evidenceCoverage, countDocs(docs, ai.VectorDocEvidence)+countDocs(docs, ai.VectorDocClue)), coverageScore(procedureCoverage, countDocs(docs, ai.VectorDocProcedureStep)), distractorHits, true
}

func scoringDocTypesForEvent(event domain.ScenarioEvidenceEvent, eventIndex, totalEvents int) []string {
	if event.EventType == "root_cause_claim" && eventIndex == totalEvents-1 {
		return []string{ai.VectorDocRootCause}
	}
	return []string{ai.VectorDocRootCause, ai.VectorDocEvidence, ai.VectorDocProcedureStep, ai.VectorDocClue, ai.VectorDocDistractor}
}

func keywordVectorHit(text string, doc ai.ScenarioVectorDocument) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	lowerText := strings.ToLower(text)
	for _, token := range meaningfulVectorTokens(doc.DocText) {
		lowerToken := strings.ToLower(token)
		if strings.Contains(lowerText, lowerToken) {
			return true
		}
	}
	return false
}

func meaningfulVectorTokens(text string) []string {
	replacer := strings.NewReplacer(
		"：", " ", "，", " ", "。", " ", "、", " ", "；", " ", ";", " ", ",", " ", ".", " ",
		"(", " ", ")", " ", "（", " ", "）", " ", "[", " ", "]", " ", "【", " ", "】", " ",
		"+", " ", "-", " ", "=", " ",
	)
	ignored := map[string]bool{
		"题目": true, "描述": true, "领域": true, "难度": true, "类型": true, "标签": true, "根因": true, "关键词": true, "关键证据": true, "线索": true, "触发词": true, "推荐追问": true,
		"问题": true, "接口": true, "服务": true, "异常": true, "继续": true, "确认": true, "查看": true, "检查": true, "是否": true, "没有": true, "明显": true,
	}
	domainTerms := []string{
		"慢查询", "rows_examined", "EXPLAIN", "possible_keys", "type=ALL", "执行计划", "发布", "筛选条件", "status", "created_at", "索引", "联合索引", "全表扫描", "回表",
		"数据库阶段", "耗时", "回归", "网络", "DNS", "VIP", "健康检查", "跨机房", "CPU", "内存", "连接数", "缓存", "回源", "配置", "回滚",
	}
	tokens := []string{}
	lowerText := strings.ToLower(text)
	for _, term := range domainTerms {
		if strings.Contains(lowerText, strings.ToLower(term)) {
			tokens = append(tokens, term)
		}
	}
	for _, token := range strings.Fields(replacer.Replace(text)) {
		token = strings.TrimSpace(token)
		if len([]rune(token)) < 2 || ignored[token] {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func coverageScore(coverage map[string]float64, total int) int {
	if total <= 0 {
		return 0
	}
	sum := 0.0
	for _, score := range coverage {
		sum += math.Min(score, 1)
	}
	return clampInt(int(math.Round(sum / float64(total) * 100)))
}

func reasoningDepthScore(events []domain.ScenarioEvidenceEvent, evidenceScore, procedureScore int) int {
	types := map[string]bool{}
	for _, event := range events {
		if event.EventType != "noise" {
			types[event.EventType] = true
		}
	}
	score := len(types) * 18
	score += evidenceScore / 3
	score += procedureScore / 4
	return clampInt(score)
}

func missingRootKeywords(answer string, keywords []string) []string {
	missing := []string{}
	for _, keyword := range keywords {
		if !ai.ContainsAny(answer, []string{keyword}) {
			missing = append(missing, keyword)
		}
	}
	if len(missing) > 3 {
		missing = missing[:3]
	}
	return missing
}

func scenarioScoreExplanation(root, evidence, procedure, clue, depth int, penalties []string) string {
	parts := []string{fmt.Sprintf("根因相似度 %d，证据链 %d，步骤覆盖 %d，线索利用 %d，推理深度 %d。", root, evidence, procedure, clue, depth)}
	if len(penalties) > 0 {
		parts = append(parts, strings.Join(penalties, ""))
	}
	return strings.Join(parts, " ")
}

func containsScoringTerm(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func countDocs(docs []ai.ScenarioVectorDocument, docType string) int {
	count := 0
	for _, doc := range docs {
		if doc.DocType == docType {
			count++
		}
	}
	return count
}

func dedupeMatchedDocuments(matches []domain.ScenarioMatchedDocument, limit int) []domain.ScenarioMatchedDocument {
	seen := map[string]bool{}
	out := []domain.ScenarioMatchedDocument{}
	for _, match := range matches {
		key := match.DocType + "|" + match.DocKey
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, match)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func truncateScoringSnippet(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes-1]) + "..."
}

func roundScore(score float64) float64 {
	return math.Round(score*100) / 100
}

func clampInt(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
