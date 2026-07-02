package httpapi

import (
	"net/http"
	"testing"
	"time"

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
