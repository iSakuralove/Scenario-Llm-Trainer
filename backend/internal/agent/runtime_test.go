package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRuntimeRecordsSuccessfulTrace(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		Agent: "diagnostic_agent",
		Mode:  "server_react",
		NewRunID: func() string {
			return "run-1"
		},
	})

	trace, err := runtime.Execute(context.Background(), []Step{
		{
			Name: "detect_root_cause_leak",
			Kind: "tool",
			Run: func(context.Context, *StepRecorder) (ToolResult, error) {
				return ToolResult{
					Summary:  "已完成防泄露检查",
					Metadata: map[string]string{"leak_detected": "false"},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trace.RunID != "run-1" || trace.Agent != "diagnostic_agent" || trace.Mode != "server_react" {
		t.Fatalf("unexpected trace header: %#v", trace)
	}
	if trace.ToolCount != 1 {
		t.Fatalf("expected 1 tool call, got %d", trace.ToolCount)
	}
	if len(trace.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(trace.Steps))
	}
	step := trace.Steps[0]
	if strings.Contains(step.Name, "root_cause") || step.Status != "success" {
		t.Fatalf("unexpected step: %#v", step)
	}
	if step.Metadata["leak_detected"] != "false" {
		t.Fatalf("metadata was not recorded: %#v", step.Metadata)
	}
}

func TestRuntimeRecordsFailedStep(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		Agent: "diagnostic_agent",
		Mode:  "server_react",
		NewRunID: func() string {
			return "run-2"
		},
	})
	expectedErr := errors.New("tool failed")

	trace, err := runtime.Execute(context.Background(), []Step{
		{
			Name: "find_triggered_clue",
			Kind: "tool",
			Run: func(context.Context, *StepRecorder) (ToolResult, error) {
				return ToolResult{Summary: "线索匹配失败"}, expectedErr
			},
		},
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected tool error, got %v", err)
	}
	if len(trace.Steps) != 1 {
		t.Fatalf("expected failed step, got %#v", trace.Steps)
	}
	if trace.Steps[0].Status != "failed" {
		t.Fatalf("expected failed status, got %#v", trace.Steps[0])
	}
	if !strings.Contains(trace.Steps[0].Summary, "线索匹配失败") {
		t.Fatalf("expected failure summary, got %q", trace.Steps[0].Summary)
	}
}

func TestRuntimeFiltersSensitiveTraceMetadata(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		Agent: "diagnostic_agent",
		Mode:  "server_react",
		NewRunID: func() string {
			return "run-3"
		},
	})

	trace, err := runtime.Execute(context.Background(), []Step{
		{
			Name: "safety_rewrite",
			Kind: "tool",
			Run: func(context.Context, *StepRecorder) (ToolResult, error) {
				return ToolResult{
					Summary: "已过滤 root_cause 和 standard_procedure",
					Metadata: map[string]string{
						"root_cause":         "连接池耗尽",
						"standard_procedure": "查看完整步骤",
						"safe":               "ok",
					},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	serialized := trace.Steps[0].Summary + " " + strings.Join(mapKeys(trace.Steps[0].Metadata), " ")
	if strings.Contains(serialized, "root_cause") || strings.Contains(serialized, "standard_procedure") || strings.Contains(serialized, "连接池耗尽") {
		t.Fatalf("trace contains sensitive data: %#v", trace.Steps[0])
	}
	if trace.Steps[0].Metadata["safe"] != "ok" {
		t.Fatalf("safe metadata should remain: %#v", trace.Steps[0].Metadata)
	}
}

func TestRuntimeRedactsSensitiveTraceNameAndSummary(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		Agent:          "DiagnosticAgent",
		Mode:           "server_react",
		ForbiddenTerms: []string{"索引缺失导致慢查询"},
		NewRunID: func() string {
			return "run-4"
		},
	})

	trace, err := runtime.Execute(context.Background(), []Step{
		{
			Name: "check_RootCause_and_standardProcedure",
			Kind: "tool",
			Run: func(context.Context, *StepRecorder) (ToolResult, error) {
				return ToolResult{
					Summary: "根因是索引缺失导致慢查询，标准步骤是直接扩容",
					Metadata: map[string]string{
						"RootCause": "索引缺失导致慢查询",
						"safe":      "ok",
					},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	step := trace.Steps[0]
	serialized := step.Name + " " + step.Summary + " " + strings.Join(mapKeys(step.Metadata), " ") + " " + strings.Join(mapValues(step.Metadata), " ")
	for _, forbidden := range []string{"RootCause", "root_cause", "standardProcedure", "索引缺失导致慢查询", "标准步骤", "根因"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("trace leaked %q in %#v", forbidden, step)
		}
	}
	if step.Metadata["safe"] != "ok" {
		t.Fatalf("safe metadata should remain: %#v", step.Metadata)
	}
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func mapValues(values map[string]string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}
