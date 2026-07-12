package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func TestProfileImportUpdatesResumeSummaryFromTextFile(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestMultipartProfileImport(t, handler, token, "resume.txt", "text/plain", []byte("负责数据库治理和缓存一致性改造"))
	if status != http.StatusOK {
		t.Fatalf("profile import status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Profile.ResumeSummary != "负责数据库治理和缓存一致性改造" {
		t.Fatalf("expected resume summary to be imported, got %+v", updated.Profile)
	}
}

func TestProfileImportRejectsInvalidPDF(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")

	status, env := requestMultipartProfileImport(t, handler, token, "resume.pdf", "application/pdf", []byte("not-a-real-pdf"))
	if status != http.StatusBadRequest {
		t.Fatalf("profile import invalid pdf status=%d message=%s", status, env.Message)
	}
	if env.Message != "invalid pdf file" {
		t.Fatalf("unexpected invalid pdf message: %+v", env)
	}
}

func TestProfileImportParsesPDFIntoResumeSummary(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	pdfPath := filepath.Join("testdata", "resume-sample.pdf")
	payload, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}

	status, env := requestMultipartProfileImport(t, handler, token, "resume.pdf", "application/pdf", payload)
	if status != http.StatusOK {
		t.Fatalf("profile import pdf status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Profile.ResumeSummary == "" {
		t.Fatalf("expected non-empty resume summary after pdf import, got %+v", updated.Profile)
	}
}

func TestProfileImportExtractsTargetRoleAndProjectSummary(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	payload := []byte("求职意向：后端开发工程师\n个人优势：熟悉数据库和缓存治理\n项目经历：负责订单系统重构，推进缓存一致性方案落地。")

	status, env := requestMultipartProfileImport(t, handler, token, "resume.txt", "text/plain", payload)
	if status != http.StatusOK {
		t.Fatalf("profile import structured status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Profile.TargetRole != "后端开发工程师" {
		t.Fatalf("expected target role extracted, got %+v", updated.Profile)
	}
	if updated.Profile.ProjectSummary == "" {
		t.Fatalf("expected project summary extracted, got %+v", updated.Profile)
	}
	if updated.Profile.ResumeSummary == "" {
		t.Fatalf("expected resume summary still populated, got %+v", updated.Profile)
	}
	if strings.Contains(updated.Profile.ResumeSummary, "求职意向") || strings.Contains(updated.Profile.ResumeSummary, "项目经历") {
		t.Fatalf("expected structured sections excluded from resume summary, got %+v", updated.Profile)
	}
}

func TestExtractStructuredResumeFieldsSupportsEnglishSections(t *testing.T) {
	text := "Target Role: Backend Engineer\nSummary: Distributed systems developer\nProject Experience:\nRebuilt the order platform\nWork Experience:\nExample Corp"

	targetRole, resumeSummary, projectSummary := extractStructuredResumeFields(text)

	if targetRole != "Backend Engineer" {
		t.Fatalf("expected English target role, got %q", targetRole)
	}
	if projectSummary != "Rebuilt the order platform" {
		t.Fatalf("expected project section to stop at work experience, got %q", projectSummary)
	}
	if strings.Contains(resumeSummary, "Target Role") || strings.Contains(resumeSummary, "Project Experience") || strings.Contains(resumeSummary, "Rebuilt the order platform") {
		t.Fatalf("expected structured sections excluded from resume summary, got %q", resumeSummary)
	}
	if !strings.Contains(resumeSummary, "Example Corp") {
		t.Fatalf("expected non-project experience preserved in resume summary, got %q", resumeSummary)
	}
}

func requestMultipartProfileImport(t *testing.T, handler http.Handler, token, filename, mimeType string, payload []byte) (int, testEnvelope) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/profile/import", &body)
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
