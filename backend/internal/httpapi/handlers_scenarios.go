package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"situational-teaching/backend/internal/agentclient"
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
			Content       string `json:"content"`
			RequestID     string `json:"request_id"`
			StateRevision *int   `json:"state_revision"`
			AfterSequence int    `json:"after_sequence"`
		}
		if !decode(w, r, &req) {
			return
		}
		var writer *sseWriter
		requestID := strings.TrimSpace(req.RequestID)
		if requestID == "" {
			requestID = store.NewID()
		}
		userContent := strings.TrimSpace(req.Content)
		sentThrough := req.AfterSequence
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			for _, event := range scenarioInitialRunEvents(requestID, userContent) {
				if event.Sequence <= sentThrough {
					continue
				}
				writer.runEvent(event)
				sentThrough = event.Sequence
			}
		}
		onRunEvent := func(event domain.ScenarioRunEvent) {
			if writer == nil || event.Sequence <= sentThrough {
				return
			}
			writer.runEvent(event)
			sentThrough = event.Sequence
		}
		message, session, err := s.processScenarioMessage(r.Context(), user, scenarioMessageInput{
			SessionID:     sessionID,
			Content:       userContent,
			RequestID:     requestID,
			StateRevision: req.StateRevision,
			OnRunEvent:    onRunEvent,
		})
		if err != nil {
			status, code, message := scenarioMessageError(err)
			if writer != nil {
				writer.runEvent(domain.ScenarioRunEvent{
					RequestID: requestID,
					Sequence:  sentThrough + 1,
					Kind:      "turn_failed",
					Status:    "failed",
					Summary:   message,
					ErrorCode: code,
				})
				return
			}
			writeErrorWithData(w, status, message, map[string]string{"error_code": code})
			return
		}
		payload := map[string]interface{}{
			"message":        message,
			"response_meta":  message.ResponseMeta,
			"run_events":     message.ResponseMeta.RunEvents,
			"session_status": session.Status,
			"session":        scenarioSessionView(session),
		}
		if writer != nil {
			for _, event := range message.ResponseMeta.RunEvents {
				if event.Sequence <= sentThrough {
					continue
				}
				writer.runEvent(event)
				sentThrough = event.Sequence
			}
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

type scenarioMessageInput struct {
	SessionID     string
	Content       string
	RequestID     string
	StateRevision *int
	OnRunEvent    func(domain.ScenarioRunEvent)
}

func (s *Server) processScenarioMessage(ctx context.Context, user *domain.User, input scenarioMessageInput) (message domain.ScenarioMessage, responseSession *domain.ScenarioSession, err error) {
	if input.Content == "" {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("content is required")
	}
	if input.RequestID == "" || len(input.RequestID) > 160 {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("request_id is invalid")
	}
	session, ok := s.store.GetScenarioSession(input.SessionID)
	if !ok || session.UserID != user.ID {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session not found")
	}
	fingerprint := scenarioRequestFingerprint(session.ID, input.Content)
	if existing, ok := s.store.GetScenarioAgentTurn(session.ID, input.RequestID); ok {
		if existing.RequestFingerprint != fingerprint {
			return domain.ScenarioMessage{}, nil, domain.ScenarioRequestConflictError{RequestID: input.RequestID}
		}
		replayedSession := existing.SessionSnapshot
		return existing.Message, &replayedSession, nil
	}
	flight, leader, err := s.beginScenarioTurnFlight(session.ID, input.RequestID, fingerprint)
	if err != nil {
		return domain.ScenarioMessage{}, nil, err
	}
	if !leader {
		return waitScenarioTurnFlight(ctx, flight)
	}
	defer func() {
		s.finishScenarioTurnFlight(session.ID, input.RequestID, flight, message, responseSession, err)
	}()
	if s.expireScenarioSessionIfIdle(session) {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session is abandoned")
	}
	if session.Status != "active" {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session is not active")
	}
	if session.CurrentTurn >= session.MaxTurns {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("max turns reached, please submit an answer")
	}
	expectedRevision := session.StateRevision
	if input.StateRevision != nil {
		expectedRevision = *input.StateRevision
	}
	if expectedRevision != session.StateRevision {
		return domain.ScenarioMessage{}, nil, domain.ScenarioRevisionConflictError{
			Expected: expectedRevision,
			Current:  session.StateRevision,
		}
	}
	question := &session.QuestionSnapshot
	if question.Content.ModelVersion != domain.HiddenWorldContractVersion ||
		question.Content.PublicScenario == nil || question.Content.HiddenWorld == nil {
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "scenario_contract_unsupported",
			Message: "当前题目不是可运行的 HiddenWorld 题目",
		}
	}
	if s.scenarioAgent == nil {
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusServiceUnavailable,
			Code:    "agent_not_configured",
			Message: "排查导师服务尚未配置",
		}
	}
	existingMessages := s.store.ListScenarioMessages(session.ID)
	// request_id 已登记为在途幂等轮次后，浏览器断线只应停止当前 HTTP 等待，
	// 不能取消 Python 主链并让重连重复执行 compare_answer。保留请求值，
	// 但用服务端总 deadline 约束这次业务执行。
	agentContext, cancelAgent := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancelAgent()
	agentRequest := agentclient.TurnRequest{
		ContractVersion: agentclient.ContractVersion,
		RequestID:       input.RequestID,
		SessionID:       session.ID,
		StateRevision:   expectedRevision,
		PublicScenario:  *question.Content.PublicScenario,
		HiddenWorld:     *question.Content.HiddenWorld,
		LearnerState:    learnerStateForAgent(session.LearnerState),
		Transcript:      scenarioTranscript(existingMessages),
		UserMessage:     input.Content,
		Budget:          agentclient.Budget{DeadlineMS: 15000, MaxReleases: 3},
	}
	var result agentclient.TurnResult
	var streamValidator *scenarioPublicTraceStream
	if streamingClient, ok := s.scenarioAgent.(scenarioAgentStreamingClient); ok && input.OnRunEvent != nil {
		streamValidator = newScenarioPublicTraceStream(
			input.RequestID,
			input.Content,
			question.Content.HiddenWorld,
			session.LearnerState.Normalized(),
		)
		result, err = streamingClient.TurnStream(agentContext, agentRequest, agentclient.StreamCallbacks{
			OnTurnAnalysis: streamValidator.onTurnAnalysis,
			OnPublicTrace: func(trace agentclient.PublicTraceEvent) error {
				if err := streamValidator.onPublicTrace(trace); err != nil {
					return err
				}
				sequence := len(scenarioInitialRunEvents(input.RequestID, input.Content)) + streamValidator.emittedCount
				input.OnRunEvent(mapScenarioPublicTraceEvent(input.RequestID, trace, sequence))
				return nil
			},
		})
	} else {
		result, err = s.scenarioAgent.Turn(agentContext, agentRequest)
	}
	if err != nil {
		if streamValidator != nil && streamValidator.validationError != nil {
			return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
				Status:  http.StatusBadGateway,
				Code:    "public_trace_rejected",
				Message: "排查导师返回了未通过公开协议校验的过程事件",
			}
		}
		return domain.ScenarioMessage{}, nil, classifyScenarioAgentError(err)
	}
	nextState, approvals, err := approveScenarioProposals(session, question.Content.HiddenWorld, result)
	if err != nil {
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "proposal_rejected",
			Message: "排查导师返回了未通过业务校验的状态提议",
		}
	}
	if err := validateScenarioReply(result.Reply, question.Content.HiddenWorld, nextState); err != nil {
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "reply_guard_rejected",
			Message: "排查导师回复未通过安全校验",
		}
	}
	if err := validateScenarioPublicTrace(input.RequestID, input.Content, result, question.Content.HiddenWorld, nextState); err != nil {
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "public_trace_rejected",
			Message: "排查导师返回了未通过公开协议校验的过程事件",
		}
	}
	nextSession := *session
	nextSession.CurrentTurn++
	nextSession.LastActiveAt = time.Now()
	nextSession.LearnerState = nextState
	nextSession.RevealedClueIDs = append([]string{}, nextState.CollectedEvidence...)
	reply := strings.TrimSpace(result.Reply)
	if nextSession.CurrentTurn >= nextSession.MaxTurns {
		reply += " 当前会话已达到最大轮次，请提交最终根因答案。"
	}
	runEvents := buildScenarioRunEvents(input.RequestID, input.Content, result, reply)
	publicTrace := marshalAgentAudit(runEvents)
	message = domain.ScenarioMessage{
		SessionID:        session.ID,
		TurnNumber:       nextSession.CurrentTurn,
		Role:             "assistant",
		UserContent:      input.Content,
		AssistantContent: reply,
		ResponseMeta: domain.ResponseMeta{
			ResponseType: "mentor_reply",
			RequestID:    input.RequestID,
			Revision:     expectedRevision + 1,
			RunEvents:    runEvents,
		},
	}
	nextMessages := append(append([]domain.ScenarioMessage{}, existingMessages...), message)
	nextSession.ConversationSummary = buildScenarioConversationSummary(nextSession.ConversationSummary, question, nextMessages)
	commitResult, err := s.store.CommitScenarioAgentTurn(domain.ScenarioAgentTurnCommit{
		SessionID:            session.ID,
		RequestID:            input.RequestID,
		RequestFingerprint:   fingerprint,
		ExpectedRevision:     expectedRevision,
		Message:              message,
		NextSession:          nextSession,
		PublicTrace:          publicTrace,
		InternalVerification: marshalAgentAudit(result.InternalVerification),
		InternalAudit:        marshalAgentAudit(result.InternalAudit),
		ApprovalAudit:        marshalAgentAudit(approvals),
	})
	if err != nil {
		return domain.ScenarioMessage{}, nil, err
	}
	committedSession := commitResult.Record.SessionSnapshot
	return commitResult.Record.Message, &committedSession, nil
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
	userFocus := []string{}
	for _, message := range older {
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
