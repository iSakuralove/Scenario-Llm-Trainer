package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

// newServerForValidationTest 构造指定验证模式的服务端。
func newServerForValidationTest(t *testing.T, mode scenarioValidationMode, client scenarioAgentClient) (*Server, http.Handler, *store.MemoryStore) {
	t.Helper()
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", 3600), client)
	server.scenarioValidationMode = mode
	return server, server.Handler(), dataStore
}

func TestScenarioValidationModeFromEnvParsesKnownValues(t *testing.T) {
	cases := map[string]scenarioValidationMode{
		"":         scenarioValidationStrict,
		"   ":      scenarioValidationStrict,
		"strict":   scenarioValidationStrict,
		"STRICT":   scenarioValidationStrict,
		" Log ":    scenarioValidationLog,
		"OFF":      scenarioValidationOff,
		"garbage":  scenarioValidationStrict,
		"shadow":   scenarioValidationStrict,
		"disablex": scenarioValidationStrict,
	}
	original := getenvValue
	defer func() { getenvValue = original }()
	for raw, expected := range cases {
		getenvValue = func(string) string { return raw }
		if got := scenarioValidationModeFromEnv(); got != expected {
			t.Fatalf("env %q: expected %s, got %s", raw, expected, got)
		}
	}
}

func TestScenarioValidationDefaultServerIsStrict(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", 3600))
	if server.scenarioValidationMode != scenarioValidationStrict {
		t.Fatalf("tests must default to strict, got %s", server.scenarioValidationMode)
	}
}

// log 模式：坏 proposal（世界图里不存在的证据）不再炸整轮，
// 被拒提议进审批审计（accepted=false），回复照常落库。
func TestScenarioLogModeSurvivesInvalidProposalAndAuditsIt(t *testing.T) {
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "我们先看看服务状态。")
		result.Proposals = []agentclient.Proposal{
			{Kind: "release_evidence", EvidenceID: "E_NOT_IN_WORLD"},
			{Kind: "set_stalled_turns", Value: request.LearnerState.StalledTurns + 1},
			{Kind: "record_opening", Text: "我们先看看服务状态。"},
		}
		return result, nil
	})
	_, handler, dataStore := newServerForValidationTest(t, scenarioValidationLog, client)
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "我打算先看服务状态", "request_id": "req-log-proposal", "state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("log mode must survive invalid proposal, status=%d message=%s", status, env.Message)
	}
	messages := dataStore.ListScenarioMessages(sessionID)
	if len(messages) != 1 {
		t.Fatalf("expected committed message, got %d", len(messages))
	}
	session, ok := dataStore.GetScenarioSession(sessionID)
	if !ok {
		t.Fatal("session missing")
	}
	for _, evidence := range session.LearnerState.CollectedEvidence {
		if evidence == "E_NOT_IN_WORLD" {
			t.Fatal("soft-rejected proposal must not mutate state")
		}
	}
	bypass := 0
	for _, event := range dataStore.ListAuditEvents(100) {
		if event.Action == "scenario.validation_bypassed" {
			bypass++
			if event.Metadata["validator"] != "proposal" {
				t.Fatalf("expected proposal validator, got %q", event.Metadata["validator"])
			}
		}
	}
	if bypass == 0 {
		t.Fatal("expected scenario.validation_bypassed audit event for soft-rejected proposal")
	}
}

// log 模式：回复包含未释放实体（剧透）时不再拦截整轮，落审计。
func TestScenarioLogModeSurvivesSpoilerReply(t *testing.T) {
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		// 直接把隐藏根因描述抄进回复，strict 下必被 reply guard 拦截。
		return noProgressTurnResult(request, "根因是："+request.HiddenWorld.RootCause.Description), nil
	})
	_, handler, dataStore := newServerForValidationTest(t, scenarioValidationLog, client)
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "给我点提示", "request_id": "req-log-spoiler", "state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("log mode must survive spoiler reply, status=%d message=%s", status, env.Message)
	}
	foundGuardBypass := false
	for _, event := range dataStore.ListAuditEvents(100) {
		if event.Action == "scenario.validation_bypassed" && event.Metadata["validator"] == "reply_guard" {
			foundGuardBypass = true
		}
	}
	if !foundGuardBypass {
		t.Fatal("expected reply_guard bypass audit event")
	}
}

// log 模式：协议外 trace 事件（Python 伪造 reply_delta）不再拦截，落审计。
func TestScenarioLogModeSurvivesProtocolViolationTrace(t *testing.T) {
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "先看入口网关。")
		result.PublicTrace = append(result.PublicTrace, agentclient.PublicTraceEvent{
			Sequence: 2,
			Kind:     "reply_delta",
			Status:   "completed",
			Text:     "伪造回复",
		})
		return result, nil
	})
	_, handler, dataStore := newServerForValidationTest(t, scenarioValidationLog, client)
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "继续", "request_id": "req-log-trace", "state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("log mode must survive protocol violation, status=%d message=%s", status, env.Message)
	}
	foundTraceBypass := false
	for _, event := range dataStore.ListAuditEvents(100) {
		if event.Action == "scenario.validation_bypassed" && strings.HasPrefix(event.Metadata["validator"], "public_trace") {
			foundTraceBypass = true
		}
	}
	if !foundTraceBypass {
		t.Fatal("expected public_trace bypass audit event")
	}
}

// off 模式：世界图外的证据直接放行进状态（闸门全开），无审计噪音。
func TestScenarioOffModeSkipsGatesEntirely(t *testing.T) {
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "先看数据库连接池。")
		result.Proposals = []agentclient.Proposal{
			{Kind: "release_evidence", EvidenceID: "E_NOT_IN_WORLD"},
			{Kind: "set_stalled_turns", Value: request.LearnerState.StalledTurns + 1},
			{Kind: "record_opening", Text: "先看数据库连接池。"},
		}
		result.Reply = "根因是：" + request.HiddenWorld.RootCause.Description
		result.Proposals[2].Text = "根因是：" + request.HiddenWorld.RootCause.Description
		return result, nil
	})
	_, handler, dataStore := newServerForValidationTest(t, scenarioValidationOff, client)
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "我猜是数据库", "request_id": "req-off", "state_revision": 0,
	})
	if status != http.StatusOK {
		t.Fatalf("off mode must not gate, status=%d message=%s", status, env.Message)
	}
	session, ok := dataStore.GetScenarioSession(sessionID)
	if !ok {
		t.Fatal("session missing")
	}
	found := false
	for _, evidence := range session.LearnerState.CollectedEvidence {
		if evidence == "E_NOT_IN_WORLD" {
			found = true
		}
	}
	if !found {
		t.Fatal("off mode must apply ungated evidence release")
	}
	for _, event := range dataStore.ListAuditEvents(100) {
		if event.Action == "scenario.validation_bypassed" {
			t.Fatal("off mode must not emit bypass audit noise")
		}
	}
}

// strict 模式回归：既有拦截行为一字不差（坏 proposal 仍是 502 proposal_rejected）。
func TestScenarioStrictModeStillRejectsInvalidProposal(t *testing.T) {
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "UNSAFE_HALF_REPLY")
		result.Proposals = []agentclient.Proposal{{Kind: "release_evidence", EvidenceID: "E_NOT_IN_WORLD"}}
		return result, nil
	})
	_, handler, dataStore := newServerForValidationTest(t, scenarioValidationStrict, client)
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", token, map[string]any{
		"content": "测试越权", "request_id": "req-strict", "state_revision": 0,
	})
	if status != http.StatusBadGateway || !strings.Contains(string(env.Data), "proposal_rejected") {
		t.Fatalf("strict mode must keep rejecting, status=%d env=%+v", status, env)
	}
	if len(dataStore.ListScenarioMessages(sessionID)) != 0 {
		t.Fatal("strict rejection must not write messages")
	}
}

// log 模式流式路径：流中协议违规不再中断 delta 流。
func TestScenarioLogModeStreamingKeepsDeltasFlowing(t *testing.T) {
	client := scenarioAgentClientFunc(func(_ context.Context, request agentclient.TurnRequest) (agentclient.TurnResult, error) {
		result := noProgressTurnResult(request, "我们一步步来，先确认服务在线。")
		// 序号回退：流式校验器会当场报错，log 模式应继续消费。
		result.PublicTrace = append(result.PublicTrace,
			agentclient.PublicTraceEvent{Sequence: 3, Kind: "response_summary", Status: "completed", Summary: "导师已组织回复。"},
			agentclient.PublicTraceEvent{Sequence: 3, Kind: "mentor_buffered", Status: "completed", Summary: "回复已缓冲。"},
		)
		return result, nil
	})
	_, handler, dataStore := newServerForValidationTest(t, scenarioValidationLog, client)
	token, sessionID := createScenarioSessionForAgentTest(t, handler, dataStore)

	body, _ := json.Marshal(map[string]any{"content": "开始排查", "request_id": "req-log-stream", "state_revision": 0})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionID+"/messages", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	raw := recorder.Body.String()
	if !strings.Contains(raw, "我们一步步来") {
		t.Fatalf("log mode streaming must still deliver reply: %s", raw)
	}
	if strings.Contains(raw, "public_trace_rejected") {
		t.Fatalf("log mode must not reject trace mid-stream: %s", raw)
	}
	found := false
	for _, event := range dataStore.ListAuditEvents(100) {
		if event.Action == "scenario.validation_bypassed" && strings.HasPrefix(event.Metadata["validator"], "public_trace") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected stream bypass audit")
	}
}

var _ = domain.ScenarioLearnerState{}
