package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/agentclient"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/ratelimit"
	"situational-teaching/backend/internal/store"
)

type Server struct {
	store                  store.Store
	auth                   *auth.Manager
	limiter                ratelimit.Limiter
	allowAnonPasswordReset bool
	smtpHost               string
	smtpPort               string
	smtpUsername           string
	smtpPassword           string
	smtpFrom               string
	appPublicURL           string
	llmMu                  sync.RWMutex
	llm                    *ai.Router
	stt                    STTProvider
	embedding              ai.EmbeddingClient
	scenarioAgent          scenarioAgentClient
	assets                 AssetStorage
	jobMu                  sync.Mutex
	jobStop                map[string]context.CancelFunc
	// openingStemCache 缓存被 LLM 改写过的开场题干，按题目 ID 索引。
	// 既避免每次「开始面试」都同步等一次模型，也保证同一道题的题干稳定不变。
	openingStemMu    sync.RWMutex
	openingStemCache map[string]string
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

type agentAuditPayload struct {
	Agent           string
	Action          string
	ResourceType    string
	ResourceID      string
	Status          string
	ToolCount       int
	FallbackUsed    bool
	SafetyRewritten bool
	Flagged         bool
	ErrorSummary    string
}

type envelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func NewServer(dataStore store.Store, authManager *auth.Manager, limiter ratelimit.Limiter, routers ...*ai.Router) *Server {
	if limiter == nil {
		limiter = ratelimit.NewNoopLimiter()
	}
	llmRouter := ai.NewRouter(ai.Config{Provider: ai.ProviderMock})
	if len(routers) > 0 && routers[0] != nil {
		llmRouter = routers[0]
	}
	assetStorage := NewAssetStorageFromEnv()
	server := &Server{
		store:                  dataStore,
		auth:                   authManager,
		limiter:                limiter,
		allowAnonPasswordReset: anonymousPasswordResetEnabled(dataStore),
		smtpHost:               strings.TrimSpace(os.Getenv("SMTP_HOST")),
		smtpPort:               envOrDefault("SMTP_PORT", "587"),
		smtpUsername:           strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		smtpPassword:           os.Getenv("SMTP_PASSWORD"),
		smtpFrom:               strings.TrimSpace(os.Getenv("SMTP_FROM")),
		appPublicURL:           strings.TrimRight(strings.TrimSpace(os.Getenv("APP_PUBLIC_URL")), "/"),
		llm:                    llmRouter,
		assets:                 assetStorage,
		stt:                    NewSTTProviderFromEnv(assetStorage),
		embedding:              ai.NewEmbeddingClientFromEnv(),
		scenarioAgent: agentclient.New(agentclient.Config{
			BaseURL: envOrDefault("AGENT_BASE_URL", "http://127.0.0.1:8091"),
			Timeout: 20 * time.Second,
		}),
		jobStop: map[string]context.CancelFunc{},
	}
	server.applyPromptOverrides()
	return server
}

func NewServerForTests(dataStore store.Store, authManager *auth.Manager, scenarioAgents ...scenarioAgentClient) *Server {
	var limiter ratelimit.Limiter = ratelimit.NewNoopLimiter()
	server := NewServer(dataStore, authManager, limiter, ai.NewRouter(ai.Config{Provider: ai.ProviderMock}))
	server.stt = MockSTTProvider{}
	server.embedding = nil
	server.scenarioAgent = deterministicScenarioAgentClient{}
	if len(scenarioAgents) > 0 && scenarioAgents[0] != nil {
		server.scenarioAgent = scenarioAgents[0]
	}
	return server
}

func (s *Server) llmRouter() *ai.Router {
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	if s.llm == nil {
		return ai.NewRouter(ai.Config{Provider: ai.ProviderMock})
	}
	return s.llm
}

func (s *Server) setLLMRouter(router *ai.Router) {
	if router == nil {
		router = ai.NewRouter(ai.Config{Provider: ai.ProviderMock})
	}
	s.llmMu.Lock()
	defer s.llmMu.Unlock()
	s.llm = router
}

func (s *Server) applyPromptOverrides() {
	for _, item := range s.store.ListPromptTemplates() {
		if !item.IsModified {
			continue
		}
		if err := ai.SetPromptOverride(item.Name, item.RenderEngine, item.Content); err != nil {
			s.store.RecordAuditEvent(domain.AuditEvent{
				Action:       "ai.prompt_override_error",
				ResourceType: "prompt_template",
				ResourceID:   item.Name,
				Metadata:     map[string]string{"error": err.Error()},
			})
		}
	}
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !setCORS(w, r) {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/healthz" {
			writeOK(w, map[string]string{"status": "ok"})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case path == "/system/ai":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeOK(w, publicAIStatus(s.llmRouter().Info()))
		case path == "/system/status":
			s.withUser(w, r, func(user *domain.User) {
				if !hasAnyRole(user, domain.RoleAdmin) {
					writeError(w, http.StatusForbidden, "admin role required")
					return
				}
				if r.Method != http.MethodGet {
					writeError(w, http.StatusMethodNotAllowed, "method not allowed")
					return
				}
				writeOK(w, s.systemStatus())
			})
		case strings.HasPrefix(path, "/ai"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleAI(w, r, user, strings.TrimPrefix(path, "/ai"))
			})
		case strings.HasPrefix(path, "/assets"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleAssets(w, r, user, strings.TrimPrefix(path, "/assets"))
			})
		case strings.HasPrefix(path, "/auth/"):
			if !s.allow(w, r, "ip:"+clientIP(r), 60) {
				return
			}
			s.handleAuth(w, r, strings.TrimPrefix(path, "/auth/"))
		case strings.HasPrefix(path, "/users/me"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleMe(w, r, user, strings.TrimPrefix(path, "/users/me"))
			})
		case strings.HasPrefix(path, "/scenarios"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleScenarios(w, r, user, strings.TrimPrefix(path, "/scenarios"))
			})
		case strings.HasPrefix(path, "/interviews"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleInterviews(w, r, user, strings.TrimPrefix(path, "/interviews"))
			})
		case strings.HasPrefix(path, "/community"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleCommunity(w, r, user, strings.TrimPrefix(path, "/community"))
			})
		case strings.HasPrefix(path, "/admin"):
			s.withUser(w, r, func(user *domain.User) {
				s.handleAdmin(w, r, user, strings.TrimPrefix(path, "/admin"))
			})
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
	})
}

func (s *Server) auditAgentRun(request *http.Request, user *domain.User, payload agentAuditPayload) {
	action := strings.TrimSpace(payload.Action)
	if action == "" {
		return
	}
	status := firstNonEmpty(strings.TrimSpace(payload.Status), "completed")
	agentName := firstNonEmpty(strings.TrimSpace(payload.Agent), "agent")
	metadata := map[string]string{
		"agent":            agentName,
		"status":           status,
		"tool_count":       strconv.Itoa(payload.ToolCount),
		"fallback_used":    strconv.FormatBool(payload.FallbackUsed),
		"safety_rewritten": strconv.FormatBool(payload.SafetyRewritten),
		"flagged":          strconv.FormatBool(payload.Flagged),
	}
	if strings.TrimSpace(payload.ErrorSummary) != "" {
		metadata["error"] = payload.ErrorSummary
	}
	if request != nil {
		s.audit(request, user, action, payload.ResourceType, payload.ResourceID, metadata)
		return
	}
	actorID := ""
	if user != nil {
		actorID = user.ID
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      actorID,
		Action:       action,
		ResourceType: payload.ResourceType,
		ResourceID:   payload.ResourceID,
		Metadata:     metadata,
	})
}

func (s *Server) auditInterviewAgentRun(request *http.Request, user *domain.User, sessionID string, trace domain.AgentTrace, result agentruntime.InterviewResult, status string, runErr error) {
	errorSummary := ""
	if runErr != nil {
		errorSummary = "agent run failed"
	}
	s.auditAgentRun(request, user, agentAuditPayload{
		Agent:           firstNonEmpty(strings.TrimSpace(trace.Agent), "interview_agent"),
		Action:          "agent.interview_run",
		ResourceType:    "interview_session",
		ResourceID:      sessionID,
		Status:          status,
		ToolCount:       trace.ToolCount,
		FallbackUsed:    result.FallbackUsed,
		SafetyRewritten: result.SafetyRewritten,
		ErrorSummary:    errorSummary,
	})
}

func (s *Server) auditCommunityReviewAgentRun(request *http.Request, user *domain.User, postID string, trace domain.AgentTrace, summary *domain.ModerationSummary, status string, runErr error) {
	errorSummary := ""
	if runErr != nil {
		errorSummary = "agent run failed"
	}
	flagged := false
	if summary != nil {
		flagged = summary.Flagged
	}
	s.auditAgentRun(request, user, agentAuditPayload{
		Agent:           firstNonEmpty(strings.TrimSpace(trace.Agent), "cm_review_agent"),
		Action:          "agent.community_review_run",
		ResourceType:    "community_post",
		ResourceID:      postID,
		Status:          status,
		ToolCount:       trace.ToolCount,
		SafetyRewritten: false,
		Flagged:         flagged,
		ErrorSummary:    errorSummary,
	})
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func truncateText(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	if maxRunes <= 1 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "..."
}

func (s *Server) withUser(w http.ResponseWriter, r *http.Request, next func(*domain.User)) {
	token, err := auth.BearerToken(r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	user, _, err := s.authUser(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if !s.allow(w, r, "user:"+user.ID, 120) {
		return
	}
	next(user)
}

func (s *Server) authUser(token string) (*domain.User, auth.Claims, error) {
	claims, err := s.auth.Validate(token)
	if err != nil {
		return nil, auth.Claims{}, err
	}
	user, ok := s.store.GetUser(claims.Subject)
	if !ok {
		return nil, auth.Claims{}, errors.New("user not found")
	}
	if claims.TokenVersion != user.TokenVersion {
		return nil, auth.Claims{}, errors.New("token revoked")
	}
	return user, claims, nil
}

func anonymousPasswordResetEnabled(dataStore store.Store) bool {
	switch dataStore.(type) {
	case *store.PostgresStore:
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_ANON_PASSWORD_RESET"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func publicAIStatus(info ai.ProviderInfo) interface{} {
	return struct {
		Provider           string `json:"provider"`
		Model              string `json:"model"`
		Fallback           bool   `json:"fallback"`
		ConfiguredProvider string `json:"configured_provider,omitempty"`
		ConfiguredModel    string `json:"configured_model,omitempty"`
		StreamEnabled      bool   `json:"stream_enabled"`
		RouterVersion      string `json:"router_version"`
		Healthy            bool   `json:"healthy"`
		Health             string `json:"health"`
		Transport          string `json:"transport"`
	}{
		Provider:           info.Provider,
		Model:              info.Model,
		Fallback:           info.Fallback,
		ConfiguredProvider: info.ConfiguredProvider,
		ConfiguredModel:    info.ConfiguredModel,
		StreamEnabled:      info.StreamEnabled,
		RouterVersion:      info.RouterVersion,
		Healthy:            info.Healthy,
		Health:             info.Health,
		Transport:          info.Transport,
	}
}

var defaultCORSAllowedOrigins = map[string]bool{
	"http://localhost:5173": true,
	"http://127.0.0.1:5173": true,
	"http://0.0.0.0:5173":   true,
	"http://localhost:4173": true,
	"http://127.0.0.1:4173": true,
	"http://0.0.0.0:4173":   true,
}

func corsAllowedOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	configured := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if configured == "" {
		if defaultCORSAllowedOrigins[origin] {
			return origin
		}
		return ""
	}
	if configured == "*" {
		return "*"
	}
	for _, item := range strings.Split(configured, ",") {
		if strings.EqualFold(strings.TrimSpace(item), origin) {
			return origin
		}
	}
	return ""
}

func (s *Server) allow(w http.ResponseWriter, r *http.Request, key string, limit int) bool {
	if s.limiter == nil || !s.limiter.Enabled() {
		return true
	}
	if s.limiter.Allow(context.Background(), "ratelimit:"+key, limit, time.Minute) {
		return true
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		Action:       "rate_limit.hit",
		ResourceType: "rate_limit",
		ResourceID:   key,
		IPAddress:    clientIP(r),
		UserAgent:    truncateText(r.UserAgent(), 160),
		Metadata:     map[string]string{"limit": strconv.Itoa(limit)},
	})
	writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
	return false
}

func (s *Server) allowAI(w http.ResponseWriter, r *http.Request, user *domain.User, action string, limit int) bool {
	if user == nil {
		return false
	}
	if action == "scenario-generation" {
		if user.Role == domain.RoleStudent {
			limit = 3
		} else if limit <= 0 {
			limit = 10
		}
	}
	return s.allow(w, r, "ai:"+action+":user:"+user.ID, limit)
}

func clientIP(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ruleSensitiveCheck(field, text string) domain.SensitiveCheckResult {
	result := domain.SensitiveCheckResult{
		Status:    "clear",
		Findings:  []domain.SensitiveFinding{},
		Source:    "rule",
		RiskLevel: "none",
		Summary:   "规则检测未发现敏感信息风险。",
		CheckedAt: time.Now(),
	}
	if strings.TrimSpace(text) == "" {
		return result
	}
	add := func(kind, excerpt, severity, suggestion string) {
		result.Findings = append(result.Findings, domain.SensitiveFinding{
			Type:            kind,
			Field:           field,
			Excerpt:         ai.Sanitize(truncateText(excerpt, 80)),
			Severity:        severity,
			Suggestion:      suggestion,
			Source:          "rule",
			Confidence:      1,
			RedactedExcerpt: ai.Sanitize(truncateText(excerpt, 80)),
		})
	}
	if strings.Contains(strings.ToLower(text), "password=") || strings.Contains(strings.ToLower(text), "passwd") {
		add("password", "password/passwd", "high", "删除或替换密码字段后再发布。")
	}
	if strings.Contains(strings.ToLower(text), "secret") || strings.Contains(strings.ToLower(text), "api_key") || strings.Contains(strings.ToLower(text), "sk-") {
		add("secret", "secret/api_key/sk-", "high", "删除真实密钥，仅保留脱敏占位。")
	}
	for _, token := range strings.Fields(text) {
		if net.ParseIP(strings.Trim(token, " ,;，。")) != nil {
			add("ip", token, "medium", "将真实 IP 替换为网段或脱敏占位。")
		}
	}
	companyTokens := []string{"有限公司", "集团", "corp", "inc", "company"}
	for _, token := range companyTokens {
		if strings.Contains(strings.ToLower(text), strings.ToLower(token)) {
			add("company", token, "medium", "将真实公司名替换为业务系统代称。")
			break
		}
	}
	if len(result.Findings) > 0 {
		result.Status = "risk"
		result.Sanitized = true
		result.RiskLevel = "high"
		result.Blocked = true
		for _, finding := range result.Findings {
			if finding.Severity != "high" {
				result.RiskLevel = "medium"
				result.Blocked = false
				break
			}
		}
		result.Summary = fmt.Sprintf("规则检测发现 %d 项敏感信息风险。", len(result.Findings))
	}
	return ai.NormalizeSensitiveCheck(result, "rule")
}

func (s *Server) sensitiveCheck(r *http.Request, user *domain.User, field, text string) domain.SensitiveCheckResult {
	ruleResult := ruleSensitiveCheck(field, text)
	if strings.TrimSpace(text) == "" {
		return ruleResult
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	modelResult, meta, err := s.llmRouter().CheckSensitiveContent(ctx, ai.SensitiveCheckRequest{
		Field: field,
		Text:  text,
	})
	if err != nil {
		result := ai.SensitiveFallbackResult(ruleResult, "rule_fallback")
		s.audit(r, user, "ai.safety_check_fallback", "sensitive_check", field, map[string]string{
			"provider": meta.Provider,
			"reason":   truncateText(err.Error(), 120),
		})
		return result
	}
	if meta.FallbackUsed {
		result := ai.SensitiveFallbackResult(ruleResult, "rule_fallback")
		s.audit(r, user, "ai.safety_check_fallback", "sensitive_check", field, map[string]string{
			"provider": meta.Provider,
			"reason":   "llm router fallback used",
		})
		return result
	}
	modelResult.CheckedAt = time.Now()
	merged := ai.MergeSensitiveChecks(ruleResult, modelResult)
	return merged
}

func (s *Server) audit(r *http.Request, user *domain.User, action, resourceType, resourceID string, metadata map[string]string) {
	actorID := ""
	if user != nil {
		actorID = user.ID
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		IPAddress:    clientIP(r),
		UserAgent:    truncateText(r.UserAgent(), 160),
		Metadata:     metadata,
	})
}

func paginate[T any](items []T, r *http.Request) []T {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func split(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// maxJSONBodyBytes caps the size of JSON request bodies accepted by decode.
// Multipart asset uploads do not go through decode and keep their own larger
// limit (maxVoiceAssetBytes).
const maxJSONBodyBytes int64 = 4 << 20 // 4 MiB

func decode(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func writeOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(envelope{Code: 200, Message: "success", Data: data})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Code: status, Message: message})
}

func setCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	allowedOrigin := corsAllowedOrigin(origin)
	if allowedOrigin == "" {
		return false
	}
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Max-Age", "600")
	return true
}
