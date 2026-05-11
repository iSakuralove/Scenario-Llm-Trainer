package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

type CommunityReviewConfig struct {
	NewRunID func() string
	Now      func() time.Time
}

type CommunityReviewRequest struct {
	Post         *domain.CommunityPost
	Stage        string
	ReviewerRole string
	OnStage      func(step, message string)
}

type CommunityReviewResult struct {
	Summary *domain.ModerationSummary
	Trace   domain.AgentTrace
}

type CommunityReviewAgent struct {
	config CommunityReviewConfig
}

func NewCommunityReviewAgent(config CommunityReviewConfig) *CommunityReviewAgent {
	return &CommunityReviewAgent{config: config}
}

func (a *CommunityReviewAgent) Run(ctx context.Context, req CommunityReviewRequest) (CommunityReviewResult, error) {
	if req.Post == nil {
		return CommunityReviewResult{}, fmt.Errorf("post is required")
	}
	content := effectiveReviewContent(req.Post)
	runtime := NewRuntime(RuntimeConfig{
		Agent:          "cm_review_agent",
		Mode:           "server_react",
		ForbiddenTerms: communityReviewForbiddenTerms(req.Post),
		NewRunID:       a.config.NewRunID,
		Now:            a.config.Now,
	})
	state := communityReviewState{
		post:         req.Post,
		stage:        strings.TrimSpace(req.Stage),
		reviewerRole: strings.TrimSpace(req.ReviewerRole),
		content:      content,
		summary: &domain.ModerationSummary{
			Status:     "reviewed",
			SafeLabels: []string{},
			Reasons:    []string{},
		},
	}
	trace, err := runtime.Execute(ctx, []Step{
		a.evaluateRiskStep(req, &state),
		a.buildRecommendationStep(req, &state),
		a.composeSafeSummaryStep(req, &state),
	})
	if err != nil {
		return CommunityReviewResult{Summary: state.summary, Trace: trace}, err
	}
	state.summary.AgentTrace = &trace
	state.summary.UpdatedAt = communityReviewNow(a.config.Now)
	return CommunityReviewResult{
		Summary: state.summary,
		Trace:   trace,
	}, nil
}

type communityReviewState struct {
	post         *domain.CommunityPost
	stage        string
	reviewerRole string
	content      domain.ScenarioContent
	summary      *domain.ModerationSummary
}

func (a *CommunityReviewAgent) evaluateRiskStep(req CommunityReviewRequest, state *communityReviewState) Step {
	return Step{
		Name: "evaluate_review_risk",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_policy", "正在检查审核风险")
			risk := firstNonEmpty(strings.TrimSpace(state.post.SensitiveCheck.RiskLevel), "low")
			flagged := state.post.SensitiveCheck.Blocked || risk == "high" || len(state.post.SensitiveCheck.Findings) > 0
			if state.summary == nil {
				state.summary = &domain.ModerationSummary{}
			}
			state.summary.RiskLevel = risk
			state.summary.Flagged = flagged
			state.summary.SafeLabels = reviewLabels(state.post, risk, flagged)
			state.summary.Reasons = reviewReasons(state.post, state.content, risk, flagged)
			return ToolResult{
				Summary: "已完成审核风险评估",
				Metadata: map[string]string{
					"risk_level": risk,
					"flagged":    fmt.Sprintf("%t", flagged),
				},
			}, nil
		},
	}
}

func (a *CommunityReviewAgent) buildRecommendationStep(req CommunityReviewRequest, state *communityReviewState) Step {
	return Step{
		Name: "build_review_recommendation",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_eval", "正在生成审核建议")
			if state.summary == nil {
				state.summary = &domain.ModerationSummary{}
			}
			switch {
			case state.summary.Flagged:
				state.summary.Recommendation = "建议人工重点复核"
				state.summary.SuggestedNote = "建议先核对敏感信息处理与证据链完整度，再决定是否通过。"
			case len(state.content.KeyEvidence) == 0 || len(state.content.StandardProcedure) == 0:
				state.summary.Recommendation = "建议退回补充"
				state.summary.SuggestedNote = "建议补充关键证据、处置步骤和回滚说明后再提交审核。"
			default:
				state.summary.Recommendation = "建议通过"
				state.summary.SuggestedNote = "结构完整，可进入下一审核环节。"
			}
			return ToolResult{
				Summary: "已确定审核建议动作",
				Metadata: map[string]string{
					"recommendation": state.summary.Recommendation,
				},
			}, nil
		},
	}
}

func (a *CommunityReviewAgent) composeSafeSummaryStep(req CommunityReviewRequest, state *communityReviewState) Step {
	return Step{
		Name: "compose_safe_review_summary",
		Kind: "tool",
		Run: func(context.Context, *StepRecorder) (ToolResult, error) {
			emitStage(req.OnStage, "agent_reply", "正在整理安全审核摘要")
			if state.summary == nil {
				state.summary = &domain.ModerationSummary{}
			}
			state.summary.SafeSummary = safeTraceText(reviewSummary(state.content, state.summary.Recommendation))
			state.summary.SafeRiskNote = safeTraceText(reviewRiskNote(state.post, state.summary.RiskLevel))
			state.summary.SafeActionHint = safeTraceText(reviewActionHint(state.summary.Recommendation, state.stage))
			state.summary.Status = "reviewed"
			for i := range state.summary.Reasons {
				state.summary.Reasons[i] = safeTraceText(state.summary.Reasons[i])
			}
			for i := range state.summary.SafeLabels {
				state.summary.SafeLabels[i] = safeTraceText(state.summary.SafeLabels[i])
			}
			state.summary.SuggestedNote = safeTraceText(state.summary.SuggestedNote)
			return ToolResult{
				Summary: "已输出安全审核摘要",
				Metadata: map[string]string{
					"reason_count": fmt.Sprintf("%d", len(state.summary.Reasons)),
				},
			}, nil
		},
	}
}

func effectiveReviewContent(post *domain.CommunityPost) domain.ScenarioContent {
	if post == nil {
		return domain.ScenarioContent{}
	}
	if post.EditedStructuredContent != nil {
		return *post.EditedStructuredContent
	}
	return post.AIStructuredContent
}

func communityReviewForbiddenTerms(post *domain.CommunityPost) []string {
	if post == nil {
		return nil
	}
	content := effectiveReviewContent(post)
	terms := []string{
		post.RawContent,
		content.RootCause,
		post.Title,
		"prompt",
		"tool_args",
	}
	terms = append(terms, content.StandardProcedure...)
	terms = append(terms, content.KeyEvidence...)
	for _, finding := range post.SensitiveCheck.Findings {
		terms = append(terms, finding.Excerpt, finding.RedactedExcerpt)
	}
	return normalizeForbiddenTerms(terms)
}

func reviewLabels(post *domain.CommunityPost, risk string, flagged bool) []string {
	labels := []string{}
	if post != nil && strings.TrimSpace(post.Status) != "" {
		labels = append(labels, "当前状态:"+post.Status)
	}
	if risk != "" {
		labels = append(labels, "风险:"+risk)
	}
	if flagged {
		labels = append(labels, "需重点复核")
	} else {
		labels = append(labels, "结构可复核")
	}
	return labels
}

func reviewReasons(post *domain.CommunityPost, content domain.ScenarioContent, risk string, flagged bool) []string {
	reasons := []string{}
	if len(content.KeyEvidence) > 0 {
		reasons = append(reasons, fmt.Sprintf("已整理 %d 条关键证据，便于快速核对问题背景。", len(content.KeyEvidence)))
	} else {
		reasons = append(reasons, "关键证据仍不完整，建议先补齐再进入下一审核环节。")
	}
	if len(content.StandardProcedure) > 0 {
		reasons = append(reasons, fmt.Sprintf("已包含 %d 条处置步骤，可支持审核者判断可执行性。", len(content.StandardProcedure)))
	} else {
		reasons = append(reasons, "处置步骤不足，当前不适合直接发布。")
	}
	if post != nil && len(post.SensitiveCheck.Findings) > 0 {
		reasons = append(reasons, fmt.Sprintf("敏感检测发现 %d 处风险，需要人工确认脱敏结果。", len(post.SensitiveCheck.Findings)))
	} else if flagged || risk == "high" {
		reasons = append(reasons, "当前风险等级偏高，建议优先核对敏感字段和审核说明。")
	} else {
		reasons = append(reasons, "敏感检测未发现明显阻断项，可聚焦结构质量和教学价值。")
	}
	return reasons
}

func reviewSummary(content domain.ScenarioContent, recommendation string) string {
	switch recommendation {
	case "建议退回补充":
		return "案例结构尚未稳定，建议补全关键证据与处置步骤后再继续审核。"
	case "建议人工重点复核":
		return "案例已形成基础结构，但存在需要人工确认的风险项，建议重点复核后再决定去向。"
	default:
		return "案例结构较完整，证据与步骤信息可支撑进入下一审核阶段。"
	}
}

func reviewRiskNote(post *domain.CommunityPost, risk string) string {
	if post != nil && len(post.SensitiveCheck.Findings) > 0 {
		return fmt.Sprintf("当前敏感风险等级为 %s，已建议审核者优先核对脱敏与合规性。", firstNonEmpty(risk, "medium"))
	}
	return fmt.Sprintf("当前敏感风险等级为 %s，可继续结合结构完整度进行审核。", firstNonEmpty(risk, "low"))
}

func reviewActionHint(recommendation, stage string) string {
	stage = firstNonEmpty(stage, "review")
	switch recommendation {
	case "建议退回补充":
		return fmt.Sprintf("建议在 %s 阶段退回并补充证据链、处置步骤与审核说明。", stage)
	case "建议人工重点复核":
		return fmt.Sprintf("建议在 %s 阶段保留人工判断，由审核者确认风险项是否可接受。", stage)
	default:
		return fmt.Sprintf("建议在 %s 阶段继续流转，并保留审核备注作为交接说明。", stage)
	}
}

func communityReviewNow(now func() time.Time) time.Time {
	if now != nil {
		return now()
	}
	return time.Now()
}
