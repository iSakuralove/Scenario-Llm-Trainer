package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
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
		question, llmMeta, validationErrors, err := s.generateScenarioWithRetries(r.Context(), user.ID, normalized)
		if err != nil {
			writeScenarioGenerationFailure(w, http.StatusBadGateway, scenarioGenerationErrorMessage(err, llmMeta), validationErrors)
			return
		}
		created := s.store.AddScenario(question)
		s.auditScenarioGenerationCompleted(user.ID, "", created, llmMeta, normalized)
		writeOK(w, map[string]interface{}{
			"question_id":   created.ID,
			"status":        "active",
			"question":      scenarioPublicView(&created),
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
			Content          string `json:"content"`
			RequestID        string `json:"request_id"`
			StateRevision    *int   `json:"state_revision"`
			AfterSequence    int    `json:"after_sequence"`
			StructuredAction *struct {
				ActionID        string `json:"action_id"`
				CatalogVersion  string `json:"catalog_version"`
				NormalizedScope string `json:"normalized_scope"`
			} `json:"structured_user_action"`
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
		structuredAction := (*scenarioStructuredUserAction)(nil)
		if req.StructuredAction != nil && strings.TrimSpace(req.StructuredAction.ActionID) != "" {
			structuredAction = &scenarioStructuredUserAction{
				ActionID:        strings.TrimSpace(req.StructuredAction.ActionID),
				CatalogVersion:  strings.TrimSpace(req.StructuredAction.CatalogVersion),
				NormalizedScope: strings.TrimSpace(req.StructuredAction.NormalizedScope),
			}
		}
		diagnostics := &scenarioTurnDiagnostics{
			StartedAt:        time.Now(),
			StateRevision:    scenarioRequestedRevision(req.StateRevision),
			DeadlineMS:       s.scenarioTurnDeadlineMS,
			AgentTimeoutMS:   int(s.scenarioAgentTimeout.Milliseconds()),
			ActionID:         scenarioActionID(structuredAction),
			StructuredAction: structuredAction != nil,
			QuickAction:      structuredAction != nil && userContent == "",
			Streaming:        writer != nil,
		}
		sentThrough := req.AfterSequence
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			diagnostics.Streaming = true
		}
		onRunEvent := func(event domain.ScenarioRunEvent) {
			if writer == nil || event.Sequence <= sentThrough {
				return
			}
			writer.runEvent(event)
			diagnostics.EventsSent++
			if event.Kind == "turn_started" {
				diagnostics.TurnStartedSent = true
			}
			sentThrough = event.Sequence
		}
		var onReasoningRawDelta func(string)
		if writer != nil && scenarioRawReasoningStreamEnabled() {
			onReasoningRawDelta = func(text string) {
				writer.debugTrace("reasoning_raw_delta", text)
			}
		}
		message, session, err := s.processScenarioMessage(r.Context(), user, scenarioMessageInput{
			SessionID:           sessionID,
			Content:             userContent,
			RequestID:           requestID,
			StateRevision:       req.StateRevision,
			StructuredAction:    structuredAction,
			OnRunEvent:          onRunEvent,
			OnReasoningRawDelta: onReasoningRawDelta,
			Diagnostics:         diagnostics,
		})
		if err != nil {
			status, code, message := scenarioMessageError(err)
			logScenarioTurnFailure(requestID, sessionID, diagnostics, status, code, err)
			if writer != nil {
				writer.runEvent(scenarioTurnFailedEvent(
					requestID,
					s.scenarioSessionRevision(sessionID),
					sentThrough+1,
					code,
					message,
					scenarioErrorCodeRetryable(code),
				))
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
			// 实时序号与落库序号共用同一空间：流式阶段已下发的事件（含
			// reply_delta）满足 Sequence <= sentThrough 自动跳过，避免重复渲染；
			// 断线重连（after_sequence）或幂等回放时补发客户端尚未确认的事件。
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
		if session.Status != "evaluated" {
			writeError(w, http.StatusBadRequest, "请先提交排查结论，再查看完整复盘")
			return
		}
		writeOK(w, map[string]interface{}{
			"session":         scenarioSessionView(session),
			"messages":        s.store.ListScenarioMessages(sessionID),
			"standard_answer": scenarioStandardAnswer(session),
			"standard_steps":  session.QuestionSnapshot.Content.StandardProcedure,
			"key_evidence":    session.QuestionSnapshot.Content.KeyEvidence,
			"debrief":         buildScenarioDebrief(session),
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
		nextSession := *session
		nextSession.Status = "abandoned"
		nextSession.EndedAt = &now
		committed, err := s.store.CommitScenarioSessionTransition(domain.ScenarioSessionTransition{
			SessionID:        session.ID,
			ExpectedRevision: session.StateRevision,
			NextSession:      nextSession,
		})
		if err != nil {
			writeError(w, http.StatusConflict, "会话状态已变化，请刷新后重试")
			return
		}
		writeOK(w, map[string]interface{}{"status": "abandoned", "session": scenarioSessionView(committed)})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

type scenarioStructuredUserAction struct {
	ActionID        string
	CatalogVersion  string
	NormalizedScope string
}

type scenarioMessageInput struct {
	SessionID           string
	Content             string
	RequestID           string
	StateRevision       *int
	StructuredAction    *scenarioStructuredUserAction
	OnRunEvent          func(domain.ScenarioRunEvent)
	OnReasoningRawDelta func(string)
	Diagnostics         *scenarioTurnDiagnostics
}

type scenarioTurnDiagnostics struct {
	StartedAt          time.Time
	StateRevision      int
	ExpectedRevision   int
	ActionID           string
	StructuredAction   bool
	QuickAction        bool
	Streaming          bool
	Phase              string
	EventsSent         int
	TurnStartedSent    bool
	ReasoningChunks    int
	ReplyChunks        int
	DeadlineMS         int
	AgentTimeoutMS     int
	AcceptedTraceCount int
	DroppedTraceCount  int
	ResultReceived     bool
	CommitAttempted    bool
	ActivityTouched    bool
	UpstreamStatus     int
	UpstreamCode       string
	UpstreamMessage    string
}

func (d *scenarioTurnDiagnostics) setPhase(phase string) {
	if d != nil && strings.TrimSpace(phase) != "" {
		d.Phase = phase
	}
}

func (d *scenarioTurnDiagnostics) setUpstreamError(err error) {
	if d == nil || err == nil {
		return
	}
	var httpErr agentclient.HTTPError
	if errors.As(err, &httpErr) {
		d.UpstreamStatus = httpErr.StatusCode
		d.UpstreamCode = httpErr.Code
		d.UpstreamMessage = truncateScenarioLogValue(httpErr.Message)
	}
}

func truncateScenarioLogValue(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 512 {
		return value[:512] + "..."
	}
	return value
}

func scenarioRequestedRevision(revision *int) int {
	if revision == nil {
		return -1
	}
	return *revision
}

func scenarioActionID(action *scenarioStructuredUserAction) string {
	if action == nil {
		return ""
	}
	return action.ActionID
}

func logScenarioTurnFailure(requestID, sessionID string, diagnostics *scenarioTurnDiagnostics, status int, code string, err error) {
	if diagnostics == nil {
		log.Printf("[scenario-agent] request_id=%s session_id=%s status=%d error_code=%s error_type=%T error=%v",
			requestID, sessionID, status, code, err, err)
		return
	}
	elapsedMS := time.Since(diagnostics.StartedAt).Milliseconds()
	log.Printf("[scenario-agent] request_id=%s session_id=%s state_revision=%d expected_revision=%d action_id=%s structured_action=%t quick_action=%t streaming=%t phase=%s elapsed_ms=%d deadline_ms=%d agent_timeout_ms=%d turn_started_sent=%t events_sent=%d reasoning_chunks=%d reply_chunks=%d accepted_traces=%d dropped_traces=%d result_received=%t commit_attempted=%t activity_touched=%t upstream_status=%d upstream_code=%s upstream_message=%q status=%d error_code=%s error_type=%T error=%v",
		requestID,
		sessionID,
		diagnostics.StateRevision,
		diagnostics.ExpectedRevision,
		diagnostics.ActionID,
		diagnostics.StructuredAction,
		diagnostics.QuickAction,
		diagnostics.Streaming,
		diagnostics.Phase,
		elapsedMS,
		diagnostics.DeadlineMS,
		diagnostics.AgentTimeoutMS,
		diagnostics.TurnStartedSent,
		diagnostics.EventsSent,
		diagnostics.ReasoningChunks,
		diagnostics.ReplyChunks,
		diagnostics.AcceptedTraceCount,
		diagnostics.DroppedTraceCount,
		diagnostics.ResultReceived,
		diagnostics.CommitAttempted,
		diagnostics.ActivityTouched,
		diagnostics.UpstreamStatus,
		diagnostics.UpstreamCode,
		diagnostics.UpstreamMessage,
		status,
		code,
		err,
		err)
}

// scenarioSessionRevision 取会话当前业务状态版本，供 V2 事件外层携带。
// 会话不存在时返回 0；调用方都处于已鉴权路径，这里只做尽力读取。
func (s *Server) scenarioSessionRevision(sessionID string) int {
	if session, ok := s.store.GetScenarioSession(sessionID); ok {
		return session.StateRevision
	}
	return 0
}

// scenarioErrorCodeRetryable 判断失败码是否值得原样重试。
// 契约不兼容与 request_id 冲突重试也不会成功，其余（超时、熔断、上游异常）
// 都允许学生直接重发。
func scenarioErrorCodeRetryable(code string) bool {
	switch code {
	case "agent_contract_mismatch", "request_id_conflict", "unknown_structured_action":
		return false
	default:
		return true
	}
}

// scenarioReplyEchoesUserMessage identifies the unsafe fallback where the
// mentor returns the student's prompt verbatim. Both values are trimmed here
// because the HTTP boundary already normalizes user content and model output
// may carry incidental surrounding whitespace; the comparison itself remains
// an exact, case-sensitive match and never exposes the prompt in an error.
func scenarioReplyEchoesUserMessage(reply, userMessage string) bool {
	reply = strings.TrimSpace(reply)
	userMessage = strings.TrimSpace(userMessage)
	return reply != "" && userMessage != "" && reply == userMessage
}

func (s *Server) processScenarioMessage(ctx context.Context, user *domain.User, input scenarioMessageInput) (message domain.ScenarioMessage, responseSession *domain.ScenarioSession, err error) {
	if input.Diagnostics != nil {
		input.Diagnostics.setPhase("request_validating")
	}
	// QuickAction 轮没有自然语言正文：结构化动作本身就是用户输入。
	if input.Content == "" && input.StructuredAction == nil {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("content is required")
	}
	if input.RequestID == "" || len(input.RequestID) > 160 {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("request_id is invalid")
	}
	session, ok := s.store.GetScenarioSession(input.SessionID)
	if !ok || session.UserID != user.ID {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session not found")
	}
	if input.Diagnostics != nil {
		input.Diagnostics.StateRevision = session.StateRevision
		input.Diagnostics.setPhase("session_loaded")
	}
	// 指纹包含结构化动作 ID：同一会话里不同 QuickAction 的空正文轮不能共用指纹，
	// 否则 request_id 重放校验会把两次不同点击误判为冲突。
	fingerprintContent := input.Content
	if input.StructuredAction != nil {
		fingerprintContent += "\x00action:" + input.StructuredAction.ActionID
	}
	fingerprint := scenarioRequestFingerprint(session.ID, fingerprintContent)
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
	activityRevision := session.StateRevision
	expectedRevision := -1
	agentStarted := false
	defer func() {
		if err != nil && agentStarted && activityRevision >= 0 {
			touched, touchErr := s.store.TouchScenarioSessionActivity(session.ID, activityRevision)
			if touchErr != nil {
				log.Printf("[scenario-session] request_id=%s session_id=%s expected_revision=%d touch_activity=false error_type=%T error=%v",
					input.RequestID, session.ID, activityRevision, touchErr, touchErr)
			} else if input.Diagnostics != nil {
				input.Diagnostics.ActivityTouched = touched
			}
		}
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
	expectedRevision = session.StateRevision
	if input.StateRevision != nil {
		expectedRevision = *input.StateRevision
	}
	if input.Diagnostics != nil {
		input.Diagnostics.ExpectedRevision = expectedRevision
		input.Diagnostics.setPhase("revision_checked")
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
			Message: "排查服务尚未配置",
		}
	}
	// 结构化动作白名单：只接受题目声明的观察动作（Runtime ActionCatalog 切片），
	// 未知动作在入口拒绝，不进入 Python。
	if input.StructuredAction != nil && !scenarioActionDeclared(question.Content.HiddenWorld, input.StructuredAction.ActionID) {
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "unknown_structured_action",
			Message: "该快捷动作不在当前题目目录中",
		}
	}
	existingMessages := s.store.ListScenarioMessages(session.ID)
	// request_id 已登记为在途幂等轮次后，浏览器断线只应停止当前 HTTP 等待，
	// 不能取消 Python 主链并让重连重复执行 compare_answer。保留请求值，
	// 但用服务端总 deadline 约束这次业务执行。
	agentContext, cancelAgent := context.WithTimeout(context.WithoutCancel(ctx), s.scenarioAgentTimeout)
	defer cancelAgent()
	var structuredAction *agentclient.StructuredUserAction
	if input.StructuredAction != nil {
		// state_revision 由服务端按 CAS 期望值写入：客户端不能自带别的版本号，
		// Python 侧用它做同轮绑定校验。
		structuredAction = &agentclient.StructuredUserAction{
			ActionID:        input.StructuredAction.ActionID,
			CatalogVersion:  input.StructuredAction.CatalogVersion,
			StateRevision:   expectedRevision,
			NormalizedScope: input.StructuredAction.NormalizedScope,
		}
	}
	agentRequest := agentclient.TurnRequest{
		ContractVersion:      agentclient.ContractVersion,
		RequestID:            input.RequestID,
		SessionID:            session.ID,
		StateRevision:        expectedRevision,
		PublicScenario:       *question.Content.PublicScenario,
		HiddenWorld:          *question.Content.HiddenWorld,
		LearnerState:         learnerStateForAgent(session.LearnerState),
		Transcript:           scenarioRecentTranscript(existingMessages, 4),
		ConversationSummary:  session.ConversationSummary,
		UserMessage:          input.Content,
		Phase:                "new_user_turn",
		TurnID:               input.RequestID,
		OriginalUserMessage:  input.Content,
		ActionHistory:        scenarioActionHistoryForAgent(existingMessages, question.Content.HiddenWorld),
		ToolStates:           scenarioToolStatesForAgent(existingMessages, session.LearnerState, question.Content.HiddenWorld),
		StructuredUserAction: structuredAction,
		Budget:               agentclient.Budget{DeadlineMS: s.scenarioTurnDeadlineMS, MaxReleases: 3},
	}
	if input.Diagnostics != nil {
		input.Diagnostics.setPhase("agent_request_prepared")
	}
	// V2 事件统一携带提交后的业务状态版本；catalog 版本绑定题目快照。
	committedRevision := expectedRevision + 1
	catalogVersion := question.ID + ":" + question.Content.ModelVersion
	var result agentclient.TurnResult
	var streamValidator *scenarioPublicTraceStream
	var acceptedStreamTraces []agentclient.PublicTraceEvent
	var streamedReplyChunks []string
	tracesBeforeReply := -1
	if streamingClient, ok := s.scenarioAgent.(scenarioAgentStreamingClient); ok && input.OnRunEvent != nil {
		agentStarted = true
		if input.Diagnostics != nil {
			input.Diagnostics.Streaming = true
			input.Diagnostics.setPhase("agent_streaming")
		}
		// 提交屏障：本阶段不向 SSE 发送任何正式 RunEvent。turn_started、观察、
		// 线索、提示和回复正文都要等 CommitScenarioAgentTurn 成功后，由调用方
		// 回放持久化的 ResponseMeta.RunEvents，避免提交失败时已经泄露正文。
		streamValidator = newScenarioPublicTraceStream(
			input.RequestID,
			input.Content,
			question.Content.HiddenWorld,
			question.Content.PublicScenario,
			session.LearnerState.Normalized(),
			s.scenarioPublicTraceValidationMode,
		)
		projectedTraceEvents := 0
		result, err = streamingClient.TurnStream(agentContext, agentRequest, agentclient.StreamCallbacks{
			OnTurnAnalysis: streamValidator.onTurnAnalysis,
			OnPublicTrace: func(trace agentclient.PublicTraceEvent) error {
				if err := streamValidator.onPublicTrace(trace); err != nil {
					return err
				}
				if !streamValidator.lastAccepted {
					if input.Diagnostics != nil {
						input.Diagnostics.DroppedTraceCount++
					}
					// 过程事件降级只影响事件本身；不把未通过复核的 payload
					// 放进正式历史，也不在提交前向前端发送。
					return nil
				}
				acceptedStreamTraces = append(acceptedStreamTraces, trace)
				if input.Diagnostics != nil {
					input.Diagnostics.AcceptedTraceCount++
				}
				// 公开 trace 仅缓存在内存中，最终由 buildScenarioRunEvents 统一
				// 投影并随提交结果持久化；不能在提交前发送 observation/clue/hint。
				if projected, ok := projectScenarioTraceEvents(input.RequestID, committedRevision, question.Content.HiddenWorld, trace); ok {
					projectedTraceEvents += len(projected)
				}
				return nil
			},
			OnReplyDelta: func(text string) error {
				if text == "" {
					return nil
				}
				if tracesBeforeReply < 0 {
					tracesBeforeReply = projectedTraceEvents
				}
				streamedReplyChunks = append(streamedReplyChunks, text)
				if input.Diagnostics != nil {
					input.Diagnostics.ReplyChunks++
				}
				// assistant 正文同样只缓存在 streamedReplyChunks；提交成功后由
				// 持久化 RunEvents 唯一回放，失败时浏览器只能收到 turn_failed。
				return nil
			},
			OnReasoningRawDelta: func(text string) error {
				if !scenarioRawReasoningStreamEnabled() || input.OnReasoningRawDelta == nil {
					return nil
				}
				input.OnReasoningRawDelta(text)
				if input.Diagnostics != nil {
					input.Diagnostics.ReasoningChunks++
				}
				return nil
			},
		})
	} else {
		agentStarted = true
		if input.Diagnostics != nil {
			input.Diagnostics.setPhase("agent_requesting")
		}
		result, err = s.scenarioAgent.Turn(agentContext, agentRequest)
	}
	if err != nil {
		if input.Diagnostics != nil {
			input.Diagnostics.setUpstreamError(err)
			input.Diagnostics.setPhase("agent_failed")
		}
		return domain.ScenarioMessage{}, nil, classifyScenarioAgentError(err)
	}
	if input.Diagnostics != nil {
		input.Diagnostics.ResultReceived = true
		input.Diagnostics.setPhase("agent_result_received")
	}
	if streamValidator != nil {
		// 实时投影和落库必须使用同一份“通过过程事件复核”的子集；否则 log
		// 模式虽然能继续流正文，收尾时 rebuild 又可能把坏事件投影回历史。
		result.PublicTrace = acceptedStreamTraces
	} else if s.scenarioPublicTraceValidationMode != scenarioValidationOff {
		// 非 SSE 路径同样逐条复核并只保留通过的公开事件。log 模式只放宽
		// 业务回合是否继续，不允许违规 observation 进入响应或历史。
		traceFilter := newScenarioPublicTraceStream(
			input.RequestID,
			input.Content,
			question.Content.HiddenWorld,
			question.Content.PublicScenario,
			session.LearnerState.Normalized(),
			s.scenarioPublicTraceValidationMode,
		)
		_ = traceFilter.onTurnAnalysis(result.TurnAnalysis)
		accepted := make([]agentclient.PublicTraceEvent, 0, len(result.PublicTrace))
		for _, trace := range result.PublicTrace {
			if err := traceFilter.onPublicTrace(trace); err != nil {
				// 过程事件属于可丢弃旁路；即使未来恢复严格过滤，
				// 也不能因为一条坏 trace 阻断最终正文。
				continue
			}
			if traceFilter.lastAccepted {
				accepted = append(accepted, trace)
				if input.Diagnostics != nil {
					input.Diagnostics.AcceptedTraceCount++
				}
			} else if input.Diagnostics != nil {
				input.Diagnostics.DroppedTraceCount++
			}
		}
		traceFilter.drainBypasses(s)
		result.PublicTrace = accepted
	}
	// Final backend barrier: never persist or stream a mentor reply that is
	// exactly the student's own message. This check is independent of the
	// validation migration mode so an upstream/runtime fallback cannot bypass
	// it. Returning an error here makes the outer SSE handler emit only
	// turn_failed; no message or RunEvents have been committed yet.
	if scenarioReplyEchoesUserMessage(result.Reply, input.Content) {
		if input.Diagnostics != nil {
			input.Diagnostics.setPhase("reply_echo_guard")
		}
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "reply_echoed_user_message",
			Message: "本轮回复未通过安全校验",
		}
	}
	nextState, approvals, err := approveScenarioProposals(session, question.Content.HiddenWorld, result, s.scenarioValidationMode)
	if err != nil {
		if input.Diagnostics != nil {
			input.Diagnostics.setPhase("proposal_validation")
		}
		// 保留具体拒绝分支在服务端，用户侧继续使用稳定的业务错误码；
		// 这样跨 Python/Go 状态提议不一致时可以直接定位，而不会把内部
		// 题库字段或校验细节泄露到浏览器。
		log.Printf("[scenario-validation] validator=proposal request_id=%s session_id=%s violation=%v",
			input.RequestID, session.ID, err)
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "proposal_rejected",
			Message: "排查服务返回了未通过业务校验的状态提议",
		}
	}
	if s.scenarioValidationMode == scenarioValidationLog {
		for _, approval := range approvals {
			if !approval.Accepted {
				s.recordScenarioValidationBypass("proposal", input.RequestID,
					fmt.Sprintf("kind=%s reason=%s", approval.Kind, approval.ReasonCode))
			}
		}
	}
	nextState = scenarioFillCurrentFocus(nextState, question.Content.HiddenWorld)
	if s.scenarioValidationMode != scenarioValidationOff {
		if input.Diagnostics != nil {
			input.Diagnostics.setPhase("reply_guard")
		}
		if err := validateScenarioReply(
			result.Reply,
			question.Content.HiddenWorld,
			question.Content.PublicScenario,
			nextState,
			scenarioPublicObservationTexts(result.PublicTrace)...,
		); err != nil {
			if s.scenarioValidationMode == scenarioValidationLog {
				s.recordScenarioValidationBypass("reply_guard", input.RequestID, err.Error())
			} else {
				return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
					Status:  http.StatusBadGateway,
					Code:    "reply_guard_rejected",
					Message: "本轮回复未通过安全校验",
				}
			}
		}
	}
	// 过程事件有独立的旁路过滤：无论迁移档位如何，单条 trace 的协议分歧
	// 都只记录并丢弃，不能阻断已经通过最终回复/状态契约的正文。真正不可
	// 恢复的结果解码、状态提交和最终回合契约错误仍在前面返回。
	if s.scenarioPublicTraceValidationMode != scenarioValidationOff {
		if err := validateScenarioPublicTrace(input.RequestID, input.Content, result, question.Content.HiddenWorld, question.Content.PublicScenario, nextState); err != nil {
			s.recordScenarioValidationBypass("public_trace", input.RequestID, err.Error())
		}
	}
	// 流式影子模式下放行的违规统一在这里落审计（proposal 已在上方即时记录）。
	if streamValidator != nil {
		streamValidator.drainBypasses(s)
	}
	nextSession := *session
	if input.Diagnostics != nil {
		input.Diagnostics.setPhase("building_commit")
	}
	nextSession.CurrentTurn++
	nextSession.LastActiveAt = time.Now()
	nextSession.LearnerState = nextState
	nextSession.RevealedClueIDs = append([]string{}, nextState.CollectedEvidence...)
	reply := strings.TrimSpace(result.Reply)
	if nextSession.CurrentTurn >= nextSession.MaxTurns {
		reply += " 当前会话已达到最大轮次，请提交排查结论。"
	}
	runEvents := buildScenarioRunEvents(
		input.RequestID,
		result,
		reply,
		committedRevision,
		question.Content.HiddenWorld,
		session.LearnerState,
		nextState,
		catalogVersion,
		streamedReplyChunks,
		tracesBeforeReply,
	)
	publicTrace := marshalAgentAudit(runEvents)
	// QuickAction 轮的展示正文：Agent 收到的 user_message 仍为空（结构化动作
	// 走授权投影），但消息记录必须保留学生点了什么——否则历史回放只剩
	// 一条孤立的回复，对话上下文断裂。标题与 QuickActions 按钮同源。
	userDisplayContent := input.Content
	if input.StructuredAction != nil && userDisplayContent == "" {
		userDisplayContent = scenarioActionDisplayTitle(question.Content.HiddenWorld, input.StructuredAction.ActionID)
	}
	message = domain.ScenarioMessage{
		SessionID:        session.ID,
		TurnNumber:       nextSession.CurrentTurn,
		Role:             "assistant",
		UserContent:      userDisplayContent,
		AssistantContent: reply,
		ResponseMeta: domain.ResponseMeta{
			ResponseType: "mentor_reply",
			RequestID:    input.RequestID,
			Revision:     expectedRevision + 1,
			RunEvents:    runEvents,
		},
	}
	nextMessages := append(append([]domain.ScenarioMessage{}, existingMessages...), message)
	nextSession.ConversationSummary = buildScenarioConversationSummary(
		nextSession.ConversationSummary,
		question,
		nextMessages,
		nextState,
	)
	if input.Diagnostics != nil {
		input.Diagnostics.CommitAttempted = true
		input.Diagnostics.setPhase("session_commit")
	}
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
		if input.Diagnostics != nil {
			input.Diagnostics.setPhase("session_commit_failed")
		}
		// 数据库/存储错误不能沿用通用 err.Error() 回传，否则会把
		// PostgreSQL 主机、连接串或驱动细节暴露给学生。外层仍只发
		// turn_failed，用户可用新的 request_id 重试。
		return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "scenario_commit_failed",
			Message: "本轮未能保存，请重试",
		}
	}
	committedSession := commitResult.Record.SessionSnapshot
	return commitResult.Record.Message, &committedSession, nil
}

func buildScenarioConversationSummary(
	existing string,
	question *domain.ScenarioQuestion,
	messages []domain.ScenarioMessage,
	states ...domain.ScenarioLearnerState,
) string {
	if len(messages) == 0 {
		return strings.TrimSpace(existing)
	}
	olderCount := len(messages) - 4
	if olderCount < 0 {
		olderCount = 0
	}
	topics := make([]string, 0, 6)
	seenTopics := map[string]bool{}
	for _, message := range messages[:olderCount] {
		text := truncateText(strings.TrimSpace(message.UserContent), 80)
		if text == "" || seenTopics[text] {
			continue
		}
		seenTopics[text] = true
		topics = append(topics, text)
		if len(topics) == 6 {
			break
		}
	}
	state := domain.ScenarioLearnerState{}.Normalized()
	if len(states) > 0 {
		state = states[0].Normalized()
	}
	masteredConcepts := []string{}
	if question != nil && question.Content.HiddenWorld != nil && question.Content.HiddenWorld.TeachingModel != nil {
		for _, concept := range question.Content.HiddenWorld.TeachingModel.Concepts {
			if state.ConceptMastery[concept.ConceptID] >= 2 {
				masteredConcepts = append(masteredConcepts, concept.Label)
			}
		}
	}
	facts := append([]string{}, state.EstablishedFacts...)
	if len(facts) > 4 {
		facts = facts[len(facts)-4:]
	}
	var builder strings.Builder
	if question != nil {
		builder.WriteString("题目：")
		builder.WriteString(question.Title)
		builder.WriteString("。\n")
	}
	if len(topics) > 0 {
		builder.WriteString("已讨论主题：")
		builder.WriteString(strings.Join(topics, " / "))
		builder.WriteString("。\n")
	}
	if len(masteredConcepts) > 0 {
		builder.WriteString("已掌握概念：")
		builder.WriteString(strings.Join(masteredConcepts, "、"))
		builder.WriteString("。\n")
	}
	if len(facts) > 0 {
		builder.WriteString("已形成事实：")
		builder.WriteString(strings.Join(facts, "；"))
		builder.WriteString("。\n")
	}
	if strings.TrimSpace(state.CurrentFocus) != "" {
		builder.WriteString("当前关注：")
		builder.WriteString(state.CurrentFocus)
		builder.WriteString("。")
	}
	return truncateText(strings.TrimSpace(builder.String()), 1800)
}

type scenarioDebrief struct {
	DirectTrigger string   `json:"direct_trigger"`
	LatentIssues  []string `json:"latent_issues"`
	Phenomenon    string   `json:"phenomenon"`
	DerivedRisks  []string `json:"derived_risks"`
	CausalChain   []string `json:"causal_chain"`
	Solutions     []string `json:"solutions"`
	Verification  []string `json:"verification"`
}

func scenarioStandardAnswer(session *domain.ScenarioSession) string {
	if session == nil {
		return ""
	}
	world := session.QuestionSnapshot.Content.HiddenWorld
	if world != nil && world.CanonicalAnswer != nil && strings.TrimSpace(world.CanonicalAnswer.CanonicalConclusion) != "" {
		return world.CanonicalAnswer.CanonicalConclusion
	}
	return session.QuestionSnapshot.Content.RootCause
}

func buildScenarioDebrief(session *domain.ScenarioSession) scenarioDebrief {
	debrief := scenarioDebrief{
		LatentIssues: []string{},
		DerivedRisks: []string{},
		CausalChain:  []string{},
		Solutions:    []string{},
		Verification: []string{},
	}
	if session == nil || session.QuestionSnapshot.Content.HiddenWorld == nil {
		return debrief
	}
	world := session.QuestionSnapshot.Content.HiddenWorld
	if world.CanonicalAnswer != nil {
		answer := world.CanonicalAnswer
		debrief.DirectTrigger = answer.DirectTrigger
		debrief.LatentIssues = append([]string{}, answer.LatentIssues...)
		debrief.Phenomenon = answer.Phenomenon
		debrief.DerivedRisks = append([]string{}, answer.DerivedRisks...)
		debrief.Solutions = append([]string{}, answer.SolutionRequirements...)
		required := stringSet(answer.RequiredEvidenceIDs)
		for _, node := range world.EvidenceGraph {
			if required[node.EvidenceID] && strings.TrimSpace(node.Content) != "" {
				debrief.CausalChain = append(debrief.CausalChain, node.Content)
			}
		}
	}
	if len(debrief.Solutions) == 0 {
		debrief.Solutions = append([]string{}, world.SolutionRubric.RequiredActions...)
	}
	debrief.Verification = append([]string{}, world.SolutionRubric.VerificationSteps...)
	return debrief
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
	parallelScore := scoreScenarioWithHiddenWorld(
		question,
		session,
		answer,
		s.store.ListScenarioAgentTurns(sessionID),
	)
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      user.ID,
		Action:       "scenario.scoring_parallel_compare",
		ResourceType: "scenario_session",
		ResourceID:   sessionID,
		Metadata:     parallelScore.auditMetadata(score),
	})
	evaluation := &domain.ScenarioEvaluation{
		IsCorrect:         score.Accuracy >= 70,
		MatchDegree:       score.Accuracy,
		MissingPoints:     missing,
		StandardProcedure: question.Content.StandardProcedure,
		ScoringReport:     report,
	}
	now := time.Now()
	nextSession := *session
	nextSession.UserAnswer = answer
	nextSession.EvaluationResult = evaluation
	nextSession.Score = score
	nextSession.Status = "evaluated"
	nextSession.EndedAt = &now
	committed, err := s.store.CommitScenarioSessionTransition(domain.ScenarioSessionTransition{
		SessionID:        session.ID,
		ExpectedRevision: session.StateRevision,
		NextSession:      nextSession,
	})
	if err != nil {
		return nil, err
	}
	s.store.RecordScenarioScore(user.ID, question.Domain, score.Total)
	return committed, nil
}
func (s *Server) expireScenarioSessionIfIdle(session *domain.ScenarioSession) bool {
	if session == nil || session.Status != "active" {
		return false
	}
	if time.Since(session.LastActiveAt) <= 30*time.Minute {
		return false
	}
	now := time.Now()
	nextSession := *session
	nextSession.Status = "abandoned"
	nextSession.EndedAt = &now
	committed, err := s.store.CommitScenarioSessionTransition(domain.ScenarioSessionTransition{
		SessionID:        session.ID,
		ExpectedRevision: session.StateRevision,
		NextSession:      nextSession,
	})
	if err != nil {
		latest, ok := s.store.GetScenarioSession(session.ID)
		if ok && latest.Status != "active" {
			*session = *latest
			return true
		}
		return false
	}
	*session = *committed
	return true
}
