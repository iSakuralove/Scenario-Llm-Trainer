package httpapi

import (
	"context"
	"fmt"
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
		sentThrough := req.AfterSequence
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			for _, event := range scenarioInitialRunEvents(requestID, s.scenarioSessionRevision(sessionID)) {
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
			SessionID:        sessionID,
			Content:          userContent,
			RequestID:        requestID,
			StateRevision:    req.StateRevision,
			StructuredAction: structuredAction,
			OnRunEvent:       onRunEvent,
		})
		if err != nil {
			status, code, message := scenarioMessageError(err)
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

type scenarioStructuredUserAction struct {
	ActionID        string
	CatalogVersion  string
	NormalizedScope string
}

type scenarioMessageInput struct {
	SessionID        string
	Content          string
	RequestID        string
	StateRevision    *int
	StructuredAction *scenarioStructuredUserAction
	OnRunEvent       func(domain.ScenarioRunEvent)
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

func (s *Server) processScenarioMessage(ctx context.Context, user *domain.User, input scenarioMessageInput) (message domain.ScenarioMessage, responseSession *domain.ScenarioSession, err error) {
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
		Transcript:           scenarioTranscript(existingMessages),
		UserMessage:          input.Content,
		StructuredUserAction: structuredAction,
		Budget:               agentclient.Budget{DeadlineMS: s.scenarioTurnDeadlineMS, MaxReleases: 3},
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
		streamValidator = newScenarioPublicTraceStream(
			input.RequestID,
			input.Content,
			question.Content.HiddenWorld,
			question.Content.PublicScenario,
			session.LearnerState.Normalized(),
			s.scenarioPublicTraceValidationMode,
		)
		// Go 是 public sequence 唯一生成者：turn_started 占 1，此后每个 V2 事件
		// 按真实到达顺序递增；落库（buildScenarioRunEvents）用同一投影函数重建，
		// 断线重连（after_sequence）回放与实时流逐位对齐，不重新编号。
		realtimeSeq := 1
		projectedTraceEvents := 0
		result, err = streamingClient.TurnStream(agentContext, agentRequest, agentclient.StreamCallbacks{
			OnTurnAnalysis: streamValidator.onTurnAnalysis,
			OnPublicTrace: func(trace agentclient.PublicTraceEvent) error {
				if err := streamValidator.onPublicTrace(trace); err != nil {
					return err
				}
				if !streamValidator.lastAccepted {
					// 过程事件降级只影响事件本身；正文回调继续向前端流动，
					// 但不把未通过复核的 payload 放进正式历史。
					return nil
				}
				acceptedStreamTraces = append(acceptedStreamTraces, trace)
				event, ok := projectScenarioTraceEvent(input.RequestID, committedRevision, question.Content.HiddenWorld, trace)
				if !ok {
					return nil
				}
				realtimeSeq++
				event.Sequence = realtimeSeq
				projectedTraceEvents++
				input.OnRunEvent(event)
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
				realtimeSeq++
				input.OnRunEvent(scenarioAssistantDeltaEvent(input.RequestID, committedRevision, realtimeSeq, "replying", text))
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
				Message: "排查服务返回了未通过公开协议校验的过程事件",
			}
		}
		return domain.ScenarioMessage{}, nil, classifyScenarioAgentError(err)
	}
	if streamValidator != nil {
		// 实时投影和落库必须使用同一份“通过过程事件复核”的子集；否则 log
		// 模式虽然能继续流正文，收尾时 rebuild 又可能把坏事件投影回历史。
		result.PublicTrace = acceptedStreamTraces
	}
	nextState, approvals, err := approveScenarioProposals(session, question.Content.HiddenWorld, result, s.scenarioValidationMode)
	if err != nil {
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
	if s.scenarioValidationMode != scenarioValidationOff {
		if err := validateScenarioReply(result.Reply, question.Content.HiddenWorld, question.Content.PublicScenario, nextState); err != nil {
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
	// 过程事件有独立的迁移闸门：默认 log，只记录协议分歧，不阻断已经通过
	// 回复安全校验的正文；off 才会完全跳过这一组复核，strict 可在联调时恢复。
	if s.scenarioPublicTraceValidationMode != scenarioValidationOff {
		if err := validateScenarioPublicTrace(input.RequestID, input.Content, result, question.Content.HiddenWorld, question.Content.PublicScenario, nextState); err != nil {
			if s.scenarioPublicTraceValidationMode == scenarioValidationLog {
				s.recordScenarioValidationBypass("public_trace", input.RequestID, err.Error())
			} else {
				return domain.ScenarioMessage{}, nil, scenarioAgentHTTPError{
					Status:  http.StatusBadGateway,
					Code:    "public_trace_rejected",
					Message: "排查服务返回了未通过公开协议校验的过程事件",
				}
			}
		}
	}
	// 流式影子模式下放行的违规统一在这里落审计（proposal 已在上方即时记录）。
	if streamValidator != nil {
		streamValidator.drainBypasses(s)
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
	runEvents := buildScenarioRunEvents(
		input.RequestID,
		result,
		reply,
		committedRevision,
		question.Content.HiddenWorld,
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
