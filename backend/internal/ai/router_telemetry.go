package ai

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const routerHistoryLimit = 24

type RouterTelemetry struct {
	TotalCalls         int64             `json:"total_calls"`
	SuccessfulCalls    int64             `json:"successful_calls"`
	FailedCalls        int64             `json:"failed_calls"`
	FallbackCalls      int64             `json:"fallback_calls"`
	StreamCalls        int64             `json:"stream_calls"`
	JSONCalls          int64             `json:"json_calls"`
	SafetyRewrites     int64             `json:"safety_rewrites"`
	ValidationErrors   int64             `json:"validation_errors"`
	ProviderCalls      map[string]int64  `json:"provider_calls"`
	TaskCalls          map[string]int64  `json:"task_calls"`
	RecentAttempts     []FallbackAttempt `json:"recent_attempts,omitempty"`
	RecentDecisions    []RouterDecision  `json:"recent_decisions"`
	LastDecision       *RouterDecision   `json:"last_decision,omitempty"`
	LastError          string            `json:"last_error,omitempty"`
	LastErrorType      string            `json:"last_error_type,omitempty"`
	LastFallbackReason string            `json:"last_fallback_reason,omitempty"`
	LastFallbackError  string            `json:"last_fallback_error,omitempty"`
	LastErrorAt        *time.Time        `json:"last_error_at,omitempty"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type routerTelemetryStore struct {
	mu        sync.Mutex
	summary   RouterTelemetry
	providers map[string]int64
	tasks     map[string]int64
	recent    []RouterDecision
}

func newRouterTelemetryStore() *routerTelemetryStore {
	return &routerTelemetryStore{
		providers: map[string]int64{},
		tasks:     map[string]int64{},
	}
}

func (s *routerTelemetryStore) record(decision RouterDecision, meta CallMeta, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	isStatusCheck := decision.Task == RouterTaskStatusCheck
	if !isStatusCheck {
		s.summary.TotalCalls++
		if decision.Stream {
			s.summary.StreamCalls++
		}
		if decision.OutputMode == OutputModeJSON {
			s.summary.JSONCalls++
		}
		if meta.FallbackUsed {
			s.summary.FallbackCalls++
		}
		if len(decision.FallbackAttempts) > 0 {
			s.summary.RecentAttempts = append([]FallbackAttempt{}, decision.FallbackAttempts...)
		}
		if meta.SafetyRewritten {
			s.summary.SafetyRewrites++
		}
		if decision.Validation.Status == "failed" {
			s.summary.ValidationErrors++
		}
		if err != nil {
			s.summary.FailedCalls++
			s.summary.LastError = sanitizeErrorMessage(err.Error())
			s.summary.LastErrorType = classifyRouterError(err)
			s.summary.LastErrorAt = &now
		} else {
			s.summary.SuccessfulCalls++
		}
	}
	if meta.FallbackUsed && decision.Meta != nil {
		if reason := strings.TrimSpace(decision.Meta["fallback_reason"]); reason != "" {
			s.summary.LastFallbackReason = reason
		}
		if fallbackError := strings.TrimSpace(decision.Meta["fallback_error"]); fallbackError != "" {
			s.summary.LastFallbackError = fallbackError
		}
	}
	provider := decision.Provider
	if provider == "" {
		provider = meta.Provider
	}
	if provider == "" {
		provider = "unknown"
	}
	task := decision.Task
	if task == "" {
		task = "unknown"
	}
	if !isStatusCheck {
		s.providers[provider]++
		s.tasks[task]++
	}
	decision.ErrorMessage = sanitizeErrorMessage(decision.ErrorMessage)
	s.recent = append([]RouterDecision{decision}, s.recent...)
	if len(s.recent) > routerHistoryLimit {
		s.recent = s.recent[:routerHistoryLimit]
	}
	copied := decision
	s.summary.LastDecision = &copied
	s.summary.UpdatedAt = now
}

func (s *routerTelemetryStore) snapshot() RouterTelemetry {
	if s == nil {
		return RouterTelemetry{UpdatedAt: time.Now()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := s.summary
	out.ProviderCalls = copyIntMap(s.providers)
	out.TaskCalls = copyIntMap(s.tasks)
	out.RecentDecisions = append([]RouterDecision(nil), s.recent...)
	out.RecentAttempts = append([]FallbackAttempt(nil), s.summary.RecentAttempts...)
	if s.summary.LastDecision != nil {
		copied := *s.summary.LastDecision
		out.LastDecision = &copied
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now()
	}
	return out
}

func copyIntMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var routerTraceSeq uint64

func nextRouterTraceID(task string) string {
	seq := atomic.AddUint64(&routerTraceSeq, 1)
	normalized := strings.ReplaceAll(strings.TrimSpace(task), "_", "-")
	if normalized == "" {
		normalized = "task"
	}
	return fmt.Sprintf("llm-router-%s-%d", normalized, seq)
}

func classifyRouterError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "schema"), strings.Contains(message, "json"), strings.Contains(message, "validate"):
		return "validation"
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline"):
		return "timeout"
	case strings.Contains(message, "rate"), strings.Contains(message, "429"):
		return "rate_limit"
	case strings.Contains(message, "auth"), strings.Contains(message, "key"), strings.Contains(message, "401"), strings.Contains(message, "403"):
		return "auth"
	case strings.Contains(message, "provider"), strings.Contains(message, "llm"):
		return "provider"
	default:
		return "unknown"
	}
}

func sanitizeErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	return Sanitize(truncate(message, 160))
}

func truncate(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
