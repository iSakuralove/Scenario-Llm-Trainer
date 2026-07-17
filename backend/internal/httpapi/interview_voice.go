package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"strings"
	"unicode"
)

func writeInterviewValidationError(w http.ResponseWriter, validation domain.InterviewAnswerValidation) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(envelope{
		Code:    http.StatusUnprocessableEntity,
		Message: "invalid_interview_answer: " + validation.Message,
		Data:    validation,
	})
}
func validateInterviewAnswer(question *domain.InterviewQuestion, content, transcript string, asset *domain.Asset, sttConfidence float64) domain.InterviewAnswerValidation {
	answer := strings.TrimSpace(content)
	if answer == "" {
		answer = strings.TrimSpace(transcript)
	}
	quality := domain.VoiceQualityResult{
		DetectedLanguage:    detectAnswerLanguage(answer),
		STTConfidence:       sttConfidence,
		TopicRelevanceScore: interviewTopicRelevance(question, answer),
		KeywordHits:         interviewKeywordHits(question, answer),
		Reasons:             []string{},
		Status:              "draft_ready",
	}
	if quality.STTConfidence <= 0 {
		quality.STTConfidence = 0.9
	}
	if asset != nil {
		if err := validateVoiceAsset(asset.Filename, asset.MimeType, asset.Size); err != nil {
			quality.Status = "rejected"
			quality.Reasons = append(quality.Reasons, err.Error())
			return domain.InterviewAnswerValidation{Valid: false, Message: "语音文件类型无效，请重新上传音频文件", Quality: quality}
		}
	}
	if len([]rune(answer)) < 12 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "转写内容过短")
		return domain.InterviewAnswerValidation{Valid: false, Message: "转写内容过短，请重新上传或改为文本回答", Quality: quality}
	}
	if quality.STTConfidence < 0.45 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "语音识别置信度过低")
		return domain.InterviewAnswerValidation{Valid: false, Message: "语音识别置信度过低，请重新上传或改为文本回答", Quality: quality}
	}
	if quality.TopicRelevanceScore < 25 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "转写内容与本题相关性不足")
		return domain.InterviewAnswerValidation{Valid: false, Message: "转写内容与本题相关性不足，请重新上传或改为文本回答", Quality: quality}
	}
	if quality.DetectedLanguage == "en" && len(quality.KeywordHits) < 2 && quality.TopicRelevanceScore < 50 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "英文内容未覆盖本题关键技术点")
		return domain.InterviewAnswerValidation{Valid: false, Message: "英文内容未覆盖本题关键技术点，请使用中文结构化回答", Quality: quality}
	}
	if quality.DetectedLanguage == "en" {
		quality.Status = "needs_review"
		quality.Reasons = append(quality.Reasons, "检测到英文为主，建议补充中文说明")
	}
	return domain.InterviewAnswerValidation{Valid: true, Quality: quality}
}
func detectAnswerLanguage(value string) string {
	var han, latin int
	for _, r := range value {
		switch {
		case r >= '\u4e00' && r <= '\u9fff':
			han++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			latin++
		}
	}
	switch {
	case han == 0 && latin == 0:
		return "unknown"
	case han == 0 && latin > 0:
		return "en"
	case latin > han*4:
		return "en"
	case han > 0:
		return "zh"
	default:
		return "mixed"
	}
}
func defaultSTTLanguage(language, transcript string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	switch language {
	case "zh", "zh-cn", "chinese", "cn":
		return "zh"
	case "en", "english":
		return "en"
	case "":
		return detectAnswerLanguage(transcript)
	default:
		return language
	}
}
func interviewTopicRelevance(question *domain.InterviewQuestion, answer string) int {
	if question == nil {
		return 0
	}
	score := ai.RootCauseMatch(answer, strings.Join([]string{question.Title, question.Description, question.ReferenceAnswer}, "\n"), question.ReferenceKeywords)
	// 用实质关键词，避免仅命中 domain（闲聊复述“数据库面试题”）被抬到通过线。
	if hits := len(interviewSubstantialKeywordHits(question, answer)); hits > 0 {
		bonus := 10 + hits*8
		if score < bonus {
			score = bonus
		}
	}
	// 中文作答与题面短语重叠时补充相关度，避免只靠英文/词面相似度。
	if overlap := interviewChinesePhraseOverlap(question, answer); overlap > 0 {
		bonus := 12 + overlap*10
		if score < bonus {
			score = bonus
		}
	}
	if interviewLooksLikeTechnicalAnswer(answer) {
		if score < 18 {
			score = 18
		}
	}
	if score > 100 {
		return 100
	}
	return score
}
func interviewKeywordHits(question *domain.InterviewQuestion, answer string) []string {
	if question == nil {
		return []string{}
	}
	hits := []string{}
	seen := map[string]bool{}
	keywords := interviewTerminologyLexicon(question)
	for _, keyword := range keywords {
		normalized := strings.TrimSpace(keyword)
		if normalized == "" || seen[strings.ToLower(normalized)] {
			continue
		}
		if ai.ContainsAny(answer, []string{normalized}) {
			seen[strings.ToLower(normalized)] = true
			hits = append(hits, normalized)
		}
	}
	return hits
}

type terminologyCandidate struct {
	Canonical string
	Aliases   []string
}

func interviewSTTLanguageHint(question *domain.InterviewQuestion) string {
	if question == nil {
		return "zh"
	}
	if ai.ContainsAny(question.Title+" "+question.Description, []string{"English", "英文", "英语"}) {
		return "en"
	}
	return "zh"
}
func buildInterviewSTTPrompt(question *domain.InterviewQuestion) string {
	terms := interviewTerminologyLexicon(question)
	if len(terms) == 0 {
		return ""
	}
	preview := strings.Join(terms, ", ")
	runes := []rune(preview)
	if len(runes) > 220 {
		preview = string(runes[:220])
	}
	return "这是技术面试语音转写，请优先保留专业术语原文，不要翻译成中文谐音。重点术语包括：" + preview
}
func interviewTerminologyLexicon(question *domain.InterviewQuestion) []string {
	seen := map[string]bool{}
	terms := []string{}
	appendTerm := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		terms = append(terms, value)
	}
	for _, value := range []string{"MySQL", "nginx", "Nginx", "EXPLAIN", "SQL", "索引", "慢查询", "执行计划", "回滚", "灰度", "slow log"} {
		appendTerm(value)
	}
	// 跨领域通用术语：追问轮次常围绕命令/指标/回滚，而不是开场题原文关键词。
	for _, value := range []string{
		"load", "load average", "CPU", "vmstat", "iostat", "ps", "top", "IO wait", "D 状态", "不可中断",
		"DNS", "VIP", "负载均衡", "健康检查", "连接池", "超时", "降级", "重试",
		"AK/SK", "密钥", "轮换", "最小权限", "访问日志", "影响面",
		"发布", "流水线", "回滚策略", "灰度发布",
		"定位", "排查", "验证", "命令", "指标", "日志", "监控", "限流", "熔断",
	} {
		appendTerm(value)
	}
	if question != nil {
		appendTerm(question.Domain)
		for _, keyword := range question.ReferenceKeywords {
			appendTerm(keyword)
		}
		for _, value := range splitTerminologyText(question.Title + " " + question.Description + " " + question.ReferenceAnswer) {
			appendTerm(value)
		}
	}
	return terms
}
func splitTerminologyText(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= 'A' && r <= 'Z':
			return false
		case r >= '0' && r <= '9':
			return false
		case r == '+' || r == '#' || r == '-' || r == '_':
			return false
		default:
			return true
		}
	})
	out := []string{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) >= 3 {
			out = append(out, field)
		}
	}
	return out
}

// interviewLooksLikeTechnicalAnswer 识别明显的技术排查/方案结构，避免认真中文作答被判离题。
func interviewLooksLikeTechnicalAnswer(answer string) bool {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return false
	}
	signals := []string{
		"排查", "定位", "验证", "命令", "指标", "日志", "监控", "回滚", "灰度", "限流", "熔断",
		"超时", "重试", "降级", "索引", "执行计划", "慢查询", "连接池", "负载", "健康检查",
		"密钥", "权限", "发布", "流水线", "CPU", "IO", "DNS", "VIP", "EXPLAIN", "SQL",
		"vmstat", "iostat", "top", "ps", "load", "MySQL", "nginx",
	}
	hits := 0
	for _, signal := range signals {
		if ai.ContainsAny(trimmed, []string{signal}) {
			hits++
		}
	}
	// 同时命中 2 个以上技术结构词，或单条很长且含 1 个结构词，视为认真技术作答。
	if hits >= 2 {
		return true
	}
	return hits >= 1 && len([]rune(trimmed)) >= 120
}

// interviewSubstantialKeywordHits 过滤弱命中：单独的 domain 或过短词不计入“相关”。
func interviewSubstantialKeywordHits(question *domain.InterviewQuestion, answer string) []string {
	hits := interviewKeywordHits(question, answer)
	if len(hits) == 0 {
		return hits
	}
	domain := ""
	if question != nil {
		domain = strings.ToLower(strings.TrimSpace(question.Domain))
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		normalized := strings.TrimSpace(hit)
		if normalized == "" {
			continue
		}
		lower := strings.ToLower(normalized)
		if domain != "" && lower == domain {
			continue
		}
		// 过短英文/数字弱命中（如 "ps"/"io"）单独出现时不够证明相关，需配合技术结构判断。
		if len([]rune(normalized)) < 2 {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// interviewChinesePhraseOverlap 统计作答与题面中文短语重叠数。
func interviewChinesePhraseOverlap(question *domain.InterviewQuestion, answer string) int {
	if question == nil {
		return 0
	}
	answerPhrases := map[string]bool{}
	for _, phrase := range chinesePhrases(answer) {
		// 仅 4 字及以上，避免“旅行计划”误命中“执行计划”的 3-gram 片段。
		if len([]rune(phrase)) < 4 {
			continue
		}
		answerPhrases[phrase] = true
	}
	if len(answerPhrases) == 0 {
		return 0
	}
	source := question.Title + " " + question.Description + " " + question.ReferenceAnswer + " " + strings.Join(question.ReferenceKeywords, " ")
	overlap := 0
	seen := map[string]bool{}
	for _, phrase := range chinesePhrases(source) {
		if len([]rune(phrase)) < 4 {
			continue
		}
		if !answerPhrases[phrase] || seen[phrase] {
			continue
		}
		seen[phrase] = true
		overlap++
	}
	return overlap
}

// chinesePhrases 从文本中切出 2-4 字中文短语，用于题面与作答的弱匹配。
func chinesePhrases(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	appendPhrase := func(phrase string) {
		if seen[phrase] {
			return
		}
		seen[phrase] = true
		out = append(out, phrase)
	}
	// 连续汉字切段后做 3-4-gram，降低短片段误匹配。
	var buf []rune
	flush := func() {
		if len(buf) < 3 {
			buf = buf[:0]
			return
		}
		for n := 3; n <= 4; n++ {
			if len(buf) < n {
				break
			}
			for i := 0; i+n <= len(buf); i++ {
				appendPhrase(string(buf[i : i+n]))
			}
		}
		buf = buf[:0]
	}
	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			buf = append(buf, r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func detectInterviewTermSuggestions(question *domain.InterviewQuestion, transcript string) []domain.TranscriptSuggestion {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil
	}
	candidates := interviewTerminologyCandidates(question)
	suggestions := []domain.TranscriptSuggestion{}
	seen := map[string]bool{}
	lowerTranscript := strings.ToLower(transcript)
	for _, candidate := range candidates {
		if ai.ContainsAny(transcript, []string{candidate.Canonical}) {
			continue
		}
		for _, alias := range candidate.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" || !strings.Contains(lowerTranscript, strings.ToLower(alias)) {
				continue
			}
			key := strings.ToLower(candidate.Canonical) + "|" + strings.ToLower(alias)
			if seen[key] {
				continue
			}
			seen[key] = true
			suggestions = append(suggestions, domain.TranscriptSuggestion{
				Original:  alias,
				Suggested: candidate.Canonical,
				Reason:    "检测到可能的中文谐音或拆写术语",
			})
			break
		}
	}
	return suggestions
}
func interviewTerminologyCandidates(question *domain.InterviewQuestion) []terminologyCandidate {
	candidates := []terminologyCandidate{
		{Canonical: "nginx", Aliases: []string{"恩金克斯", "恩静克斯", "engine x", "enginex"}},
		{Canonical: "MySQL", Aliases: []string{"买SQL", "买sql", "my sql", "麦SQL", "mysql"}},
		{Canonical: "EXPLAIN", Aliases: []string{"explain", "xplain", "解释计划"}},
	}
	lexicon := interviewTerminologyLexicon(question)
	if ai.ContainsAny(strings.Join(lexicon, " "), []string{"Redis"}) {
		candidates = append(candidates, terminologyCandidate{Canonical: "Redis", Aliases: []string{"瑞迪斯", "redis"}})
	}
	return candidates
}
func inferSubmissionSource(content, transcript string) string {
	content = strings.TrimSpace(content)
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return "text"
	}
	if content == "" || content == transcript || ai.Similarity(content, transcript) >= 0.82 {
		return "voice_transcript"
	}
	return "voice_edited"
}
func mockVoiceTranscriptDraft(asset *domain.Asset, session *domain.InterviewSession) string {
	filename := "语音答案"
	if asset != nil && strings.TrimSpace(asset.Filename) != "" {
		filename = asset.Filename
	}
	round := 1
	if session != nil && session.CurrentRound > 0 {
		round = session.CurrentRound
	}
	return fmt.Sprintf("第 %d 轮 %s 转写草稿：我会先定位 MySQL 慢查询，查看 slow log 和 EXPLAIN 执行计划，核对索引覆盖情况，再给出灰度修复、回滚和验证方案。", round, filename)
}
