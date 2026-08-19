package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"situational-teaching/backend/internal/domain"
)

func TestClientTurnSendsVersionedRequestAndDecodesTypedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/turn" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var request TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ContractVersion != ContractVersion || request.RequestID != "request-1" {
			t.Fatalf("unexpected request contract: %+v", request)
		}
		if request.PublicScenario.Title != "订单查询变慢" || request.HiddenWorld.RootCause.ID != "RC_INDEX" {
			t.Fatalf("missing HiddenWorld request data: %+v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TurnResult{
			ContractVersion:  ContractVersion,
			RequestID:        request.RequestID,
			ExpectedRevision: request.StateRevision,
			Reply:            "先检查慢查询。",
			TurnAnalysis: TurnAnalysis{
				Actions:               []string{"inspect:logs.slow_query"},
				HypothesisID:          "",
				HypothesisRaw:         "",
				MadeClaim:             false,
				ContainsAnswerAttempt: false,
				AnswerAttemptText:     "",
				EstablishedFacts:      []string{},
				IsStuck:               false,
				IsNoise:               false,
				StudentAffect:         "engaged",
				Confidence:            0.95,
			},
			Proposals:   []Proposal{{Kind: "record_action", Action: "inspect:logs.slow_query"}},
			PublicTrace: []PublicTraceEvent{},
			InternalVerification: VerificationResult{
				Relation: "unknown",
				Coverage: 0,
			},
			InternalAudit: AuditTrace{RulesVersion: ContractVersion},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.Turn(context.Background(), TurnRequest{
		RequestID:      "request-1",
		SessionID:      "session-1",
		StateRevision:  3,
		PublicScenario: domain.PublicScenario{Title: "订单查询变慢", Description: "接口变慢"},
		HiddenWorld: domain.HiddenWorld{RootCause: domain.RootCause{
			ID: "RC_INDEX", Description: "索引缺失",
		}},
		UserMessage: "先看慢查询",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "先检查慢查询。" || result.ExpectedRevision != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Proposals) != 1 || result.Proposals[0].Action != "inspect:logs.slow_query" {
		t.Fatalf("unexpected proposals: %+v", result.Proposals)
	}
}

func TestClientTurnStreamForwardsTypedEventsAndReturnsFinalResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/turn/stream" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var request TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeAgentSSE(t, w, "turn_analysis", TurnAnalysis{
			ContainsAnswerAttempt: true,
			AnswerAttemptText:     request.UserMessage,
			StudentAffect:         "engaged",
			Confidence:            0.95,
		})
		writeAgentSSE(t, w, "public_trace", PublicTraceEvent{
			Sequence: 1,
			Kind:     "tool_started",
			Status:   "started",
			ToolName: "compare_answer",
			Tool: &ToolEventPayload{
				Name:              "compare_answer",
				RedactedArguments: map[string]string{"answer_attempt_id": request.RequestID + ":answer"},
			},
		})
		result := validTurnResult(request.RequestID, request.StateRevision)
		result.TurnAnalysis.ContainsAnswerAttempt = true
		result.TurnAnalysis.AnswerAttemptText = request.UserMessage
		result.PublicTrace = []PublicTraceEvent{{
			Sequence: 1,
			Kind:     "tool_started",
			Status:   "started",
			ToolName: "compare_answer",
			Tool: &ToolEventPayload{
				Name:              "compare_answer",
				RedactedArguments: map[string]string{"answer_attempt_id": request.RequestID + ":answer"},
			},
		}}
		writeAgentSSE(t, w, "result", result)
	}))
	defer server.Close()

	var analysis TurnAnalysis
	events := []PublicTraceEvent{}
	result, err := New(Config{BaseURL: server.URL, Timeout: time.Second}).TurnStream(
		context.Background(),
		minimalTurnRequest("request-stream", 4, LearnerState{}),
		StreamCallbacks{
			OnTurnAnalysis: func(value TurnAnalysis) error {
				analysis = value
				return nil
			},
			OnPublicTrace: func(event PublicTraceEvent) error {
				events = append(events, event)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.ContainsAnswerAttempt || len(events) != 1 || events[0].Kind != "tool_started" {
		t.Fatalf("stream callbacks did not receive typed events: analysis=%+v events=%+v", analysis, events)
	}
	if result.RequestID != "request-stream" || result.ExpectedRevision != 4 {
		t.Fatalf("unexpected streamed result: %+v", result)
	}
}

func TestClientTurnStreamReturnsStructuredErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAgentSSE(t, w, "error", map[string]string{
			"code": "model_not_configured", "message": "real model requests are disabled",
		})
	}))
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL, Timeout: time.Second}).TurnStream(
		context.Background(),
		minimalTurnRequest("request-stream-error", 0, LearnerState{}),
		StreamCallbacks{},
	)
	var httpErr HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != "model_not_configured" {
		t.Fatalf("expected structured stream error, got %v", err)
	}
}

func TestClientTurnNormalizesEveryLearnerStateSlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		state := payload["learner_state"].(map[string]any)
		for _, name := range []string{
			"collected_evidence",
			"ruled_out_hypotheses",
			"established_facts",
			"actions_taken",
			"recent_openings",
		} {
			if value, ok := state[name].([]any); !ok || value == nil {
				t.Fatalf("%s must be encoded as an array, got %#v", name, state[name])
			}
		}
		writeTurnResult(t, w, "request-normalize", 0)
	}))
	defer server.Close()

	state := LearnerState{CollectedEvidence: []string{"E_ONE"}}
	_, err := New(Config{BaseURL: server.URL}).Turn(context.Background(), minimalTurnRequest("request-normalize", 0, state))
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientTurnRejectsContractVersionMismatch(t *testing.T) {
	server := resultServer(t, func(result *TurnResult) {
		result.ContractVersion = "hiddenworld.v0"
	})
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).Turn(context.Background(), minimalTurnRequest("request-version", 1, LearnerState{}))
	var versionErr ContractVersionError
	if !errors.As(err, &versionErr) || versionErr.Received != "hiddenworld.v0" {
		t.Fatalf("expected ContractVersionError, got %v", err)
	}
}

func TestClientTurnRejectsRequestIDMismatch(t *testing.T) {
	server := resultServer(t, func(result *TurnResult) {
		result.RequestID = "another-request"
	})
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).Turn(context.Background(), minimalTurnRequest("request-id", 1, LearnerState{}))
	if err == nil || !strings.Contains(err.Error(), "request_id mismatch") {
		t.Fatalf("expected request_id mismatch, got %v", err)
	}
}

func TestClientTurnRejectsRevisionMismatch(t *testing.T) {
	server := resultServer(t, func(result *TurnResult) {
		result.ExpectedRevision++
	})
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).Turn(context.Background(), minimalTurnRequest("request-revision", 3, LearnerState{}))
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") {
		t.Fatalf("expected revision mismatch, got %v", err)
	}
}

func TestClientTurnRejectsUnknownMissingAndTrailingFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown", body: validResultJSON("request-shape", 1, `,"surprise":true`), want: "unknown field"},
		{name: "missing", body: `{"contract_version":"hiddenworld.v1","request_id":"request-shape"}`, want: "required field"},
		{name: "trailing", body: validResultJSON("request-shape", 1, ``) + ` {}`, want: "after top-level value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := New(Config{BaseURL: server.URL}).Turn(context.Background(), minimalTurnRequest("request-shape", 1, LearnerState{}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestClientTurnParsesFastAPIErrorAndFourXXDoesNotOpenCircuit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":{"code":"contract_version_mismatch","message":"version mismatch"}}`))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, FailureThreshold: 1, OpenDuration: time.Minute})

	for index := 0; index < 2; index++ {
		_, err := client.Turn(context.Background(), minimalTurnRequest(fmt.Sprintf("request-4xx-%d", index), 0, LearnerState{}))
		var httpErr HTTPError
		if !errors.As(err, &httpErr) || httpErr.Code != "contract_version_mismatch" || httpErr.Message != "version mismatch" {
			t.Fatalf("unexpected FastAPI error: %#v (%v)", httpErr, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("4xx must not open circuit, calls=%d", calls.Load())
	}
}

func TestClientTurnOpensCircuitAfterConsecutiveServerFailures(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, FailureThreshold: 2, OpenDuration: time.Minute})

	for index := 0; index < 2; index++ {
		_, err := client.Turn(context.Background(), minimalTurnRequest(fmt.Sprintf("request-5xx-%d", index), 0, LearnerState{}))
		var httpErr HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %v", err)
		}
	}
	_, err := client.Turn(context.Background(), minimalTurnRequest("request-short-circuit", 0, LearnerState{}))
	if !errors.Is(err, ErrCircuitOpen) || calls.Load() != 2 {
		t.Fatalf("expected short circuit after two failures, calls=%d err=%v", calls.Load(), err)
	}
}

func TestClientTurnSuccessResetsFailureCount(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 || call == 3 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		var request TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		writeTurnResult(t, w, request.RequestID, request.StateRevision)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, FailureThreshold: 2, OpenDuration: time.Minute})

	requests := []string{"failure-1", "success", "failure-2", "success-after-reset"}
	for index, requestID := range requests {
		_, err := client.Turn(context.Background(), minimalTurnRequest(requestID, 0, LearnerState{}))
		if index == 0 || index == 2 {
			if err == nil {
				t.Fatalf("call %d should fail", index+1)
			}
			continue
		}
		if err != nil {
			t.Fatalf("call %d should succeed after reset: %v", index+1, err)
		}
	}
}

func TestClientTurnClassifiesTimeoutAndCountsItForCircuit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	client := New(Config{
		BaseURL:          server.URL,
		Timeout:          10 * time.Millisecond,
		FailureThreshold: 1,
		OpenDuration:     time.Minute,
		HTTPClient:       &http.Client{},
	})

	_, err := client.Turn(context.Background(), minimalTurnRequest("request-timeout", 0, LearnerState{}))
	if !errors.Is(err, ErrRequestTimeout) {
		t.Fatalf("expected timeout classification, got %v", err)
	}
	_, err = client.Turn(context.Background(), minimalTurnRequest("request-timeout-short-circuit", 0, LearnerState{}))
	if !errors.Is(err, ErrCircuitOpen) || calls.Load() != 1 {
		t.Fatalf("timeout should open circuit, calls=%d err=%v", calls.Load(), err)
	}
}

func minimalTurnRequest(requestID string, revision int, state LearnerState) TurnRequest {
	return TurnRequest{
		RequestID:      requestID,
		SessionID:      "session-1",
		StateRevision:  revision,
		PublicScenario: domain.PublicScenario{Title: "订单查询变慢", Description: "接口变慢"},
		HiddenWorld: domain.HiddenWorld{RootCause: domain.RootCause{
			ID: "RC_INDEX", Description: "索引缺失",
		}},
		LearnerState: state,
		UserMessage:  "先看慢查询",
	}
}

func resultServer(t *testing.T, mutate func(*TurnResult)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		result := validTurnResult(request.RequestID, request.StateRevision)
		mutate(&result)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			t.Fatal(err)
		}
	}))
}

func writeTurnResult(t *testing.T, w http.ResponseWriter, requestID string, revision int) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(validTurnResult(requestID, revision)); err != nil {
		t.Fatal(err)
	}
}

func writeAgentSSE(t *testing.T, w http.ResponseWriter, name string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		t.Fatal(err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func validTurnResult(requestID string, revision int) TurnResult {
	return TurnResult{
		ContractVersion:  ContractVersion,
		RequestID:        requestID,
		ExpectedRevision: revision,
		Reply:            "继续检查。",
		TurnAnalysis: TurnAnalysis{
			Actions:          []string{},
			EstablishedFacts: []string{},
			StudentAffect:    "engaged",
			Confidence:       0.9,
		},
		Proposals:   []Proposal{},
		PublicTrace: []PublicTraceEvent{},
		InternalVerification: VerificationResult{
			Relation:         "unknown",
			RuledOutThisTurn: []string{},
		},
		InternalAudit: AuditTrace{ReasonCodes: []string{}, RulesVersion: ContractVersion},
	}
}

func validResultJSON(requestID string, revision int, suffix string) string {
	result, err := json.Marshal(validTurnResult(requestID, revision))
	if err != nil {
		panic(err)
	}
	value := strings.TrimSuffix(string(result), "}")
	return value + suffix + "}"
}
