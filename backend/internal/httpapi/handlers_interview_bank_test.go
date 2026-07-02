package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

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
