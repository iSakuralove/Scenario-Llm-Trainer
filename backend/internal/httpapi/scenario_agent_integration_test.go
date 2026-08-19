package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

type scenarioAgentClientFunc func(context.Context, agentclient.TurnRequest) (agentclient.TurnResult, error)

func (fn scenarioAgentClientFunc) Turn(ctx context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
	return fn(ctx, request)
}

func TestScenarioMessageCommitsApprovedAgentTurnAndKeepsPrivateAuditPrivate(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var calls atomic.Int32
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		calls.Add(1)
		if request.ContractVersion != agentclient.ContractVersion || request.StateRevision != 0 {
			t.Fatalf("unexpected request contract: %+v", request)
		}
		if request.HiddenWorld.RootCause.ID == "" || request.PublicScenario.Title == "" {
			t.Fatalf("missing HiddenWorld snapshot: %+v", request)
		}
		node, action := firstImmediatelyAvailableEvidence(t, request.HiddenWorld)
		reply := "先把这条公开观察确认清楚，再决定下一步。"
		return agentclient.TurnResult{
			ContractVersion:  agentclient.ContractVersion,
			RequestID:        request.RequestID,
			ExpectedRevision: request.StateRevision,
			Reply:            reply,
			TurnAnalysis: agentclient.TurnAnalysis{
				Actions:          []string{action},
				EstablishedFacts: []string{},
				StudentAffect:    "engaged",
				Confidence:       0.95,
			},
			Proposals: []agentclient.Proposal{
				{Kind: "release_evidence", EvidenceID: node.EvidenceID},
				{Kind: "record_action", Action: action},
				{Kind: "advance_effective_turn", Value: 1},
				{Kind: "set_stalled_turns", Value: 0},
				{Kind: "record_opening", Text: reply},
			},
			PublicTrace: []agentclient.PublicTraceEvent{
				{Sequence: 1, Kind: "reasoning_summary_completed", Status: "completed", Summary: "已识别公开排查动作。"},
				{Sequence: 2, Kind: "guard_passed", Status: "completed", Summary: "回复已通过安全校验。"},
			},
			InternalVerification: agentclient.VerificationResult{
				Relation:         "target",
				Coverage:         0.5,
				RuledOutThisTurn: []string{},
			},
			InternalAudit: agentclient.AuditTrace{
				ReasonCodes:     []string{"private_reason"},
				MentorRationale: "PRIVATE_MENTOR_RATIONALE",
				RulesVersion:    agentclient.ContractVersion,
			},
		}, nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content":        "先检查公开日志",
		"request_id":     "request-approved",
		"state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("message status=%d message=%s", status, env.Message)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one Python Agent call, got %d", calls.Load())
	}
	raw := string(env.Data)
	for _, forbidden := range []string{"PRIVATE_MENTOR_RATIONALE", "private_reason", `"relation":"target"`, "internal_verification", "internal_audit"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("public response leaked private audit %q: %s", forbidden, raw)
		}
	}
	session, ok := dataStore.GetScenarioSession(sessionID)
	if !ok || session.StateRevision != 1 || session.CurrentTurn != 1 || len(session.LearnerState.CollectedEvidence) != 1 {
		t.Fatalf("unexpected committed state: %+v", session)
	}
	record, ok := dataStore.GetScenarioAgentTurn(sessionID, "request-approved")
	if !ok || !strings.Contains(string(record.InternalAudit), "PRIVATE_MENTOR_RATIONALE") {
		t.Fatalf("private audit was not persisted independently: %+v", record)
	}
}

func TestScenarioMessageUsesConfiguredAgentDeadlineAndTimeout(t *testing.T) {
	t.Setenv("AGENT_TURN_DEADLINE_MS", "45000")
	t.Setenv("AGENT_TIMEOUT_SECONDS", "50")
	dataStore := store.NewMemoryStore(auth.HashPassword)
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		if request.Budget.DeadlineMS != 45000 {
			t.Fatalf("unexpected configured turn deadline: %+v", request.Budget)
		}
		return noProgressTurnResult(request, "配置化超时测试回复。"), nil
	})
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client)
	if server.scenarioAgentTimeout != 50*time.Second || server.scenarioTurnDeadlineMS != 45000 {
		t.Fatalf("unexpected agent timing settings: timeout=%s deadline_ms=%d", server.scenarioAgentTimeout, server.scenarioTurnDeadlineMS)
	}
	handler := server.Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content":        "验证配置化单轮预算",
		"request_id":     "request-configured-deadline",
		"state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("message status=%d message=%s", status, env.Message)
	}
}

func TestScenarioMessageReplaysSameRequestWithoutCallingAgentOrWritingAgain(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var calls atomic.Int32
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		calls.Add(1)
		return noProgressTurnResult(request, "幂等回复。"), nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)
	payload := map[string]any{"content": "同一轮内容", "request_id": "request-replay", "state_revision": 0}

	var messageIDs []string
	for index := 0; index < 2; index++ {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, payload)
		if status != http.StatusOK {
			t.Fatalf("request %d status=%d message=%s", index+1, status, env.Message)
		}
		var result struct {
			Message domain.ScenarioMessage `json:"message"`
		}
		mustDecodeData(t, env, &result)
		messageIDs = append(messageIDs, result.Message.ID)
	}
	if calls.Load() != 1 || messageIDs[0] == "" || messageIDs[0] != messageIDs[1] {
		t.Fatalf("expected exact replay, calls=%d ids=%v", calls.Load(), messageIDs)
	}
	if messages := dataStore.ListScenarioMessages(sessionID); len(messages) != 1 {
		t.Fatalf("replay wrote duplicate messages: %+v", messages)
	}
}

func TestScenarioMessageCoalescesConcurrentSameRequestBeforeAgentCommit(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return noProgressTurnResult(request, "并发重连只生成一次。"), nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)
	payload := map[string]any{"content": "同一轮并发重连", "request_id": "request-in-flight", "state_revision": 0}

	type response struct {
		status int
		env    testEnvelope
		err    error
	}
	responses := make(chan response, 2)
	rawBody, _ := json.Marshal(payload)
	request := func() {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", strings.NewReader(string(rawBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		var env testEnvelope
		err := json.Unmarshal(recorder.Body.Bytes(), &env)
		responses <- response{status: recorder.Code, env: env, err: err}
	}
	go request()
	<-started
	go request()
	time.Sleep(40 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("concurrent retry called Python Agent more than once before release: %d", calls.Load())
	}
	close(release)

	messageIDs := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		result := <-responses
		if result.err != nil {
			t.Fatalf("request %d decode response: %v", index+1, result.err)
		}
		if result.status != http.StatusOK {
			t.Fatalf("request %d status=%d message=%s", index+1, result.status, result.env.Message)
		}
		var payload struct {
			Message domain.ScenarioMessage `json:"message"`
		}
		mustDecodeData(t, result.env, &payload)
		messageIDs = append(messageIDs, payload.Message.ID)
	}
	if calls.Load() != 1 || messageIDs[0] == "" || messageIDs[0] != messageIDs[1] {
		t.Fatalf("expected one in-flight execution and exact replay, calls=%d ids=%v", calls.Load(), messageIDs)
	}
	if messages := dataStore.ListScenarioMessages(sessionID); len(messages) != 1 {
		t.Fatalf("concurrent retry wrote duplicate messages: %+v", messages)
	}
}

func TestScenarioMessageReconnectAfterClientCancellationReusesInFlightExecution(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	client := scenarioAgentClientFunc(func(ctx context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-ctx.Done():
			return agentclient.TurnResult{}, ctx.Err()
		case <-release:
			return noProgressTurnResult(request, "取消连接后仍只生成一次。"), nil
		}
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)
	rawBody, _ := json.Marshal(map[string]any{
		"content": "断线后重连", "request_id": "request-cancel-reconnect", "state_revision": 0,
	})

	type response struct {
		status int
		env    testEnvelope
		err    error
	}
	responses := make(chan response, 2)
	request := func(ctx context.Context) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", strings.NewReader(string(rawBody))).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		var env testEnvelope
		err := json.Unmarshal(recorder.Body.Bytes(), &env)
		responses <- response{status: recorder.Code, env: env, err: err}
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	go request(firstContext)
	<-started
	cancelFirst()
	time.Sleep(20 * time.Millisecond)
	go request(context.Background())
	time.Sleep(40 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("client cancellation caused duplicate Python Agent execution: %d", calls.Load())
	}
	close(release)

	messageIDs := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		result := <-responses
		if result.err != nil {
			t.Fatalf("request %d decode response: %v", index+1, result.err)
		}
		if result.status != http.StatusOK {
			t.Fatalf("request %d status=%d message=%s", index+1, result.status, result.env.Message)
		}
		var payload struct {
			Message domain.ScenarioMessage `json:"message"`
		}
		mustDecodeData(t, result.env, &payload)
		messageIDs = append(messageIDs, payload.Message.ID)
	}
	if calls.Load() != 1 || messageIDs[0] == "" || messageIDs[0] != messageIDs[1] {
		t.Fatalf("expected cancellation-safe in-flight replay, calls=%d ids=%v", calls.Load(), messageIDs)
	}
}

func TestScenarioMessageRejectsStaleRevisionBeforeCallingAgent(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var calls atomic.Int32
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		calls.Add(1)
		return noProgressTurnResult(request, "第一轮。"), nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "第一轮", "request_id": "request-first", "state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("first turn failed: %d %s", status, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "过期轮", "request_id": "request-stale", "state_revision": 0,
	})
	if status != http.StatusConflict || !strings.Contains(string(env.Data), "revision_conflict") {
		t.Fatalf("expected structured revision conflict, status=%d env=%+v", status, env)
	}
	if calls.Load() != 1 || len(dataStore.ListScenarioMessages(sessionID)) != 1 {
		t.Fatalf("stale turn must not call Agent or write state: calls=%d messages=%d", calls.Load(), len(dataStore.ListScenarioMessages(sessionID)))
	}
}

func TestScenarioMessageRejectsProposalBeforeAnyReplyDeltaIsPublished(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "UNSAFE_HALF_REPLY")
		result.Proposals = []agentclient.Proposal{{Kind: "release_evidence", EvidenceID: "E_NOT_IN_WORLD"}}
		return result, nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)
	body, _ := json.Marshal(map[string]any{"content": "测试越权", "request_id": "request-rejected", "state_revision": 0})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	raw := recorder.Body.String()
	if strings.Contains(raw, "UNSAFE_HALF_REPLY") || strings.Contains(raw, "event: delta") {
		t.Fatalf("rejected proposal leaked buffered reply: %s", raw)
	}
	if !strings.Contains(raw, "proposal_rejected") || len(dataStore.ListScenarioMessages(sessionID)) != 0 {
		t.Fatalf("expected structured rejection without writes: %s", raw)
	}
}

func TestScenarioMessageDoesNotFallbackWhenPythonAgentIsUnavailable(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	client := scenarioAgentClientFunc(func(context.Context, agentclient.TurnRequest) (agentclient.TurnResult, error) {
		return agentclient.TurnResult{}, errors.Join(agentclient.ErrAgentUnavailable, errors.New("dial failed"))
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "继续排查", "request_id": "request-unavailable", "state_revision": 0,
	})
	if status != http.StatusServiceUnavailable || !strings.Contains(string(env.Data), "agent_unavailable") {
		t.Fatalf("expected structured unavailable error, status=%d env=%+v", status, env)
	}
	if len(dataStore.ListScenarioMessages(sessionID)) != 0 {
		t.Fatal("unavailable Python Agent must not fall back to old Go reply")
	}
}

// stallProposalClient 前两轮无进展把 StalledTurns 顶到 2，第三轮提交一条卡住兜底释放。
func stallProposalClient(evidenceID func(domain.HiddenWorld) string) scenarioAgentClientFunc {
	var calls atomic.Int32
	return scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		turn := calls.Add(1)
		result := noProgressTurnResult(request, "我们先一起找一个能看的地方。")
		if turn < 3 {
			return result, nil
		}
		result.TurnAnalysis.IsStuck = true
		result.Proposals = append(
			[]agentclient.Proposal{{Kind: "release_evidence_on_stall", EvidenceID: evidenceID(request.HiddenWorld)}},
			result.Proposals...,
		)
		return result, nil
	})
}

func entryLevelEvidenceID(world domain.HiddenWorld) string {
	for _, node := range world.EvidenceGraph {
		if len(node.Prerequisites) == 0 {
			return node.EvidenceID
		}
	}
	return ""
}

func prerequisiteGatedEvidenceID(world domain.HiddenWorld) string {
	for _, node := range world.EvidenceGraph {
		if len(node.Prerequisites) > 0 {
			return node.EvidenceID
		}
	}
	return ""
}

func driveScenarioTurns(t *testing.T, handler http.Handler, token, sessionID string, contents []string) (int, testEnvelope) {
	t.Helper()
	var status int
	var env testEnvelope
	for index, content := range contents {
		status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
			"content":        content,
			"request_id":     "request-stall-" + strconv.Itoa(index),
			"state_revision": index,
		})
		if index < len(contents)-1 && status != http.StatusOK {
			t.Fatalf("turn %d failed: %d %s", index, status, env.Message)
		}
	}
	return status, env
}

func TestScenarioStallReleaseUnlocksEntryEvidenceForAStuckStudent(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	client := stallProposalClient(entryLevelEvidenceID)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := driveScenarioTurns(t, handler, token, sessionID, []string{
		"我不知道从哪看起", "还是没有头绪", "能不能给我点信息，我什么都不知道啊，我是菜鸟",
	})
	if status != http.StatusOK {
		t.Fatalf("stall release turn rejected: %d %s data=%s", status, env.Message, env.Data)
	}

	session, ok := dataStore.GetScenarioSession(sessionID)
	if !ok {
		t.Fatal("session missing")
	}
	if len(session.LearnerState.CollectedEvidence) != 1 {
		t.Fatalf("stuck student got no evidence: %+v", session.LearnerState.CollectedEvidence)
	}
	// 兜底释放是系统给的，不是学生挣来的：stalled_turns 继续累加，effective_turns 不动。
	if session.LearnerState.StalledTurns != 3 {
		t.Fatalf("stall release must not reset stalled turns, got %d", session.LearnerState.StalledTurns)
	}
	if session.LearnerState.EffectiveTurns != 0 {
		t.Fatalf("stall release must not advance effective turns, got %d", session.LearnerState.EffectiveTurns)
	}
}

func TestScenarioStallReleaseRejectedBeforeThresholdAndForGatedEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		turns    []string
		evidence func(domain.HiddenWorld) string
	}{
		// StalledTurns 只有 1，达不到阈值。Go 只认自己持有的计数，不认模型自报的 is_stuck。
		{name: "below threshold", turns: []string{"我不知道", "还是不知道"}, evidence: entryLevelEvidenceID},
		// 达到阈值但请求了有前置的节点：兜底只放入口级证据，不能跳过推理链。
		{name: "prerequisite gated", turns: []string{"我不知道", "还是不知道", "真的不会"}, evidence: prerequisiteGatedEvidenceID},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dataStore := store.NewMemoryStore(auth.HashPassword)
			var calls atomic.Int32
			target := len(testCase.turns)
			client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
				turn := int(calls.Add(1))
				result := noProgressTurnResult(request, "我们先一起找一个能看的地方。")
				if turn < target {
					return result, nil
				}
				result.TurnAnalysis.IsStuck = true
				result.Proposals = append(
					[]agentclient.Proposal{{Kind: "release_evidence_on_stall", EvidenceID: testCase.evidence(request.HiddenWorld)}},
					result.Proposals...,
				)
				return result, nil
			})
			handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
			token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

			status, env := driveScenarioTurns(t, handler, token, sessionID, testCase.turns)
			if status == http.StatusOK || !strings.Contains(string(env.Data), "proposal_rejected") {
				t.Fatalf("expected proposal_rejected, status=%d data=%s", status, env.Data)
			}
			session, _ := dataStore.GetScenarioSession(sessionID)
			if len(session.LearnerState.CollectedEvidence) != 0 {
				t.Fatalf("rejected stall release must not write evidence: %+v", session.LearnerState.CollectedEvidence)
			}
		})
	}
}

func TestScenarioMessageReconnectReplaysOnlyEventsAfterRequestedSequence(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	var calls atomic.Int32
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		calls.Add(1)
		return noProgressTurnResult(request, "断线后仍是同一条回复。"), nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)
	payload := map[string]any{
		"content": "测试重连", "request_id": "request-reconnect", "state_revision": 0,
	}
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, payload)
	if status != http.StatusOK {
		t.Fatalf("initial turn failed: %d %s", status, env.Message)
	}

	payload["after_sequence"] = 5
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if calls.Load() != 1 {
		t.Fatalf("reconnect must replay committed result without a second Agent call, got %d", calls.Load())
	}
	for _, event := range collectScenarioRunEvents(t, recorder.Body.String()) {
		if event.Sequence <= 5 {
			t.Fatalf("reconnect repeated acknowledged event: %+v", event)
		}
	}
}

func TestScenarioRunEventsExposeOnlyPublicCompareAnswerResult(t *testing.T) {
	result := agentclient.TurnResult{
		PublicTrace: []agentclient.PublicTraceEvent{
			{
				Sequence: 1,
				Kind:     "tool_result",
				Status:   "completed",
				Tool: &agentclient.ToolEventPayload{
					Name:              "compare_answer",
					RedactedArguments: map[string]string{"answer_attempt_id": "request-tool:answer"},
					DurationMS:        12,
					Result: &agentclient.PublicAnswerComparison{
						Tool:          "compare_answer",
						Status:        "completed",
						UserPoints:    []string{"索引可能缺失"},
						SupportStatus: "needs_more_evidence",
						NextAction:    "继续补充直接观察。",
					},
				},
			},
		},
	}
	events := buildScenarioRunEvents("request-tool", "我认为是索引问题", result, "继续验证。")
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"claim_alignment", "completion_allowed", "missing_evidence", `"correct"`, `"target"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public tool event leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"name":"compare_answer"`) || !strings.Contains(text, `"support_status":"needs_more_evidence"`) {
		t.Fatalf("missing public compare_answer payload: %s", text)
	}
}

func TestScenarioMessageRejectsPythonReplyDeltaInPublicTrace(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "安全的最终回复。")
		result.PublicTrace = []agentclient.PublicTraceEvent{
			{Sequence: 1, Kind: "reply_delta", Status: "running", Text: "UNSAFE_TRACE_REPLY"},
			{Sequence: 2, Kind: "guard_passed", Status: "completed", Summary: "回复已通过安全校验。"},
		}
		return result, nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)
	body, _ := json.Marshal(map[string]any{
		"content": "继续排查", "request_id": "request-trace-reply", "state_revision": 0,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	raw := recorder.Body.String()
	if strings.Contains(raw, "UNSAFE_TRACE_REPLY") || !strings.Contains(raw, "public_trace_rejected") {
		t.Fatalf("unsafe Python trace must be rejected before publication: %s", raw)
	}
	if len(dataStore.ListScenarioMessages(sessionID)) != 0 {
		t.Fatal("rejected public trace must not write a scenario message")
	}
}

func TestScenarioMessageRejectsFabricatedToolTraceOnOrdinaryTurn(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "普通轮回复。")
		result.PublicTrace = []agentclient.PublicTraceEvent{
			{
				Sequence: 1,
				Kind:     "tool_started",
				Status:   "started",
				ToolName: "compare_answer",
				Tool: &agentclient.ToolEventPayload{
					Name:              "compare_answer",
					RedactedArguments: map[string]string{"answer_attempt_id": request.RequestID + ":answer"},
				},
			},
			{Sequence: 2, Kind: "guard_passed", Status: "completed", Summary: "回复已通过安全校验。"},
		}
		return result, nil
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour), client).Handler()
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "先看看公开现象", "request_id": "request-fake-tool", "state_revision": 0,
	})
	if status != http.StatusBadGateway || !strings.Contains(string(env.Data), "public_trace_rejected") {
		t.Fatalf("ordinary turn must reject fabricated tool trace, status=%d env=%+v", status, env)
	}
	if len(dataStore.ListScenarioMessages(sessionID)) != 0 {
		t.Fatal("fabricated tool trace must not write a scenario message")
	}
}

func TestValidateScenarioPublicTraceAcceptsBoundCompareAnswerLifecycle(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	if question.Content.HiddenWorld == nil {
		t.Fatal("fixed HiddenWorld question is missing")
	}
	requestID := "request-valid-tool"
	userContent := "我认为目前的证据还需要继续验证索引问题"
	payload := &agentclient.ToolEventPayload{
		Name:              "compare_answer",
		RedactedArguments: map[string]string{"answer_attempt_id": requestID + ":answer"},
		DurationMS:        12,
		Result: &agentclient.PublicAnswerComparison{
			Tool:          "compare_answer",
			Status:        "completed",
			UserPoints:    []string{"我认为目前的证据还需要继续验证索引问题"},
			SupportStatus: "needs_more_evidence",
			NextAction:    "继续补充能支撑这个结论的直接观察。",
		},
	}
	result := agentclient.TurnResult{
		TurnAnalysis: agentclient.TurnAnalysis{ContainsAnswerAttempt: true, Confidence: 0.95},
		PublicTrace: []agentclient.PublicTraceEvent{
			{Sequence: 1, Kind: "tool_started", Status: "started", ToolName: "compare_answer", Tool: &agentclient.ToolEventPayload{Name: payload.Name, RedactedArguments: payload.RedactedArguments}},
			{Sequence: 2, Kind: "tool_result", Status: "completed", ToolName: "compare_answer", DurationMS: 12, Tool: payload},
			{Sequence: 3, Kind: "tool_completed", Status: "completed", ToolName: "compare_answer", DurationMS: 12, Tool: payload},
			{Sequence: 4, Kind: "guard_passed", Status: "completed", Summary: "回复已通过安全校验。"},
		},
	}
	if err := validateScenarioPublicTrace(requestID, userContent, result, question.Content.HiddenWorld, question.Content.PublicScenario, domain.ScenarioLearnerState{}); err != nil {
		t.Fatalf("valid compare_answer lifecycle was rejected: %v", err)
	}
}

func createScenarioSessionForAgentTest(t *testing.T, handler http.Handler, dataStore *store.MemoryStore) (string, string) {
	t.Helper()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]
	if question.Content.ModelVersion != domain.HiddenWorldContractVersion || question.Content.HiddenWorld == nil {
		t.Fatalf("expected fixed HiddenWorld question, got %+v", question.Content)
	}
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)
	return token, created.SessionID
}

func firstImmediatelyAvailableEvidence(t *testing.T, world domain.HiddenWorld) (domain.EvidenceNode, string) {
	t.Helper()
	for _, node := range world.EvidenceGraph {
		if len(node.Prerequisites) == 0 && len(node.ObtainedBy) > 0 {
			return node, node.ObtainedBy[0]
		}
	}
	t.Fatal("fixed world has no immediately available evidence")
	return domain.EvidenceNode{}, ""
}

func noProgressTurnResult(request agentclient.TurnRequest, reply string) agentclient.TurnResult {
	return agentclient.TurnResult{
		ContractVersion:  agentclient.ContractVersion,
		RequestID:        request.RequestID,
		ExpectedRevision: request.StateRevision,
		Reply:            reply,
		TurnAnalysis: agentclient.TurnAnalysis{
			Actions:          []string{},
			EstablishedFacts: []string{},
			StudentAffect:    "engaged",
			Confidence:       0.9,
		},
		Proposals: []agentclient.Proposal{
			{Kind: "set_stalled_turns", Value: request.LearnerState.StalledTurns + 1},
			{Kind: "record_opening", Text: reply},
		},
		PublicTrace: []agentclient.PublicTraceEvent{
			{Sequence: 1, Kind: "guard_passed", Status: "completed", Summary: "回复已通过安全校验。"},
		},
		InternalVerification: agentclient.VerificationResult{Relation: "unknown", RuledOutThisTurn: []string{}},
		InternalAudit:        agentclient.AuditTrace{ReasonCodes: []string{}, RulesVersion: agentclient.ContractVersion},
	}
}

func collectScenarioRunEvents(t *testing.T, raw string) []domain.ScenarioRunEvent {
	t.Helper()
	events := []domain.ScenarioRunEvent{}
	for _, block := range strings.Split(raw, "\n\n") {
		if !strings.HasPrefix(strings.TrimSpace(block), "event: run_event") {
			continue
		}
		_, data, ok := strings.Cut(block, "data: ")
		if !ok {
			continue
		}
		var event domain.ScenarioRunEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &event); err != nil {
			t.Fatalf("decode run event: %v", err)
		}
		events = append(events, event)
	}
	return events
}
