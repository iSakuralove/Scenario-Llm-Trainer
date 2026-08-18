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
		"我先查看慢查询日志，确认订单列表 SQL 的耗时和 rows_examined",
		"继续看 EXPLAIN 执行计划，确认 type、key 和扫描行数",
		"再核对上午发布记录与 orders 表变更前后的 DDL 差异",
	} {
		status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", token, map[string]string{"content": content})
		if status != http.StatusOK {
			t.Fatalf("message status=%d message=%s", status, env.Message)
		}
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/answer", token, map[string]string{
		"answer": "orders 表结构变更后遗漏 idx_user_created (user_id, created_at) 联合索引，导致订单列表查询退化为全表扫描。",
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
	if report.OverallScore < 70 || report.EvidenceChainScore < 50 || report.ProcedureCoverageScore < 40 || report.RootCauseSimilarity < 80 {
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
		"answer": "orders 表结构变更后遗漏 idx_user_created (user_id, created_at) 联合索引，导致订单列表查询退化为全表扫描。",
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
	if report.RootCauseSimilarity < 80 || report.EvidenceChainScore >= 35 {
		t.Fatalf("expected a correct root-cause claim without a supporting evidence chain, got %+v", report)
	}
	if payload.Score.Total >= 50 {
		t.Fatalf("root guess without evidence must score materially below a strong process: %+v report=%+v", payload.Score, report)
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
