package agent

import (
	"strings"
	"unicode"
)

// safeReplyStream 是模型分片和学生屏幕之间的安全闸门。
//
// 排查工坊的根因、关键证据和标准步骤都是禁露内容，一旦发出去就收不回来，
// 所以分片必须先过闸门再外发：
//   - 缓冲尾部一旦是某个禁词的前缀就暂时扣住，等下一片到达再判断，
//     这样即便禁词被切在两个分片之间也不会漏出去；
//   - 一旦累积文本命中完整禁词，立即永久关闸，剩下的交给 safety_rewrite 和最终 finish 事件。
//
// 扣留长度按实际重叠计算而不是取最长禁词长度：后者会让每条回复都白白压住几十个字，
// 学生要等大半段生成完才看到第一个字，流式就失去意义了。
type safeReplyStream struct {
	emit      func(string)
	forbidden [][]rune
	buffer    []rune
	lowered   []rune
	emitted   int
	blocked   bool
}

func newSafeReplyStream(emit func(string), forbidden []string) *safeReplyStream {
	terms := make([][]rune, 0, len(forbidden))
	for _, term := range forbidden {
		if trimmed := strings.TrimSpace(term); trimmed != "" {
			terms = append(terms, loweredRunes(trimmed))
		}
	}
	return &safeReplyStream{emit: emit, forbidden: terms}
}

func loweredRunes(text string) []rune {
	runes := []rune(text)
	out := make([]rune, len(runes))
	for i, r := range runes {
		out[i] = unicode.ToLower(r)
	}
	return out
}

func (s *safeReplyStream) accept(chunk string) {
	if s == nil || s.emit == nil || s.blocked || chunk == "" {
		return
	}
	s.buffer = append(s.buffer, []rune(chunk)...)
	s.lowered = append(s.lowered, loweredRunes(chunk)...)
	if s.containsForbidden() {
		s.blocked = true
		return
	}
	safeLen := len(s.buffer) - s.pendingHoldback()
	if safeLen <= s.emitted {
		return
	}
	s.emit(string(s.buffer[s.emitted:safeLen]))
	s.emitted = safeLen
}

func (s *safeReplyStream) containsForbidden() bool {
	for _, term := range s.forbidden {
		if containsRunes(s.lowered, term) {
			return true
		}
	}
	return false
}

// pendingHoldback 返回缓冲尾部与任一禁词前缀的最长重叠长度。
// 没有重叠时返回 0，也就是模型吐多少就放行多少。
func (s *safeReplyStream) pendingHoldback() int {
	longest := 0
	for _, term := range s.forbidden {
		limit := len(term) - 1
		if limit > len(s.lowered) {
			limit = len(s.lowered)
		}
		for k := limit; k > longest; k-- {
			if runesEqual(s.lowered[len(s.lowered)-k:], term[:k]) {
				longest = k
				break
			}
		}
	}
	return longest
}

func (s *safeReplyStream) emittedText() string {
	if s == nil || s.emitted == 0 {
		return ""
	}
	return string(s.buffer[:s.emitted])
}

func (s *safeReplyStream) callback() func(string) {
	if s == nil || s.emit == nil {
		return nil
	}
	return s.accept
}

func containsRunes(haystack, needle []rune) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if runesEqual(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func runesEqual(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
