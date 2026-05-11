package diagram

import (
	"fmt"
	"regexp"
	"strings"

	"situational-teaching/backend/internal/domain"
)

type MermaidResult struct {
	Code     string
	Status   string
	Warnings []string
	Valid    bool
	Error    string
}

var (
	allowedHeaderPattern = regexp.MustCompile(`^(graph|flowchart)\s+(TD|TB|BT|LR|RL)$`)
	allowedDirection     = regexp.MustCompile(`^(TD|LR)$`)
	diagramNodeIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
	nodeIDPattern        = regexp.MustCompile(`[A-Za-z0-9_]+`)
	unsafeTokens         = []string{"<script", "</script", "<iframe", "</iframe", "javascript:", "click ", "href ", "style ", "classDef "}
)

func BuildMermaidFromSpec(spec domain.ScenarioDiagramSpec) MermaidResult {
	direction := strings.TrimSpace(spec.Direction)
	if direction == "" {
		direction = "TD"
	}
	if !allowedDirection.MatchString(direction) {
		return MermaidResult{Status: "invalid", Valid: false, Error: "structured diagram direction must be TD or LR"}
	}
	if len(spec.Nodes) == 0 {
		return MermaidResult{Status: "invalid", Valid: false, Error: "structured diagram requires nodes"}
	}
	if len(spec.Edges) == 0 {
		return MermaidResult{Status: "invalid", Valid: false, Error: "structured diagram requires edges"}
	}
	labels := map[string]string{}
	lines := []string{"graph " + direction}
	for _, node := range spec.Nodes {
		id := strings.TrimSpace(node.ID)
		if !diagramNodeIDPattern.MatchString(id) {
			return MermaidResult{Status: "invalid", Valid: false, Error: fmt.Sprintf("structured diagram node id %q is invalid", id)}
		}
		if _, exists := labels[id]; exists {
			return MermaidResult{Status: "invalid", Valid: false, Error: fmt.Sprintf("structured diagram node id %q is duplicated", id)}
		}
		labels[id] = safeLabel(node.Label)
		lines = append(lines, fmt.Sprintf("%s[\"%s\"]", id, labels[id]))
	}
	for _, edge := range spec.Edges {
		from := strings.TrimSpace(edge.From)
		to := strings.TrimSpace(edge.To)
		if _, ok := labels[from]; !ok {
			return MermaidResult{Status: "invalid", Valid: false, Error: fmt.Sprintf("structured diagram edge source %q is missing", from)}
		}
		if _, ok := labels[to]; !ok {
			return MermaidResult{Status: "invalid", Valid: false, Error: fmt.Sprintf("structured diagram edge target %q is missing", to)}
		}
		arrow := "-->"
		style := strings.TrimSpace(edge.Style)
		switch style {
		case "", "solid":
		case "dotted":
			arrow = "-.->"
		default:
			return MermaidResult{Status: "invalid", Valid: false, Error: fmt.Sprintf("structured diagram edge style %q is invalid", style)}
		}
		label := safeEdgeLabel(edge.Label)
		if label != "" {
			lines = append(lines, fmt.Sprintf("%s %s|\"%s\"| %s", from, arrow, label, to))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", from, arrow, to))
	}
	result := NormalizeMermaidDiagram(strings.Join(lines, "\n"))
	if !result.Valid {
		result.Error = "structured diagram generated invalid mermaid: " + result.Error
		return result
	}
	result.Status = "generated"
	return result
}

func NormalizeMermaidDiagram(raw string) MermaidResult {
	original := strings.TrimSpace(raw)
	code := strings.ReplaceAll(original, "\r\n", "\n")
	code = strings.ReplaceAll(code, "\r", "\n")
	code = strings.TrimSpace(code)
	warnings := []string{}
	if strings.HasPrefix(strings.ToLower(code), "```mermaid") {
		code = strings.TrimSpace(code[len("```mermaid"):])
		if strings.HasSuffix(code, "```") {
			code = strings.TrimSpace(strings.TrimSuffix(code, "```"))
			warnings = append(warnings, "removed_markdown_fence")
		}
	} else if strings.HasPrefix(code, "```") {
		code = strings.TrimSpace(strings.TrimPrefix(code, "```"))
		if strings.HasSuffix(code, "```") {
			code = strings.TrimSpace(strings.TrimSuffix(code, "```"))
			warnings = append(warnings, "removed_markdown_fence")
		}
	}
	code = normalizeLineWhitespace(code)
	if err := validateNormalizedMermaid(code); err != nil {
		return MermaidResult{Code: code, Status: "invalid", Warnings: warnings, Valid: false, Error: err.Error()}
	}
	status := "validated"
	if code != original || len(warnings) > 0 {
		status = "normalized"
	}
	return MermaidResult{Code: code, Status: status, Warnings: warnings, Valid: true}
}

func FallbackScenarioDiagram(question domain.ScenarioQuestion) string {
	result := BuildMermaidFromSpec(FallbackScenarioDiagramSpec(question))
	if result.Valid {
		return result.Code
	}
	title := firstNonEmpty(question.Title, "情景题")
	domainName := firstNonEmpty(question.Domain, "训练领域")
	evidence := firstFrom(question.Content.KeyEvidence, "关键证据")
	step := firstFrom(question.Content.StandardProcedure, "排查步骤")
	return strings.Join([]string{
		"graph TD",
		fmt.Sprintf("A[\"%s\"] --> B[\"%s\"]", safeLabel(title), safeLabel(domainName)),
		fmt.Sprintf("B --> C[\"%s\"]", safeLabel(evidence)),
		fmt.Sprintf("C --> D[\"%s\"]", safeLabel(step)),
	}, "\n")
}

func FallbackScenarioDiagramSpec(question domain.ScenarioQuestion) domain.ScenarioDiagramSpec {
	return domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "A", Label: firstNonEmpty(question.Title, "情景题")},
			{ID: "B", Label: firstNonEmpty(question.Domain, "训练领域")},
			{ID: "C", Label: firstFrom(question.Content.KeyEvidence, "关键证据")},
			{ID: "D", Label: firstFrom(question.Content.StandardProcedure, "排查步骤")},
		},
		Edges: []domain.ScenarioDiagramEdge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
			{From: "C", To: "D"},
		},
	}
}

func normalizeLineWhitespace(code string) string {
	lines := strings.Split(code, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func validateNormalizedMermaid(code string) error {
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("mermaid diagram is empty")
	}
	lower := strings.ToLower(code)
	for _, token := range unsafeTokens {
		if strings.Contains(lower, strings.ToLower(token)) {
			return fmt.Errorf("mermaid diagram contains unsafe token %q", strings.TrimSpace(token))
		}
	}
	if strings.Contains(code, "```") {
		return fmt.Errorf("mermaid diagram must not contain markdown fences")
	}
	lines := strings.Split(code, "\n")
	if len(lines) < 2 {
		return fmt.Errorf("mermaid diagram must contain at least one node or edge")
	}
	if !allowedHeaderPattern.MatchString(lines[0]) {
		return fmt.Errorf("mermaid diagram must start with graph or flowchart direction")
	}
	hasStatement := false
	for _, line := range lines[1:] {
		if !balanced(line, '[', ']') || !balanced(line, '(', ')') || !balanced(line, '{', '}') {
			return fmt.Errorf("mermaid diagram has unbalanced delimiters")
		}
		if hasInvalidPlainSquareLabel(line) {
			return fmt.Errorf("mermaid square labels must not contain raw parentheses or braces")
		}
		if strings.Contains(line, "-->") || strings.Contains(line, "---") || strings.Contains(line, "-.->") || nodeIDPattern.MatchString(line) {
			hasStatement = true
		}
	}
	if !hasStatement {
		return fmt.Errorf("mermaid diagram must contain a node or edge statement")
	}
	return nil
}

func balanced(value string, open, close rune) bool {
	count := 0
	for _, r := range value {
		if r == open {
			count++
		}
		if r == close {
			count--
			if count < 0 {
				return false
			}
		}
	}
	return count == 0
}

func hasInvalidPlainSquareLabel(line string) bool {
	for start := strings.IndexByte(line, '['); start >= 0; {
		end := strings.IndexByte(line[start:], ']')
		if end < 0 {
			return false
		}
		end += start
		segment := line[start : end+1]
		if strings.HasPrefix(segment, "[(") && strings.HasSuffix(segment, ")]") {
			start = strings.IndexByte(line[end+1:], '[')
			if start >= 0 {
				start += end + 1
			}
			continue
		}
		if strings.HasPrefix(segment, "[\"") && strings.HasSuffix(segment, "\"]") {
			start = strings.IndexByte(line[end+1:], '[')
			if start >= 0 {
				start += end + 1
			}
			continue
		}
		inner := segment[1 : len(segment)-1]
		if strings.ContainsAny(inner, "(){}") {
			return true
		}
		start = strings.IndexByte(line[end+1:], '[')
		if start >= 0 {
			start += end + 1
		}
	}
	return false
}

func safeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "未命名节点"
	}
	replacer := strings.NewReplacer("\n", " ", "\r", " ", `"`, "'", "<", "", ">", "", "[", "(", "]", ")", "{", "(", "}", ")")
	value = replacer.Replace(value)
	runes := []rune(value)
	if len(runes) > 36 {
		value = string(runes[:36])
	}
	return value
}

func safeEdgeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = safeLabel(value)
	runes := []rune(value)
	if len(runes) > 24 {
		value = string(runes[:24])
	}
	return value
}

func firstFrom(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
