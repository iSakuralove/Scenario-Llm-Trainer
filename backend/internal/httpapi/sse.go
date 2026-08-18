package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"strings"
	"time"
)

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	return &sseWriter{w: w, flusher: flusher}
}
func (s *sseWriter) event(name string, data interface{}) {
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, mustJSON(data))
	if s.flusher != nil {
		s.flusher.Flush()
	}
}
func (s *sseWriter) stage(step, message string) {
	s.event("stage", map[string]string{"step": step, "message": message})
}

// 只丢空串，不丢纯空白：真流式下单个空格本身就是一个 token，
// 按空白过滤会把它吞掉，拼出来的文本就和最终落库内容不一致了。
func (s *sseWriter) delta(chunk string, displayable bool) {
	if chunk == "" {
		return
	}
	s.event("delta", map[string]interface{}{"chunk": chunk, "displayable": displayable})
}
func (s *sseWriter) deltaDisplay(chunk string) {
	if chunk == "" {
		return
	}
	s.event("delta", map[string]interface{}{"chunk": chunk, "displayable": true})
}
func (s *sseWriter) finish(payload interface{}) {
	s.event("finish", payload)
}
func (s *sseWriter) fail(message string) {
	s.event("error", map[string]string{"message": message})
}
func writeSSE(w http.ResponseWriter, payload map[string]interface{}, content string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunkText(content, 28) {
		fmt.Fprintf(w, "event: delta\ndata: %s\n\n", mustJSON(map[string]string{"chunk": chunk}))
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintf(w, "event: finish\ndata: %s\n\n", mustJSON(payload))
	if flusher != nil {
		flusher.Flush()
	}
}

// streamInterviewFeedbackDisplay 是流式反馈的兜底回放：只在模型没能边生成边吐字段时使用。
// skipScore 表示总分行已经由 interviewFeedbackLiveDisplay 发过，不能重复。
func streamInterviewFeedbackDisplay(writer *sseWriter, feedback ai.InterviewFeedback, evaluation domain.InterviewEvaluation, needReport, skipScore bool) {
	if writer == nil {
		return
	}
	highlights := feedback.Highlights
	if len(highlights) == 0 {
		highlights = evaluation.Highlights
	}
	deficiencies := feedback.Deficiencies
	if len(deficiencies) == 0 {
		deficiencies = evaluation.Deficiencies
	}
	sections := []struct {
		label string
		text  string
	}{
		{label: "亮点", text: strings.Join(highlights, "；")},
		{label: "待改进", text: strings.Join(deficiencies, "；")},
	}
	if !skipScore {
		sections = append([]struct {
			label string
			text  string
		}{{label: "总分", text: fmt.Sprintf("%d 分", evaluation.TotalScore)}}, sections...)
	}
	if evaluation.FollowUpTriggered {
		followUp := strings.TrimSpace(feedback.FollowUpQuestion)
		if followUp == "" {
			followUp = evaluation.FollowUpQuestion
		}
		sections = append(sections, struct {
			label string
			text  string
		}{label: "追问", text: followUp})
	}
	if needReport {
		sections = append(sections, struct {
			label string
			text  string
		}{label: "综合评价", text: feedback.FinalReport})
	}
	for _, section := range sections {
		text := strings.TrimSpace(section.text)
		if text == "" {
			continue
		}
		for _, chunk := range chunkText(section.label+"："+text+"\n", 42) {
			writer.deltaDisplay(chunk)
			time.Sleep(35 * time.Millisecond)
		}
	}
}

type interviewFeedbackLiveDisplay struct {
	writer          *sseWriter
	raw             strings.Builder
	highlightCount  int
	deficiencyCount int
	followUpSent    bool
	finalReportSent bool
	fieldContent    bool
	needReport      bool
}

func newInterviewFeedbackLiveDisplay(writer *sseWriter, totalScore int, needReport bool) *interviewFeedbackLiveDisplay {
	display := &interviewFeedbackLiveDisplay{writer: writer, needReport: needReport}
	if writer != nil {
		writer.deltaDisplay(fmt.Sprintf("总分：%d 分\n", totalScore))
	}
	return display
}
func (d *interviewFeedbackLiveDisplay) accept(chunk string) {
	if d == nil || d.writer == nil || chunk == "" {
		return
	}
	d.raw.WriteString(chunk)
	snapshot := parseInterviewFeedbackStreamSnapshot(d.raw.String(), d.needReport)
	d.emitNewListItems("亮点", snapshot.Highlights, &d.highlightCount)
	d.emitNewListItems("待改进", snapshot.Deficiencies, &d.deficiencyCount)
	if !d.followUpSent && strings.TrimSpace(snapshot.FollowUpQuestion) != "" {
		d.emitLine("追问", snapshot.FollowUpQuestion)
		d.followUpSent = true
	}
	if d.needReport && !d.finalReportSent && strings.TrimSpace(snapshot.FinalReport) != "" {
		d.emitLine("综合评价", snapshot.FinalReport)
		d.finalReportSent = true
	}
}
func (d *interviewFeedbackLiveDisplay) hasFieldContent() bool {
	return d != nil && d.fieldContent
}
func (d *interviewFeedbackLiveDisplay) emitNewListItems(label string, items []string, sent *int) {
	for *sent < len(items) {
		d.emitLine(label, items[*sent])
		(*sent)++
	}
}
func (d *interviewFeedbackLiveDisplay) emitLine(label, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	d.fieldContent = true
	for _, piece := range chunkText(label+"："+text+"\n", 36) {
		d.writer.deltaDisplay(piece)
		time.Sleep(25 * time.Millisecond)
	}
}

type interviewFeedbackStreamSnapshot struct {
	Highlights       []string
	Deficiencies     []string
	FollowUpQuestion string
	FinalReport      string
}

func parseInterviewFeedbackStreamSnapshot(raw string, needReport bool) interviewFeedbackStreamSnapshot {
	snapshot := interviewFeedbackStreamSnapshot{
		Highlights:       extractJSONArrayStrings(raw, "highlights"),
		Deficiencies:     extractJSONArrayStrings(raw, "deficiencies"),
		FollowUpQuestion: extractJSONStringValue(raw, "follow_up_question"),
	}
	if needReport {
		snapshot.FinalReport = extractJSONStringValue(raw, "final_report")
	}
	return snapshot
}
func extractJSONArrayStrings(raw, key string) []string {
	start := jsonValueStart(raw, key, '[')
	if start < 0 {
		return nil
	}
	items := []string{}
	inString := false
	escaped := false
	var item strings.Builder
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if !inString {
			if ch == ']' {
				break
			}
			if ch == '"' {
				inString = true
				escaped = false
				item.Reset()
			}
			continue
		}
		if escaped {
			appendEscapedJSONByte(&item, ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = false
			if text := strings.TrimSpace(item.String()); text != "" {
				items = append(items, text)
			}
			continue
		}
		item.WriteByte(ch)
	}
	return items
}
func extractJSONStringValue(raw, key string) string {
	start := jsonValueStart(raw, key, '"')
	if start < 0 {
		return ""
	}
	escaped := false
	var value strings.Builder
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			appendEscapedJSONByte(&value, ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return value.String()
		}
		value.WriteByte(ch)
	}
	return ""
}
func jsonValueStart(raw, key string, expected byte) int {
	keyToken := `"` + key + `"`
	keyIndex := strings.Index(raw, keyToken)
	if keyIndex < 0 {
		return -1
	}
	i := keyIndex + len(keyToken)
	for i < len(raw) && raw[i] != ':' {
		i++
	}
	if i >= len(raw) {
		return -1
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != expected {
		return -1
	}
	return i + 1
}
func appendEscapedJSONByte(builder *strings.Builder, ch byte) {
	switch ch {
	case '"', '\\', '/':
		builder.WriteByte(ch)
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
func mustJSON(value interface{}) string {
	data, _ := json.Marshal(value)
	return string(data)
}
func chunkText(text string, size int) []string {
	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}
	chunks := []string{}
	for len(runes) > 0 {
		n := size
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
