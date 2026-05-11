package ai

import (
	"regexp"
	"strings"
)

var (
	fieldIPPattern       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	fieldEmailPattern    = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`)
	fieldSecretKeyPattern = regexp.MustCompile(`sk-[A-Za-z0-9_\-]+`)
	fieldCredentialPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(password|passwd|api[_ -]?key|apikey|access[_ -]?key|secret[_ -]?key|token|secret|key)\s*[:=：]\s*[^\s,，。]+`),
		regexp.MustCompile(`(?i)(密码|密钥|口令|令牌|凭证|API\s*KEY|Token)\s*(为|是|[:=：])\s*[^\s,，。]+`),
		regexp.MustCompile(`(?i)(?:密码|口令|密钥|令牌|凭证|API\s*KEY|Token|key|secret)[^，。,；;]*?(?:设置成了|设置为|改成了|改为了|设为|是|为)\s*[^\s,，。；;]+`),
		regexp.MustCompile(`(?i)(?:设置成了|设置为|改成了|改为了|设为)\s*[^\s,，。；;]*(?:@|[_-])[^\s,，。；;]*`),
		regexp.MustCompile(`(?i)(API\s*KEY|api[_ -]?key|apikey|token|secret|key)\s*[-=]\s*(?:\[已脱敏\]|\[[^\]]*脱敏[^\]]*\])?[^\s,，。]+`),
		regexp.MustCompile(`(?i)(password|passwd)\s*[-=]\s*(?:\[已脱敏\]|\[[^\]]*脱敏[^\]]*\])?[^\s,，。]+`),
	}
	fieldLooseSecretPattern = regexp.MustCompile(`(?i)\b(?:sk|sl|ak|tk)-[A-Za-z0-9_\-;']{6,}\b`)
	fieldOrgPattern = regexp.MustCompile(`[\p{Han}A-Za-z0-9+._-]{2,}(?:公司|教育|学校|机构|集团|银行|医院)`)
)

// SanitizeFields performs field-level redaction for learner-facing text.
func SanitizeFields(text string) string {
	text = Sanitize(text)
	text = fieldIPPattern.ReplaceAllString(text, "【IP地址】")
	text = fieldEmailPattern.ReplaceAllString(text, "【邮箱】")
	text = fieldSecretKeyPattern.ReplaceAllString(text, "【密钥】")
	text = fieldLooseSecretPattern.ReplaceAllString(text, "【密钥】")
	for _, pattern := range fieldCredentialPatterns {
		text = maskCredentialField(text, pattern)
	}
	text = fieldOrgPattern.ReplaceAllString(text, "【机构A】")
	return text
}

func maskCredentialField(text string, pattern *regexp.Regexp) string {
	separator := regexp.MustCompile(`设置成了|设置为|改成了|改为了|设为|[:=：]|为|是|-`)
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		locations := separator.FindAllStringIndex(match, -1)
		if len(locations) == 0 {
			return "【密钥】"
		}
		loc := locations[len(locations)-1]
		prefix := strings.TrimSpace(match[:loc[1]])
		return prefix + "【密钥】"
	})
}
