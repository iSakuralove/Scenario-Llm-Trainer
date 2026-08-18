package httpapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

var interviewFocusAreaLabels = map[string]string{
	"technical_accuracy":   "技术准确性",
	"logical_completeness": "逻辑完整性",
	"solution_feasibility": "方案可落地性",
	"depth_breadth":        "深度与广度",
	"expression_structure": "表达结构",
}

var interviewFocusAreaOrder = []string{
	"technical_accuracy",
	"logical_completeness",
	"solution_feasibility",
	"depth_breadth",
	"expression_structure",
}

func normalizeInterviewFocusAreas(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if !validInterviewFocusArea(value) {
			return nil, fmt.Errorf("focus_areas contains unsupported value: %s", value)
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, nil
}

// normalizeInterviewMaxRounds: 0/缺省 → 3；合法范围 1–15。
func normalizeInterviewMaxRounds(value int) (int, error) {
	if value == 0 {
		return 3, nil
	}
	if value < 1 || value > 15 {
		return 0, fmt.Errorf("max_rounds must be between 1 and 15")
	}
	return value, nil
}

func validInterviewFocusArea(value string) bool {
	_, ok := interviewFocusAreaLabels[strings.TrimSpace(value)]
	return ok
}

func interviewFocusAreaLabel(value string) string {
	if label, ok := interviewFocusAreaLabels[strings.TrimSpace(value)]; ok {
		return label
	}
	return value
}

func interviewFocusAreaLabelsText(values []string) string {
	labels := []string{}
	for _, value := range values {
		if validInterviewFocusArea(value) {
			labels = append(labels, interviewFocusAreaLabel(value))
		}
	}
	return strings.Join(labels, "、")
}

func interviewLaunchpadDomainLabel(value string) string {
	switch strings.TrimSpace(value) {
	case "java":
		return "Java"
	case "database":
		return "数据库"
	case "cache":
		return "缓存"
	case "middleware":
		return "中间件"
	case "system_design":
		return "系统设计"
	case "frontend":
		return "前端"
	case "ai_llm":
		return "AI / LLM"
	case "hr_soft_skill":
		return "HR 软技能"
	default:
		return value
	}
}

// openingStemForAtom 是知识原子题干的唯一口径：启动台卡片、按 ID 开场、
// 按领域开场都必须经过它，否则同一道题在三处会显示三个不同的问法。
// 人工精编的 OpeningQuestion 永远优先。
func openingStemForAtom(atom domain.InterviewKnowledgeAtom) string {
	subject := firstNonEmpty(atom.Subject, atom.Title, atom.Category, atom.Domain)
	if opening := strings.TrimSpace(atom.OpeningQuestion); opening != "" {
		return ensureSinglePointOpeningDescription(opening, subject)
	}
	source := firstNonEmpty(strings.TrimSpace(atom.Title), strings.Join(atom.Principles, "；"), subject)
	return ensureSinglePointOpeningDescription(source, subject)
}

func (s *Server) selectInterviewOpeningQuestion(domainName, difficulty, questionType string) (*domain.InterviewQuestion, domain.InterviewQuestionSnapshot, bool) {
	if atom, ok := s.selectInterviewOpeningAtom(domainName, difficulty); ok {
		question := interviewQuestionFromAtom(*atom, questionType)
		question.Description = s.resolveOpeningDescription(atom.ID, openingStemForAtom(*atom), firstNonEmpty(atom.Subject, atom.Title, atom.Category), domainName)
		snapshot := interviewQuestionSnapshotFromAtom(*atom, question.QuestionType)
		snapshot.Description = question.Description
		return question, snapshot, true
	}
	question, ok := s.store.FindInterviewQuestion(domainName, difficulty, questionType)
	if !ok {
		return nil, domain.InterviewQuestionSnapshot{}, false
	}
	normalized := *question
	subject := firstNonEmpty(question.Title, question.Domain)
	normalized.Description = s.resolveOpeningDescription(question.ID, question.Description, subject, domainName)
	return &normalized, interviewQuestionSnapshotFromQuestion(&normalized), true
}

func (s *Server) selectInterviewOpeningQuestionByID(questionID string) (*domain.InterviewQuestion, domain.InterviewQuestionSnapshot, bool) {
	questionID = strings.TrimSpace(questionID)
	if questionID == "" {
		return nil, domain.InterviewQuestionSnapshot{}, false
	}
	if atom, ok := s.store.GetInterviewKnowledgeAtom(questionID); ok && atom.Status == "published" && (atom.QuestionRole == "opening" || atom.QuestionRole == "mixed") {
		question := interviewQuestionFromAtom(*atom, normalizeLaunchpadQuestionType(atom.QuestionType))
		question.Description = s.resolveOpeningDescription(atom.ID, openingStemForAtom(*atom), firstNonEmpty(atom.Subject, atom.Title), firstNonEmpty(atom.Category, atom.Domain))
		snapshot := interviewQuestionSnapshotFromAtom(*atom, question.QuestionType)
		snapshot.Description = question.Description
		return question, snapshot, true
	}
	question, ok := s.store.GetInterviewQuestion(questionID)
	if !ok {
		return nil, domain.InterviewQuestionSnapshot{}, false
	}
	normalized := *question
	normalized.Description = s.resolveOpeningDescription(question.ID, normalized.Description, firstNonEmpty(normalized.Title, normalized.Domain), normalized.Domain)
	return &normalized, interviewQuestionSnapshotFromQuestion(&normalized), true
}

// openingRewriteTimeout 卡在「开始面试」这个同步动作上，宁可退回规则改写也不让用户干等。
const openingRewriteTimeout = 4 * time.Second

// resolveOpeningDescription:
//  1. 合格单点题干原样（或轻量补问号），不触碰模型
//  2. 空/复合题干才走 LLM 重写，并按题目 ID 缓存
//  3. LLM 失败或仍复合 → 规则单点规范化
//
// 缓存同时保证同一道题每次进来的题干稳定：开场题会写进会话快照，
// 每次都换一个问法会让历史记录和报告对不上。
func (s *Server) resolveOpeningDescription(cacheKey, sourceText, subject, domainName string) string {
	sourceText = strings.TrimSpace(sourceText)
	subject = strings.TrimSpace(subject)
	ruleBased := ensureSinglePointOpeningDescription(sourceText, subject)
	if sourceText != "" && !looksLikeMultiPointOpening(sourceText) {
		return ruleBased
	}
	if s == nil || s.llmRouter() == nil {
		return ruleBased
	}
	cacheKey = strings.TrimSpace(cacheKey)
	if cached, ok := s.cachedOpeningDescription(cacheKey); ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(context.Background(), openingRewriteTimeout)
	defer cancel()
	out, _, err := s.llmRouter().RewriteInterviewOpening(ctx, ai.InterviewOpeningRequest{
		Subject:    firstNonEmpty(subject, domainName, "当前问题"),
		Domain:     strings.TrimSpace(domainName),
		SourceText: firstNonEmpty(sourceText, subject),
	})
	if err != nil {
		return ruleBased
	}
	opening := strings.TrimSpace(out.Opening)
	if opening == "" || looksLikeMultiPointOpening(opening) {
		return ruleBased
	}
	// 仍要求像问句，否则回退
	if !strings.Contains(opening, "？") && !strings.Contains(opening, "?") && !strings.Contains(opening, "请") {
		return ruleBased
	}
	s.storeOpeningDescription(cacheKey, opening)
	return opening
}

func (s *Server) cachedOpeningDescription(key string) (string, bool) {
	if s == nil || key == "" {
		return "", false
	}
	s.openingStemMu.RLock()
	defer s.openingStemMu.RUnlock()
	value, ok := s.openingStemCache[key]
	return value, ok
}

func (s *Server) storeOpeningDescription(key, value string) {
	if s == nil || key == "" || value == "" {
		return
	}
	s.openingStemMu.Lock()
	defer s.openingStemMu.Unlock()
	if s.openingStemCache == nil {
		s.openingStemCache = map[string]string{}
	}
	s.openingStemCache[key] = value
}

// ensureSinglePointOpeningDescription 保证开场对候选人只暴露一个主问。
// 合格题干原样返回；空/复合题干用规则改写，不调用 LLM。
func ensureSinglePointOpeningDescription(description, subject string) string {
	description = strings.TrimSpace(description)
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "当前问题"
	}
	if description == "" {
		return defaultSinglePointOpening(subject)
	}
	if !looksLikeMultiPointOpening(description) {
		if !strings.Contains(description, "？") && !strings.Contains(description, "?") && !strings.Contains(description, "请") {
			return fmt.Sprintf("%s。你第一步会怎么做？", strings.TrimRight(description, "。.!！"))
		}
		return description
	}
	scenario := extractOpeningScenario(description)
	if scenario == "" {
		scenario = fmt.Sprintf("先看一个场景：与「%s」相关", subject)
	}
	return fmt.Sprintf("%s。你第一步会先看什么？", strings.TrimRight(scenario, "。.!！？?"))
}

func defaultSinglePointOpening(subject string) string {
	return fmt.Sprintf("先看一个场景：你需要处理与「%s」相关的线上问题。你第一步会先确认什么？", subject)
}

// 面试官一次只问一个点。以下三类词用于判断题干是否在一句里索要多个交付物：
// 索取动词（请说明/请给出…）、交付名词（排查顺序/回滚策略/修复方案…）、并列连接（、和以及）。
var (
	openingRequestVerbs = []string{"请说明", "请给出", "请描述", "请分析", "请阐述", "请回答", "请列出", "谈谈你的"}
	openingDeliverables = []string{
		"排查顺序", "定位路径", "排查路径", "分析路径", "处理路径", "恢复路径",
		"回滚策略", "回滚方案", "回滚考虑", "修复方案", "改进方案", "处理策略", "补偿策略",
		"关键命令", "验证命令", "验证路径", "验证指标", "风险控制", "风险点", "适用边界",
		"可能原因", "判断依据", "监控方案", "演进路径",
	}
)

// looksLikeMultiPointOpening 判断题干是否把多个交付物压在一问里。
// 用结构化启发式而不是固定文案清单：题库来自外部导入，写死的句子撑不住真实数据。
func looksLikeMultiPointOpening(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	deliverables := 0
	for _, item := range openingDeliverables {
		if strings.Contains(normalized, item) {
			deliverables++
		}
	}
	// 两个以上交付名词同时出现，本身就是三件套题干。
	if deliverables >= 2 {
		return true
	}
	hasVerb := false
	for _, verb := range openingRequestVerbs {
		if strings.Contains(normalized, verb) {
			hasVerb = true
			break
		}
	}
	enumerations := strings.Count(normalized, "、")
	// 「请说明 A、B、C」：索取动词 + 至少两个顿号并列。
	if hasVerb && enumerations >= 2 {
		return true
	}
	// 「请说明 A 和 B」：索取动词 + 一个交付名词 + 并列连接。
	if hasVerb && deliverables >= 1 && (strings.Contains(normalized, "和") || strings.Contains(normalized, "以及") || strings.Contains(normalized, "并")) {
		return true
	}
	if enumerations >= 3 && (strings.Contains(normalized, "和") || strings.Contains(normalized, "以及")) {
		return true
	}
	return false
}

func extractOpeningScenario(text string) string {
	text = strings.TrimSpace(text)
	cutMarkers := []string{
		"请说明", "请给出", "请回答", "你会如何", "你会怎样", "请先",
	}
	for _, marker := range cutMarkers {
		if idx := strings.Index(text, marker); idx > 0 {
			return strings.TrimSpace(text[:idx])
		}
	}
	// 取第一句作为情景
	for _, sep := range []string{"。", "！", "？", "\n"} {
		if idx := strings.Index(text, sep); idx > 0 {
			return strings.TrimSpace(text[:idx])
		}
	}
	return ""
}

func (s *Server) selectInterviewOpeningAtom(domainName, difficulty string) (*domain.InterviewKnowledgeAtom, bool) {
	candidates := s.openingAtomsByFilter(domain.InterviewKnowledgeAtomFilter{
		Status:     "published",
		Category:   domainName,
		Difficulty: difficulty,
	})
	if len(candidates) == 0 {
		candidates = s.openingAtomsByFilter(domain.InterviewKnowledgeAtomFilter{
			Status:     "published",
			Domain:     domainName,
			Difficulty: difficulty,
		})
	}
	if len(candidates) == 0 {
		return nil, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		leftIndexed := candidates[i].VectorStatus == "indexed"
		rightIndexed := candidates[j].VectorStatus == "indexed"
		if leftIndexed != rightIndexed {
			return leftIndexed
		}
		leftOpening := candidates[i].QuestionRole == "opening"
		rightOpening := candidates[j].QuestionRole == "opening"
		if leftOpening != rightOpening {
			return leftOpening
		}
		if candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})
	return &candidates[0], true
}

func (s *Server) openingAtomsByFilter(filter domain.InterviewKnowledgeAtomFilter) []domain.InterviewKnowledgeAtom {
	items := s.store.ListInterviewKnowledgeAtoms(filter)
	out := []domain.InterviewKnowledgeAtom{}
	for _, atom := range items {
		if atom.QuestionRole != "opening" && atom.QuestionRole != "mixed" {
			continue
		}
		out = append(out, atom)
	}
	return out
}

func interviewKnowledgeQuestionID(atom domain.InterviewKnowledgeAtom) string {
	version := atom.CurrentVersion
	if version <= 0 {
		version = 1
	}
	return fmt.Sprintf("interview-knowledge:%s:v%d", strings.TrimSpace(atom.ID), version)
}

func interviewQuestionFromAtom(atom domain.InterviewKnowledgeAtom, questionType string) *domain.InterviewQuestion {
	questionType = strings.TrimSpace(questionType)
	if questionType == "" {
		questionType = "principle"
	}
	domainName := firstNonEmpty(atom.Category, atom.Domain)
	subject := firstNonEmpty(atom.Subject, atom.Title, domainName)
	// 描述由 selectInterviewOpeningQuestion 经 resolveOpeningDescription 最终定稿；此处先给规则底稿。
	description := ensureSinglePointOpeningDescription(strings.TrimSpace(atom.Title), subject)
	createdAt := atom.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return &domain.InterviewQuestion{
		ID:                   interviewKnowledgeQuestionID(atom),
		Title:                firstNonEmpty(atom.Title, subject),
		Description:          description,
		Domain:               domainName,
		Difficulty:           atom.Difficulty,
		QuestionType:         questionType,
		ReferenceAnswer:      strings.Join(atom.Principles, "\n"),
		ReferenceKeywords:    interviewReferenceKeywordsFromAtom(atom),
		EvaluationDimensions: defaultInterviewEvaluationDimensions(),
		FollowUpStrategies:   followUpStrategiesFromAtom(atom),
		CreatedAt:            createdAt,
	}
}

func interviewQuestionSnapshotFromAtom(atom domain.InterviewKnowledgeAtom, questionType string) domain.InterviewQuestionSnapshot {
	question := interviewQuestionFromAtom(atom, questionType)
	return domain.InterviewQuestionSnapshot{
		ID:                   question.ID,
		Version:              maxInt(atom.CurrentVersion, 1),
		Title:                atom.Title,
		Subject:              firstNonEmpty(atom.Subject, atom.Title),
		Description:          question.Description,
		Domain:               question.Domain,
		Difficulty:           atom.Difficulty,
		Category:             atom.Category,
		QuestionRole:         atom.QuestionRole,
		QuestionType:         question.QuestionType,
		QuestionSource:       "interview_knowledge",
		SourceRef:            atom.SourceRef,
		Tags:                 append([]string{}, atom.Tags...),
		Principles:           append([]string{}, atom.Principles...),
		Pitfalls:             append([]string{}, atom.Pitfalls...),
		FollowUpPaths:        append([]string{}, atom.FollowUpPaths...),
		ReferenceKeywords:    append([]string{}, question.ReferenceKeywords...),
		EvaluationDimensions: append([]domain.EvaluationDimension{}, question.EvaluationDimensions...),
		FollowUpStrategies:   append([]domain.FollowUpStrategy{}, question.FollowUpStrategies...),
	}
}

func interviewQuestionSnapshotFromQuestion(question *domain.InterviewQuestion) domain.InterviewQuestionSnapshot {
	if question == nil {
		return domain.InterviewQuestionSnapshot{}
	}
	return domain.InterviewQuestionSnapshot{
		ID:                   question.ID,
		Version:              1,
		Title:                question.Title,
		Subject:              question.Title,
		Description:          question.Description,
		Domain:               question.Domain,
		Difficulty:           question.Difficulty,
		QuestionRole:         "opening",
		QuestionType:         question.QuestionType,
		QuestionSource:       "compatibility_question",
		ReferenceKeywords:    append([]string{}, question.ReferenceKeywords...),
		EvaluationDimensions: append([]domain.EvaluationDimension{}, question.EvaluationDimensions...),
		FollowUpStrategies:   append([]domain.FollowUpStrategy{}, question.FollowUpStrategies...),
	}
}

func interviewQuestionFromSnapshot(snapshot domain.InterviewQuestionSnapshot) *domain.InterviewQuestion {
	if strings.TrimSpace(snapshot.ID) == "" {
		return nil
	}
	return &domain.InterviewQuestion{
		ID:                   snapshot.ID,
		Title:                firstNonEmpty(snapshot.Title, snapshot.Subject),
		Description:          snapshot.Description,
		Domain:               snapshot.Domain,
		Difficulty:           snapshot.Difficulty,
		QuestionType:         firstNonEmpty(snapshot.QuestionType, "principle"),
		ReferenceAnswer:      strings.Join(snapshot.Principles, "\n"),
		ReferenceKeywords:    append([]string{}, snapshot.ReferenceKeywords...),
		EvaluationDimensions: append([]domain.EvaluationDimension{}, snapshot.EvaluationDimensions...),
		FollowUpStrategies:   append([]domain.FollowUpStrategy{}, snapshot.FollowUpStrategies...),
	}
}

func defaultInterviewEvaluationDimensions() []domain.EvaluationDimension {
	return []domain.EvaluationDimension{
		{Name: "technical_accuracy", Weight: 0.3, Criteria: "技术概念、命令、机制和判断是否准确"},
		{Name: "logical_completeness", Weight: 0.22, Criteria: "排查链路、因果关系和步骤是否完整"},
		{Name: "solution_feasibility", Weight: 0.2, Criteria: "方案是否可执行，是否覆盖验证、回滚和风险控制"},
		{Name: "depth_breadth", Weight: 0.16, Criteria: "是否能解释底层原理并覆盖关键边界情况"},
		{Name: "expression_structure", Weight: 0.12, Criteria: "表达是否结构化，重点是否清晰"},
	}
}

func followUpStrategiesFromAtom(atom domain.InterviewKnowledgeAtom) []domain.FollowUpStrategy {
	strategies := []domain.FollowUpStrategy{}
	for i, path := range atom.FollowUpPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		dimension := interviewFocusAreaOrder[i%len(interviewFocusAreaOrder)]
		strategies = append(strategies, domain.FollowUpStrategy{
			TriggerCondition: dimension + " < 60",
			QuestionTemplate: path,
			Type:             "deepen",
		})
	}
	if len(strategies) == 0 {
		strategies = append(strategies, domain.FollowUpStrategy{
			TriggerCondition: "logical_completeness < 60",
			QuestionTemplate: "你刚才最关键的那个判断，依据是什么？",
			Type:             "supplement",
		})
	}
	return strategies
}

func interviewReferenceKeywordsFromAtom(atom domain.InterviewKnowledgeAtom) []string {
	values := []string{atom.Title, atom.Subject, atom.Domain, atom.Category}
	values = append(values, atom.Tags...)
	values = append(values, atom.Principles...)
	return uniqueStrings(values)
}

func (s *Server) resolveInterviewSessionQuestion(session *domain.InterviewSession) (*domain.InterviewQuestion, bool) {
	if session == nil {
		return nil, false
	}
	if question, ok := s.store.GetInterviewQuestion(session.QuestionID); ok {
		return question, true
	}
	if question := interviewQuestionFromSnapshot(session.QuestionSnapshot); question != nil {
		return question, true
	}
	return nil, false
}

func (s *Server) retrieveInterviewFollowUpContext(ctx context.Context, req agentruntime.InterviewRetrievalRequest) (agentruntime.InterviewRetrievalResult, error) {
	baseSubject := interviewRetrievalBaseSubject(req)
	if !req.Evaluation.FollowUpTriggered {
		return agentruntime.InterviewRetrievalResult{
			FollowUpSubject: baseSubject,
			FallbackUsed:    false,
			SummaryText:     "本轮未触发追问，保留当前考察点摘要。",
		}, nil
	}
	queryText := buildInterviewRetrievalQuery(req)
	vectorStore := interviewKnowledgeVectorStore(s.store)
	if vectorStore == nil {
		result := interviewRetrievalFallback(baseSubject, "题库检索不可用，继续使用规则追问。")
		s.recordInterviewRetrievalLog(req, queryText, result)
		return result, nil
	}
	if strings.TrimSpace(queryText) == "" {
		result := interviewRetrievalFallback(baseSubject, "追问检索上下文为空，继续使用规则追问。")
		s.recordInterviewRetrievalLog(req, queryText, result)
		return result, nil
	}
	vector := s.embeddingVectorForInterviewRetrieval(ctx, queryText)
	results, err := vectorStore.SearchInterviewKnowledge(ctx, store.InterviewKnowledgeVectorSearchQuery{
		Category:      firstNonEmpty(req.Session.QuestionSnapshot.Category, req.Question.Domain),
		Difficulty:    firstNonEmpty(req.Session.QuestionSnapshot.Difficulty, req.Question.Difficulty),
		QuestionRoles: []string{"followup", "mixed"},
		Text:          queryText,
		Vector:        vector,
		Limit:         6,
	})
	if err != nil {
		result := interviewRetrievalFallback(baseSubject, "题库检索失败，继续使用规则追问。")
		s.recordInterviewRetrievalLog(req, queryText, result)
		return result, nil
	}
	matches, subjects := s.filteredInterviewRetrievalMatches(results)
	if len(matches) == 0 {
		result := interviewRetrievalFallback(baseSubject, "未命中可用题库追问原子，继续使用规则追问。")
		s.recordInterviewRetrievalLog(req, queryText, result)
		return result, nil
	}
	top, ok := s.store.GetInterviewKnowledgeAtom(matches[0].AtomID)
	if !ok {
		result := interviewRetrievalFallback(baseSubject, "命中原子已不可用，继续使用规则追问。")
		s.recordInterviewRetrievalLog(req, queryText, result)
		return result, nil
	}
	followUpQuestion := firstFollowUpPath(*top)
	if followUpQuestion == "" {
		followUpQuestion = req.Evaluation.FollowUpQuestion
	}
	followUpType := firstNonEmpty(req.Evaluation.FollowUpType, "deepen")
	subject := firstNonEmpty(top.Subject, baseSubject)
	result := agentruntime.InterviewRetrievalResult{
		FollowUpQuestion:  followUpQuestion,
		FollowUpType:      followUpType,
		FollowUpSubject:   subject,
		FallbackUsed:      false,
		RetrievedSubjects: subjects,
		MatchedAtoms:      matches,
		SummaryText:       fmt.Sprintf("命中 %d 个题库考察点，优先围绕“%s”追问。", len(matches), subject),
	}
	s.recordInterviewRetrievalLog(req, queryText, result)
	return result, nil
}

func (s *Server) embeddingVectorForInterviewRetrieval(ctx context.Context, queryText string) []float64 {
	if s.embedding == nil {
		return nil
	}
	result, err := s.embedding.Embed(ctx, []string{queryText})
	if err != nil || len(result.Vectors) != 1 || len(result.Vectors[0]) != interviewKnowledgeExpectedEmbeddingDim {
		return nil
	}
	return append([]float64{}, result.Vectors[0]...)
}

func (s *Server) filteredInterviewRetrievalMatches(results []store.InterviewKnowledgeVectorSearchResult) ([]domain.InterviewKnowledgeAtomLightSnapshot, []string) {
	matches := []domain.InterviewKnowledgeAtomLightSnapshot{}
	subjects := []string{}
	seenAtoms := map[string]bool{}
	for _, result := range results {
		atomID := strings.TrimSpace(result.Document.AtomID)
		if atomID == "" || seenAtoms[atomID] {
			continue
		}
		atom, ok := s.store.GetInterviewKnowledgeAtom(atomID)
		if !ok || atom.Status != "published" || atom.VectorStatus != "indexed" {
			continue
		}
		if atom.QuestionRole != "followup" && atom.QuestionRole != "mixed" {
			continue
		}
		seenAtoms[atomID] = true
		matches = append(matches, domain.InterviewKnowledgeAtomLightSnapshot{
			AtomID:   atom.ID,
			Version:  maxInt(atom.CurrentVersion, 1),
			Title:    atom.Title,
			Subject:  atom.Subject,
			Domain:   atom.Domain,
			Category: atom.Category,
		})
		subjects = append(subjects, atom.Subject)
	}
	return matches, uniqueStrings(subjects)
}

func interviewRetrievalFallback(subject, summary string) agentruntime.InterviewRetrievalResult {
	return agentruntime.InterviewRetrievalResult{
		FollowUpType:    "fallback_rule_only",
		FollowUpSubject: subject,
		FallbackUsed:    true,
		SummaryText:     summary,
	}
}

func (s *Server) recordInterviewRetrievalLog(req agentruntime.InterviewRetrievalRequest, queryText string, result agentruntime.InterviewRetrievalResult) {
	if req.Session == nil {
		return
	}
	round := req.Evaluation.Round
	if round <= 0 {
		round = req.Session.CurrentRound
	}
	errorMessage := ""
	if result.FallbackUsed {
		errorMessage = ai.Sanitize(result.SummaryText)
	}
	s.store.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID:    req.Session.ID,
		Round:        round,
		QueryText:    truncateText(ai.Sanitize(queryText), 500),
		MatchedAtoms: result.MatchedAtoms,
		FallbackUsed: result.FallbackUsed,
		ErrorMessage: truncateText(errorMessage, 300),
	})
}

func buildInterviewRetrievalQuery(req agentruntime.InterviewRetrievalRequest) string {
	setupNotes := ""
	focusAreas := []string{}
	difficultyLevel := ""
	snapshot := domain.InterviewQuestionSnapshot{}
	if req.Session != nil {
		setupNotes = req.Session.SetupNotes
		focusAreas = req.Session.FocusAreas
		difficultyLevel = req.Session.DifficultyLevel
		snapshot = req.Session.QuestionSnapshot
	}
	questionTitle := ""
	questionDescription := ""
	if req.Question != nil {
		questionTitle = req.Question.Title
		questionDescription = req.Question.Description
	}
	parts := []string{
		req.Answer,
		setupNotes,
		interviewFocusAreaLabelsText(focusAreas),
		difficultyLevel,
		snapshot.Subject,
		snapshot.Title,
		questionTitle,
		questionDescription,
		lowScoreDimensionsText(req.Evaluation),
	}
	return strings.Join(nonEmptyTexts(parts...), "\n")
}

func interviewRetrievalBaseSubject(req agentruntime.InterviewRetrievalRequest) string {
	if req.Session != nil {
		return firstNonEmpty(req.Session.QuestionSnapshot.Subject, req.Session.QuestionSnapshot.Title)
	}
	if req.Question != nil {
		return firstNonEmpty(req.Question.Title, req.Question.Domain)
	}
	return ""
}

func lowScoreDimensionsText(evaluation domain.InterviewEvaluation) string {
	if len(evaluation.DimensionScores) == 0 {
		return ""
	}
	parts := []string{}
	for _, key := range interviewFocusAreaOrder {
		score, ok := evaluation.DimensionScores[key]
		if !ok || score >= 70 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d", interviewFocusAreaLabel(key), score))
	}
	return strings.Join(parts, "；")
}

func firstFollowUpPath(atom domain.InterviewKnowledgeAtom) string {
	for _, item := range atom.FollowUpPaths {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

type interviewSessionResponse struct {
	ID                string                             `json:"id"`
	UserID            string                             `json:"user_id"`
	QuestionID        string                             `json:"question_id"`
	Mode              string                             `json:"mode,omitempty"`
	ResumeDocumentIDs []string                           `json:"resume_document_ids,omitempty"`
	Status            string                             `json:"status"`
	CurrentRound      int                                `json:"current_round"`
	MaxRounds         int                                `json:"max_rounds"`
	SmartClose        bool                               `json:"smart_close"`
	EndReason         string                             `json:"end_reason,omitempty"`
	DifficultyLevel   string                             `json:"difficulty_level,omitempty"`
	FocusAreas        []string                           `json:"focus_areas,omitempty"`
	SetupNotes        string                             `json:"setup_notes,omitempty"`
	Submissions       []domain.InterviewSubmission       `json:"submissions"`
	Evaluations       []domain.InterviewEvaluation       `json:"evaluations"`
	FollowUpQuestion  string                             `json:"follow_up_question,omitempty"`
	FinalScore        int                                `json:"final_score,omitempty"`
	FinalReport       string                             `json:"final_report,omitempty"`
	QuestionSnapshot  *interviewQuestionSnapshotResponse `json:"question_snapshot,omitempty"`
	StartedAt         time.Time                          `json:"started_at"`
	EndedAt           *time.Time                         `json:"ended_at,omitempty"`
}

type interviewQuestionSnapshotResponse struct {
	ID             string `json:"id"`
	Version        int    `json:"version,omitempty"`
	Title          string `json:"title"`
	Subject        string `json:"subject,omitempty"`
	Description    string `json:"description,omitempty"`
	Domain         string `json:"domain"`
	Difficulty     string `json:"difficulty"`
	Category       string `json:"category,omitempty"`
	QuestionRole   string `json:"question_role,omitempty"`
	QuestionType   string `json:"question_type,omitempty"`
	QuestionSource string `json:"question_source,omitempty"`
}

func interviewSessionView(session *domain.InterviewSession) interviewSessionResponse {
	if session == nil {
		return interviewSessionResponse{}
	}
	return interviewSessionResponse{
		ID:                session.ID,
		UserID:            session.UserID,
		QuestionID:        session.QuestionID,
		Mode:              session.Mode,
		ResumeDocumentIDs: append([]string{}, session.ResumeDocumentIDs...),
		Status:            session.Status,
		CurrentRound:      session.CurrentRound,
		MaxRounds:         session.MaxRounds,
		SmartClose:        session.SmartClose,
		EndReason:         session.EndReason,
		DifficultyLevel:   session.DifficultyLevel,
		FocusAreas:        append([]string{}, session.FocusAreas...),
		SetupNotes:        session.SetupNotes,
		Submissions:       append([]domain.InterviewSubmission{}, session.Submissions...),
		Evaluations:       append([]domain.InterviewEvaluation{}, session.Evaluations...),
		FollowUpQuestion:  session.FollowUpQuestion,
		FinalScore:        session.FinalScore,
		FinalReport:       session.FinalReport,
		QuestionSnapshot:  interviewQuestionSnapshotView(session.QuestionSnapshot),
		StartedAt:         session.StartedAt,
		EndedAt:           session.EndedAt,
	}
}

func interviewQuestionSnapshotView(snapshot domain.InterviewQuestionSnapshot) *interviewQuestionSnapshotResponse {
	if strings.TrimSpace(snapshot.ID) == "" {
		return nil
	}
	return &interviewQuestionSnapshotResponse{
		ID:             snapshot.ID,
		Version:        snapshot.Version,
		Title:          snapshot.Title,
		Subject:        snapshot.Subject,
		Description:    snapshot.Description,
		Domain:         snapshot.Domain,
		Difficulty:     snapshot.Difficulty,
		Category:       snapshot.Category,
		QuestionRole:   snapshot.QuestionRole,
		QuestionType:   snapshot.QuestionType,
		QuestionSource: snapshot.QuestionSource,
	}
}

type interviewReportRetrievalSummary struct {
	SummaryText           string                                 `json:"summary_text"`
	HitRounds             int                                    `json:"hit_rounds"`
	FallbackRounds        int                                    `json:"fallback_rounds"`
	SubjectCount          int                                    `json:"subject_count"`
	Subjects              []string                               `json:"subjects"`
	Rounds                []interviewReportRoundRetrievalSummary `json:"rounds"`
	Coverage              []interviewReportKnowledgeCoverage     `json:"coverage"`
	RetrainingSuggestions []interviewReportRetrainingSuggestion  `json:"retraining_suggestions"`
}

type interviewReportRoundRetrievalSummary struct {
	Round        int    `json:"round"`
	Subject      string `json:"subject"`
	FallbackUsed bool   `json:"fallback_used"`
	FollowUpType string `json:"follow_up_type"`
}

type interviewReportKnowledgeCoverage struct {
	Subject        string   `json:"subject"`
	RoundCount     int      `json:"round_count"`
	HitCount       int      `json:"hit_count"`
	FallbackCount  int      `json:"fallback_count"`
	AverageScore   int      `json:"average_score"`
	LowestScore    int      `json:"lowest_score"`
	WeakDimensions []string `json:"weak_dimensions"`
	SourceRounds   []int    `json:"-"`
}

type interviewReportRetrainingSuggestion struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Priority     int      `json:"priority"`
	Reason       string   `json:"reason"`
	Actions      []string `json:"actions"`
	TargetScore  int      `json:"target_score"`
	SourceRounds []int    `json:"source_rounds"`
}

type interviewReportCoverageAccumulator struct {
	subject          string
	roundCount       int
	hitCount         int
	fallbackCount    int
	totalScore       int
	scoreCount       int
	lowestScore      int
	sourceRounds     []int
	weakDimensionKey map[string]bool
}

func buildInterviewReportRetrievalSummary(session *domain.InterviewSession) interviewReportRetrievalSummary {
	summary := interviewReportRetrievalSummary{
		Subjects:              []string{},
		Rounds:                []interviewReportRoundRetrievalSummary{},
		Coverage:              []interviewReportKnowledgeCoverage{},
		RetrainingSuggestions: []interviewReportRetrainingSuggestion{},
	}
	if session == nil {
		return summary
	}
	subjectSet := map[string]bool{}
	coverageBySubject := map[string]*interviewReportCoverageAccumulator{}
	for _, evaluation := range session.Evaluations {
		subject := firstNonEmpty(evaluation.FollowUpSubject, firstString(evaluation.RetrievedSubjects), session.QuestionSnapshot.Subject, session.QuestionSnapshot.Title)
		if subject != "" && !subjectSet[subject] {
			subjectSet[subject] = true
			summary.Subjects = append(summary.Subjects, subject)
		}
		if len(evaluation.RetrievedSubjects) > 0 {
			summary.HitRounds++
		}
		if evaluation.FallbackUsed {
			summary.FallbackRounds++
		}
		summary.Rounds = append(summary.Rounds, interviewReportRoundRetrievalSummary{
			Round:        evaluation.Round,
			Subject:      subject,
			FallbackUsed: evaluation.FallbackUsed,
			FollowUpType: firstNonEmpty(evaluation.FollowUpType, "none"),
		})
		collectInterviewReportCoverage(coverageBySubject, subject, evaluation)
	}
	sort.Strings(summary.Subjects)
	summary.SubjectCount = len(summary.Subjects)
	summary.Coverage = interviewReportKnowledgeCoverageList(coverageBySubject)
	summary.RetrainingSuggestions = interviewReportRetrainingSuggestions(summary.Coverage)
	summary.SummaryText = interviewReportRetrievalSummaryText(summary)
	return summary
}

func interviewReportRetrievalSummaryText(summary interviewReportRetrievalSummary) string {
	if len(summary.Rounds) == 0 {
		return "本场暂无追问检索记录。"
	}
	return fmt.Sprintf("本场覆盖 %d 个考察点，题库命中 %d 轮，规则回退 %d 轮。", summary.SubjectCount, summary.HitRounds, summary.FallbackRounds)
}

func collectInterviewReportCoverage(bySubject map[string]*interviewReportCoverageAccumulator, subject string, evaluation domain.InterviewEvaluation) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	acc := bySubject[subject]
	if acc == nil {
		acc = &interviewReportCoverageAccumulator{
			subject:          subject,
			weakDimensionKey: map[string]bool{},
		}
		bySubject[subject] = acc
	}
	acc.roundCount++
	if len(evaluation.RetrievedSubjects) > 0 {
		acc.hitCount++
	}
	if evaluation.FallbackUsed {
		acc.fallbackCount++
	}
	if acc.scoreCount == 0 || evaluation.TotalScore < acc.lowestScore {
		acc.lowestScore = evaluation.TotalScore
	}
	acc.totalScore += evaluation.TotalScore
	acc.scoreCount++
	if evaluation.Round > 0 {
		acc.sourceRounds = append(acc.sourceRounds, evaluation.Round)
	}
	for key, score := range evaluation.DimensionScores {
		if score < 70 {
			acc.weakDimensionKey[strings.TrimSpace(key)] = true
		}
	}
}

func interviewReportKnowledgeCoverageList(bySubject map[string]*interviewReportCoverageAccumulator) []interviewReportKnowledgeCoverage {
	items := make([]interviewReportKnowledgeCoverage, 0, len(bySubject))
	for _, acc := range bySubject {
		averageScore := 0
		if acc.scoreCount > 0 {
			averageScore = (acc.totalScore + acc.scoreCount/2) / acc.scoreCount
		}
		sort.Ints(acc.sourceRounds)
		items = append(items, interviewReportKnowledgeCoverage{
			Subject:        acc.subject,
			RoundCount:     acc.roundCount,
			HitCount:       acc.hitCount,
			FallbackCount:  acc.fallbackCount,
			AverageScore:   averageScore,
			LowestScore:    acc.lowestScore,
			WeakDimensions: interviewReportWeakDimensionLabels(acc.weakDimensionKey),
			SourceRounds:   uniqueInts(acc.sourceRounds),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RoundCount != items[j].RoundCount {
			return items[i].RoundCount > items[j].RoundCount
		}
		return items[i].Subject < items[j].Subject
	})
	return items
}

func interviewReportWeakDimensionLabels(keys map[string]bool) []string {
	labels := []string{}
	seen := map[string]bool{}
	for _, key := range interviewFocusAreaOrder {
		if keys[key] {
			label := interviewFocusAreaLabel(key)
			labels = append(labels, label)
			seen[label] = true
		}
	}
	extra := []string{}
	for key := range keys {
		label := interviewFocusAreaLabel(key)
		if label != "" && !seen[label] {
			extra = append(extra, label)
		}
	}
	sort.Strings(extra)
	return append(labels, extra...)
}

func interviewReportRetrainingSuggestions(coverage []interviewReportKnowledgeCoverage) []interviewReportRetrainingSuggestion {
	suggestions := []interviewReportRetrainingSuggestion{}
	for _, item := range coverage {
		reasons := []string{}
		actions := []string{}
		priority := 4
		if item.RoundCount > 0 && item.LowestScore < 75 {
			priority = minPositive(priority, 1)
			weakText := "基础概念、判断依据或表达结构"
			if len(item.WeakDimensions) > 0 {
				weakText = strings.Join(item.WeakDimensions, "、")
			}
			reasons = append(reasons, fmt.Sprintf("最低分 %d，薄弱项：%s。", item.LowestScore, weakText))
			actions = append(actions, fmt.Sprintf("复盘“%s”相关低分轮次，补齐关键概念、判断依据和验证路径。", item.Subject))
			actions = append(actions, fmt.Sprintf("围绕“%s”重新完成一轮中等难度面试，目标达到 75 分以上。", item.Subject))
		}
		if item.FallbackCount > 0 {
			priority = minPositive(priority, 2)
			reasons = append(reasons, fmt.Sprintf("%d 轮使用规则回退，需要先把回答线索收束到清晰知识点。", item.FallbackCount))
			actions = append(actions, fmt.Sprintf("用 5 分钟整理“%s”的定义、适用场景、风险点和排查步骤。", item.Subject))
		}
		if len(coverage) == 1 {
			priority = minPositive(priority, 3)
			reasons = append(reasons, "本场覆盖知识点较集中，建议扩大相邻考察点。")
			actions = append(actions, "下一场选择同一领域的相邻训练入口，补齐横向覆盖。")
		}
		if len(reasons) == 0 {
			continue
		}
		suggestions = append(suggestions, interviewReportRetrainingSuggestion{
			ID:           interviewReportSuggestionID(item.Subject),
			Subject:      item.Subject,
			Priority:     priority,
			Reason:       strings.Join(uniqueStrings(reasons), " "),
			Actions:      uniqueStrings(actions),
			TargetScore:  75,
			SourceRounds: append([]int{}, item.SourceRounds...),
		})
	}
	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Priority != suggestions[j].Priority {
			return suggestions[i].Priority < suggestions[j].Priority
		}
		return suggestions[i].Subject < suggestions[j].Subject
	})
	if len(suggestions) > 5 {
		return suggestions[:5]
	}
	return suggestions
}

func interviewReportSuggestionID(subject string) string {
	normalized := strings.ToLower(strings.TrimSpace(subject))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "：", "-", "，", "-", ",", "-")
	normalized = strings.Trim(replacer.Replace(normalized), "-")
	if normalized == "" {
		return "retrain-coverage"
	}
	return "retrain-" + normalized
}

func minPositive(left, right int) int {
	if left <= 0 || right < left {
		return right
	}
	return left
}

func uniqueInts(values []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func nonEmptyTexts(values ...string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
