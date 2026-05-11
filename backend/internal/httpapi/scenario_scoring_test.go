package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestScenarioEvaluationIncludesEvidenceChainScoringReport(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	for _, content := range []string{
		"我先确认接口耗时是否集中在数据库阶段，并查看慢查询日志 rows_examined 是否升高",
		"继续看 EXPLAIN 执行计划，确认 possible_keys 和 type 是否异常",
		"再核对最近发布是否新增 status 和 created_at 筛选条件",
	} {
		status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", token, map[string]string{"content": content})
		if status != http.StatusOK {
			t.Fatalf("message status=%d message=%s", status, env.Message)
		}
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/answer", token, map[string]string{
		"answer": "根因是新增 status 与 created_at 筛选后没有补联合索引，导致订单查询全表扫描并回表。",
	})
	if status != http.StatusOK {
		t.Fatalf("answer status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Result domain.ScenarioEvaluation `json:"result"`
		Score  domain.ScenarioScore      `json:"score"`
	}
	mustDecodeData(t, env, &payload)

	report := payload.Result.ScoringReport
	if report == nil {
		t.Fatal("expected scoring_report")
	}
	if report.OverallScore != payload.Score.Total {
		t.Fatalf("report total and score total diverged: report=%+v score=%+v", report, payload.Score)
	}
	if report.EvidenceChainScore < 60 || report.ProcedureCoverageScore < 40 || report.RootCauseSimilarity < 80 {
		t.Fatalf("expected strong process scoring report, got %+v", report)
	}
	if len(report.EvidenceEvents) < 3 || len(report.MatchedDocuments) == 0 {
		t.Fatalf("expected evidence events and matches, got %+v", report)
	}
}

func TestScenarioEvaluationPenalizesRootGuessWithoutEvidenceChain(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/answer", token, map[string]string{
		"answer": "根因是缺少联合索引导致全表扫描并回表。",
	})
	if status != http.StatusOK {
		t.Fatalf("answer status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Result domain.ScenarioEvaluation `json:"result"`
		Score  domain.ScenarioScore      `json:"score"`
	}
	mustDecodeData(t, env, &payload)

	report := payload.Result.ScoringReport
	if report == nil {
		t.Fatal("expected scoring_report")
	}
	if len(report.Penalties) == 0 {
		t.Fatalf("expected evidence-chain penalty, got %+v", report)
	}
	if payload.Score.Total >= 90 {
		t.Fatalf("root guess without evidence must not get near-perfect score: %+v report=%+v", payload.Score, report)
	}
}

func TestScenarioScoringUsesVectorStoreWhenAvailable(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	question := dataStore.ListScenarios("database", "", "")[0]
	vectorStore := store.NewMemoryVectorStore()
	vectorDoc := question.Content.KeyEvidence[0]
	if vectorDoc == "" {
		t.Fatal("seed scenario must contain evidence")
	}
	if err := vectorStore.UpsertDocuments(context.Background(), []ai.ScenarioVectorDocument{
		{
			QuestionID:    question.ID,
			SourceVersion: question.Version,
			DocType:       ai.VectorDocEvidence,
			DocKey:        "vector-only-evidence",
			DocText:       vectorDoc,
			TextHash:      "vector-only-hash",
			Status:        "active",
		},
	}); err != nil {
		t.Fatalf("upsert vector docs: %v", err)
	}

	_, report, _ := scoreScenarioWithEvidenceChain(scenarioScoringInput{
		Question: &question,
		Messages: []domain.ScenarioMessage{
			{TurnNumber: 1, UserContent: vectorDoc},
		},
		Answer:      "我还没有最终结论",
		VectorStore: vectorStore,
		CurrentTurn: 1,
	})

	if report == nil {
		t.Fatal("expected scoring report")
	}
	found := false
	for _, match := range report.MatchedDocuments {
		if match.DocKey == "vector-only-evidence" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scoring to use vector store document, got matches=%+v", report.MatchedDocuments)
	}
}
