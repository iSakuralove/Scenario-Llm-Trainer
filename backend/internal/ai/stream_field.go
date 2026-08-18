package ai

import "strings"

// JSONFieldStreamer 从增量到达的 JSON 原文里持续抽取某个字符串字段的新增内容。
//
// 结构化输出的流式响应到达时是 `{"reply":"哎呀，` 这样的 JSON 片段，
// 直接转发给前端会暴露协议细节；这里只把字段值本身按增量吐出。
type JSONFieldStreamer struct {
	field    string
	raw      strings.Builder
	emitted  int
	finished bool
}

func NewJSONFieldStreamer(field string) *JSONFieldStreamer {
	return &JSONFieldStreamer{field: field}
}

// Accept 吃进一段 JSON 原文，返回该字段这次新解出的文本（可能为空）。
func (s *JSONFieldStreamer) Accept(chunk string) string {
	if s == nil || s.finished || chunk == "" {
		return ""
	}
	s.raw.WriteString(chunk)
	value, complete := decodeJSONStringField(s.raw.String(), s.field)
	if complete {
		s.finished = true
	}
	runes := []rune(value)
	if len(runes) <= s.emitted {
		return ""
	}
	delta := string(runes[s.emitted:])
	s.emitted = len(runes)
	return delta
}

// decodeJSONStringField 解析 `"field": "..."`，返回已到达的值和它是否已闭合。
// 未闭合时同样返回当前已解出的部分，用于流式增量输出。
func decodeJSONStringField(raw, field string) (string, bool) {
	token := `"` + field + `"`
	index := strings.Index(raw, token)
	if index < 0 {
		return "", false
	}
	i := index + len(token)
	for i < len(raw) && raw[i] != ':' {
		i++
	}
	if i >= len(raw) {
		return "", false
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != '"' {
		return "", false
	}
	i++
	var value strings.Builder
	for ; i < len(raw); i++ {
		ch := raw[i]
		if ch == '\\' {
			// 转义序列可能被切在两个分片之间，等下一片到达再解析。
			if i+1 >= len(raw) {
				break
			}
			i++
			writeUnescapedJSONByte(&value, raw[i])
			continue
		}
		if ch == '"' {
			return value.String(), true
		}
		value.WriteByte(ch)
	}
	return value.String(), false
}

func writeUnescapedJSONByte(builder *strings.Builder, ch byte) {
	switch ch {
	case 'n':
		builder.WriteByte('\n')
	case 'r':
		builder.WriteByte('\r')
	case 't':
		builder.WriteByte('\t')
	default:
		builder.WriteByte(ch)
	}
}
