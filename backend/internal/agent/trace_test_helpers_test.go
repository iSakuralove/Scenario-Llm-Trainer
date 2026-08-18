package agent

import (
	"strings"

	"situational-teaching/backend/internal/domain"
)

func mustTraceText(trace *domain.AgentTrace) string {
	if trace == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(trace.RunID)
	builder.WriteString(trace.Agent)
	builder.WriteString(trace.Mode)
	for _, step := range trace.Steps {
		builder.WriteString(step.Name)
		builder.WriteString(step.Kind)
		builder.WriteString(step.Status)
		builder.WriteString(step.Summary)
		for key, value := range step.Metadata {
			builder.WriteString(key)
			builder.WriteString(value)
		}
	}
	return builder.String()
}
