package ai

import (
	"strings"
	"testing"
)

func TestJSONFieldStreamerEmitsIncrementalText(t *testing.T) {
	streamer := NewJSONFieldStreamer("reply")
	// 模拟模型按 token 吐出的 JSON 片段
	pieces := []string{`{"re`, `ply":"先看`, `慢查询日`, `志"}`}
	got := []string{}
	for _, piece := range pieces {
		if text := streamer.Accept(piece); text != "" {
			got = append(got, text)
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected incremental output, got %#v", got)
	}
	if joined := strings.Join(got, ""); joined != "先看慢查询日志" {
		t.Fatalf("unexpected assembled text: %q", joined)
	}
}

func TestJSONFieldStreamerHandlesEscapesAndSplitEscapes(t *testing.T) {
	streamer := NewJSONFieldStreamer("reply")
	var builder strings.Builder
	for _, piece := range []string{`{"reply":"第一行\`, `n第二行 \"引用\" 结束"}`} {
		builder.WriteString(streamer.Accept(piece))
	}
	if got := builder.String(); got != "第一行\n第二行 \"引用\" 结束" {
		t.Fatalf("unexpected decoded text: %q", got)
	}
}

func TestJSONFieldStreamerIgnoresUnrelatedFields(t *testing.T) {
	streamer := NewJSONFieldStreamer("reply")
	if text := streamer.Accept(`{"other":"无关内容"`); text != "" {
		t.Fatalf("expected no output before the target field, got %q", text)
	}
	if text := streamer.Accept(`,"reply":"正文"}`); text != "正文" {
		t.Fatalf("expected the target field only, got %q", text)
	}
}

func TestJSONFieldStreamerStopsAfterFieldCloses(t *testing.T) {
	streamer := NewJSONFieldStreamer("reply")
	if text := streamer.Accept(`{"reply":"完整"}`); text != "完整" {
		t.Fatalf("unexpected text: %q", text)
	}
	if text := streamer.Accept(`{"reply":"第二段"}`); text != "" {
		t.Fatalf("streamer must stop after the field closed, got %q", text)
	}
}
