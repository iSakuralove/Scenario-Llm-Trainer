package httpapi

import (
	"fmt"
	"time"

	"situational-teaching/backend/internal/domain"
)

type mentorRisk struct {
	Level   string `json:"level"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type mentorAction struct {
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	ActionLabel string `json:"action_label"`
	ActionPath  string `json:"action_path"`
}

type mentorCoverage struct {
	CoveragePercent   int      `json:"coverage_percent"`
	CompletedSessions int      `json:"completed_sessions"`
	SubjectCount      int      `json:"subject_count"`
	TopSubjects       []string `json:"top_subjects"`
	UncoveredTracks   []string `json:"uncovered_tracks"`
}

func (s *Server) mentorSnapshot(user *domain.User) map[string]interface{} {
	plan := s.learningPlan(user)
	launchpad := s.interviewLaunchpad(user)
	overview := plan.Summary
	strengths := []string{}
	weaknesses := []string{}
	for i, item := range sortedInsightsDesc(plan.DomainInsights) {
		if i == 2 {
			break
		}
		strengths = append(strengths, fmt.Sprintf("%s 当前表现较稳，画像分 %d。", displayDomain(item.Domain), item.Score))
	}
	for i, item := range plan.DomainInsights {
		if i == 2 {
			break
		}
		weaknesses = append(weaknesses, fmt.Sprintf("%s 仍需补强，画像分 %d。", displayDomain(item.Domain), item.Score))
	}
	actions := []mentorAction{}
	for _, item := range plan.Recommendations {
		actions = append(actions, mentorAction{
			Title:       item.Title,
			Detail:      item.Reason,
			ActionLabel: firstNonEmpty(item.ActionLabel, "进入训练"),
			ActionPath:  firstNonEmpty(item.ActionPath, "/dashboard"),
		})
		if len(actions) == 3 {
			break
		}
	}
	coverageStats := s.mentorCoverage(user, launchpad)
	return map[string]interface{}{
		"generated_at": time.Now(),
		"overview":     overview,
		"strengths":    strengths,
		"weaknesses":   weaknesses,
		"risks":        mentorRisksFromLaunchpadAndProfile(user, launchpad),
		"actions":      actions,
		"coverage":     coverageStats,
		"profile": map[string]interface{}{
			"target_level":        user.Profile.TargetLevel,
			"target_role":         user.Profile.TargetRole,
			"preferred_domains":   append([]string{}, user.Profile.PreferredDomains...),
			"has_resume_summary":  user.Profile.ResumeSummary != "",
			"has_project_summary": user.Profile.ProjectSummary != "",
		},
		"sample_ready": coverageStats.CompletedSessions > 0,
	}
}

func mentorCoverageFromLaunchpad(snapshot map[string]interface{}) mentorCoverage {
	coverageMap, _ := snapshot["coverage_stats"].(map[string]interface{})
	return mentorCoverage{
		CoveragePercent:   intValue(coverageMap["coverage_percent"]),
		CompletedSessions: intValue(coverageMap["completed_sessions"]),
		SubjectCount:      intValue(coverageMap["subject_count"]),
		TopSubjects:       stringSliceValue(coverageMap["top_subjects"]),
		UncoveredTracks:   mentorUncoveredTrackLabels(snapshot, stringSliceValue(coverageMap["uncovered_track_ids"])),
	}
}

func (s *Server) mentorCoverage(user *domain.User, snapshot map[string]interface{}) mentorCoverage {
	coverage := mentorCoverageFromLaunchpad(snapshot)
	if user == nil {
		return coverage
	}
	subjectCounts := map[string]int{}
	sessions := s.store.ListInterviewSessionsForUser(user.ID)
	completed := 0
	for _, session := range sessions {
		if session.Status != "final_evaluated" {
			continue
		}
		completed++
		reportSummary := buildInterviewReportRetrievalSummary(&session)
		if len(reportSummary.Coverage) > 0 {
			for _, item := range reportSummary.Coverage {
				if item.Subject == "" {
					continue
				}
				increment := item.RoundCount
				if increment <= 0 {
					increment = 1
				}
				subjectCounts[item.Subject] += increment
			}
			continue
		}
		for _, subject := range reportSummary.Subjects {
			if subject != "" {
				subjectCounts[subject]++
			}
		}
	}
	coverage.CompletedSessions = completed
	coverage.SubjectCount = len(subjectCounts)
	if len(subjectCounts) == 0 {
		return coverage
	}
	type subjectStat struct {
		Subject string
		Count   int
	}
	items := make([]subjectStat, 0, len(subjectCounts))
	for subject, count := range subjectCounts {
		items = append(items, subjectStat{Subject: subject, Count: count})
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].Count > items[i].Count || (items[j].Count == items[i].Count && items[j].Subject < items[i].Subject) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	coverage.TopSubjects = coverage.TopSubjects[:0]
	for _, item := range items {
		coverage.TopSubjects = append(coverage.TopSubjects, item.Subject)
		if len(coverage.TopSubjects) == 5 {
			break
		}
	}
	return coverage
}

func mentorRisksFromLaunchpadAndProfile(user *domain.User, snapshot map[string]interface{}) []mentorRisk {
	items := []mentorRisk{}
	summaryMap, _ := snapshot["summary"].(map[string]interface{})
	state := stringValue(summaryMap["state"])
	if state == "compatibility_fallback" {
		items = append(items, mentorRisk{Level: "warning", Title: "兼容模式", Message: "当前启动台处于兼容轨道模式，正式题库聚合尚未完全接管。"})
	} else if state == "retrieval_degraded" || state == "retrieval_partial" {
		items = append(items, mentorRisk{Level: "warning", Title: "追问增强降级", Message: "部分训练入口仍在追问增强降级状态，建议优先选择已索引方向。"})
	}
	coverage := mentorCoverageFromLaunchpad(snapshot)
	if coverage.CoveragePercent < 50 {
		items = append(items, mentorRisk{Level: "danger", Title: "覆盖率偏低", Message: fmt.Sprintf("当前开放轨道覆盖率仅 %d%% ，建议优先补齐待补方向。", coverage.CoveragePercent)})
	}
	if user.Profile.ResumeSummary == "" && user.Profile.ProjectSummary == "" {
		items = append(items, mentorRisk{Level: "info", Title: "档案信息不足", Message: "补充简历摘要或项目摘要后，Mentor 建议和面试追问会更贴近你的真实背景。"})
	}
	return items
}

func mentorUncoveredTrackLabels(snapshot map[string]interface{}, ids []string) []string {
	tracks := map[string]string{}
	if openTracks, ok := snapshot["open_tracks"].([]interviewLaunchpadTrack); ok {
		for _, track := range openTracks {
			tracks[track.ID] = fmt.Sprintf("%s / %s", track.DomainLabel, track.Difficulty)
		}
		return mapTrackLabels(ids, tracks)
	}
	if openTracks, ok := snapshot["open_tracks"].([]interface{}); ok {
		for _, raw := range openTracks {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			tracks[stringValue(item["id"])] = fmt.Sprintf("%s / %s", firstNonEmpty(stringValue(item["domain_label"]), stringValue(item["domain"])), stringValue(item["difficulty"]))
		}
	}
	return mapTrackLabels(ids, tracks)
}

func mapTrackLabels(ids []string, tracks map[string]string) []string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, firstNonEmpty(tracks[id], id))
	}
	return items
}

func sortedInsightsDesc(items []domain.LearningDomainInsight) []domain.LearningDomainInsight {
	out := append([]domain.LearningDomainInsight{}, items...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Score > out[i].Score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func stringValue(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func stringSliceValue(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []interface{}:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return []string{}
	}
}
