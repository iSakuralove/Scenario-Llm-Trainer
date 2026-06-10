package httpapi

import (
	"fmt"
	"situational-teaching/backend/internal/domain"
	"sort"
	"strings"
	"time"
)

type learningScoreEntry struct {
	Score      int
	At         time.Time
	QuestionID string
}
type learningDomainStats struct {
	Entries              []learningScoreEntry
	CompletedQuestionIDs map[string]bool
}

func (s *Server) learningPlan(user *domain.User) domain.LearningPlan {
	if user == nil {
		return domain.LearningPlan{GeneratedAt: time.Now()}
	}
	scenarioSessions := s.store.ListScenarioSessionsForUser(user.ID)
	interviewSessions := s.store.ListInterviewSessionsForUser(user.ID)
	scenarios := s.store.ListScenarios("", "", "")
	statsByDomain := map[string]*learningDomainStats{}
	completedQuestions := map[string]bool{}

	ensureStats := func(domainName string) *learningDomainStats {
		domainName = strings.TrimSpace(domainName)
		if domainName == "" {
			domainName = "general"
		}
		item := statsByDomain[domainName]
		if item == nil {
			item = &learningDomainStats{CompletedQuestionIDs: map[string]bool{}}
			statsByDomain[domainName] = item
		}
		return item
	}

	for _, session := range scenarioSessions {
		domainName := session.QuestionSnapshot.Domain
		if domainName == "" {
			domainName = "general"
		}
		item := ensureStats(domainName)
		if session.QuestionID != "" {
			item.CompletedQuestionIDs[session.QuestionID] = true
			completedQuestions[session.QuestionID] = true
		}
		if session.Score != nil && session.Score.Total > 0 {
			item.Entries = append(item.Entries, learningScoreEntry{
				Score:      session.Score.Total,
				At:         session.LastActiveAt,
				QuestionID: session.QuestionID,
			})
		}
	}

	for _, session := range interviewSessions {
		if session.FinalScore <= 0 {
			continue
		}
		question, ok := s.store.GetInterviewQuestion(session.QuestionID)
		if !ok {
			continue
		}
		item := ensureStats(question.Domain)
		item.Entries = append(item.Entries, learningScoreEntry{
			Score:      session.FinalScore,
			At:         session.StartedAt,
			QuestionID: session.QuestionID,
		})
	}

	domainNames := map[string]bool{}
	for _, domainName := range user.Profile.PreferredDomains {
		if strings.TrimSpace(domainName) != "" {
			domainNames[domainName] = true
		}
	}
	for domainName := range user.Profile.CapabilityRadar {
		domainNames[domainName] = true
	}
	for domainName := range statsByDomain {
		domainNames[domainName] = true
	}
	for _, scenario := range scenarios {
		if scenario.Status == "active" && strings.TrimSpace(scenario.Domain) != "" {
			domainNames[scenario.Domain] = true
		}
	}

	insights := make([]domain.LearningDomainInsight, 0, len(domainNames))
	for domainName := range domainNames {
		item := statsByDomain[domainName]
		entries := []learningScoreEntry{}
		completedCount := 0
		if item != nil {
			entries = append(entries, item.Entries...)
			completedCount = len(item.CompletedQuestionIDs)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].At.Before(entries[j].At)
		})
		score := user.Profile.CapabilityRadar[domainName]
		if score == 0 {
			score = averageLearningScore(entries, 50)
		}
		lastScore := 0
		if len(entries) > 0 {
			lastScore = entries[len(entries)-1].Score
		}
		insights = append(insights, domain.LearningDomainInsight{
			Domain:         domainName,
			Score:          clamp(score, 0, 100),
			Level:          learningLevel(score),
			Trend:          learningTrend(entries),
			CompletedCount: completedCount,
			LastScore:      lastScore,
			Reason:         learningReason(domainName, score, completedCount),
		})
	}
	sort.Slice(insights, func(i, j int) bool {
		if insights[i].Score == insights[j].Score {
			return insights[i].Domain < insights[j].Domain
		}
		return insights[i].Score < insights[j].Score
	})

	focusDomains := make([]string, 0, 3)
	for _, insight := range insights {
		if len(focusDomains) == 3 {
			break
		}
		focusDomains = append(focusDomains, insight.Domain)
	}
	if len(focusDomains) == 0 {
		focusDomains = append(focusDomains, user.Profile.PreferredDomains...)
	}
	if len(focusDomains) > 3 {
		focusDomains = focusDomains[:3]
	}

	recommendations := s.learningRecommendations(user, scenarios, insights, focusDomains, completedQuestions)
	plan := domain.LearningPlan{
		GeneratedAt:     time.Now(),
		Summary:         learningSummary(user, insights, focusDomains),
		TargetLevel:     user.Profile.TargetLevel,
		FocusDomains:    focusDomains,
		DomainInsights:  insights,
		Recommendations: recommendations,
		ReviewPlan:      buildReviewPlan(focusDomains, recommendations, scenarioSessions, interviewSessions),
	}
	return plan
}
func (s *Server) learningRecommendations(user *domain.User, scenarios []domain.ScenarioQuestion, insights []domain.LearningDomainInsight, focusDomains []string, completedQuestions map[string]bool) []domain.LearningRecommendation {
	focus := map[string]bool{}
	for _, domainName := range focusDomains {
		focus[domainName] = true
	}
	scoreByDomain := map[string]int{}
	for _, insight := range insights {
		scoreByDomain[insight.Domain] = insight.Score
	}

	items := []domain.LearningRecommendation{}
	for _, scenario := range scenarios {
		if scenario.Status != "active" || completedQuestions[scenario.ID] {
			continue
		}
		if len(focus) > 0 && !focus[scenario.Domain] {
			continue
		}
		view := scenarioView(&scenario, user)
		score := scoreByDomain[scenario.Domain]
		priority := clamp(115-score, 40, 100)
		items = append(items, domain.LearningRecommendation{
			ID:          "scenario:" + scenario.ID,
			Kind:        "scenario",
			Domain:      scenario.Domain,
			Title:       scenario.Title,
			Description: scenario.Description,
			Difficulty:  scenario.Difficulty,
			Priority:    priority,
			Reason:      fmt.Sprintf("AI 推荐：%s 当前画像分为 %d，适合通过情景排查补强。", displayDomain(scenario.Domain), score),
			ActionLabel: "进入排查工坊",
			ActionPath:  "/scenarios",
			Question:    &view,
		})
	}
	if len(items) == 0 {
		for _, scenario := range scenarios {
			if scenario.Status != "active" {
				continue
			}
			view := scenarioView(&scenario, user)
			items = append(items, domain.LearningRecommendation{
				ID:          "scenario:" + scenario.ID,
				Kind:        "scenario",
				Domain:      scenario.Domain,
				Title:       scenario.Title,
				Description: scenario.Description,
				Difficulty:  scenario.Difficulty,
				Priority:    65,
				Reason:      "规则回退：作为通用复习题补齐近期训练节奏。",
				ActionLabel: "进入排查工坊",
				ActionPath:  "/scenarios",
				Question:    &view,
			})
			if len(items) == 3 {
				break
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].Title < items[j].Title
		}
		return items[i].Priority > items[j].Priority
	})
	if len(items) > 4 {
		items = items[:4]
	}
	if len(focusDomains) > 0 {
		domainName := focusDomains[0]
		score := scoreByDomain[domainName]
		items = append(items, domain.LearningRecommendation{
			ID:          "interview:" + domainName,
			Kind:        "interview",
			Domain:      domainName,
			Title:       displayDomain(domainName) + "专项面试追问",
			Description: "用两轮文本追问检查排查表达、证据组织和根因归纳。",
			Difficulty:  targetInterviewDifficulty(user.Profile.TargetLevel),
			Priority:    clamp(105-score, 45, 95),
			Reason:      fmt.Sprintf("AI 推荐：围绕 %s 做一次面试模拟，验证能否把排查过程讲清楚。", displayDomain(domainName)),
			ActionLabel: "进入面试舱",
			ActionPath:  "/interviews",
		})
	}
	return items
}
func scenarioRecommendationsFromPlan(plan domain.LearningPlan) []domain.ScenarioQuestionView {
	views := []domain.ScenarioQuestionView{}
	for _, item := range plan.Recommendations {
		if item.Question == nil {
			continue
		}
		views = append(views, *item.Question)
		if len(views) == 3 {
			break
		}
	}
	return views
}
func weakPointsFromPlan(plan domain.LearningPlan, fallback []domain.WeakPoint) []domain.WeakPoint {
	points := []domain.WeakPoint{}
	questionsByDomain := map[string][]string{}
	for _, item := range plan.Recommendations {
		if item.Kind != "scenario" || item.Question == nil {
			continue
		}
		questionsByDomain[item.Domain] = append(questionsByDomain[item.Domain], item.Question.ID)
	}
	for _, insight := range plan.DomainInsights {
		if insight.Score >= 75 && insight.CompletedCount > 0 {
			continue
		}
		topic := "基线训练"
		if insight.CompletedCount > 0 {
			topic = insight.Level
		}
		lastScore := insight.LastScore
		if lastScore == 0 {
			lastScore = insight.Score
		}
		points = append(points, domain.WeakPoint{
			Domain:             insight.Domain,
			Topic:              topic,
			LastScore:          lastScore,
			SuggestedQuestions: append([]string{}, questionsByDomain[insight.Domain]...),
		})
		if len(points) == 3 {
			break
		}
	}
	if len(points) == 0 {
		return fallback
	}
	return points
}
func reviewCalendarFromPlan(user *domain.User, plan domain.LearningPlan, now time.Time) domain.ReviewCalendar {
	today := now.Format("2006-01-02")
	checkinDates := []string{}
	streakDays := 0
	todayChecked := false
	if user != nil {
		checkinDates = normalizeCheckinDates(user.Profile.CheckinDates)
		if user.Profile.LastCheckinDate != "" && !containsString(checkinDates, user.Profile.LastCheckinDate) {
			checkinDates = append(checkinDates, user.Profile.LastCheckinDate)
			checkinDates = normalizeCheckinDates(checkinDates)
		}
		todayChecked = containsString(checkinDates, today)
		streakDays = streakFromDates(checkinDates, today)
	}
	return domain.ReviewCalendar{
		GeneratedAt:  now,
		CheckinDates: checkinDates,
		StreakDays:   streakDays,
		TodayChecked: todayChecked,
		Today:        today,
		ReviewPlan:   plan.ReviewPlan,
		FocusDomains: append([]string{}, plan.FocusDomains...),
		NextAction:   nextReviewAction(plan),
	}
}
func (s *Server) checkin(user *domain.User, now time.Time) (domain.CheckinResult, *domain.User, error) {
	if user == nil {
		return domain.CheckinResult{}, nil, fmt.Errorf("user not found")
	}
	today := now.Format("2006-01-02")
	profile := user.Profile
	dates := normalizeCheckinDates(profile.CheckinDates)
	already := containsString(dates, today)
	if !already {
		dates = append(dates, today)
		dates = normalizeCheckinDates(dates)
	}
	profile.CheckinDates = dates
	profile.LastCheckinDate = today
	profile.TotalStats.StreakDays = streakFromDates(dates, today)
	updated, err := s.store.SaveUserProfile(user.ID, profile)
	if err != nil {
		return domain.CheckinResult{}, nil, err
	}
	result := domain.CheckinResult{
		CheckedIn:        true,
		AlreadyCheckedIn: already,
		CheckinDate:      today,
		StreakDays:       updated.Profile.TotalStats.StreakDays,
		NextAction:       nextReviewAction(s.learningPlan(updated)),
	}
	return result, updated, nil
}
func normalizeCheckinDates(dates []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dates))
	for _, value := range dates {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func streakFromDates(dates []string, today string) int {
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	current, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 0
	}
	checked := map[string]bool{}
	for _, value := range normalizeCheckinDates(dates) {
		checked[value] = true
	}
	streak := 0
	for {
		key := current.Format("2006-01-02")
		if !checked[key] {
			break
		}
		streak++
		current = current.AddDate(0, 0, -1)
	}
	return streak
}
func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func nextReviewAction(plan domain.LearningPlan) string {
	if len(plan.ReviewPlan) == 0 {
		return "先完成一题排查训练，建立本周复习样本。"
	}
	first := plan.ReviewPlan[0]
	if len(first.Actions) > 0 {
		return first.DayLabel + "：" + first.Actions[0]
	}
	return first.DayLabel + "：" + first.Focus
}
func averageLearningScore(entries []learningScoreEntry, fallback int) int {
	if len(entries) == 0 {
		return fallback
	}
	total := 0
	for _, entry := range entries {
		total += entry.Score
	}
	return total / len(entries)
}
func learningLevel(score int) string {
	switch {
	case score >= 85:
		return "稳定"
	case score >= 70:
		return "可提升"
	case score >= 60:
		return "需巩固"
	default:
		return "重点补强"
	}
}
func learningTrend(entries []learningScoreEntry) string {
	if len(entries) < 2 {
		return "样本不足"
	}
	last := entries[len(entries)-1].Score
	prev := entries[len(entries)-2].Score
	switch {
	case last-prev >= 5:
		return "上升"
	case prev-last >= 5:
		return "下降"
	default:
		return "稳定"
	}
}
func learningReason(domainName string, score int, completedCount int) string {
	switch {
	case completedCount == 0:
		return fmt.Sprintf("%s 还没有完成记录，建议先做一轮基础训练。", displayDomain(domainName))
	case score < 60:
		return fmt.Sprintf("%s 得分偏低，需要优先补证据收集和根因归纳。", displayDomain(domainName))
	case score < 75:
		return fmt.Sprintf("%s 已有基础，但稳定性还不够，适合做专项复盘。", displayDomain(domainName))
	default:
		return fmt.Sprintf("%s 表现较稳，可以用面试追问提高表达质量。", displayDomain(domainName))
	}
}
func learningSummary(user *domain.User, insights []domain.LearningDomainInsight, focusDomains []string) string {
	if len(focusDomains) == 0 {
		return "当前训练样本还不多，建议先完成一次排查题和一次面试，建立基线画像。"
	}
	focusLabels := make([]string, 0, len(focusDomains))
	for _, domainName := range focusDomains {
		focusLabels = append(focusLabels, displayDomain(domainName))
	}
	average := user.Profile.TotalStats.AverageScore
	if average == 0 {
		return fmt.Sprintf("目标职级为 %s，建议先围绕 %s 建立训练样本。", displayTargetLevel(user.Profile.TargetLevel), strings.Join(focusLabels, "、"))
	}
	return fmt.Sprintf("当前平均分 %d，下一轮优先补强 %s，并用面试追问验证表达完整度。", average, strings.Join(focusLabels, "、"))
}
func buildReviewPlan(focusDomains []string, recommendations []domain.LearningRecommendation, scenarioSessions []domain.ScenarioSession, interviewSessions []domain.InterviewSession) []domain.ReviewPlanItem {
	wrongItems := reviewItemsFromHistory(scenarioSessions, interviewSessions)
	if len(wrongItems) >= 3 {
		return wrongItems[:3]
	}
	if len(focusDomains) == 0 {
		focusDomains = []string{"database", "network", "os"}
	}
	templates := []struct {
		Day     string
		Focus   string
		Actions []string
		Minutes int
		Target  int
	}{
		{Day: "第 1 天", Focus: "完成一题并标记关键证据", Actions: []string{"完成 1 道情景排查题", "记录至少 3 条关键证据", "复盘遗漏线索"}, Minutes: 35, Target: 70},
		{Day: "第 2 天", Focus: "补一次面试表达", Actions: []string{"完成 1 次文本面试", "把根因、证据、修复动作压缩成 2 分钟表达", "整理追问中的缺口"}, Minutes: 30, Target: 75},
		{Day: "第 3 天", Focus: "回看错因并做同域巩固", Actions: []string{"重看最近复盘报告", "复述标准排查步骤", "再做 1 道同域题或 UGC 转化题"}, Minutes: 40, Target: 80},
	}
	items := make([]domain.ReviewPlanItem, 0, len(templates))
	items = append(items, wrongItems...)
	for i, template := range templates {
		if len(items) == 3 {
			break
		}
		domainName := focusDomains[i%len(focusDomains)]
		questionIDs := []string{}
		for _, recommendation := range recommendations {
			if recommendation.Kind == "scenario" && recommendation.Domain == domainName && recommendation.Question != nil {
				questionIDs = append(questionIDs, recommendation.Question.ID)
			}
			if len(questionIDs) == 2 {
				break
			}
		}
		items = append(items, domain.ReviewPlanItem{
			DayLabel:         template.Day,
			Domain:           domainName,
			Focus:            displayDomain(domainName) + "：" + template.Focus,
			Actions:          append([]string{}, template.Actions...),
			EstimatedMinutes: template.Minutes,
			TargetScore:      template.Target,
			QuestionIDs:      questionIDs,
			SourceKind:       "recommendation",
			Reason:           "当前没有足够低分错题，使用画像推荐补齐复习计划。",
		})
	}
	return items
}
func reviewItemsFromHistory(scenarioSessions []domain.ScenarioSession, interviewSessions []domain.InterviewSession) []domain.ReviewPlanItem {
	items := []domain.ReviewPlanItem{}
	for _, session := range scenarioSessions {
		if session.Score == nil || session.Score.Total >= 75 {
			continue
		}
		domainName := session.QuestionSnapshot.Domain
		if domainName == "" {
			domainName = "general"
		}
		items = append(items, domain.ReviewPlanItem{
			DayLabel:         fmt.Sprintf("第 %d 天", len(items)+1),
			Domain:           domainName,
			Focus:            displayDomain(domainName) + "：复盘低分排查题",
			Actions:          []string{"重看完整对话记录", "对照标准步骤补齐遗漏证据", "重新写一版根因判断"},
			EstimatedMinutes: 35,
			TargetScore:      80,
			QuestionIDs:      []string{session.QuestionID},
			SourceKind:       "scenario_wrong",
			SourceID:         session.ID,
			Reason:           fmt.Sprintf("最近排查得分 %d，优先安排错题复盘。", session.Score.Total),
		})
	}
	for _, session := range interviewSessions {
		if session.FinalScore <= 0 || session.FinalScore >= 75 {
			continue
		}
		items = append(items, domain.ReviewPlanItem{
			DayLabel:         fmt.Sprintf("第 %d 天", len(items)+1),
			Domain:           "interview",
			Focus:            "面试表达：复盘低分回答",
			Actions:          []string{"重读本轮不足项", "按五维评分补一版结构化回答", "控制在 2 分钟内复述"},
			EstimatedMinutes: 30,
			TargetScore:      78,
			QuestionIDs:      []string{session.QuestionID},
			SourceKind:       "interview_wrong",
			SourceID:         session.ID,
			Reason:           fmt.Sprintf("最近面试得分 %d，建议优先复盘表达结构。", session.FinalScore),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].SourceID > items[j].SourceID
	})
	return items
}
func targetInterviewDifficulty(targetLevel string) string {
	switch targetLevel {
	case "junior":
		return "L2"
	case "senior", "architect":
		return "L4"
	default:
		return "L3"
	}
}
func displayDomain(value string) string {
	switch value {
	case "database":
		return "数据库"
	case "network":
		return "网络"
	case "os":
		return "操作系统"
	case "security":
		return "安全"
	case "devops":
		return "DevOps"
	default:
		return value
	}
}
func displayTargetLevel(value string) string {
	switch value {
	case "junior":
		return "初级"
	case "senior":
		return "高级"
	case "architect":
		return "架构师"
	default:
		return "中级"
	}
}
