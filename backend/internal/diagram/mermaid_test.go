package diagram

import (
	"strings"
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestNormalizeMermaidDiagramStripsFenceAndKeepsGraph(t *testing.T) {
	result := NormalizeMermaidDiagram("```mermaid\r\ngraph TD\r\nA[API] --> B[DB]\r\n```")
	if result.Status != "normalized" {
		t.Fatalf("expected normalized status, got %+v", result)
	}
	if result.Code != "graph TD\nA[API] --> B[DB]" {
		t.Fatalf("unexpected normalized code: %q", result.Code)
	}
}

func TestNormalizeMermaidDiagramRejectsUnsafeDirectives(t *testing.T) {
	result := NormalizeMermaidDiagram("graph TD\nA[API] --> B[<script>alert(1)</script>]")
	if result.Valid {
		t.Fatalf("expected unsafe diagram to be invalid, got %+v", result)
	}
	if !strings.Contains(result.Error, "unsafe") {
		t.Fatalf("expected unsafe error, got %+v", result)
	}
}

func TestNormalizeMermaidDiagramRejectsParenthesesInsideSquareLabels(t *testing.T) {
	result := NormalizeMermaidDiagram("graph TD\nD --> E[错误IP(无服务)]")
	if result.Valid {
		t.Fatalf("expected diagram with raw parentheses inside square label to be invalid, got %+v", result)
	}
}

func TestNormalizeMermaidDiagramRejectsConcatenatedEdgeAfterSquareLabel(t *testing.T) {
	result := NormalizeMermaidDiagram("graph TD\nD --> E[错误IP(无服务)]B --> F[正常权威服务器]")
	if result.Valid {
		t.Fatalf("expected concatenated edge statement to be invalid, got %+v", result)
	}
}

func TestFallbackScenarioDiagramIsValid(t *testing.T) {
	question := domain.ScenarioQuestion{
		Title:      "连接池耗尽导致请求排队",
		Domain:     "database",
		Difficulty: "L3",
		Content: domain.ScenarioContent{
			KeyEvidence:       []string{"活跃连接接近上限"},
			StandardProcedure: []string{"查看接口耗时", "检查连接池指标"},
		},
	}
	code := FallbackScenarioDiagram(question)
	result := NormalizeMermaidDiagram(code)
	if !result.Valid {
		t.Fatalf("expected fallback diagram to be valid, code=%q result=%+v", code, result)
	}
	if !strings.Contains(code, "graph TD") || strings.Contains(code, "<script>") {
		t.Fatalf("unexpected fallback diagram: %q", code)
	}
}

func TestBuildMermaidFromSpecQuotesLabelsAndValidatesOutput(t *testing.T) {
	spec := domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "Client", Label: "内网客户端"},
			{ID: "DNS", Label: "内网 DNS 递归器"},
			{ID: "BadIP", Label: "错误IP(无服务): vip/10.0.0.5"},
		},
		Edges: []domain.ScenarioDiagramEdge{
			{From: "Client", To: "DNS"},
			{From: "DNS", To: "BadIP", Label: "异常解析"},
		},
	}

	result := BuildMermaidFromSpec(spec)
	if !result.Valid {
		t.Fatalf("expected structured diagram to build, got %+v", result)
	}
	if !strings.Contains(result.Code, `BadIP["错误IP(无服务): vip/10.0.0.5"]`) {
		t.Fatalf("expected quoted label with punctuation, got %q", result.Code)
	}
	if normalized := NormalizeMermaidDiagram(result.Code); !normalized.Valid {
		t.Fatalf("generated mermaid must pass normalizer: %+v code=%q", normalized, result.Code)
	}
}

func TestBuildMermaidFromSpecRejectsUnsafeNodeIDs(t *testing.T) {
	result := BuildMermaidFromSpec(domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "A", Label: "API"},
			{ID: "bad-id", Label: "Bad"},
		},
		Edges: []domain.ScenarioDiagramEdge{{From: "A", To: "bad-id"}},
	})
	if result.Valid {
		t.Fatalf("expected unsafe node id to fail, got %+v", result)
	}
}

func TestBuildMermaidFromSpecRejectsUnknownEdgeStyle(t *testing.T) {
	result := BuildMermaidFromSpec(domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "A", Label: "API"},
			{ID: "B", Label: "DB"},
		},
		Edges: []domain.ScenarioDiagramEdge{{From: "A", To: "B", Style: "thick"}},
	})
	if result.Valid {
		t.Fatalf("expected unknown edge style to fail, got %+v", result)
	}
}
