package store

import (
	"encoding/json"
	"errors"
	"testing"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
)

func TestMemoryStoreScenarioAgentTurnIsAtomicIdempotentAndRevisioned(t *testing.T) {
	dataStore := NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	session, err := dataStore.CreateScenarioSession("user-demo", question.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := *session
	next.CurrentTurn = 1
	next.LearnerState.CollectedEvidence = []string{"E_SLOW_QUERY"}
	commit := domain.ScenarioAgentTurnCommit{
		SessionID:          session.ID,
		RequestID:          "request-1",
		RequestFingerprint: "fingerprint-1",
		ExpectedRevision:   0,
		Message: domain.ScenarioMessage{
			SessionID:        session.ID,
			TurnNumber:       1,
			Role:             "assistant",
			UserContent:      "先看慢查询",
			AssistantContent: "可以，先确认慢查询范围。",
		},
		NextSession:          next,
		PublicTrace:          json.RawMessage(`[]`),
		InternalVerification: json.RawMessage(`{"relation":"unknown"}`),
		InternalAudit:        json.RawMessage(`{"reason_codes":[]}`),
		ApprovalAudit:        json.RawMessage(`[]`),
	}

	first, err := dataStore.CommitScenarioAgentTurn(commit)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Record.CommittedRevision != 1 || first.Record.SessionSnapshot.StateRevision != 1 {
		t.Fatalf("unexpected first commit: %+v", first)
	}
	if len(dataStore.ListScenarioMessages(session.ID)) != 1 {
		t.Fatal("message and state must be committed together")
	}

	replay, err := dataStore.CommitScenarioAgentTurn(commit)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Record.Message.ID != first.Record.Message.ID {
		t.Fatalf("expected exact replay, got %+v", replay)
	}
	if len(dataStore.ListScenarioMessages(session.ID)) != 1 {
		t.Fatal("idempotent replay must not append another message")
	}

	conflictingRequest := commit
	conflictingRequest.RequestFingerprint = "different"
	if _, err := dataStore.CommitScenarioAgentTurn(conflictingRequest); err == nil {
		t.Fatal("reusing request_id with different content must fail")
	} else {
		var conflict domain.ScenarioRequestConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("expected request conflict, got %v", err)
		}
	}

	stale := commit
	stale.RequestID = "request-stale"
	stale.RequestFingerprint = "fingerprint-stale"
	if _, err := dataStore.CommitScenarioAgentTurn(stale); err == nil {
		t.Fatal("stale revision must fail")
	} else {
		var conflict domain.ScenarioRevisionConflictError
		if !errors.As(err, &conflict) || conflict.Current != 1 {
			t.Fatalf("expected revision conflict at revision 1, got %v", err)
		}
	}
	if len(dataStore.ListScenarioMessages(session.ID)) != 1 {
		t.Fatal("revision conflict must not partially write a message")
	}
}
