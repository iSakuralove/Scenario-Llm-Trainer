package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
