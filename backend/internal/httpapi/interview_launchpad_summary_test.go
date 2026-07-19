package httpapi

import (
	"testing"

	"situational-teaching/backend/internal/domain"
)

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
