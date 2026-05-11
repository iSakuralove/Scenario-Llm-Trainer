package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestScenarioGenerationJobCompletes(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
		"tags":          []string{"demo", "async"},
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)
	if created.Job.ID == "" || created.Job.Status != domain.AIJobStatusQueued {
		t.Fatalf("unexpected created job: %+v", created.Job)
	}

	var job *domain.AIJob
	for i := 0; i < 20; i++ {
		current, ok := dataStore.GetAIJob(created.Job.ID)
		if ok && current.Status == domain.AIJobStatusCompleted {
			job = current
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("generation job did not complete")
	}
	if job.ResultQuestionID == "" || job.Progress != 100 {
		t.Fatalf("unexpected completed job: %+v", job)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/ai/jobs/"+created.Job.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get generation job status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Job      domain.AIJob                `json:"job"`
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Job.Status != domain.AIJobStatusCompleted || payload.Question.ID == "" {
		t.Fatalf("unexpected job payload: %+v", payload)
	}
	if payload.Question.Content.ArchitectureDiagram == "" {
		t.Fatalf("expected generated question content, got %+v", payload.Question)
	}
	events := dataStore.ListAuditEvents(20)
	foundAudit := false
	for _, event := range events {
		if event.Action != "scenario.generate.completed" || event.ResourceID != payload.Question.ID {
			continue
		}
		foundAudit = true
		if event.ActorID != "user-demo" || event.Metadata["job_id"] != created.Job.ID || event.Metadata["difficulty"] != "L2" || event.Metadata["store_mode"] != "memory" {
			t.Fatalf("unexpected generation audit metadata: %+v", event)
		}
	}
	if !foundAudit {
		t.Fatalf("expected scenario generation audit event, got %+v", events)
	}
}

func TestScenarioGenerationJobUsesRequestedDifficulty(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L4",
		"scenario_type": "troubleshooting",
		"tags":          []string{"demo", "difficulty"},
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)
	job := waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusCompleted)
	if job.ResultQuestionID == "" {
		t.Fatalf("expected completed job with question id, got %+v", job)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/ai/jobs/"+created.Job.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get generation job status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Job      domain.AIJob                `json:"job"`
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Question.Difficulty != "L4" {
		t.Fatalf("expected generated question difficulty L4, got %+v", payload.Question)
	}
}

func TestScenarioGenerationRejectsInvalidDifficulty(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "LX",
		"scenario_type": "troubleshooting",
		"tags":          []string{"demo", "invalid"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected invalid difficulty to return 400, got status=%d message=%s", status, env.Message)
	}
	if !strings.Contains(env.Message, "difficulty") {
		t.Fatalf("expected difficulty validation message, got %q", env.Message)
	}
}

func TestScenarioGenerationRejectsInvalidScenarioType(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "chaos",
		"tags":          []string{"demo", "invalid"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected invalid scenario_type to return 400, got status=%d message=%s", status, env.Message)
	}
	if !strings.Contains(env.Message, "scenario_type") {
		t.Fatalf("expected scenario_type validation message, got %q", env.Message)
	}
}

func TestScenarioGenerationAcceptsConstraintsAndPersistsAuditMetadata(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L3",
		"scenario_type": "troubleshooting",
		"constraints": map[string]interface{}{
			"title":           "约束标题",
			"description":     "约束描述",
			"topic_scope":     []string{"主从复制", "读流量"},
			"root_cause_hint": "从库延迟",
			"evidence_hints":  []string{"Seconds_Behind_Master 上升"},
			"clue_hints":      []string{"主库正常"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)
	job := waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusCompleted)
	if job.ResultQuestionID == "" {
		t.Fatalf("expected completed job with question id, got %+v", job)
	}

	events := dataStore.ListAuditEvents(20)
	for _, event := range events {
		if event.Action != "scenario.generate.completed" || event.Metadata["job_id"] != created.Job.ID {
			continue
		}
		if event.Metadata["has_constraints"] != "true" {
			t.Fatalf("expected has_constraints=true, got %+v", event.Metadata)
		}
		if event.Metadata["creator_role"] != domain.RoleStudent {
			t.Fatalf("expected creator_role student, got %+v", event.Metadata)
		}
		if !strings.Contains(event.Metadata["constraint_fields"], "title") {
			t.Fatalf("expected constraint fields to include title, got %+v", event.Metadata)
		}
		return
	}
	t.Fatalf("expected scenario generation audit event with constraints metadata")
}

func TestScenarioGenerationRejectsSensitiveConstraintText(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
		"constraints": map[string]interface{}{
			"description": "生产密码 password=Secret123!",
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected sensitive constraints to return 400, got status=%d message=%s", status, env.Message)
	}
	if !strings.Contains(strings.ToLower(env.Message), "sensitive") && !strings.Contains(env.Message, "敏感") {
		t.Fatalf("expected sensitive validation message, got %q", env.Message)
	}
}

func TestScenarioGenerationRejectsDuplicateTitleConstraint(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	existing := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        existing.Domain,
		"difficulty":    existing.Difficulty,
		"scenario_type": existing.ScenarioType,
		"constraints": map[string]interface{}{
			"title": existing.Title,
		},
	})
	if status != http.StatusConflict {
		t.Fatalf("expected duplicate constraints to return 409, got status=%d message=%s", status, env.Message)
	}
	if !strings.Contains(strings.ToLower(env.Message), "duplicate") && !strings.Contains(env.Message, "重复") {
		t.Fatalf("expected duplicate validation message, got %q", env.Message)
	}
}

func TestScenarioGenerationJobUsesDefaultPayloadValues(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)
	waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusCompleted)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/ai/jobs/"+created.Job.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get generation job status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Question domain.ScenarioQuestionView `json:"question"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Question.Domain != "database" || payload.Question.Difficulty != "L2" || payload.Question.ScenarioType != "troubleshooting" {
		t.Fatalf("expected default database/L2/troubleshooting, got %+v", payload.Question)
	}
}

func TestSystemStatusReturnsProviderPoolSnapshot(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	adminToken := loginToken(t, handler, "admin", "admin123")

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("system status=%d message=%s", status, env.Message)
	}
	var payload struct {
		AI struct {
			ProviderPool ai.ProviderPoolSnapshot `json:"provider_pool"`
		} `json:"ai"`
	}
	mustDecodeData(t, env, &payload)
	pool := payload.AI.ProviderPool
	if pool.ActiveProvider == "" {
		t.Fatalf("expected active provider in pool: %+v", pool)
	}
	if len(pool.FallbackOrder) < 3 || pool.FallbackOrder[0] != ai.ProviderDeepSeek || pool.FallbackOrder[len(pool.FallbackOrder)-1] != ai.ProviderMock {
		t.Fatalf("expected default fallback order, got %+v", pool.FallbackOrder)
	}
	if pool.ProviderByName(ai.ProviderMock) == nil {
		t.Fatalf("expected mock provider in pool: %+v", pool)
	}
}

func TestScenarioGenerationJobsUpdateRouterTelemetry(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	for i := 0; i < 2; i++ {
		status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
			"domain":        "database",
			"difficulty":    "L2",
			"scenario_type": "troubleshooting",
			"tags":          []string{"demo", "router"},
		})
		if status != http.StatusOK {
			t.Fatalf("create generation job %d status=%d message=%s", i+1, status, env.Message)
		}
		var created struct {
			Job domain.AIJob `json:"job"`
		}
		mustDecodeData(t, env, &created)
		waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusCompleted)
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("system status=%d message=%s", status, env.Message)
	}
	var payload struct {
		AI struct {
			Telemetry ai.RouterTelemetry `json:"telemetry"`
		} `json:"ai"`
	}
	mustDecodeData(t, env, &payload)
	if payload.AI.Telemetry.TotalCalls != 2 || payload.AI.Telemetry.TaskCalls[ai.RouterTaskScenarioGenerate] != 2 {
		t.Fatalf("expected two scenario_generate calls, got %+v", payload.AI.Telemetry)
	}
}

func TestScenarioGenerationJobPersistsActualModel(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "deepseek-v4-flash" {
			t.Fatalf("expected scenario generation model deepseek-v4-flash, got %s", req.Model)
		}
		_, _ = w.Write([]byte(openAICompatibleScenarioResponse("job model scenario")))
	}))
	defer provider.Close()

	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{
		Provider: ai.ProviderDeepSeek,
		BaseURL:  provider.URL,
		APIKey:   "deepseek-key",
		Model:    "deepseek-chat",
		Timeout:  time.Second,
	}))
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
		"tags":          []string{"demo", "model"},
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)
	if created.Job.Provider != ai.ProviderDeepSeek || created.Job.Model != "deepseek-v4-flash" {
		t.Fatalf("expected queued job to expose planned deepseek model, got %+v", created.Job)
	}

	job := waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusCompleted)
	if job.Provider != ai.ProviderDeepSeek {
		t.Fatalf("expected deepseek provider, got %+v", job)
	}
	if job.Model != "deepseek-v4-flash" {
		t.Fatalf("expected stored model deepseek-v4-flash, got %+v", job)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/ai/jobs/"+created.Job.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get generation job status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &payload)
	if payload.Job.Model != "deepseek-v4-flash" {
		t.Fatalf("expected payload model deepseek-v4-flash, got %+v", payload.Job)
	}
}

func TestScenarioGenerationJobReportsTimeoutWithoutMockFallback(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte(openAICompatibleScenarioResponse("slow job scenario")))
	}))
	defer provider.Close()

	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{
		Provider: ai.ProviderOpenAICompatible,
		BaseURL:  provider.URL,
		APIKey:   "test-key",
		Model:    "fake-model",
		Timeout:  10 * time.Millisecond,
	}))
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)

	job := waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusFailed)
	if job.Provider == ai.ProviderMock || job.FallbackUsed {
		t.Fatalf("timeout should not be converted to mock fallback, got %+v", job)
	}
	if !strings.Contains(job.ErrorMessage, "模型响应超时") {
		t.Fatalf("expected actionable timeout message, got %+v", job)
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/ai/jobs/"+created.Job.ID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get generation job status=%d message=%s", status, env.Message)
	}
	var payload struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &payload)
	if !strings.Contains(payload.Job.ErrorMessage, "模型响应超时") {
		t.Fatalf("expected actionable timeout message, got %+v", payload.Job)
	}
}

func TestScenarioGenerationJobReportsValidationFailureWithoutMockFallback(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"not json"}}]}`))
	}))
	defer provider.Close()

	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{
		Provider: ai.ProviderOpenAICompatible,
		BaseURL:  provider.URL,
		APIKey:   "test-key",
		Model:    "fake-model",
		Timeout:  time.Second,
	}))
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)

	job := waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusFailed)
	if job.Provider == ai.ProviderMock || job.FallbackUsed {
		t.Fatalf("validation failure should not be converted to mock fallback, got %+v", job)
	}
	if job.Provider != ai.ProviderOpenAICompatible || job.Model != "fake-model" {
		t.Fatalf("validation failure should keep provider/model metadata, got %+v", job)
	}
	if job.Stage != "validating_output" {
		t.Fatalf("validation failure should keep failing stage, got %+v", job)
	}
	if !strings.Contains(job.ErrorMessage, "结构") && !strings.Contains(job.ErrorMessage, "校验") {
		t.Fatalf("expected actionable validation message, got %+v", job)
	}
}

func TestCanceledScenarioGenerationJobDoesNotUpdateRouterTelemetry(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")
	initialScenarioCount := len(dataStore.ListScenarios("", "", ""))

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/generate/jobs", token, map[string]interface{}{
		"domain":        "database",
		"difficulty":    "L2",
		"scenario_type": "troubleshooting",
		"tags":          []string{"demo", "cancel"},
	})
	if status != http.StatusOK {
		t.Fatalf("create generation job status=%d message=%s", status, env.Message)
	}
	var created struct {
		Job domain.AIJob `json:"job"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/ai/jobs/"+created.Job.ID+"/cancel", token, nil)
	if status != http.StatusOK {
		t.Fatalf("cancel generation job status=%d message=%s", status, env.Message)
	}
	waitForAIJobStatus(t, dataStore, created.Job.ID, domain.AIJobStatusCanceled)

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("system status=%d message=%s", status, env.Message)
	}
	var payload struct {
		AI struct {
			Telemetry ai.RouterTelemetry `json:"telemetry"`
		} `json:"ai"`
		Counts struct {
			Scenarios int `json:"scenarios"`
		} `json:"counts"`
	}
	mustDecodeData(t, env, &payload)
	if payload.AI.Telemetry.TotalCalls != 0 {
		t.Fatalf("expected canceled job not to increase telemetry, got %+v", payload.AI.Telemetry)
	}
	if payload.AI.Telemetry.TaskCalls[ai.RouterTaskScenarioGenerate] != 0 {
		t.Fatalf("expected canceled job not to increase scenario_generate count, got %+v", payload.AI.Telemetry.TaskCalls)
	}
	if payload.Counts.Scenarios != initialScenarioCount {
		t.Fatalf("unexpected scenario count after cancel: %d", payload.Counts.Scenarios)
	}
}

func waitForAIJobStatus(t *testing.T, dataStore store.Store, jobID string, expected string) *domain.AIJob {
	t.Helper()
	var last *domain.AIJob
	for i := 0; i < 40; i++ {
		current, ok := dataStore.GetAIJob(jobID)
		if ok {
			last = current
		}
		if ok && current.Status == expected {
			return current
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("ai job %s did not reach %s, last=%+v", jobID, expected, last)
	return nil
}

func openAICompatibleScenarioResponse(title string) string {
	content := `{"title":` + quoteJSON(title) + `,"description":"用于验证情景题生成任务模型记录的题目。","domain":"database","difficulty":"L2","scenario_type":"troubleshooting","tags":["数据库","生成题"],"content":{"root_cause":"数据库连接池耗尽导致请求排队。","root_cause_keywords":["连接池","排队"],"key_evidence":["活跃连接接近上限"],"standard_procedure":["查看接口耗时","检查连接池指标"],"architecture_diagram":"","architecture_diagram_spec":{"direction":"TD","nodes":[{"id":"API","label":"API"},{"id":"Pool","label":"DB Pool"},{"id":"DB","label":"数据库(主库)"}],"edges":[{"from":"API","to":"Pool"},{"from":"Pool","to":"DB"}]},"reference_links":["连接池监控"],"reveal_strategy":{"surface_clues":[{"clue_id":"c1","trigger_keywords":["连接池"],"content":"活跃连接接近上限。","is_distractor":false}],"deep_clues":[{"clue_id":"c2","trigger_keywords":["排队"],"content":"等待队列持续增长。","is_distractor":false}],"distractors":[{"clue_id":"d1","trigger_keywords":["网络"],"content":"网络延迟正常。","is_distractor":true}]}}}`
	return `{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(content) + `}}]}`
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestScenarioMessageSSE(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create scenario session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	body, _ := json.Marshal(map[string]string{"content": "请给出当前 CPU 和慢查询指标。"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/sessions/"+sessionData.SessionID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if !strings.Contains(raw, "event: stage") || !strings.Contains(raw, "event: finish") {
		t.Fatalf("expected stage and finish events, got %s", raw)
	}
	if strings.Contains(raw, "event: delta") {
		t.Fatalf("scenario message SSE should not expose structured JSON delta, got %s", raw)
	}
}

func TestInterviewSubmitSSE(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	body, _ := json.Marshal(map[string]string{"content": "先看慢查询日志，再用 EXPLAIN 验证索引覆盖，最后灰度补索引。", "type": "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/sessions/"+sessionData.SessionID+"/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if !strings.Contains(raw, "event: stage") || !strings.Contains(raw, "event: delta") || !strings.Contains(raw, "event: finish") {
		t.Fatalf("expected stage, delta and finish events, got %s", raw)
	}
	if !strings.Contains(raw, "session_status") {
		t.Fatalf("expected finish payload, got %s", raw)
	}
	if !strings.Contains(raw, `"displayable":true`) {
		t.Fatalf("expected displayable interview feedback delta, got %s", raw)
	}
	for _, block := range strings.Split(raw, "\n\n") {
		if !strings.Contains(block, "event: delta") || !strings.Contains(block, `"displayable":true`) {
			continue
		}
		for _, field := range []string{`"highlights"`, `"deficiencies"`, `"follow_up_question"`, `"final_report"`} {
			if strings.Contains(block, field) {
				t.Fatalf("displayable interview delta should not expose json field %s, got %s", field, block)
			}
		}
	}
}

func TestInterviewSubmitSSEStreamsDisplayBeforeSaving(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"highlights\\\":[\\\"定位路径清晰\\\"],\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\\\"deficiencies\\\":[\\\"回滚验证不足\\\"],\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\\\"follow_up_question\\\":\\\"请补充灰度方案\\\",\\\"final_report\\\":\\\"整体达到要求\\\"}\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer provider.Close()
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{
		Provider:         ai.ProviderOpenAICompatible,
		BaseURL:          provider.URL,
		APIKey:           "test-key",
		Model:            "fake-model",
		Timeout:          time.Second,
		StreamEnabled:    true,
		StreamConfigured: true,
	}))
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	body, _ := json.Marshal(map[string]string{"content": "首先定位慢查询日志，然后用 EXPLAIN 验证索引覆盖，最后灰度补索引并准备回滚。", "type": "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/sessions/"+sessionData.SessionID+"/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	savingIndex := strings.Index(raw, `"step":"saving"`)
	if savingIndex < 0 {
		t.Fatalf("expected saving stage, got %s", raw)
	}
	displayIndex := strings.Index(raw, `"displayable":true`)
	if displayIndex < 0 || displayIndex > savingIndex {
		t.Fatalf("expected visible delta before saving stage, got %s", raw)
	}
	for _, field := range []string{`"highlights"`, `"deficiencies"`, `"follow_up_question"`, `"final_report"`} {
		for _, block := range strings.Split(raw[:savingIndex], "\n\n") {
			if strings.Contains(block, `"displayable":true`) && strings.Contains(block, field) {
				t.Fatalf("visible streaming delta should not expose json field %s, got %s", field, block)
			}
		}
	}
}

func TestInterviewSubmitSSEKeepsChineseDisplayDeltasUTF8(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"highlights":["提到了限流和降级保护接口，体现了高可用意识",`,
			`"考虑了灰度修复，有风险控制思路",`,
			`"关注P95、错误率和慢查询等关键指标"],`,
			`"deficiencies":["未详细说明定位MySQL慢查询的具体路径和命令（如慢查询日志、EXPLAIN、performance_schema等）"],`,
			`"follow_up_question":"请补充关键命令和回滚验证。",`,
			`"final_report":"整体表达清晰，但需要补充数据库定位细节。"}`,
		}
		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + mustJSON(map[string]interface{}{
				"choices": []map[string]interface{}{{"delta": map[string]string{"content": chunk}}},
			}) + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer provider.Close()
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServer(dataStore, auth.NewManager("test-secret", time.Hour), nil, ai.NewRouter(ai.Config{
		Provider:         ai.ProviderOpenAICompatible,
		BaseURL:          provider.URL,
		APIKey:           "test-key",
		Model:            "fake-model",
		Timeout:          time.Second,
		StreamEnabled:    true,
		StreamConfigured: true,
	}))
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	body, _ := json.Marshal(map[string]string{"content": "## 关键命令\n- 查看慢查询日志\n\n## 处理与回滚\n- 灰度发布并观察 P95。", "type": "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/sessions/"+sessionData.SessionID+"/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if strings.Contains(raw, "�") || strings.Contains(raw, "??") || !utf8.ValidString(raw) {
		t.Fatalf("expected valid readable chinese SSE without replacement chars, got %s", raw)
	}
	for _, block := range strings.Split(raw, "\n\n") {
		if !strings.Contains(block, `"displayable":true`) {
			continue
		}
		if !utf8.ValidString(block) || strings.Contains(block, "�") || strings.Contains(block, "??") {
			t.Fatalf("invalid display delta block: %s", block)
		}
	}
	if !strings.Contains(raw, "考虑了灰度修复") || !strings.Contains(raw, "关注P95") {
		t.Fatalf("expected later highlight items to stream as separate readable lines, got %s", raw)
	}
}

func TestCommunityPostCreateSSE(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	body, _ := json.Marshal(map[string]interface{}{
		"title":       "缓存命中率异常下降",
		"raw_content": "发布后缓存 key 规则变化，数据库读请求升高。",
		"domain":      "database",
		"tags":        []string{"缓存", "变更"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/community/posts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if !strings.Contains(raw, "event: stage") || !strings.Contains(raw, "event: finish") {
		t.Fatalf("expected stage and finish events, got %s", raw)
	}
	if !strings.Contains(raw, "pending_review") {
		t.Fatalf("expected created community post, got %s", raw)
	}
}

func TestCommunityPostCreateSSEStreamsSensitiveProgressWithoutLeakingSecrets(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	secret := "sk-community-sse-secret"
	password := "secret123"
	body, _ := json.Marshal(map[string]interface{}{
		"title":       "cache hit ratio incident",
		"raw_content": "release changed cache key and exposed password=" + password + " with api_key=" + secret,
		"domain":      "database",
		"tags":        []string{"cache", "change"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/community/posts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sse status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	for _, step := range []string{"received", "llm", "schema_validated", "rule_sensitive_check", "model_sensitive_check", "sanitized", "saving", "completed"} {
		if !strings.Contains(raw, `"step":"`+step+`"`) {
			t.Fatalf("expected SSE stage %s, got %s", step, raw)
		}
	}
	if strings.Contains(raw, secret) || strings.Contains(raw, password) || strings.Contains(raw, "{re") {
		t.Fatalf("community SSE leaked raw sensitive data or structured json marker: %s", raw)
	}
	if !utf8.ValidString(raw) || strings.Contains(raw, "锟") || strings.Contains(raw, "�") {
		t.Fatalf("community SSE contains invalid or replacement text: %s", raw)
	}
	if !strings.Contains(raw, "pending_review") {
		t.Fatalf("expected created community post, got %s", raw)
	}
}

func TestScenarioContextCompressionAfterTenTurns(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create scenario session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	for i := 0; i < 11; i++ {
		status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionData.SessionID+"/messages", token, map[string]string{
			"content": "请从日志、指标或变更角度继续给出线索",
		})
		if status != http.StatusOK {
			t.Fatalf("message %d status=%d message=%s", i+1, status, env.Message)
		}
	}
	session, ok := dataStore.GetScenarioSession(sessionData.SessionID)
	if !ok {
		t.Fatal("missing scenario session")
	}
	if session.ConversationSummary == "" {
		t.Fatal("expected conversation summary after more than ten turns")
	}
	if !strings.Contains(session.ConversationSummary, "已压缩前") {
		t.Fatalf("unexpected conversation summary: %s", session.ConversationSummary)
	}
	if len([]rune(session.ConversationSummary)) > 1800 {
		t.Fatalf("conversation summary should stay within existing 1800-char cap: %d", len([]rune(session.ConversationSummary)))
	}
}

func TestScenarioQuitBlocksFurtherMessages(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create scenario session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionData.SessionID+"/quit", token, nil)
	if status != http.StatusOK {
		t.Fatalf("quit status=%d message=%s", status, env.Message)
	}
	var quitData struct {
		Status  string                 `json:"status"`
		Session domain.ScenarioSession `json:"session"`
	}
	mustDecodeData(t, env, &quitData)
	if quitData.Status != "abandoned" || quitData.Session.Status != "abandoned" {
		t.Fatalf("expected abandoned quit response, got %+v", quitData)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionData.SessionID+"/messages", token, map[string]string{
		"content": "继续查看慢查询日志",
	})
	if status != http.StatusBadRequest || env.Message != "session is not active" {
		t.Fatalf("expected inactive session error, status=%d message=%s", status, env.Message)
	}
}

func TestScenarioIdleTimeoutAbandonsSession(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	question := dataStore.ListScenarios("database", "", "")[0]

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/"+question.ID+"/sessions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("create scenario session status=%d message=%s", status, env.Message)
	}
	var sessionData struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &sessionData)

	session, ok := dataStore.GetScenarioSession(sessionData.SessionID)
	if !ok {
		t.Fatal("missing scenario session")
	}
	session.LastActiveAt = time.Now().Add(-31 * time.Minute)
	dataStore.SaveScenarioSession(session)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/scenarios/sessions/"+sessionData.SessionID+"/messages", token, map[string]string{
		"content": "继续查看慢查询日志",
	})
	if status != http.StatusBadRequest || env.Message != "session is abandoned" {
		t.Fatalf("expected abandoned session error, status=%d message=%s", status, env.Message)
	}
	updated, ok := dataStore.GetScenarioSession(sessionData.SessionID)
	if !ok {
		t.Fatal("missing updated scenario session")
	}
	if updated.Status != "abandoned" || updated.EndedAt == nil {
		t.Fatalf("expected abandoned session with ended_at, got %+v", updated)
	}
}
