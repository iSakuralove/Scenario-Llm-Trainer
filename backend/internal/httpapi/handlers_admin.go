package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	if !hasAnyRole(user, domain.RoleAdmin) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	parts := split(suffix)
	if len(parts) >= 1 && parts[0] == "interview-bank" {
		s.handleAdminInterviewBank(w, r, user, parts[1:])
		return
	}
	if len(parts) == 1 && parts[0] == "users" && r.Method == http.MethodGet {
		writeOK(w, map[string]interface{}{"list": s.store.ListUsers()})
		return
	}
	if len(parts) == 3 && parts[0] == "users" && parts[2] == "role" && r.Method == http.MethodPut {
		var req struct {
			Role string `json:"role"`
		}
		if !decode(w, r, &req) {
			return
		}
		role := strings.TrimSpace(req.Role)
		if !domain.ValidRole(role) {
			writeError(w, http.StatusBadRequest, "invalid role")
			return
		}
		updated, err := s.store.UpdateUserRole(parts[1], role)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.audit(r, user, "admin.user_role_update", "user", updated.ID, map[string]string{"role": updated.Role})
		writeOK(w, updated)
		return
	}
	if len(parts) == 1 && parts[0] == "prompts" && r.Method == http.MethodGet {
		writeOK(w, map[string]interface{}{"list": s.store.ListPromptTemplates()})
		return
	}
	if len(parts) == 2 && parts[0] == "prompts" && r.Method == http.MethodPut {
		var req struct {
			Content      string `json:"content"`
			RenderEngine string `json:"render_engine"`
			ResetDefault bool   `json:"reset_default"`
		}
		if !decode(w, r, &req) {
			return
		}
		existing, ok := s.store.GetPromptTemplate(parts[1])
		if !ok {
			writeError(w, http.StatusNotFound, "prompt template not found")
			return
		}
		content := req.Content
		if req.ResetDefault {
			content = existing.Default
		}
		if !validPromptContent(content) {
			writeError(w, http.StatusBadRequest, "prompt content is too short")
			return
		}
		if err := ai.ValidateManagedPromptContent(existing.Name, content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		renderEngine := firstNonEmpty(strings.TrimSpace(req.RenderEngine), existing.RenderEngine, ai.PromptRenderEngineGoTemplate)
		if req.ResetDefault {
			renderEngine = ai.PromptRenderEngineGoTemplate
			ai.ClearPromptOverride(existing.Name)
		} else if err := ai.SetPromptOverride(existing.Name, renderEngine, content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		existing.Content = content
		existing.RenderEngine = renderEngine
		existing.UpdatedBy = user.ID
		updated, err := s.store.SavePromptTemplate(*existing)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, user, "admin.prompt_update", "prompt_template", updated.Name, map[string]string{"reset_default": strconv.FormatBool(req.ResetDefault)})
		writeOK(w, updated)
		return
	}
	if len(parts) == 1 && parts[0] == "ai-config" && r.Method == http.MethodGet {
		writeOK(w, s.store.GetAIConfig())
		return
	}
	if len(parts) == 1 && parts[0] == "ai-config" && r.Method == http.MethodPut {
		var req domain.AIConfig
		if !decode(w, r, &req) {
			return
		}
		req.UpdatedBy = user.ID
		updated, err := s.store.SaveAIConfig(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.setLLMRouter(ai.NewRouter(aiConfigToRouterConfig(updated)))
		s.audit(r, user, "admin.ai_config_update", "ai_config", "default", map[string]string{"provider": updated.Provider, "model": updated.Model})
		writeOK(w, updated)
		return
	}
	if len(parts) == 1 && parts[0] == "audit-events" && r.Method == http.MethodGet {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 30
		}
		writeOK(w, map[string]interface{}{"list": s.store.ListAuditEvents(limit)})
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}
func (s *Server) systemStatus() map[string]interface{} {
	storeStatus := "ok"
	storeDetail := "in-memory demo store"
	storeMode := "memory"
	storePersistent := false
	storeWarning := "临时内存模式：生成题目、AI 任务和会话会在后端进程退出后丢失。"
	if pingable, ok := s.store.(interface{ Ping(context.Context) error }); ok {
		storeMode = "postgres"
		storePersistent = true
		storeWarning = ""
		storeDetail = "PostgreSQL persistence"
		if err := pingable.Ping(context.Background()); err != nil {
			storeStatus = "degraded"
			storeDetail = "PostgreSQL ping failed"
			storeWarning = "PostgreSQL ping failed; generated data may not be durable until database connectivity is restored."
		}
	}
	scenarios := s.store.ListScenarios("", "", "")
	aiJobsCount := s.store.CountAIJobs()
	communityPosts := s.store.ListCommunityPosts()
	users := s.store.ListUsers()
	activeScenarios := 0
	seedScenarios := 0
	generatedScenarios := 0
	for _, scenario := range scenarios {
		if scenario.Status == "active" {
			activeScenarios++
		}
		if scenario.Source == "seed" {
			seedScenarios++
		}
		if scenario.Source == "llm_generated" {
			generatedScenarios++
		}
	}
	pendingUGC := 0
	for _, post := range communityPosts {
		if post.Status == "pending_review" || post.Status == "instructor_approved" {
			pendingUGC++
		}
	}
	aiInfo := s.llmRouter().Info()
	aiStatus := "ok"
	if aiInfo.Fallback {
		aiStatus = "fallback"
	}
	redisStatus := "disabled"
	if s.limiter.Enabled() {
		redisStatus = "ok"
	}
	auditEvents := s.store.ListAuditEvents(50)
	sensitiveStatus := sensitiveDetectionStatus(aiInfo, auditEvents)
	interviewBankSummary := s.store.InterviewKnowledgeSummary()
	return map[string]interface{}{
		"generated_at": time.Now(),
		"services": []map[string]interface{}{
			{"name": "API", "status": "ok", "detail": "HTTP router is serving /healthz and /api/v1"},
			{"name": "Database", "status": storeStatus, "detail": storeDetail},
			{"name": "Redis", "status": redisStatus, "detail": redisDetail(redisStatus)},
			{"name": "AI Provider", "status": aiStatus, "detail": aiProviderStatusDetail(aiInfo)},
			{"name": "Sensitive Detection", "status": sensitiveStatus["status"], "detail": sensitiveStatus["detail"]},
			{"name": "Seed Data", "status": seedDataStatus(seedScenarios), "detail": fmt.Sprintf("%d seed scenarios, %d active scenarios", seedScenarios, activeScenarios)},
			{"name": "Interview Bank", "status": interviewBankStatus(interviewBankSummary), "detail": fmt.Sprintf("%d atoms, %d failed vectors", interviewBankSummary.TotalAtoms, interviewBankSummary.VectorFailedAtoms)},
		},
		"ai":        aiInfo,
		"ai_config": s.store.GetAIConfig(),
		"store": map[string]interface{}{
			"mode":       storeMode,
			"persistent": storePersistent,
			"warning":    storeWarning,
		},
		"prompt_templates":  promptTemplateStatusList(s.store.ListPromptTemplates()),
		"schema_validators": ai.SchemaValidatorStatus(),
		"rate_limit": map[string]interface{}{
			"enabled": s.limiter.Enabled(),
			"detail":  redisDetail(redisStatus),
		},
		"sensitive_detection": sensitiveStatus,
		"audit_summary":       auditSummary(s.store.ListAuditEvents(20)),
		"agent_summary":       agentSummary(auditEvents),
		"recent_ai_errors":    recentAIErrors(auditEvents),
		"interview_bank":      interviewBankSummary,
		"counts": map[string]int{
			"users":                     len(users),
			"scenarios":                 len(scenarios),
			"active_scenarios":          activeScenarios,
			"generated_scenarios":       generatedScenarios,
			"ai_jobs":                   aiJobsCount,
			"community_posts":           communityPostCount(communityPosts),
			"pending_ugc":               pendingUGC,
			"interview_knowledge_atoms": interviewBankSummary.TotalAtoms,
		},
		"demo_accounts": []map[string]string{
			{"role": "student", "username": "demo", "purpose": "排查、面试、发布 UGC"},
			{"role": "instructor", "username": "instructor", "purpose": "讲师初审 UGC"},
			{"role": "admin", "username": "admin", "purpose": "终审发布、系统检查"},
		},
		"runbook": []map[string]string{
			{"title": "演示验收", "command": ".\\scripts\\demo-acceptance.ps1"},
			{"title": "跳过真实生成", "command": ".\\scripts\\demo-acceptance.ps1 -SkipScenarioGenerate"},
			{"title": "重置演示数据", "command": ".\\scripts\\reset-demo-data.ps1"},
		},
	}
}

type promptTemplateStatus struct {
	Name          string    `json:"name"`
	Task          string    `json:"task"`
	RenderEngine  string    `json:"render_engine"`
	UpdatedBy     string    `json:"updated_by,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
	IsModified    bool      `json:"is_modified"`
	Validator     string    `json:"validator"`
	Summary       string    `json:"summary"`
	ContentLength int       `json:"content_length"`
	DefaultLength int       `json:"default_length"`
}

func promptTemplateStatusList(templates []domain.PromptTemplate) []promptTemplateStatus {
	items := make([]promptTemplateStatus, 0, len(templates))
	for _, template := range templates {
		items = append(items, promptTemplateStatusFromTemplate(template))
	}
	return items
}
func promptTemplateStatusFromTemplate(template domain.PromptTemplate) promptTemplateStatus {
	contentLength := len([]rune(template.Content))
	defaultLength := len([]rune(template.Default))
	state := "default"
	if template.IsModified {
		state = "modified"
	}
	return promptTemplateStatus{
		Name:          template.Name,
		Task:          template.Task,
		RenderEngine:  firstNonEmpty(strings.TrimSpace(template.RenderEngine), ai.PromptRenderEngineGoTemplate),
		UpdatedBy:     template.UpdatedBy,
		UpdatedAt:     template.UpdatedAt,
		IsModified:    template.IsModified,
		Validator:     template.Validator,
		Summary:       fmt.Sprintf("%s prompt template, %d characters", state, contentLength),
		ContentLength: contentLength,
		DefaultLength: defaultLength,
	}
}
func validPromptContent(content string) bool {
	return len([]rune(strings.TrimSpace(content))) >= 8
}
func aiConfigToRouterConfig(config domain.AIConfig) ai.Config {
	cfg := ai.ConfigFromEnv()
	if provider := strings.TrimSpace(config.Provider); provider != "" {
		cfg.Provider = provider
	}
	if model := strings.TrimSpace(config.Model); model != "" {
		cfg.Model = model
	}
	if baseURL := strings.TrimSpace(config.BaseURL); baseURL != "" {
		cfg.BaseURL = baseURL
	}
	cfg.Temperature = config.Temperature
	cfg.TopP = config.TopP
	cfg.TopK = config.TopK
	cfg.MaxTokens = config.MaxTokens
	cfg.StreamEnabled = config.StreamEnabled
	cfg.StreamConfigured = true
	return cfg
}
func auditSummary(events []domain.AuditEvent) map[string]interface{} {
	counts := map[string]int{}
	for _, event := range events {
		counts[event.Action]++
	}
	return map[string]interface{}{"total_recent": len(events), "by_action": counts, "latest": events}
}
func agentSummary(events []domain.AuditEvent) map[string]interface{} {
	total := 0
	failed := 0
	safetyRewritten := 0
	flagged := 0
	latestAgent := ""
	latestRunAt := ""
	perAgentCounts := map[string]map[string]interface{}{}
	for _, event := range events {
		if !strings.HasPrefix(event.Action, "agent.") {
			continue
		}
		total++
		if latestAgent == "" {
			latestAgent = event.Metadata["agent"]
			latestRunAt = event.CreatedAt.Format(time.RFC3339)
		}
		if event.Metadata["status"] == "failed" {
			failed++
		}
		if event.Metadata["safety_rewritten"] == "true" {
			safetyRewritten++
		}
		if event.Metadata["flagged"] == "true" {
			flagged++
		}
		agentName := firstNonEmpty(event.Metadata["agent"], "unknown_agent")
		row, ok := perAgentCounts[agentName]
		if !ok {
			row = map[string]interface{}{
				"agent":                   agentName,
				"total_recent":            0,
				"failed_recent":           0,
				"safety_rewritten_recent": 0,
				"latest_run_at":           "",
				"latest_status":           "",
			}
			perAgentCounts[agentName] = row
		}
		row["total_recent"] = row["total_recent"].(int) + 1
		if row["latest_run_at"] == "" {
			row["latest_run_at"] = event.CreatedAt.Format(time.RFC3339)
			row["latest_status"] = event.Metadata["status"]
		}
		if event.Metadata["status"] == "failed" {
			row["failed_recent"] = row["failed_recent"].(int) + 1
		}
		if event.Metadata["safety_rewritten"] == "true" {
			row["safety_rewritten_recent"] = row["safety_rewritten_recent"].(int) + 1
		}
	}
	if latestAgent == "" {
		latestAgent = "diagnostic_agent"
	}
	perAgent := make([]map[string]interface{}, 0, len(perAgentCounts))
	for _, row := range perAgentCounts {
		perAgent = append(perAgent, row)
	}
	sort.Slice(perAgent, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, firstNonEmpty(perAgent[i]["latest_run_at"].(string), time.RFC3339))
		right, _ := time.Parse(time.RFC3339, firstNonEmpty(perAgent[j]["latest_run_at"].(string), time.RFC3339))
		return left.After(right)
	})
	return map[string]interface{}{
		"total_recent":            total,
		"latest_agent":            latestAgent,
		"latest_run_at":           latestRunAt,
		"failed_recent":           failed,
		"safety_rewritten_recent": safetyRewritten,
		"flagged_recent":          flagged,
		"per_agent":               perAgent,
	}
}
func recentAIErrors(events []domain.AuditEvent) []map[string]string {
	out := []map[string]string{}
	for _, event := range events {
		if event.Action != "ai.error" && event.Action != "rate_limit.hit" && event.Action != "ai.safety_check_fallback" {
			continue
		}
		out = append(out, map[string]string{
			"action":      event.Action,
			"resource_id": event.ResourceID,
			"created_at":  event.CreatedAt.Format(time.RFC3339),
		})
		if len(out) == 5 {
			break
		}
	}
	return out
}
func sensitiveDetectionStatus(info ai.ProviderInfo, events []domain.AuditEvent) map[string]interface{} {
	fallbacks := 0
	for _, event := range events {
		if event.Action == "ai.safety_check_fallback" {
			fallbacks++
		}
	}
	status := "ok"
	detail := "规则检测和模型辅助检测均已启用"
	if info.Provider == ai.ProviderMock || info.Fallback {
		status = "fallback"
		detail = "模型检测使用 mock 或 fallback，规则检测保持兜底"
	}
	if fallbacks > 0 {
		status = "degraded"
		detail = "最近存在模型检测回退，规则检测已接管"
	}
	return map[string]interface{}{
		"status":          status,
		"provider":        info.Provider,
		"model":           info.Model,
		"fallback_count":  fallbacks,
		"fallback_used":   fallbacks > 0 || info.Fallback,
		"rule_enabled":    true,
		"model_enabled":   true,
		"schema":          ai.SchemaSensitiveCheck,
		"detail":          detail,
		"checked_actions": []string{"community.create", "community.draft_update", "community.submit", "scenario.fork", "ai.safety.check"},
	}
}
func redisDetail(status string) string {
	if status == "ok" {
		return "Redis limiter enabled"
	}
	return "Rate limiting is using noop fallback"
}
func aiProviderStatusDetail(info ai.ProviderInfo) string {
	if info.Fallback {
		if strings.TrimSpace(info.ConfiguredProvider) != "" {
			return fmt.Sprintf("fallback active, configured provider %s is unavailable", info.ConfiguredProvider)
		}
		return "mock fallback active"
	}
	if strings.TrimSpace(info.BaseURL) != "" {
		return fmt.Sprintf("%s %s via %s", info.Provider, info.Model, info.BaseURL)
	}
	return fmt.Sprintf("%s %s", info.Provider, info.Model)
}
func seedDataStatus(seedScenarios int) string {
	if seedScenarios >= 3 {
		return "ok"
	}
	if seedScenarios > 0 {
		return "degraded"
	}
	return "missing"
}

func interviewBankStatus(summary domain.InterviewKnowledgeSummary) string {
	if summary.VectorFailedAtoms > 0 {
		return "degraded"
	}
	if summary.TotalAtoms == 0 {
		return "empty"
	}
	return "ok"
}
