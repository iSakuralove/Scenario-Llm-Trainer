package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"situational-teaching/backend/internal/domain"
	"strconv"
	"strings"
	"time"
)

type STTConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
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
