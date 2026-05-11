package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/ratelimit"
	"situational-teaching/backend/internal/store"
)

type Server struct {
	store     store.Store
	auth      *auth.Manager
	limiter   ratelimit.Limiter
	llmMu     sync.RWMutex
	llm       *ai.Router
	stt       STTProvider
	embedding ai.EmbeddingClient
	assets    AssetStorage
	jobMu     sync.Mutex
	jobStop   map[string]context.CancelFunc
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

type STTConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

type envelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type scenarioGenerationPayload struct {
	Domain       string                                `json:"domain"`
	Difficulty   string                                `json:"difficulty"`
	ScenarioType string                                `json:"scenario_type"`
	Tags         []string                              `json:"tags"`
	Constraints  *scenarioGenerationConstraintsPayload `json:"constraints,omitempty"`
}

type scenarioGenerationConstraintsPayload struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TopicScope    []string `json:"topic_scope"`
	RootCauseHint string   `json:"root_cause_hint"`
	EvidenceHints []string `json:"evidence_hints"`
	ClueHints     []string `json:"clue_hints"`
}

type STTRequest struct {
	Asset    *domain.Asset
	Session  *domain.InterviewSession
	Seed     string
	Language string
	Prompt   string
}

type STTResult struct {
	Transcript       string
	DurationSeconds  int
	DetectedLanguage string
	Confidence       float64
	Status           string
}

type STTProvider interface {
	Transcribe(context.Context, STTRequest) (STTResult, error)
}

type STTProviderError struct {
	StatusCode      int
	ProviderType    string
	ProviderMessage string
}

type scenarioGenerationValidationError struct {
	status  int
	message string
}

func (e scenarioGenerationValidationError) Error() string {
	return e.message
}

func (e STTProviderError) Error() string {
	message := strings.TrimSpace(e.ProviderMessage)
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	if message == "" {
		message = "unknown stt provider error"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("stt provider returned status %d: %s", e.StatusCode, message)
	}
	return "stt provider error: " + message
}

type MockSTTProvider struct{}

func (MockSTTProvider) Transcribe(_ context.Context, req STTRequest) (STTResult, error) {
	transcript := strings.TrimSpace(req.Seed)
	if transcript == "" {
		transcript = mockVoiceTranscriptDraft(req.Asset, req.Session)
	}
	return STTResult{
		Transcript:       transcript,
		DurationSeconds:  0,
		DetectedLanguage: detectAnswerLanguage(transcript),
		Confidence:       0.92,
		Status:           "transcribed",
	}, nil
}

type OpenAITranscriptionProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
	assets  AssetStorage
}

const (
	defaultZetaSTTBaseURL   = "https://api.zetatechs.com"
	defaultZetaSTTModel     = "gpt-4o-mini-transcribe-2025-12-15"
	defaultJianyiSTTBaseURL = "https://jeniya.top"
	defaultJianyiSTTModel   = "gpt-4o-mini-transcribe"
)

var (
	allowedScenarioDifficulties = map[string]bool{"L1": true, "L2": true, "L3": true, "L4": true, "L5": true}
	allowedScenarioTypes        = map[string]bool{"troubleshooting": true, "design": true, "performance": true}
)

func NewSTTProviderFromEnv(assets AssetStorage) STTProvider {
	sttAPIKey := strings.TrimSpace(os.Getenv("STT_API_KEY"))
	zetaKey := strings.TrimSpace(os.Getenv("ZETA_KEY"))
	jianyiKey := strings.TrimSpace(os.Getenv("JIANYI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("STT_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("STT_MODEL"))
	if baseURL == "" {
		if zetaKey != "" || sttAPIKey != "" {
			baseURL = defaultZetaSTTBaseURL
		} else {
			baseURL = defaultJianyiSTTBaseURL
		}
	}
	if model == "" {
		if zetaKey != "" || sttAPIKey != "" {
			model = defaultZetaSTTModel
		} else {
			model = defaultJianyiSTTModel
		}
	}
	cfg := STTConfig{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  firstNonEmpty(sttAPIKey, zetaKey, jianyiKey),
		Model:   model,
		Timeout: 60 * time.Second,
	}
	if rawTimeout := strings.TrimSpace(os.Getenv("STT_TIMEOUT_SECONDS")); rawTimeout != "" {
		if seconds, err := strconv.Atoi(rawTimeout); err == nil && seconds > 0 {
			cfg.Timeout = time.Duration(seconds) * time.Second
		}
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return MockSTTProvider{}
	}
	return NewOpenAITranscriptionProvider(cfg, assets)
}

func NewOpenAITranscriptionProvider(cfg STTConfig, assets AssetStorage) *OpenAITranscriptionProvider {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &OpenAITranscriptionProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: cfg.Timeout},
		assets:  assets,
	}
}

func (p *OpenAITranscriptionProvider) Transcribe(ctx context.Context, req STTRequest) (STTResult, error) {
	if req.Asset == nil {
		return STTResult{}, fmt.Errorf("asset is required")
	}
	file, err := p.assets.Open(ctx, req.Asset)
	if err != nil {
		return STTResult{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", p.model); err != nil {
		return STTResult{}, err
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return STTResult{}, err
	}
	if language := strings.TrimSpace(req.Language); language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return STTResult{}, err
		}
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		if err := writer.WriteField("prompt", prompt); err != nil {
			return STTResult{}, err
		}
	}
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename="%s"`, req.Asset.Filename)},
		"Content-Type":        {req.Asset.MimeType},
	})
	if err != nil {
		return STTResult{}, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return STTResult{}, err
	}
	if err := writer.Close(); err != nil {
		return STTResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return STTResult{}, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return STTResult{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return STTResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return STTResult{}, parseSTTProviderError(resp.StatusCode, respBody)
	}
	var parsed struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return STTResult{}, err
	}
	transcript := strings.TrimSpace(parsed.Text)
	if transcript == "" {
		return STTResult{}, fmt.Errorf("empty transcription")
	}
	return STTResult{
		Transcript:       transcript,
		DurationSeconds:  int(parsed.Duration + 0.5),
		DetectedLanguage: defaultSTTLanguage(parsed.Language, transcript),
		Confidence:       0.9,
		Status:           "transcribed",
	}, nil
}

func parseSTTProviderError(statusCode int, respBody []byte) error {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	message := strings.TrimSpace(string(respBody))
	providerType := ""
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		if parsed.Error.Message != "" {
			message = parsed.Error.Message
		} else if parsed.Message != "" {
			message = parsed.Message
		}
		if parsed.Error.Type != "" {
			providerType = parsed.Error.Type
		} else if parsed.Type != "" {
			providerType = parsed.Type
		}
	}
	return STTProviderError{
		StatusCode:      statusCode,
		ProviderType:    truncateText(providerType, 80),
		ProviderMessage: truncateText(message, 240),
	}
}

func writeSTTError(w http.ResponseWriter, err error) {
	writeError(w, sttErrorHTTPStatus(err), sttErrorUserMessage(err))
}

func sttErrorHTTPStatus(err error) int {
	var providerErr STTProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return http.StatusBadGateway
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests
		default:
			if providerErr.StatusCode >= 500 {
				return http.StatusBadGateway
			}
			if providerErr.StatusCode >= 400 {
				return http.StatusBadGateway
			}
		}
	}
	return http.StatusBadGateway
}

func sttErrorUserMessage(err error) string {
	var providerErr STTProviderError
	if errors.As(err, &providerErr) {
		detail := strings.ToLower(providerErr.ProviderMessage + " " + providerErr.ProviderType)
		switch {
		case providerErr.StatusCode == http.StatusUnauthorized || providerErr.StatusCode == http.StatusForbidden:
			return "语音转写服务鉴权失败，请检查 ZETA_KEY、STT_API_KEY 或 JIANYI_API_KEY 配置后重试"
		case providerErr.StatusCode == http.StatusTooManyRequests:
			return "语音转写服务请求过于频繁，请稍后重试"
		case strings.Contains(detail, "无可用渠道") || strings.Contains(detail, "distributor") || strings.Contains(detail, "no available"):
			return "语音转写服务当前无可用通道，请检查 STT_MODEL、STT_BASE_URL 或中转站渠道配置后重试"
		case providerErr.StatusCode >= 500:
			return "语音转写服务暂时不可用，请稍后重试或检查中转站模型通道"
		case providerErr.StatusCode >= 400:
			return "语音转写请求被服务拒绝，请检查音频格式、STT_MODEL、STT_BASE_URL 和中转站配置后重试"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "语音转写服务响应超时，请稍后重试"
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "empty transcription") {
		return "语音转写失败，请确认文件包含可识别语音后重新上传"
	}
	return "语音转写失败，请稍后重试或改为文本回答"
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
		store:     dataStore,
		auth:      authManager,
		limiter:   limiter,
		llm:       llmRouter,
		assets:    assetStorage,
		stt:       NewSTTProviderFromEnv(assetStorage),
		embedding: ai.NewEmbeddingClientFromEnv(),
		jobStop:   map[string]context.CancelFunc{},
	}
	server.applyPromptOverrides()
	return server
}

func NewServerForTests(dataStore store.Store, authManager *auth.Manager) *Server {
	var limiter ratelimit.Limiter = ratelimit.NewNoopLimiter()
	server := NewServer(dataStore, authManager, limiter, ai.NewRouter(ai.Config{Provider: ai.ProviderMock}))
	server.stt = MockSTTProvider{}
	server.embedding = nil
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
		setCORS(w)
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
			writeOK(w, s.llmRouter().Info())
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

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request, path string) {
	switch path {
	case "register":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Username string `json:"username"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		user, err := s.store.CreateUser(req.Username, req.Email, auth.HashPassword(req.Password))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, user, "auth.register", "user", user.ID, nil)
		access, refresh, err := s.auth.IssuePair(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": user, "access_token": access, "refresh_token": refresh})
	case "login":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Identifier string `json:"identifier"`
			Password   string `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		user, ok := s.store.FindUserByIdentifier(req.Identifier)
		if !ok || !auth.CheckPassword(req.Password, user.PasswordHash) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		s.audit(r, user, "auth.login", "user", user.ID, nil)
		access, refresh, err := s.auth.IssuePair(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": user, "access_token": access, "refresh_token": refresh})
	case "password-reset":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Identifier  string `json:"identifier"`
			NewPassword string `json:"new_password"`
		}
		if !decode(w, r, &req) {
			return
		}
		user, ok := s.store.FindUserByIdentifier(req.Identifier)
		if !ok {
			writeError(w, http.StatusNotFound, "账号不存在")
			return
		}
		updated, err := s.resetUserPassword(r, user, req.NewPassword)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": updated})
	case "refresh":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if !decode(w, r, &req) {
			return
		}
		claims, err := s.auth.Validate(req.RefreshToken)
		if err != nil || claims.Type != "refresh" {
			writeError(w, http.StatusUnauthorized, "invalid refresh token")
			return
		}
		user, ok := s.store.GetUser(claims.Subject)
		if !ok {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		access, refresh, err := s.auth.IssuePair(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": user, "access_token": access, "refresh_token": refresh})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) resetUserPassword(r *http.Request, user *domain.User, newPassword string) (*domain.User, error) {
	password := strings.TrimSpace(newPassword)
	if len(password) < 6 {
		return nil, errors.New("新密码至少需要 6 位")
	}
	updated, err := s.store.UpdateUserPassword(user.ID, auth.HashPassword(password))
	if err != nil {
		return nil, err
	}
	s.audit(r, updated, "auth.password_reset", "user", updated.ID, nil)
	return updated, nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	switch suffix {
	case "":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, user)
	case "/profile":
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			TargetLevel      string   `json:"target_level"`
			PreferredDomains []string `json:"preferred_domains"`
		}
		if !decode(w, r, &req) {
			return
		}
		updated, err := s.store.UpdateProfile(user.ID, req.TargetLevel, req.PreferredDomains)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, updated)
	case "/password":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			NewPassword string `json:"new_password"`
		}
		if !decode(w, r, &req) {
			return
		}
		updated, err := s.resetUserPassword(r, user, req.NewPassword)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": updated})
	case "/dashboard":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		plan := s.learningPlan(user)
		calendar := reviewCalendarFromPlan(user, plan, time.Now())
		writeOK(w, map[string]interface{}{
			"user":             user,
			"stats":            user.Profile.TotalStats,
			"capability_radar": user.Profile.CapabilityRadar,
			"weak_points":      weakPointsFromPlan(plan, user.Profile.WeakPoints),
			"recommendations":  scenarioRecommendationsFromPlan(plan),
			"learning_plan":    plan,
			"review_calendar":  calendar,
		})
	case "/history":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.history(user.ID))
	case "/recommendations":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.learningPlan(user))
	case "/learning-plan":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.learningPlan(user))
	case "/review-calendar":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, reviewCalendarFromPlan(user, s.learningPlan(user), time.Now()))
	case "/checkin":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		result, updated, err := s.checkin(user, time.Now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"checkin": result, "user": updated})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleAI(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 2 && parts[0] == "safety" && parts[1] == "check" && r.Method == http.MethodPost {
		var req struct {
			Text  string `json:"text"`
			Field string `json:"field"`
		}
		if !decode(w, r, &req) {
			return
		}
		writeOK(w, s.sensitiveCheck(r, user, req.Field, req.Text))
		return
	}
	if len(parts) == 2 && parts[0] == "jobs" && r.Method == http.MethodGet {
		job, ok := s.store.GetAIJob(parts[1])
		if !ok || !canViewAIJob(job, user) {
			writeError(w, http.StatusNotFound, "ai job not found")
			return
		}
		writeOK(w, s.aiJobPayload(job, user))
		return
	}
	if len(parts) == 3 && parts[0] == "jobs" && parts[2] == "events" && r.Method == http.MethodGet {
		job, ok := s.store.GetAIJob(parts[1])
		if !ok || !canViewAIJob(job, user) {
			writeError(w, http.StatusNotFound, "ai job not found")
			return
		}
		s.writeAIJobEvents(w, r, user, job.ID)
		return
	}
	if len(parts) == 3 && parts[0] == "jobs" && parts[2] == "cancel" && r.Method == http.MethodPost {
		job, ok := s.store.GetAIJob(parts[1])
		if !ok || !canViewAIJob(job, user) {
			writeError(w, http.StatusNotFound, "ai job not found")
			return
		}
		canceled, err := s.cancelAIJob(job)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, s.aiJobPayload(canceled, user))
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 0 && r.Method == http.MethodPost {
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			s.handleAssetUpload(w, r, user)
			return
		}
		s.handleAssetMetadataCreate(w, r, user)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		asset, ok := s.store.GetAsset(parts[0])
		if !ok || (asset.UserID != user.ID && user.Role != domain.RoleAdmin) {
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		normalized := normalizeAssetURLs(*asset)
		if r.URL.Query().Get("content") == "1" || r.URL.Query().Get("download") == "1" {
			s.serveAssetContent(w, r, &normalized)
			return
		}
		writeOK(w, normalized)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleAssetMetadataCreate(w http.ResponseWriter, r *http.Request, user *domain.User) {
	var req struct {
		Kind     string `json:"kind"`
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}
	if !decode(w, r, &req) {
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "voice"
	}
	mimeType := strings.TrimSpace(req.MimeType)
	if kind == "voice" {
		if err := validateVoiceAsset(strings.TrimSpace(req.Filename), mimeType, req.Size); err != nil {
			writeAssetValidationError(w, err)
			return
		}
	}
	asset, err := s.store.CreateAsset(domain.Asset{
		UserID:   user.ID,
		Kind:     kind,
		Filename: strings.TrimSpace(req.Filename),
		MimeType: mimeType,
		Size:     req.Size,
		Checksum: strings.TrimSpace(req.Checksum),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset = normalizeAssetURLs(asset)
	s.audit(r, user, "asset.create", "asset", asset.ID, map[string]string{"kind": asset.Kind, "mode": "metadata"})
	writeOK(w, asset)
}

func (s *Server) handleAssetUpload(w http.ResponseWriter, r *http.Request, user *domain.User) {
	if err := r.ParseMultipartForm(maxVoiceAssetBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_asset: cannot read uploaded file")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("asset")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_asset: audio file is required")
		return
	}
	defer file.Close()

	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind == "" {
		kind = "voice"
	}
	filename := strings.TrimSpace(header.Filename)
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = mimeTypeFromFilename(filename)
	}
	if err := validateVoiceAsset(filename, mimeType, header.Size); kind == "voice" && err != nil {
		writeAssetValidationError(w, err)
		return
	}

	assetID := store.NewID()
	stored, err := s.assets.Save(r.Context(), AssetStorageSaveRequest{
		UserID:   user.ID,
		AssetID:  assetID,
		Filename: filename,
		MaxBytes: maxVoiceAssetBytes,
	}, file)
	if err != nil {
		writeAssetStorageError(w, err)
		return
	}

	asset, err := s.store.CreateAsset(domain.Asset{
		ID:         assetID,
		UserID:     user.ID,
		Kind:       kind,
		Filename:   filename,
		MimeType:   mimeType,
		Size:       stored.Size,
		StorageKey: stored.StorageKey,
		URL:        assetMetadataURL(assetID),
		ContentURL: assetContentURL(assetID),
		Checksum:   stored.Checksum,
	})
	if err != nil {
		_ = s.assets.Delete(r.Context(), &domain.Asset{StorageKey: stored.StorageKey})
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset = normalizeAssetURLs(asset)
	s.audit(r, user, "asset.create", "asset", asset.ID, map[string]string{"kind": asset.Kind, "mode": "upload"})
	writeOK(w, asset)
}

func (s *Server) serveAssetContent(w http.ResponseWriter, r *http.Request, asset *domain.Asset) {
	reader, err := s.assets.Open(r.Context(), asset)
	if err != nil {
		writeAssetStorageError(w, err)
		return
	}
	defer reader.Close()
	if asset.MimeType != "" {
		w.Header().Set("Content-Type", asset.MimeType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(asset.Filename)))
	http.ServeContent(w, r, filepath.Base(asset.Filename), asset.CreatedAt, reader)
}

func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	if suffix == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items := s.store.ListScenarios(r.URL.Query().Get("domain"), r.URL.Query().Get("difficulty"), r.URL.Query().Get("tag"))
		views := make([]domain.ScenarioQuestionView, 0, len(items))
		for _, item := range items {
			if item.Status != "active" || !canViewScenario(&item, user) {
				continue
			}
			views = append(views, scenarioPublicView(&item))
		}
		writeOK(w, map[string]interface{}{"list": paginate(views, r), "total": len(views)})
		return
	}

	if suffix == "/generate/jobs" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowAI(w, r, user, "scenario-generation", 10) {
			return
		}
		var req scenarioGenerationPayload
		if !decode(w, r, &req) {
			return
		}
		normalized, err := normalizeScenarioGenerationPayload(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateScenarioGenerationRequest(r, user, normalized); err != nil {
			writeScenarioGenerationValidationError(w, err)
			return
		}
		job, err := s.createScenarioGenerationJob(user.ID, normalized)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create AI job")
			return
		}
		writeOK(w, s.aiJobPayload(&job, user))
		return
	}

	if suffix == "/generate" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowAI(w, r, user, "scenario-generation", 10) {
			return
		}
		var req scenarioGenerationPayload
		if !decode(w, r, &req) {
			return
		}
		normalized, err := normalizeScenarioGenerationPayload(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateScenarioGenerationRequest(r, user, normalized); err != nil {
			writeScenarioGenerationValidationError(w, err)
			return
		}
		question, llmMeta, err := s.llmRouter().GenerateScenario(r.Context(), ai.ScenarioGenerationRequest{
			Domain:       normalized.Domain,
			Difficulty:   normalized.Difficulty,
			ScenarioType: normalized.ScenarioType,
			Tags:         normalized.Tags,
			Constraints:  normalized.toAIConstraints(),
			UserID:       user.ID,
			Nonce:        fmt.Sprintf("%d", time.Now().UnixNano()),
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, scenarioGenerationErrorMessage(err, llmMeta))
			return
		}
		created := s.store.AddScenario(question)
		s.auditScenarioGenerationCompleted(user.ID, "", created, llmMeta, normalized)
		writeOK(w, map[string]interface{}{
			"question_id":   created.ID,
			"status":        "active",
			"question":      scenarioView(&created, user),
			"provider":      llmMeta.Provider,
			"model":         llmMeta.Model,
			"validated":     llmMeta.Validated,
			"fallback_used": llmMeta.FallbackUsed,
		})
		return
	}

	parts := split(suffix)
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if parts[0] == "sessions" {
		s.handleScenarioSession(w, r, user, parts[1:])
		return
	}

	questionID := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		question, ok := s.store.GetScenario(questionID)
		if !ok {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		if question.Status != "active" || !canViewScenario(question, user) {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		writeOK(w, scenarioDetailView(question, user))
		return
	}
	if len(parts) == 2 && parts[1] == "sessions" && r.Method == http.MethodPost {
		question, ok := s.store.GetScenario(questionID)
		if !ok || question.Status != "active" {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		session, err := s.store.CreateScenarioSession(user.ID, questionID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{
			"session_id":        session.ID,
			"status":            session.Status,
			"question_snapshot": scenarioPublicView(&session.QuestionSnapshot),
		})
		return
	}
	if len(parts) == 2 && parts[1] == "fork" && r.Method == http.MethodPost {
		question, ok := s.store.GetScenario(questionID)
		if !ok || !canViewScenario(question, user) || question.Status != "active" {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		post := communityPostFromScenarioFork(question, user.ID)
		post.SensitiveCheck = s.sensitiveCheck(r, user, "fork_source", strings.Join([]string{post.Title, post.RawContent, strings.Join(post.Tags, " ")}, "\n"))
		post = s.store.AddCommunityPost(post)
		s.audit(r, user, "scenario.fork", "community_post", post.ID, map[string]string{"source_scenario_id": question.ID})
		writeOK(w, post)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleScenarioSession(w http.ResponseWriter, r *http.Request, user *domain.User, parts []string) {
	if len(parts) == 1 && r.Method == http.MethodGet {
		sessionID := parts[0]
		session, ok := s.store.GetScenarioSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		_ = s.expireScenarioSessionIfIdle(session)
		writeOK(w, map[string]interface{}{
			"session":  scenarioSessionView(session),
			"messages": s.store.ListScenarioMessages(sessionID),
		})
		return
	}
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	sessionID := parts[0]
	action := parts[1]
	switch action {
	case "messages":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.allowAI(w, r, user, "scenario-reply", 60) {
			return
		}
		var req struct {
			Content string `json:"content"`
		}
		if !decode(w, r, &req) {
			return
		}
		var writer *sseWriter
		var onStage func(string, string)
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			onStage = writer.stage
			writer.stage("agent_intent", "正在分析你的排查意图")
		}
		message, session, err := s.processScenarioMessage(r.Context(), user, sessionID, strings.TrimSpace(req.Content), r, onStage)
		if err != nil {
			if writer != nil {
				writer.fail(err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		payload := map[string]interface{}{
			"message":        message,
			"response_meta":  message.ResponseMeta,
			"session_status": session.Status,
			"session":        scenarioSessionView(session),
		}
		if writer != nil {
			writer.stage("completed", "本轮 Agent 排查完成")
			writer.finish(payload)
			return
		}
		writeOK(w, payload)
	case "answer":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Answer string `json:"answer"`
		}
		if !decode(w, r, &req) {
			return
		}
		session, err := s.evaluateScenarioAnswer(user, sessionID, req.Answer)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{
			"evaluation_id": session.ID + "-evaluation",
			"status":        session.Status,
			"result":        session.EvaluationResult,
			"score":         session.Score,
		})
	case "review":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		session, ok := s.store.GetScenarioSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if s.expireScenarioSessionIfIdle(session) {
			writeError(w, http.StatusBadRequest, "session is abandoned")
			return
		}
		writeOK(w, map[string]interface{}{
			"session":         scenarioSessionView(session),
			"messages":        s.store.ListScenarioMessages(sessionID),
			"standard_answer": session.QuestionSnapshot.Content.RootCause,
			"standard_steps":  session.QuestionSnapshot.Content.StandardProcedure,
			"key_evidence":    session.QuestionSnapshot.Content.KeyEvidence,
		})
	case "quit":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		session, ok := s.store.GetScenarioSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if s.expireScenarioSessionIfIdle(session) {
			writeOK(w, map[string]interface{}{"status": session.Status, "session": scenarioSessionView(session)})
			return
		}
		now := time.Now()
		session.Status = "abandoned"
		session.EndedAt = &now
		s.store.SaveScenarioSession(session)
		writeOK(w, map[string]interface{}{"status": "abandoned", "session": scenarioSessionView(session)})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleInterviews(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 1 && parts[0] == "sessions" && r.Method == http.MethodPost {
		var req struct {
			Domain       string `json:"domain"`
			Difficulty   string `json:"difficulty"`
			QuestionType string `json:"question_type"`
		}
		if !decode(w, r, &req) {
			return
		}
		req.Domain = strings.TrimSpace(req.Domain)
		req.Difficulty = strings.TrimSpace(req.Difficulty)
		req.QuestionType = strings.TrimSpace(req.QuestionType)
		if req.Domain == "" || req.Difficulty == "" || req.QuestionType == "" {
			writeError(w, http.StatusBadRequest, "domain, difficulty and question_type are required")
			return
		}
		question, ok := s.store.FindInterviewQuestion(req.Domain, req.Difficulty, req.QuestionType)
		if !ok {
			writeError(w, http.StatusNotFound, "interview question not found")
			return
		}
		session := s.store.CreateInterviewSession(user.ID, question)
		writeOK(w, map[string]interface{}{
			"session_id": session.ID,
			"status":     session.Status,
			"question":   interviewQuestionView(question, user),
			"session":    session,
		})
		return
	}
	if len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodGet {
		sessionID := parts[1]
		session, ok := s.store.GetInterviewSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		question, ok := s.store.GetInterviewQuestion(session.QuestionID)
		if !ok {
			writeError(w, http.StatusNotFound, "interview question not found")
			return
		}
		hydrateInterviewSubmissionAssets(s.store, session)
		writeOK(w, map[string]interface{}{
			"session":  session,
			"question": interviewQuestionView(question, user),
		})
		return
	}
	if len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodDelete {
		sessionID := parts[1]
		session, ok := s.store.GetInterviewSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		if !s.store.DeleteInterviewSession(sessionID) {
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		writeOK(w, map[string]interface{}{
			"deleted": true,
			"id":      sessionID,
		})
		return
	}
	if len(parts) >= 3 && parts[0] == "sessions" {
		sessionID := parts[1]
		action := parts[2]
		if action == "submit" && r.Method == http.MethodPost {
			s.handleInterviewSubmission(w, r, user, sessionID)
			return
		}
		if action == "voice" && r.Method == http.MethodPost {
			s.handleInterviewVoice(w, r, user, sessionID)
			return
		}
		if len(parts) == 4 && action == "followup" && parts[3] == "answer" && r.Method == http.MethodPost {
			s.handleInterviewSubmission(w, r, user, sessionID)
			return
		}
		if action == "report" && r.Method == http.MethodGet {
			session, ok := s.store.GetInterviewSession(sessionID)
			if !ok || session.UserID != user.ID {
				writeError(w, http.StatusNotFound, "interview session not found")
				return
			}
			question, _ := s.store.GetInterviewQuestion(session.QuestionID)
			hydrateInterviewSubmissionAssets(s.store, session)
			writeOK(w, map[string]interface{}{
				"session":      session,
				"question":     interviewQuestionView(question, user),
				"radar_data":   radarData(session),
				"final_score":  session.FinalScore,
				"final_report": session.FinalReport,
			})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleInterviewSubmission(w http.ResponseWriter, r *http.Request, user *domain.User, sessionID string) {
	if !s.allowAI(w, r, user, "interview-feedback", 30) {
		return
	}
	var req struct {
		Content             string `json:"content"`
		Type                string `json:"type"`
		Source              string `json:"source"`
		AssetID             string `json:"asset_id"`
		Transcript          string `json:"transcript"`
		DurationSeconds     int    `json:"duration_seconds"`
		ConfirmedTranscript bool   `json:"confirmed_transcript"`
	}
	if !decode(w, r, &req) {
		return
	}
	var writer *sseWriter
	if wantsSSE(r) {
		writer = newSSEWriter(w)
	}
	if req.Type == "" {
		req.Type = "text"
	}
	if req.Type == "voice" && strings.TrimSpace(req.Content) == "" {
		req.Content = req.Transcript
	}
	if strings.TrimSpace(req.Content) == "" {
		if writer != nil {
			writer.fail("content is required")
			return
		}
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	session, ok := s.store.GetInterviewSession(sessionID)
	if !ok || session.UserID != user.ID {
		if writer != nil {
			writer.fail("interview session not found")
			return
		}
		writeError(w, http.StatusNotFound, "interview session not found")
		return
	}
	if !interviewSessionAcceptsSubmission(session) {
		if writer != nil {
			writer.fail("interview session is already completed")
			return
		}
		writeError(w, http.StatusConflict, "interview session is already completed")
		return
	}
	question, ok := s.store.GetInterviewQuestion(session.QuestionID)
	if !ok {
		if writer != nil {
			writer.fail("interview question not found")
			return
		}
		writeError(w, http.StatusNotFound, "interview question not found")
		return
	}
	round := session.CurrentRound
	assetURL := ""
	var assetSnapshot *domain.Asset
	source := strings.TrimSpace(req.Source)
	transcript := strings.TrimSpace(req.Transcript)
	var voiceQuality *domain.VoiceQualityResult
	if req.AssetID != "" {
		asset, ok := s.store.GetAsset(req.AssetID)
		if !ok || asset.UserID != user.ID {
			if writer != nil {
				writer.fail("asset not found")
				return
			}
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		normalizedAsset := normalizeAssetURLs(*asset)
		assetURL = normalizedAsset.ContentURL
		assetSnapshot = &normalizedAsset
		if req.Type == "voice" {
			if !req.ConfirmedTranscript {
				if writer != nil {
					writer.fail("please confirm transcript before scoring")
					return
				}
				writeError(w, http.StatusBadRequest, "please confirm transcript before scoring")
				return
			}
			if err := validateVoiceAsset(asset.Filename, asset.MimeType, asset.Size); err != nil {
				if writer != nil {
					writer.fail(err.Error())
					return
				}
				writeAssetValidationError(w, err)
				return
			}
			validation := validateInterviewAnswer(question, req.Content, transcript, asset, 0.9)
			validation.Quality.TranscriptSuggestions = detectInterviewTermSuggestions(question, transcript)
			if len(validation.Quality.TranscriptSuggestions) > 0 && validation.Quality.Status == "draft_ready" {
				validation.Quality.Status = "needs_review"
				validation.Quality.Reasons = append(validation.Quality.Reasons, "检测到可能需要人工确认的技术术语转写")
			}
			if !validation.Valid {
				s.audit(r, user, "interview.voice_rejected", "interview_session", session.ID, map[string]string{
					"asset_id": req.AssetID,
					"reason":   validation.Message,
				})
				if writer != nil {
					writer.fail(validation.Message)
					return
				}
				writeInterviewValidationError(w, validation)
				return
			}
			voiceQuality = &validation.Quality
			if source == "" {
				source = inferSubmissionSource(req.Content, transcript)
			}
			if source == "text" || source == "voice_edited" {
				req.Type = "text"
			}
		}
	}
	if source == "" {
		source = "text"
	}
	submission := domain.InterviewSubmission{
		Round:           round,
		Content:         req.Content,
		Type:            req.Type,
		Source:          source,
		AssetID:         strings.TrimSpace(req.AssetID),
		AssetURL:        assetURL,
		Asset:           assetSnapshot,
		Transcript:      transcript,
		DurationSeconds: req.DurationSeconds,
		VoiceQuality:    voiceQuality,
		SubmittedAt:     time.Now(),
	}
	if decision := evaluateIrrelevantInterviewAnswer(question, req.Content, irrelevantInterviewSubmissionCount(session)); decision.Irrelevant {
		submission.QualityFlag = "irrelevant"
		evaluation := irrelevantInterviewEvaluation(round, decision)
		session.Submissions = append(session.Submissions, submission)
		session.Evaluations = append(session.Evaluations, evaluation)
		if decision.Final {
			now := time.Now()
			session.Status = "final_evaluated"
			session.FinalScore = 0
			session.FinalReport = "继续沉淀"
			session.FollowUpQuestion = ""
			session.EndedAt = &now
		} else {
			session.Status = fmt.Sprintf("follow_up_%d_presented", round)
			session.FollowUpQuestion = evaluation.FollowUpQuestion
			session.CurrentRound = round
		}
		s.store.SaveInterviewSession(session)
		s.audit(r, user, "interview.irrelevant_answer", "interview_session", session.ID, map[string]string{
			"attempt": fmt.Sprintf("%d", decision.Attempt),
			"final":   fmt.Sprintf("%t", decision.Final),
		})
		payload := map[string]interface{}{
			"evaluation":     evaluation,
			"session_status": session.Status,
			"session":        session,
		}
		if writer != nil {
			writer.stage("agent_intent", decision.Message)
			writer.deltaDisplay(decision.Message)
			writer.stage("completed", "本轮 Agent 面试完成")
			writer.finish(payload)
			return
		}
		writeOK(w, payload)
		return
	}
	if writer != nil {
		writer.stage("received", "已收到回答，正在准备评分")
	}
	interviewAgent := agentruntime.NewInterviewAgent(agentruntime.InterviewConfig{
		Feedback: func(ctx context.Context, feedbackReq ai.InterviewFeedbackRequest, _ func(string)) (ai.InterviewFeedback, ai.CallMeta, error) {
			return s.llmRouter().GenerateInterviewFeedbackStream(ctx, feedbackReq, nil)
		},
	})
	agentResult, agentErr := interviewAgent.Run(r.Context(), agentruntime.InterviewRequest{
		Session:  session,
		Question: question,
		Answer:   req.Content,
		OnStage: func(step, message string) {
			if writer != nil {
				writer.stage(step, message)
			}
		},
	})
	if agentErr != nil {
		s.auditInterviewAgentRun(r, user, session.ID, agentResult.Trace, agentResult, "failed", agentErr)
		if writer != nil {
			writer.fail("interview agent failed")
			return
		}
		writeError(w, http.StatusInternalServerError, "interview agent failed")
		return
	}
	evaluation := agentResult.Evaluation
	feedback := agentResult.Feedback
	needReport := agentResult.NeedReport
	if needReport && strings.TrimSpace(agentResult.FinalReport) != "" {
		session.FinalReport = agentResult.FinalReport
	}
	if writer != nil {
		streamInterviewFeedbackDisplay(writer, feedback, evaluation, needReport)
	}
	if writer != nil {
		writer.stage("saving", "正在整理评分结果")
	}
	session.Submissions = append(session.Submissions, submission)
	session.Evaluations = append(session.Evaluations, evaluation)
	if evaluation.FollowUpTriggered && round < session.MaxRounds {
		session.Status = fmt.Sprintf("follow_up_%d_presented", round)
		session.FollowUpQuestion = evaluation.FollowUpQuestion
		session.CurrentRound = round + 1
	} else {
		now := time.Now()
		session.Status = "final_evaluated"
		session.FinalScore = evaluation.TotalScore
		if strings.TrimSpace(session.FinalReport) == "" {
			session.FinalReport = ai.DefaultInterviewReport(evaluation)
		}
		session.EndedAt = &now
		s.store.RecordInterviewScore(user.ID, question.Domain, evaluation.TotalScore)
	}
	s.store.SaveInterviewSession(session)
	s.auditInterviewAgentRun(r, user, session.ID, agentResult.Trace, agentResult, "completed", nil)
	s.audit(r, user, "interview.submit", "interview_session", session.ID, map[string]string{"type": req.Type, "status": session.Status})
	payload := map[string]interface{}{
		"evaluation":     evaluation,
		"session_status": session.Status,
		"session":        session,
	}
	if writer != nil {
		writer.stage("completed", "本轮 Agent 面试完成")
		writer.finish(payload)
		return
	}
	writeOK(w, payload)
}

func interviewSessionAcceptsSubmission(session *domain.InterviewSession) bool {
	if session == nil {
		return false
	}
	status := strings.TrimSpace(session.Status)
	return status == "question_presented" ||
		status == "active" ||
		(strings.HasPrefix(status, "follow_up_") && strings.HasSuffix(status, "_presented"))
}

func (s *Server) handleInterviewVoice(w http.ResponseWriter, r *http.Request, user *domain.User, sessionID string) {
	var req struct {
		AssetID         string `json:"asset_id"`
		Transcript      string `json:"transcript"`
		DurationSeconds int    `json:"duration_seconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	session, ok := s.store.GetInterviewSession(sessionID)
	if !ok || session.UserID != user.ID {
		writeError(w, http.StatusNotFound, "interview session not found")
		return
	}
	asset, ok := s.store.GetAsset(req.AssetID)
	if !ok || asset.UserID != user.ID {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	normalizedAsset := normalizeAssetURLs(*asset)
	if err := validateVoiceAsset(asset.Filename, asset.MimeType, asset.Size); err != nil {
		writeAssetValidationError(w, err)
		return
	}
	question, ok := s.store.GetInterviewQuestion(session.QuestionID)
	if !ok {
		writeError(w, http.StatusNotFound, "interview question not found")
		return
	}
	sttResult, err := s.stt.Transcribe(r.Context(), STTRequest{
		Asset:    asset,
		Session:  session,
		Seed:     req.Transcript,
		Language: interviewSTTLanguageHint(question),
		Prompt:   buildInterviewSTTPrompt(question),
	})
	if err != nil {
		s.audit(r, user, "interview.voice_transcript_failed", "interview_session", session.ID, map[string]string{
			"asset_id": asset.ID,
			"error":    truncateText(err.Error(), 240),
		})
		writeSTTError(w, err)
		return
	}
	durationSeconds := req.DurationSeconds
	if durationSeconds == 0 {
		durationSeconds = sttResult.DurationSeconds
	}
	validation := validateInterviewAnswer(question, sttResult.Transcript, sttResult.Transcript, asset, sttResult.Confidence)
	if sttResult.DetectedLanguage != "" {
		validation.Quality.DetectedLanguage = sttResult.DetectedLanguage
	}
	if sttResult.Confidence > 0 {
		validation.Quality.STTConfidence = sttResult.Confidence
	}
	validation.Quality.TranscriptSuggestions = detectInterviewTermSuggestions(question, sttResult.Transcript)
	if len(validation.Quality.TranscriptSuggestions) > 0 && validation.Quality.Status == "draft_ready" {
		validation.Quality.Status = "needs_review"
		validation.Quality.Reasons = append(validation.Quality.Reasons, "检测到可能需要人工确认的技术术语转写")
	}
	if !validation.Valid {
		s.audit(r, user, "interview.voice_rejected", "interview_session", session.ID, map[string]string{
			"asset_id": asset.ID,
			"reason":   validation.Message,
			"stage":    "transcribe",
		})
	}
	status := validation.Quality.Status
	if status == "" {
		status = "draft_ready"
	}
	s.audit(r, user, "interview.voice_transcript", "interview_session", session.ID, map[string]string{"asset_id": asset.ID, "status": status})
	writeOK(w, map[string]interface{}{
		"asset":            normalizedAsset,
		"transcript":       sttResult.Transcript,
		"duration_seconds": durationSeconds,
		"status":           status,
		"quality":          validation.Quality,
	})
}

func (s *Server) handleCommunity(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	if suffix == "/posts" && r.Method == http.MethodGet {
		query := r.URL.Query()
		writeOK(w, map[string]interface{}{"list": s.visibleCommunityPosts(user, query.Get("status"), query.Get("view"))})
		return
	}
	if suffix == "/posts" && r.Method == http.MethodPost {
		if !s.allowAI(w, r, user, "community-structure", 20) {
			return
		}
		var req struct {
			Title      string   `json:"title"`
			RawContent string   `json:"raw_content"`
			Domain     string   `json:"domain"`
			Tags       []string `json:"tags"`
		}
		if !decode(w, r, &req) {
			return
		}
		var writer *sseWriter
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			writer.stage("received", "case received")
		}
		structureReq := ai.CommunityStructureRequest{
			Title:      req.Title,
			RawContent: req.RawContent,
			Domain:     req.Domain,
			Tags:       req.Tags,
		}
		var structured domain.ScenarioContent
		var err error
		if writer != nil {
			writer.stage("llm", "generating structured preview")
			structured, _, err = s.llmRouter().StructureCommunityPostStream(r.Context(), structureReq, nil)
		} else {
			structured, _, err = s.llmRouter().StructureCommunityPost(r.Context(), structureReq)
		}
		if err != nil {
			if writer != nil {
				writer.fail("AI structure preview failed, please retry")
				return
			}
			writeError(w, http.StatusBadGateway, "AI structure preview failed, please retry")
			return
		}
		if writer != nil {
			writer.stage("schema_validated", "structured preview schema validated")
			writer.stage("rule_sensitive_check", "running rule sensitive check")
			writer.stage("model_sensitive_check", "running model sensitive check")
		}
		check := s.sensitiveCheck(r, user, "raw_content", strings.Join([]string{req.Title, req.RawContent, strings.Join(req.Tags, " ")}, "\n"))
		structured = sanitizeScenarioContent(structured)
		if writer != nil {
			if check.FallbackUsed {
				writer.stage("fallback_sensitive_check", "rule fallback sensitive check used")
			}
			writer.stage("sanitized", "sensitive fields sanitized")
			writer.stage("saving", "saving community preview")
		}
		post := s.store.AddCommunityPost(domain.CommunityPost{
			UserID:              user.ID,
			Title:               ai.Sanitize(req.Title),
			RawContent:          ai.Sanitize(req.RawContent),
			Domain:              req.Domain,
			Tags:                req.Tags,
			AIStructuredContent: structured,
			ReviewHistory:       []domain.ReviewHistoryItem{},
			SensitiveCheck:      check,
			Status:              "pending_review",
		})
		post = s.refreshCommunityModerationSummary(post, "instructor_review")
		s.audit(r, user, "community.create", "community_post", post.ID, map[string]string{"status": post.Status})
		if writer != nil {
			writer.stage("completed", "structured preview completed")
			writer.finish(s.communityPostView(user, &post))
			return
		}
		writeOK(w, s.communityPostView(user, &post))
		return
	}

	parts := split(suffix)
	if len(parts) == 2 && parts[0] == "posts" && r.Method == http.MethodGet {
		post, ok := s.store.GetCommunityPost(parts[1])
		if !ok || !canViewCommunityPost(user, post) {
			writeError(w, http.StatusNotFound, "community post not found")
			return
		}
		writeOK(w, s.communityPostView(user, post))
		return
	}
	if len(parts) == 2 && parts[0] == "posts" && r.Method == http.MethodDelete {
		s.handleCommunityPostDelete(w, r, user, parts[1])
		return
	}
	if len(parts) == 2 && parts[0] == "posts" && r.Method == http.MethodPut {
		s.handleCommunityPostDraftUpdate(w, r, user, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "submit" && r.Method == http.MethodPost {
		s.handleCommunityPostSubmit(w, r, user, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "instructor-review" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleInstructorReview(w, r, user, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "final-review" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleFinalReview(w, r, user, parts[1])
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleCommunityPostDraftUpdate(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	var req struct {
		Title             string                  `json:"title"`
		RawContent        string                  `json:"raw_content"`
		Domain            string                  `json:"domain"`
		Tags              []string                `json:"tags"`
		StructuredContent *domain.ScenarioContent `json:"structured_content"`
	}
	if !decode(w, r, &req) {
		return
	}
	post, ok := s.store.GetCommunityPost(postID)
	if !ok || post.UserID != user.ID {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "draft" && post.Status != "pending_review" {
		writeError(w, http.StatusBadRequest, "post is not editable")
		return
	}
	checkTitle := post.Title
	checkRawContent := post.RawContent
	checkTags := append([]string{}, post.Tags...)
	if strings.TrimSpace(req.Title) != "" {
		checkTitle = req.Title
		post.Title = ai.Sanitize(req.Title)
	}
	if strings.TrimSpace(req.RawContent) != "" {
		checkRawContent = req.RawContent
		post.RawContent = ai.Sanitize(req.RawContent)
	}
	if strings.TrimSpace(req.Domain) != "" {
		post.Domain = strings.TrimSpace(req.Domain)
	}
	if req.Tags != nil {
		checkTags = append([]string{}, req.Tags...)
		post.Tags = req.Tags
	}
	if req.StructuredContent != nil {
		edited := normalizeScenarioContent(sanitizeScenarioContent(*req.StructuredContent), post.AIStructuredContent)
		post.EditedStructuredContent = &edited
	}
	post.SensitiveCheck = s.sensitiveCheck(r, user, "community_post", strings.Join([]string{checkTitle, checkRawContent, strings.Join(checkTags, " ")}, "\n"))
	if post.Status == "pending_review" {
		post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "author_update", "pending_review", "pending_review", "作者更新待审草稿", post.EditedStructuredContent))
	}
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "instructor_review")
	s.audit(r, user, "community.draft_update", "community_post", updated.ID, map[string]string{"status": updated.Status})
	writeOK(w, s.communityPostView(user, &updated))
}

func (s *Server) handleCommunityPostSubmit(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	post, ok := s.store.GetCommunityPost(postID)
	if !ok || post.UserID != user.ID {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "draft" {
		writeError(w, http.StatusBadRequest, "post is not a draft")
		return
	}
	if !validCommunityScenarioContent(effectiveCommunityContent(post)) {
		writeError(w, http.StatusBadRequest, "structured content is incomplete")
		return
	}
	fromStatus := post.Status
	post.Status = "pending_review"
	post.SensitiveCheck = s.sensitiveCheck(r, user, "community_post", strings.Join([]string{post.Title, post.RawContent, strings.Join(post.Tags, " ")}, "\n"))
	post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "author_submit", fromStatus, post.Status, "提交讲师初审", effectiveCommunityContent(post)))
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "instructor_review")
	s.audit(r, user, "community.submit", "community_post", updated.ID, map[string]string{"status": updated.Status})
	writeOK(w, s.communityPostView(user, &updated))
}

func (s *Server) handleCommunityPostDelete(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	post, ok := s.store.GetCommunityPost(postID)
	if !ok {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if !canDeleteCommunityPost(user, post) {
		writeError(w, http.StatusForbidden, "post cannot be deleted")
		return
	}
	if user.Role == domain.RoleAdmin && post.Status == "published" && strings.TrimSpace(post.ConvertedQuestionID) != "" {
		if scenario, ok := s.store.GetScenario(post.ConvertedQuestionID); ok {
			scenario.Status = "archived"
			s.store.AddScenario(*scenario)
		}
	}
	if !s.store.DeleteCommunityPost(post.ID) {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	s.audit(r, user, "community.delete", "community_post", post.ID, map[string]string{"status": post.Status})
	writeOK(w, map[string]interface{}{"deleted": true, "id": post.ID})
}

func (s *Server) handleInstructorReview(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	if !hasAnyRole(user, domain.RoleInstructor, domain.RoleAdmin) {
		writeError(w, http.StatusForbidden, "instructor role required")
		return
	}
	var req struct {
		Decision          string                  `json:"decision"`
		Note              string                  `json:"note"`
		StructuredContent *domain.ScenarioContent `json:"structured_content"`
	}
	if !decode(w, r, &req) {
		return
	}
	decision := strings.TrimSpace(req.Decision)
	if decision != "approve" && decision != "reject" {
		writeError(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	post, ok := s.store.GetCommunityPost(postID)
	if !ok {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "pending_review" {
		writeError(w, http.StatusBadRequest, "post is not pending instructor review")
		return
	}
	now := time.Now()
	fromStatus := post.Status
	post.ReviewedBy = user.ID
	post.ReviewedAt = &now
	post.ReviewNote = strings.TrimSpace(req.Note)
	if decision == "approve" {
		post.Status = "instructor_approved"
		if req.StructuredContent != nil {
			edited := normalizeScenarioContent(sanitizeScenarioContent(*req.StructuredContent), post.AIStructuredContent)
			post.EditedStructuredContent = &edited
		}
	} else {
		post.Status = "instructor_rejected"
	}
	post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "instructor_"+decision, fromStatus, post.Status, post.ReviewNote, post.EditedStructuredContent))
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "final_review")
	s.audit(r, user, "community.instructor_review", "community_post", post.ID, map[string]string{"decision": decision, "status": post.Status})
	writeOK(w, s.communityPostView(user, &updated))
}

func (s *Server) handleFinalReview(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	if !hasAnyRole(user, domain.RoleAdmin) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	decision := strings.TrimSpace(req.Decision)
	if decision != "publish" && decision != "reject" {
		writeError(w, http.StatusBadRequest, "decision must be publish or reject")
		return
	}
	post, ok := s.store.GetCommunityPost(postID)
	if !ok {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "instructor_approved" {
		writeError(w, http.StatusBadRequest, "post is not pending final review")
		return
	}
	now := time.Now()
	fromStatus := post.Status
	post.FinalizedBy = user.ID
	post.FinalizedAt = &now
	post.FinalNote = strings.TrimSpace(req.Note)
	if decision == "reject" {
		post.Status = "pending_review"
		post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "final_reject", fromStatus, post.Status, post.FinalNote, nil))
		updated := s.store.SaveCommunityPost(post)
		updated = s.refreshCommunityModerationSummary(updated, "instructor_review")
		s.audit(r, user, "community.final_review", "community_post", post.ID, map[string]string{"decision": decision, "status": post.Status})
		writeOK(w, map[string]interface{}{"post": s.communityPostView(user, &updated)})
		return
	}

	content := effectiveCommunityContent(post)
	if !validCommunityScenarioContent(content) {
		writeError(w, http.StatusBadRequest, "structured content is incomplete")
		return
	}
	scenario := s.scenarioFromCommunityPost(post, user.ID)
	created := s.store.AddScenario(scenario)
	post.Status = "published"
	post.ConvertedQuestionID = created.ID
	post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "final_publish", fromStatus, post.Status, post.FinalNote, effectiveCommunityContent(post)))
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "final_review")
	s.audit(r, user, "community.final_review", "community_post", post.ID, map[string]string{"decision": decision, "status": post.Status, "scenario_id": created.ID})
	writeOK(w, map[string]interface{}{
		"post":     s.communityPostView(user, &updated),
		"question": scenarioView(&created, user),
	})
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	if !hasAnyRole(user, domain.RoleAdmin) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	parts := split(suffix)
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
	return map[string]interface{}{
		"generated_at": time.Now(),
		"services": []map[string]interface{}{
			{"name": "API", "status": "ok", "detail": "HTTP router is serving /healthz and /api/v1"},
			{"name": "Database", "status": storeStatus, "detail": storeDetail},
			{"name": "Redis", "status": redisStatus, "detail": redisDetail(redisStatus)},
			{"name": "AI Provider", "status": aiStatus, "detail": aiProviderStatusDetail(aiInfo)},
			{"name": "Sensitive Detection", "status": sensitiveStatus["status"], "detail": sensitiveStatus["detail"]},
			{"name": "Seed Data", "status": seedDataStatus(seedScenarios), "detail": fmt.Sprintf("%d seed scenarios, %d active scenarios", seedScenarios, activeScenarios)},
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
		"counts": map[string]int{
			"users":               len(users),
			"scenarios":           len(scenarios),
			"active_scenarios":    activeScenarios,
			"generated_scenarios": generatedScenarios,
			"ai_jobs":             aiJobsCount,
			"community_posts":     communityPostCount(communityPosts),
			"pending_ugc":         pendingUGC,
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

func normalizeScenarioGenerationPayload(req scenarioGenerationPayload) (scenarioGenerationPayload, error) {
	req.Domain = firstNonEmpty(strings.TrimSpace(req.Domain), "database")
	req.Difficulty = firstNonEmpty(strings.TrimSpace(req.Difficulty), "L2")
	req.ScenarioType = firstNonEmpty(strings.TrimSpace(req.ScenarioType), "troubleshooting")
	req.Tags = normalizeScenarioGenerationTags(req.Tags, req.Domain)
	req.Constraints = normalizeScenarioGenerationConstraints(req.Constraints)
	if !allowedScenarioDifficulties[req.Difficulty] {
		return scenarioGenerationPayload{}, fmt.Errorf("difficulty must be one of L1, L2, L3, L4, L5")
	}
	if !allowedScenarioTypes[req.ScenarioType] {
		return scenarioGenerationPayload{}, fmt.Errorf("scenario_type must be one of troubleshooting, design, performance")
	}
	return req, nil
}

func normalizeScenarioGenerationConstraints(input *scenarioGenerationConstraintsPayload) *scenarioGenerationConstraintsPayload {
	if input == nil {
		return nil
	}
	normalized := &scenarioGenerationConstraintsPayload{
		Title:         strings.TrimSpace(input.Title),
		Description:   strings.TrimSpace(input.Description),
		TopicScope:    normalizeScenarioGenerationHints(input.TopicScope, 6),
		RootCauseHint: strings.TrimSpace(input.RootCauseHint),
		EvidenceHints: normalizeScenarioGenerationHints(input.EvidenceHints, 6),
		ClueHints:     normalizeScenarioGenerationHints(input.ClueHints, 6),
	}
	if normalized.Title == "" &&
		normalized.Description == "" &&
		len(normalized.TopicScope) == 0 &&
		normalized.RootCauseHint == "" &&
		len(normalized.EvidenceHints) == 0 &&
		len(normalized.ClueHints) == 0 {
		return nil
	}
	return normalized
}

func normalizeScenarioGenerationHints(values []string, limit int) []string {
	items := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[strings.ToLower(trimmed)] {
			continue
		}
		seen[strings.ToLower(trimmed)] = true
		items = append(items, trimmed)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items
}

func (req scenarioGenerationPayload) toAIConstraints() ai.ScenarioGenerationConstraints {
	if req.Constraints == nil {
		return ai.ScenarioGenerationConstraints{}
	}
	return ai.ScenarioGenerationConstraints{
		Title:         req.Constraints.Title,
		Description:   req.Constraints.Description,
		TopicScope:    append([]string{}, req.Constraints.TopicScope...),
		RootCauseHint: req.Constraints.RootCauseHint,
		EvidenceHints: append([]string{}, req.Constraints.EvidenceHints...),
		ClueHints:     append([]string{}, req.Constraints.ClueHints...),
	}
}

func (s *Server) validateScenarioGenerationRequest(r *http.Request, user *domain.User, req scenarioGenerationPayload) error {
	if req.Constraints == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "title", value: req.Constraints.Title},
		{name: "description", value: req.Constraints.Description},
		{name: "root_cause_hint", value: req.Constraints.RootCauseHint},
	} {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, field.name, field.value); err != nil {
			return err
		}
	}
	for _, item := range req.Constraints.TopicScope {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, "topic_scope", item); err != nil {
			return err
		}
	}
	for _, item := range req.Constraints.EvidenceHints {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, "evidence_hints", item); err != nil {
			return err
		}
	}
	for _, item := range req.Constraints.ClueHints {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, "clue_hints", item); err != nil {
			return err
		}
	}
	if err := s.ensureScenarioGenerationNotDuplicate(req); err != nil {
		return err
	}
	return nil
}

func (s *Server) ensureScenarioGenerationConstraintSafe(r *http.Request, user *domain.User, field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	check := s.sensitiveCheck(r, user, field, value)
	if check.Blocked || check.Status == "risk" {
		return scenarioGenerationValidationError{
			status:  http.StatusBadRequest,
			message: fmt.Sprintf("sensitive content is not allowed in %s", field),
		}
	}
	return nil
}

func (s *Server) ensureScenarioGenerationNotDuplicate(req scenarioGenerationPayload) error {
	title := ""
	if req.Constraints != nil {
		title = strings.TrimSpace(req.Constraints.Title)
	}
	if title == "" {
		return nil
	}
	items := s.store.ListScenarios(req.Domain, req.Difficulty, "")
	for _, item := range items {
		if item.Status != "active" || item.ScenarioType != req.ScenarioType {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.Title), title) || ai.Similarity(item.Title, title) >= 0.92 {
			return scenarioGenerationValidationError{
				status:  http.StatusConflict,
				message: "duplicate scenario title detected",
			}
		}
	}
	return nil
}

func writeScenarioGenerationValidationError(w http.ResponseWriter, err error) {
	var validationErr scenarioGenerationValidationError
	if errors.As(err, &validationErr) {
		writeError(w, validationErr.status, validationErr.message)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func normalizeScenarioGenerationTags(tags []string, domainName string) []string {
	cleaned := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		value := strings.TrimSpace(tag)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	if len(cleaned) == 0 {
		return []string{"AI生成", domainName}
	}
	return cleaned
}

func (s *Server) createScenarioGenerationJob(userID string, req scenarioGenerationPayload) (domain.AIJob, error) {
	now := time.Now()
	planned := s.llmRouter().PlannedProviderInfo(ai.RouterTaskScenarioGenerate)
	job, err := s.store.CreateAIJob(domain.AIJob{
		UserID:    userID,
		Kind:      domain.AIJobKindScenarioGeneration,
		Status:    domain.AIJobStatusQueued,
		Stage:     "queued",
		Progress:  5,
		Provider:  planned.Provider,
		Model:     planned.Model,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return domain.AIJob{}, err
	}
	go s.runScenarioGenerationJob(job.ID, userID, req)
	return job, nil
}

func (s *Server) runScenarioGenerationJob(jobID, userID string, req scenarioGenerationPayload) {
	job, ok := s.store.GetAIJob(jobID)
	if !ok {
		return
	}
	if job.Status == domain.AIJobStatusCanceled {
		return
	}
	startedAt := time.Now()
	planned := s.llmRouter().PlannedProviderInfo(ai.RouterTaskScenarioGenerate)
	job.Status = domain.AIJobStatusRunning
	job.Stage = "calling_model"
	job.Progress = 30
	if strings.TrimSpace(job.Provider) == "" {
		job.Provider = planned.Provider
	}
	if strings.TrimSpace(job.Model) == "" {
		job.Model = planned.Model
	}
	job.StartedAt = &startedAt
	if _, err := s.store.SaveAIJob(job); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	s.registerAIJobCancel(jobID, cancel)
	defer s.unregisterAIJobCancel(jobID)
	defer cancel()
	if canceled := s.loadCancelableAIJob(jobID); canceled != nil && canceled.Status == domain.AIJobStatusCanceled {
		return
	}
	question, llmMeta, err := s.llmRouter().GenerateScenario(ctx, ai.ScenarioGenerationRequest{
		Domain:       req.Domain,
		Difficulty:   req.Difficulty,
		ScenarioType: req.ScenarioType,
		Tags:         req.Tags,
		Constraints:  req.toAIConstraints(),
		UserID:       userID,
		Nonce:        fmt.Sprintf("%d", time.Now().UnixNano()),
	})
	if err != nil {
		if ctx.Err() == context.Canceled {
			if canceled := s.loadCancelableAIJob(jobID); canceled != nil && canceled.Status == domain.AIJobStatusCanceled {
				return
			}
		}
		completedAt := time.Now()
		job.Status = domain.AIJobStatusFailed
		job.Stage = scenarioGenerationFailureStage(llmMeta)
		job.Progress = 100
		job.ErrorMessage = scenarioGenerationErrorMessage(err, llmMeta)
		if strings.TrimSpace(llmMeta.Provider) != "" {
			job.Provider = llmMeta.Provider
		}
		if strings.TrimSpace(llmMeta.Model) != "" {
			job.Model = llmMeta.Model
		}
		job.Validated = llmMeta.Validated
		job.FallbackUsed = llmMeta.FallbackUsed
		job.CompletedAt = &completedAt
		_, _ = s.store.SaveAIJob(job)
		s.auditScenarioGenerationFailed(userID, jobID, *job, llmMeta, req, err)
		return
	}

	job.Stage = "validating_output"
	job.Progress = 75
	job.Provider = llmMeta.Provider
	job.Model = llmMeta.Model
	job.Validated = llmMeta.Validated
	job.FallbackUsed = llmMeta.FallbackUsed
	if _, err := s.store.SaveAIJob(job); err != nil {
		return
	}

	created := s.store.AddScenario(question)
	completedAt := time.Now()
	job.Status = domain.AIJobStatusCompleted
	job.Stage = "completed"
	job.Progress = 100
	job.ResultQuestionID = created.ID
	job.CompletedAt = &completedAt
	_, _ = s.store.SaveAIJob(job)
	s.auditScenarioGenerationCompleted(userID, jobID, created, llmMeta, req)
}

func (s *Server) cancelAIJob(job *domain.AIJob) (*domain.AIJob, error) {
	if job == nil {
		return nil, errors.New("ai job not found")
	}
	switch job.Status {
	case domain.AIJobStatusCompleted, domain.AIJobStatusFailed:
		return nil, errors.New("ai job already finished")
	case domain.AIJobStatusCanceled:
		return job, nil
	}
	completedAt := time.Now()
	job.Status = domain.AIJobStatusCanceled
	job.Stage = "canceled"
	job.Progress = 100
	job.CompletedAt = &completedAt
	job.ErrorMessage = ""
	saved, err := s.store.SaveAIJob(job)
	if err != nil {
		return nil, err
	}
	s.triggerAIJobCancel(job.ID)
	return &saved, nil
}

func scenarioGenerationErrorMessage(err error, meta ai.CallMeta) string {
	if meta.ErrorType == "timeout" || errors.Is(err, context.DeadlineExceeded) {
		return "模型响应超时，请稍后重试或检查当前模型服务状态。"
	}
	if meta.ErrorType == "auth" {
		return "模型鉴权失败，请检查 API Key 或模型服务配置。"
	}
	if meta.ErrorType == "rate_limit" {
		return "模型服务限流，请稍后重试。"
	}
	if meta.ErrorType == "validation" {
		return "模型返回结构未通过校验，请重新生成题目。"
	}
	return "题目生成失败，请稍后重试。"
}

func scenarioGenerationFailureStage(meta ai.CallMeta) string {
	if meta.ErrorType == "validation" || meta.ErrorType == "safety_blocked" {
		return "validating_output"
	}
	return "calling_model"
}

func (s *Server) auditScenarioGenerationFailed(userID, jobID string, job domain.AIJob, meta ai.CallMeta, req scenarioGenerationPayload, err error) {
	metadata := map[string]string{
		"job_id":        jobID,
		"provider":      firstNonEmpty(strings.TrimSpace(meta.Provider), strings.TrimSpace(job.Provider), "unknown"),
		"model":         firstNonEmpty(strings.TrimSpace(meta.Model), strings.TrimSpace(job.Model), "unknown"),
		"stage":         firstNonEmpty(strings.TrimSpace(job.Stage), scenarioGenerationFailureStage(meta)),
		"error_type":    firstNonEmpty(strings.TrimSpace(meta.ErrorType), "unknown"),
		"error_summary": truncateText(ai.Sanitize(err.Error()), 160),
		"difficulty":    req.Difficulty,
		"domain":        req.Domain,
		"scenario_type": req.ScenarioType,
		"store_mode":    s.storeMode(),
	}
	if raw := strings.TrimSpace(meta.RawOutput); raw != "" {
		metadata["raw_output_preview"] = truncateText(ai.Sanitize(raw), 500)
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      userID,
		Action:       "scenario.generate.failed",
		ResourceType: "ai_job",
		ResourceID:   jobID,
		Metadata:     metadata,
	})
}

func (s *Server) loadCancelableAIJob(jobID string) *domain.AIJob {
	job, ok := s.store.GetAIJob(jobID)
	if !ok {
		return nil
	}
	return job
}

func (s *Server) registerAIJobCancel(jobID string, cancel context.CancelFunc) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	s.jobStop[jobID] = cancel
}

func (s *Server) unregisterAIJobCancel(jobID string) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	delete(s.jobStop, jobID)
}

func (s *Server) triggerAIJobCancel(jobID string) {
	s.jobMu.Lock()
	cancel := s.jobStop[jobID]
	s.jobMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) auditScenarioGenerationCompleted(userID, jobID string, question domain.ScenarioQuestion, meta ai.CallMeta, req scenarioGenerationPayload) {
	metadata := map[string]string{
		"provider":      meta.Provider,
		"model":         meta.Model,
		"validated":     strconv.FormatBool(meta.Validated),
		"fallback_used": strconv.FormatBool(meta.FallbackUsed),
		"difficulty":    question.Difficulty,
		"domain":        question.Domain,
		"scenario_type": question.ScenarioType,
		"store_mode":    s.storeMode(),
		"creator_role":  scenarioGenerationActorRole(userID, s.store),
	}
	fields := req.toAIConstraints().ActiveFields()
	metadata["has_constraints"] = strconv.FormatBool(len(fields) > 0)
	metadata["constraint_fields"] = strings.Join(fields, ",")
	metadata["duplicate_blocked"] = "false"
	if strings.TrimSpace(jobID) != "" {
		metadata["job_id"] = jobID
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      userID,
		Action:       "scenario.generate.completed",
		ResourceType: "scenario_question",
		ResourceID:   question.ID,
		Metadata:     metadata,
	})
}

func (s *Server) storeMode() string {
	if _, ok := s.store.(interface{ Ping(context.Context) error }); ok {
		return "postgres"
	}
	return "memory"
}

func scenarioGenerationActorRole(userID string, dataStore store.Store) string {
	if dataStore == nil || strings.TrimSpace(userID) == "" {
		return ""
	}
	user, ok := dataStore.GetUser(userID)
	if !ok || user == nil {
		return ""
	}
	return user.Role
}

func (s *Server) aiJobPayload(job *domain.AIJob, user *domain.User) map[string]interface{} {
	payload := map[string]interface{}{"job": job}
	if job != nil && job.ResultQuestionID != "" {
		if question, ok := s.store.GetScenario(job.ResultQuestionID); ok {
			payload["question_id"] = question.ID
			payload["question"] = scenarioView(question, user)
		}
	}
	return payload
}

func (s *Server) writeAIJobEvents(w http.ResponseWriter, r *http.Request, user *domain.User, jobID string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(job *domain.AIJob) bool {
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", mustJSON(s.aiJobPayload(job, user)))
		if flusher != nil {
			flusher.Flush()
		}
		return job.Status == domain.AIJobStatusCompleted || job.Status == domain.AIJobStatusFailed
	}

	if job, ok := s.store.GetAIJob(jobID); ok {
		if send(job) {
			return
		}
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
			job, ok := s.store.GetAIJob(jobID)
			if !ok || !canViewAIJob(job, user) {
				return
			}
			if send(job) {
				return
			}
		}
	}
}

func (s *Server) processScenarioMessage(ctx context.Context, user *domain.User, sessionID, content string, callbacks ...interface{}) (domain.ScenarioMessage, *domain.ScenarioSession, error) {
	if content == "" {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("content is required")
	}
	session, ok := s.store.GetScenarioSession(sessionID)
	if !ok || session.UserID != user.ID {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session not found")
	}
	if s.expireScenarioSessionIfIdle(session) {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session is abandoned")
	}
	if session.Status != "active" {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("session is not active")
	}
	if session.CurrentTurn >= session.MaxTurns {
		return domain.ScenarioMessage{}, nil, fmt.Errorf("max turns reached, please submit an answer")
	}

	question := &session.QuestionSnapshot
	existingMessages := s.store.ListScenarioMessages(sessionID)
	request, onStage, onDelta := scenarioMessageCallbacks(callbacks...)
	agent := agentruntime.NewDiagnosticAgent(agentruntime.DiagnosticConfig{
		Rewrite: func(ctx context.Context, req ai.ScenarioReplyRequest, delta func(string)) (string, ai.CallMeta, error) {
			if delta != nil {
				return s.llmRouter().RewriteScenarioReplyStream(ctx, req, delta)
			}
			return s.llmRouter().RewriteScenarioReply(ctx, req)
		},
		SemanticGate: agentruntime.NewSemanticGate(agentruntime.SemanticGateConfig{Embedding: s.embedding}),
	})
	result, err := agent.Run(ctx, agentruntime.DiagnosticRequest{
		Session:        session,
		Question:       question,
		UserMessage:    content,
		Messages:       existingMessages,
		RecentMessages: recentScenarioContext(existingMessages, 5),
		SummaryBuilder: buildScenarioConversationSummary,
		OnStage:        onStage,
		OnDelta:        onDelta,
	})
	if err != nil {
		s.auditDiagnosticAgentRun(request, user, session.ID, result.Trace, result.Meta, "failed", err)
		return domain.ScenarioMessage{}, nil, err
	}
	session.CurrentTurn++
	session.LastActiveAt = time.Now()
	if session.CurrentTurn >= session.MaxTurns {
		result.AssistantContent += " 当前会话已达到最大轮次，请提交最终根因答案。"
	}
	message := s.store.AddScenarioMessage(domain.ScenarioMessage{
		SessionID:        session.ID,
		TurnNumber:       session.CurrentTurn,
		Role:             "assistant",
		UserContent:      content,
		AssistantContent: result.AssistantContent,
		ResponseMeta:     result.Meta,
	})
	s.store.SaveScenarioSession(session)
	s.auditDiagnosticAgentRun(request, user, session.ID, result.Trace, result.Meta, "completed", nil)
	return message, session, nil
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

func (s *Server) auditDiagnosticAgentRun(request *http.Request, user *domain.User, sessionID string, trace domain.AgentTrace, meta domain.ResponseMeta, status string, runErr error) {
	errorSummary := ""
	if runErr != nil {
		errorSummary = "agent run failed"
	}
	s.auditAgentRun(request, user, agentAuditPayload{
		Agent:           firstNonEmpty(strings.TrimSpace(trace.Agent), "diagnostic_agent"),
		Action:          "agent.diagnostic_run",
		ResourceType:    "scenario_session",
		ResourceID:      sessionID,
		Status:          status,
		ToolCount:       trace.ToolCount,
		FallbackUsed:    meta.FallbackUsed,
		SafetyRewritten: meta.SafetyRewritten,
		ErrorSummary:    errorSummary,
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

func scenarioMessageCallbacks(callbacks ...interface{}) (*http.Request, func(string, string), func(string)) {
	var request *http.Request
	var onStage func(string, string)
	var onDelta func(string)
	for _, callback := range callbacks {
		switch value := callback.(type) {
		case *http.Request:
			request = value
		case func(string, string):
			onStage = value
		case func(string):
			onDelta = value
		}
	}
	return request, onStage, onDelta
}

func buildScenarioConversationSummary(existing string, question *domain.ScenarioQuestion, messages []domain.ScenarioMessage) string {
	if len(messages) == 0 {
		return strings.TrimSpace(existing)
	}
	limit := len(messages) - 5
	if limit < 0 {
		limit = len(messages)
	}
	if limit == 0 {
		return strings.TrimSpace(existing)
	}
	older := messages[:limit]
	revealed := []string{}
	userFocus := []string{}
	for _, message := range older {
		if message.ResponseMeta.RevealedClueID != "" {
			revealed = append(revealed, message.ResponseMeta.RevealedClueID)
		}
		if strings.TrimSpace(message.UserContent) != "" {
			userFocus = append(userFocus, truncateText(message.UserContent, 80))
		}
	}
	var builder strings.Builder
	if strings.TrimSpace(existing) != "" {
		builder.WriteString(strings.TrimSpace(existing))
		builder.WriteString("\n")
	}
	if question != nil {
		builder.WriteString("题目：")
		builder.WriteString(question.Title)
		builder.WriteString("。")
	}
	fmt.Fprintf(&builder, "已压缩前 %d 轮对话。", limit)
	if len(revealed) > 0 {
		builder.WriteString("已释放线索ID：")
		builder.WriteString(strings.Join(uniqueStrings(revealed), ","))
		builder.WriteString("。")
	}
	if len(userFocus) > 0 {
		if len(userFocus) > 8 {
			userFocus = userFocus[len(userFocus)-8:]
		}
		builder.WriteString("用户主要追问：")
		builder.WriteString(strings.Join(userFocus, " / "))
		builder.WriteString("。")
	}
	return truncateText(strings.TrimSpace(builder.String()), 1800)
}

func recentScenarioContext(messages []domain.ScenarioMessage, limit int) []ai.ScenarioContextMessage {
	if limit <= 0 || len(messages) == 0 {
		return []ai.ScenarioContextMessage{}
	}
	start := len(messages) - limit
	if start < 0 {
		start = 0
	}
	out := make([]ai.ScenarioContextMessage, 0, len(messages[start:]))
	for _, message := range messages[start:] {
		out = append(out, ai.ScenarioContextMessage{
			TurnNumber:       message.TurnNumber,
			UserContent:      message.UserContent,
			AssistantContent: truncateText(message.AssistantContent, 240),
		})
	}
	return out
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

func (s *Server) evaluateScenarioAnswer(user *domain.User, sessionID, answer string) (*domain.ScenarioSession, error) {
	session, ok := s.store.GetScenarioSession(sessionID)
	if !ok || session.UserID != user.ID {
		return nil, fmt.Errorf("session not found")
	}
	if s.expireScenarioSessionIfIdle(session) {
		return nil, fmt.Errorf("session is abandoned")
	}
	if session.Status != "active" {
		return nil, fmt.Errorf("session is not active")
	}
	question := &session.QuestionSnapshot
	messages := s.store.ListScenarioMessages(sessionID)
	var vectorStore store.VectorStore
	if provider, ok := s.store.(store.VectorStoreProvider); ok {
		vectorStore = provider.VectorStore()
	}
	score, report, missing := scoreScenarioWithEvidenceChain(scenarioScoringInput{
		Question:    question,
		Messages:    messages,
		Answer:      answer,
		RevealedIDs: session.RevealedClueIDs,
		CurrentTurn: session.CurrentTurn,
		VectorStore: vectorStore,
	})
	evaluation := &domain.ScenarioEvaluation{
		IsCorrect:         score.Accuracy >= 70,
		MatchDegree:       score.Accuracy,
		MissingPoints:     missing,
		StandardProcedure: question.Content.StandardProcedure,
		ScoringReport:     report,
	}
	now := time.Now()
	session.UserAnswer = answer
	session.EvaluationResult = evaluation
	session.Score = score
	session.Status = "evaluated"
	session.EndedAt = &now
	s.store.SaveScenarioSession(session)
	s.store.RecordScenarioScore(user.ID, question.Domain, score.Total)
	return session, nil
}

func (s *Server) expireScenarioSessionIfIdle(session *domain.ScenarioSession) bool {
	if session == nil || session.Status != "active" {
		return false
	}
	if time.Since(session.LastActiveAt) <= 30*time.Minute {
		return false
	}
	now := time.Now()
	session.Status = "abandoned"
	session.EndedAt = &now
	s.store.SaveScenarioSession(session)
	return true
}

func evaluateInterview(question *domain.InterviewQuestion, answer string, round, maxRounds int) domain.InterviewEvaluation {
	return agentruntime.EvaluateInterview(question, answer, round, maxRounds)
}

func (s *Server) withUser(w http.ResponseWriter, r *http.Request, next func(*domain.User)) {
	token, err := auth.BearerToken(r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	claims, err := s.auth.Validate(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	user, ok := s.store.GetUser(claims.Subject)
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}
	if !s.allow(w, r, "user:"+user.ID, 120) {
		return
	}
	next(user)
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

type learningScoreEntry struct {
	Score      int
	At         time.Time
	QuestionID string
}

type learningDomainStats struct {
	Entries              []learningScoreEntry
	CompletedQuestionIDs map[string]bool
}

func (s *Server) learningPlan(user *domain.User) domain.LearningPlan {
	if user == nil {
		return domain.LearningPlan{GeneratedAt: time.Now()}
	}
	scenarioSessions := s.store.ListScenarioSessionsForUser(user.ID)
	interviewSessions := s.store.ListInterviewSessionsForUser(user.ID)
	scenarios := s.store.ListScenarios("", "", "")
	statsByDomain := map[string]*learningDomainStats{}
	completedQuestions := map[string]bool{}

	ensureStats := func(domainName string) *learningDomainStats {
		domainName = strings.TrimSpace(domainName)
		if domainName == "" {
			domainName = "general"
		}
		item := statsByDomain[domainName]
		if item == nil {
			item = &learningDomainStats{CompletedQuestionIDs: map[string]bool{}}
			statsByDomain[domainName] = item
		}
		return item
	}

	for _, session := range scenarioSessions {
		domainName := session.QuestionSnapshot.Domain
		if domainName == "" {
			domainName = "general"
		}
		item := ensureStats(domainName)
		if session.QuestionID != "" {
			item.CompletedQuestionIDs[session.QuestionID] = true
			completedQuestions[session.QuestionID] = true
		}
		if session.Score != nil && session.Score.Total > 0 {
			item.Entries = append(item.Entries, learningScoreEntry{
				Score:      session.Score.Total,
				At:         session.LastActiveAt,
				QuestionID: session.QuestionID,
			})
		}
	}

	for _, session := range interviewSessions {
		if session.FinalScore <= 0 {
			continue
		}
		question, ok := s.store.GetInterviewQuestion(session.QuestionID)
		if !ok {
			continue
		}
		item := ensureStats(question.Domain)
		item.Entries = append(item.Entries, learningScoreEntry{
			Score:      session.FinalScore,
			At:         session.StartedAt,
			QuestionID: session.QuestionID,
		})
	}

	domainNames := map[string]bool{}
	for _, domainName := range user.Profile.PreferredDomains {
		if strings.TrimSpace(domainName) != "" {
			domainNames[domainName] = true
		}
	}
	for domainName := range user.Profile.CapabilityRadar {
		domainNames[domainName] = true
	}
	for domainName := range statsByDomain {
		domainNames[domainName] = true
	}
	for _, scenario := range scenarios {
		if scenario.Status == "active" && strings.TrimSpace(scenario.Domain) != "" {
			domainNames[scenario.Domain] = true
		}
	}

	insights := make([]domain.LearningDomainInsight, 0, len(domainNames))
	for domainName := range domainNames {
		item := statsByDomain[domainName]
		entries := []learningScoreEntry{}
		completedCount := 0
		if item != nil {
			entries = append(entries, item.Entries...)
			completedCount = len(item.CompletedQuestionIDs)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].At.Before(entries[j].At)
		})
		score := user.Profile.CapabilityRadar[domainName]
		if score == 0 {
			score = averageLearningScore(entries, 50)
		}
		lastScore := 0
		if len(entries) > 0 {
			lastScore = entries[len(entries)-1].Score
		}
		insights = append(insights, domain.LearningDomainInsight{
			Domain:         domainName,
			Score:          clamp(score, 0, 100),
			Level:          learningLevel(score),
			Trend:          learningTrend(entries),
			CompletedCount: completedCount,
			LastScore:      lastScore,
			Reason:         learningReason(domainName, score, completedCount),
		})
	}
	sort.Slice(insights, func(i, j int) bool {
		if insights[i].Score == insights[j].Score {
			return insights[i].Domain < insights[j].Domain
		}
		return insights[i].Score < insights[j].Score
	})

	focusDomains := make([]string, 0, 3)
	for _, insight := range insights {
		if len(focusDomains) == 3 {
			break
		}
		focusDomains = append(focusDomains, insight.Domain)
	}
	if len(focusDomains) == 0 {
		focusDomains = append(focusDomains, user.Profile.PreferredDomains...)
	}
	if len(focusDomains) > 3 {
		focusDomains = focusDomains[:3]
	}

	recommendations := s.learningRecommendations(user, scenarios, insights, focusDomains, completedQuestions)
	plan := domain.LearningPlan{
		GeneratedAt:     time.Now(),
		Summary:         learningSummary(user, insights, focusDomains),
		TargetLevel:     user.Profile.TargetLevel,
		FocusDomains:    focusDomains,
		DomainInsights:  insights,
		Recommendations: recommendations,
		ReviewPlan:      buildReviewPlan(focusDomains, recommendations, scenarioSessions, interviewSessions),
	}
	return plan
}

func (s *Server) learningRecommendations(user *domain.User, scenarios []domain.ScenarioQuestion, insights []domain.LearningDomainInsight, focusDomains []string, completedQuestions map[string]bool) []domain.LearningRecommendation {
	focus := map[string]bool{}
	for _, domainName := range focusDomains {
		focus[domainName] = true
	}
	scoreByDomain := map[string]int{}
	for _, insight := range insights {
		scoreByDomain[insight.Domain] = insight.Score
	}

	items := []domain.LearningRecommendation{}
	for _, scenario := range scenarios {
		if scenario.Status != "active" || completedQuestions[scenario.ID] {
			continue
		}
		if len(focus) > 0 && !focus[scenario.Domain] {
			continue
		}
		view := scenarioView(&scenario, user)
		score := scoreByDomain[scenario.Domain]
		priority := clamp(115-score, 40, 100)
		items = append(items, domain.LearningRecommendation{
			ID:          "scenario:" + scenario.ID,
			Kind:        "scenario",
			Domain:      scenario.Domain,
			Title:       scenario.Title,
			Description: scenario.Description,
			Difficulty:  scenario.Difficulty,
			Priority:    priority,
			Reason:      fmt.Sprintf("AI 推荐：%s 当前画像分为 %d，适合通过情景排查补强。", displayDomain(scenario.Domain), score),
			ActionLabel: "进入排查工坊",
			ActionPath:  "/scenarios",
			Question:    &view,
		})
	}
	if len(items) == 0 {
		for _, scenario := range scenarios {
			if scenario.Status != "active" {
				continue
			}
			view := scenarioView(&scenario, user)
			items = append(items, domain.LearningRecommendation{
				ID:          "scenario:" + scenario.ID,
				Kind:        "scenario",
				Domain:      scenario.Domain,
				Title:       scenario.Title,
				Description: scenario.Description,
				Difficulty:  scenario.Difficulty,
				Priority:    65,
				Reason:      "规则回退：作为通用复习题补齐近期训练节奏。",
				ActionLabel: "进入排查工坊",
				ActionPath:  "/scenarios",
				Question:    &view,
			})
			if len(items) == 3 {
				break
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].Title < items[j].Title
		}
		return items[i].Priority > items[j].Priority
	})
	if len(items) > 4 {
		items = items[:4]
	}
	if len(focusDomains) > 0 {
		domainName := focusDomains[0]
		score := scoreByDomain[domainName]
		items = append(items, domain.LearningRecommendation{
			ID:          "interview:" + domainName,
			Kind:        "interview",
			Domain:      domainName,
			Title:       displayDomain(domainName) + "专项面试追问",
			Description: "用两轮文本追问检查排查表达、证据组织和根因归纳。",
			Difficulty:  targetInterviewDifficulty(user.Profile.TargetLevel),
			Priority:    clamp(105-score, 45, 95),
			Reason:      fmt.Sprintf("AI 推荐：围绕 %s 做一次面试模拟，验证能否把排查过程讲清楚。", displayDomain(domainName)),
			ActionLabel: "进入面试舱",
			ActionPath:  "/interviews",
		})
	}
	return items
}

func scenarioRecommendationsFromPlan(plan domain.LearningPlan) []domain.ScenarioQuestionView {
	views := []domain.ScenarioQuestionView{}
	for _, item := range plan.Recommendations {
		if item.Question == nil {
			continue
		}
		views = append(views, *item.Question)
		if len(views) == 3 {
			break
		}
	}
	return views
}

func weakPointsFromPlan(plan domain.LearningPlan, fallback []domain.WeakPoint) []domain.WeakPoint {
	points := []domain.WeakPoint{}
	questionsByDomain := map[string][]string{}
	for _, item := range plan.Recommendations {
		if item.Kind != "scenario" || item.Question == nil {
			continue
		}
		questionsByDomain[item.Domain] = append(questionsByDomain[item.Domain], item.Question.ID)
	}
	for _, insight := range plan.DomainInsights {
		if insight.Score >= 75 && insight.CompletedCount > 0 {
			continue
		}
		topic := "基线训练"
		if insight.CompletedCount > 0 {
			topic = insight.Level
		}
		lastScore := insight.LastScore
		if lastScore == 0 {
			lastScore = insight.Score
		}
		points = append(points, domain.WeakPoint{
			Domain:             insight.Domain,
			Topic:              topic,
			LastScore:          lastScore,
			SuggestedQuestions: append([]string{}, questionsByDomain[insight.Domain]...),
		})
		if len(points) == 3 {
			break
		}
	}
	if len(points) == 0 {
		return fallback
	}
	return points
}

func reviewCalendarFromPlan(user *domain.User, plan domain.LearningPlan, now time.Time) domain.ReviewCalendar {
	today := now.Format("2006-01-02")
	checkinDates := []string{}
	streakDays := 0
	todayChecked := false
	if user != nil {
		checkinDates = normalizeCheckinDates(user.Profile.CheckinDates)
		if user.Profile.LastCheckinDate != "" && !containsString(checkinDates, user.Profile.LastCheckinDate) {
			checkinDates = append(checkinDates, user.Profile.LastCheckinDate)
			checkinDates = normalizeCheckinDates(checkinDates)
		}
		todayChecked = containsString(checkinDates, today)
		streakDays = streakFromDates(checkinDates, today)
	}
	return domain.ReviewCalendar{
		GeneratedAt:  now,
		CheckinDates: checkinDates,
		StreakDays:   streakDays,
		TodayChecked: todayChecked,
		Today:        today,
		ReviewPlan:   plan.ReviewPlan,
		FocusDomains: append([]string{}, plan.FocusDomains...),
		NextAction:   nextReviewAction(plan),
	}
}

func (s *Server) checkin(user *domain.User, now time.Time) (domain.CheckinResult, *domain.User, error) {
	if user == nil {
		return domain.CheckinResult{}, nil, fmt.Errorf("user not found")
	}
	today := now.Format("2006-01-02")
	profile := user.Profile
	dates := normalizeCheckinDates(profile.CheckinDates)
	already := containsString(dates, today)
	if !already {
		dates = append(dates, today)
		dates = normalizeCheckinDates(dates)
	}
	profile.CheckinDates = dates
	profile.LastCheckinDate = today
	profile.TotalStats.StreakDays = streakFromDates(dates, today)
	updated, err := s.store.SaveUserProfile(user.ID, profile)
	if err != nil {
		return domain.CheckinResult{}, nil, err
	}
	result := domain.CheckinResult{
		CheckedIn:        true,
		AlreadyCheckedIn: already,
		CheckinDate:      today,
		StreakDays:       updated.Profile.TotalStats.StreakDays,
		NextAction:       nextReviewAction(s.learningPlan(updated)),
	}
	return result, updated, nil
}

func normalizeCheckinDates(dates []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dates))
	for _, value := range dates {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func streakFromDates(dates []string, today string) int {
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	current, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 0
	}
	checked := map[string]bool{}
	for _, value := range normalizeCheckinDates(dates) {
		checked[value] = true
	}
	streak := 0
	for {
		key := current.Format("2006-01-02")
		if !checked[key] {
			break
		}
		streak++
		current = current.AddDate(0, 0, -1)
	}
	return streak
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func nextReviewAction(plan domain.LearningPlan) string {
	if len(plan.ReviewPlan) == 0 {
		return "先完成一题排查训练，建立本周复习样本。"
	}
	first := plan.ReviewPlan[0]
	if len(first.Actions) > 0 {
		return first.DayLabel + "：" + first.Actions[0]
	}
	return first.DayLabel + "：" + first.Focus
}

func averageLearningScore(entries []learningScoreEntry, fallback int) int {
	if len(entries) == 0 {
		return fallback
	}
	total := 0
	for _, entry := range entries {
		total += entry.Score
	}
	return total / len(entries)
}

func learningLevel(score int) string {
	switch {
	case score >= 85:
		return "稳定"
	case score >= 70:
		return "可提升"
	case score >= 60:
		return "需巩固"
	default:
		return "重点补强"
	}
}

func learningTrend(entries []learningScoreEntry) string {
	if len(entries) < 2 {
		return "样本不足"
	}
	last := entries[len(entries)-1].Score
	prev := entries[len(entries)-2].Score
	switch {
	case last-prev >= 5:
		return "上升"
	case prev-last >= 5:
		return "下降"
	default:
		return "稳定"
	}
}

func learningReason(domainName string, score int, completedCount int) string {
	switch {
	case completedCount == 0:
		return fmt.Sprintf("%s 还没有完成记录，建议先做一轮基础训练。", displayDomain(domainName))
	case score < 60:
		return fmt.Sprintf("%s 得分偏低，需要优先补证据收集和根因归纳。", displayDomain(domainName))
	case score < 75:
		return fmt.Sprintf("%s 已有基础，但稳定性还不够，适合做专项复盘。", displayDomain(domainName))
	default:
		return fmt.Sprintf("%s 表现较稳，可以用面试追问提高表达质量。", displayDomain(domainName))
	}
}

func learningSummary(user *domain.User, insights []domain.LearningDomainInsight, focusDomains []string) string {
	if len(focusDomains) == 0 {
		return "当前训练样本还不多，建议先完成一次排查题和一次面试，建立基线画像。"
	}
	focusLabels := make([]string, 0, len(focusDomains))
	for _, domainName := range focusDomains {
		focusLabels = append(focusLabels, displayDomain(domainName))
	}
	average := user.Profile.TotalStats.AverageScore
	if average == 0 {
		return fmt.Sprintf("目标职级为 %s，建议先围绕 %s 建立训练样本。", displayTargetLevel(user.Profile.TargetLevel), strings.Join(focusLabels, "、"))
	}
	return fmt.Sprintf("当前平均分 %d，下一轮优先补强 %s，并用面试追问验证表达完整度。", average, strings.Join(focusLabels, "、"))
}

func buildReviewPlan(focusDomains []string, recommendations []domain.LearningRecommendation, scenarioSessions []domain.ScenarioSession, interviewSessions []domain.InterviewSession) []domain.ReviewPlanItem {
	wrongItems := reviewItemsFromHistory(scenarioSessions, interviewSessions)
	if len(wrongItems) >= 3 {
		return wrongItems[:3]
	}
	if len(focusDomains) == 0 {
		focusDomains = []string{"database", "network", "os"}
	}
	templates := []struct {
		Day     string
		Focus   string
		Actions []string
		Minutes int
		Target  int
	}{
		{Day: "第 1 天", Focus: "完成一题并标记关键证据", Actions: []string{"完成 1 道情景排查题", "记录至少 3 条关键证据", "复盘遗漏线索"}, Minutes: 35, Target: 70},
		{Day: "第 2 天", Focus: "补一次面试表达", Actions: []string{"完成 1 次文本面试", "把根因、证据、修复动作压缩成 2 分钟表达", "整理追问中的缺口"}, Minutes: 30, Target: 75},
		{Day: "第 3 天", Focus: "回看错因并做同域巩固", Actions: []string{"重看最近复盘报告", "复述标准排查步骤", "再做 1 道同域题或 UGC 转化题"}, Minutes: 40, Target: 80},
	}
	items := make([]domain.ReviewPlanItem, 0, len(templates))
	items = append(items, wrongItems...)
	for i, template := range templates {
		if len(items) == 3 {
			break
		}
		domainName := focusDomains[i%len(focusDomains)]
		questionIDs := []string{}
		for _, recommendation := range recommendations {
			if recommendation.Kind == "scenario" && recommendation.Domain == domainName && recommendation.Question != nil {
				questionIDs = append(questionIDs, recommendation.Question.ID)
			}
			if len(questionIDs) == 2 {
				break
			}
		}
		items = append(items, domain.ReviewPlanItem{
			DayLabel:         template.Day,
			Domain:           domainName,
			Focus:            displayDomain(domainName) + "：" + template.Focus,
			Actions:          append([]string{}, template.Actions...),
			EstimatedMinutes: template.Minutes,
			TargetScore:      template.Target,
			QuestionIDs:      questionIDs,
			SourceKind:       "recommendation",
			Reason:           "当前没有足够低分错题，使用画像推荐补齐复习计划。",
		})
	}
	return items
}

func reviewItemsFromHistory(scenarioSessions []domain.ScenarioSession, interviewSessions []domain.InterviewSession) []domain.ReviewPlanItem {
	items := []domain.ReviewPlanItem{}
	for _, session := range scenarioSessions {
		if session.Score == nil || session.Score.Total >= 75 {
			continue
		}
		domainName := session.QuestionSnapshot.Domain
		if domainName == "" {
			domainName = "general"
		}
		items = append(items, domain.ReviewPlanItem{
			DayLabel:         fmt.Sprintf("第 %d 天", len(items)+1),
			Domain:           domainName,
			Focus:            displayDomain(domainName) + "：复盘低分排查题",
			Actions:          []string{"重看完整对话记录", "对照标准步骤补齐遗漏证据", "重新写一版根因判断"},
			EstimatedMinutes: 35,
			TargetScore:      80,
			QuestionIDs:      []string{session.QuestionID},
			SourceKind:       "scenario_wrong",
			SourceID:         session.ID,
			Reason:           fmt.Sprintf("最近排查得分 %d，优先安排错题复盘。", session.Score.Total),
		})
	}
	for _, session := range interviewSessions {
		if session.FinalScore <= 0 || session.FinalScore >= 75 {
			continue
		}
		items = append(items, domain.ReviewPlanItem{
			DayLabel:         fmt.Sprintf("第 %d 天", len(items)+1),
			Domain:           "interview",
			Focus:            "面试表达：复盘低分回答",
			Actions:          []string{"重读本轮不足项", "按五维评分补一版结构化回答", "控制在 2 分钟内复述"},
			EstimatedMinutes: 30,
			TargetScore:      78,
			QuestionIDs:      []string{session.QuestionID},
			SourceKind:       "interview_wrong",
			SourceID:         session.ID,
			Reason:           fmt.Sprintf("最近面试得分 %d，建议优先复盘表达结构。", session.FinalScore),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].SourceID > items[j].SourceID
	})
	return items
}

func targetInterviewDifficulty(targetLevel string) string {
	switch targetLevel {
	case "junior":
		return "L2"
	case "senior", "architect":
		return "L4"
	default:
		return "L3"
	}
}

func displayDomain(value string) string {
	switch value {
	case "database":
		return "数据库"
	case "network":
		return "网络"
	case "os":
		return "操作系统"
	case "security":
		return "安全"
	case "devops":
		return "DevOps"
	default:
		return value
	}
}

func displayTargetLevel(value string) string {
	switch value {
	case "junior":
		return "初级"
	case "senior":
		return "高级"
	case "architect":
		return "架构师"
	default:
		return "中级"
	}
}

func (s *Server) visibleCommunityPosts(user *domain.User, status string, view string) []domain.CommunityPost {
	status = strings.TrimSpace(status)
	view = strings.TrimSpace(view)
	items := s.store.ListCommunityPosts()
	out := make([]domain.CommunityPost, 0, len(items))
	for _, item := range items {
		if !canViewCommunityPost(user, &item) {
			continue
		}
		if view != "" {
			if !matchesCommunityPostHistoryView(user, &item, view) {
				continue
			}
			viewItem := s.communityPostView(user, &item)
			out = append(out, viewItem)
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		viewItem := s.communityPostView(user, &item)
		out = append(out, viewItem)
	}
	return out
}

func matchesCommunityPostHistoryView(user *domain.User, post *domain.CommunityPost, view string) bool {
	if user == nil || post == nil {
		return false
	}
	action := ""
	switch view {
	case "instructor_reviewed":
		action = "instructor_approve"
	case "instructor_rejected":
		action = "instructor_reject"
	default:
		return false
	}
	for _, item := range post.ReviewHistory {
		if item.ActorID == user.ID && item.Action == action {
			return true
		}
	}
	return false
}

func (s *Server) scenarioFromCommunityPost(post *domain.CommunityPost, adminID string) domain.ScenarioQuestion {
	domainName := strings.TrimSpace(post.Domain)
	if domainName == "" {
		domainName = "database"
	}
	tags := append([]string{}, post.Tags...)
	if len(tags) == 0 {
		tags = []string{"UGC", domainName}
	}
	description := strings.TrimSpace(post.RawContent)
	if description == "" {
		description = post.Title
	}
	scenario := domain.ScenarioQuestion{
		Title:        ai.SanitizeFields(post.Title),
		Description:  ai.SanitizeFields(description),
		Domain:       domainName,
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         sanitizeTextSlice(tags),
		Content:      sanitizeScenarioContent(*effectiveCommunityContent(post)),
		Status:       "active",
		Source:       "ugc_structured",
		CreatedBy:    adminID,
		Version:      1,
	}
	scenario.Content = ai.PrepareScenarioContent(scenario.Content, scenario)
	return scenario
}

func communityPostFromScenarioFork(source *domain.ScenarioQuestion, userID string) domain.CommunityPost {
	content := forkDraftContent(source)
	title := ""
	rawContent := ""
	domainName := "database"
	tags := []string{"Fork"}
	sourceID := ""
	if source != nil {
		title = "派生题目：" + source.Title
		rawContent = source.Description
		domainName = source.Domain
		tags = append([]string{}, source.Tags...)
		sourceID = source.ID
	}
	if len(tags) == 0 {
		tags = []string{"Fork", domainName}
	}
	edited := content
	return domain.CommunityPost{
		UserID:                  userID,
		Title:                   title,
		RawContent:              rawContent,
		Domain:                  domainName,
		Tags:                    tags,
		ForkedFromScenarioID:    sourceID,
		AIStructuredContent:     content,
		EditedStructuredContent: &edited,
		SensitiveCheck:          ruleSensitiveCheck("fork_source", strings.Join([]string{title, rawContent, strings.Join(tags, " ")}, "\n")),
		Status:                  "draft",
		ReviewHistory:           []domain.ReviewHistoryItem{},
	}
}

func forkDraftContent(source *domain.ScenarioQuestion) domain.ScenarioContent {
	content := domain.ScenarioContent{
		RevealStrategy: domain.RevealStrategy{
			SurfaceClues: []domain.Clue{},
			DeepClues:    []domain.Clue{},
			Distractors:  []domain.Clue{},
		},
		ReferenceLinks: []string{},
	}
	if source == nil {
		return content
	}
	content.ArchitectureDiagram = source.Content.ArchitectureDiagram
	return content
}

func effectiveCommunityContent(post *domain.CommunityPost) *domain.ScenarioContent {
	if post != nil && post.EditedStructuredContent != nil {
		return post.EditedStructuredContent
	}
	if post == nil {
		return &domain.ScenarioContent{}
	}
	return &post.AIStructuredContent
}

func (s *Server) refreshCommunityModerationSummary(post domain.CommunityPost, stage string) domain.CommunityPost {
	agent := agentruntime.NewCommunityReviewAgent(agentruntime.CommunityReviewConfig{})
	result, err := agent.Run(context.Background(), agentruntime.CommunityReviewRequest{
		Post:         &post,
		Stage:        stage,
		ReviewerRole: domain.RoleInstructor,
	})
	if err != nil {
		s.auditCommunityReviewAgentRun(nil, nil, post.ID, result.Trace, result.Summary, "failed", err)
		return post
	}
	post.ModerationSummary = result.Summary
	updated := s.store.SaveCommunityPost(&post)
	s.auditCommunityReviewAgentRun(nil, nil, updated.ID, result.Trace, result.Summary, "completed", nil)
	return updated
}

func (s *Server) communityPostView(user *domain.User, post *domain.CommunityPost) domain.CommunityPost {
	if post == nil {
		return domain.CommunityPost{}
	}
	view := *post
	view.AuthorUsername = communityPostAuthorName(s.store, post)
	view.AIStructuredContent = prepareCommunityContentForView(view.AIStructuredContent, view.Title, view.Domain)
	if post.EditedStructuredContent != nil {
		edited := prepareCommunityContentForView(*post.EditedStructuredContent, view.Title, view.Domain)
		view.EditedStructuredContent = &edited
	}
	if len(view.ReviewHistory) > 0 {
		view.ReviewHistory = append([]domain.ReviewHistoryItem{}, view.ReviewHistory...)
		for i := range view.ReviewHistory {
			if view.ReviewHistory[i].Content == nil {
				continue
			}
			content := prepareCommunityContentForView(*view.ReviewHistory[i].Content, view.Title, view.Domain)
			view.ReviewHistory[i].Content = &content
		}
	}
	if post.ModerationSummary != nil {
		summary := *post.ModerationSummary
		summary.SafeLabels = append([]string{}, post.ModerationSummary.SafeLabels...)
		summary.Reasons = append([]string{}, post.ModerationSummary.Reasons...)
		summary.AgentTrace = nil
		view.ModerationSummary = &summary
	}
	if !hasAnyRole(user, domain.RoleInstructor, domain.RoleAdmin) {
		view.ModerationSummary = nil
	}
	return view
}

func communityPostAuthorName(dataStore store.Store, post *domain.CommunityPost) string {
	if dataStore == nil || post == nil || strings.TrimSpace(post.UserID) == "" {
		return "未知作者"
	}
	user, ok := dataStore.GetUser(post.UserID)
	if !ok || user == nil || strings.TrimSpace(user.Username) == "" {
		return "用户已注销"
	}
	return user.Username
}

func prepareCommunityContentForView(content domain.ScenarioContent, title, domainName string) domain.ScenarioContent {
	return ai.PrepareScenarioContent(content, domain.ScenarioQuestion{
		Title:   title,
		Domain:  domainName,
		Content: content,
	})
}

func validCommunityScenarioContent(content *domain.ScenarioContent) bool {
	if content == nil {
		return false
	}
	return strings.TrimSpace(content.RootCause) != "" &&
		len(content.KeyEvidence) > 0 &&
		len(content.StandardProcedure) > 0
}

func normalizeScenarioContent(content domain.ScenarioContent, fallback domain.ScenarioContent) domain.ScenarioContent {
	if strings.TrimSpace(content.RootCause) == "" {
		content.RootCause = fallback.RootCause
	}
	if len(content.RootCauseKeywords) == 0 {
		content.RootCauseKeywords = append([]string{}, fallback.RootCauseKeywords...)
	}
	if len(content.KeyEvidence) == 0 {
		content.KeyEvidence = append([]string{}, fallback.KeyEvidence...)
	}
	if len(content.StandardProcedure) == 0 {
		content.StandardProcedure = append([]string{}, fallback.StandardProcedure...)
	}
	if strings.TrimSpace(content.ArchitectureDiagram) == "" {
		content.ArchitectureDiagram = fallback.ArchitectureDiagram
	}
	if len(content.ReferenceLinks) == 0 {
		content.ReferenceLinks = append([]string{}, fallback.ReferenceLinks...)
	}
	if len(content.RevealStrategy.SurfaceClues) == 0 && len(content.RevealStrategy.DeepClues) == 0 && len(content.RevealStrategy.Distractors) == 0 {
		content.RevealStrategy = fallback.RevealStrategy
	}
	return content
}

func sanitizeScenarioContent(content domain.ScenarioContent) domain.ScenarioContent {
	content.RootCause = ai.SanitizeFields(content.RootCause)
	for i, value := range content.RootCauseKeywords {
		content.RootCauseKeywords[i] = ai.SanitizeFields(value)
	}
	for i, value := range content.KeyEvidence {
		content.KeyEvidence[i] = ai.SanitizeFields(value)
	}
	for i, value := range content.StandardProcedure {
		content.StandardProcedure[i] = ai.SanitizeFields(value)
	}
	for i, value := range content.ReferenceLinks {
		content.ReferenceLinks[i] = ai.SanitizeFields(value)
	}
	content.ArchitectureDiagram = ai.SanitizeFields(content.ArchitectureDiagram)
	content.ArchitectureDiagramSpec = ai.SanitizeScenarioDiagramSpec(content.ArchitectureDiagramSpec)
	for i, clue := range content.RevealStrategy.SurfaceClues {
		clue.Content = ai.SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = ai.SanitizeFields(clue.RecommendedNextAsk)
		content.RevealStrategy.SurfaceClues[i] = clue
	}
	for i, clue := range content.RevealStrategy.DeepClues {
		clue.Content = ai.SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = ai.SanitizeFields(clue.RecommendedNextAsk)
		content.RevealStrategy.DeepClues[i] = clue
	}
	for i, clue := range content.RevealStrategy.Distractors {
		clue.Content = ai.SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = ai.SanitizeFields(clue.RecommendedNextAsk)
		content.RevealStrategy.Distractors[i] = clue
	}
	return content
}

func sanitizeTextSlice(values []string) []string {
	items := append([]string{}, values...)
	for i, value := range items {
		items[i] = ai.SanitizeFields(value)
	}
	return items
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

func voiceTranscriptDraft(asset *domain.Asset, session *domain.InterviewSession) string {
	filename := "语音答案"
	if asset != nil && strings.TrimSpace(asset.Filename) != "" {
		filename = asset.Filename
	}
	round := 1
	if session != nil && session.CurrentRound > 0 {
		round = session.CurrentRound
	}
	return fmt.Sprintf("第 %d 轮 %s 转写草稿：我会先说明定位路径，再补充关键命令、验证指标、修复方案和回滚策略。", round, filename)
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

func canViewCommunityPost(user *domain.User, post *domain.CommunityPost) bool {
	if user == nil || post == nil {
		return false
	}
	if post.UserID == user.ID {
		return true
	}
	if post.Status == "draft" {
		return false
	}
	return user.Role == domain.RoleInstructor || user.Role == domain.RoleAdmin
}

func canDeleteCommunityPost(user *domain.User, post *domain.CommunityPost) bool {
	if user == nil || post == nil {
		return false
	}
	if post.Status == "published" {
		return user.Role == domain.RoleAdmin
	}
	if user.Role == domain.RoleAdmin {
		return true
	}
	if post.UserID != user.ID {
		return false
	}
	return post.Status == "draft" || post.Status == "pending_review" || post.Status == "instructor_rejected" || post.Status == "final_rejected"
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

func reviewHistoryItem(actorID, action, fromStatus, toStatus, note string, content *domain.ScenarioContent) domain.ReviewHistoryItem {
	item := domain.ReviewHistoryItem{
		ID:         store.NewID(),
		ActorID:    actorID,
		Action:     action,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Note:       strings.TrimSpace(note),
		CreatedAt:  time.Now(),
	}
	if content != nil {
		copy := *content
		item.Content = &copy
	}
	return item
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

func communityPostCount(posts []domain.CommunityPost) int {
	return len(posts)
}

func (s *Server) history(userID string) map[string]interface{} {
	scenarioSessions := s.store.ListScenarioSessionsForUser(userID)
	interviewSessions := s.store.ListInterviewSessionsForUser(userID)
	communityPosts := s.communityPostsForUserHistory(userID)
	sort.Slice(scenarioSessions, func(i, j int) bool {
		return scenarioSessions[i].StartedAt.After(scenarioSessions[j].StartedAt)
	})
	sort.Slice(interviewSessions, func(i, j int) bool {
		return interviewSessions[i].StartedAt.After(interviewSessions[j].StartedAt)
	})
	return map[string]interface{}{"scenarios": scenarioSessionViews(scenarioSessions), "interviews": interviewSessions, "community_posts": communityPosts}
}

func (s *Server) communityPostsForUserHistory(userID string) []domain.CommunityPost {
	items := s.store.ListCommunityPosts()
	out := make([]domain.CommunityPost, 0, len(items))
	user := &domain.User{ID: userID, Role: domain.RoleStudent}
	for _, item := range items {
		if item.UserID != userID {
			continue
		}
		viewItem := s.communityPostView(user, &item)
		out = append(out, viewItem)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func hasAnyRole(user *domain.User, roles ...string) bool {
	if user == nil {
		return false
	}
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	return false
}

func canViewScenario(question *domain.ScenarioQuestion, user *domain.User) bool {
	if question.Status == "active" {
		return true
	}
	if user == nil {
		return false
	}
	return hasAnyRole(user, domain.RoleInstructor, domain.RoleAdmin) || user.ID == question.CreatedBy
}

func canViewAIJob(job *domain.AIJob, user *domain.User) bool {
	if job == nil || user == nil {
		return false
	}
	return user.Role == domain.RoleAdmin || job.UserID == user.ID
}

func scenarioPublicView(question *domain.ScenarioQuestion) domain.ScenarioQuestionView {
	content := sanitizeScenarioContent(question.Content)
	preparedQuestion := *question
	preparedQuestion.Title = ai.SanitizeFields(question.Title)
	preparedQuestion.Description = ai.SanitizeFields(question.Description)
	preparedQuestion.Tags = sanitizeTextSlice(question.Tags)
	preparedQuestion.Content = content
	content = ai.PrepareScenarioContent(content, preparedQuestion)
	content.RootCause = ""
	content.RootCauseKeywords = nil
	content.KeyEvidence = nil
	content.StandardProcedure = nil
	content.RevealStrategy = domain.RevealStrategy{}
	return scenarioQuestionViewFrom(question, preparedQuestion.Title, preparedQuestion.Description, preparedQuestion.Tags, content, true)
}

func scenarioFullView(question *domain.ScenarioQuestion) domain.ScenarioQuestionView {
	content := ai.PrepareScenarioContent(question.Content, *question)
	return scenarioQuestionViewFrom(question, question.Title, question.Description, append([]string{}, question.Tags...), content, false)
}

func scenarioDetailView(question *domain.ScenarioQuestion, user *domain.User) domain.ScenarioQuestionView {
	if canViewFullScenario(question, user) {
		return scenarioFullView(question)
	}
	return scenarioPublicView(question)
}

func canViewFullScenario(question *domain.ScenarioQuestion, user *domain.User) bool {
	if question == nil || user == nil {
		return false
	}
	return user.Role == domain.RoleAdmin || user.Role == domain.RoleInstructor || user.ID == question.CreatedBy
}

func scenarioView(question *domain.ScenarioQuestion, user *domain.User) domain.ScenarioQuestionView {
	return scenarioDetailView(question, user)
}

type scenarioSessionResponse struct {
	ID                  string                      `json:"id"`
	UserID              string                      `json:"user_id"`
	QuestionID          string                      `json:"question_id"`
	Status              string                      `json:"status"`
	CurrentTurn         int                         `json:"current_turn"`
	MaxTurns            int                         `json:"max_turns"`
	RevealedClueIDs     []string                    `json:"revealed_clue_ids"`
	UserAnswer          string                      `json:"user_answer,omitempty"`
	EvaluationResult    *domain.ScenarioEvaluation  `json:"evaluation_result,omitempty"`
	Score               *domain.ScenarioScore       `json:"score,omitempty"`
	QuestionSnapshot    domain.ScenarioQuestionView `json:"question_snapshot"`
	HintLevel           int                         `json:"hint_level"`
	NoNewClueStreak     int                         `json:"no_new_clue_streak"`
	ConversationSummary string                      `json:"conversation_summary,omitempty"`
	StartedAt           time.Time                   `json:"started_at"`
	LastActiveAt        time.Time                   `json:"last_active_at"`
	EndedAt             *time.Time                  `json:"ended_at,omitempty"`
}

func scenarioSessionView(session *domain.ScenarioSession) scenarioSessionResponse {
	if session == nil {
		return scenarioSessionResponse{}
	}
	return scenarioSessionResponse{
		ID:                  session.ID,
		UserID:              session.UserID,
		QuestionID:          session.QuestionID,
		Status:              session.Status,
		CurrentTurn:         session.CurrentTurn,
		MaxTurns:            session.MaxTurns,
		RevealedClueIDs:     append([]string{}, session.RevealedClueIDs...),
		UserAnswer:          session.UserAnswer,
		EvaluationResult:    session.EvaluationResult,
		Score:               session.Score,
		QuestionSnapshot:    scenarioPublicView(&session.QuestionSnapshot),
		HintLevel:           session.HintLevel,
		NoNewClueStreak:     session.NoNewClueStreak,
		ConversationSummary: session.ConversationSummary,
		StartedAt:           session.StartedAt,
		LastActiveAt:        session.LastActiveAt,
		EndedAt:             session.EndedAt,
	}
}

func scenarioSessionViews(sessions []domain.ScenarioSession) []scenarioSessionResponse {
	views := make([]scenarioSessionResponse, 0, len(sessions))
	for i := range sessions {
		views = append(views, scenarioSessionView(&sessions[i]))
	}
	return views
}

func scenarioQuestionViewFrom(question *domain.ScenarioQuestion, title, description string, tags []string, content domain.ScenarioContent, isSanitized bool) domain.ScenarioQuestionView {
	return domain.ScenarioQuestionView{
		ID: question.ID, Title: title, Description: description,
		Domain: question.Domain, Difficulty: question.Difficulty, ScenarioType: question.ScenarioType,
		Tags: tags, Content: content, Status: question.Status, Source: question.Source,
		CreatedBy: question.CreatedBy, Version: question.Version, CreatedAt: question.CreatedAt,
		UpdatedAt: question.UpdatedAt, IsSanitized: isSanitized,
	}
}

func interviewQuestionView(question *domain.InterviewQuestion, user *domain.User) *domain.InterviewQuestion {
	if question == nil {
		return nil
	}
	copy := *question
	if user == nil || user.Role == domain.RoleStudent {
		copy.ReferenceAnswer = ""
		copy.ReferenceKeywords = nil
	}
	return &copy
}

func generatedScenario(domainName, difficulty, scenarioType string, tags []string, userID string) domain.ScenarioQuestion {
	if domainName == "" {
		domainName = "database"
	}
	if difficulty == "" {
		difficulty = "L2"
	}
	if scenarioType == "" {
		scenarioType = "troubleshooting"
	}
	if len(tags) == 0 {
		tags = []string{"AI生成", domainName}
	}
	title := fmt.Sprintf("%s 方向 %s 演示情景题", domainName, difficulty)
	root := "配置变更后缺少必要验证，导致核心链路出现异常。"
	return domain.ScenarioQuestion{
		Title:        title,
		Description:  "这是一道由 MVP mock LLM Router 生成的演示题。用户需要通过日志、指标、变更和依赖链路逐步收集线索。",
		Domain:       domainName,
		Difficulty:   difficulty,
		ScenarioType: scenarioType,
		Tags:         tags,
		Status:       "active",
		Source:       "llm_generated",
		CreatedBy:    userID,
		Version:      1,
		Content: domain.ScenarioContent{
			RootCause:           root,
			RootCauseKeywords:   []string{"配置变更", "验证", "核心链路"},
			KeyEvidence:         []string{"异常开始时间与配置发布时间一致", "回滚配置后指标恢复", "下游服务本身无异常"},
			StandardProcedure:   []string{"确认异常窗口", "聚合日志与指标", "比对最近变更", "验证依赖链路", "灰度回滚并观察"},
			ArchitectureDiagram: "graph TD\nA[Client] --> B[API]\nB --> C[Core Service]\nC --> D[Dependency]\nC --> E[Config Center]",
			ReferenceLinks:      []string{"变更管理", "故障复盘"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{
					{ClueID: "c1", TriggerKeywords: []string{"日志", "时间", "窗口"}, Content: "异常开始时间与一次配置发布高度重合。", RecommendedNextAsk: "继续询问变更内容。"},
					{ClueID: "c2", TriggerKeywords: []string{"指标", "监控", "依赖"}, Content: "下游依赖服务自身指标正常，异常主要集中在核心服务调用分支。", RecommendedNextAsk: "继续询问配置或回滚。"},
				},
				DeepClues: []domain.Clue{
					{ClueID: "c3", TriggerKeywords: []string{"配置", "变更", "回滚"}, PrerequisiteClues: []string{"c1"}, Content: "灰度回滚配置后，错误率从 8% 降至 0.5%。", RecommendedNextAsk: "可以提交根因判断。"},
				},
				Distractors: []domain.Clue{
					{ClueID: "d1", TriggerKeywords: []string{"网络", "CPU"}, Content: "网络和 CPU 指标都在正常范围内。", IsDistractor: true},
				},
			},
		},
	}
}

func countValidRevealed(strategy domain.RevealStrategy, revealed []string) int {
	valid := map[string]bool{}
	for _, clue := range strategy.SurfaceClues {
		valid[clue.ClueID] = true
	}
	for _, clue := range strategy.DeepClues {
		valid[clue.ClueID] = true
	}
	count := 0
	for _, clueID := range revealed {
		if valid[clueID] {
			count++
		}
	}
	return count
}

func finalInterviewReport(evaluation domain.InterviewEvaluation) string {
	if evaluation.TotalScore >= 85 {
		return "整体表现优秀，能够覆盖关键定位路径，并具备较好的落地意识。"
	}
	if evaluation.TotalScore >= 70 {
		return "整体达到岗位要求，建议继续强化底层原理与应急取舍。"
	}
	return "当前回答还有明显缺口，建议围绕关键命令、验证路径和回滚方案进行专项练习。"
}

type irrelevantInterviewDecision struct {
	Irrelevant bool
	Final      bool
	Attempt    int
	Message    string
}

func evaluateIrrelevantInterviewAnswer(question *domain.InterviewQuestion, answer string, previousAttempts int) irrelevantInterviewDecision {
	trimmed := strings.TrimSpace(answer)
	if len([]rune(trimmed)) < 80 {
		return irrelevantInterviewDecision{}
	}
	relevance := interviewTopicRelevance(question, trimmed)
	hits := len(interviewKeywordHits(question, trimmed))
	if relevance >= 25 || hits > 0 {
		return irrelevantInterviewDecision{}
	}
	attempt := previousAttempts + 1
	decision := irrelevantInterviewDecision{
		Irrelevant: true,
		Attempt:    attempt,
		Message:    "请认真回答面试问题，围绕题目说明你的定位路径、关键命令、修复方案和回滚考虑。",
	}
	if attempt >= 4 {
		decision.Final = true
		decision.Message = "面试官认为你还没有准备好，请先继续沉淀，再重新开始本场面试。"
	}
	return decision
}

func irrelevantInterviewSubmissionCount(session *domain.InterviewSession) int {
	if session == nil {
		return 0
	}
	count := 0
	for _, submission := range session.Submissions {
		if submission.QualityFlag == "irrelevant" {
			count++
		}
	}
	return count
}

func irrelevantInterviewEvaluation(round int, decision irrelevantInterviewDecision) domain.InterviewEvaluation {
	evaluation := domain.InterviewEvaluation{
		Round:           round,
		TotalScore:      0,
		DimensionScores: map[string]int{},
		IsPassed:        false,
		Highlights:      []string{},
		Deficiencies:    []string{decision.Message},
		CreatedAt:       time.Now(),
	}
	if decision.Final {
		evaluation.FollowUpTriggered = false
		return evaluation
	}
	evaluation.FollowUpTriggered = true
	evaluation.FollowUpType = "guidance"
	evaluation.FollowUpQuestion = decision.Message
	return evaluation
}

func radarData(session *domain.InterviewSession) []map[string]interface{} {
	if session == nil || len(session.Evaluations) == 0 {
		return []map[string]interface{}{}
	}
	last := session.Evaluations[len(session.Evaluations)-1]
	data := make([]map[string]interface{}, 0, len(last.DimensionScores))
	for key, value := range last.DimensionScores {
		data = append(data, map[string]interface{}{"dimension": key, "score": value})
	}
	sort.Slice(data, func(i, j int) bool {
		return fmt.Sprint(data[i]["dimension"]) < fmt.Sprint(data[j]["dimension"])
	})
	return data
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

func decode(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
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

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	return &sseWriter{w: w, flusher: flusher}
}

func (s *sseWriter) event(name string, data interface{}) {
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, mustJSON(data))
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *sseWriter) stage(step, message string) {
	s.event("stage", map[string]string{"step": step, "message": message})
}

func (s *sseWriter) delta(chunk string, displayable bool) {
	if strings.TrimSpace(chunk) == "" {
		return
	}
	s.event("delta", map[string]interface{}{"chunk": chunk, "displayable": displayable})
}

func (s *sseWriter) deltaDisplay(chunk string) {
	if strings.TrimSpace(chunk) == "" {
		return
	}
	s.event("delta", map[string]interface{}{"chunk": chunk, "displayable": true})
}

func (s *sseWriter) finish(payload interface{}) {
	s.event("finish", payload)
}

func (s *sseWriter) fail(message string) {
	s.event("error", map[string]string{"message": message})
}

func writeSSE(w http.ResponseWriter, payload map[string]interface{}, content string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunkText(content, 28) {
		fmt.Fprintf(w, "event: delta\ndata: %s\n\n", mustJSON(map[string]string{"chunk": chunk}))
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintf(w, "event: finish\ndata: %s\n\n", mustJSON(payload))
	if flusher != nil {
		flusher.Flush()
	}
}

func streamInterviewFeedbackDisplay(writer *sseWriter, feedback ai.InterviewFeedback, evaluation domain.InterviewEvaluation, needReport bool) {
	if writer == nil {
		return
	}
	highlights := feedback.Highlights
	if len(highlights) == 0 {
		highlights = evaluation.Highlights
	}
	deficiencies := feedback.Deficiencies
	if len(deficiencies) == 0 {
		deficiencies = evaluation.Deficiencies
	}
	sections := []struct {
		label string
		text  string
	}{
		{label: "总分", text: fmt.Sprintf("%d 分", evaluation.TotalScore)},
		{label: "亮点", text: strings.Join(highlights, "；")},
		{label: "待改进", text: strings.Join(deficiencies, "；")},
	}
	if evaluation.FollowUpTriggered {
		followUp := strings.TrimSpace(feedback.FollowUpQuestion)
		if followUp == "" {
			followUp = evaluation.FollowUpQuestion
		}
		sections = append(sections, struct {
			label string
			text  string
		}{label: "追问", text: followUp})
	}
	if needReport {
		sections = append(sections, struct {
			label string
			text  string
		}{label: "综合评价", text: feedback.FinalReport})
	}
	for _, section := range sections {
		text := strings.TrimSpace(section.text)
		if text == "" {
			continue
		}
		for _, chunk := range chunkText(section.label+"："+text+"\n", 42) {
			writer.deltaDisplay(chunk)
			time.Sleep(35 * time.Millisecond)
		}
	}
}

type interviewFeedbackLiveDisplay struct {
	writer          *sseWriter
	raw             strings.Builder
	highlightCount  int
	deficiencyCount int
	followUpSent    bool
	finalReportSent bool
	fieldContent    bool
	needReport      bool
}

func newInterviewFeedbackLiveDisplay(writer *sseWriter, totalScore int, needReport bool) *interviewFeedbackLiveDisplay {
	display := &interviewFeedbackLiveDisplay{writer: writer, needReport: needReport}
	if writer != nil {
		writer.deltaDisplay(fmt.Sprintf("总分：%d 分\n", totalScore))
	}
	return display
}

func (d *interviewFeedbackLiveDisplay) accept(chunk string) {
	if d == nil || d.writer == nil || chunk == "" {
		return
	}
	d.raw.WriteString(chunk)
	snapshot := parseInterviewFeedbackStreamSnapshot(d.raw.String(), d.needReport)
	d.emitNewListItems("亮点", snapshot.Highlights, &d.highlightCount)
	d.emitNewListItems("待改进", snapshot.Deficiencies, &d.deficiencyCount)
	if !d.followUpSent && strings.TrimSpace(snapshot.FollowUpQuestion) != "" {
		d.emitLine("追问", snapshot.FollowUpQuestion)
		d.followUpSent = true
	}
	if d.needReport && !d.finalReportSent && strings.TrimSpace(snapshot.FinalReport) != "" {
		d.emitLine("综合评价", snapshot.FinalReport)
		d.finalReportSent = true
	}
}

func (d *interviewFeedbackLiveDisplay) hasFieldContent() bool {
	return d != nil && d.fieldContent
}

func (d *interviewFeedbackLiveDisplay) emitNewListItems(label string, items []string, sent *int) {
	for *sent < len(items) {
		d.emitLine(label, items[*sent])
		(*sent)++
	}
}

func (d *interviewFeedbackLiveDisplay) emitLine(label, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	d.fieldContent = true
	for _, piece := range chunkText(label+"："+text+"\n", 36) {
		d.writer.deltaDisplay(piece)
		time.Sleep(25 * time.Millisecond)
	}
}

type interviewFeedbackStreamSnapshot struct {
	Highlights       []string
	Deficiencies     []string
	FollowUpQuestion string
	FinalReport      string
}

func parseInterviewFeedbackStreamSnapshot(raw string, needReport bool) interviewFeedbackStreamSnapshot {
	snapshot := interviewFeedbackStreamSnapshot{
		Highlights:       extractJSONArrayStrings(raw, "highlights"),
		Deficiencies:     extractJSONArrayStrings(raw, "deficiencies"),
		FollowUpQuestion: extractJSONStringValue(raw, "follow_up_question"),
	}
	if needReport {
		snapshot.FinalReport = extractJSONStringValue(raw, "final_report")
	}
	return snapshot
}

func extractJSONArrayStrings(raw, key string) []string {
	start := jsonValueStart(raw, key, '[')
	if start < 0 {
		return nil
	}
	items := []string{}
	inString := false
	escaped := false
	var item strings.Builder
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if !inString {
			if ch == ']' {
				break
			}
			if ch == '"' {
				inString = true
				escaped = false
				item.Reset()
			}
			continue
		}
		if escaped {
			appendEscapedJSONByte(&item, ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = false
			if text := strings.TrimSpace(item.String()); text != "" {
				items = append(items, text)
			}
			continue
		}
		item.WriteByte(ch)
	}
	return items
}

func extractJSONStringValue(raw, key string) string {
	start := jsonValueStart(raw, key, '"')
	if start < 0 {
		return ""
	}
	escaped := false
	var value strings.Builder
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			appendEscapedJSONByte(&value, ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return value.String()
		}
		value.WriteByte(ch)
	}
	return ""
}

func jsonValueStart(raw, key string, expected byte) int {
	keyToken := `"` + key + `"`
	keyIndex := strings.Index(raw, keyToken)
	if keyIndex < 0 {
		return -1
	}
	i := keyIndex + len(keyToken)
	for i < len(raw) && raw[i] != ':' {
		i++
	}
	if i >= len(raw) {
		return -1
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != expected {
		return -1
	}
	return i + 1
}

func appendEscapedJSONByte(builder *strings.Builder, ch byte) {
	switch ch {
	case '"', '\\', '/':
		builder.WriteByte(ch)
	case 'n':
		builder.WriteByte('\n')
	case 'r':
		builder.WriteByte('\r')
	case 't':
		builder.WriteByte('\t')
	default:
		builder.WriteByte(ch)
	}
}

func mustJSON(value interface{}) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func chunkText(text string, size int) []string {
	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}
	chunks := []string{}
	for len(runes) > 0 {
		n := size
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
}

const maxVoiceAssetBytes int64 = 20 * 1024 * 1024

var voiceFileExtensions = map[string]bool{
	".aac":  true,
	".flac": true,
	".m4a":  true,
	".mp3":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".webm": true,
}

type assetValidationError struct {
	status  int
	message string
}

func (e assetValidationError) Error() string {
	return e.message
}

func validateVoiceAsset(filename, mimeType string, size int64) error {
	if size <= 0 {
		return assetValidationError{status: http.StatusBadRequest, message: "invalid_asset: uploaded audio is empty"}
	}
	if size > maxVoiceAssetBytes {
		return assetValidationError{status: http.StatusBadRequest, message: "invalid_asset: uploaded audio is too large"}
	}
	normalizedMime := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(normalizedMime, "video/") {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: please upload an audio file"}
	}
	if normalizedMime != "" && !strings.HasPrefix(normalizedMime, "audio/") && normalizedMime != "application/ogg" {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: please upload an audio file"}
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: audio extension is required"}
	}
	if !voiceFileExtensions[ext] {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: audio extension is not supported"}
	}
	return nil
}

func writeAssetValidationError(w http.ResponseWriter, err error) {
	var validationErr assetValidationError
	if errors.As(err, &validationErr) {
		writeError(w, validationErr.status, validationErr.message)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeInterviewValidationError(w http.ResponseWriter, validation domain.InterviewAnswerValidation) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(envelope{
		Code:    http.StatusUnprocessableEntity,
		Message: "invalid_interview_answer: " + validation.Message,
		Data:    validation,
	})
}

func validateInterviewAnswer(question *domain.InterviewQuestion, content, transcript string, asset *domain.Asset, sttConfidence float64) domain.InterviewAnswerValidation {
	answer := strings.TrimSpace(content)
	if answer == "" {
		answer = strings.TrimSpace(transcript)
	}
	quality := domain.VoiceQualityResult{
		DetectedLanguage:    detectAnswerLanguage(answer),
		STTConfidence:       sttConfidence,
		TopicRelevanceScore: interviewTopicRelevance(question, answer),
		KeywordHits:         interviewKeywordHits(question, answer),
		Reasons:             []string{},
		Status:              "draft_ready",
	}
	if quality.STTConfidence <= 0 {
		quality.STTConfidence = 0.9
	}
	if asset != nil {
		if err := validateVoiceAsset(asset.Filename, asset.MimeType, asset.Size); err != nil {
			quality.Status = "rejected"
			quality.Reasons = append(quality.Reasons, err.Error())
			return domain.InterviewAnswerValidation{Valid: false, Message: "璇煶鏂囦欢绫诲瀷鏃犳晥锛岃閲嶆柊涓婁紶闊抽鏂囦欢", Quality: quality}
		}
	}
	if len([]rune(answer)) < 12 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "转写内容过短")
		return domain.InterviewAnswerValidation{Valid: false, Message: "转写内容过短，请重新上传或改为文本回答", Quality: quality}
	}
	if quality.STTConfidence < 0.45 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "语音识别置信度过低")
		return domain.InterviewAnswerValidation{Valid: false, Message: "璇煶璇嗗埆缃俊搴﹁繃浣庯紝璇烽噸鏂颁笂浼犳垨鏀逛负鏂囨湰鍥炵瓟", Quality: quality}
	}
	if quality.TopicRelevanceScore < 25 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "转写内容与本题相关性不足")
		return domain.InterviewAnswerValidation{Valid: false, Message: "杞啓鍐呭涓庢湰棰樼浉鍏虫€т笉瓒筹紝璇烽噸鏂颁笂浼犳垨鏀逛负鏂囨湰鍥炵瓟", Quality: quality}
	}
	if quality.DetectedLanguage == "en" && len(quality.KeywordHits) < 2 && quality.TopicRelevanceScore < 50 {
		quality.Status = "rejected"
		quality.Reasons = append(quality.Reasons, "英文内容未覆盖本题关键技术点")
		return domain.InterviewAnswerValidation{Valid: false, Message: "英文内容未覆盖本题关键技术点，请使用中文结构化回答", Quality: quality}
	}
	if quality.DetectedLanguage == "en" {
		quality.Status = "needs_review"
		quality.Reasons = append(quality.Reasons, "检测到英文为主，建议补充中文说明")
	}
	return domain.InterviewAnswerValidation{Valid: true, Quality: quality}
}

func detectAnswerLanguage(value string) string {
	var han, latin int
	for _, r := range value {
		switch {
		case r >= '\u4e00' && r <= '\u9fff':
			han++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			latin++
		}
	}
	switch {
	case han == 0 && latin == 0:
		return "unknown"
	case han == 0 && latin > 0:
		return "en"
	case latin > han*4:
		return "en"
	case han > 0:
		return "zh"
	default:
		return "mixed"
	}
}

func defaultSTTLanguage(language, transcript string) string {
	language = strings.TrimSpace(strings.ToLower(language))
	switch language {
	case "zh", "zh-cn", "chinese", "cn":
		return "zh"
	case "en", "english":
		return "en"
	case "":
		return detectAnswerLanguage(transcript)
	default:
		return language
	}
}

func interviewTopicRelevance(question *domain.InterviewQuestion, answer string) int {
	if question == nil {
		return 0
	}
	score := ai.RootCauseMatch(answer, strings.Join([]string{question.Title, question.Description, question.ReferenceAnswer}, "\n"), question.ReferenceKeywords)
	if hits := len(interviewKeywordHits(question, answer)); hits > 0 {
		bonus := 10 + hits*8
		if score < bonus {
			score = bonus
		}
	}
	if score > 100 {
		return 100
	}
	return score
}

func interviewKeywordHits(question *domain.InterviewQuestion, answer string) []string {
	if question == nil {
		return []string{}
	}
	hits := []string{}
	seen := map[string]bool{}
	keywords := interviewTerminologyLexicon(question)
	for _, keyword := range keywords {
		normalized := strings.TrimSpace(keyword)
		if normalized == "" || seen[strings.ToLower(normalized)] {
			continue
		}
		if ai.ContainsAny(answer, []string{normalized}) {
			seen[strings.ToLower(normalized)] = true
			hits = append(hits, normalized)
		}
	}
	return hits
}

type terminologyCandidate struct {
	Canonical string
	Aliases   []string
}

func interviewSTTLanguageHint(question *domain.InterviewQuestion) string {
	if question == nil {
		return "zh"
	}
	if ai.ContainsAny(question.Title+" "+question.Description, []string{"English", "鑻辨枃", "鑻辫"}) {
		return "en"
	}
	return "zh"
}

func buildInterviewSTTPrompt(question *domain.InterviewQuestion) string {
	terms := interviewTerminologyLexicon(question)
	if len(terms) == 0 {
		return ""
	}
	preview := strings.Join(terms, ", ")
	runes := []rune(preview)
	if len(runes) > 220 {
		preview = string(runes[:220])
	}
	return "杩欐槸鎶€鏈潰璇曡闊宠浆鍐欙紝璇蜂紭鍏堜繚鐣欎笓涓氭湳璇師鏂囷紝涓嶈缈绘垚涓枃璋愰煶銆傞噸鐐规湳璇寘鎷細" + preview
}

func interviewTerminologyLexicon(question *domain.InterviewQuestion) []string {
	seen := map[string]bool{}
	terms := []string{}
	appendTerm := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		terms = append(terms, value)
	}
	for _, value := range []string{"MySQL", "nginx", "Nginx", "EXPLAIN", "SQL", "索引", "慢查询", "执行计划", "回滚", "灰度", "slow log"} {
		appendTerm(value)
	}
	if question != nil {
		appendTerm(question.Domain)
		for _, keyword := range question.ReferenceKeywords {
			appendTerm(keyword)
		}
		for _, value := range splitTerminologyText(question.Title + " " + question.Description + " " + question.ReferenceAnswer) {
			appendTerm(value)
		}
	}
	return terms
}

func splitTerminologyText(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= 'A' && r <= 'Z':
			return false
		case r >= '0' && r <= '9':
			return false
		case r == '+' || r == '#' || r == '-' || r == '_':
			return false
		default:
			return true
		}
	})
	out := []string{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) >= 3 {
			out = append(out, field)
		}
	}
	return out
}

func detectInterviewTermSuggestions(question *domain.InterviewQuestion, transcript string) []domain.TranscriptSuggestion {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil
	}
	candidates := interviewTerminologyCandidates(question)
	suggestions := []domain.TranscriptSuggestion{}
	seen := map[string]bool{}
	lowerTranscript := strings.ToLower(transcript)
	for _, candidate := range candidates {
		if ai.ContainsAny(transcript, []string{candidate.Canonical}) {
			continue
		}
		for _, alias := range candidate.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" || !strings.Contains(lowerTranscript, strings.ToLower(alias)) {
				continue
			}
			key := strings.ToLower(candidate.Canonical) + "|" + strings.ToLower(alias)
			if seen[key] {
				continue
			}
			seen[key] = true
			suggestions = append(suggestions, domain.TranscriptSuggestion{
				Original:  alias,
				Suggested: candidate.Canonical,
				Reason:    "妫€娴嬪埌鍙兘鐨勪腑鏂囪皭闊虫垨鎷嗗啓鏈",
			})
			break
		}
	}
	return suggestions
}

func interviewTerminologyCandidates(question *domain.InterviewQuestion) []terminologyCandidate {
	candidates := []terminologyCandidate{
		{Canonical: "nginx", Aliases: []string{"恩金克斯", "恩静克斯", "engine x", "enginex"}},
		{Canonical: "MySQL", Aliases: []string{"买SQL", "买sql", "my sql", "麦SQL", "mysql"}},
		{Canonical: "EXPLAIN", Aliases: []string{"explain", "xplain", "解释计划"}},
	}
	lexicon := interviewTerminologyLexicon(question)
	if ai.ContainsAny(strings.Join(lexicon, " "), []string{"Redis"}) {
		candidates = append(candidates, terminologyCandidate{Canonical: "Redis", Aliases: []string{"瑞迪斯", "redis"}})
	}
	return candidates
}

func inferSubmissionSource(content, transcript string) string {
	content = strings.TrimSpace(content)
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return "text"
	}
	if content == "" || content == transcript || ai.Similarity(content, transcript) >= 0.82 {
		return "voice_transcript"
	}
	return "voice_edited"
}

func mockVoiceTranscriptDraft(asset *domain.Asset, session *domain.InterviewSession) string {
	filename := "语音答案"
	if asset != nil && strings.TrimSpace(asset.Filename) != "" {
		filename = asset.Filename
	}
	round := 1
	if session != nil && session.CurrentRound > 0 {
		round = session.CurrentRound
	}
	return fmt.Sprintf("第 %d 轮 %s 转写草稿：我会先定位 MySQL 慢查询，查看 slow log 和 EXPLAIN 执行计划，核对索引覆盖情况，再给出灰度修复、回滚和验证方案。", round, filename)
}

func mimeTypeFromFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".m4a":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	default:
		return "application/octet-stream"
	}
}

func assetMetadataURL(assetID string) string {
	return "/api/v1/assets/" + strings.TrimSpace(assetID)
}

func assetContentURL(assetID string) string {
	return assetMetadataURL(assetID) + "?content=1"
}

func normalizeAssetURLs(asset domain.Asset) domain.Asset {
	asset.ID = strings.TrimSpace(asset.ID)
	if strings.TrimSpace(asset.URL) == "" || strings.Contains(asset.URL, "?content=1") {
		asset.URL = assetMetadataURL(asset.ID)
	}
	if strings.TrimSpace(asset.ContentURL) == "" {
		asset.ContentURL = assetContentURL(asset.ID)
	}
	return asset
}

func hydrateInterviewSubmissionAssets(dataStore store.Store, session *domain.InterviewSession) {
	if dataStore == nil || session == nil {
		return
	}
	for index := range session.Submissions {
		submission := &session.Submissions[index]
		if submission.Asset != nil {
			normalized := normalizeAssetURLs(*submission.Asset)
			submission.Asset = &normalized
			if strings.TrimSpace(submission.AssetID) == "" {
				submission.AssetID = normalized.ID
			}
			submission.AssetURL = normalized.ContentURL
			continue
		}
		if strings.TrimSpace(submission.AssetID) == "" {
			continue
		}
		asset, ok := dataStore.GetAsset(submission.AssetID)
		if !ok {
			continue
		}
		normalized := normalizeAssetURLs(*asset)
		submission.Asset = &normalized
		submission.AssetURL = normalized.ContentURL
	}
}

func localAssetRoot() string {
	if root := strings.TrimSpace(os.Getenv("ASSET_STORAGE_DIR")); root != "" {
		return root
	}
	return filepath.Join(".", "data", "assets")
}

func assetStorageKey(userID, assetID, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if !voiceFileExtensions[ext] {
		ext = ".bin"
	}
	return filepath.ToSlash(filepath.Join("voice", safePathSegment(userID), assetID+ext))
}

func localAssetPath(storageKey string) (string, error) {
	cleanKey := filepath.Clean(filepath.FromSlash(strings.TrimSpace(storageKey)))
	if cleanKey == "." || cleanKey == "" || filepath.IsAbs(cleanKey) || strings.HasPrefix(cleanKey, "..") {
		return "", errInvalidStorageKey
	}
	root, err := filepath.Abs(localAssetRoot())
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, cleanKey))
	if err != nil {
		return "", err
	}
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", errInvalidStorageKey
	}
	return target, nil
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "anonymous"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return builder.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func scoreIf(answer string, keywords []string, points int) int {
	if ai.ContainsAny(answer, keywords) {
		return points
	}
	return 0
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "待补充"
	}
	if len([]rune(text)) > 42 {
		return string([]rune(text)[:42]) + "..."
	}
	return text
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
