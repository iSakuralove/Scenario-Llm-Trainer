package agent

import (
	"context"
	"strings"
	"testing"

	"situational-teaching/backend/internal/domain"
)

func TestCommunityReviewAgentBuildsSafeSummary(t *testing.T) {
	post := sampleCommunityPost()
	agent := NewCommunityReviewAgent(CommunityReviewConfig{})

	result, err := agent.Run(context.Background(), CommunityReviewRequest{
		Post:        post,
		Stage:       "instructor_review",
		ReviewerRole: domain.RoleInstructor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary == nil {
		t.Fatal("expected moderation summary")
	}
	if result.Summary.Recommendation == "" || result.Summary.SafeSummary == "" {
		t.Fatalf("expected safe recommendation and summary: %+v", result.Summary)
	}
	if len(result.Summary.Reasons) == 0 {
		t.Fatalf("expected structured reasons: %+v", result.Summary)
	}
	if result.Trace.Agent != "cm_review_agent" || result.Trace.ToolCount == 0 {
		t.Fatalf("expected cm_review_agent trace, got %+v", result.Trace)
	}
}

func TestCommunityReviewAgentRedactsSensitiveTerms(t *testing.T) {
	post := sampleCommunityPost()
	post.RawContent = "原文包含 password=secret 和 token=internal-secret。"
	post.SensitiveCheck.Findings = []domain.SensitiveFinding{
		{
			Type:     "password",
			Field:    "raw_content",
			Excerpt:  "password=secret",
			Severity: "high",
		},
	}
	agent := NewCommunityReviewAgent(CommunityReviewConfig{})

	result, err := agent.Run(context.Background(), CommunityReviewRequest{
		Post:        post,
		Stage:       "final_review",
		ReviewerRole: domain.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	serialized := result.Summary.SafeSummary + " " + result.Summary.SafeRiskNote + " " + result.Summary.SafeActionHint + " " + strings.Join(result.Summary.SafeLabels, " ")
	for _, forbidden := range []string{"password=secret", "internal-secret", "token=", "password="} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(forbidden)) {
			t.Fatalf("summary leaked forbidden text %q in %q", forbidden, serialized)
		}
	}
}

func TestCommunityReviewAgentFlagsHighRiskReview(t *testing.T) {
	post := sampleCommunityPost()
	post.SensitiveCheck.Status = "risk"
	post.SensitiveCheck.RiskLevel = "high"
	post.SensitiveCheck.Blocked = true
	post.SensitiveCheck.Findings = []domain.SensitiveFinding{
		{Type: "credential", Field: "raw_content", Severity: "high", Excerpt: "token=secret"},
	}
	agent := NewCommunityReviewAgent(CommunityReviewConfig{})

	result, err := agent.Run(context.Background(), CommunityReviewRequest{
		Post:        post,
		Stage:       "instructor_review",
		ReviewerRole: domain.RoleInstructor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Summary.Flagged {
		t.Fatalf("expected flagged high-risk moderation summary: %+v", result.Summary)
	}
	if result.Summary.RiskLevel != "high" {
		t.Fatalf("expected high risk level, got %+v", result.Summary)
	}
}

func sampleCommunityPost() *domain.CommunityPost {
	return &domain.CommunityPost{
		ID:         "community-post-1",
		UserID:     "user-demo",
		Title:      "缓存规则变更导致回源升高",
		RawContent: "发布后缓存 key 规则发生变化，命中率下降，数据库读请求升高。",
		Domain:     "database",
		Tags:       []string{"缓存", "变更"},
		AIStructuredContent: domain.ScenarioContent{
			RootCause:         "缓存 key 规则不兼容导致热点回源",
			KeyEvidence:       []string{"命中率下降发生在发布后", "数据库读流量显著升高"},
			StandardProcedure: []string{"确认发布时间", "回滚缓存 key 规则", "观察命中率恢复"},
			RevealStrategy: domain.RevealStrategy{
				SurfaceClues: []domain.Clue{},
				DeepClues:    []domain.Clue{},
				Distractors:  []domain.Clue{},
			},
		},
		SensitiveCheck: domain.SensitiveCheckResult{
			Status:    "clear",
			RiskLevel: "low",
			Findings:  []domain.SensitiveFinding{},
		},
		Status: "pending_review",
	}
}
