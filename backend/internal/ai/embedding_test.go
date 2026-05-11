package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIEmbeddingClientFallsBackToSecondaryModel(t *testing.T) {
	models := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing authorization header: %s", r.Header.Get("Authorization"))
		}
		var body embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		models = append(models, body.Model)
		if body.Model == "text-embedding-3-small" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"model is not available on embeddings endpoint"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"index": 0, "embedding": []float64{1, 0, 0}},
				{"index": 1, "embedding": []float64{0, 1, 0}},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAIEmbeddingClient(EmbeddingConfig{
		BaseURL:       server.URL,
		APIKey:        "test-key",
		Model:         "text-embedding-3-small",
		FallbackModel: "text-embedding-3-large",
		Timeout:       time.Second,
	})
	result, err := client.Embed(t.Context(), []string{"left", "right"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 || models[0] != "text-embedding-3-small" || models[1] != "text-embedding-3-large" {
		t.Fatalf("unexpected model attempts: %#v", models)
	}
	if !result.FallbackUsed || result.Model != "text-embedding-3-large" {
		t.Fatalf("expected fallback model metadata: %#v", result)
	}
	if len(result.Vectors) != 2 || result.Vectors[0][0] != 1 || result.Vectors[1][1] != 1 {
		t.Fatalf("unexpected vectors: %#v", result.Vectors)
	}
}

func TestOpenAIEmbeddingClientRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"index": 0, "embedding": []float64{1}},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAIEmbeddingClient(EmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "primary",
		Timeout: time.Second,
	})
	_, err := client.Embed(t.Context(), []string{"left", "right"})
	if err == nil {
		t.Fatal("expected malformed response error")
	}
}

func TestEmbeddingConfigFromEnvPrefersDedicatedJianyiEmbeddingKey(t *testing.T) {
	t.Setenv("EMBEDDING_API_KEY", "")
	t.Setenv("jeniya_embedding_key", "dedicated-key")
	t.Setenv("JIANYI_API_KEY", "general-key")

	cfg := EmbeddingConfigFromEnv()

	if cfg.APIKey != "dedicated-key" {
		t.Fatalf("expected dedicated embedding key, got %q", cfg.APIKey)
	}
}
