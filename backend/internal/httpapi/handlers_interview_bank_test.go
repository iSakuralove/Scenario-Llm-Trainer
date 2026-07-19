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

func TestAdminInterviewBankOpsActionDetailIncludesCurrentAtomContext(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	atom := validInterviewBankAtomForRebuild("atom-ops-detail", "published", "failed")
	savedAtom, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "详情样例")
	if err != nil {
		t.Fatal(err)
	}
	action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
		ActionType: domain.InterviewBankOpsActionTypeRebuildIndex,
		Status:     domain.InterviewBankOpsActionStatusOpen,
		Priority:   "P1",
		Source:     domain.InterviewBankOpsActionSourceIndexStatus,
		Title:      "重建题库索引：" + savedAtom.Title,
		Reason:     "已发布题目索引状态为 failed，可能影响后续追问检索。",
		Domain:     savedAtom.Domain,
		Category:   savedAtom.Category,
		Difficulty: savedAtom.Difficulty,
		AtomID:     savedAtom.ID,
		Evidence:   map[string]interface{}{"vector_status": "failed"},
		CreatedBy:  "admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("ops action detail status=%d message=%s", status, env.Message)
	}
	var detail struct {
		Action      domain.InterviewBankOpsAction `json:"action"`
		AtomContext struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			Status         string `json:"status"`
			VectorStatus   string `json:"vector_status"`
			CurrentVersion int    `json:"current_version"`
		} `json:"atom_context"`
		Stale       bool   `json:"stale"`
		StaleReason string `json:"stale_reason"`
	}
	mustDecodeData(t, env, &detail)
	if detail.Action.ID != action.ID || detail.Action.AtomID != savedAtom.ID {
		t.Fatalf("unexpected action detail: %+v", detail.Action)
	}
	if detail.Stale {
		t.Fatalf("expected non-stale action detail, got stale=%v reason=%q", detail.Stale, detail.StaleReason)
	}
	if detail.AtomContext.ID != savedAtom.ID || detail.AtomContext.Status != "published" || detail.AtomContext.VectorStatus != "failed" {
		t.Fatalf("unexpected atom context: %+v", detail.AtomContext)
	}
	if detail.AtomContext.CurrentVersion != savedAtom.CurrentVersion || detail.AtomContext.Title != savedAtom.Title {
		t.Fatalf("unexpected atom version/title context: %+v saved=%+v", detail.AtomContext, savedAtom)
	}
}

func TestAdminInterviewBankOpsActionDetailMarksArchivedOrMissingAtomStale(t *testing.T) {
	t.Run("archived atom", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		atom := validInterviewBankAtomForRebuild("atom-ops-archived", "archived", "pending")
		savedAtom, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "详情 stale 样例")
		if err != nil {
			t.Fatal(err)
		}
		action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeObserve,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P3",
			Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
			Title:      "观察已归档题目",
			Reason:     "原动作关联资源已被下架。",
			AtomID:     savedAtom.ID,
			Evidence:   map[string]interface{}{"hit_count": float64(0)},
			CreatedBy:  "admin-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("archived detail status=%d message=%s", status, env.Message)
		}
		var detail struct {
			Stale       bool   `json:"stale"`
			StaleReason string `json:"stale_reason"`
			AtomContext struct {
				Status string `json:"status"`
			} `json:"atom_context"`
		}
		mustDecodeData(t, env, &detail)
		if !detail.Stale || detail.StaleReason != "关联 atom 已归档" || detail.AtomContext.Status != "archived" {
			t.Fatalf("expected archived atom stale detail, got %+v", detail)
		}
	})

	t.Run("missing atom", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFixAtom,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P2",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "检查缺失 atom",
			Reason:     "原始动作关联原子已不存在。",
			AtomID:     "atom-missing",
			Evidence:   map[string]interface{}{"source": "manual"},
			CreatedBy:  "admin-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("missing atom detail status=%d message=%s", status, env.Message)
		}
		var detail struct {
			Stale       bool        `json:"stale"`
			StaleReason string      `json:"stale_reason"`
			AtomContext interface{} `json:"atom_context"`
		}
		mustDecodeData(t, env, &detail)
		if !detail.Stale || detail.StaleReason != "关联 atom 不存在" || detail.AtomContext != nil {
			t.Fatalf("expected missing atom stale detail, got %+v", detail)
		}
	})
}

func TestAdminInterviewBankOpsActionDetailRequiresAdmin(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
		ActionType: domain.InterviewBankOpsActionTypeFillGap,
		Status:     domain.InterviewBankOpsActionStatusOpen,
		Priority:   "P1",
		Source:     domain.InterviewBankOpsActionSourceManual,
		Title:      "管理员详情权限",
		Reason:     "验证详情接口只允许管理员访问。",
		Domain:     "backend",
		Category:   "cache",
		Difficulty: "L3",
		Evidence:   map[string]interface{}{"source": "manual"},
		CreatedBy:  "admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student ops action detail code=%d", env.Code)
	}
}

func TestAdminInterviewBankOpsActionUpdateStatusAndHistory(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
		ActionType: domain.InterviewBankOpsActionTypeRebuildIndex,
		Status:     domain.InterviewBankOpsActionStatusOpen,
		Priority:   "P1",
		Source:     domain.InterviewBankOpsActionSourceIndexStatus,
		Title:      "重建索引动作闭环样例",
		Reason:     "索引失败，需要先处理后关闭。",
		Domain:     "backend",
		Category:   "cache",
		Difficulty: "L3",
		AtomID:     "atom-status-loop",
		Evidence:   map[string]interface{}{"vector_status": "failed"},
		CreatedBy:  "admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, map[string]interface{}{
		"status": "resolved",
		"note":   "已完成索引重建并复查通过",
	})
	if status != http.StatusOK {
		t.Fatalf("ops action patch status=%d message=%s", status, env.Message)
	}
	var updated struct {
		Action       domain.InterviewBankOpsAction `json:"action"`
		HistoryEntry struct {
			ActionID   string `json:"action_id"`
			FromStatus string `json:"from_status"`
			ToStatus   string `json:"to_status"`
			Note       string `json:"note"`
			CreatedBy  string `json:"created_by"`
		} `json:"history_entry"`
	}
	mustDecodeData(t, env, &updated)
	if updated.Action.Status != domain.InterviewBankOpsActionStatusResolved {
		t.Fatalf("expected resolved action, got %+v", updated.Action)
	}
	if updated.HistoryEntry.ActionID != action.ID || updated.HistoryEntry.FromStatus != "open" || updated.HistoryEntry.ToStatus != "resolved" {
		t.Fatalf("unexpected history entry statuses: %+v", updated.HistoryEntry)
	}
	if updated.HistoryEntry.Note != "已完成索引重建并复查通过" || updated.HistoryEntry.CreatedBy != "user-admin" {
		t.Fatalf("unexpected history entry note/admin: %+v", updated.HistoryEntry)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("ops action detail after patch status=%d message=%s", status, env.Message)
	}
	var detail struct {
		Action  domain.InterviewBankOpsAction `json:"action"`
		History []struct {
			FromStatus string `json:"from_status"`
			ToStatus   string `json:"to_status"`
			Note       string `json:"note"`
			CreatedBy  string `json:"created_by"`
		} `json:"history"`
	}
	mustDecodeData(t, env, &detail)
	if detail.Action.Status != domain.InterviewBankOpsActionStatusResolved {
		t.Fatalf("expected resolved action in detail, got %+v", detail.Action)
	}
	if len(detail.History) != 1 {
		t.Fatalf("expected one history row, got %+v", detail.History)
	}
	if detail.History[0].FromStatus != "open" || detail.History[0].ToStatus != "resolved" || detail.History[0].Note != "已完成索引重建并复查通过" {
		t.Fatalf("unexpected history detail row: %+v", detail.History[0])
	}
}

func TestAdminInterviewBankOpsActionUpdateStatusValidatesNoteAndReopenFlow(t *testing.T) {
	t.Run("resolved note required", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P1",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "备注校验",
			Reason:     "关闭必须留痕。",
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
			Evidence:   map[string]interface{}{"source": "manual"},
			CreatedBy:  "admin-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, map[string]interface{}{
			"status": "resolved",
			"note":   " ",
		})
		if status != http.StatusBadRequest || env.Message != "note is required for resolved or dismissed" {
			t.Fatalf("expected resolved note rejection, status=%d env=%+v", status, env)
		}
	})

	t.Run("reopened requires closed state", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeObserve,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P3",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "重开校验",
			Reason:     "未关闭前不能重开。",
			AtomID:     "atom-reopen-check",
			Evidence:   map[string]interface{}{"source": "manual"},
			CreatedBy:  "admin-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, map[string]interface{}{
			"status": "reopened",
		})
		if status != http.StatusBadRequest || env.Message != "reopened status requires resolved or dismissed action" {
			t.Fatalf("expected reopen rejection, status=%d env=%+v", status, env)
		}
	})
}

func TestAdminInterviewBankOpsActionUpdateStatusReopenedAndHistoryOrder(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	action, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
		ActionType: domain.InterviewBankOpsActionTypeObserve,
		Status:     domain.InterviewBankOpsActionStatusOpen,
		Priority:   "P3",
		Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
		Title:      "观察后重开",
		Reason:     "需要验证重开和历史顺序。",
		AtomID:     "atom-reopened",
		Evidence:   map[string]interface{}{"hit_count": float64(0)},
		CreatedBy:  "admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	demoToken := loginToken(t, handler, "demo", "demo123")

	_, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, demoToken, map[string]interface{}{
		"status": "watching",
	})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student ops action patch code=%d", env.Code)
	}

	for _, payload := range []map[string]interface{}{
		{"status": "watching", "note": "先观察一轮真实检索"},
		{"status": "resolved", "note": "确认暂不需要继续处理"},
		{"status": "reopened", "note": "新一轮真实检索再次触发"},
	} {
		status, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, payload)
		if status != http.StatusOK {
			t.Fatalf("status transition payload=%+v status=%d env=%+v", payload, status, env)
		}
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions/"+action.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("detail after reopen status=%d env=%+v", status, env)
	}
	var detail struct {
		Action  domain.InterviewBankOpsAction `json:"action"`
		History []struct {
			FromStatus string `json:"from_status"`
			ToStatus   string `json:"to_status"`
			Note       string `json:"note"`
		} `json:"history"`
	}
	mustDecodeData(t, env, &detail)
	if detail.Action.Status != domain.InterviewBankOpsActionStatusReopened {
		t.Fatalf("expected reopened action, got %+v", detail.Action)
	}
	if len(detail.History) != 3 {
		t.Fatalf("expected three history rows, got %+v", detail.History)
	}
	if detail.History[0].FromStatus != "resolved" || detail.History[0].ToStatus != "reopened" {
		t.Fatalf("expected latest history to be reopen, got %+v", detail.History[0])
	}
	if detail.History[1].FromStatus != "watching" || detail.History[1].ToStatus != "resolved" {
		t.Fatalf("expected middle history to be resolve, got %+v", detail.History[1])
	}
	if detail.History[2].FromStatus != "open" || detail.History[2].ToStatus != "watching" {
		t.Fatalf("expected oldest history to be watching, got %+v", detail.History[2])
	}
}

func TestAdminInterviewBankOpsActionReopenRejectsActiveDedupeConflict(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	closed, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
		ActionType: domain.InterviewBankOpsActionTypeFillGap,
		Status:     domain.InterviewBankOpsActionStatusResolved,
		Priority:   "P1",
		Source:     domain.InterviewBankOpsActionSourceHealthDiagnostic,
		DedupeKey:  "fill_gap|combo|backend|cache|L3",
		Title:      "已关闭旧动作",
		Reason:     "已处理过一次。",
		Domain:     "backend",
		Category:   "cache",
		Difficulty: "L3",
		Evidence:   map[string]interface{}{"status": "blocked"},
		CreatedBy:  "admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
		ActionType: domain.InterviewBankOpsActionTypeFillGap,
		Status:     domain.InterviewBankOpsActionStatusOpen,
		Priority:   "P0",
		Source:     domain.InterviewBankOpsActionSourceHealthDiagnostic,
		DedupeKey:  closed.DedupeKey,
		Title:      "新 open 动作",
		Reason:     "重新出现同类问题。",
		Domain:     "backend",
		Category:   "cache",
		Difficulty: "L3",
		Evidence:   map[string]interface{}{"status": "blocked"},
		CreatedBy:  "admin-1",
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPatch, "/api/v1/admin/interview-bank/ops-actions/"+closed.ID, adminToken, map[string]interface{}{
		"status": "reopened",
		"note":   "尝试重开旧动作",
	})
	if status != http.StatusBadRequest || env.Message != "another active action already uses this dedupe_key" {
		t.Fatalf("expected reopen dedupe rejection, status=%d env=%+v", status, env)
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

func TestAdminInterviewBankOpsActionCandidatesFromHealthDiagnostic(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	blocked := validInterviewBankAtomForRebuild("atom-candidate-blocked-opening", "published", "indexed")
	blocked.Category = "database"
	blocked.Difficulty = "L2"
	blocked.QuestionRole = "opening"
	if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(blocked, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "候选生成样例"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources":    []string{"health_diagnostic"},
		"domain":     "backend",
		"category":   "database",
		"difficulty": "L2",
	})
	if status != http.StatusOK {
		t.Fatalf("candidate generation status=%d message=%s", status, env.Message)
	}
	var response struct {
		List []struct {
			CandidateKey string                 `json:"candidate_key"`
			ActionType   string                 `json:"action_type"`
			Priority     string                 `json:"priority"`
			Source       string                 `json:"source"`
			DedupeKey    string                 `json:"dedupe_key"`
			Title        string                 `json:"title"`
			Reason       string                 `json:"reason"`
			Domain       string                 `json:"domain"`
			Category     string                 `json:"category"`
			Difficulty   string                 `json:"difficulty"`
			Evidence     map[string]interface{} `json:"evidence"`
		} `json:"list"`
		Total           int `json:"total"`
		SkippedExisting int `json:"skipped_existing"`
	}
	mustDecodeData(t, env, &response)
	if response.Total != 1 || len(response.List) != 1 {
		t.Fatalf("expected one candidate, got %+v", response)
	}
	candidate := response.List[0]
	if candidate.ActionType != domain.InterviewBankOpsActionTypeFillGap || candidate.Priority != "P0" || candidate.Source != domain.InterviewBankOpsActionSourceHealthDiagnostic {
		t.Fatalf("unexpected candidate classification: %+v", candidate)
	}
	if candidate.Domain != "backend" || candidate.Category != "database" || candidate.Difficulty != "L2" {
		t.Fatalf("unexpected candidate target: %+v", candidate)
	}
	if candidate.DedupeKey != "fill_gap|combo|backend|database|L2" || candidate.CandidateKey == "" {
		t.Fatalf("unexpected candidate keys: %+v", candidate)
	}
	if !strings.Contains(candidate.Reason, "追问题不足") || candidate.Title == "" {
		t.Fatalf("expected health reason/title, got %+v", candidate)
	}
	if candidate.Evidence["status"] != "blocked" {
		t.Fatalf("expected compact health evidence, got %+v", candidate.Evidence)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions?status=open", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list ops actions after candidates status=%d message=%s", status, env.Message)
	}
	var listed struct {
		Total int `json:"total"`
	}
	mustDecodeData(t, env, &listed)
	if listed.Total != 0 {
		t.Fatalf("candidate generation must not persist actions, got %+v", listed)
	}
}

func TestAdminInterviewBankOpsActionCandidatesFromHealthWarning(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	opening := validInterviewBankAtomForRebuild("atom-candidate-warning-opening", "published", "indexed")
	opening.QuestionRole = "opening"
	indexedFollowup := validInterviewBankAtomForRebuild("atom-candidate-warning-indexed", "published", "indexed")
	indexedFollowup.QuestionRole = "followup"
	failedFollowup := validInterviewBankAtomForRebuild("atom-candidate-warning-failed", "published", "failed")
	failedFollowup.QuestionRole = "followup"
	for _, atom := range []domain.InterviewKnowledgeAtom{opening, indexedFollowup, failedFollowup} {
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "健康 warning 候选样例"); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources":    []string{"health_diagnostic"},
		"domain":     "backend",
		"category":   "cache",
		"difficulty": "L3",
	})
	if status != http.StatusOK {
		t.Fatalf("candidate generation status=%d message=%s", status, env.Message)
	}
	var response domain.InterviewBankOpsActionCandidateResponse
	mustDecodeData(t, env, &response)
	if response.Total != 1 || len(response.List) != 1 {
		t.Fatalf("expected one warning candidate, got %+v", response)
	}
	candidate := response.List[0]
	if candidate.ActionType != domain.InterviewBankOpsActionTypeRebuildIndex || candidate.Priority != "P1" {
		t.Fatalf("unexpected warning candidate classification: %+v", candidate)
	}
	if candidate.Source != domain.InterviewBankOpsActionSourceHealthDiagnostic || candidate.DedupeKey != "rebuild_index|combo|backend|cache|L3" {
		t.Fatalf("unexpected warning candidate source/key: %+v", candidate)
	}
	if !strings.Contains(candidate.Reason, "索引失败") || candidate.Evidence["status"] != "warning" {
		t.Fatalf("expected warning evidence and reason, got %+v", candidate)
	}
}

func TestAdminInterviewBankOpsActionCandidatesFromIndexStatus(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	failed := validInterviewBankAtomForRebuild("atom-candidate-failed", "published", "failed")
	pending := validInterviewBankAtomForRebuild("atom-candidate-pending", "published", "pending")
	draft := validInterviewBankAtomForRebuild("atom-candidate-draft", "draft", "failed")
	archived := validInterviewBankAtomForRebuild("atom-candidate-archived", "archived", "failed")
	for _, atom := range []domain.InterviewKnowledgeAtom{failed, pending, draft, archived} {
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "索引候选样例"); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources": []string{"index_status"},
		"limit":   10,
	})
	if status != http.StatusOK {
		t.Fatalf("index candidate generation status=%d message=%s", status, env.Message)
	}
	var response domain.InterviewBankOpsActionCandidateResponse
	mustDecodeData(t, env, &response)
	if response.Total != 2 || len(response.List) != 2 {
		t.Fatalf("expected failed and pending candidates only, got %+v", response)
	}
	candidates := map[string]domain.InterviewBankOpsActionCandidate{}
	for _, candidate := range response.List {
		candidates[candidate.AtomID] = candidate
		if candidate.ActionType != domain.InterviewBankOpsActionTypeRebuildIndex || candidate.Source != domain.InterviewBankOpsActionSourceIndexStatus {
			t.Fatalf("unexpected index candidate: %+v", candidate)
		}
		if candidate.DedupeKey != "rebuild_index|atom|"+candidate.AtomID {
			t.Fatalf("unexpected atom dedupe key: %+v", candidate)
		}
		if _, ok := candidate.Evidence["principles"]; ok {
			t.Fatalf("candidate evidence must not include atom body content: %+v", candidate.Evidence)
		}
	}
	if candidates[failed.ID].Priority != "P1" {
		t.Fatalf("failed atom should be P1, got %+v", candidates[failed.ID])
	}
	if candidates[pending.ID].Priority != "P2" {
		t.Fatalf("pending atom should be P2, got %+v", candidates[pending.ID])
	}
	if _, ok := candidates[draft.ID]; ok {
		t.Fatalf("draft atom must not create index candidate: %+v", response.List)
	}
	if _, ok := candidates[archived.ID]; ok {
		t.Fatalf("archived atom must not create index candidate: %+v", response.List)
	}
}

func TestAdminInterviewBankOpsActionCandidatesFromRetrievalFallbacks(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	dataStore.InterviewSessions["session-candidate-fallback-hot"] = &domain.InterviewSession{
		ID: "session-candidate-fallback-hot",
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
		},
	}
	dataStore.InterviewSessions["session-candidate-fallback-light"] = &domain.InterviewSession{
		ID: "session-candidate-fallback-light",
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "database",
			Difficulty: "L2",
		},
	}
	for i := 0; i < 3; i++ {
		dataStore.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
			SessionID:    "session-candidate-fallback-hot",
			Round:        i + 1,
			QueryText:    "脱敏后的检索摘要，不应进入候选证据",
			FallbackUsed: true,
			ErrorMessage: "未命中可用题库追问原子，继续使用规则追问。",
			CreatedAt:    time.Now().Add(time.Duration(i) * time.Minute),
		})
	}
	dataStore.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID:    "session-candidate-fallback-light",
		Round:        1,
		QueryText:    "另一个回退摘要",
		FallbackUsed: true,
		ErrorMessage: "embedding client is not configured",
		CreatedAt:    time.Now().Add(5 * time.Minute),
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources": []string{"retrieval_analytics"},
		"limit":   10,
	})
	if status != http.StatusOK {
		t.Fatalf("retrieval candidate generation status=%d message=%s", status, env.Message)
	}
	var response domain.InterviewBankOpsActionCandidateResponse
	mustDecodeData(t, env, &response)
	if response.Total != 2 || len(response.List) != 2 {
		t.Fatalf("expected two fallback candidates, got %+v", response)
	}
	candidates := map[string]domain.InterviewBankOpsActionCandidate{}
	for _, candidate := range response.List {
		candidates[candidate.Category] = candidate
		if candidate.ActionType != domain.InterviewBankOpsActionTypeFillGap || candidate.Source != domain.InterviewBankOpsActionSourceRetrievalAnalytics {
			t.Fatalf("unexpected retrieval fallback candidate: %+v", candidate)
		}
		if _, ok := candidate.Evidence["query_text"]; ok {
			t.Fatalf("fallback evidence must not include full query text: %+v", candidate.Evidence)
		}
	}
	if candidates["cache"].Priority != "P0" || candidates["cache"].DedupeKey != "fill_gap|combo|backend|cache|L3" {
		t.Fatalf("expected hot fallback P0 candidate, got %+v", candidates["cache"])
	}
	if candidates["database"].Priority != "P1" || candidates["database"].DedupeKey != "fill_gap|combo|backend|database|L2" {
		t.Fatalf("expected light fallback P1 candidate, got %+v", candidates["database"])
	}
	if count, _ := candidates["cache"].Evidence["fallback_count"].(float64); count != 3 {
		t.Fatalf("expected fallback_count evidence, got %+v", candidates["cache"].Evidence)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions?status=open", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list ops actions after retrieval candidates status=%d message=%s", status, env.Message)
	}
	var listed struct {
		Total int `json:"total"`
	}
	mustDecodeData(t, env, &listed)
	if listed.Total != 0 {
		t.Fatalf("retrieval candidate generation must not persist actions, got %+v", listed)
	}
}

func TestAdminInterviewBankOpsActionCandidatesFromRetrievalLowHits(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	hitAtom := validInterviewBankAtomForRebuild("atom-candidate-retrieval-hit", "published", "indexed")
	hitAtom.QuestionRole = "followup"
	zeroHitAtom := validInterviewBankAtomForRebuild("atom-candidate-retrieval-zero-hit", "published", "indexed")
	zeroHitAtom.QuestionRole = "mixed"
	for _, atom := range []domain.InterviewKnowledgeAtom{hitAtom, zeroHitAtom} {
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "真实命中候选样例"); err != nil {
			t.Fatal(err)
		}
	}
	dataStore.InterviewSessions["session-candidate-hit"] = &domain.InterviewSession{
		ID: "session-candidate-hit",
		QuestionSnapshot: domain.InterviewQuestionSnapshot{
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
		},
	}
	dataStore.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
		SessionID: "session-candidate-hit",
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
		CreatedAt: time.Now(),
	})
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources":    []string{"retrieval_analytics"},
		"domain":     "backend",
		"category":   "cache",
		"difficulty": "L3",
	})
	if status != http.StatusOK {
		t.Fatalf("retrieval low-hit candidate status=%d message=%s", status, env.Message)
	}
	var response domain.InterviewBankOpsActionCandidateResponse
	mustDecodeData(t, env, &response)
	if response.Total != 1 || len(response.List) != 1 {
		t.Fatalf("expected one zero-hit observe candidate, got %+v", response)
	}
	candidate := response.List[0]
	if candidate.AtomID != zeroHitAtom.ID || candidate.ActionType != domain.InterviewBankOpsActionTypeObserve || candidate.Priority != "P3" {
		t.Fatalf("unexpected low-hit candidate: %+v", candidate)
	}
	if candidate.DedupeKey != "observe|atom|"+zeroHitAtom.ID {
		t.Fatalf("unexpected low-hit dedupe key: %+v", candidate)
	}
	if candidate.Evidence["hit_count"] != float64(0) {
		t.Fatalf("expected hit_count=0 evidence, got %+v", candidate.Evidence)
	}
	if _, ok := candidate.Evidence["principles"]; ok {
		t.Fatalf("low-hit evidence must not include atom body content: %+v", candidate.Evidence)
	}
}

func TestAdminInterviewBankOpsActionCandidatesSkipActiveDedupeOnly(t *testing.T) {
	t.Run("active action skips candidate", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		blocked := validInterviewBankAtomForRebuild("atom-candidate-active-dedupe", "published", "indexed")
		blocked.Category = "database"
		blocked.Difficulty = "L2"
		blocked.QuestionRole = "opening"
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(blocked, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "active dedupe 样例"); err != nil {
			t.Fatal(err)
		}
		if _, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P0",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "已有补题动作",
			Reason:     "已经进入处理队列。",
			Domain:     "backend",
			Category:   "database",
			Difficulty: "L2",
		}); err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
			"sources":    []string{"health_diagnostic"},
			"domain":     "backend",
			"category":   "database",
			"difficulty": "L2",
		})
		if status != http.StatusOK {
			t.Fatalf("candidate generation status=%d message=%s", status, env.Message)
		}
		var response domain.InterviewBankOpsActionCandidateResponse
		mustDecodeData(t, env, &response)
		if response.Total != 0 || response.SkippedExisting != 1 {
			t.Fatalf("expected active dedupe skip, got %+v", response)
		}
	})

	t.Run("active action skips retrieval candidate", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		dataStore.InterviewSessions["session-candidate-retrieval-active"] = &domain.InterviewSession{
			ID: "session-candidate-retrieval-active",
			QuestionSnapshot: domain.InterviewQuestionSnapshot{
				Domain:     "backend",
				Category:   "cache",
				Difficulty: "L3",
			},
		}
		dataStore.SaveInterviewRetrievalLog(domain.InterviewRetrievalLog{
			SessionID:    "session-candidate-retrieval-active",
			Round:        1,
			QueryText:    "脱敏后的回退摘要",
			FallbackUsed: true,
			ErrorMessage: "未命中可用题库追问原子。",
			CreatedAt:    time.Now(),
		})
		if _, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P1",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "已有真实回退补题动作",
			Reason:     "已经进入处理队列。",
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
		}); err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
			"sources":    []string{"retrieval_analytics"},
			"domain":     "backend",
			"category":   "cache",
			"difficulty": "L3",
		})
		if status != http.StatusOK {
			t.Fatalf("retrieval candidate generation status=%d message=%s", status, env.Message)
		}
		var response domain.InterviewBankOpsActionCandidateResponse
		mustDecodeData(t, env, &response)
		if response.Total != 0 || response.SkippedExisting != 1 {
			t.Fatalf("expected active retrieval dedupe skip, got %+v", response)
		}
	})

	t.Run("closed action does not skip candidate", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		blocked := validInterviewBankAtomForRebuild("atom-candidate-closed-dedupe", "published", "indexed")
		blocked.Category = "database"
		blocked.Difficulty = "L2"
		blocked.QuestionRole = "opening"
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(blocked, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "closed dedupe 样例"); err != nil {
			t.Fatal(err)
		}
		if _, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Status:     domain.InterviewBankOpsActionStatusResolved,
			Priority:   "P0",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "已关闭补题动作",
			Reason:     "历史问题已处理。",
			Domain:     "backend",
			Category:   "database",
			Difficulty: "L2",
		}); err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
			"sources":    []string{"health_diagnostic"},
			"domain":     "backend",
			"category":   "database",
			"difficulty": "L2",
		})
		if status != http.StatusOK {
			t.Fatalf("candidate generation status=%d message=%s", status, env.Message)
		}
		var response domain.InterviewBankOpsActionCandidateResponse
		mustDecodeData(t, env, &response)
		if response.Total != 1 || response.SkippedExisting != 0 {
			t.Fatalf("expected closed action to allow candidate, got %+v", response)
		}
	})
}

func TestAdminInterviewBankOpsActionCandidatesRequireAdminAndValidatePolicy(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	for _, id := range []string{"atom-candidate-limit-1", "atom-candidate-limit-2"} {
		atom := validInterviewBankAtomForRebuild(id, "published", "failed")
		if _, _, err := dataStore.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, "admin-1", "策略校验样例"); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", demoToken, map[string]interface{}{
		"sources": []string{"index_status"},
	})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student candidate generation code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources": []string{"retrieval_log"},
	})
	if status != http.StatusBadRequest || env.Message != "source is invalid" {
		t.Fatalf("expected invalid source rejection, status=%d env=%+v", status, env)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources": []string{"index_status"},
		"limit":   1,
	})
	if status != http.StatusOK {
		t.Fatalf("candidate policy status=%d message=%s", status, env.Message)
	}
	var response domain.InterviewBankOpsActionCandidateResponse
	mustDecodeData(t, env, &response)
	if response.Policy.Limit != 1 || response.Total != 1 || len(response.List) != 1 {
		t.Fatalf("expected limit=1 to cap returned candidates, got %+v", response)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates", adminToken, map[string]interface{}{
		"sources": []string{"index_status", "index_status"},
		"limit":   999,
	})
	if status != http.StatusOK {
		t.Fatalf("candidate max policy status=%d message=%s", status, env.Message)
	}
	mustDecodeData(t, env, &response)
	if response.Policy.Limit != 200 || len(response.Policy.Sources) != 1 || response.Policy.Sources[0] != domain.InterviewBankOpsActionSourceIndexStatus {
		t.Fatalf("expected normalized policy, got %+v", response.Policy)
	}
}

func TestAdminInterviewBankOpsActionSaveCandidatesPersistsGeneratedActions(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	candidate := domain.InterviewBankOpsActionCandidate{
		CandidateKey: "health_diagnostic|fill_gap|combo|backend|cache|L3",
		ActionType:   domain.InterviewBankOpsActionTypeFillGap,
		Priority:     "P0",
		Source:       domain.InterviewBankOpsActionSourceHealthDiagnostic,
		DedupeKey:    "fill_gap|combo|backend|cache|L3",
		Title:        "补齐 backend/cache/L3 题库资源",
		Reason:       "健康诊断显示该组合 blocked。",
		Domain:       "backend",
		Category:     "cache",
		Difficulty:   "L3",
		Evidence: map[string]interface{}{
			"status": "blocked",
		},
	}
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
		"candidates": []domain.InterviewBankOpsActionCandidate{candidate},
	})
	if status != http.StatusOK {
		t.Fatalf("save candidates status=%d message=%s", status, env.Message)
	}
	var response struct {
		List            []domain.InterviewBankOpsAction `json:"list"`
		Saved           int                             `json:"saved"`
		Total           int                             `json:"total"`
		SkippedExisting int                             `json:"skipped_existing"`
	}
	mustDecodeData(t, env, &response)
	if response.Saved != 1 || response.Total != 1 || response.SkippedExisting != 0 || len(response.List) != 1 {
		t.Fatalf("unexpected save response: %+v", response)
	}
	saved := response.List[0]
	if saved.Source != domain.InterviewBankOpsActionSourceHealthDiagnostic || saved.Status != domain.InterviewBankOpsActionStatusOpen {
		t.Fatalf("candidate save should preserve generated source and force open status, got %+v", saved)
	}
	if saved.DedupeKey != candidate.DedupeKey || saved.CreatedBy == "" {
		t.Fatalf("candidate save should preserve dedupe and created_by, got %+v", saved)
	}
	if saved.Evidence["status"] != "blocked" {
		t.Fatalf("candidate evidence should be persisted compactly, got %+v", saved.Evidence)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/admin/interview-bank/ops-actions?status=open", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list saved candidates status=%d message=%s", status, env.Message)
	}
	var listed struct {
		List  []domain.InterviewBankOpsAction `json:"list"`
		Total int                             `json:"total"`
	}
	mustDecodeData(t, env, &listed)
	if listed.Total != 1 || len(listed.List) != 1 || listed.List[0].Source != domain.InterviewBankOpsActionSourceHealthDiagnostic {
		t.Fatalf("expected saved candidate in open queue, got %+v", listed)
	}
}

func TestAdminInterviewBankOpsActionSaveCandidatesDedupePolicy(t *testing.T) {
	t.Run("active action skips candidate save", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		if _, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Status:     domain.InterviewBankOpsActionStatusOpen,
			Priority:   "P0",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "已有补题动作",
			Reason:     "已经进入处理队列。",
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
		}); err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
			"candidates": []domain.InterviewBankOpsActionCandidate{{
				ActionType: domain.InterviewBankOpsActionTypeFillGap,
				Priority:   "P0",
				Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
				DedupeKey:  "fill_gap|combo|backend|cache|L3",
				Title:      "补齐真实回退组合 backend/cache/L3 题库资源",
				Reason:     "真实回退组合需要补题。",
				Domain:     "backend",
				Category:   "cache",
				Difficulty: "L3",
				Evidence:   map[string]interface{}{"fallback_count": 3},
			}},
		})
		if status != http.StatusOK {
			t.Fatalf("save active duplicate status=%d message=%s", status, env.Message)
		}
		var response struct {
			Saved           int `json:"saved"`
			Total           int `json:"total"`
			SkippedExisting int `json:"skipped_existing"`
		}
		mustDecodeData(t, env, &response)
		if response.Saved != 0 || response.Total != 0 || response.SkippedExisting != 1 {
			t.Fatalf("expected active duplicate skip, got %+v", response)
		}
	})

	t.Run("resolved action allows candidate save", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		if _, err := dataStore.CreateInterviewBankOpsAction(domain.InterviewBankOpsAction{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Status:     domain.InterviewBankOpsActionStatusResolved,
			Priority:   "P0",
			Source:     domain.InterviewBankOpsActionSourceManual,
			Title:      "已解决补题动作",
			Reason:     "历史问题已解决。",
			Domain:     "backend",
			Category:   "cache",
			Difficulty: "L3",
		}); err != nil {
			t.Fatal(err)
		}
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")

		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
			"candidates": []domain.InterviewBankOpsActionCandidate{{
				ActionType: domain.InterviewBankOpsActionTypeFillGap,
				Priority:   "P1",
				Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
				DedupeKey:  "fill_gap|combo|backend|cache|L3",
				Title:      "重新补齐真实回退组合 backend/cache/L3 题库资源",
				Reason:     "近期又出现真实回退。",
				Domain:     "backend",
				Category:   "cache",
				Difficulty: "L3",
				Evidence:   map[string]interface{}{"fallback_count": 1},
			}},
		})
		if status != http.StatusOK {
			t.Fatalf("save after resolved status=%d message=%s", status, env.Message)
		}
		var response struct {
			Saved           int `json:"saved"`
			SkippedExisting int `json:"skipped_existing"`
		}
		mustDecodeData(t, env, &response)
		if response.Saved != 1 || response.SkippedExisting != 0 {
			t.Fatalf("expected resolved dedupe to allow save, got %+v", response)
		}
	})

	t.Run("same request duplicate key saves once", func(t *testing.T) {
		dataStore := store.NewMemoryStore(auth.HashPassword)
		handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
		adminToken := loginToken(t, handler, "admin", "admin123")
		candidate := domain.InterviewBankOpsActionCandidate{
			ActionType: domain.InterviewBankOpsActionTypeFillGap,
			Priority:   "P1",
			Source:     domain.InterviewBankOpsActionSourceRetrievalAnalytics,
			DedupeKey:  "fill_gap|combo|frontend|frontend|L2",
			Title:      "补齐真实回退组合 frontend/frontend/L2 题库资源",
			Reason:     "真实回退组合需要补题。",
			Domain:     "frontend",
			Category:   "frontend",
			Difficulty: "L2",
			Evidence:   map[string]interface{}{"fallback_count": 2},
		}

		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
			"candidates": []domain.InterviewBankOpsActionCandidate{candidate, candidate},
		})
		if status != http.StatusOK {
			t.Fatalf("save same request duplicate status=%d message=%s", status, env.Message)
		}
		var response struct {
			Saved           int `json:"saved"`
			Total           int `json:"total"`
			SkippedExisting int `json:"skipped_existing"`
		}
		mustDecodeData(t, env, &response)
		if response.Saved != 1 || response.Total != 1 || response.SkippedExisting != 1 {
			t.Fatalf("expected same request duplicate save once, got %+v", response)
		}
	})
}

func TestAdminInterviewBankOpsActionSaveCandidatesRequireAdminAndValidate(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	validCandidate := domain.InterviewBankOpsActionCandidate{
		ActionType: domain.InterviewBankOpsActionTypeRebuildIndex,
		Priority:   "P1",
		Source:     domain.InterviewBankOpsActionSourceIndexStatus,
		DedupeKey:  "rebuild_index|atom|atom-save-candidate-invalid",
		Title:      "重建题库索引：候选保存校验",
		Reason:     "索引状态 failed。",
		AtomID:     "atom-save-candidate-invalid",
		Evidence:   map[string]interface{}{"vector_status": "failed"},
	}
	_, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", demoToken, map[string]interface{}{
		"candidates": []domain.InterviewBankOpsActionCandidate{validCandidate},
	})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student candidate save code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
		"candidates": []domain.InterviewBankOpsActionCandidate{},
	})
	if status != http.StatusBadRequest || env.Message != "candidates is required" {
		t.Fatalf("expected empty candidate rejection, status=%d env=%+v", status, env)
	}

	invalidSource := validCandidate
	invalidSource.Source = domain.InterviewBankOpsActionSourceManual
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
		"candidates": []domain.InterviewBankOpsActionCandidate{invalidSource},
	})
	if status != http.StatusBadRequest || env.Message != "candidate source is invalid" {
		t.Fatalf("expected invalid source rejection, status=%d env=%+v", status, env)
	}

	missingDedupe := validCandidate
	missingDedupe.DedupeKey = ""
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
		"candidates": []domain.InterviewBankOpsActionCandidate{missingDedupe},
	})
	if status != http.StatusBadRequest || env.Message != "candidate dedupe_key is required" {
		t.Fatalf("expected missing dedupe rejection, status=%d env=%+v", status, env)
	}

	missingTarget := validCandidate
	missingTarget.AtomID = ""
	missingTarget.Domain = ""
	missingTarget.Category = ""
	missingTarget.Difficulty = ""
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/admin/interview-bank/ops-actions/candidates/save", adminToken, map[string]interface{}{
		"candidates": []domain.InterviewBankOpsActionCandidate{missingTarget},
	})
	if status != http.StatusBadRequest || env.Message != "target scope is required" {
		t.Fatalf("expected missing target rejection, status=%d env=%+v", status, env)
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
				"id":               atomID,
				"title":            "缓存击穿治理",
				"subject":          "缓存击穿治理",
				"opening_question": "热点缓存失效后大量请求同时回源，你会先采取什么措施？",
				"question_type":    "troubleshooting",
				"stable_code":      legacyInterviewStableCode("cache", atomID),
				"domain":           "backend",
				"difficulty":       "L3",
				"category":         "cache",
				"question_role":    "mixed",
				"source_ref":       "fixture/cache-breakdown",
				"tags":             []string{"cache", "hot-key", "cache"},
				"principles":       []string{"说明互斥锁或 singleflight 控制并发回源", "说明热点 key 预热和过期时间抖动"},
				"pitfalls":         []string{"只说加缓存但不处理失效瞬间并发", "忽略数据库被瞬时流量打满的风险"},
				"follow_up_paths":  []string{"追问缓存雪崩和缓存穿透的差异", "追问本地缓存与分布式缓存的一致性取舍"},
				"vector_status":    "failed",
			},
		},
	}
}

func validInterviewBankAtomForRebuild(id, status, vectorStatus string) domain.InterviewKnowledgeAtom {
	return domain.InterviewKnowledgeAtom{
		ID:              id,
		Title:           "缓存击穿治理",
		Subject:         "缓存击穿治理",
		OpeningQuestion: "热点缓存失效后大量请求同时回源，你会先采取什么措施？",
		QuestionType:    "troubleshooting",
		StableCode:      legacyInterviewStableCode("cache", id),
		Domain:          "backend",
		Difficulty:      "L3",
		Category:        "cache",
		QuestionRole:    "mixed",
		SourceRef:       "fixture/cache-breakdown",
		Tags:            []string{"cache", "hot-key"},
		Principles:      []string{"说明互斥锁或 singleflight 控制并发回源", "说明热点 key 预热和过期时间抖动"},
		Pitfalls:        []string{"只说加缓存但不处理失效瞬间并发", "忽略数据库被瞬时流量打满的风险"},
		FollowUpPaths:   []string{"追问缓存雪崩和缓存穿透的差异", "追问本地缓存与分布式缓存的一致性取舍"},
		Status:          status,
		VectorStatus:    vectorStatus,
	}
}

func validInterviewBankUpdatePayload(atom domain.InterviewKnowledgeAtom) map[string]interface{} {
	return map[string]interface{}{
		"base_version":     atom.CurrentVersion,
		"change_note":      "管理员在线编辑",
		"title":            atom.Title,
		"subject":          atom.Subject,
		"opening_question": atom.OpeningQuestion,
		"question_type":    atom.QuestionType,
		"stable_code":      atom.StableCode,
		"domain":           atom.Domain,
		"difficulty":       atom.Difficulty,
		"category":         atom.Category,
		"question_role":    atom.QuestionRole,
		"source_ref":       atom.SourceRef,
		"tags":             append([]string{}, atom.Tags...),
		"principles":       append([]string{}, atom.Principles...),
		"pitfalls":         append([]string{}, atom.Pitfalls...),
		"follow_up_paths":  append([]string{}, atom.FollowUpPaths...),
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
