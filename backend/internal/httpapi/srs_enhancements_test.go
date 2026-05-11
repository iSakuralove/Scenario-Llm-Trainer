package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

type captureSTTProvider struct {
	result  STTResult
	err     error
	lastReq STTRequest
	calls   int
}

func (p *captureSTTProvider) Transcribe(_ context.Context, req STTRequest) (STTResult, error) {
	p.calls++
	p.lastReq = req
	return p.result, p.err
}

func TestNewSTTProviderFromEnvPrefersZetaDefaults(t *testing.T) {
	t.Setenv("STT_BASE_URL", "")
	t.Setenv("STT_API_KEY", "")
	t.Setenv("STT_MODEL", "")
	t.Setenv("ZETA_KEY", "zeta-test-key")
	t.Setenv("JIANYI_API_KEY", "jianyi-test-key")

	provider, ok := NewSTTProviderFromEnv(nil).(*OpenAITranscriptionProvider)
	if !ok {
		t.Fatalf("expected openai transcription provider, got %T", provider)
	}
	if provider.baseURL != defaultZetaSTTBaseURL {
		t.Fatalf("expected zeta base url %q, got %q", defaultZetaSTTBaseURL, provider.baseURL)
	}
	if provider.model != defaultZetaSTTModel {
		t.Fatalf("expected zeta stt model %q, got %q", defaultZetaSTTModel, provider.model)
	}
	if provider.apiKey != "zeta-test-key" {
		t.Fatalf("expected zeta key to be preferred, got %q", provider.apiKey)
	}
}

func TestNewSTTProviderFromEnvAllowsExplicitSTTOverrides(t *testing.T) {
	t.Setenv("STT_BASE_URL", "https://api.zetatechs.online/")
	t.Setenv("STT_API_KEY", "explicit-stt-key")
	t.Setenv("STT_MODEL", "custom-transcribe")
	t.Setenv("ZETA_KEY", "zeta-test-key")
	t.Setenv("JIANYI_API_KEY", "jianyi-test-key")

	provider, ok := NewSTTProviderFromEnv(nil).(*OpenAITranscriptionProvider)
	if !ok {
		t.Fatalf("expected openai transcription provider, got %T", provider)
	}
	if provider.baseURL != "https://api.zetatechs.online" {
		t.Fatalf("expected trimmed override base url, got %q", provider.baseURL)
	}
	if provider.model != "custom-transcribe" {
		t.Fatalf("expected override stt model, got %q", provider.model)
	}
	if provider.apiKey != "explicit-stt-key" {
		t.Fatalf("expected explicit stt key to win, got %q", provider.apiKey)
	}
}

func TestNewSTTProviderFromEnvKeepsJianyiFallback(t *testing.T) {
	t.Setenv("STT_BASE_URL", "")
	t.Setenv("STT_API_KEY", "")
	t.Setenv("STT_MODEL", "")
	t.Setenv("ZETA_KEY", "")
	t.Setenv("JIANYI_API_KEY", "jianyi-test-key")

	provider, ok := NewSTTProviderFromEnv(nil).(*OpenAITranscriptionProvider)
	if !ok {
		t.Fatalf("expected openai transcription provider, got %T", provider)
	}
	if provider.baseURL != defaultJianyiSTTBaseURL {
		t.Fatalf("expected jianyi base url %q, got %q", defaultJianyiSTTBaseURL, provider.baseURL)
	}
	if provider.model != defaultJianyiSTTModel {
		t.Fatalf("expected jianyi stt model %q, got %q", defaultJianyiSTTModel, provider.model)
	}
	if provider.apiKey != "jianyi-test-key" {
		t.Fatalf("expected jianyi key fallback, got %q", provider.apiKey)
	}
}

func TestVoiceAssetMultipartUploadValidationAndRead(t *testing.T) {
	t.Setenv("ASSET_STORAGE_DIR", t.TempDir())
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestMultipartAsset(t, handler, token, "answer.webm", "audio/webm", []byte("voice-bytes"))
	if status != http.StatusOK {
		t.Fatalf("audio upload status=%d message=%s", status, env.Message)
	}
	var asset domain.Asset
	mustDecodeData(t, env, &asset)
	if asset.ID == "" || asset.Checksum == "" || asset.Size != int64(len("voice-bytes")) || asset.ContentURL == "" {
		t.Fatalf("unexpected uploaded asset: %+v", asset)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+asset.ID+"?content=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "voice-bytes" {
		t.Fatalf("asset content status=%d body=%q", rr.Code, rr.Body.String())
	}

	status, env = requestMultipartAsset(t, handler, token, "bad.mp4", "video/mp4", []byte("not-audio"))
	if status != http.StatusUnsupportedMediaType || env.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected video rejection, status=%d env=%+v", status, env)
	}

	status, env = requestMultipartAsset(t, handler, token, "screen-recording.webm", "video/webm", []byte("not-audio"))
	if status != http.StatusUnsupportedMediaType || env.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected webm video rejection, status=%d env=%+v", status, env)
	}

	status, env = requestMultipartAsset(t, handler, token, "voice", "audio/webm", []byte("missing-extension"))
	if status != http.StatusUnsupportedMediaType || env.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected missing extension rejection, status=%d env=%+v", status, env)
	}

	status, env = requestMultipartAsset(t, handler, token, "empty.webm", "audio/webm", []byte{})
	if status != http.StatusBadRequest || env.Code != http.StatusBadRequest {
		t.Fatalf("expected empty upload rejection, status=%d env=%+v", status, env)
	}
}

func requestMultipartAsset(t *testing.T, handler http.Handler, token, filename, mimeType string, payload []byte) (int, testEnvelope) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("kind", "voice"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="` + filename + `"`},
		"Content-Type":        {mimeType},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var env testEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	return rr.Code, env
}

func TestInterviewVoiceAssetTranscriptAndReport(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/assets", token, map[string]interface{}{
		"kind":      "voice",
		"filename":  "answer.webm",
		"mime_type": "audio/webm",
		"size":      4096,
	})
	if status != http.StatusOK {
		t.Fatalf("asset status=%d message=%s", status, env.Message)
	}
	var asset domain.Asset
	mustDecodeData(t, env, &asset)
	if asset.ID == "" || asset.URL == "" || asset.ContentURL == "" {
		t.Fatalf("unexpected asset: %+v", asset)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+created.SessionID+"/voice", token, map[string]interface{}{
		"asset_id":         asset.ID,
		"duration_seconds": 18,
	})
	if status != http.StatusOK {
		t.Fatalf("voice status=%d message=%s", status, env.Message)
	}
	var voice struct {
		Transcript string                    `json:"transcript"`
		Status     string                    `json:"status"`
		Quality    domain.VoiceQualityResult `json:"quality"`
	}
	mustDecodeData(t, env, &voice)
	if voice.Status != "draft_ready" || voice.Transcript == "" || voice.Quality.TopicRelevanceScore == 0 {
		t.Fatalf("unexpected voice transcript: %+v", voice)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+created.SessionID+"/submit", token, map[string]interface{}{
		"type":                 "voice",
		"content":              voice.Transcript,
		"transcript":           voice.Transcript,
		"asset_id":             asset.ID,
		"duration_seconds":     18,
		"confirmed_transcript": true,
	})
	if status != http.StatusOK {
		t.Fatalf("voice submit status=%d message=%s", status, env.Message)
	}
	var submitted struct {
		Session domain.InterviewSession `json:"session"`
	}
	mustDecodeData(t, env, &submitted)
	if len(submitted.Session.Submissions) != 1 || submitted.Session.Submissions[0].Type != "voice" || submitted.Session.Submissions[0].AssetID != asset.ID {
		t.Fatalf("unexpected voice submission: %+v", submitted.Session.Submissions)
	}
	if submitted.Session.Submissions[0].Asset == nil || submitted.Session.Submissions[0].Asset.ContentURL == "" {
		t.Fatalf("expected asset snapshot in submission: %+v", submitted.Session.Submissions[0])
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/interviews/sessions/"+created.SessionID+"/report", token, nil)
	if status != http.StatusOK {
		t.Fatalf("report status=%d message=%s", status, env.Message)
	}
	var report struct {
		Session domain.InterviewSession `json:"session"`
	}
	mustDecodeData(t, env, &report)
	if len(report.Session.Submissions) != 1 || report.Session.Submissions[0].Asset == nil || report.Session.Submissions[0].Asset.ContentURL == "" {
		t.Fatalf("expected report asset evidence chain: %+v", report.Session.Submissions)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+created.SessionID+"/submit", token, map[string]interface{}{
		"type":                 "voice",
		"content":              "伪造资产提交",
		"transcript":           "伪造资产提交",
		"asset_id":             "asset-not-found",
		"confirmed_transcript": true,
	})
	if status != http.StatusNotFound || env.Code != http.StatusNotFound {
		t.Fatalf("expected invalid asset rejection, status=%d env=%+v", status, env)
	}
}

func TestInterviewVoiceRejectsIrrelevantEnglishWithoutScoring(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	asset := createVoiceAssetForTest(t, handler, token, "english.webm")
	irrelevant := "Today I want to talk about travel plans, music, movies and weekend shopping with friends."

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/voice", token, map[string]interface{}{
		"asset_id":         asset.ID,
		"transcript":       irrelevant,
		"duration_seconds": 15,
	})
	if status != http.StatusOK || env.Code != http.StatusOK {
		t.Fatalf("expected transcript draft with quality warning, status=%d env=%+v", status, env)
	}
	var voiceDraft struct {
		Status  string                    `json:"status"`
		Quality domain.VoiceQualityResult `json:"quality"`
	}
	mustDecodeData(t, env, &voiceDraft)
	if voiceDraft.Status != "rejected" || len(voiceDraft.Quality.Reasons) == 0 {
		t.Fatalf("expected rejected quality in transcript draft: %+v", voiceDraft)
	}

	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]interface{}{
		"type":                 "voice",
		"content":              irrelevant,
		"transcript":           irrelevant,
		"asset_id":             asset.ID,
		"confirmed_transcript": true,
	})
	if status != http.StatusUnprocessableEntity || env.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected irrelevant voice rejection during submit, status=%d env=%+v", status, env)
	}

	session, ok := dataStore.GetInterviewSession(sessionID)
	if !ok {
		t.Fatal("missing interview session")
	}
	if len(session.Submissions) != 0 || len(session.Evaluations) != 0 || session.Status != "question_presented" {
		t.Fatalf("invalid voice should not change session: %+v", session)
	}
}

func TestInterviewVoiceRequiresTranscriptConfirmation(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	asset := createVoiceAssetForTest(t, handler, token, "answer.webm")
	transcript := "我会先定位 MySQL 慢查询日志，再使用 EXPLAIN 验证索引覆盖，并准备灰度发布和回滚验证。"

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]interface{}{
		"type":       "voice",
		"content":    transcript,
		"transcript": transcript,
		"asset_id":   asset.ID,
	})
	if status != http.StatusBadRequest || env.Code != http.StatusBadRequest {
		t.Fatalf("expected transcript confirmation rejection, status=%d env=%+v", status, env)
	}
	session, ok := dataStore.GetInterviewSession(sessionID)
	if !ok {
		t.Fatal("missing interview session")
	}
	if len(session.Submissions) != 0 || len(session.Evaluations) != 0 {
		t.Fatalf("unconfirmed voice should not score: %+v", session)
	}
}

func TestInterviewRejectsCompletedSessionResubmit(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	session, ok := dataStore.GetInterviewSession(sessionID)
	if !ok {
		t.Fatal("missing interview session")
	}
	session.Status = "final_evaluated"
	session.FinalScore = 80
	dataStore.SaveInterviewSession(session)

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]interface{}{
		"type":    "text",
		"content": "再次提交应该被拒绝，避免报告出现重复轮次。",
	})
	if status != http.StatusConflict || env.Code != http.StatusConflict {
		t.Fatalf("expected completed session rejection, status=%d env=%+v", status, env)
	}
}

func TestInterviewVoiceAllowsTechnicalEnglishAndMarksEditedSource(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	asset := createVoiceAssetForTest(t, handler, token, "technical.webm")
	transcript := "I will inspect MySQL slow query logs, run EXPLAIN, check index coverage, review execution plan, rollback risky changes and verify with gray release."

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/voice", token, map[string]interface{}{
		"asset_id":         asset.ID,
		"transcript":       transcript,
		"duration_seconds": 20,
	})
	if status != http.StatusOK {
		t.Fatalf("technical english voice should pass status=%d message=%s", status, env.Message)
	}
	var voice struct {
		Status  string                    `json:"status"`
		Quality domain.VoiceQualityResult `json:"quality"`
	}
	mustDecodeData(t, env, &voice)
	if voice.Status != "needs_review" || len(voice.Quality.KeywordHits) < 2 {
		t.Fatalf("unexpected voice quality: %+v", voice.Quality)
	}

	edited := "我会按线上 MySQL 慢查询定位：先看接口耗时和 slow log，再用 EXPLAIN 查看执行计划与索引覆盖，最后灰度加索引并准备回滚验证。"
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]interface{}{
		"type":                 "voice",
		"content":              edited,
		"transcript":           transcript,
		"asset_id":             asset.ID,
		"duration_seconds":     20,
		"confirmed_transcript": true,
	})
	if status != http.StatusOK {
		t.Fatalf("edited voice submit status=%d message=%s", status, env.Message)
	}
	var submitted struct {
		Session domain.InterviewSession `json:"session"`
	}
	mustDecodeData(t, env, &submitted)
	if len(submitted.Session.Submissions) != 1 {
		t.Fatalf("expected one submission: %+v", submitted.Session.Submissions)
	}
	item := submitted.Session.Submissions[0]
	if item.Type != "text" || item.Source != "voice_edited" || item.VoiceQuality == nil {
		t.Fatalf("edited voice should be stored as text with voice_edited source: %+v", item)
	}
}

func TestInterviewVoiceSuggestsTechnicalTermCorrections(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	asset := createVoiceAssetForTest(t, handler, token, "term-alias.webm")
	transcript := "我会先看恩金克斯访问日志，再排查买SQL慢查询，最后用 explain 验证索引。"

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/voice", token, map[string]interface{}{
		"asset_id":         asset.ID,
		"transcript":       transcript,
		"duration_seconds": 18,
	})
	if status != http.StatusOK {
		t.Fatalf("technical alias voice should pass status=%d message=%s", status, env.Message)
	}
	var voice struct {
		Transcript string                    `json:"transcript"`
		Status     string                    `json:"status"`
		Quality    domain.VoiceQualityResult `json:"quality"`
	}
	mustDecodeData(t, env, &voice)
	if voice.Status != "needs_review" {
		t.Fatalf("expected needs_review status for technical term corrections: %+v", voice)
	}
	if len(voice.Quality.TranscriptSuggestions) < 2 {
		t.Fatalf("expected transcript suggestions: %+v", voice.Quality)
	}
	if voice.Quality.TranscriptSuggestions[0].Suggested == "" || voice.Quality.TranscriptSuggestions[0].Original == "" {
		t.Fatalf("expected populated transcript suggestion fields: %+v", voice.Quality.TranscriptSuggestions)
	}

	corrected := "我会先看 nginx 访问日志，再排查 MySQL 慢查询，最后用 EXPLAIN 验证索引。"
	status, env = requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/submit", token, map[string]interface{}{
		"type":                 "voice",
		"content":              corrected,
		"transcript":           transcript,
		"asset_id":             asset.ID,
		"duration_seconds":     18,
		"confirmed_transcript": true,
	})
	if status != http.StatusOK {
		t.Fatalf("corrected voice submit status=%d message=%s", status, env.Message)
	}
	var submitted struct {
		Session domain.InterviewSession `json:"session"`
	}
	mustDecodeData(t, env, &submitted)
	if len(submitted.Session.Submissions) != 1 {
		t.Fatalf("expected one submission: %+v", submitted.Session.Submissions)
	}
	item := submitted.Session.Submissions[0]
	if item.Transcript != transcript {
		t.Fatalf("expected original transcript to be retained for evidence chain: %+v", item)
	}
	if item.Source != "voice_edited" || item.VoiceQuality == nil || len(item.VoiceQuality.TranscriptSuggestions) < 2 {
		t.Fatalf("expected edited submission with retained transcript suggestions: %+v", item)
	}
}

func TestInterviewVoiceReturnsTerminologySuggestionsForHomophoneTranscript(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	stt := &captureSTTProvider{
		result: STTResult{
			Transcript:       "我会先看恩金克斯访问日志，再排查买SQL慢查询，最后用 explain 判断索引和执行计划。",
			DetectedLanguage: "zh",
			Confidence:       0.93,
			Status:           "transcribed",
		},
	}
	server.stt = stt
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	asset := createVoiceAssetForTest(t, handler, token, "homophone.webm")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/voice", token, map[string]interface{}{
		"asset_id":         asset.ID,
		"duration_seconds": 26,
	})
	if status != http.StatusOK {
		t.Fatalf("voice status=%d message=%s", status, env.Message)
	}
	var voice struct {
		Transcript string                    `json:"transcript"`
		Status     string                    `json:"status"`
		Quality    domain.VoiceQualityResult `json:"quality"`
	}
	mustDecodeData(t, env, &voice)
	if voice.Transcript != stt.result.Transcript {
		t.Fatalf("transcript should keep original stt output, got %q", voice.Transcript)
	}
	if voice.Status != "needs_review" && voice.Status != "draft_ready" {
		t.Fatalf("unexpected voice status: %q", voice.Status)
	}
	if stt.calls != 1 {
		t.Fatalf("expected one stt call, got %d", stt.calls)
	}
	if stt.lastReq.Language != "zh" {
		t.Fatalf("expected zh guidance language, got %q", stt.lastReq.Language)
	}
	if stt.lastReq.Prompt == "" {
		t.Fatal("expected terminology guidance prompt")
	}
	if !strings.Contains(stt.lastReq.Prompt, "MySQL") || !strings.Contains(stt.lastReq.Prompt, "nginx") {
		t.Fatalf("prompt should include merged technical terms, got %q", stt.lastReq.Prompt)
	}
	if len(voice.Quality.TranscriptSuggestions) < 2 {
		t.Fatalf("expected term suggestions, got %+v", voice.Quality)
	}
	foundMySQL := false
	foundNginx := false
	for _, item := range voice.Quality.TranscriptSuggestions {
		if item.Suggested == "MySQL" && strings.Contains(item.Original, "买SQL") {
			foundMySQL = true
		}
		if item.Suggested == "nginx" && strings.Contains(item.Original, "恩金克斯") {
			foundNginx = true
		}
	}
	if !foundMySQL || !foundNginx {
		t.Fatalf("missing expected term suggestions: %+v", voice.Quality.TranscriptSuggestions)
	}
}

func TestInterviewVoiceReportsSTTChannelUnavailableWithoutChangingSession(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	server := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour))
	stt := &captureSTTProvider{
		err: STTProviderError{
			StatusCode:      http.StatusServiceUnavailable,
			ProviderType:    "new_api_error",
			ProviderMessage: "分组 Codex专属 下模型 gpt-4o-mini-transcribe 无可用渠道（distributor）",
		},
	}
	server.stt = stt
	handler := server.Handler()
	token := loginToken(t, handler, "demo", "demo123")
	sessionID := createInterviewSessionForTest(t, handler, token)
	asset := createVoiceAssetForTest(t, handler, token, "channel-unavailable.webm")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions/"+sessionID+"/voice", token, map[string]interface{}{
		"asset_id":         asset.ID,
		"duration_seconds": 28,
	})
	if status != http.StatusBadGateway || env.Code != http.StatusBadGateway {
		t.Fatalf("expected stt service failure, status=%d env=%+v", status, env)
	}
	if !strings.Contains(env.Message, "无可用通道") || !strings.Contains(env.Message, "STT_MODEL") {
		t.Fatalf("expected actionable stt channel message, got %q", env.Message)
	}
	if strings.Contains(env.Message, "sk-") {
		t.Fatalf("stt error message must not expose api key: %q", env.Message)
	}
	if stt.calls != 1 {
		t.Fatalf("expected one stt call, got %d", stt.calls)
	}
	session, ok := dataStore.GetInterviewSession(sessionID)
	if !ok {
		t.Fatal("missing interview session")
	}
	if len(session.Submissions) != 0 || len(session.Evaluations) != 0 || session.Status != "question_presented" {
		t.Fatalf("stt failure should not score or advance session: %+v", session)
	}
}

func createInterviewSessionForTest(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/interviews/sessions", token, map[string]string{
		"domain":        "database",
		"difficulty":    "L3",
		"question_type": "scenario_analysis",
	})
	if status != http.StatusOK {
		t.Fatalf("create interview status=%d message=%s", status, env.Message)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	mustDecodeData(t, env, &created)
	return created.SessionID
}

func createVoiceAssetForTest(t *testing.T, handler http.Handler, token, filename string) domain.Asset {
	t.Helper()
	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/assets", token, map[string]interface{}{
		"kind":      "voice",
		"filename":  filename,
		"mime_type": "audio/webm",
		"size":      4096,
	})
	if status != http.StatusOK {
		t.Fatalf("asset status=%d message=%s", status, env.Message)
	}
	var asset domain.Asset
	mustDecodeData(t, env, &asset)
	return asset
}

func TestAdminPromptAIConfigAuditAndSystemStatus(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	demoToken := loginToken(t, handler, "demo", "demo123")
	adminToken := loginToken(t, handler, "admin", "admin123")

	_, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/prompts", demoToken, nil)
	if env.Code != http.StatusForbidden {
		t.Fatalf("student prompt access code=%d", env.Code)
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/admin/prompts", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("prompts status=%d message=%s", status, env.Message)
	}
	var promptList struct {
		List []domain.PromptTemplate `json:"list"`
	}
	mustDecodeData(t, env, &promptList)
	if len(promptList.List) == 0 {
		t.Fatal("expected seeded prompt templates")
	}

	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/admin/prompts/scenario_reply", adminToken, map[string]string{
		"content": "在不泄露答案的前提下输出 JSON reply 字段。",
	})
	if status != http.StatusOK {
		t.Fatalf("prompt update status=%d message=%s", status, env.Message)
	}
	t.Cleanup(func() {
		_, _ = requestJSON(t, handler, http.MethodPut, "/api/v1/admin/prompts/scenario_reply", adminToken, map[string]interface{}{
			"reset_default": true,
		})
	})
	var updatedPrompt domain.PromptTemplate
	mustDecodeData(t, env, &updatedPrompt)
	if !updatedPrompt.IsModified {
		t.Fatalf("expected modified prompt: %+v", updatedPrompt)
	}
	if updatedPrompt.RenderEngine == "" {
		t.Fatalf("expected prompt render engine metadata: %+v", updatedPrompt)
	}
	rawPromptContent := updatedPrompt.Content
	if rawPromptContent == "" {
		t.Fatal("expected admin prompt endpoint to return editable prompt content")
	}

	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/admin/ai-config", adminToken, map[string]interface{}{
		"provider":       "deepseek",
		"model":          "deepseek-chat",
		"temperature":    0.35,
		"top_p":          0.8,
		"top_k":          24,
		"max_tokens":     3072,
		"stream_enabled": true,
		"fallback_model": "mock",
	})
	if status != http.StatusOK {
		t.Fatalf("ai config update status=%d message=%s", status, env.Message)
	}
	var cfg domain.AIConfig
	mustDecodeData(t, env, &cfg)
	if cfg.Provider != "deepseek" || cfg.UpdatedBy != "user-admin" {
		t.Fatalf("unexpected ai config: %+v", cfg)
	}
	if cfg.Temperature != 0.35 || cfg.TopP != 0.8 || cfg.TopK != 24 || cfg.MaxTokens != 3072 {
		t.Fatalf("expected sampling params in response: %+v", cfg)
	}
	if info := dataStore.GetAIConfig(); info.Provider != "deepseek" || info.Model != "deepseek-chat" || info.Temperature != 0.35 || info.TopP != 0.8 || info.TopK != 24 || info.MaxTokens != 3072 {
		t.Fatalf("stored ai config not updated: %+v", info)
	}
	for i := 0; i < 501; i++ {
		_, err := dataStore.CreateAIJob(domain.AIJob{
			ID:        fmt.Sprintf("status-count-job-%03d", i),
			UserID:    "user-admin",
			Kind:      "scenario_generation",
			Status:    "completed",
			Stage:     "completed",
			Progress:  100,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		})
		if err != nil {
			t.Fatalf("create ai job %d: %v", i, err)
		}
	}

	status, env = requestJSON(t, handler, http.MethodGet, "/api/v1/system/status", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("system status=%d message=%s", status, env.Message)
	}
	var system struct {
		PromptTemplates  []map[string]json.RawMessage `json:"prompt_templates"`
		SchemaValidators []map[string]string          `json:"schema_validators"`
		RateLimit        map[string]interface{}       `json:"rate_limit"`
		AuditSummary     map[string]interface{}       `json:"audit_summary"`
		Services         []map[string]interface{}     `json:"services"`
		Store            struct {
			Mode       string `json:"mode"`
			Persistent bool   `json:"persistent"`
			Warning    string `json:"warning"`
		} `json:"store"`
		AIConfig domain.AIConfig `json:"ai_config"`
		AI       struct {
			ProviderPool struct {
				FallbackOrder []string `json:"fallback_order"`
			} `json:"provider_pool"`
		} `json:"ai"`
		Counts struct {
			AIJobs             int `json:"ai_jobs"`
			GeneratedScenarios int `json:"generated_scenarios"`
		} `json:"counts"`
	}
	mustDecodeData(t, env, &system)
	if len(system.PromptTemplates) == 0 || len(system.SchemaValidators) == 0 || system.AuditSummary["total_recent"] == nil {
		t.Fatalf("unexpected system admin payload: %+v", system)
	}
	if system.Store.Mode != "memory" || system.Store.Persistent || system.Store.Warning == "" {
		t.Fatalf("expected explicit non-persistent store metadata for memory store: %+v", system.Store)
	}
	if system.AIConfig.TopP != 0.8 || system.AIConfig.TopK != 24 || system.AIConfig.MaxTokens != 3072 {
		t.Fatalf("expected ai_config sampling params in system status: %+v", system.AIConfig)
	}
	if system.Counts.AIJobs != 501 || system.Counts.GeneratedScenarios < 0 {
		t.Fatalf("expected non-negative AI job and generated scenario counts: %+v", system.Counts)
	}
	if len(system.AI.ProviderPool.FallbackOrder) < 5 {
		t.Fatalf("expected expanded provider fallback order in system status: %+v", system.AI.ProviderPool)
	}
	for _, prompt := range system.PromptTemplates {
		if _, ok := prompt["content"]; ok {
			t.Fatalf("system status leaked prompt content field: %+v", prompt)
		}
		if _, ok := prompt["default"]; ok {
			t.Fatalf("system status leaked prompt default field: %+v", prompt)
		}
		var summary string
		var contentLength int
		var defaultLength int
		var renderEngine string
		_ = json.Unmarshal(prompt["summary"], &summary)
		_ = json.Unmarshal(prompt["content_length"], &contentLength)
		_ = json.Unmarshal(prompt["default_length"], &defaultLength)
		_ = json.Unmarshal(prompt["render_engine"], &renderEngine)
		if summary == "" || contentLength == 0 || defaultLength == 0 {
			t.Fatalf("expected prompt summary metadata without raw content: %+v", prompt)
		}
		if renderEngine == "" {
			t.Fatalf("expected prompt render engine without raw content: %+v", prompt)
		}
	}
	statusBody := string(env.Data)
	if strings.Contains(statusBody, rawPromptContent) {
		t.Fatalf("system status leaked raw prompt content: %s", statusBody)
	}
	foundSensitiveService := false
	for _, service := range system.Services {
		if service["name"] == "Sensitive Detection" && service["status"] != "" && service["detail"] != "" {
			foundSensitiveService = true
			break
		}
	}
	if !foundSensitiveService {
		t.Fatalf("expected Sensitive Detection service summary: %+v", system.Services)
	}
	firstSchema := system.SchemaValidators[0]
	if firstSchema["schema_name"] == "" || firstSchema["version"] == "" || firstSchema["task"] == "" || firstSchema["description"] == "" || firstSchema["status"] == "" {
		t.Fatalf("schema validator metadata is incomplete: %+v", firstSchema)
	}

	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/admin/prompts/scenario_generate", adminToken, map[string]string{
		"content": "{{ .Missing ",
	})
	if status != http.StatusBadRequest || env.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid prompt template rejection, status=%d env=%+v", status, env)
	}
}

func TestSensitiveCommunityContentIsFlaggedAndSanitized(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestJSON(t, handler, http.MethodPost, "/api/v1/community/posts", token, map[string]interface{}{
		"title":       "真实公司故障",
		"raw_content": "某有限公司线上 10.2.3.4 password=secret 发布后缓存异常，api_key=sk-demo。",
		"domain":      "database",
		"tags":        []string{"敏感"},
	})
	if status != http.StatusOK {
		t.Fatalf("community create status=%d message=%s", status, env.Message)
	}
	var post domain.CommunityPost
	mustDecodeData(t, env, &post)
	if post.SensitiveCheck.Status != "risk" || len(post.SensitiveCheck.Findings) == 0 {
		t.Fatalf("expected sensitive findings: %+v", post.SensitiveCheck)
	}
	if post.RawContent == "某有限公司线上 10.2.3.4 password=secret 发布后缓存异常，api_key=sk-demo。" {
		t.Fatalf("expected sanitized raw content, got %q", post.RawContent)
	}
}
