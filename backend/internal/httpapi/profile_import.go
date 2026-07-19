package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/ledongthuc/pdf"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

const maxProfileImportBytes = 2 * 1024 * 1024

func (s *Server) importProfileResume(user *domain.User, r *http.Request) (*domain.User, error) {
	if user == nil {
		return nil, errors.New("user not found")
	}
	if err := r.ParseMultipartForm(maxProfileImportBytes); err != nil {
		return nil, errors.New("invalid multipart form")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, errors.New("file is required")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxProfileImportBytes+1))
	if err != nil {
		return nil, errors.New("read upload failed")
	}
	if len(content) == 0 {
		return nil, errors.New("file is empty")
	}
	if len(content) > maxProfileImportBytes {
		return nil, errors.New("file too large")
	}
	filename := filepath.Base(strings.TrimSpace(header.Filename))
	text, err := extractProfileImportText(filename, content)
	if err != nil {
		return nil, err
	}
	targetRole, resumeSummary, projectSummary := extractStructuredResumeFields(text)
	if ok, reason := resumeContentQuality(resumeSummary, projectSummary); !ok {
		return nil, errors.New(reason)
	}

	now := time.Now()
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	document := domain.ResumeDocument{
		ID:            store.NewID(),
		Name:          filename,
		SourceType:    "upload",
		Format:        format,
		ExtractedText: strings.TrimSpace(text),
		ParseStatus:   "parsed",
		QualityStatus: "passed",
		Editable:      format == "txt" || format == "md",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if document.Editable {
		document.Content = strings.TrimSpace(text)
	} else {
		assetID := store.NewID()
		stored, saveErr := s.assets.Save(r.Context(), AssetStorageSaveRequest{
			UserID:   user.ID,
			AssetID:  assetID,
			Filename: filename,
			MaxBytes: maxProfileImportBytes,
		}, bytes.NewReader(content))
		if saveErr != nil {
			return nil, saveErr
		}
		asset, createErr := s.store.CreateAsset(domain.Asset{
			ID:         assetID,
			UserID:     user.ID,
			Kind:       "resume",
			Filename:   filename,
			MimeType:   resumeMimeType(format),
			Size:       stored.Size,
			StorageKey: stored.StorageKey,
			URL:        assetMetadataURL(assetID),
			ContentURL: assetContentURL(assetID),
			Checksum:   stored.Checksum,
		})
		if createErr != nil {
			_ = s.assets.Delete(r.Context(), &domain.Asset{StorageKey: stored.StorageKey})
			return nil, createErr
		}
		document.AssetID = asset.ID
		document.ContentURL = asset.ContentURL
	}

	profile := user.Profile
	if strings.TrimSpace(profile.TargetRole) == "" && strings.TrimSpace(targetRole) != "" {
		profile.TargetRole = truncateText(strings.TrimSpace(targetRole), 120)
	}
	if strings.TrimSpace(profile.ResumeSummary) == "" {
		profile.ResumeSummary = truncateText(strings.TrimSpace(resumeSummary), 4000)
	}
	if strings.TrimSpace(profile.ProjectSummary) == "" && strings.TrimSpace(projectSummary) != "" {
		profile.ProjectSummary = truncateText(strings.TrimSpace(projectSummary), 4000)
	}
	profile.ResumeDocuments = append(profile.ResumeDocuments, document)
	updated, err := s.store.SaveUserProfile(user.ID, profile)
	if err != nil && document.AssetID != "" {
		if asset, ok := s.store.GetAsset(document.AssetID); ok {
			_ = s.assets.Delete(r.Context(), asset)
		}
		s.store.DeleteAsset(document.AssetID)
	}
	return updated, err
}

func resumeMimeType(format string) string {
	switch format {
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "md":
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func resumeContentQuality(resumeSummary, projectSummary string) (bool, string) {
	fields := []struct {
		name string
		text string
	}{
		{name: "简历摘要", text: strings.TrimSpace(resumeSummary)},
		{name: "项目经历", text: strings.TrimSpace(projectSummary)},
	}
	combined := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.text == "" {
			continue
		}
		if reason := resumeFieldQualityReason(field.name, field.text); reason != "" {
			return false, reason
		}
		combined = append(combined, field.text)
	}
	text := strings.Join(combined, "\n")
	if resumeEffectiveRuneCount(text) < 60 {
		return false, "简历有效信息不足，请至少补充 60 个文字或数字字符"
	}
	if resumeInformationCategoryCount(text) < 2 {
		return false, "简历信息过于单一，请补充岗位经历、技能、项目职责、成果或教育背景中的至少两类"
	}
	return true, ""
}

func resumeFieldQualityReason(name, text string) string {
	nonSpace := 0
	symbols := 0
	validCounts := map[rune]int{}
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		nonSpace++
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			validCounts[unicode.ToLower(r)]++
		} else {
			symbols++
		}
	}
	if nonSpace == 0 {
		return name + "不能为空"
	}
	if float64(symbols)/float64(nonSpace) >= 0.6 {
		return name + "主要由符号组成，请填写真实经历"
	}
	validTotal := 0
	dominant := 0
	for _, count := range validCounts {
		validTotal += count
		if count > dominant {
			dominant = count
		}
	}
	if validTotal > 0 && float64(dominant)/float64(validTotal) >= 0.6 {
		return name + "包含大量重复字符，请填写真实经历"
	}
	if repeatedResumeFragmentRatio(text) >= 0.6 {
		return name + "包含大量重复内容，请删减后重试"
	}
	return ""
}

func resumeEffectiveRuneCount(text string) int {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			count++
		}
	}
	return count
}

func repeatedResumeFragmentRatio(text string) float64 {
	fragments := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == '\n' || r == '\r' || r == '。' || r == '！' || r == '？' || r == '!' || r == '?' || r == ';' || r == '；'
	})
	total := 0
	repeated := 0
	seen := map[string]bool{}
	for _, fragment := range fragments {
		fragment = strings.Join(strings.Fields(fragment), "")
		length := resumeEffectiveRuneCount(fragment)
		if length < 4 {
			continue
		}
		total += length
		if seen[fragment] {
			repeated += length
		} else {
			seen[fragment] = true
		}
	}
	if total == 0 {
		return 0
	}
	return float64(repeated) / float64(total)
}

func resumeInformationCategoryCount(text string) int {
	normalized := strings.ToLower(text)
	categories := [][]string{
		{"工程师", "开发者", "工作经历", "实习经历", "任职", "职位", "岗位", "engineer", "developer", "employment", "intern"},
		{"技能", "技术栈", "熟悉", "掌握", "java", "python", "golang", "go语言", "c++", "javascript", "typescript", "react", "vue", "mysql", "postgresql", "redis", "docker", "kubernetes", "linux", "spring", "node", "llm", "大模型", "数据库", "缓存", "消息队列"},
		{"项目", "系统", "平台", "服务", "模块", "架构", "设计", "开发", "实施", "负责", "职责", "project", "system", "platform", "service", "responsible"},
		{"提升", "降低", "优化", "减少", "增长", "节省", "性能", "吞吐", "延迟", "可用性", "qps", "tps", "成果", "结果", "上线", "落地", "increase", "reduce", "improve", "latency"},
		{"大学", "学院", "本科", "硕士", "博士", "学历", "专业", "教育经历", "university", "college", "bachelor", "master", "phd", "education"},
	}
	count := 0
	for _, keywords := range categories {
		for _, keyword := range keywords {
			if strings.Contains(normalized, keyword) {
				count++
				break
			}
		}
	}
	return count
}

const manualResumeDocumentID = "manual-profile"

func (s *Server) handleResumeDocuments(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 0 && r.Method == http.MethodGet {
		documents := resumeDocumentsForProfile(user.Profile)
		writeOK(w, map[string]interface{}{"list": documents, "total": len(documents)})
		return
	}
	if len(parts) != 1 || r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req struct {
		Content        *string `json:"content"`
		ResumeSummary  *string `json:"resume_summary"`
		ProjectSummary *string `json:"project_summary"`
	}
	if !decode(w, r, &req) {
		return
	}

	profile := user.Profile
	documentID := parts[0]
	if documentID == manualResumeDocumentID {
		resumeSummary := profile.ResumeSummary
		projectSummary := profile.ProjectSummary
		if req.ResumeSummary != nil {
			resumeSummary = strings.TrimSpace(*req.ResumeSummary)
		}
		if req.ProjectSummary != nil {
			projectSummary = strings.TrimSpace(*req.ProjectSummary)
		}
		if resumeSummary == "" && projectSummary == "" {
			writeError(w, http.StatusBadRequest, "手动填写内容不能为空")
			return
		}
		if ok, reason := resumeContentQuality(resumeSummary, projectSummary); !ok {
			writeError(w, http.StatusBadRequest, reason)
			return
		}
		now := time.Now()
		profile.ResumeSummary = resumeSummary
		profile.ProjectSummary = projectSummary
		profile.ManualResumeUpdatedAt = &now
		updated, err := s.store.SaveUserProfile(user.ID, profile)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		document, _ := resumeDocumentByID(resumeDocumentsForProfile(updated.Profile), manualResumeDocumentID)
		writeOK(w, document)
		return
	}

	documentIndex := -1
	for index := range profile.ResumeDocuments {
		if profile.ResumeDocuments[index].ID == documentID {
			documentIndex = index
			break
		}
	}
	if documentIndex < 0 {
		writeError(w, http.StatusNotFound, "resume document not found")
		return
	}
	document := profile.ResumeDocuments[documentIndex]
	if !document.Editable || (document.Format != "txt" && document.Format != "md") {
		writeError(w, http.StatusBadRequest, "该简历格式不支持在线编辑")
		return
	}
	if req.Content == nil || strings.TrimSpace(*req.Content) == "" {
		writeError(w, http.StatusBadRequest, "简历内容不能为空")
		return
	}
	content := strings.TrimSpace(*req.Content)
	_, resumeSummary, projectSummary := extractStructuredResumeFields(content)
	if ok, reason := resumeContentQuality(resumeSummary, projectSummary); !ok {
		writeError(w, http.StatusBadRequest, reason)
		return
	}
	document.Content = content
	document.ExtractedText = content
	document.ParseStatus = "parsed"
	document.QualityStatus = "passed"
	document.QualityReason = ""
	document.UpdatedAt = time.Now()
	profile.ResumeDocuments[documentIndex] = document
	updated, err := s.store.SaveUserProfile(user.ID, profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updatedDocument, _ := resumeDocumentByID(resumeDocumentsForProfile(updated.Profile), documentID)
	writeOK(w, updatedDocument)
}

func resumeDocumentsForProfile(profile domain.UserProfile) []domain.ResumeDocument {
	documents := make([]domain.ResumeDocument, 0, len(profile.ResumeDocuments)+1)
	manualExists := strings.TrimSpace(profile.ResumeSummary) != "" || strings.TrimSpace(profile.ProjectSummary) != ""
	if manualExists && (profile.ManualResumeUpdatedAt != nil || len(profile.ResumeDocuments) == 0) {
		updatedAt := profile.UpdatedAt
		if profile.ManualResumeUpdatedAt != nil {
			updatedAt = *profile.ManualResumeUpdatedAt
		}
		qualityStatus := "passed"
		qualityReason := ""
		if ok, reason := resumeContentQuality(profile.ResumeSummary, profile.ProjectSummary); !ok {
			qualityStatus = "rejected"
			qualityReason = reason
		}
		content := manualResumeMarkdown(profile.ResumeSummary, profile.ProjectSummary)
		documents = append(documents, domain.ResumeDocument{
			ID:            manualResumeDocumentID,
			Name:          "手动填写",
			SourceType:    "manual",
			Format:        "md",
			Content:       content,
			ExtractedText: content,
			ParseStatus:   "parsed",
			QualityStatus: qualityStatus,
			QualityReason: qualityReason,
			Editable:      true,
			CreatedAt:     updatedAt,
			UpdatedAt:     updatedAt,
		})
	}
	for _, source := range profile.ResumeDocuments {
		document := source
		if document.AssetID != "" && strings.TrimSpace(document.ContentURL) == "" {
			document.ContentURL = assetContentURL(document.AssetID)
		}
		text := firstNonEmpty(document.ExtractedText, document.Content)
		_, resumeSummary, projectSummary := extractStructuredResumeFields(text)
		if ok, reason := resumeContentQuality(resumeSummary, projectSummary); !ok {
			document.QualityStatus = "rejected"
			document.QualityReason = reason
		}
		documents = append(documents, document)
	}
	return documents
}

func resumeDocumentByID(documents []domain.ResumeDocument, id string) (domain.ResumeDocument, bool) {
	for _, document := range documents {
		if document.ID == id {
			return document, true
		}
	}
	return domain.ResumeDocument{}, false
}

func manualResumeMarkdown(resumeSummary, projectSummary string) string {
	sections := make([]string, 0, 2)
	if text := strings.TrimSpace(resumeSummary); text != "" {
		sections = append(sections, "# 简历摘要\n\n"+text)
	}
	if text := strings.TrimSpace(projectSummary); text != "" {
		sections = append(sections, "# 项目经历\n\n"+text)
	}
	return strings.Join(sections, "\n\n")
}

func extractProfileImportText(filename string, payload []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".txt", ".md":
		return strings.TrimSpace(string(payload)), nil
	case ".docx":
		return extractDocxText(payload)
	case ".pdf":
		return extractPDFText(payload)
	default:
		return "", fmt.Errorf("unsupported resume format: %s", ext)
	}
}

func extractPDFText(payload []byte) (string, error) {
	tempFile, err := os.CreateTemp("", "profile-resume-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	if _, err := tempFile.Write(payload); err != nil {
		return "", err
	}
	if _, err := tempFile.Seek(0, 0); err != nil {
		return "", err
	}
	file, reader, err := pdf.Open(tempFile.Name())
	if err != nil {
		return "", errors.New("invalid pdf file")
	}
	defer file.Close()
	stream, err := reader.GetPlainText()
	if err != nil {
		return "", errors.New("invalid pdf file")
	}
	text, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(text)), nil
}

func extractDocxText(payload []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return "", errors.New("invalid docx file")
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			return "", err
		}
		defer handle.Close()
		return extractWordDocumentXMLText(handle)
	}
	return "", errors.New("docx missing document.xml")
}

func extractWordDocumentXMLText(reader io.Reader) (string, error) {
	decoder := xml.NewDecoder(reader)
	var parts []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local != "t" {
				continue
			}
			var value string
			if err := decoder.DecodeElement(&value, &element); err != nil {
				return "", err
			}
			value = strings.TrimSpace(value)
			if value != "" {
				parts = append(parts, value)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func extractStructuredResumeFields(text string) (string, string, string) {
	lines := normalizeResumeLines(text)
	targetRole := extractResumeTargetRole(lines)
	projectSummary, projectStart, projectEnd := extractResumeSection(lines, []string{"项目经历", "项目经验", "projects", "project experience"})
	resumeLines := make([]string, 0, len(lines))
	for idx, line := range lines {
		if idx >= projectStart && idx < projectEnd {
			continue
		}
		if _, ok := extractLabeledResumeValue(line, []string{"求职意向", "目标岗位", "应聘岗位", "target role", "position"}); ok {
			continue
		}
		resumeLines = append(resumeLines, line)
	}
	resumeSummary := strings.TrimSpace(strings.Join(resumeLines, "\n"))
	if resumeSummary == "" {
		resumeSummary = strings.TrimSpace(text)
	}
	return targetRole, resumeSummary, projectSummary
}

func normalizeResumeLines(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func extractResumeTargetRole(lines []string) string {
	roleLabels := []string{"求职意向", "目标岗位", "应聘岗位", "target role", "position"}
	for _, line := range lines {
		if value, ok := extractLabeledResumeValue(line, roleLabels); ok {
			return value
		}
	}
	return ""
}

func extractResumeSection(lines []string, sectionNames []string) (string, int, int) {
	start := -1
	firstValue := ""
	for idx, line := range lines {
		if value, ok := extractLabeledResumeValue(line, sectionNames); ok {
			start = idx
			firstValue = value
			break
		}
	}
	if start < 0 {
		return "", -1, -1
	}
	values := make([]string, 0, len(lines)-start)
	if firstValue != "" {
		values = append(values, firstValue)
	}
	end := len(lines)
	for idx := start + 1; idx < len(lines); idx++ {
		line := lines[idx]
		if looksLikeResumeSectionHeader(line) {
			end = idx
			break
		}
		values = append(values, line)
	}
	return strings.TrimSpace(strings.Join(values, "\n")), start, end
}

func looksLikeResumeSectionHeader(line string) bool {
	headers := []string{
		"个人优势", "个人简介", "教育经历", "教育背景", "工作经历", "工作经验", "技能", "证书", "求职意向", "目标岗位", "应聘岗位",
		"summary", "profile", "education", "education background", "work experience", "experience", "skills", "certificates", "certifications", "target role", "position", "projects", "project experience",
	}
	_, ok := extractLabeledResumeValue(line, headers)
	return ok
}

func extractLabeledResumeValue(line string, labels []string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || !strings.HasPrefix(lower, strings.ToLower(label)) {
			continue
		}
		remainder := strings.TrimSpace(trimmed[len(label):])
		if remainder == "" {
			return "", true
		}
		if strings.HasPrefix(remainder, "：") {
			return strings.TrimSpace(strings.TrimPrefix(remainder, "：")), true
		}
		if strings.HasPrefix(remainder, ":") {
			return strings.TrimSpace(strings.TrimPrefix(remainder, ":")), true
		}
	}
	return "", false
}
