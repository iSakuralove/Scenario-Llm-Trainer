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

	"situational-teaching/backend/internal/domain"
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
	text, err := extractProfileImportText(strings.TrimSpace(header.Filename), content)
	if err != nil {
		return nil, err
	}
	targetRole, resumeSummary, projectSummary := extractStructuredResumeFields(text)
	profile := user.Profile
	if strings.TrimSpace(targetRole) != "" {
		profile.TargetRole = truncateText(strings.TrimSpace(targetRole), 120)
	}
	profile.ResumeSummary = truncateText(strings.TrimSpace(resumeSummary), 4000)
	if strings.TrimSpace(projectSummary) != "" {
		profile.ProjectSummary = truncateText(strings.TrimSpace(projectSummary), 4000)
	}
	return s.store.SaveUserProfile(user.ID, profile)
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
