package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

const (
	stepStatusSuccess = "success"
	stepStatusFailed  = "failed"
)

type ToolResult struct {
	Summary  string
	Metadata map[string]string
}

type StepFunc func(context.Context, *StepRecorder) (ToolResult, error)

type Step struct {
	Name string
	Kind string
	Run  StepFunc
}

type RuntimeConfig struct {
	Agent          string
	Mode           string
	ForbiddenTerms []string
	NewRunID       func() string
	Now            func() time.Time
}

type StepRecorder struct {
	step *domain.AgentStep
}

type Runtime struct {
	config RuntimeConfig
}

func NewRuntime(config RuntimeConfig) *Runtime {
	if config.Agent == "" {
		config.Agent = "agent"
	}
	if config.Mode == "" {
		config.Mode = "server_react"
	}
	if config.NewRunID == nil {
		config.NewRunID = func() string {
			return fmt.Sprintf("agent-%d", time.Now().UnixNano())
		}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	config.ForbiddenTerms = normalizeForbiddenTerms(config.ForbiddenTerms)
	return &Runtime{config: config}
}

func (r *Runtime) Execute(ctx context.Context, steps []Step) (domain.AgentTrace, error) {
	if r == nil {
		r = NewRuntime(RuntimeConfig{})
	}
	startedAt := r.config.Now()
	trace := domain.AgentTrace{
		RunID:     r.safeTraceText(r.config.NewRunID()),
		Agent:     r.safeTraceText(r.config.Agent),
		Mode:      r.safeTraceText(r.config.Mode),
		Steps:     []domain.AgentStep{},
		StartedAt: startedAt,
	}
	for _, step := range steps {
		if step.Run == nil {
			continue
		}
		start := r.config.Now()
		record := domain.AgentStep{
			Name:      r.safeTraceText(step.Name),
			Kind:      r.safeTraceText(firstNonEmpty(step.Kind, "tool")),
			Status:    stepStatusSuccess,
			StartedAt: start,
		}
		recorder := &StepRecorder{step: &record}
		result, err := step.Run(ctx, recorder)
		record.Summary = r.safeTraceText(firstNonEmpty(result.Summary, record.Summary))
		record.Metadata = r.safeTraceMetadata(mergeMetadata(record.Metadata, result.Metadata))
		record.EndedAt = r.config.Now()
		if err != nil {
			record.Status = stepStatusFailed
			if record.Summary == "" {
				record.Summary = "步骤执行失败"
			}
			trace.Steps = append(trace.Steps, record)
			trace.ToolCount = countToolSteps(trace.Steps)
			trace.FinishedAt = r.config.Now()
			return trace, err
		}
		if record.Summary == "" {
			record.Summary = "步骤执行完成"
		}
		trace.Steps = append(trace.Steps, record)
	}
	trace.ToolCount = countToolSteps(trace.Steps)
	trace.FinishedAt = r.config.Now()
	return trace, nil
}

func (r *Runtime) RunTool(ctx context.Context, name string, fn func(context.Context) (ToolResult, error)) (domain.AgentTrace, ToolResult, error) {
	var toolResult ToolResult
	trace, err := r.Execute(ctx, []Step{
		{
			Name: name,
			Kind: "tool",
			Run: func(ctx context.Context, _ *StepRecorder) (ToolResult, error) {
				result, err := fn(ctx)
				toolResult = result
				return result, err
			},
		},
	})
	return trace, toolResult, err
}

func (r *StepRecorder) SetSummary(summary string) {
	if r == nil || r.step == nil {
		return
	}
	r.step.Summary = safeTraceText(summary)
}

func (r *StepRecorder) SetMetadata(key, value string) {
	if r == nil || r.step == nil {
		return
	}
	if r.step.Metadata == nil {
		r.step.Metadata = map[string]string{}
	}
	key = safeTraceText(key)
	if key == "" {
		return
	}
	r.step.Metadata[key] = safeTraceText(value)
}

func countToolSteps(steps []domain.AgentStep) int {
	count := 0
	for _, step := range steps {
		if step.Kind == "tool" {
			count++
		}
	}
	return count
}

func mergeMetadata(left, right map[string]string) map[string]string {
	if len(left) == 0 {
		return right
	}
	out := map[string]string{}
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (r *Runtime) safeTraceMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		safeKey := r.safeTraceText(key)
		safeValue := r.safeTraceText(value)
		if safeKey == "" || safeKey == "[redacted]" || safeValue == "[redacted]" {
			continue
		}
		output[safeKey] = safeValue
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func safeTraceText(input string) string {
	return NewRuntime(RuntimeConfig{}).safeTraceText(input)
}

func (r *Runtime) safeTraceText(input string) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}
	for _, field := range append(sensitiveTraceTerms(), r.config.ForbiddenTerms...) {
		text = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(field)).ReplaceAllString(text, "[redacted]")
	}
	if r.isSensitiveTraceText(text) {
		return "[redacted]"
	}
	ipPattern := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	text = ipPattern.ReplaceAllString(text, "[redacted]")
	credentialPattern := regexp.MustCompile(`(?i)\b(password|passwd|api_key|apikey|token|secret|key)\s*[:=]\s*[^\s,;]+`)
	text = credentialPattern.ReplaceAllString(text, "[redacted]")
	return text
}

func isSensitiveTraceText(input string) bool {
	return NewRuntime(RuntimeConfig{}).isSensitiveTraceText(input)
}

func (r *Runtime) isSensitiveTraceText(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return false
	}
	for _, term := range append(sensitiveTraceTerms(), r.config.ForbiddenTerms...) {
		if strings.Contains(normalized, strings.ToLower(term)) {
			return true
		}
	}
	for _, content := range []string{"连接池耗尽", "标准步骤", "关键证据", "根因"} {
		if strings.Contains(normalized, strings.ToLower(content)) {
			return true
		}
	}
	return false
}

func sensitiveTraceTerms() []string {
	return []string{
		"root_cause", "root cause", "rootcause",
		"standard_procedure", "standard procedure", "standardprocedure",
		"key_evidence", "key evidence", "keyevidence",
		"reveal_strategy", "reveal strategy", "revealstrategy",
	}
}

func normalizeForbiddenTerms(terms []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 {
			continue
		}
		key := strings.ToLower(term)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, term)
	}
	return out
}
