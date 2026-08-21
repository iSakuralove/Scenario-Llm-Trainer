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
	if err := ValidateScenarioContent(question.Content, false); err != nil {
		// 统一前缀让路由错误分类（classifyRouterError）把所有内容校验错误
		// 识别为 validation 类：自修复重试可拿到具体错误定向修复，而不是
		// 被当成 unknown 直接放弃。
		return fmt.Errorf("scenario validation failed: %w", err)
	}
	return nil
}

func PrepareScenarioQuestion(question domain.ScenarioQuestion) domain.ScenarioQuestion {
	question.Content = PrepareScenarioContent(question.Content, question)
	return question
}

func PrepareScenarioForPersistence(question domain.ScenarioQuestion) domain.ScenarioQuestion {
	return PrepareScenarioQuestion(question)
}

func PrepareScenarioContent(content domain.ScenarioContent, question domain.ScenarioQuestion) domain.ScenarioContent {
	if isHiddenWorldScenarioContent(content) {
		return prepareHiddenWorldScenarioContent(content, question)
	}
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
	if isHiddenWorldScenarioContent(content) {
		return validateHiddenWorldScenarioContent(content)
	}
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

func isHiddenWorldScenarioContent(content domain.ScenarioContent) bool {
	return strings.TrimSpace(content.ModelVersion) != "" || content.PublicScenario != nil || content.HiddenWorld != nil
}

func prepareHiddenWorldScenarioContent(content domain.ScenarioContent, question domain.ScenarioQuestion) domain.ScenarioContent {
	var publicScenario *domain.PublicScenario
	if content.PublicScenario != nil {
		prepared := *content.PublicScenario
		prepared.InitialSymptoms = append([]string{}, content.PublicScenario.InitialSymptoms...)
		// 结构化 spec 优先：后端确定性渲染 Mermaid，杜绝 LLM 手写图的语法/注入风险。
		if prepared.ArchitectureDiagramSpec != nil {
			spec := SanitizeScenarioDiagramSpec(prepared.ArchitectureDiagramSpec)
			prepared.ArchitectureDiagramSpec = spec
			if result := diagram.BuildMermaidFromSpec(*spec); result.Valid {
				prepared.ArchitectureDiagram = result.Code
			} else {
				// spec 无效 → 兜底图（不含隐藏答案的通用四节点链）
				prepared.ArchitectureDiagram = diagram.FallbackScenarioDiagram(question)
				prepared.ArchitectureDiagramSpec = nil
			}
		} else if strings.TrimSpace(prepared.ArchitectureDiagram) != "" {
			result := diagram.NormalizeMermaidDiagram(prepared.ArchitectureDiagram)
			if result.Valid {
				prepared.ArchitectureDiagram = result.Code
			} else {
				prepared.ArchitectureDiagram = diagram.FallbackScenarioDiagram(question)
			}
		}
		publicScenario = &prepared
	}
	return (domain.ScenarioContent{
		ModelVersion:   strings.TrimSpace(content.ModelVersion),
		PublicScenario: publicScenario,
		HiddenWorld:    content.HiddenWorld,
	}).WithHiddenWorldCompatibility()
}

func validateHiddenWorldScenarioContent(content domain.ScenarioContent) error {
	if content.ModelVersion != "hiddenworld.v1" {
		return fmt.Errorf("hidden world model version is invalid")
	}
	if content.PublicScenario == nil {
		return fmt.Errorf("public scenario is required")
	}
	if content.HiddenWorld == nil {
		return fmt.Errorf("hidden world is required")
	}
	if strings.TrimSpace(content.PublicScenario.Title) == "" {
		return fmt.Errorf("public scenario title is required")
	}
	if strings.TrimSpace(content.PublicScenario.Description) == "" {
		return fmt.Errorf("public scenario description is required")
	}
	if strings.TrimSpace(content.PublicScenario.ArchitectureDiagram) != "" {
		if result := diagram.NormalizeMermaidDiagram(content.PublicScenario.ArchitectureDiagram); !result.Valid {
			return fmt.Errorf("public scenario architecture diagram must be valid mermaid: %s", result.Error)
		}
	}
	world := content.HiddenWorld
	if world.CanonicalAnswer == nil {
		return fmt.Errorf("hidden world canonical answer is required")
	}
	answer := world.CanonicalAnswer
	if strings.TrimSpace(answer.CanonicalConclusion) == "" || strings.TrimSpace(answer.RootCauseID) == "" {
		return fmt.Errorf("hidden world canonical answer identity is required")
	}
	if answer.AnswerVersion != "hiddenworld.v2" {
		return fmt.Errorf("hidden world canonical answer version is invalid")
	}
	if answer.RootCauseID != world.RootCause.ID {
		return fmt.Errorf("hidden world canonical answer root cause does not match")
	}
	if strings.TrimSpace(world.RootCause.Description) == "" {
		return fmt.Errorf("hidden world root cause is required")
	}
	if strings.TrimSpace(world.RootCause.ID) == "" || strings.TrimSpace(world.RootCause.Component) == "" {
		return fmt.Errorf("hidden world root cause identity is required")
	}
	if len(world.RootCause.AcceptedHypotheses) == 0 {
		return fmt.Errorf("hidden world accepted hypotheses are required")
	}
	if len(world.RootCause.SufficientEvidenceSets) < 2 {
		return fmt.Errorf("hidden world requires at least two sufficient evidence paths")
	}
	if len(world.Hypotheses) < 4 {
		return fmt.Errorf("hidden world requires at least four hypotheses")
	}
	if len(world.EvidenceGraph) < 6 {
		return fmt.Errorf("hidden world requires at least six evidence nodes")
	}
	if len(world.Observations) < 1 {
		return fmt.Errorf("hidden world observations are required")
	}
	if len(world.MisconceptionRules) < 1 {
		return fmt.Errorf("hidden world misconception rules are required")
	}

	hypothesisIDs := make(map[string]struct{}, len(world.Hypotheses))
	for _, hypothesis := range world.Hypotheses {
		id := strings.TrimSpace(hypothesis.HypothesisID)
		if id == "" || strings.TrimSpace(hypothesis.Label) == "" {
			return fmt.Errorf("hidden world hypothesis identity is required")
		}
		if _, exists := hypothesisIDs[id]; exists {
			return fmt.Errorf("hidden world hypothesis id is duplicated: %s", id)
		}
		hypothesisIDs[id] = struct{}{}
	}
	if _, ok := hypothesisIDs["H_OTHER"]; !ok {
		return fmt.Errorf("hidden world hypotheses must include H_OTHER")
	}
	accepted := make(map[string]struct{}, len(world.RootCause.AcceptedHypotheses))
	for _, id := range world.RootCause.AcceptedHypotheses {
		id = strings.TrimSpace(id)
		if id == "H_OTHER" {
			return fmt.Errorf("H_OTHER cannot be an accepted hypothesis")
		}
		if _, ok := hypothesisIDs[id]; !ok {
			return fmt.Errorf("accepted hypothesis does not exist: %s", id)
		}
		if _, exists := accepted[id]; exists {
			return fmt.Errorf("accepted hypothesis is duplicated: %s", id)
		}
		accepted[id] = struct{}{}
	}

	evidenceIDs := make(map[string]struct{}, len(world.EvidenceGraph))
	causalRelations := make(map[string]struct{}, len(world.DiagnosticRelations))
	for _, relation := range world.DiagnosticRelations {
		if strings.TrimSpace(relation) != "" {
			causalRelations[relation] = struct{}{}
		}
	}
	for _, relation := range answer.RequiredCausalRelations {
		if _, ok := causalRelations[relation]; !ok {
			return fmt.Errorf("hidden world canonical answer references unknown causal relation: %s", relation)
		}
	}
	prerequisiteGraph := make(map[string][]string, len(world.EvidenceGraph))
	for _, node := range world.EvidenceGraph {
		id := strings.TrimSpace(node.EvidenceID)
		if id == "" || strings.TrimSpace(node.Content) == "" || len(node.ObtainedBy) == 0 {
			return fmt.Errorf("hidden world evidence node is incomplete")
		}
		if _, exists := evidenceIDs[id]; exists {
			return fmt.Errorf("hidden world evidence id is duplicated: %s", id)
		}
		evidenceIDs[id] = struct{}{}
		prerequisiteGraph[id] = append([]string{}, node.Prerequisites...)
	}
	for _, evidenceID := range answer.RequiredEvidenceIDs {
		if _, ok := evidenceIDs[evidenceID]; !ok {
			return fmt.Errorf("hidden world canonical answer references unknown evidence: %s", evidenceID)
		}
	}
	if !setEqual(answer.SolutionRequirements, world.RootCause.SolutionRequirements) {
		return fmt.Errorf("hidden world canonical answer solution requirements drift")
	}
	for id, prerequisites := range prerequisiteGraph {
		for _, prerequisite := range prerequisites {
			if _, ok := evidenceIDs[prerequisite]; !ok {
				return fmt.Errorf("evidence prerequisite does not exist: %s -> %s", id, prerequisite)
			}
			if prerequisite == id {
				return fmt.Errorf("evidence cannot depend on itself: %s", id)
			}
		}
	}
	if hiddenWorldGraphHasCycle(prerequisiteGraph) {
		return fmt.Errorf("hidden world evidence prerequisites contain a cycle")
	}

	observationIDs := make(map[string]struct{}, len(world.Observations))
	producedBy := make(map[string]string, len(world.EvidenceGraph))
	negativeCount := 0
	ruledOut := make(map[string]struct{})
	for _, observation := range world.Observations {
		action := strings.TrimSpace(observation.Action)
		if action == "" || strings.TrimSpace(observation.Result) == "" {
			return fmt.Errorf("hidden world observation is incomplete")
		}
		if _, exists := observationIDs[action]; exists {
			return fmt.Errorf("observation action is duplicated: %s", action)
		}
		observationIDs[action] = struct{}{}
		if observation.IsNegative {
			negativeCount++
		}
		for _, evidenceID := range observation.YieldsEvidence {
			if _, ok := evidenceIDs[evidenceID]; !ok {
				return fmt.Errorf("observation yields unknown evidence: %s", evidenceID)
			}
			if previous, exists := producedBy[evidenceID]; exists {
				return fmt.Errorf("evidence is produced by multiple observations: %s and %s", previous, action)
			}
			producedBy[evidenceID] = action
		}
		for _, hypothesisID := range observation.RulesOut {
			if _, ok := hypothesisIDs[hypothesisID]; !ok {
				return fmt.Errorf("observation rules out unknown hypothesis: %s", hypothesisID)
			}
			ruledOut[hypothesisID] = struct{}{}
		}
	}
	if negativeCount == 0 {
		return fmt.Errorf("hidden world requires at least one negative observation")
	}
	for _, tool := range world.VirtualTools {
		if strings.TrimSpace(tool.ToolID) == "" || strings.TrimSpace(tool.Kind) == "" || strings.TrimSpace(tool.Target) == "" || strings.TrimSpace(tool.SimulatedOutput) == "" || strings.TrimSpace(tool.ObservationAction) == "" {
			return fmt.Errorf("virtual tool is incomplete")
		}
		if _, ok := observationIDs[tool.ObservationAction]; !ok {
			return fmt.Errorf("virtual tool references unknown observation: %s", tool.ObservationAction)
		}
		for _, evidenceID := range tool.EvidenceIDs {
			if _, ok := evidenceIDs[evidenceID]; !ok {
				return fmt.Errorf("virtual tool references unknown evidence: %s", evidenceID)
			}
		}
	}

	for index, evidenceSet := range world.RootCause.SufficientEvidenceSets {
		if len(evidenceSet) == 0 {
			return fmt.Errorf("sufficient evidence path %d is empty", index)
		}
		for _, evidenceID := range evidenceSet {
			if _, ok := evidenceIDs[evidenceID]; !ok {
				return fmt.Errorf("sufficient evidence path references unknown evidence: %s", evidenceID)
			}
		}
	}
	for _, rule := range world.MisconceptionRules {
		if strings.TrimSpace(rule.MisconceptionID) == "" || strings.TrimSpace(rule.WhyWrong) == "" {
			return fmt.Errorf("hidden world misconception rule is incomplete")
		}
		for _, hypothesisID := range rule.PatternHypotheses {
			if _, ok := hypothesisIDs[hypothesisID]; !ok {
				return fmt.Errorf("misconception rule references unknown hypothesis: %s", hypothesisID)
			}
		}
	}

	reachable := make(map[string]struct{}, len(evidenceIDs))
	for {
		grew := false
		for evidenceID, action := range producedBy {
			_ = action
			if _, already := reachable[evidenceID]; already {
				continue
			}
			ready := true
			for _, prerequisite := range prerequisiteGraph[evidenceID] {
				if _, obtained := reachable[prerequisite]; !obtained {
					ready = false
					break
				}
			}
			if ready {
				reachable[evidenceID] = struct{}{}
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	if len(reachable) != len(evidenceIDs) {
		return fmt.Errorf("hidden world evidence graph contains unreachable evidence")
	}
	completablePath := false
	for _, evidenceSet := range world.RootCause.SufficientEvidenceSets {
		pathReady := true
		for _, evidenceID := range evidenceSet {
			if _, ok := reachable[evidenceID]; !ok {
				pathReady = false
				break
			}
		}
		if pathReady {
			completablePath = true
			break
		}
	}
	if !completablePath {
		return fmt.Errorf("hidden world has no reachable sufficient evidence path")
	}
	for hypothesisID := range hypothesisIDs {
		if hypothesisID == "H_OTHER" {
			continue
		}
		if _, isAccepted := accepted[hypothesisID]; isAccepted {
			continue
		}
		if _, isRuledOut := ruledOut[hypothesisID]; !isRuledOut {
			return fmt.Errorf("distractor hypothesis has no ruling-out observation: %s", hypothesisID)
		}
	}
	return nil
}

func hiddenWorldGraphHasCycle(graph map[string][]string) bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	colors := make(map[string]int, len(graph))
	var visit func(string) bool
	visit = func(node string) bool {
		switch colors[node] {
		case gray:
			return true
		case black:
			return false
		}
		colors[node] = gray
		for _, prerequisite := range graph[node] {
			if visit(prerequisite) {
				return true
			}
		}
		colors[node] = black
		return false
	}
	for node := range graph {
		if visit(node) {
			return true
		}
	}
	return false
}

func setEqual(left, right []string) bool {
	leftSet := make(map[string]struct{}, len(left))
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range left {
		leftSet[strings.TrimSpace(value)] = struct{}{}
	}
	for _, value := range right {
		rightSet[strings.TrimSpace(value)] = struct{}{}
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for value := range leftSet {
		if _, ok := rightSet[value]; !ok {
			return false
		}
	}
	return true
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

func ValidateInterviewOpening(out InterviewOpeningRewrite) error {
	opening := strings.TrimSpace(out.Opening)
	if opening == "" {
		return fmt.Errorf("interview opening is required")
	}
	if len([]rune(opening)) < 8 {
		return fmt.Errorf("interview opening is too short")
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
