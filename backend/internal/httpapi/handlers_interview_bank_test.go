package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestAdminInterviewBankRequiresAdminAndValidateHasNoSideEffect(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/summary", demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student interview bank summary code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/import/validate", adminToken, validInterviewBankImportPayload("batch-validate", "atom-validate-only"))
	if status != http.StatusOK {
		t.Fatalf("validate status=%d message=%s", status, env.Message)
	}
	var report struct {
		Summary map[string]int `json:"summary"`
		Errors  []string       `json:"errors"`
	}
	mustDecodeData(t, env, &report)
	if report.Summary["valid_count"] != 1 || report.Summary["error_count"] != 0 {
		t.Fatalf("unexpected validate report: %+v", report)
	}
	if _, ok := dataStore.GetInterviewKnowledgeAtom("atom-validate-only"); ok {
		t.Fatal("validate must not persist interview knowledge atom")
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/import/validate", adminToken, map[string]interface{}{
		"items": []map[string]interface{}{{"id": "broken"}},
	})
	if status != http.StatusOK {
		t.Fatalf("invalid validate should still return report, status=%d message=%s", status, env.Message)
	}
	mustDecodeData(t, env, &report)
	if report.Summary["error_count"] == 0 || len(report.Errors) == 0 {
		t.Fatalf("expected validation errors, got %+v", report)
	}
	if _, ok := dataStore.GetInterviewKnowledgeAtom("broken"); ok {
		t.Fatal("invalid validate must not persist atom")
	}
}

func TestAdminInterviewBankOpsActionsCreateAndList(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions", adminToken, map[string]interface{}{
		"action_type": "fill_gap",
		"priority":    "P1",
		"title":       "补齐后端缓存 L3 追问题",
		"reason":      "真实面试检索多次回退，需要补充追问资源。",
		"domain":      "backend",
		"category":    "cache",
		"difficulty":  "L3",
		"evidence": map[string]interface{}{
			"fallback_count": float64(3),
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create ops action status=%d message=%s", status, env.Message)
	}
	var created struct {
		Action domain.InterviewBankOpsAction `json:"action"`
	}
	mustDecodeData(t, env, &created)
	if created.Action.ID == "" || created.Action.Status != domain.InterviewBankOpsActionStatusOpen {
		t.Fatalf("expected created open action, got %+v", created.Action)
	}
	if created.Action.Source != domain.InterviewBankOpsActionSourceManual || created.Action.CreatedBy != "user-admin" {
		t.Fatalf("expected manual admin action, got %+v", created.Action)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions?status=open&type=fill_gap&domain=backend&category=cache&difficulty=L3", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list ops actions status=%d message=%s", status, env.Message)
	}
	var listed struct {
		List  []domain.InterviewBankOpsAction `json:"list"`
		Total int                             `json:"total"`
	}
	mustDecodeData(t, env, &listed)
	if listed.Total != 1 || len(listed.List) != 1 {
		t.Fatalf("expected one ops action, got %+v", listed)
	}
	if listed.List[0].ID != created.Action.ID || listed.List[0].Title != "补齐后端缓存 L3 追问题" {
		t.Fatalf("unexpected listed action: %+v", listed.List[0])
	}
}

func TestAdminInterviewBankOpsActionsRequireAdmin(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions", demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student ops action list code=%d", env.Code)
	}
	_, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions", demoToken, map[string]interface{}{
		"action_type": "fill_gap",
		"priority":    "P1",
		"title":       "补题动作",
		"reason":      "回退次数过高。",
		"domain":      "backend",
		"category":    "cache",
		"difficulty":  "L3",
	})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student ops action create code=%d", env.Code)
	}
}

func TestAdminInterviewBankOpsActionsValidateCreateRequest(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	validPayload := map[string]interface{}{
		"action_type": "fill_gap",
		"priority":    "P1",
		"title":       "补齐后端缓存 L3 追问题",
		"reason":      "真实面试检索多次回退。",
		"domain":      "backend",
		"category":    "cache",
		"difficulty":  "L3",
	}

	cases := []struct {
		name    string
		mutate  func(map[string]interface{})
		message string
	}{
		{
			name:    "missing title",
			mutate:  func(payload map[string]interface{}) { payload["title"] = "" },
			message: "title is required",
		},
		{
			name:    "missing reason",
			mutate:  func(payload map[string]interface{}) { payload["reason"] = " " },
			message: "reason is required",
		},
		{
			name:    "invalid type",
			mutate:  func(payload map[string]interface{}) { payload["action_type"] = "repair_everything" },
			message: "invalid action_type",
		},
		{
			name:    "invalid priority",
			mutate:  func(payload map[string]interface{}) { payload["priority"] = "urgent" },
			message: "invalid priority",
		},
		{
			name: "missing target",
			mutate: func(payload map[string]interface{}) {
				delete(payload, "domain")
				delete(payload, "category")
				delete(payload, "difficulty")
			},
			message: "target scope is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{}
			for key, value := range validPayload {
				payload[key] = value
			}
			tc.mutate(payload)
			status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions", adminToken, payload)
			if status != http.StatusBadRequest || !strings.Contains(env.Message, tc.message) {
				t.Fatalf("expected %q bad request, got status=%d message=%s", tc.message, status, env.Message)
			}
		})
	}
}

func TestAdminInterviewBankPublishVersionsAndFailedVectorFilter(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	payload := validInterviewBankImportPayload("batch-publish", "atom-publish-failed")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/import/publish", adminToken, payload)
	if status != http.StatusOK {
		t.Fatalf("publish status=%d message=%s", status, env.Message)
	}
	var published struct {
		Report struct {
			Summary map[string]int `json:"summary"`
			Results []struct {
				ID     string `json:"id"`
				Action string `json:"action"`
			} `json:"results"`
		} `json:"report"`
		Batch domain.InterviewKnowledgeBatch `json:"batch"`
	}
	mustDecodeData(t, env, &published)
	if published.Report.Summary["published_count"] != 1 || published.Report.Results[0].Action != "create" {
		t.Fatalf("unexpected publish report: %+v", published.Report)
	}
	if published.Batch.Status != "published" || published.Batch.AtomCount != 1 {
		t.Fatalf("unexpected batch: %+v", published.Batch)
	}
	atom, ok := dataStore.GetInterviewKnowledgeAtom("atom-publish-failed")
	if !ok || atom.VectorStatus != "failed" {
		t.Fatalf("expected persisted failed-vector atom, got ok=%v atom=%+v", ok, atom)
	}
	if versions := dataStore.ListInterviewKnowledgeAtomVersions(atom.ID); len(versions) != 1 || versions[0].VersionType != domain.InterviewKnowledgeVersionContentUpdate {
		t.Fatalf("expected first content version, got %+v", versions)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/import/publish", adminToken, payload)
	if status != http.StatusOK {
		t.Fatalf("duplicate publish status=%d message=%s", status, env.Message)
	}
	mustDecodeData(t, env, &published)
	if published.Report.Summary["duplicate_count"] != 1 || published.Report.Results[0].Action != "duplicate_import" {
		t.Fatalf("expected duplicate import report, got %+v", published.Report)
	}
	versions := dataStore.ListInterviewKnowledgeAtomVersions(atom.ID)
	if len(versions) != 2 || versions[0].VersionType != domain.InterviewKnowledgeVersionDuplicateImport || !versions[0].NoContentChange {
		t.Fatalf("expected duplicate import version, got %+v", versions)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/atoms?vector_status=failed", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("failed atom list status=%d message=%s", status, env.Message)
	}
	var list struct {
		List  []domain.InterviewKnowledgeAtom `json:"list"`
		Total int                             `json:"total"`
	}
	mustDecodeData(t, env, &list)
	if list.Total != 1 || len(list.List) != 1 || list.List[0].ID != atom.ID {
		t.Fatalf("expected failed vector filter to return published atom, got %+v", list)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("system status=%d message=%s", status, env.Message)
	}
	var system struct {
		InterviewBank domain.InterviewKnowledgeSummary `json:"interview_bank"`
	}
	mustDecodeData(t, env, &system)
	if system.InterviewBank.TotalAtoms != 1 || system.InterviewBank.VectorFailedAtoms != 1 {
		t.Fatalf("expected system interview bank summary, got %+v", system.InterviewBank)
	}
}

func TestAdminInterviewBankHealthDiagnosesCombinations(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	opening := validInterviewBankAtomForRebuild("atom-health-opening", "published", "pending")
	opening.QuestionRole = "opening"
	indexedFollowup := validInterviewBankAtomForRebuild("atom-health-indexed-followup", "published", "indexed")
	indexedFollowup.QuestionRole = "followup"
	failedFollowup := validInterviewBankAtomForRebuild("atom-health-failed-followup", "published", "failed")
	failedFollowup.QuestionRole = "followup"
	blocked := validInterviewBankAtomForRebuild("atom-health-blocked-opening", "published", "indexed")
	blocked.Category = "database"
	blocked.Difficulty = "L2"
	blocked.QuestionRole = "opening"
	archived := validInterviewBankAtomForRebuild("atom-health-archived", "archived", "pending")
	for _, atom := range []domain.InterviewKnowledgeAtom{opening, indexedFollowup, failedFollowup, blocked, archived} {
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "健康诊断样例"); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/health", demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student health code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/health", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("health status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeHealthResponse
	mustDecodeData(t, env, &response)
	if response.Summary.TotalAtoms != 5 || response.Summary.WarningCombinations != 1 || response.Summary.BlockedCombinations != 1 {
		t.Fatalf("unexpected health summary: %+v", response.Summary)
	}
	cacheCombo := findInterviewKnowledgeHealthCombination(response.Combinations, "backend", "cache", "L3")
	if cacheCombo == nil || cacheCombo.Status != "warning" || cacheCombo.OpeningCount != 1 || cacheCombo.IndexedFollowupCount != 1 || cacheCombo.FailedCount != 1 {
		t.Fatalf("unexpected cache health combination: %+v", cacheCombo)
	}
	if !strings.Contains(strings.Join(cacheCombo.Reasons, ";"), "索引失败") {
		t.Fatalf("expected failed index reason, got %+v", cacheCombo.Reasons)
	}
	databaseCombo := findInterviewKnowledgeHealthCombination(response.Combinations, "backend", "database", "L2")
	if databaseCombo == nil || databaseCombo.Status != "blocked" || databaseCombo.FollowupCount != 0 {
		t.Fatalf("unexpected database health combination: %+v", databaseCombo)
	}
	if !strings.Contains(strings.Join(databaseCombo.Reasons, ";"), "追问题不足") {
		t.Fatalf("expected followup shortage reason, got %+v", databaseCombo.Reasons)
	}
}

func TestAdminInterviewBankRetrievalPreviewRequiresAdminAndReturnsMatches(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-preview-hit", "published", "pending")
	atom.QuestionRole = "followup"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "检索预览样例"); err != nil {
		t.Fatal(err)
	}
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	embedding := &mockInterviewBankEmbeddingClient{model: "test-embedding", dim: 1536}
	server.embedding = embedding
	handler := server.Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/retrieval-preview", demoToken, map[string]interface{}{
		"domain":     "backend",
		"category":   "cache",
		"difficulty": "L3",
		"query":      "热点 key 互斥锁",
	})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student retrieval preview code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/index/rebuild", adminToken, map[string]interface{}{
		"atom_ids": []string{atom.ID},
	})
	if status != http.StatusOK {
		t.Fatalf("rebuild status=%d message=%s", status, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/retrieval-preview", adminToken, map[string]interface{}{
		"domain":     "backend",
		"category":   "cache",
		"difficulty": "L3",
		"query":      "热点 key 互斥锁",
		"limit":      3,
	})
	if status != http.StatusOK {
		t.Fatalf("preview status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeRetrievalPreviewResponse
	mustDecodeData(t, env, &response)
	if response.FallbackUsed || response.MatchedCount != 1 || len(response.Results) != 1 {
		t.Fatalf("expected one preview hit without fallback, got %+v", response)
	}
	if response.Results[0].AtomID != atom.ID || response.Diagnostics.IndexedCandidates != 1 {
		t.Fatalf("unexpected preview response: %+v", response)
	}
	if embedding.calls != 2 {
		t.Fatalf("expected embedding for rebuild and preview, got %d", embedding.calls)
	}
	if versions := dataStore.ListInterviewKnowledgeAtomVersions(atom.ID); len(versions) != 1 {
		t.Fatalf("retrieval preview must not create versions, got %+v", versions)
	}
}

func TestAdminInterviewBankRetrievalPreviewMissingEmbeddingFallsBack(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-preview-no-embedding", "published", "indexed")
	atom.QuestionRole = "mixed"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "检索预览 fallback 样例"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/retrieval-preview", adminToken, map[string]interface{}{
		"domain":     "backend",
		"category":   "cache",
		"difficulty": "L3",
		"query":      "热点 key",
	})
	if status != http.StatusOK {
		t.Fatalf("preview status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeRetrievalPreviewResponse
	mustDecodeData(t, env, &response)
	if !response.FallbackUsed || !strings.Contains(response.FallbackReason, "embedding") {
		t.Fatalf("expected embedding fallback, got %+v", response)
	}
	if response.Diagnostics.IndexedCandidates != 1 || response.Diagnostics.EmbeddingAvailable {
		t.Fatalf("unexpected diagnostics: %+v", response.Diagnostics)
	}
	if versions := dataStore.ListInterviewKnowledgeAtomVersions(atom.ID); len(versions) != 1 {
		t.Fatalf("retrieval preview must not create versions, got %+v", versions)
	}
}

func TestInterviewRuntimeRetrievalWritesSanitizedHitAndFallbackLogs(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-runtime-retrieval-hit", "published", "indexed")
	atom.QuestionRole = "followup"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "运行时检索样例"); err != nil {
		t.Fatal(err)
	}
	docs := ai.BuildInterviewKnowledgeVectorDocuments(atom)
	if err := dataStore.VectorStore().RebuildInterviewKnowledgeIndex(context.Background(), docs); err != nil {
		t.Fatalf("seed interview knowledge vectors: %v", err)
	}
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))

	session := &domain.InterviewSession{
		ID:           "session-runtime-retrieval",
		CurrentRound: 2,
		SetupNotes:   "候选人提到了 password=secret 和 10.1.2.3",
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
			Subject:    "缓存击穿治理",
			Title:      "缓存击穿治理",
		},
	}
	question := &domain.InterviewQuestion{Title: "缓存击穿治理", Description: "singleflight", Domain: "backend", Difficulty: "L3"}
	result, err := server.retrieveInterviewFollowUpContext(context.Background(), agentruntime.InterviewRetrievalRequest{
		Session:  session,
		Question: question,
		Answer:   strings.Repeat("singleflight ", 80) + "api_key=abc 10.1.2.3",
		Evaluation: domain.InterviewEvaluation{
			Round:             2,
			FollowUpTriggered: true,
			FollowUpQuestion:  "请继续说明 singleflight。",
			FollowUpType:      "deepen",
			DimensionScores:   map[string]int{"technical_accuracy": 55},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FallbackUsed || len(result.MatchedAtoms) == 0 {
		t.Fatalf("expected runtime retrieval hit, got %+v", result)
	}

	fallbackSession := &domain.InterviewSession{
		ID:           "session-runtime-fallback",
		CurrentRound: 3,
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "database",
			Difficulty: "L3",
			Subject:    "慢查询定位",
			Title:      "慢查询定位",
		},
	}
	fallback, err := server.retrieveInterviewFollowUpContext(context.Background(), agentruntime.InterviewRetrievalRequest{
		Session:  fallbackSession,
		Question: &domain.InterviewQuestion{Title: "慢查询定位", Description: "EXPLAIN", Domain: "backend", Difficulty: "L3"},
		Answer:   "没有覆盖缓存内容 token=abcdef",
		Evaluation: domain.InterviewEvaluation{
			Round:             3,
			FollowUpTriggered: true,
			FollowUpQuestion:  "请补充排查路径。",
			FollowUpType:      "supplement",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fallback.FallbackUsed {
		t.Fatalf("expected runtime retrieval fallback, got %+v", fallback)
	}

	logs := dataStore.ListInterviewRetrievalLogs(domain.InterviewRetrievalLogFilter{Limit: 10})
	if len(logs) != 2 {
		t.Fatalf("expected 2 retrieval logs, got %+v", logs)
	}
	var hitLog, fallbackLog *domain.InterviewRetrievalLog
	for i := range logs {
		if logs[i].SessionID == session.ID {
			hitLog = &logs[i]
		}
		if logs[i].SessionID == fallbackSession.ID {
			fallbackLog = &logs[i]
		}
	}
	if hitLog == nil || fallbackLog == nil {
		t.Fatalf("expected hit and fallback logs, got %+v", logs)
	}
	if hitLog.SessionID != session.ID || hitLog.Round != 2 || hitLog.FallbackUsed || len(hitLog.MatchedAtoms) == 0 {
		t.Fatalf("unexpected hit log: %+v", hitLog)
	}
	if len([]rune(hitLog.QueryText)) > 500 || strings.Contains(hitLog.QueryText, "10.1.2.3") || strings.Contains(hitLog.QueryText, "api_key=abc") || strings.Contains(hitLog.QueryText, "password=secret") {
		t.Fatalf("query text must be sanitized and truncated, got %q", hitLog.QueryText)
	}
	if fallbackLog.SessionID != fallbackSession.ID || !fallbackLog.FallbackUsed || fallbackLog.ErrorMessage == "" {
		t.Fatalf("unexpected fallback log: %+v", fallbackLog)
	}
}

func TestAdminInterviewBankRetrievalLogsAndAnalytics(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	hitAtom := validInterviewBankAtomForRebuild("atom-analytics-hit", "published", "indexed")
	hitAtom.QuestionRole = "followup"
	lowAtom := validInterviewBankAtomForRebuild("atom-analytics-low", "published", "indexed")
	lowAtom.QuestionRole = "followup"
	for _, atom := range []domain.InterviewKnowledgeAtom{hitAtom, lowAtom} {
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "运营看板样例"); err != nil {
			t.Fatal(err)
		}
	}
	dataStore.InterviewSessions["session-analytics"] = &domain.InterviewSession{
		ID: "session-analytics",
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
		},
	}
	dataStore.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID: "session-analytics",
		Round:     1,
		QueryText: "singleflight",
		MatchedAtoms: []domain.InterviewKnowledgeAtomLightSnapshot{{
			AtomID:   hitAtom.ID,
			Version:  1,
			Title:    hitAtom.Title,
			Subject:  hitAtom.Subject,
			Domain:   hitAtom.Domain,
			Category: hitAtom.Category,
		}},
		CreatedAt: time.Now().Add(-time.Minute),
	})
	dataStore.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID:    "session-analytics",
		Round:        2,
		QueryText:    "未命中",
		FallbackUsed: true,
		ErrorMessage: "未命中可用题库追问原子，继续使用规则追问。",
		CreatedAt:    time.Now(),
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/retrieval-logs", demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student retrieval logs code=%d", env.Code)
	}
	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/retrieval-logs?fallback_used=bad", adminToken, nil)
	if status != http.StatusBadRequest || env.Message != "fallback_used must be true or false" {
		t.Fatalf("expected fallback_used validation, status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/retrieval-logs?fallback_used=true&domain=backend&category=cache&difficulty=L3&limit=5", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("retrieval logs status=%d message=%s", status, env.Message)
	}
	var logResponse struct {
		List  []domain.InterviewRetrievalLog `json:"list"`
		Total int                            `json:"total"`
	}
	mustDecodeData(t, env, &logResponse)
	if logResponse.Total != 1 || len(logResponse.List) != 1 || !logResponse.List[0].FallbackUsed {
		t.Fatalf("expected one fallback retrieval log, got %+v", logResponse)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/retrieval-analytics?domain=backend&category=cache&difficulty=L3&limit=20", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("analytics status=%d message=%s", status, env.Message)
	}
	var analytics domain.InterviewRetrievalAnalytics
	mustDecodeData(t, env, &analytics)
	if analytics.TotalLogs != 2 || analytics.HitLogs != 1 || analytics.FallbackLogs != 1 {
		t.Fatalf("unexpected analytics counters: %+v", analytics)
	}
	if len(analytics.TopHitAtoms) != 1 || analytics.TopHitAtoms[0].AtomID != hitAtom.ID {
		t.Fatalf("unexpected top hits: %+v", analytics.TopHitAtoms)
	}
	if len(analytics.LowHitAtoms) == 0 || analytics.LowHitAtoms[0].AtomID != lowAtom.ID {
		t.Fatalf("unexpected low hits: %+v", analytics.LowHitAtoms)
	}
	if len(analytics.FallbackCombinations) != 1 || analytics.FallbackCombinations[0].Category != "cache" {
		t.Fatalf("unexpected fallback combinations: %+v", analytics.FallbackCombinations)
	}
	if len(analytics.RecentFallbacks) != 1 || analytics.RecentFallbacks[0].ErrorMessage == "" {
		t.Fatalf("expected recent fallback details, got %+v", analytics.RecentFallbacks)
	}
}

func TestAdminInterviewBankAtomDetailVersionsAndManualEdit(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-edit-manual", "published", "indexed")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "初始导入"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/atoms/"+atom.ID, demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student atom detail code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/atoms/"+atom.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("atom detail status=%d message=%s", status, env.Message)
	}
	var detail struct {
		Atom domain.InterviewKnowledgeAtom `json:"atom"`
	}
	mustDecodeData(t, env, &detail)
	if detail.Atom.ID != atom.ID || detail.Atom.CurrentVersion != 1 {
		t.Fatalf("unexpected atom detail: %+v", detail.Atom)
	}

	update := validInterviewBankUpdatePayload(detail.Atom)
	update["title"] = "缓存击穿治理修订"
	update["change_note"] = "补充热点 key 处理"
	status, env = requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/atoms/"+atom.ID, adminToken, update)
	if status != http.StatusOK {
		t.Fatalf("manual edit status=%d message=%s", status, env.Message)
	}
	var edited struct {
		Atom    domain.InterviewKnowledgeAtom        `json:"atom"`
		Version domain.InterviewKnowledgeAtomVersion `json:"version"`
	}
	mustDecodeData(t, env, &edited)
	if edited.Atom.CurrentVersion != 2 || edited.Atom.Title != "缓存击穿治理修订" {
		t.Fatalf("expected updated atom version, got %+v", edited.Atom)
	}
	if edited.Atom.VectorStatus != "pending" {
		t.Fatalf("manual edit must reset vector status to pending, got %+v", edited.Atom)
	}
	if edited.Version.VersionType != domain.InterviewKnowledgeVersionManualEdit || edited.Version.ChangeNote != "补充热点 key 处理" {
		t.Fatalf("expected manual edit version, got %+v", edited.Version)
	}
	if edited.Version.NoContentChange {
		t.Fatalf("changed title must not be marked no_content_change: %+v", edited.Version)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/atoms/"+atom.ID+"/versions", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("version history status=%d message=%s", status, env.Message)
	}
	var history struct {
		List []domain.InterviewKnowledgeAtomVersion `json:"list"`
	}
	mustDecodeData(t, env, &history)
	if len(history.List) != 2 || history.List[0].Version != 2 || history.List[0].VersionType != domain.InterviewKnowledgeVersionManualEdit {
		t.Fatalf("expected newest manual edit version first, got %+v", history.List)
	}
}

func TestAdminInterviewBankManualEditValidationAndNoContentChange(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-edit-validation", "published", "failed")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "初始导入"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	current, ok := dataStore.GetInterviewKnowledgeAtom(atom.ID)
	if !ok {
		t.Fatal("expected atom")
	}
	base := validInterviewBankUpdatePayload(*current)

	missingNote := cloneStringMap(base)
	missingNote["change_note"] = " "
	status, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/atoms/"+atom.ID, adminToken, missingNote)
	if status != http.StatusBadRequest || env.Message != "change_note is required" {
		t.Fatalf("expected change note rejection, status=%d env=%+v", status, env)
	}

	conflict := cloneStringMap(base)
	conflict["base_version"] = 99
	status, env = requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/atoms/"+atom.ID, adminToken, conflict)
	if status != http.StatusBadRequest || env.Message != "版本已更新，请刷新后重试" {
		t.Fatalf("expected version conflict, status=%d env=%+v", status, env)
	}

	invalid := cloneStringMap(base)
	invalid["category"] = "unknown"
	invalid["principles"] = []string{"只有一条"}
	status, env = requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/atoms/"+atom.ID, adminToken, invalid)
	if status != http.StatusBadRequest || !strings.Contains(env.Message, "category is invalid") || !strings.Contains(env.Message, "principles must include at least 2 items") {
		t.Fatalf("expected field validation errors, status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/atoms/"+atom.ID, adminToken, base)
	if status != http.StatusOK {
		t.Fatalf("no-change edit status=%d message=%s", status, env.Message)
	}
	var edited struct {
		Atom    domain.InterviewKnowledgeAtom        `json:"atom"`
		Version domain.InterviewKnowledgeAtomVersion `json:"version"`
	}
	mustDecodeData(t, env, &edited)
	if edited.Atom.CurrentVersion != 2 || edited.Atom.VectorStatus != "pending" {
		t.Fatalf("expected version advance and pending vector status, got %+v", edited.Atom)
	}
	if !edited.Version.NoContentChange || edited.Version.VersionType != domain.InterviewKnowledgeVersionManualEdit {
		t.Fatalf("expected no-content-change manual edit version, got %+v", edited.Version)
	}
}

func TestAdminInterviewBankArchiveAndRestore(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-archive-restore", "published", "indexed")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "初始导入"); err != nil {
		t.Fatal(err)
	}
	vectorStore := dataStore.VectorStore()
	if err := vectorStore.RebuildInterviewKnowledgeIndex(context.Background(), ai.BuildInterviewKnowledgeVectorDocuments(atom)); err != nil {
		t.Fatalf("seed vector docs: %v", err)
	}
	beforeArchive, err := vectorStore.SearchInterviewKnowledge(context.Background(), store.InterviewKnowledgeVectorSearchQuery{
		Category:      atom.Category,
		Difficulty:    atom.Difficulty,
		QuestionRoles: []string{atom.QuestionRole},
		Text:          "singleflight",
		Limit:         5,
	})
	if err != nil || len(beforeArchive) == 0 {
		t.Fatalf("expected searchable vector docs before archive, results=%+v err=%v", beforeArchive, err)
	}

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+atom.ID+"/archive", demoToken, map[string]string{"reason": "过期题目"})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student archive code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+atom.ID+"/archive", adminToken, map[string]string{"reason": " "})
	if status != http.StatusBadRequest || env.Message != "reason is required" {
		t.Fatalf("expected reason rejection, status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+atom.ID+"/archive", adminToken, map[string]string{"reason": "过期题目"})
	if status != http.StatusOK {
		t.Fatalf("archive status=%d message=%s", status, env.Message)
	}
	var archived struct {
		Atom    domain.InterviewKnowledgeAtom        `json:"atom"`
		Version domain.InterviewKnowledgeAtomVersion `json:"version"`
	}
	mustDecodeData(t, env, &archived)
	if archived.Atom.Status != "archived" || archived.Atom.VectorStatus != "pending" || archived.Atom.CurrentVersion != 2 {
		t.Fatalf("expected archived pending atom v2, got %+v", archived.Atom)
	}
	if archived.Version.VersionType != domain.InterviewKnowledgeVersionArchive || archived.Version.ChangeNote != "过期题目" {
		t.Fatalf("expected archive version, got %+v", archived.Version)
	}
	afterArchive, err := vectorStore.SearchInterviewKnowledge(context.Background(), store.InterviewKnowledgeVectorSearchQuery{
		Category:      atom.Category,
		Difficulty:    atom.Difficulty,
		QuestionRoles: []string{atom.QuestionRole},
		Text:          "singleflight",
		Limit:         5,
	})
	if err != nil || len(afterArchive) != 0 {
		t.Fatalf("expected archived atom vector docs deleted, results=%+v err=%v", afterArchive, err)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+atom.ID+"/archive", adminToken, map[string]string{"reason": "重复归档"})
	if status != http.StatusBadRequest || env.Message != "interview knowledge atom is already archived" {
		t.Fatalf("expected duplicate archive rejection, status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+atom.ID+"/restore", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("restore status=%d message=%s", status, env.Message)
	}
	var restored struct {
		Atom    domain.InterviewKnowledgeAtom        `json:"atom"`
		Version domain.InterviewKnowledgeAtomVersion `json:"version"`
	}
	mustDecodeData(t, env, &restored)
	if restored.Atom.Status != "published" || restored.Atom.VectorStatus != "pending" || restored.Atom.CurrentVersion != 3 {
		t.Fatalf("expected restored published pending atom v3, got %+v", restored.Atom)
	}
	if restored.Version.VersionType != domain.InterviewKnowledgeVersionRestoreArchived {
		t.Fatalf("expected restore version, got %+v", restored.Version)
	}
	versions := dataStore.ListInterviewKnowledgeAtomVersions(atom.ID)
	if len(versions) != 3 || versions[0].VersionType != domain.InterviewKnowledgeVersionRestoreArchived || versions[1].VersionType != domain.InterviewKnowledgeVersionArchive {
		t.Fatalf("expected restore then archive versions, got %+v", versions)
	}
}

func TestAdminInterviewBankRestoreValidation(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	published := validInterviewBankAtomForRebuild("atom-restore-published", "published", "pending")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(published, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "已发布样例"); err != nil {
		t.Fatal(err)
	}
	invalidArchived := validInterviewBankAtomForRebuild("atom-restore-invalid", "archived", "pending")
	invalidArchived.Principles = []string{"只有一条"}
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(invalidArchived, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "非法归档样例"); err != nil {
		t.Fatal(err)
	}

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+published.ID+"/restore", adminToken, nil)
	if status != http.StatusBadRequest || env.Message != "only archived interview knowledge atoms can be restored" {
		t.Fatalf("expected non-archived restore rejection, status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/atoms/"+invalidArchived.ID+"/restore", adminToken, nil)
	if status != http.StatusBadRequest || !strings.Contains(env.Message, "principles must include at least 2 items") {
		t.Fatalf("expected restore hard validation rejection, status=%d env=%+v", status, env)
	}
}

func TestAdminInterviewBankRebuildRequiresAdminAndIndexesPendingFailed(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	pending := validInterviewBankAtomForRebuild("atom-rebuild-pending", "published", "pending")
	failed := validInterviewBankAtomForRebuild("atom-rebuild-failed", "published", "failed")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(pending, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "待索引样例"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(failed, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "失败索引样例"); err != nil {
		t.Fatal(err)
	}

	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	embedding := &mockInterviewBankEmbeddingClient{model: "test-embedding", dim: 1536}
	server.embedding = embedding
	handler := server.Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/index/rebuild", demoToken, map[string]interface{}{
		"vector_status": "pending_failed",
	})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student rebuild code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/index/rebuild", adminToken, map[string]interface{}{
		"vector_status": "pending_failed",
		"limit":         50,
	})
	if status != http.StatusOK {
		t.Fatalf("rebuild status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeIndexRebuildResponse
	mustDecodeData(t, env, &response)
	if response.Total != 2 || response.Indexed != 2 || response.Failed != 0 || response.Skipped != 0 {
		t.Fatalf("unexpected rebuild response: %+v", response)
	}
	if embedding.calls != 2 {
		t.Fatalf("expected embedding called once per atom, got %d", embedding.calls)
	}
	for _, result := range response.Results {
		if result.DocCount != 7 || result.EmbeddingModel != "test-embedding" {
			t.Fatalf("unexpected rebuild result: %+v", result)
		}
		atom, ok := dataStore.GetInterviewKnowledgeAtom(result.AtomID)
		if !ok || atom.VectorStatus != "indexed" || atom.LastIndexedAt == nil {
			t.Fatalf("expected indexed atom with timestamp, ok=%v atom=%+v", ok, atom)
		}
	}
}

func TestAdminInterviewBankRebuildFailureKeepsLastIndexedAt(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	lastIndexedAt := time.Now().Add(-2 * time.Hour).Truncate(time.Microsecond)
	atom := validInterviewBankAtomForRebuild("atom-rebuild-provider-failure", "published", "indexed")
	atom.LastIndexedAt = &lastIndexedAt
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "已索引样例"); err != nil {
		t.Fatal(err)
	}

	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	server.embedding = &mockInterviewBankEmbeddingClient{err: errors.New("provider unavailable")}
	handler := server.Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/index/rebuild", adminToken, map[string]interface{}{
		"atom_ids": []string{atom.ID},
		"limit":    50,
	})
	if status != http.StatusOK {
		t.Fatalf("rebuild status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeIndexRebuildResponse
	mustDecodeData(t, env, &response)
	if response.Total != 1 || response.Failed != 1 || response.Results[0].Status != "failed" {
		t.Fatalf("expected failed rebuild response: %+v", response)
	}
	updated, ok := dataStore.GetInterviewKnowledgeAtom(atom.ID)
	if !ok {
		t.Fatal("expected atom after failed rebuild")
	}
	if updated.VectorStatus != "failed" {
		t.Fatalf("expected failed vector status, got %+v", updated)
	}
	if updated.LastIndexedAt == nil || !updated.LastIndexedAt.Equal(lastIndexedAt) {
		t.Fatalf("failed rebuild must preserve last_indexed_at, got %+v want %v", updated.LastIndexedAt, lastIndexedAt)
	}
	if versions := dataStore.ListInterviewKnowledgeAtomVersions(atom.ID); len(versions) != 1 {
		t.Fatalf("index status update must not create atom version, got %+v", versions)
	}
}

func TestAdminInterviewBankRebuildSkipsDraftAndArchivedWithoutEmbedding(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	draft := validInterviewBankAtomForRebuild("atom-rebuild-draft", "draft", "pending")
	archived := validInterviewBankAtomForRebuild("atom-rebuild-archived", "archived", "failed")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(draft, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "草稿样例"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(archived, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "归档样例"); err != nil {
		t.Fatal(err)
	}

	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	embedding := &mockInterviewBankEmbeddingClient{dim: 1536}
	server.embedding = embedding
	handler := server.Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/index/rebuild", adminToken, map[string]interface{}{
		"atom_ids": []string{draft.ID, archived.ID},
		"limit":    50,
	})
	if status != http.StatusOK {
		t.Fatalf("rebuild status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeIndexRebuildResponse
	mustDecodeData(t, env, &response)
	if response.Total != 2 || response.Skipped != 2 || response.Indexed != 0 || response.Failed != 0 {
		t.Fatalf("expected skipped non-published atoms, got %+v", response)
	}
	if embedding.calls != 0 {
		t.Fatalf("draft/archived atoms must not call embedding, got %d calls", embedding.calls)
	}
}

func TestAdminInterviewBankRebuildMissingEmbeddingMarksFailed(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-rebuild-no-embedding", "published", "pending")
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "缺少 embedding 样例"); err != nil {
		t.Fatal(err)
	}

	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/index/rebuild", adminToken, map[string]interface{}{
		"atom_ids": []string{atom.ID},
	})
	if status != http.StatusOK {
		t.Fatalf("rebuild status=%d message=%s", status, env.Message)
	}
	var response interviewKnowledgeIndexRebuildResponse
	mustDecodeData(t, env, &response)
	if response.Total != 1 || response.Failed != 1 || response.Results[0].Status != "failed" {
		t.Fatalf("expected missing embedding failure, got %+v", response)
	}
	updated, ok := dataStore.GetInterviewKnowledgeAtom(atom.ID)
	if !ok || updated.VectorStatus != "failed" || updated.LastIndexedAt != nil {
		t.Fatalf("expected failed atom without last_indexed_at, ok=%v atom=%+v", ok, updated)
	}
}

func findInterviewKnowledgeHealthCombination(items []interviewKnowledgeHealthCombination, domainName, category, difficulty string) *interviewKnowledgeHealthCombination {
	for i := range items {
		if items[i].Domain == domainName && items[i].Category == category && items[i].Difficulty == difficulty {
			return &items[i]
		}
	}
	return nil
}

func validInterviewBankImportPayload(batchID, atomID string) map[string]interface{} {
	return map[string]interface{}{
		"batch_id":   batchID,
		"source_ref": "fixture/interview-bank.json",
		"items": []map[string]interface{}{
			{
				"id":              atomID,
				"title":           "缓存击穿治理",
				"subject":         "缓存击穿治理",
				"domain":          "backend",
				"difficulty":      "L3",
				"category":        "cache",
				"question_role":   "mixed",
				"source_ref":      "fixture/cache-breakdown",
				"tags":            []string{"cache", "hot-key", "cache"},
				"principles":      []string{"说明互斥锁或 singleflight 控制并发回源", "说明热点 key 预热和过期时间抖动"},
				"pitfalls":        []string{"只说加缓存但不处理失效瞬间并发", "忽略数据库被瞬时流量打满的风险"},
				"follow_up_paths": []string{"追问缓存雪崩和缓存穿透的差异", "追问本地缓存与分布式缓存的一致性取舍"},
				"vector_status":   "failed",
			},
		},
	}
}

func validInterviewBankAtomForRebuild(id, status, vectorStatus string) domain.InterviewKnowledgeAtom {
	return domain.InterviewKnowledgeAtom{
		ID:            id,
		Title:         "缓存击穿治理",
		Subject:       "缓存击穿治理",
		Domain:        "backend",
		Difficulty:    "L3",
		Category:      "cache",
		QuestionRole:  "mixed",
		SourceRef:     "fixture/cache-breakdown",
		Tags:          []string{"cache", "hot-key"},
		Principles:    []string{"说明互斥锁或 singleflight 控制并发回源", "说明热点 key 预热和过期时间抖动"},
		Pitfalls:      []string{"只说加缓存但不处理失效瞬间并发", "忽略数据库被瞬时流量打满的风险"},
		FollowUpPaths: []string{"追问缓存雪崩和缓存穿透的差异", "追问本地缓存与分布式缓存的一致性取舍"},
		Status:        status,
		VectorStatus:  vectorStatus,
	}
}

func validInterviewBankUpdatePayload(atom domain.InterviewKnowledgeAtom) map[string]interface{} {
	return map[string]interface{}{
		"base_version":    atom.CurrentVersion,
		"change_note":     "管理员在线编辑",
		"title":           atom.Title,
		"subject":         atom.Subject,
		"domain":          atom.Domain,
		"difficulty":      atom.Difficulty,
		"category":        atom.Category,
		"question_role":   atom.QuestionRole,
		"source_ref":      atom.SourceRef,
		"tags":            append([]string{}, atom.Tags...),
		"principles":      append([]string{}, atom.Principles...),
		"pitfalls":        append([]string{}, atom.Pitfalls...),
		"follow_up_paths": append([]string{}, atom.FollowUpPaths...),
	}
}

func cloneStringMap(input map[string]interface{}) map[string]interface{} {
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

type mockInterviewBankEmbeddingClient struct {
	model  string
	dim    int
	err    error
	calls  int
	inputs [][]string
}

func (m *mockInterviewBankEmbeddingClient) Embed(_ context.Context, input []string) (ai.EmbeddingResult, error) {
	m.calls++
	m.inputs = append(m.inputs, append([]string{}, input...))
	if m.err != nil {
		return ai.EmbeddingResult{}, m.err
	}
	dim := m.dim
	if dim <= 0 {
		dim = 1536
	}
	model := m.model
	if model == "" {
		model = "test-embedding"
	}
	vectors := make([][]float64, len(input))
	for i := range input {
		vector := make([]float64, dim)
		vector[0] = 1
		if dim > 1 {
			vector[1] = float64(i+1) / 100
		}
		vectors[i] = vector
	}
	return ai.EmbeddingResult{Model: model, Vectors: vectors}, nil
}
