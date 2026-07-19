package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
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

	payload := []byte("后端开发工程师，具备三年 Java 服务开发经验，熟悉 MySQL、Redis 和消息队列。负责订单系统重构与缓存一致性治理，将接口平均延迟降低 35%，并推动监控告警和回滚流程落地。")
	status, env := requestMultipartProfileImport(t, handler, token, "resume.txt", "text/plain", payload)
	if status != http.StatusOK {
		t.Fatalf("profile import status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if updated.Profile.ResumeSummary != string(payload) {
		t.Fatalf("expected resume summary to be imported, got %+v", updated.Profile)
	}
	if len(updated.Profile.ResumeDocuments) != 1 || !updated.Profile.ResumeDocuments[0].Editable {
		t.Fatalf("expected one editable resume document, got %+v", updated.Profile.ResumeDocuments)
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

func TestProfileImportRejectsPDFWithoutResumeInformation(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	pdfPath := filepath.Join("testdata", "resume-sample.pdf")
	payload, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}

	status, env := requestMultipartProfileImport(t, handler, token, "resume.pdf", "application/pdf", payload)
	if status != http.StatusBadRequest {
		t.Fatalf("profile import pdf status=%d message=%s", status, env.Message)
	}
	if !strings.Contains(env.Message, "简历信息") {
		t.Fatalf("expected resume quality error, got %+v", env)
	}
}

func TestProfileImportStoresDocxAsReadonlyAsset(t *testing.T) {
	t.Setenv("ASSET_STORAGE_DIR", t.TempDir())
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	text := "Java 后端开发工程师，具备三年微服务项目经验，熟悉 MySQL、Redis、Kafka 和 Kubernetes。负责交易平台架构升级，将接口延迟降低 35%，并完善容量评估、监控告警和回滚流程。"
	payload := makeResumeDocx(t, text)

	status, env := requestMultipartProfileImport(t, handler, token, "resume.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", payload)
	if status != http.StatusOK {
		t.Fatalf("profile import docx status=%d message=%s", status, env.Message)
	}
	var updated domain.User
	mustDecodeData(t, env, &updated)
	if len(updated.Profile.ResumeDocuments) != 1 {
		t.Fatalf("expected one resume document, got %+v", updated.Profile.ResumeDocuments)
	}
	document := updated.Profile.ResumeDocuments[0]
	if document.Editable || document.AssetID == "" || document.ContentURL == "" {
		t.Fatalf("expected readonly asset-backed document, got %+v", document)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+document.AssetID+"?content=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), payload) {
		t.Fatalf("asset content status=%d size=%d", rr.Code, rr.Body.Len())
	}
}

func TestProfileImportExtractsTargetRoleAndProjectSummary(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	payload := []byte("求职意向：后端开发工程师\n个人优势：具备三年 Java 服务开发经验，熟悉 MySQL、Redis 和消息队列。\n项目经历：负责订单系统重构，设计缓存一致性方案并推动上线，将高峰期接口延迟降低 30%，完善监控告警与回滚流程。")

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

func TestProfileImportRejectsLowQualityContentWithoutOverwritingProfile(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	valid := []byte("后端开发工程师，具备三年 Java 项目经验，熟悉 MySQL、Redis 和 Docker。负责订单平台重构与缓存治理，将接口延迟降低 30%，并完善监控、发布和回滚流程。")

	status, env := requestMultipartProfileImport(t, handler, token, "valid.md", "text/markdown", valid)
	if status != http.StatusOK {
		t.Fatalf("valid import status=%d message=%s", status, env.Message)
	}
	status, env = requestMultipartProfileImport(t, handler, token, "junk.txt", "text/plain", []byte("哈哈哈哈哈哈！！！！！！！！"))
	if status != http.StatusBadRequest || !strings.Contains(env.Message, "简历") {
		t.Fatalf("junk import status=%d message=%s", status, env.Message)
	}

	user, ok := dataStore.FindUserByIdentifier("demo")
	if !ok {
		t.Fatal("demo user not found")
	}
	if len(user.Profile.ResumeDocuments) != 1 || user.Profile.ResumeSummary != string(valid) {
		t.Fatalf("invalid import changed profile: %+v", user.Profile)
	}
}

func TestResumeDocumentsListAndEditTextDocument(t *testing.T) {
	dataStore := store.NewMemoryStore(auth.HashPassword)
	handler := NewServerForTests(dataStore, auth.NewManager("test-secret", time.Hour)).Handler()
	token := loginToken(t, handler, "demo", "demo123")
	first := []byte("Java 后端开发工程师，具备三年系统开发经验，熟悉 MySQL、Redis、Docker。负责订单平台重构，将接口延迟降低 25%，并补齐监控告警与回滚流程。")
	second := []byte("Python 工程师，具备数据平台项目经验，熟悉 FastAPI、PostgreSQL 和 Kubernetes。负责任务调度服务设计，将失败率降低 40%，并推动自动化部署落地。")

	for index, payload := range [][]byte{first, second} {
		status, env := requestMultipartProfileImport(t, handler, token, fmt.Sprintf("resume-%d.md", index+1), "text/markdown", payload)
		if status != http.StatusOK {
			t.Fatalf("import %d status=%d message=%s", index, status, env.Message)
		}
	}

	status, env := requestJSON(t, handler, http.MethodGet, "/api/v1/users/me/resumes", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list resumes status=%d message=%s", status, env.Message)
	}
	var list struct {
		List  []domain.ResumeDocument `json:"list"`
		Total int                     `json:"total"`
	}
	mustDecodeData(t, env, &list)
	if list.Total != 2 || len(list.List) != 2 {
		t.Fatalf("expected two resumes, got %+v", list)
	}

	edited := "Java 后端开发工程师，具备五年微服务项目经验，熟悉 MySQL、Redis、Kafka 和 Kubernetes。负责交易平台架构升级，将峰值吞吐提升 50%，并完善容量评估、监控和回滚机制。"
	status, env = requestJSON(t, handler, http.MethodPut, "/api/v1/users/me/resumes/"+list.List[0].ID, token, map[string]any{"content": edited})
	if status != http.StatusOK {
		t.Fatalf("edit resume status=%d message=%s", status, env.Message)
	}
	var updated domain.ResumeDocument
	mustDecodeData(t, env, &updated)
	if updated.Content != edited || updated.QualityStatus != "passed" {
		t.Fatalf("unexpected edited resume: %+v", updated)
	}
}

func TestResumeContentQualityRules(t *testing.T) {
	valid := "后端开发工程师，具备三年 Java 项目经验，熟悉 MySQL、Redis 和 Docker。负责订单平台重构，将接口延迟降低 30%，并完善监控告警与回滚流程。"
	if ok, reason := resumeContentQuality(valid, ""); !ok {
		t.Fatalf("expected valid resume, got %s", reason)
	}
	if ok, _ := resumeContentQuality("后端工程师", ""); ok {
		t.Fatal("expected short resume to be rejected")
	}
	repeated := strings.Repeat("负责订单系统开发。", 8)
	if ok, _ := resumeContentQuality(repeated, ""); ok {
		t.Fatal("expected repeated resume to be rejected")
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

func makeResumeDocx(t *testing.T, text string) []byte {
	t.Helper()
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	part, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	xml := `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`
	if _, err := part.Write([]byte(xml)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}
