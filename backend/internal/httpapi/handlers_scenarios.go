package httpapi

import (
	"context"
	"fmt"
	"net/http"
	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
	"strings"
	"time"
)

func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	if suffix == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items := s.store.ListScenarios(r.URL.Query().Get("domain"), r.URL.Query().Get("difficulty"), r.URL.Query().Get("tag"))
		views := make([]domain.ScenarioQuestionView, 0, len(items))
		for _, item := range items {
			if item.Status != "active" || !canViewScenario(&item, user) {
				continue
			}
			views = append(views, scenarioPublicView(&item))
		}
		writeOK(w, map[string]interface{}{"list": paginate(views, r), "total": len(views)})
		return
	}

	if suffix == "/generate/jobs" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowAI(w, r, user, "scenario-generation", 10) {
			return
		}
		var req scenarioGenerationPayload
		if !decode(w, r, &req) {
			return
		}
		normalized, err := normalizeScenarioGenerationPayload(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateScenarioGenerationRequest(r, user, normalized); err != nil {
			writeScenarioGenerationValidationError(w, err)
			return
		}
		job, err := s.createScenarioGenerationJob(user.ID, normalized)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create AI job")
			return
		}
		writeOK(w, s.aiJobPayload(&job, user))
		return
	}

	if suffix == "/generate" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowAI(w, r, user, "scenario-generation", 10) {
			return
		}
		var req scenarioGenerationPayload
		if !decode(w, r, &req) {
			return
		}
		normalized, err := normalizeScenarioGenerationPayload(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateScenarioGenerationRequest(r, user, normalized); err != nil {
			writeScenarioGenerationValidationError(w, err)
			return
		}
		question, llmMeta, err := s.llmRouter().GenerateScenario(r.Context(), ai.ScenarioGenerationRequest{
			Domain:       normalized.Domain,
			Difficulty:   normalized.Difficulty,
			ScenarioType: normalized.ScenarioType,
			Tags:         normalized.Tags,
			Constraints:  normalized.toAIConstraints(),
			UserID:       user.ID,
			Nonce:        fmt.Sprintf("%d", time.Now().UnixNano()),
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, scenarioGenerationErrorMessage(err, llmMeta))
			return
		}
		created := s.store.AddScenario(question)
		s.auditScenarioGenerationCompleted(user.ID, "", created, llmMeta, normalized)
		writeOK(w, map[string]interface{}{
			"question_id":   created.ID,
			"status":        "active",
			"question":      scenarioView(&created, user),
			"provider":      llmMeta.Provider,
			"model":         llmMeta.Model,
			"validated":     llmMeta.Validated,
			"fallback_used": llmMeta.FallbackUsed,
		})
		return
	}

	parts := split(suffix)
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if parts[0] == "sessions" {
		s.handleScenarioSession(w, r, user, parts[1:])
		return
	}

	questionID := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		question, ok := s.store.GetScenario(questionID)
		if !ok {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		if question.Status != "active" || !canViewScenario(question, user) {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		writeOK(w, scenarioDetailView(question, user))
		return
	}
	if len(parts) == 2 && parts[1] == "sessions" && r.Method == http.MethodPost {
		question, ok := s.store.GetScenario(questionID)
		if !ok || question.Status != "active" {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		session, err := s.store.CreateScenarioSession(user.ID, questionID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{
			"session_id":        session.ID,
			"status":            session.Status,
			"question_snapshot": scenarioPublicView(&session.QuestionSnapshot),
		})
		return
	}
	if len(parts) == 2 && parts[1] == "fork" && r.Method == http.MethodPost {
		question, ok := s.store.GetScenario(questionID)
		if !ok || !canViewScenario(question, user) || question.Status != "active" {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		post := communityPostFromScenarioFork(question, user.ID)
		post.SensitiveCheck = s.sensitiveCheck(r, user, "fork_source", strings.Join([]string{post.Title, post.RawContent, strings.Join(post.Tags, " ")}, "\n"))
		post = s.store.AddCommunityPost(post)
		s.audit(r, user, "scenario.fork", "community_post", post.ID, map[string]string{"source_scenario_id": question.ID})
		writeOK(w, post)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}
func (s *Server) handleScenarioSession(w http.ResponseWriter, r *http.Request, user *domain.User, parts []string) {
	if len(parts) == 1 && r.Method == http.MethodGet {
		sessionID := parts[0]
		session, ok := s.store.GetScenarioSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		_ = s.expireScenarioSessionIfIdle(session)
		writeOK(w, map[string]interface{}{
			"session":  scenarioSessionView(session),
			"messages": s.store.ListScenarioMessages(sessionID),
		})
		return
	}
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	sessionID := parts[0]
	action := parts[1]
	switch action {
	case "messages":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowAI(w, r, user, "scenario-reply", 60) {
			return
		}
		var req struct {
			Content string `json:"content"`
		}
		if !decode(w, r, &req) {
			return
		}
		var writer *sseWriter
		var onStage func(string, string)
		var onDelta func(string)
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			onStage = writer.stage
			onDelta = func(chunk string) { writer.delta(chunk, true) }
			writer.stage("agent_intent", "正在分析你的排查意图")
		}
		message, session, err := s.processScenarioMessage(r.Context(), user, sessionID, strings.TrimSpace(req.Content), r, onStage, onDelta)
		if err != nil {
			if writer != nil {
				writer.fail(err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		payload := map[string]interface{}{
			"message":        message,
			"response_meta":  message.ResponseMeta,
			"session_status": session.Status,
			"session":        scenarioSessionView(session),
		}
		if writer != nil {
			writer.stage("completed", "本轮 Agent 排查完成")
			writer.finish(payload)
			return
		}
		writeOK(w, payload)
	case "answer":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Answer string `json:"answer"`
		}
		if !decode(w, r, &req) {
			return
		}
		session, err := s.evaluateScenarioAnswer(user, sessionID, req.Answer)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{
			"evaluation_id": session.ID + "-evaluation",
			"status":        session.Status,
			"result":        session.EvaluationResult,
			"score":         session.Score,
		})
	case "review":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		session, ok := s.store.GetScenarioSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if s.expireScenarioSessionIfIdle(session) {
			writeError(w, http.StatusBadRequest, "session is abandoned")
			return
		}
		writeOK(w, map[string]interface{}{
			"session":         scenarioSessionView(session),
			"messages":        s.store.ListScenarioMessages(sessionID),
			"standard_answer": session.QuestionSnapshot.Content.RootCause,
			"standard_steps":  session.QuestionSnapshot.Content.StandardProcedure,
			"key_evidence":    session.QuestionSnapshot.Content.KeyEvidence,
		})
	case "quit":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		session, ok := s.store.GetScenarioSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if s.expireScenarioSessionIfIdle(session) {
			writeOK(w, map[string]interface{}{"status": session.Status, "session": scenarioSessionView(session)})
			return
		}
		now := time.Now()
		session.Status = "abandoned"
		session.EndedAt = &now
		s.store.SaveScenarioSession(session)
		writeOK(w, map[string]interface{}{"status": "abandoned", "session": scenarioSessionView(session)})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}
func (s *Server) processScenarioMessage(ctx context.Context, user *domain.User, sessionID, content string, callbacks ...interface{}) (domain.ScenarioMessage, *domain.ScenarioSession, error) {
	if content == "" {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("content is required")
	}
	session, ok := s.store.GetScenarioSession(sessionID)
	if !ok || session.UserID != user.ID {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session not found")
	}
	if s.expireScenarioSessionIfIdle(session) {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session is abandoned")
	}
	if session.Status != "active" {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session is not active")
	}
	if session.CurrentTurn >= session.MaxTurns {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("max turns reached, please submit an answer")
	}

	question := &session.QuestionSnapshot
	existingMessages := s.store.ListScenarioMessages(sessionID)
	request, onStage, onDelta := scenarioMessageCallbacks(callbacks...)
	agent := agentruntime.NewDiagnosticAgent(agentruntime.DiagnosticConfig{
		Rewrite: func(ctx context.Context, req ai.ScenarioReplyRequest, delta func(string)) (string, ai.CallMeta, error) {
			if delta != nil {
				return s.llmRouter().RewriteScenarioReplyStream(ctx, req, delta)
			}
			return s.llmRouter().RewriteScenarioReply(ctx, req)
		},
		SemanticGate: agentruntime.NewSemanticGate(agentruntime.SemanticGateConfig{Embedding: s.embedding}),
	})
	result, err := agent.Run(ctx, agentruntime.DiagnosticRequest{
		Session:        session,
		Question:       question,
		UserMessage:    content,
		Messages:       existingMessages,
		RecentMessages: recentScenarioContext(existingMessages, 5),
		SummaryBuilder: buildScenarioConversationSummary,
		OnStage:        onStage,
		OnDelta:        onDelta,
	})
	if err != nil {
		s.auditDiagnosticAgentRun(request, user, session.ID, result.Trace, result.Meta, "failed", err)
		return domain.ScenarioMessage{}, nil, err
	}
	session.CurrentTurn++
	session.LastActiveAt = time.Now()
	if session.CurrentTurn >= session.MaxTurns {
		result.AssistantContent += " 当前会话已达到最大轮次，请提交最终根因答案。"
	}
	message := s.store.AddScenarioMessage(domain.ScenarioMessage{
		SessionID:        session.ID,
		TurnNumber:       session.CurrentTurn,
		Role:             "assistant",
		UserContent:      content,
		AssistantContent: result.AssistantContent,
		ResponseMeta:     result.Meta,
	})
	s.store.SaveScenarioSession(session)
	s.auditDiagnosticAgentRun(request, user, session.ID, result.Trace, result.Meta, "completed", nil)
	return message, session, nil
}
func scenarioMessageCallbacks(callbacks ...interface{}) (*http.Request, func(string, string), func(string)) {
	var request *http.Request
	var onStage func(string, string)
	var onDelta func(string)
	for _, callback := range callbacks {
		switch value := callback.(type) {
		case *http.Request:
			request = value
		case func(string, string):
			onStage = value
		case func(string):
			onDelta = value
		}
	}
	return request, onStage, onDelta
}
func buildScenarioConversationSummary(existing string, question *domain.ScenarioQuestion, messages []domain.ScenarioMessage) string {
	if len(messages) == 0 {
		return strings.TrimSpace(existing)
	}
	limit := len(messages) - 5
	if limit < 0 {
		limit = len(messages)
	}
	if limit == 0 {
		return strings.TrimSpace(existing)
	}
	older := messages[:limit]
	revealed := []string{}
	userFocus := []string{}
	for _, message := range older {
		if message.ResponseMeta.RevealedClueID != "" {
			revealed = append(revealed, message.ResponseMeta.RevealedClueID)
		}
		if strings.TrimSpace(message.UserContent) != "" {
			userFocus = append(userFocus, truncateText(message.UserContent, 80))
		}
	}
	var builder strings.Builder
	if strings.TrimSpace(existing) != "" {
		builder.WriteString(strings.TrimSpace(existing))
		builder.WriteString("\n")
	}
	if question != nil {
		builder.WriteString("题目：")
		builder.WriteString(question.Title)
		builder.WriteString("。")
	}
	fmt.Fprintf(&builder, "已压缩前 %d 轮对话。", limit)
	if len(revealed) > 0 {
		builder.WriteString("已释放线索ID：")
		builder.WriteString(strings.Join(uniqueStrings(revealed), ","))
		builder.WriteString("。")
	}
	if len(userFocus) > 0 {
		if len(userFocus) > 8 {
			userFocus = userFocus[len(userFocus)-8:]
		}
		builder.WriteString("用户主要追问：")
		builder.WriteString(strings.Join(userFocus, " / "))
		builder.WriteString("。")
	}
	return truncateText(strings.TrimSpace(builder.String()), 1800)
}
func recentScenarioContext(messages []domain.ScenarioMessage, limit int) []ai.ScenarioContextMessage {
	if limit <= 0 || len(messages) == 0 {
		return []ai.ScenarioContextMessage{}
	}
	start := len(messages) - limit
	if start < 0 {
		start = 0
	}
	out := make([]ai.ScenarioContextMessage, 0, len(messages[start:]))
	for _, message := range messages[start:] {
		out = append(out, ai.ScenarioContextMessage{
			TurnNumber:       message.TurnNumber,
			UserContent:      message.UserContent,
			AssistantContent: truncateText(message.AssistantContent, 240),
		})
	}
	return out
}
func (s *Server) evaluateScenarioAnswer(user *domain.User, sessionID, answer string) (*domain.ScenarioSession, error) {
	session, ok := s.store.GetScenarioSession(sessionID)
	if !ok || session.UserID != user.ID {
		return nil, fmt.Errorf("session not found")
	}
	if s.expireScenarioSessionIfIdle(session) {
		return nil, fmt.Errorf("session is abandoned")
	}
	if session.Status != "active" {
		return nil, fmt.Errorf("session is not active")
	}
	question := &session.QuestionSnapshot
	messages := s.store.ListScenarioMessages(sessionID)
	var vectorStore store.VectorStore
	if provider, ok := s.store.(store.VectorStoreProvider); ok {
		vectorStore = provider.VectorStore()
	}
	score, report, missing := scoreScenarioWithEvidenceChain(scenarioScoringInput{
		Question:    question,
		Messages:    messages,
		Answer:      answer,
		RevealedIDs: session.RevealedClueIDs,
		CurrentTurn: session.CurrentTurn,
		VectorStore: vectorStore,
	})
	evaluation := &domain.ScenarioEvaluation{
		IsCorrect:         score.Accuracy >= 70,
		MatchDegree:       score.Accuracy,
		MissingPoints:     missing,
		StandardProcedure: question.Content.StandardProcedure,
		ScoringReport:     report,
	}
	now := time.Now()
	session.UserAnswer = answer
	session.EvaluationResult = evaluation
	session.Score = score
	session.Status = "evaluated"
	session.EndedAt = &now
	s.store.SaveScenarioSession(session)
	s.store.RecordScenarioScore(user.ID, question.Domain, score.Total)
	return session, nil
}
func (s *Server) expireScenarioSessionIfIdle(session *domain.ScenarioSession) bool {
	if session == nil || session.Status != "active" {
		return false
	}
	if time.Since(session.LastActiveAt) <= 30*time.Minute {
		return false
	}
	now := time.Now()
	session.Status = "abandoned"
	session.EndedAt = &now
	s.store.SaveScenarioSession(session)
	return true
}
