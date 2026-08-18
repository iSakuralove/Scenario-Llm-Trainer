package httpapi

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestInterviewLaunchpadShortTopicPrefersTitleKeyword(t *testing.T) {
	got := interviewLaunchpadShortTopic("JVM运行时数据区：核心原理与适用场景", "请解释JVM运行时数据区的核心机制，并说明它解决的问题与适用边界。")
	if got != "JVM运行时数据区" {
		t.Fatalf("unexpected short topic: %q", got)
	}
}

func TestInterviewLaunchpadShortTopicFromSubjectTemplate(t *testing.T) {
	got := interviewLaunchpadShortTopic("", "请解释Redis数据结构的核心机制，并说明它解决的问题与适用边界。")
	if got != "Redis数据结构" {
		t.Fatalf("unexpected short topic: %q", got)
	}
}

func TestInterviewLaunchpadTrackSummaryKeepsFocus(t *testing.T) {
	got := interviewLaunchpadTrackSummary([]string{"模型选型", "AI需求判断", "AI灰度发布"}, 28)
	if got != "模型选型 · AI需求判断 · AI灰度发布 等 28 题" {
		t.Fatalf("unexpected summary: %q", got)
	}
	got = interviewLaunchpadTrackSummary([]string{"缓存穿透"}, 1)
	if got != "缓存穿透" {
		t.Fatalf("unexpected single summary: %q", got)
	}
}

func TestSortInterviewLaunchpadTracksPrioritizesResumeBeforeTargetRole(t *testing.T) {
	tracks := []interviewLaunchpadTrack{
		{ID: "role", Domain: "java", DomainLabel: "Java", Tags: []string{"spring", "后端工程师"}},
		{ID: "resume", Domain: "cache", DomainLabel: "缓存", Tags: []string{"redis"}},
	}
	user := &domain.User{Profile: domain.UserProfile{
		TargetRole: "Java Spring 后端工程师",
		ResumeDocuments: []domain.ResumeDocument{{
			ID: "resume-1", ExtractedText: "数据库工程师，熟悉 Redis 缓存和 MySQL。负责电商缓存系统项目，设计热点数据治理与故障恢复流程，性能优化后延迟降低百分之三十并完成上线复盘。", QualityStatus: "passed",
		}},
	}}

	sortInterviewLaunchpadTracks(user, tracks)

	if tracks[0].ID != "resume" {
		t.Fatalf("expected resume-matched track first, got %q", tracks[0].ID)
	}
}

func TestSortInterviewLaunchpadTracksFallsBackToTargetRole(t *testing.T) {
	tracks := []interviewLaunchpadTrack{
		{ID: "resume", Domain: "cache", DomainLabel: "缓存", Tags: []string{"redis"}},
		{ID: "role", Domain: "java", DomainLabel: "Java", Tags: []string{"spring"}},
	}
	user := &domain.User{Profile: domain.UserProfile{TargetRole: "Java Spring 工程师"}}

	sortInterviewLaunchpadTracks(user, tracks)

	if tracks[0].ID != "role" {
		t.Fatalf("expected target-role track first without resume, got %q", tracks[0].ID)
	}
}
