package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
				{Sequence: 1, Kind: "reasoning_summary_completed", Summary: "已识别公开排查动作。"},
				{Sequence: 2, Kind: "guard_passed", Summary: "回复已通过安全校验。"},
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
			{Sequence: 1, Kind: "guard_passed", Summary: "回复已通过安全校验。"},
		},
		InternalVerification: agentclient.VerificationResult{Relation: "unknown", RuledOutThisTurn: []string{}},
		InternalAudit:        agentclient.AuditTrace{ReasonCodes: []string{}, RulesVersion: agentclient.ContractVersion},
	}
}
