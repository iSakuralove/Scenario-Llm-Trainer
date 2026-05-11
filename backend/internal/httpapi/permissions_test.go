package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

type testEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestRegisterDefaultsToStudentAndAdminCanUpdateRole(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	authManager := auth.NewManager("test-secret", time.Hour)
	handler := NewServerForTests(dataStore, authManager).Handler()

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "learner",
		"email":    "learner@example.com",
		"password": "secret123",
	})
	if status != http.StatusOK {
		t.Fatalf("register status=%d message=%s", status, env.Message)
	}
	var registered struct {
		User        domain.User `json:"user"`
		AccessToken string      `json:"access_token"`
	}
	mustDecodeData(t, env, &registered)
	if registered.User.Role != domain.RoleStudent {
		t.Fatalf("expected student role, got %q", registered.User.Role)
	}
	claims, err := authManager.Validate(registered.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != domain.RoleStudent {
		t.Fatalf("expected token role student, got %q", claims.Role)
	}

	_, studentEnv := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/users", registered.AccessToken, nil)
	if studentEnv.Code != http.StatusForbidden {
		t.Fatalf("student admin access code=%d", studentEnv.Code)
	}

	adminToken := loginToken(t, handler, "admin", "admin123")
	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/admin/users/"+registered.User.ID+"/role", adminToken, map[string]string{"role": domain.RoleInstructor})
	if status != http.StatusOK {
		t.Fatalf("update role status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Role != domain.RoleInstructor {
		t.Fatalf("expected instructor role, got %q", updated.Role)
	}
}

func TestSystemStatusRequiresAdminAndReturnsRunbook(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student system status code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin system status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Services []map[string]interface{} `json:"services"`
		Runbook  []map[string]string      `json:"runbook"`
		Counts   map[string]int           `json:"counts"`
	}
	mustDecodeData(t, env, &payload)
	if len(payload.Services) < 4 || len(payload.Runbook) == 0 {
		t.Fatalf("unexpected system status payload: %+v", payload)
	}
	if payload.Counts["active_scenarios"] == 0 {
		t.Fatalf("expected active scenario count, got %+v", payload.Counts)
	}
}

func TestCommunityDoubleReviewPermissionsAndPublishing(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	authManager := auth.NewManager("test-secret", time.Hour)
	handler := NewServerForTests(dataStore, authManager).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	post := createCommunityPost(t, handler, demoToken, "缓存规则变更导致回源升高")

	_, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", demoToken, map[string]string{"decision": "approve"})
	if env.Code != http.StatusForbidden {
		t.Fatalf("student instructor review code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "approve",
		"note":     "结构完整，适合进入终审。",
	})
	if status != http.StatusOK {
		t.Fatalf("instructor review status=%d message=%s", status, env.Message)
	}
	var reviewed domain.CommunityPost
	mustDecodeData(t, env, &reviewed)
	if reviewed.Status != "instructor_approved" || reviewed.ReviewedBy == "" || reviewed.ReviewedAt == nil {
		t.Fatalf("unexpected reviewed post: %+v", reviewed)
	}
	if reviewed.AuthorUsername != "demo" {
		t.Fatalf("expected author username demo, got %+v", reviewed)
	}

	_, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", instructorToken, map[string]string{"decision": "publish"})
	if env.Code != http.StatusForbidden {
		t.Fatalf("instructor final review code=%d", env.Code)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", adminToken, map[string]string{
		"decision": "publish",
		"note":     "发布为排查题。",
	})
	if status != http.StatusOK {
		t.Fatalf("final review status=%d message=%s", status, env.Message)
	}
	var finalData struct {
		Post     domain.CommunityPost        `json:"post"`
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &finalData)
	if finalData.Post.Status != "published" || finalData.Post.ConvertedQuestionID == "" {
		t.Fatalf("expected published post with question id, got %+v", finalData.Post)
	}
	if finalData.Post.AuthorUsername != "demo" {
		t.Fatalf("expected published post to include author username, got %+v", finalData.Post)
	}
	if finalData.Question.Source != "ugc_structured" || finalData.Question.CreatedBy != "user-admin" {
		t.Fatalf("unexpected converted question: %+v", finalData.Question)
	}

	unreviewedPost := createCommunityPost(t, handler, demoToken, "Visible pending post without instructor history")
	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?view=instructor_reviewed", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("instructor reviewed list status=%d message=%s", status, env.Message)
	}
	var reviewedList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &reviewedList)
	foundReviewed := false
	for _, item := range reviewedList.List {
		if item.ID == post.ID {
			foundReviewed = true
			if item.Status != "published" {
				t.Fatalf("expected reviewed history to keep current status published, got %+v", item)
			}
			if item.AuthorUsername != "demo" {
				t.Fatalf("expected reviewed list to include author username, got %+v", item)
			}
			break
		}
	}
	if !foundReviewed {
		t.Fatalf("expected instructor reviewed history to include published post, got %+v", reviewedList.List)
	}
	for _, item := range reviewedList.List {
		if item.ID == unreviewedPost.ID {
			t.Fatalf("instructor reviewed history should exclude posts without instructor approval history: %+v", reviewedList.List)
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=instructor_approved", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("instructor approved status list status=%d message=%s", status, env.Message)
	}
	var approvedStatusList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &approvedStatusList)
	for _, item := range approvedStatusList.List {
		if item.ID == post.ID {
			t.Fatalf("published post should not remain in instructor_approved status list: %+v", approvedStatusList.List)
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+finalData.Post.ConvertedQuestionID, demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("student scenario detail status=%d message=%s", status, env.Message)
	}
	var studentView domain.ScenarioQuestionView
	mustDecodeData(t, env, &studentView)
	if !studentView.IsSanitized || studentView.Content.RootCause != "" || studentView.Content.StandardProcedure != nil {
		t.Fatalf("expected sanitized student view, got %+v", studentView)
	}
}

func TestCommunityFinalPublishFallsBackInvalidMermaid(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	authManager := auth.NewManager("test-secret", time.Hour)
	handler := NewServerForTests(dataStore, authManager).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	post := createCommunityPost(t, handler, demoToken, "手工 Mermaid 坏图发布兜底")
	edited := completeForkContent(post.AIStructuredContent)
	edited.ArchitectureDiagramSpec = nil
	edited.ArchitectureDiagram = "graph TD\nA[API --> B[DB]"

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]interface{}{
		"decision":           "approve",
		"note":               "结构完整，图形由终审发布前兜底。",
		"structured_content": edited,
	})
	if status != http.StatusOK {
		t.Fatalf("instructor review status=%d message=%s", status, env.Message)
	}
	var reviewed domain.CommunityPost
	mustDecodeData(t, env, &reviewed)
	if reviewed.EditedStructuredContent == nil || reviewed.EditedStructuredContent.DiagramStatus != "fallback" {
		t.Fatalf("expected instructor review response to fallback invalid mermaid, got %+v", reviewed.EditedStructuredContent)
	}
	if strings.Contains(reviewed.EditedStructuredContent.ArchitectureDiagram, "A[API --> B[DB]") {
		t.Fatalf("invalid mermaid leaked into review response: %q", reviewed.EditedStructuredContent.ArchitectureDiagram)
	}
	storedPost, ok := dataStore.GetCommunityPost(post.ID)
	if !ok || storedPost.EditedStructuredContent == nil {
		t.Fatalf("missing stored reviewed post: %+v", storedPost)
	}
	if storedPost.EditedStructuredContent.DiagramStatus != "fallback" {
		t.Fatalf("expected stored edited content to fallback invalid mermaid, got %+v", storedPost.EditedStructuredContent)
	}
	if strings.Contains(storedPost.EditedStructuredContent.ArchitectureDiagram, "A[API --> B[DB]") {
		t.Fatalf("invalid mermaid persisted in edited content: %q", storedPost.EditedStructuredContent.ArchitectureDiagram)
	}
	if len(storedPost.ReviewHistory) == 0 || storedPost.ReviewHistory[0].Content == nil {
		t.Fatalf("expected review history content, got %+v", storedPost.ReviewHistory)
	}
	if strings.Contains(storedPost.ReviewHistory[0].Content.ArchitectureDiagram, "A[API --> B[DB]") {
		t.Fatalf("invalid mermaid persisted in review history: %q", storedPost.ReviewHistory[0].Content.ArchitectureDiagram)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", adminToken, map[string]string{
		"decision": "publish",
		"note":     "发布为排查题。",
	})
	if status != http.StatusOK {
		t.Fatalf("final review status=%d message=%s", status, env.Message)
	}
	var finalData struct {
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &finalData)
	if finalData.Question.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected fallback diagram status, got %+v", finalData.Question.Content)
	}
	if !strings.HasPrefix(finalData.Question.Content.ArchitectureDiagram, "graph TD") {
		t.Fatalf("expected fallback mermaid graph, got %q", finalData.Question.Content.ArchitectureDiagram)
	}
	if strings.Contains(finalData.Question.Content.ArchitectureDiagram, "A[API --> B[DB]") {
		t.Fatalf("invalid mermaid leaked into published question: %q", finalData.Question.Content.ArchitectureDiagram)
	}
}

func TestScenarioForkCreatesEditableDraftThenSubmitReview(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/fork", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("fork status=%d message=%s", status, env.Message)
	}
	var forkedPost domain.CommunityPost
	mustDecodeData(t, env, &forkedPost)
	if forkedPost.Status != "draft" || forkedPost.UserID != "user-demo" || forkedPost.ConvertedQuestionID != "" || forkedPost.ForkedFromScenarioID != question.ID {
		t.Fatalf("unexpected forked post: %+v", forkedPost)
	}
	if forkedPost.AIStructuredContent.RootCause != "" || len(forkedPost.AIStructuredContent.KeyEvidence) > 0 || len(forkedPost.AIStructuredContent.StandardProcedure) > 0 {
		t.Fatalf("fork draft leaked answer content: %+v", forkedPost.AIStructuredContent)
	}
	if containsString(forkedPost.Tags, "娲剧敓") {
		t.Fatalf("fork draft should not include mojibake tag: %+v", forkedPost.Tags)
	}
	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=draft", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("author draft list status=%d message=%s", status, env.Message)
	}
	var authorDraftList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &authorDraftList)
	authorDraftFound := false
	for _, post := range authorDraftList.List {
		if post.ID == forkedPost.ID {
			authorDraftFound = true
			break
		}
	}
	if !authorDraftFound {
		t.Fatalf("expected author to see fork draft in own draft list, got %+v", authorDraftList.List)
	}
	_, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+forkedPost.ID+"/submit", demoToken, nil)
	if env.Code != http.StatusBadRequest {
		t.Fatalf("expected incomplete fork draft rejection, got code=%d message=%s", env.Code, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=draft", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("draft list status=%d message=%s", status, env.Message)
	}
	var draftList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &draftList)
	for _, post := range draftList.List {
		if post.ID == forkedPost.ID {
			t.Fatalf("instructor should not see author draft before submit: %+v", draftList.List)
		}
	}
	_, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts/"+forkedPost.ID, instructorToken, nil)
	if env.Code != http.StatusNotFound {
		t.Fatalf("instructor should not fetch author draft, got code=%d", env.Code)
	}

	edited := completeForkContent(forkedPost.AIStructuredContent)
	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/community/posts/"+forkedPost.ID, demoToken, map[string]interface{}{
		"title":              "派生题目：作者自定义版本",
		"raw_content":        "作者修改后的现象描述，避免与原题一模一样。",
		"domain":             "database",
		"tags":               []string{"派生", "自定义"},
		"structured_content": edited,
	})
	if status != http.StatusOK {
		t.Fatalf("update fork draft status=%d message=%s", status, env.Message)
	}
	var updatedDraft domain.CommunityPost
	mustDecodeData(t, env, &updatedDraft)
	if updatedDraft.Status != "draft" || updatedDraft.EditedStructuredContent == nil || updatedDraft.EditedStructuredContent.RootCause != edited.RootCause {
		t.Fatalf("expected editable draft, got %+v", updatedDraft)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+forkedPost.ID+"/submit", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("submit fork draft status=%d message=%s", status, env.Message)
	}
	var submitted domain.CommunityPost
	mustDecodeData(t, env, &submitted)
	if submitted.Status != "pending_review" {
		t.Fatalf("expected submitted pending review, got %+v", submitted)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=pending_review", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("community list status=%d message=%s", status, env.Message)
	}
	var listData struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &listData)
	found := false
	for _, post := range listData.List {
		if post.ID == forkedPost.ID && post.Status == "pending_review" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected forked scenario in instructor review queue, got %+v", listData.List)
	}
}

func TestScenarioForkRejectsNonActiveScenario(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")

	hidden := dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "待审核题目",
		Description:  "尚未公开的题目不应允许派生。",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"待审"},
		Content:      completeForkContent(domain.ScenarioContent{}),
		Status:       "pending",
		Source:       "llm_generated",
		CreatedBy:    "user-admin",
		Version:      1,
	})

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+hidden.ID+"/fork", demoToken, nil)
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected hidden scenario fork rejection, status=%d env=%+v", status, env)
	}
}

func TestCommunityInstructorEditedContentAndReviewHistory(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	post := createCommunityPost(t, handler, demoToken, "讲师编辑结构化内容案例")
	edited := post.AIStructuredContent
	edited.RootCause = "讲师修正后的根因：缓存 key 规则变更导致热点回源，password=secret。"
	edited.KeyEvidence = []string{"命中率下降发生在发布后", "回源请求集中在 10.2.3.4", "回滚规则后命中率恢复"}
	edited.StandardProcedure = []string{"确认异常窗口", "对比 api_key=sk-demo 相关变更", "观察命中率和回源量", "灰度回滚并复盘"}

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]interface{}{
		"decision":           "approve",
		"note":               "已补齐证据链。",
		"structured_content": edited,
	})
	if status != http.StatusOK {
		t.Fatalf("instructor review status=%d message=%s", status, env.Message)
	}
	var reviewed domain.CommunityPost
	mustDecodeData(t, env, &reviewed)
	if reviewed.EditedStructuredContent == nil || reviewed.EditedStructuredContent.RootCause == edited.RootCause {
		t.Fatalf("expected edited structured content, got %+v", reviewed.EditedStructuredContent)
	}
	if strings.Contains(reviewed.EditedStructuredContent.RootCause, "password=secret") {
		t.Fatalf("expected instructor root cause to be sanitized: %q", reviewed.EditedStructuredContent.RootCause)
	}
	if len(reviewed.ReviewHistory) != 1 || reviewed.ReviewHistory[0].Action != "instructor_approve" {
		t.Fatalf("unexpected instructor history: %+v", reviewed.ReviewHistory)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", adminToken, map[string]string{
		"decision": "publish",
		"note":     "使用讲师编辑版本发布。",
	})
	if status != http.StatusOK {
		t.Fatalf("final review status=%d message=%s", status, env.Message)
	}
	var finalData struct {
		Post     domain.CommunityPost        `json:"post"`
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &finalData)
	if len(finalData.Post.ReviewHistory) != 2 || finalData.Post.ReviewHistory[1].Action != "final_publish" {
		t.Fatalf("unexpected final history: %+v", finalData.Post.ReviewHistory)
	}
	created, ok := dataStore.GetScenario(finalData.Post.ConvertedQuestionID)
	if !ok {
		t.Fatal("missing converted scenario")
	}
	if strings.Contains(created.Content.RootCause, "password=secret") || strings.Contains(strings.Join(created.Content.StandardProcedure, "\n"), "sk-demo") {
		t.Fatalf("converted scenario leaked sensitive content: %+v", created.Content)
	}
	if len(created.Content.KeyEvidence) != len(edited.KeyEvidence) {
		t.Fatalf("converted evidence mismatch: %+v", created.Content.KeyEvidence)
	}
}

func TestCommunityReviewQueuesExposeModerationSummaryOnlyToReviewers(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	post := createCommunityPost(t, handler, demoToken, "CM 审核摘要可见性案例")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts/"+post.ID, demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("author get post status=%d message=%s", status, env.Message)
	}
	var authorView domain.CommunityPost
	mustDecodeData(t, env, &authorView)
	if authorView.ModerationSummary != nil {
		t.Fatalf("author should not receive moderation summary: %+v", authorView.ModerationSummary)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=pending_review", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("review queue status=%d message=%s", status, env.Message)
	}
	var instructorList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &instructorList)
	var instructorView *domain.CommunityPost
	for i := range instructorList.List {
		if instructorList.List[i].ID == post.ID {
			instructorView = &instructorList.List[i]
			break
		}
	}
	if instructorView == nil {
		t.Fatalf("expected instructor queue to include post, got %+v", instructorList.List)
	}
	if instructorView.ModerationSummary == nil || strings.TrimSpace(instructorView.ModerationSummary.SafeSummary) == "" {
		t.Fatalf("expected instructor to receive moderation summary: %+v", instructorView)
	}
	if instructorView.ModerationSummary.AgentTrace != nil {
		t.Fatalf("expected moderation summary trace to stay private: %+v", instructorView.ModerationSummary)
	}
	for _, forbidden := range []string{"agent_trace", "tool_args", "prompt"} {
		if strings.Contains(strings.ToLower(string(env.Data)), forbidden) {
			t.Fatalf("review queue leaked forbidden key %q in payload: %s", forbidden, string(env.Data))
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts/"+post.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin get post status=%d message=%s", status, env.Message)
	}
	var adminView domain.CommunityPost
	mustDecodeData(t, env, &adminView)
	if adminView.ModerationSummary == nil {
		t.Fatalf("expected admin to receive moderation summary")
	}
}

func TestUserHistoryIncludesOwnCommunityPosts(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	post := createCommunityPost(t, handler, demoToken, "个人档案投稿回显案例")
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "approve",
		"note":     "结构完整，提交终审。",
	})
	if status != http.StatusOK {
		t.Fatalf("instructor review status=%d message=%s", status, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", adminToken, map[string]string{
		"decision": "publish",
		"note":     "发布为正式题。",
	})
	if status != http.StatusOK {
		t.Fatalf("final review status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/users/me/history", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("history status=%d message=%s", status, env.Message)
	}
	var history struct {
		CommunityPosts []domain.CommunityPost `json:"community_posts"`
	}
	mustDecodeData(t, env, &history)
	if len(history.CommunityPosts) == 0 {
		t.Fatalf("expected community posts in user history")
	}
	found := false
	for _, item := range history.CommunityPosts {
		if item.ID == post.ID {
			found = true
			if item.Status != "published" || item.ConvertedQuestionID == "" {
				t.Fatalf("expected published post in history, got %+v", item)
			}
			if item.ModerationSummary != nil {
				t.Fatalf("student history should not expose moderation summary: %+v", item.ModerationSummary)
			}
		}
		if item.UserID != "user-demo" {
			t.Fatalf("history leaked another user's post: %+v", item)
		}
	}
	if !found {
		t.Fatalf("expected own post in history, got %+v", history.CommunityPosts)
	}
}

func TestRejectedCommunityPostDoesNotCreateScenario(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	before := len(dataStore.ListScenarios("", "", ""))

	post := createCommunityPost(t, handler, demoToken, "缺少证据的案例")
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "reject",
		"note":     "关键证据不足。",
	})
	if status != http.StatusOK {
		t.Fatalf("reject status=%d message=%s", status, env.Message)
	}
	var rejected domain.CommunityPost
	mustDecodeData(t, env, &rejected)
	if rejected.Status != "instructor_rejected" || rejected.ConvertedQuestionID != "" {
		t.Fatalf("unexpected rejected post: %+v", rejected)
	}
	if after := len(dataStore.ListScenarios("", "", "")); after != before {
		t.Fatalf("scenario count changed after reject: before=%d after=%d", before, after)
	}
}

func TestCommunityFinalRejectReturnsPostToInstructorReviewQueue(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")
	before := len(dataStore.ListScenarios("", "", ""))

	post := createCommunityPost(t, handler, demoToken, "终审退回后重新初审案例")
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "approve",
		"note":     "结构完整，提交终审。",
	})
	if status != http.StatusOK {
		t.Fatalf("instructor approve status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", adminToken, map[string]string{
		"decision": "reject",
		"note":     "终审发现证据链仍需补充，请重新初审。",
	})
	if status != http.StatusOK {
		t.Fatalf("final reject status=%d message=%s", status, env.Message)
	}
	var finalData struct {
		Post domain.CommunityPost `json:"post"`
	}
	mustDecodeData(t, env, &finalData)
	if finalData.Post.Status != "pending_review" || finalData.Post.ConvertedQuestionID != "" {
		t.Fatalf("expected final reject to return pending review without scenario, got %+v", finalData.Post)
	}
	if finalData.Post.FinalNote == "" || finalData.Post.FinalizedBy != "user-admin" || finalData.Post.FinalizedAt == nil {
		t.Fatalf("expected final reject note and actor metadata, got %+v", finalData.Post)
	}
	if len(finalData.Post.ReviewHistory) != 2 || finalData.Post.ReviewHistory[1].Action != "final_reject" || finalData.Post.ReviewHistory[1].ToStatus != "pending_review" {
		t.Fatalf("expected final reject history to point back to pending review, got %+v", finalData.Post.ReviewHistory)
	}
	if after := len(dataStore.ListScenarios("", "", "")); after != before {
		t.Fatalf("scenario count changed after final reject: before=%d after=%d", before, after)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=pending_review", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("pending review queue status=%d message=%s", status, env.Message)
	}
	var pendingList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &pendingList)
	found := false
	for _, item := range pendingList.List {
		if item.ID == post.ID {
			found = true
			if item.Status != "pending_review" || item.FinalNote == "" {
				t.Fatalf("expected returned post in instructor queue with final note, got %+v", item)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected final rejected post to re-enter pending review queue, got %+v", pendingList.List)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "approve",
		"note":     "已按终审意见补齐后重新提交。",
	})
	if status != http.StatusOK {
		t.Fatalf("re-review after final reject status=%d message=%s", status, env.Message)
	}
	var reReviewed domain.CommunityPost
	mustDecodeData(t, env, &reReviewed)
	if reReviewed.Status != "instructor_approved" {
		t.Fatalf("expected instructor to re-approve returned post, got %+v", reReviewed)
	}
}

func TestCommunityInstructorRejectedHistoryView(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")

	post := createCommunityPost(t, handler, demoToken, "Instructor rejected history case")
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "reject",
		"note":     "missing evidence",
	})
	if status != http.StatusOK {
		t.Fatalf("reject status=%d message=%s", status, env.Message)
	}
	var rejected domain.CommunityPost
	mustDecodeData(t, env, &rejected)
	if rejected.Status != "instructor_rejected" {
		t.Fatalf("expected instructor_rejected, got %+v", rejected)
	}

	unrejectedPost := createCommunityPost(t, handler, demoToken, "Visible pending post without rejection history")
	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?view=instructor_rejected", instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("instructor rejected list status=%d message=%s", status, env.Message)
	}
	var rejectedList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &rejectedList)
	foundRejected := false
	for _, item := range rejectedList.List {
		if item.ID == post.ID {
			foundRejected = true
			if item.Status != "instructor_rejected" {
				t.Fatalf("expected rejected history to keep current status instructor_rejected, got %+v", item)
			}
			break
		}
	}
	if !foundRejected {
		t.Fatalf("expected instructor rejected history to include rejected post, got %+v", rejectedList.List)
	}
	for _, item := range rejectedList.List {
		if item.ID == unrejectedPost.ID {
			t.Fatalf("instructor rejected history should exclude posts without instructor rejection history: %+v", rejectedList.List)
		}
	}
}

func TestAuthorCanDeleteRejectedForkButReviewerCannotDeleteDraft(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/fork", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("fork status=%d message=%s", status, env.Message)
	}
	var forkedPost domain.CommunityPost
	mustDecodeData(t, env, &forkedPost)

	_, env = requestJSON(t, handler, http.MethodDelete, "/api/v1/community/posts/"+forkedPost.ID, instructorToken, nil)
	if env.Code != http.StatusNotFound && env.Code != http.StatusForbidden {
		t.Fatalf("reviewer should not delete hidden author draft, got code=%d message=%s", env.Code, env.Message)
	}

	complete := completeForkContent(forkedPost.AIStructuredContent)
	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/community/posts/"+forkedPost.ID, demoToken, map[string]interface{}{
		"structured_content": complete,
	})
	if status != http.StatusOK {
		t.Fatalf("complete fork draft status=%d message=%s", status, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+forkedPost.ID+"/submit", demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("submit fork draft status=%d message=%s", status, env.Message)
	}
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+forkedPost.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "reject",
		"note":     "evidence is not enough",
	})
	if status != http.StatusOK {
		t.Fatalf("reject fork status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodDelete, "/api/v1/community/posts/"+forkedPost.ID, demoToken, nil)
	if status != http.StatusOK {
		t.Fatalf("author delete rejected fork status=%d message=%s", status, env.Message)
	}
	_, ok := dataStore.GetCommunityPost(forkedPost.ID)
	if ok {
		t.Fatal("expected rejected fork to be deleted")
	}
}

func TestAdminCanDeletePublishedCommunityPostAndArchiveConvertedScenario(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	authManager := auth.NewManager("test-secret", time.Hour)
	handler := NewServerForTests(dataStore, authManager).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	post := createCommunityPost(t, handler, demoToken, "管理员删除已发布案例")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/instructor-review", instructorToken, map[string]string{
		"decision": "approve",
		"note":     "结构完整，适合进入终审。",
	})
	if status != http.StatusOK {
		t.Fatalf("instructor review status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+post.ID+"/final-review", adminToken, map[string]string{
		"decision": "publish",
		"note":     "发布为正式题。",
	})
	if status != http.StatusOK {
		t.Fatalf("final review status=%d message=%s", status, env.Message)
	}
	var finalData struct {
		Post     domain.CommunityPost        `json:"post"`
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &finalData)
	if finalData.Post.Status != "published" || finalData.Post.ConvertedQuestionID == "" {
		t.Fatalf("expected published post with converted scenario, got %+v", finalData.Post)
	}

	status, env = requestJSON(t, handler, http.MethodDelete, "/api/v1/community/posts/"+post.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin delete published post status=%d message=%s", status, env.Message)
	}

	if _, ok := dataStore.GetCommunityPost(post.ID); ok {
		t.Fatal("expected published community post to be deleted")
	}

	archivedScenario, ok := dataStore.GetScenario(finalData.Post.ConvertedQuestionID)
	if !ok {
		t.Fatalf("expected converted scenario %s to remain for history linkage", finalData.Post.ConvertedQuestionID)
	}
	if archivedScenario.Status == "active" {
		t.Fatalf("expected converted scenario to be archived, got %+v", archivedScenario)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?status=published", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("published list status=%d message=%s", status, env.Message)
	}
	var publishedList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &publishedList)
	for _, item := range publishedList.List {
		if item.ID == post.ID {
			t.Fatalf("deleted published post should not remain in published list: %+v", publishedList.List)
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/community/posts?scope=all", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("all list status=%d message=%s", status, env.Message)
	}
	var allList struct {
		List []domain.CommunityPost `json:"list"`
	}
	mustDecodeData(t, env, &allList)
	for _, item := range allList.List {
		if item.ID == post.ID {
			t.Fatalf("deleted published post should not remain in all list: %+v", allList.List)
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+finalData.Post.ConvertedQuestionID, demoToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("archived converted scenario should not remain visible in scenarios detail, status=%d message=%s", status, env.Message)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin scenario list after delete status=%d message=%s", status, env.Message)
	}
	var scenarioList struct {
		List []domain.ScenarioQuestionView `json:"list"`
	}
	mustDecodeData(t, env, &scenarioList)
	for _, item := range scenarioList.List {
		if item.ID == finalData.Post.ConvertedQuestionID {
			t.Fatalf("archived converted scenario should not remain in workshop list for admin: %+v", scenarioList.List)
		}
		if item.Status != "active" {
			t.Fatalf("workshop list should only expose active scenarios, got %+v", item)
		}
	}
}

func createCommunityPost(t *testing.T, handler http.Handler, token string, title string) domain.CommunityPost {
	t.Helper()
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts", token, map[string]interface{}{
		"title":       title,
		"raw_content": "发布后缓存 key 规则发生变化，缓存命中率下降，数据库读请求升高。",
		"domain":      "database",
		"tags":        []string{"缓存", "变更"},
	})
	if status != http.StatusOK {
		t.Fatalf("create community post status=%d message=%s", status, env.Message)
	}
	var post domain.CommunityPost
	mustDecodeData(t, env, &post)
	if post.Status != "pending_review" {
		t.Fatalf("expected pending_review, got %q", post.Status)
	}
	return post
}

func completeForkContent(content domain.ScenarioContent) domain.ScenarioContent {
	content.RootCause = "作者自定义后的派生根因。"
	content.RootCauseKeywords = []string{"派生", "根因"}
	content.KeyEvidence = []string{"作者补充的关键证据一", "作者补充的关键证据二"}
	content.StandardProcedure = []string{"确认异常窗口", "验证关键证据", "提交修复和回滚方案"}
	return content
}

func loginToken(t *testing.T, handler http.Handler, identifier string, password string) string {
	t.Helper()
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": identifier,
		"password":   password,
	})
	if status != http.StatusOK {
		t.Fatalf("login %s status=%d message=%s", identifier, status, env.Message)
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	mustDecodeData(t, env, &data)
	if data.AccessToken == "" {
		t.Fatal("missing access token")
	}
	return data.AccessToken
}

func requestJSON(t *testing.T, handler http.Handler, method string, path string, token string, body interface{}) (int, testEnvelope) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var env testEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	return rr.Code, env
}

func mustDecodeData(t *testing.T, env testEnvelope, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(env.Data, target); err != nil {
		t.Fatalf("decode data: %v raw=%s", err, string(env.Data))
	}
}
