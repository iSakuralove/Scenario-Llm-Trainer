package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestAISafetyCheckMergesRuleAndModelFindings(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	secret := "sk-stage24-secret"
	password := "secret123"
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/ai/safety/check", token, map[string]string{
		"field": "raw_content",
		"text":  "ACME Corp uses svc-order with password=" + password + " and api_key=" + secret + " from 10.1.2.3.",
	})
	if status != http.StatusOK {
		t.Fatalf("safety check status=%d message=%s", status, env.Message)
	}
	raw := string(env.Data)
	if strings.Contains(raw, secret) || strings.Contains(raw, password) {
		t.Fatalf("safety response leaked secret data: %s", raw)
	}

	var result domain.SensitiveCheckResult
	mustDecodeData(t, env, &result)
	if result.Status != "risk" || result.Source != "rule+model" || result.RiskLevel != "high" || !result.Blocked {
		t.Fatalf("expected high risk merged result, got %+v", result)
	}
	if len(result.Findings) < 3 {
		t.Fatalf("expected multiple rule and model findings, got %+v", result.Findings)
	}
	sources := map[string]bool{}
	for _, finding := range result.Findings {
		sources[finding.Source] = true
		if strings.Contains(finding.Excerpt, secret) || strings.Contains(finding.RedactedExcerpt, secret) {
			t.Fatalf("finding leaked raw key: %+v", finding)
		}
		if finding.Confidence <= 0 || finding.Confidence > 1 {
			t.Fatalf("finding confidence out of range: %+v", finding)
		}
	}
	if !sources["rule"] || !sources["model"] {
		t.Fatalf("expected rule and model sources, got %+v in %+v", sources, result.Findings)
	}
}

func TestCommunityCreatePersistsEnhancedSensitiveCheck(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts", token, map[string]interface{}{
		"title":       "ACME Corp cache incident",
		"raw_content": "The svc-cache topology exposed password=secret123 and api_key=sk-stage24-secret from 10.1.2.3.",
		"domain":      "database",
		"tags":        []string{"ACME", "svc-cache"},
	})
	if status != http.StatusOK {
		t.Fatalf("create community post status=%d message=%s", status, env.Message)
	}
	raw := string(env.Data)
	if strings.Contains(raw, "sk-stage24-secret") || strings.Contains(raw, "secret123") {
		t.Fatalf("community create response leaked secret data: %s", raw)
	}

	var post domain.CommunityPost
	mustDecodeData(t, env, &post)
	if post.SensitiveCheck.Source != "rule+model" || post.SensitiveCheck.Status != "risk" {
		t.Fatalf("expected enhanced sensitive check on post, got %+v", post.SensitiveCheck)
	}
	if len(post.SensitiveCheck.Findings) == 0 {
		t.Fatalf("expected sensitive findings on post: %+v", post)
	}

	stored, ok := dataStore.GetCommunityPost(post.ID)
	if !ok {
		t.Fatal("missing stored community post")
	}
	if stored.SensitiveCheck.Source != "rule+model" || len(stored.SensitiveCheck.Findings) == 0 {
		t.Fatalf("expected stored enhanced sensitive check, got %+v", stored.SensitiveCheck)
	}
}

func TestForkDraftEditAndSubmitRecalculateSensitiveCheck(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/fork", token, nil)
	if status != http.StatusOK {
		t.Fatalf("fork status=%d message=%s", status, env.Message)
	}
	var forked domain.CommunityPost
	mustDecodeData(t, env, &forked)
	if forked.SensitiveCheck.Source != "rule+model" {
		t.Fatalf("expected fork creation to use enhanced check, got %+v", forked.SensitiveCheck)
	}

	edited := completeForkContent(forked.AIStructuredContent)
	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/community/posts/"+forked.ID, token, map[string]interface{}{
		"title":              "ACME Corp derived incident",
		"raw_content":        "Author draft mentions svc-billing topology and api_key=sk-stage24-edited.",
		"domain":             "database",
		"tags":               []string{"ACME", "svc-billing"},
		"structured_content": edited,
	})
	if status != http.StatusOK {
		t.Fatalf("update fork draft status=%d message=%s", status, env.Message)
	}
	var updated domain.CommunityPost
	mustDecodeData(t, env, &updated)
	if updated.SensitiveCheck.Status != "risk" || updated.SensitiveCheck.Source != "rule+model" {
		t.Fatalf("expected draft update to recalculate sensitive check, got %+v", updated.SensitiveCheck)
	}
	if strings.Contains(string(env.Data), "sk-stage24-edited") {
		t.Fatalf("draft update response leaked raw key: %s", string(env.Data))
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts/"+forked.ID+"/submit", token, nil)
	if status != http.StatusOK {
		t.Fatalf("submit fork draft status=%d message=%s", status, env.Message)
	}
	var submitted domain.CommunityPost
	mustDecodeData(t, env, &submitted)
	if submitted.Status != "pending_review" || submitted.SensitiveCheck.Source != "rule+model" || len(submitted.SensitiveCheck.Findings) == 0 {
		t.Fatalf("expected submit to preserve recalculated sensitive check, got %+v", submitted)
	}
	if strings.Contains(string(env.Data), "sk-stage24-edited") {
		t.Fatalf("submit response leaked raw key: %s", string(env.Data))
	}
}

func TestAISafetyCheckFallbackDoesNotExposeProviderErrorToStudent(t *testing.T) {
	providerServer := failingSensitiveProvider(t)
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(
		dataStore,
		auth.NewManager("test-secret", time.Hour),
		nil,
		ai.NewRouter(ai.Config{
			Provider: ai.ProviderOpenAICompatible,
			BaseURL:  providerServer,
			APIKey:   "test-key",
			Model:    "test-model",
			Timeout:  time.Second,
		}),
	)
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/ai/safety/check", token, map[string]string{
		"field": "raw_content",
		"text":  "Only rule risk password=secret123 and api_key=sk-stage24-secret.",
	})
	if status != http.StatusOK {
		t.Fatalf("fallback safety check status=%d message=%s", status, env.Message)
	}
	raw := string(env.Data)
	if strings.Contains(raw, "invalid-json") || strings.Contains(raw, "provider failed") || strings.Contains(raw, "sk-stage24-secret") {
		t.Fatalf("fallback response exposed raw details: %s", raw)
	}
	var result domain.SensitiveCheckResult
	mustDecodeData(t, env, &result)
	if !result.FallbackUsed || result.Source != "rule_fallback" || result.Status != "risk" {
		t.Fatalf("expected rule fallback result, got %+v", result)
	}

	events := dataStore.ListAuditEvents(10)
	foundFallback := false
	for _, event := range events {
		if event.Action == "ai.safety_check_fallback" {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Fatalf("expected fallback audit event, got %+v", events)
	}
}

func TestScenarioSessionSnapshotDescriptionIsSanitized(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	question := dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "敏感描述排查题 token=title-secret",
		Description:  "真实案例 password=[已脱敏]123456，并包含 api_key=sk-session-secret。",
		Domain:       "security",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"security", "key=tag-secret"},
		Content: domain.ScenarioContent{
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{},
				DeepClues:    []domain.Clue{},
				Distractors:  []domain.Clue{},
			},
			ArchitectureDiagram: "graph TD\nA[应用] --> B[配置]",
			ReferenceLinks:      []string{},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create scenario session status=%d message=%s", status, env.Message)
	}
	var payload struct {
		QuestionSnapshot domain.ScenarioQuestionView `json:"question_snapshot"`
	}
	mustDecodeData(t, env, &payload)
	if strings.Contains(payload.QuestionSnapshot.Description, "123456") || strings.Contains(payload.QuestionSnapshot.Description, "sk-session-secret") {
		t.Fatalf("scenario snapshot leaked sensitive description: %q", payload.QuestionSnapshot.Description)
	}
	if strings.Contains(payload.QuestionSnapshot.Title, "title-secret") || strings.Contains(strings.Join(payload.QuestionSnapshot.Tags, ","), "tag-secret") {
		t.Fatalf("scenario snapshot leaked sensitive title or tags: %+v", payload.QuestionSnapshot)
	}
	if !strings.Contains(payload.QuestionSnapshot.Description, "已脱敏") {
		t.Fatalf("expected sanitized marker in description: %q", payload.QuestionSnapshot.Description)
	}
}

func TestScenarioSessionSnapshotAlwaysUsesPublicSC04View(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	studentToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	question := dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "腾讯公司案例 AI 模型key=sk-admin-visible",
		Description:  "马哥教育真实案例 密码为12345asdfasd@123qq.com,API KEY=[已脱敏];df'hww@@",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"马哥教育", "api_key=sk-session-secret"},
		Content: domain.ScenarioContent{
			RootCause:         "真实根因为 password=root-secret",
			KeyEvidence:       []string{"日志包含 api_key=sk-evidence-secret"},
			StandardProcedure: []string{"按内部 runbook 处理 token=procedure-secret"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{{ClueID: "c1", Content: "表层线索 password=surface-secret"}},
				DeepClues:    []domain.Clue{{ClueID: "c2", Content: "深层线索 api_key=sk-deep-secret"}},
				Distractors:  []domain.Clue{{ClueID: "d1", Content: "干扰项 token=distractor-secret"}},
			},
			ArchitectureDiagram: "graph TD\nA[马哥教育] --> B[password=diagram-secret]",
			ReferenceLinks:      []string{"https://internal.example.com?token=link-secret"},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", studentToken, nil)
	if status != http.StatusOK {
		t.Fatalf("student create scenario session status=%d message=%s", status, env.Message)
	}
	var studentPayload struct {
		QuestionSnapshot domain.ScenarioQuestionView `json:"question_snapshot"`
	}
	mustDecodeData(t, env, &studentPayload)
	studentRaw := string(env.Data)
	for _, leaked := range []string{"腾讯公司", "马哥教育", "12345asdfasd", "123qq.com", "df'hww@@", "sk-session-secret", "surface-secret", "diagram-secret", "link-secret"} {
		if strings.Contains(studentRaw, leaked) {
			t.Fatalf("student snapshot leaked %q in %s", leaked, studentRaw)
		}
	}
	if !studentPayload.QuestionSnapshot.IsSanitized || studentPayload.QuestionSnapshot.Content.RootCause != "" || studentPayload.QuestionSnapshot.Content.StandardProcedure != nil {
		t.Fatalf("expected student SC-04 sanitized snapshot, got %+v", studentPayload.QuestionSnapshot)
	}
	if len(studentPayload.QuestionSnapshot.Content.RevealStrategy.SurfaceClues) != 0 || len(studentPayload.QuestionSnapshot.Content.RevealStrategy.DeepClues) != 0 || len(studentPayload.QuestionSnapshot.Content.RevealStrategy.Distractors) != 0 {
		t.Fatalf("expected student snapshot to hide full reveal strategy, got %+v", studentPayload.QuestionSnapshot.Content.RevealStrategy)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin create scenario session status=%d message=%s", status, env.Message)
	}
	var adminPayload struct {
		QuestionSnapshot domain.ScenarioQuestionView `json:"question_snapshot"`
	}
	mustDecodeData(t, env, &adminPayload)
	adminRaw := string(env.Data)
	for _, leaked := range []string{"腾讯公司", "马哥教育", "12345asdfasd", "123qq.com", "df'hww@@", "password=root-secret", "surface-secret", "diagram-secret", "link-secret"} {
		if strings.Contains(adminRaw, leaked) {
			t.Fatalf("admin scenario session snapshot leaked %q in %s", leaked, adminRaw)
		}
	}
	if !adminPayload.QuestionSnapshot.IsSanitized || adminPayload.QuestionSnapshot.Content.RootCause != "" || adminPayload.QuestionSnapshot.Content.StandardProcedure != nil {
		t.Fatalf("expected admin scenario session to receive public snapshot, got %+v", adminPayload.QuestionSnapshot)
	}
	if len(adminPayload.QuestionSnapshot.Content.RevealStrategy.SurfaceClues) != 0 || len(adminPayload.QuestionSnapshot.Content.RevealStrategy.DeepClues) != 0 || len(adminPayload.QuestionSnapshot.Content.RevealStrategy.Distractors) != 0 {
		t.Fatalf("expected admin scenario session snapshot to hide full reveal strategy, got %+v", adminPayload.QuestionSnapshot.Content.RevealStrategy)
	}
}

func TestScenarioSessionResponsesKeepInternalSnapshotPrivate(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	question := dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "腾讯公司案例 AI 模型key=sk-admin-visible",
		Description:  "马哥教育真实案例 密码为12345asdfasd@123qq.com,API KEY=[已脱敏];df'hww@@",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"马哥教育", "api_key=sk-session-secret"},
		Content: domain.ScenarioContent{
			RootCause:         "真实根因为 password=root-secret",
			RootCauseKeywords: []string{"root-secret"},
			KeyEvidence:       []string{"日志包含 api_key=sk-evidence-secret"},
			StandardProcedure: []string{"按内部 runbook 处理 token=procedure-secret"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{{ClueID: "c1", TriggerKeywords: []string{"日志"}, Content: "表层线索 password=surface-secret"}},
				DeepClues:    []domain.Clue{{ClueID: "c2", TriggerKeywords: []string{"索引"}, Content: "深层线索 api_key=sk-deep-secret"}},
				Distractors:  []domain.Clue{{ClueID: "d1", TriggerKeywords: []string{"网络"}, Content: "干扰项 token=distractor-secret"}},
			},
			ArchitectureDiagram: "graph TD\nA[马哥教育] --> B[password=diagram-secret]",
			ReferenceLinks:      []string{"https://internal.example.com?token=link-secret"},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create scenario session status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", token, map[string]string{
		"content": "请从日志维度给出下一步线索",
	})
	if status != http.StatusOK {
		t.Fatalf("message status=%d message=%s", status, env.Message)
	}
	assertScenarioSessionPayloadSanitized(t, string(env.Data))

	body, _ := json.Marshal(map[string]string{"content": "请继续从索引维度给出下一步线索"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("message SSE status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertScenarioSessionPayloadSanitized(t, rr.Body.String())

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+created.SessionID+"/quit", token, nil)
	if status != http.StatusOK {
		t.Fatalf("quit status=%d message=%s", status, env.Message)
	}
	assertScenarioSessionPayloadSanitized(t, string(env.Data))

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/users/me/history", token, nil)
	if status != http.StatusOK {
		t.Fatalf("history status=%d message=%s", status, env.Message)
	}
	assertScenarioSessionPayloadSanitized(t, string(env.Data))
}

func assertScenarioSessionPayloadSanitized(t *testing.T, raw string) {
	t.Helper()
	for _, leaked := range []string{"腾讯公司", "马哥教育", "12345asdfasd", "123qq.com", "df'hww@@", "sk-session-secret", "password=root-secret", "surface-secret", "diagram-secret", "link-secret"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("scenario session response leaked %q in %s", leaked, raw)
		}
	}
	if !strings.Contains(raw, `"is_sanitized":true`) {
		t.Fatalf("expected sanitized session view in %s", raw)
	}
	if strings.Contains(raw, `"root_cause"`) || strings.Contains(raw, `"standard_procedure"`) || strings.Contains(raw, `"key_evidence"`) || strings.Contains(raw, `"surface_clues":[{`) {
		t.Fatalf("scenario session response exposed answer fields in %s", raw)
	}
}

func TestScenarioListViewAppliesFieldLevelSanitization(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "马哥教育真实案例",
		Description:  "密码为12345asdfasd@123qq.com,API KEY=[已脱敏];df'hww@@",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"马哥教育", "api_key=sk-list-secret"},
		Content: domain.ScenarioContent{
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{},
				DeepClues:    []domain.Clue{},
				Distractors:  []domain.Clue{},
			},
			ReferenceLinks: []string{},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios?tag=马哥教育", token, nil)
	if status != http.StatusOK {
		t.Fatalf("scenario list status=%d message=%s", status, env.Message)
	}
	raw := string(env.Data)
	for _, leaked := range []string{"马哥教育", "12345asdfasd", "123qq.com", "df'hww@@", "sk-list-secret"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("scenario list leaked %q in %s", leaked, raw)
		}
	}
	if !strings.Contains(raw, "【") {
		t.Fatalf("expected field-level placeholders in %s", raw)
	}
}

func TestScenarioListViewSanitizesNaturalLanguageCredentials(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "js++公司案例",
		Description:  "有位员工A忘记了后台管理密码，找了上层B设置成了alsjkgdlaz124@qq_，然后上层B问他有没有AI KEY，他说有的，sl-saklsdhglasdghl;asgz",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"缓存", "变更"},
		Content: domain.ScenarioContent{
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{},
				DeepClues:    []domain.Clue{},
				Distractors:  []domain.Clue{},
			},
			ReferenceLinks: []string{},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios", token, nil)
	if status != http.StatusOK {
		t.Fatalf("scenario list status=%d message=%s", status, env.Message)
	}
	raw := string(env.Data)
	for _, leaked := range []string{"js++公司", "alsjkgdlaz124", "qq_", "sl-saklsdhglasdghl"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("scenario list leaked %q in %s", leaked, raw)
		}
	}
	if !strings.Contains(raw, "【机构A】") || !strings.Contains(raw, "【密钥】") {
		t.Fatalf("expected organization and credential placeholders in %s", raw)
	}
}

func TestScenarioListAlwaysUsesPublicSC04ViewForPrivilegedRoles(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")
	instructorToken := loginToken(t, handler, "instructor", "instructor123")

	dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "腾讯公司案例 AI 模型key=sk-admin-visible",
		Description:  "马哥教育真实案例 密码为12345asdfasd@123qq.com,API KEY=[已脱敏];df'hww@@",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"马哥教育", "api_key=sk-list-secret"},
		Content: domain.ScenarioContent{
			RootCause:         "真实根因为 password=root-secret",
			KeyEvidence:       []string{"日志包含 api_key=sk-evidence-secret"},
			StandardProcedure: []string{"按内部 runbook 处理 token=procedure-secret"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{{ClueID: "c1", Content: "表层线索 password=surface-secret"}},
				DeepClues:    []domain.Clue{{ClueID: "c2", Content: "深层线索 api_key=sk-deep-secret"}},
				Distractors:  []domain.Clue{{ClueID: "d1", Content: "干扰项 token=distractor-secret"}},
			},
			ArchitectureDiagram: "graph TD\nA[马哥教育] --> B[password=diagram-secret]",
			ReferenceLinks:      []string{"https://internal.example.com?token=link-secret"},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	for _, tc := range []struct {
		name  string
		token string
	}{
		{name: "admin", token: adminToken},
		{name: "instructor", token: instructorToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios", tc.token, nil)
			if status != http.StatusOK {
				t.Fatalf("scenario list status=%d message=%s", status, env.Message)
			}
			raw := string(env.Data)
			for _, leaked := range []string{"腾讯公司", "马哥教育", "12345asdfasd", "123qq.com", "df'hww@@", "sk-list-secret", "password=root-secret", "surface-secret"} {
				if strings.Contains(raw, leaked) {
					t.Fatalf("%s scenario list leaked %q in %s", tc.name, leaked, raw)
				}
			}
			if !strings.Contains(raw, `"is_sanitized":true`) || strings.Contains(raw, `"root_cause"`) || strings.Contains(raw, `"surface_clues":[{`) {
				t.Fatalf("expected privileged list to use public SC-04 DTO, got %s", raw)
			}
		})
	}
}

func TestScenarioDetailUsesFullSC04ViewForPrivilegedRoles(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	studentToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	question := dataStore.AddScenario(domain.ScenarioQuestion{
		Title:        "腾讯公司案例 AI 模型key=sk-admin-visible",
		Description:  "马哥教育真实案例 密码为12345asdfasd@123qq.com",
		Domain:       "database",
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         []string{"马哥教育", "api_key=sk-detail-secret"},
		Content: domain.ScenarioContent{
			RootCause:         "真实根因为 password=root-secret",
			KeyEvidence:       []string{"日志包含 api_key=sk-evidence-secret"},
			StandardProcedure: []string{"按内部 runbook 处理 token=procedure-secret"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{{ClueID: "c1", Content: "表层线索 password=surface-secret"}},
				DeepClues:    []domain.Clue{{ClueID: "c2", Content: "深层线索 api_key=sk-deep-secret"}},
				Distractors:  []domain.Clue{{ClueID: "d1", Content: "干扰项 token=distractor-secret"}},
			},
			ArchitectureDiagram: "graph TD\nA[马哥教育] --> B[password=diagram-secret]",
			ReferenceLinks:      []string{"https://internal.example.com?token=link-secret"},
		},
		Status:    "active",
		Source:    "test",
		CreatedBy: "user-admin",
		Version:   1,
	})

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+question.ID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin detail status=%d message=%s", status, env.Message)
	}
	adminRaw := string(env.Data)
	for _, expected := range []string{"腾讯公司案例", "sk-admin-visible", "马哥教育真实案例", "12345asdfasd@123qq.com", "password=root-secret", "surface-secret"} {
		if !strings.Contains(adminRaw, expected) {
			t.Fatalf("admin detail should keep full field %q in %s", expected, adminRaw)
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/scenarios/"+question.ID, studentToken, nil)
	if status != http.StatusOK {
		t.Fatalf("student detail status=%d message=%s", status, env.Message)
	}
	studentRaw := string(env.Data)
	for _, leaked := range []string{"腾讯公司", "马哥教育", "12345asdfasd", "sk-admin-visible", "password=root-secret", "surface-secret"} {
		if strings.Contains(studentRaw, leaked) {
			t.Fatalf("student detail leaked %q in %s", leaked, studentRaw)
		}
	}
	if !strings.Contains(studentRaw, `"is_sanitized":true`) || strings.Contains(studentRaw, `"root_cause"`) || strings.Contains(studentRaw, `"surface_clues":[{`) {
		t.Fatalf("expected student detail to use public SC-04 DTO, got %s", studentRaw)
	}
}

func failingSensitiveProvider(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected provider path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"invalid-json"}}]}`))
	}))
	t.Cleanup(server.Close)
	return server.URL
}
