package agent

import (
	"strings"
	"testing"
)

func TestSafeReplyStreamEmitsImmediatelyWhenNoOverlap(t *testing.T) {
	emitted := []string{}
	stream := newSafeReplyStream(func(chunk string) { emitted = append(emitted, chunk) },
		[]string{"索引缺失导致慢查询", "使用 EXPLAIN 验证执行计划"})
	// 与禁词毫无重叠的正常回复必须逐片立刻放行，不能压着等。
	for _, piece := range []string{"先看看", "最近一次变更", "的时间点。"} {
		stream.accept(piece)
	}
	if len(emitted) != 3 {
		t.Fatalf("expected every chunk to pass through immediately, got %#v", emitted)
	}
	if got := strings.Join(emitted, ""); got != "先看看最近一次变更的时间点。" {
		t.Fatalf("unexpected assembled text: %q", got)
	}
}

func TestSafeReplyStreamHoldsBackOnlyThePrefixOverlap(t *testing.T) {
	emitted := []string{}
	stream := newSafeReplyStream(func(chunk string) { emitted = append(emitted, chunk) },
		[]string{"索引缺失导致慢查询"})
	// 尾部「索引缺」正好是禁词前缀，这三个字必须扣住等下一片。
	stream.accept("问题出在索引缺")
	if got := strings.Join(emitted, ""); got != "问题出在" {
		t.Fatalf("expected the forbidden-term prefix to be held back, got %q", got)
	}
	// 下一片证明不是禁词，扣住的部分应当补发。
	stream.accept("口处的监控")
	if got := strings.Join(emitted, ""); got != "问题出在索引缺口处的监控" {
		t.Fatalf("expected held-back text to be released, got %q", got)
	}
	if stream.blocked {
		t.Fatal("stream must stay open when the term never completes")
	}
}

func TestSafeReplyStreamBlocksOnCompletedForbiddenTerm(t *testing.T) {
	emitted := []string{}
	stream := newSafeReplyStream(func(chunk string) { emitted = append(emitted, chunk) },
		[]string{"索引缺失导致慢查询"})
	for _, piece := range []string{"结论是", "索引缺", "失导致慢查询"} {
		stream.accept(piece)
	}
	if !stream.blocked {
		t.Fatal("expected the gate to close once the forbidden term completed")
	}
	if got := strings.Join(emitted, ""); strings.Contains(got, "索引缺失导致慢查询") {
		t.Fatalf("forbidden term leaked through the gate: %q", got)
	}
}

func TestSafeReplyStreamIsCaseInsensitive(t *testing.T) {
	emitted := []string{}
	stream := newSafeReplyStream(func(chunk string) { emitted = append(emitted, chunk) }, []string{"Connection Pool Exhausted"})
	stream.accept("the cause is connection pool exhausted here")
	if !stream.blocked {
		t.Fatal("expected case-insensitive matching to close the gate")
	}
	if strings.Contains(strings.ToLower(strings.Join(emitted, "")), "connection pool exhausted") {
		t.Fatalf("forbidden term leaked: %q", strings.Join(emitted, ""))
	}
}
