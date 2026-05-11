package ai

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"situational-teaching/backend/internal/domain"
)

var wordPattern = regexp.MustCompile(`[a-zA-Z0-9_\-]+|[\p{Han}]+`)

func ContainsAny(input string, keywords []string) bool {
	normalized := strings.ToLower(input)
	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		if strings.Contains(normalized, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func RootCauseMatch(input, rootCause string, keywords []string) int {
	if strings.TrimSpace(input) == "" {
		return 0
	}
	score := int(math.Round(Similarity(input, rootCause) * 100))
	if len(keywords) > 0 {
		hits := 0
		for _, keyword := range keywords {
			if ContainsAny(input, []string{keyword}) {
				hits++
			}
		}
		keywordScore := int(math.Round(float64(hits) / float64(len(keywords)) * 100))
		if keywordScore > score {
			score = keywordScore
		}
	}
	if score > 100 {
		return 100
	}
	return score
}

func Similarity(left, right string) float64 {
	leftTokens := tokenSet(left)
	rightTokens := tokenSet(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}
	intersections := 0
	for token := range leftTokens {
		if rightTokens[token] {
			intersections++
		}
	}
	return float64(intersections) / math.Sqrt(float64(len(leftTokens)*len(rightTokens)))
}

func FindTriggeredClue(strategy domain.RevealStrategy, input string, revealed []string) (domain.Clue, bool) {
	revealedSet := map[string]bool{}
	for _, clueID := range revealed {
		revealedSet[clueID] = true
	}
	for _, clue := range strategy.SurfaceClues {
		if !revealedSet[clue.ClueID] && ContainsAny(input, clue.TriggerKeywords) {
			return clue, true
		}
	}
	for _, clue := range strategy.DeepClues {
		if revealedSet[clue.ClueID] || !ContainsAny(input, clue.TriggerKeywords) {
			continue
		}
		ready := true
		for _, prerequisite := range clue.PrerequisiteClues {
			if !revealedSet[prerequisite] {
				ready = false
				break
			}
		}
		if ready {
			return clue, true
		}
	}
	for _, clue := range strategy.Distractors {
		if !revealedSet[clue.ClueID] && ContainsAny(input, clue.TriggerKeywords) {
			return clue, true
		}
	}
	return domain.Clue{}, false
}

func NextHint(strategy domain.RevealStrategy, revealed []string) string {
	revealedSet := map[string]bool{}
	for _, clueID := range revealed {
		revealedSet[clueID] = true
	}
	for _, clue := range strategy.SurfaceClues {
		if !revealedSet[clue.ClueID] {
			return clue.RecommendedNextAsk
		}
	}
	for _, clue := range strategy.DeepClues {
		if !revealedSet[clue.ClueID] {
			if clue.RecommendedNextAsk != "" {
				return clue.RecommendedNextAsk
			}
			if len(clue.TriggerKeywords) > 0 {
				return "可以继续追问：" + clue.TriggerKeywords[0]
			}
		}
	}
	return "可以整理现有证据并提交你的根因判断。"
}

func ContainsSensitiveInfo(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "password=") || strings.Contains(lower, "passwd") || strings.Contains(lower, "secret") || strings.Contains(lower, "api_key") || strings.Contains(lower, "sk-") {
		return true
	}
	ipPattern := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	return ipPattern.MatchString(text)
}

func Sanitize(text string) string {
	ipPattern := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	text = ipPattern.ReplaceAllString(text, "[已脱敏IP]")
	credentialPattern := regexp.MustCompile(`(?i)\b(password|passwd|api_key|apikey|token|secret|key)\s*[:=]\s*[^\s,;，；。]+`)
	text = credentialPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := regexp.MustCompile(`[:=]`).Split(match, 2)
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			return "[已脱敏]"
		}
		return strings.TrimSpace(parts[0]) + "=[已脱敏]"
	})
	secretWordPattern := regexp.MustCompile(`(?i)\b(secret|api_key)\b`)
	text = secretWordPattern.ReplaceAllString(text, "[已脱敏]")
	keyPattern := regexp.MustCompile(`sk-[A-Za-z0-9_\-]+`)
	text = keyPattern.ReplaceAllString(text, "[已脱敏KEY]")
	return text
}

func tokenSet(text string) map[string]bool {
	tokens := wordPattern.FindAllString(strings.ToLower(text), -1)
	set := map[string]bool{}
	for _, token := range tokens {
		token = strings.TrimFunc(token, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r)
		})
		if token != "" {
			set[token] = true
		}
	}
	return set
}
