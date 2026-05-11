package ai

import (
	"strings"
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestValidateScenarioContentRejectsUnsafeMermaid(t *testing.T) {
	content := validScenarioQuestionSample().Content
	content.ArchitectureDiagram = "graph TD\nA[API] --> B[<script>alert(1)</script>]"
	err := ValidateScenarioContent(content, false)
	if err == nil || !strings.Contains(err.Error(), "architecture diagram") {
		t.Fatalf("expected architecture diagram validation error, got %v", err)
	}
}

func TestPrepareScenarioQuestionNormalizesFencedMermaid(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagramSpec = nil
	question.Content.ArchitectureDiagram = "```mermaid\ngraph TD\nA[API] --> B[DB]\n```"
	prepared := PrepareScenarioQuestion(question)
	if prepared.Content.ArchitectureDiagram != "graph TD\nA[API] --> B[DB]" {
		t.Fatalf("unexpected normalized diagram: %q", prepared.Content.ArchitectureDiagram)
	}
	if prepared.Content.DiagramStatus != "normalized" {
		t.Fatalf("expected normalized diagram status, got %+v", prepared.Content)
	}
}

func TestPrepareScenarioQuestionUsesFallbackForInvalidMermaid(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagramSpec = nil
	question.Content.ArchitectureDiagram = "not mermaid"
	prepared := PrepareScenarioQuestion(question)
	if !strings.HasPrefix(prepared.Content.ArchitectureDiagram, "graph TD\n") {
		t.Fatalf("expected fallback diagram, got %q", prepared.Content.ArchitectureDiagram)
	}
	if prepared.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected fallback status, got %+v", prepared.Content)
	}
}

func TestPrepareScenarioQuestionKeepsFallbackStatusWhenReprepared(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagramSpec = nil
	question.Content.ArchitectureDiagram = "not mermaid"
	prepared := PrepareScenarioQuestion(question)
	prepared.Content.ArchitectureDiagramSpec = nil
	reprepared := PrepareScenarioQuestion(prepared)
	if reprepared.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected fallback status to survive reprepare, got %+v", reprepared.Content)
	}
}

func TestPrepareScenarioQuestionBuildsMermaidFromStructuredSpec(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagram = "graph TD\nD --> E[错误IP(无服务)]"
	question.Content.ArchitectureDiagramSpec = &domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes: []domain.ScenarioDiagramNode{
			{ID: "D", Label: "上游权威服务器"},
			{ID: "E", Label: "错误IP(无服务)"},
		},
		Edges: []domain.ScenarioDiagramEdge{{From: "D", To: "E", Label: "返回"}},
	}

	prepared := PrepareScenarioQuestion(question)
	if prepared.Content.DiagramStatus != "generated" {
		t.Fatalf("expected generated status from structured spec, got %+v", prepared.Content)
	}
	if strings.Contains(prepared.Content.ArchitectureDiagram, "E[错误IP(无服务)]") {
		t.Fatalf("expected raw invalid mermaid to be replaced, got %q", prepared.Content.ArchitectureDiagram)
	}
	if !strings.Contains(prepared.Content.ArchitectureDiagram, `E["错误IP(无服务)"]`) {
		t.Fatalf("expected quoted mermaid generated from spec, got %q", prepared.Content.ArchitectureDiagram)
	}
}

func TestPrepareScenarioQuestionUsesFallbackForInvalidStructuredSpec(t *testing.T) {
	question := validScenarioQuestionSample()
	question.Content.ArchitectureDiagramSpec = &domain.ScenarioDiagramSpec{
		Direction: "TD",
		Nodes:     []domain.ScenarioDiagramNode{{ID: "A", Label: "API"}},
		Edges:     []domain.ScenarioDiagramEdge{{From: "A", To: "Missing"}},
	}

	prepared := PrepareScenarioQuestion(question)
	if prepared.Content.DiagramStatus != "fallback" {
		t.Fatalf("expected invalid structured spec to use fallback, got %+v", prepared.Content)
	}
	if !strings.Contains(strings.Join(prepared.Content.DiagramWarnings, " "), "structured diagram") {
		t.Fatalf("expected structured warning, got %+v", prepared.Content.DiagramWarnings)
	}
	if prepared.Content.ArchitectureDiagramSpec == nil {
		t.Fatal("expected fallback to replace invalid structured spec with a safe spec")
	}
	if len(prepared.Content.ArchitectureDiagramSpec.Edges) != 3 {
		t.Fatalf("expected fallback spec to replace invalid structured spec, got %+v", prepared.Content.ArchitectureDiagramSpec)
	}
}
