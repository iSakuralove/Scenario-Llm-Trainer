package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	DefaultEmbeddingBaseURL       = "https://jeniya.top"
	DefaultEmbeddingModel         = "text-embedding-3-small"
	DefaultEmbeddingFallbackModel = ""
)

type EmbeddingConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	FallbackModel string
	Timeout       time.Duration
}

type EmbeddingResult struct {
	Model        string
	FallbackUsed bool
	Vectors      [][]float64
}

type EmbeddingClient interface {
	Embed(context.Context, []string) (EmbeddingResult, error)
}

type OpenAIEmbeddingClient struct {
	baseURL       string
	apiKey        string
	model         string
	fallbackModel string
	client        *http.Client
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func EmbeddingConfigFromEnv() EmbeddingConfig {
	timeout := time.Duration(parseInt(os.Getenv("EMBEDDING_TIMEOUT_SECONDS"), 8)) * time.Second
	return NormalizeEmbeddingConfig(EmbeddingConfig{
		BaseURL:       os.Getenv("EMBEDDING_BASE_URL"),
		APIKey:        firstNonEmptyEmbedding(os.Getenv("EMBEDDING_API_KEY"), os.Getenv("jeniya_embedding_key"), os.Getenv("JIANYI_API_KEY")),
		Model:         os.Getenv("EMBEDDING_MODEL"),
		FallbackModel: os.Getenv("EMBEDDING_FALLBACK_MODEL"),
		Timeout:       timeout,
	})
}

func NormalizeEmbeddingConfig(cfg EmbeddingConfig) EmbeddingConfig {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.FallbackModel = strings.TrimSpace(cfg.FallbackModel)
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultEmbeddingBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultEmbeddingModel
	}
	if cfg.FallbackModel == "" {
		cfg.FallbackModel = DefaultEmbeddingFallbackModel
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	return cfg
}

func NewEmbeddingClientFromEnv() EmbeddingClient {
	cfg := EmbeddingConfigFromEnv()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil
	}
	return NewOpenAIEmbeddingClient(cfg)
}

func NewOpenAIEmbeddingClient(cfg EmbeddingConfig) *OpenAIEmbeddingClient {
	cfg = NormalizeEmbeddingConfig(cfg)
	return &OpenAIEmbeddingClient{
		baseURL:       cfg.BaseURL,
		apiKey:        cfg.APIKey,
		model:         cfg.Model,
		fallbackModel: cfg.FallbackModel,
		client:        &http.Client{Timeout: cfg.Timeout},
	}
}

func (c *OpenAIEmbeddingClient) Embed(ctx context.Context, input []string) (EmbeddingResult, error) {
	if c == nil {
		return EmbeddingResult{}, fmt.Errorf("embedding client is nil")
	}
	cleaned := make([]string, 0, len(input))
	for _, item := range input {
		cleaned = append(cleaned, strings.TrimSpace(item))
	}
	if len(cleaned) == 0 {
		return EmbeddingResult{}, fmt.Errorf("embedding input is empty")
	}
	models := []string{c.model}
	if c.fallbackModel != "" && c.fallbackModel != c.model {
		models = append(models, c.fallbackModel)
	}
	var lastErr error
	for i, model := range models {
		result, err := c.embedWithModel(ctx, model, cleaned)
		if err == nil {
			result.FallbackUsed = i > 0
			return result, nil
		}
		lastErr = err
	}
	return EmbeddingResult{}, lastErr
}

func (c *OpenAIEmbeddingClient) embedWithModel(ctx context.Context, model string, input []string) (EmbeddingResult, error) {
	payload, err := json.Marshal(embeddingRequest{Model: model, Input: input})
	if err != nil {
		return EmbeddingResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return EmbeddingResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		return EmbeddingResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return EmbeddingResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EmbeddingResult{}, fmt.Errorf("embedding provider returned status %d", resp.StatusCode)
	}
	var parsed embeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return EmbeddingResult{}, err
	}
	if parsed.Error != nil {
		return EmbeddingResult{}, fmt.Errorf("embedding provider error")
	}
	if len(parsed.Data) != len(input) {
		return EmbeddingResult{}, fmt.Errorf("embedding response count mismatch")
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	vectors := make([][]float64, len(parsed.Data))
	for i, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(input) || len(item.Embedding) == 0 {
			return EmbeddingResult{}, fmt.Errorf("embedding response is malformed")
		}
		vectors[i] = item.Embedding
	}
	return EmbeddingResult{Model: model, Vectors: vectors}, nil
}

func CosineSimilarity(left, right []float64) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for i := range left {
		dot += left[i] * right[i]
		leftNorm += left[i] * left[i]
		rightNorm += right[i] * right[i]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	score := dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func firstNonEmptyEmbedding(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
