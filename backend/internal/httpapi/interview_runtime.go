package httpapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	agentruntime "situational-teaching/backend/internal/agent"
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

func (s *Server) selectInterviewOpeningQuestion(domainName, difficulty, questionType string) (*domain.InterviewQuestion, domain.InterviewQuestionSnapshot, bool) {
	if atom, ok := s.selectInterviewOpeningAtom(domainName, difficulty); ok {
		question := interviewQuestionFromAtom(*atom, questionType)
		return question, interviewQuestionSnapshotFromAtom(*atom, question.QuestionType), true
	}
	question, ok := s.store.FindInterviewQuestion(domainName, difficulty, questionType)
	if !ok {
		return nil, domain.InterviewQuestionSnapshot{}, false
	}
	return question, interviewQuestionSnapshotFromQuestion(question), true
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
	description := strings.TrimSpace(atom.Title)
	if description == "" {
		description = fmt.Sprintf("请围绕%s说明你的理解、关键判断依据和落地处理路径。", subject)
	} else if !strings.Contains(description, "请") {
		description = fmt.Sprintf("请回答：%s。说明你的关键判断依据、风险点和落地处理路径。", description)
	}
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
			QuestionTemplate: "请补充说明你的关键判断依据、验证路径和风险控制。",
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
	vectorStore := interviewKnowledgeVectorStore(s.store)
	if vectorStore == nil {
		return interviewRetrievalFallback(baseSubject, "题库检索不可用，继续使用规则追问。"), nil
	}
	queryText := buildInterviewRetrievalQuery(req)
	if strings.TrimSpace(queryText) == "" {
		return interviewRetrievalFallback(baseSubject, "追问检索上下文为空，继续使用规则追问。"), nil
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
		return interviewRetrievalFallback(baseSubject, "题库检索失败，继续使用规则追问。"), nil
	}
	matches, subjects := s.filteredInterviewRetrievalMatches(results)
	if len(matches) == 0 {
		return interviewRetrievalFallback(baseSubject, "未命中可用题库追问原子，继续使用规则追问。"), nil
	}
	top, ok := s.store.GetInterviewKnowledgeAtom(matches[0].AtomID)
	if !ok {
		return interviewRetrievalFallback(baseSubject, "命中原子已不可用，继续使用规则追问。"), nil
	}
	followUpQuestion := firstFollowUpPath(*top)
	if followUpQuestion == "" {
		followUpQuestion = req.Evaluation.FollowUpQuestion
	}
	followUpType := firstNonEmpty(req.Evaluation.FollowUpType, "deepen")
	subject := firstNonEmpty(top.Subject, baseSubject)
	return agentruntime.InterviewRetrievalResult{
		FollowUpQuestion:  followUpQuestion,
		FollowUpType:      followUpType,
		FollowUpSubject:   subject,
		FallbackUsed:      false,
		RetrievedSubjects: subjects,
		MatchedAtoms:      matches,
		SummaryText:       fmt.Sprintf("命中 %d 个题库考察点，优先围绕“%s”追问。", len(matches), subject),
	}, nil
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

func buildInterviewRetrievalQuery(req agentruntime.InterviewRetrievalRequest) string {
	parts := []string{
		req.Answer,
		req.Session.SetupNotes,
		interviewFocusAreaLabelsText(req.Session.FocusAreas),
		req.Session.DifficultyLevel,
		req.Session.QuestionSnapshot.Subject,
		req.Session.QuestionSnapshot.Title,
		req.Question.Title,
		req.Question.Description,
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
	ID               string                             `json:"id"`
	UserID           string                             `json:"user_id"`
	QuestionID       string                             `json:"question_id"`
	Status           string                             `json:"status"`
	CurrentRound     int                                `json:"current_round"`
	MaxRounds        int                                `json:"max_rounds"`
	DifficultyLevel  string                             `json:"difficulty_level,omitempty"`
	FocusAreas       []string                           `json:"focus_areas,omitempty"`
	SetupNotes       string                             `json:"setup_notes,omitempty"`
	Submissions      []domain.InterviewSubmission       `json:"submissions"`
	Evaluations      []domain.InterviewEvaluation       `json:"evaluations"`
	FollowUpQuestion string                             `json:"follow_up_question,omitempty"`
	FinalScore       int                                `json:"final_score,omitempty"`
	FinalReport      string                             `json:"final_report,omitempty"`
	QuestionSnapshot *interviewQuestionSnapshotResponse `json:"question_snapshot,omitempty"`
	StartedAt        time.Time                          `json:"started_at"`
	EndedAt          *time.Time                         `json:"ended_at,omitempty"`
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
		ID:               session.ID,
		UserID:           session.UserID,
		QuestionID:       session.QuestionID,
		Status:           session.Status,
		CurrentRound:     session.CurrentRound,
		MaxRounds:        session.MaxRounds,
		DifficultyLevel:  session.DifficultyLevel,
		FocusAreas:       append([]string{}, session.FocusAreas...),
		SetupNotes:       session.SetupNotes,
		Submissions:      append([]domain.InterviewSubmission{}, session.Submissions...),
		Evaluations:      append([]domain.InterviewEvaluation{}, session.Evaluations...),
		FollowUpQuestion: session.FollowUpQuestion,
		FinalScore:       session.FinalScore,
		FinalReport:      session.FinalReport,
		QuestionSnapshot: interviewQuestionSnapshotView(session.QuestionSnapshot),
		StartedAt:        session.StartedAt,
		EndedAt:          session.EndedAt,
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
	SummaryText    string                                 `json:"summary_text"`
	HitRounds      int                                    `json:"hit_rounds"`
	FallbackRounds int                                    `json:"fallback_rounds"`
	SubjectCount   int                                    `json:"subject_count"`
	Subjects       []string                               `json:"subjects"`
	Rounds         []interviewReportRoundRetrievalSummary `json:"rounds"`
}

type interviewReportRoundRetrievalSummary struct {
	Round        int    `json:"round"`
	Subject      string `json:"subject"`
	FallbackUsed bool   `json:"fallback_used"`
	FollowUpType string `json:"follow_up_type"`
}

func buildInterviewReportRetrievalSummary(session *domain.InterviewSession) interviewReportRetrievalSummary {
	summary := interviewReportRetrievalSummary{
		Subjects: []string{},
		Rounds:   []interviewReportRoundRetrievalSummary{},
	}
	if session == nil {
		return summary
	}
	subjectSet := map[string]bool{}
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
	}
	sort.Strings(summary.Subjects)
	summary.SubjectCount = len(summary.Subjects)
	summary.SummaryText = interviewReportRetrievalSummaryText(summary)
	return summary
}

func interviewReportRetrievalSummaryText(summary interviewReportRetrievalSummary) string {
	if len(summary.Rounds) == 0 {
		return "本场暂无追问检索记录。"
	}
	return fmt.Sprintf("本场覆盖 %d 个考察点，题库命中 %d 轮，规则回退 %d 轮。", summary.SubjectCount, summary.HitRounds, summary.FallbackRounds)
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
