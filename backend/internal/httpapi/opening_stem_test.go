package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/store"
)

func TestEnsureSinglePointOpeningDescriptionKeepsGoodStem(t *testing.T) {
	in := "线上接口突然变慢，你怀疑是 MySQL 查询问题。你第一步会先看什么？"
	got := ensureSinglePointOpeningDescription(in, "慢查询")
	if got != in {
		t.Fatalf("expected good stem unchanged, got %q", got)
	}
}

func TestEnsureSinglePointOpeningDescriptionRewritesMultiPoint(t *testing.T) {
	in := "一次生产发布导致服务健康检查持续失败，流水线卡在发布阶段。请给出排查顺序、回滚策略和流水线修复方案。"
	got := ensureSinglePointOpeningDescription(in, "发布回滚")
	if strings.Contains(got, "回滚策略") || strings.Contains(got, "流水线修复") {
		t.Fatalf("multi-point stem should be rewritten, got %q", got)
	}
	if !strings.Contains(got, "你第一步会先看什么") {
		t.Fatalf("expected single-point ask, got %q", got)
	}
	if !strings.Contains(got, "健康检查") {
		t.Fatalf("expected scenario retained, got %q", got)
	}
}

func TestEnsureSinglePointOpeningDescriptionEmptyUsesDefault(t *testing.T) {
	got := ensureSinglePointOpeningDescription("", "缓存击穿")
	if !strings.Contains(got, "缓存击穿") || !strings.Contains(got, "第一步") {
		t.Fatalf("expected default single-point opening, got %q", got)
	}
}

func TestLooksLikeMultiPointOpening(t *testing.T) {
	if !looksLikeMultiPointOpening("请说明你的定位路径、关键命令、可能修复方案和回滚考虑。") {
		t.Fatal("expected multi-point detection")
	}
	if looksLikeMultiPointOpening("你第一步会先看什么？") {
		t.Fatal("single-point should not be flagged")
	}
}

func TestResolveOpeningDescriptionUsesLLMForMultiPoint(t *testing.T) {
	server := NewServerForTests(store.NewMemoryStore(auth.HashPassword), auth.NewManager("test-secret", time.Hour))
	source := "一次生产发布导致服务健康检查持续失败。请给出排查顺序、回滚策略和流水线修复方案。"
	got := server.resolveOpeningDescription("interview-devops-release-rollback", source, "发布回滚", "devops")
	if strings.Contains(got, "回滚策略") && strings.Contains(got, "流水线修复") {
		t.Fatalf("expected multi-point rewritten, got %q", got)
	}
	if !strings.Contains(got, "？") && !strings.Contains(got, "?") {
		t.Fatalf("expected a question-like opening, got %q", got)
	}
}

func TestResolveOpeningDescriptionKeepsGoodStemWithoutLLMNoise(t *testing.T) {
	server := NewServerForTests(store.NewMemoryStore(auth.HashPassword), auth.NewManager("test-secret", time.Hour))
	in := "线上接口突然变慢。你第一步会先看什么？"
	got := server.resolveOpeningDescription("interview-db-slow-query", in, "慢查询", "database")
	if got != in {
		t.Fatalf("good stem must stay unchanged, got %q", got)
	}
}

func TestResolveOpeningDescriptionCachesRewrittenStem(t *testing.T) {
	server := NewServerForTests(store.NewMemoryStore(auth.HashPassword), auth.NewManager("test-secret", time.Hour))
	source := "发布后探针持续失败。请说明排查顺序、回滚策略和修复方案。"
	first := server.resolveOpeningDescription("atom-cache-key", source, "发布回滚", "devops")
	second := server.resolveOpeningDescription("atom-cache-key", source, "发布回滚", "devops")
	if first != second {
		t.Fatalf("the same question must keep a stable stem across sessions: %q vs %q", first, second)
	}
	if cached, ok := server.cachedOpeningDescription("atom-cache-key"); !ok || cached != first {
		t.Fatalf("expected the rewritten stem to be cached, got %q ok=%t", cached, ok)
	}
}

func TestLooksLikeMultiPointOpeningUsesStructuralHeuristics(t *testing.T) {
	multi := []string{
		"请说明你的定位路径、关键命令和回滚考虑。",
		"某服务超时，请给出排查顺序和修复方案。",
		"请分析可能原因、验证命令、处理策略。",
		"请说明它的核心机制、适用边界以及风险控制。",
	}
	for _, text := range multi {
		if !looksLikeMultiPointOpening(text) {
			t.Fatalf("expected multi-point detection for %q", text)
		}
	}
	single := []string{
		"你第一步会先看什么？",
		"线上接口突然变慢，你怀疑是 MySQL 查询问题。你第一步会先看什么？",
		"请说明你最关键的那个判断依据。",
		"你会先止损哪一侧？",
	}
	for _, text := range single {
		if looksLikeMultiPointOpening(text) {
			t.Fatalf("single-point stem should not be flagged: %q", text)
		}
	}
}

func TestMockRewriteInterviewOpening(t *testing.T) {
	out, err := ai.MockProvider{}.RewriteInterviewOpening(context.Background(), ai.InterviewOpeningRequest{
		Subject:    "健康检查失败",
		Domain:     "devops",
		SourceText: "发布后探针失败，请给出排查顺序、回滚策略和流水线修复方案。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Opening, "回滚策略") && strings.Contains(out.Opening, "流水线") {
		t.Fatalf("mock should not keep multi-point list: %q", out.Opening)
	}
}
