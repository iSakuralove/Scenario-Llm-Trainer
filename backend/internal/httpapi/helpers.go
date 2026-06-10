package httpapi

import (
	"strings"

	"situational-teaching/backend/internal/ai"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func scoreIf(answer string, keywords []string, points int) int {
	if ai.ContainsAny(answer, keywords) {
		return points
	}
	return 0
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "待补充"
	}
	if len([]rune(text)) > 42 {
		return string([]rune(text)[:42]) + "..."
	}
	return text
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
