package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"situational-teaching/backend/internal/diagram"
	"situational-teaching/backend/internal/domain"
)

func ValidateScenarioQuestion(question domain.ScenarioQuestion) error {
	if strings.TrimSpace(question.Title) == "" {
		return fmt.Errorf("scenario title is required")
	}
	if strings.TrimSpace(question.Description) == "" {
		return fmt.Errorf("scenario description is required")
	}
	if strings.TrimSpace(question.Domain) == "" {
		return fmt.Errorf("scenario domain is required")
	}
	if !oneOf(question.Difficulty, "L1", "L2", "L3", "L4", "L5") {
		return fmt.Errorf("scenario difficulty is invalid")
	}
	if !oneOf(question.ScenarioType, "troubleshooting", "design", "performance") {
		return fmt.Errorf("scenario type is invalid")
	}
	if len(question.Tags) == 0 {
		return fmt.Errorf("scenario tags are required")
	}
	return ValidateScenarioContent(question.Content, false)
}

func PrepareScenarioQuestion(question domain.ScenarioQuestion) domain.ScenarioQuestion {
	question.Content = PrepareScenarioContent(question.Content, question)
	return question
}

func PrepareScenarioForPersistence(question domain.ScenarioQuestion) domain.ScenarioQuestion {
	return PrepareScenarioQuestion(question)
}

func PrepareScenarioContent(content domain.ScenarioContent, question domain.ScenarioQuestion) domain.ScenarioContent {
	question.Content = content
	if content.ArchitectureDiagramSpec != nil {
		result := diagram.BuildMermaidFromSpec(*content.ArchitectureDiagramSpec)
		if result.Valid {
			content.ArchitectureDiagram = result.Code
			if content.DiagramStatus == "fallback" {
				content.DiagramWarnings = append([]string{}, content.DiagramWarnings...)
				return content
			}
			content.DiagramStatus = result.Status
			content.DiagramWarnings = result.Warnings
			return content
		}
		content.ArchitectureDiagram = diagram.FallbackScenarioDiagram(question)
		spec := diagram.FallbackScenarioDiagramSpec(question)
		content.ArchitectureDiagramSpec = &spec
		content.DiagramStatus = "fallback"
		content.DiagramWarnings = append(result.Warnings, result.Error)
		return content
	}
	result := diagram.NormalizeMermaidDiagram(content.ArchitectureDiagram)
	if result.Valid {
		content.ArchitectureDiagram = result.Code
		if content.DiagramStatus == "fallback" {
			content.DiagramWarnings = append([]string{}, content.DiagramWarnings...)
			return content
		}
		content.DiagramStatus = result.Status
		content.DiagramWarnings = result.Warnings
		return content
	}
	content.ArchitectureDiagram = diagram.FallbackScenarioDiagram(question)
	spec := diagram.FallbackScenarioDiagramSpec(question)
	content.ArchitectureDiagramSpec = &spec
	content.DiagramStatus = "fallback"
	content.DiagramWarnings = append(result.Warnings, result.Error)
	return content
}

func ValidateScenarioContent(content domain.ScenarioContent, allowPreview bool) error {
	if strings.TrimSpace(content.RootCause) == "" {
		return fmt.Errorf("root cause is required")
	}
	if !allowPreview && len(content.RootCauseKeywords) < 2 {
		return fmt.Errorf("root cause keywords are required")
	}
	if len(content.KeyEvidence) == 0 {
		return fmt.Errorf("key evidence is required")
	}
	if len(content.StandardProcedure) < 2 {
		return fmt.Errorf("standard procedure is required")
	}
	if result := diagram.NormalizeMermaidDiagram(content.ArchitectureDiagram); !result.Valid {
		return fmt.Errorf("architecture diagram must be valid mermaid: %s", result.Error)
	}
	if len(content.RevealStrategy.SurfaceClues) == 0 {
		return fmt.Errorf("surface clues are required")
	}
	if !allowPreview && len(content.RevealStrategy.DeepClues) == 0 {
		return fmt.Errorf("deep clues are required")
	}
	for _, clue := range append(append([]domain.Clue{}, content.RevealStrategy.SurfaceClues...), content.RevealStrategy.DeepClues...) {
		if err := validateClue(clue); err != nil {
			return err
		}
	}
	for _, clue := range content.RevealStrategy.Distractors {
		if err := validateClue(clue); err != nil {
			return err
		}
		if !clue.IsDistractor {
			return fmt.Errorf("distractor clue must set is_distractor")
		}
	}
	return nil
}

func ValidateInterviewFeedback(feedback InterviewFeedback, needFollowUp, needReport bool) error {
	if len(feedback.Highlights) == 0 {
		return fmt.Errorf("interview highlights are required")
	}
	if len(feedback.Deficiencies) == 0 {
		return fmt.Errorf("interview deficiencies are required")
	}
	if needFollowUp && strings.TrimSpace(feedback.FollowUpQuestion) == "" {
		return fmt.Errorf("follow up question is required")
	}
	if needReport && strings.TrimSpace(feedback.FinalReport) == "" {
		return fmt.Errorf("final report is required")
	}
	return nil
}

func ValidateScenarioReply(reply string) error {
	if strings.TrimSpace(reply) == "" {
		return fmt.Errorf("scenario reply is required")
	}
	return nil
}

func ValidateSensitiveCheck(result domain.SensitiveCheckResult) error {
	status := strings.TrimSpace(result.Status)
	if status == "" {
		return fmt.Errorf("sensitive check status is required")
	}
	if !oneOf(status, "clear", "needs_review", "risk") {
		return fmt.Errorf("sensitive check status is invalid")
	}
	if status == "clear" && len(result.Findings) > 0 {
		return fmt.Errorf("clear sensitive check cannot contain findings")
	}
	for _, finding := range result.Findings {
		if strings.TrimSpace(finding.Type) == "" {
			return fmt.Errorf("sensitive finding type is required")
		}
		if strings.TrimSpace(finding.Field) == "" {
			return fmt.Errorf("sensitive finding field is required")
		}
		if strings.TrimSpace(finding.Excerpt) == "" {
			return fmt.Errorf("sensitive finding excerpt is required")
		}
		if !oneOf(finding.Severity, "low", "medium", "high") {
			return fmt.Errorf("sensitive finding severity is invalid")
		}
		if strings.TrimSpace(finding.Suggestion) == "" {
			return fmt.Errorf("sensitive finding suggestion is required")
		}
		if finding.Confidence < 0 || finding.Confidence > 1 {
			return fmt.Errorf("sensitive finding confidence is invalid")
		}
	}
	return nil
}

func ValidateJSONShape(raw string, target interface{}, validate func() error) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty json")
	}
	if err := json.Unmarshal([]byte(extractJSONObject(raw)), target); err != nil {
		return err
	}
	return validate()
}

func NormalizeSensitiveFinding(finding domain.SensitiveFinding, fallbackField, fallbackSource string) domain.SensitiveFinding {
	finding.Field = defaultString(finding.Field, defaultString(fallbackField, "content"))
	finding.Source = defaultString(finding.Source, defaultString(fallbackSource, "rule"))
	finding.Severity = normalizeSeverity(finding.Severity)
	finding.Excerpt = Sanitize(truncateForFinding(finding.Excerpt))
	finding.RedactedExcerpt = defaultString(Sanitize(truncateForFinding(finding.RedactedExcerpt)), finding.Excerpt)
	if finding.Confidence <= 0 {
		if finding.Source == "model" {
			finding.Confidence = 0.7
		} else {
			finding.Confidence = 1
		}
	}
	if finding.Suggestion == "" {
		finding.Suggestion = "请脱敏后再进入审核。"
	}
	return finding
}

func NormalizeSensitiveCheck(result domain.SensitiveCheckResult, fallbackSource string) domain.SensitiveCheckResult {
	result.Source = defaultString(result.Source, fallbackSource)
	result.Findings = append([]domain.SensitiveFinding{}, result.Findings...)
	for i, finding := range result.Findings {
		result.Findings[i] = NormalizeSensitiveFinding(finding, finding.Field, result.Source)
	}
	result.Status = normalizeSensitiveStatus(result.Status, result.Findings)
	result.Sanitized = result.Sanitized || len(result.Findings) > 0
	result.RiskLevel = defaultString(result.RiskLevel, riskLevelFromFindings(result.Findings))
	result.Blocked = result.Blocked || shouldBlockSensitiveFindings(result.Findings)
	result.Summary = defaultString(result.Summary, sensitiveSummary(result))
	return result
}

func MergeSensitiveChecks(ruleResult, modelResult domain.SensitiveCheckResult) domain.SensitiveCheckResult {
	ruleResult = NormalizeSensitiveCheck(ruleResult, "rule")
	modelResult = NormalizeSensitiveCheck(modelResult, "model")
	merged := domain.SensitiveCheckResult{
		Status:       "clear",
		Findings:     []domain.SensitiveFinding{},
		CheckedAt:    ruleResult.CheckedAt,
		Source:       "rule+model",
		FallbackUsed: ruleResult.FallbackUsed || modelResult.FallbackUsed,
	}
	if merged.CheckedAt.IsZero() {
		merged.CheckedAt = modelResult.CheckedAt
	}
	seen := map[string]bool{}
	for _, finding := range append(ruleResult.Findings, modelResult.Findings...) {
		normalized := NormalizeSensitiveFinding(finding, finding.Field, finding.Source)
		key := strings.ToLower(strings.Join([]string{normalized.Type, normalized.Field, normalized.Excerpt}, "|"))
		if seen[key] {
			continue
		}
		seen[key] = true
		merged.Findings = append(merged.Findings, normalized)
	}
	merged.Status = normalizeSensitiveStatus("", merged.Findings)
	merged.Sanitized = len(merged.Findings) > 0
	merged.RiskLevel = riskLevelFromFindings(merged.Findings)
	merged.Blocked = shouldBlockSensitiveFindings(merged.Findings)
	merged.Summary = sensitiveSummary(merged)
	return merged
}

func SensitiveFallbackResult(ruleResult domain.SensitiveCheckResult, source string) domain.SensitiveCheckResult {
	result := NormalizeSensitiveCheck(ruleResult, "rule")
	result.Source = defaultString(source, "rule_fallback")
	result.FallbackUsed = true
	result.Summary = "模型检测不可用，已使用规则检测兜底。"
	if result.Status == "clear" {
		result.Summary = "模型检测不可用，规则检测未发现风险。"
	}
	return result
}

func SafetyRewrite(text string, forbidden []string) (string, bool) {
	rewritten := false
	if ContainsSensitiveInfo(text) {
		text = Sanitize(text)
		rewritten = true
	}
	for _, item := range forbidden {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(item)) || RootCauseMatch(text, item, nil) >= 82 {
			return "这部分先不直接给出最终根因。请继续说明你的判断依据，并补充能支撑结论的证据。", true
		}
	}
	return text, rewritten
}

func normalizeSensitiveStatus(status string, findings []domain.SensitiveFinding) string {
	status = strings.TrimSpace(status)
	if len(findings) == 0 {
		return "clear"
	}
	if status == "risk" || hasHighSensitiveFinding(findings) {
		return "risk"
	}
	return "needs_review"
}

func riskLevelFromFindings(findings []domain.SensitiveFinding) string {
	level := "none"
	for _, finding := range findings {
		switch normalizeSeverity(finding.Severity) {
		case "high":
			return "high"
		case "medium":
			if level != "high" {
				level = "medium"
			}
		case "low":
			if level == "none" {
				level = "low"
			}
		}
	}
	return level
}

func shouldBlockSensitiveFindings(findings []domain.SensitiveFinding) bool {
	for _, finding := range findings {
		if normalizeSeverity(finding.Severity) == "high" && finding.Confidence >= 0.75 {
			return true
		}
	}
	return false
}

func hasHighSensitiveFinding(findings []domain.SensitiveFinding) bool {
	for _, finding := range findings {
		if normalizeSeverity(finding.Severity) == "high" {
			return true
		}
	}
	return false
}

func sensitiveSummary(result domain.SensitiveCheckResult) string {
	if len(result.Findings) == 0 {
		return "未发现敏感信息风险。"
	}
	return fmt.Sprintf("发现 %d 项敏感信息风险，最高等级：%s。", len(result.Findings), defaultString(result.RiskLevel, riskLevelFromFindings(result.Findings)))
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "medium"
	}
}

func truncateForFinding(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= 80 {
		return value
	}
	return string(runes[:80])
}

func DefaultInterviewReport(evaluation domain.InterviewEvaluation) string {
	if evaluation.TotalScore >= 85 {
		return "整体表现优秀，能够覆盖关键定位路径，并具备较好的落地意识。"
	}
	if evaluation.TotalScore >= 70 {
		return "整体达到岗位要求，建议继续强化底层原理与应急取舍。"
	}
	return "当前回答还有明显缺口，建议围绕关键命令、验证路径和回滚方案进行专项练习。"
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "待补充"
	}
	for _, sep := range []string{"。", "！", "？", "\n", "."} {
		if index := strings.Index(text, sep); index > 0 {
			return strings.TrimSpace(text[:index])
		}
	}
	if len([]rune(text)) > 80 {
		return string([]rune(text)[:80])
	}
	return text
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

func validateClue(clue domain.Clue) error {
	if strings.TrimSpace(clue.ClueID) == "" {
		return fmt.Errorf("clue id is required")
	}
	if strings.TrimSpace(clue.Content) == "" {
		return fmt.Errorf("clue content is required")
	}
	if len(clue.TriggerKeywords) == 0 {
		return fmt.Errorf("clue trigger keywords are required")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
