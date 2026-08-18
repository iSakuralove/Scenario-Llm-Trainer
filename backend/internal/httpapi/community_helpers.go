package httpapi

import (
	"context"
	"sort"
	"strings"
	"time"

	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

func (s *Server) visibleCommunityPosts(user *domain.User, status string, view string) []domain.CommunityPost {
	status = strings.TrimSpace(status)
	view = strings.TrimSpace(view)
	items := s.store.ListCommunityPosts()
	out := make([]domain.CommunityPost, 0, len(items))
	for _, item := range items {
		if !canViewCommunityPost(user, &item) {
			continue
		}
		if view != "" {
			if !matchesCommunityPostHistoryView(user, &item, view) {
				continue
			}
			viewItem := s.communityPostView(user, &item)
			out = append(out, viewItem)
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		viewItem := s.communityPostView(user, &item)
		out = append(out, viewItem)
	}
	return out
}
func matchesCommunityPostHistoryView(user *domain.User, post *domain.CommunityPost, view string) bool {
	if user == nil || post == nil {
		return false
	}
	action := ""
	switch view {
	case "instructor_reviewed":
		action = "instructor_approve"
	case "instructor_rejected":
		action = "instructor_reject"
	default:
		return false
	}
	for _, item := range post.ReviewHistory {
		if item.ActorID == user.ID && item.Action == action {
			return true
		}
	}
	return false
}
func (s *Server) scenarioFromCommunityPost(post *domain.CommunityPost, adminID string) domain.ScenarioQuestion {
	domainName := strings.TrimSpace(post.Domain)
	if domainName == "" {
		domainName = "database"
	}
	tags := append([]string{}, post.Tags...)
	if len(tags) == 0 {
		tags = []string{"UGC", domainName}
	}
	description := strings.TrimSpace(post.RawContent)
	if description == "" {
		description = post.Title
	}
	scenario := domain.ScenarioQuestion{
		Title:        ai.SanitizeFields(post.Title),
		Description:  ai.SanitizeFields(description),
		Domain:       domainName,
		Difficulty:   "L2",
		ScenarioType: "troubleshooting",
		Tags:         sanitizeTextSlice(tags),
		Content:      sanitizeScenarioContent(*effectiveCommunityContent(post)),
		Status:       "active",
		Source:       "ugc_structured",
		CreatedBy:    adminID,
		Version:      1,
	}
	scenario.Content = ai.PrepareScenarioContent(scenario.Content, scenario)
	return scenario
}
func communityPostFromScenarioFork(source *domain.ScenarioQuestion, userID string) domain.CommunityPost {
	content := forkDraftContent(source)
	title := ""
	rawContent := ""
	domainName := "database"
	tags := []string{"Fork"}
	sourceID := ""
	if source != nil {
		title = "派生题目：" + source.Title
		rawContent = source.Description
		domainName = source.Domain
		tags = append([]string{}, source.Tags...)
		sourceID = source.ID
	}
	if len(tags) == 0 {
		tags = []string{"Fork", domainName}
	}
	edited := content
	return domain.CommunityPost{
		UserID:                  userID,
		Title:                   title,
		RawContent:              rawContent,
		Domain:                  domainName,
		Tags:                    tags,
		ForkedFromScenarioID:    sourceID,
		AIStructuredContent:     content,
		EditedStructuredContent: &edited,
		SensitiveCheck:          ruleSensitiveCheck("fork_source", strings.Join([]string{title, rawContent, strings.Join(tags, " ")}, "\n")),
		Status:                  "draft",
		ReviewHistory:           []domain.ReviewHistoryItem{},
	}
}
func forkDraftContent(source *domain.ScenarioQuestion) domain.ScenarioContent {
	content := domain.ScenarioContent{
		RevealStrategy: domain.RevealStrategy{
			SurfaceClues: []domain.Clue{},
			DeepClues:    []domain.Clue{},
			Distractors:  []domain.Clue{},
		},
		ReferenceLinks: []string{},
	}
	if source == nil {
		return content
	}
	content.ArchitectureDiagram = source.Content.ArchitectureDiagram
	return content
}
func effectiveCommunityContent(post *domain.CommunityPost) *domain.ScenarioContent {
	if post != nil && post.EditedStructuredContent != nil {
		return post.EditedStructuredContent
	}
	if post == nil {
		return &domain.ScenarioContent{}
	}
	return &post.AIStructuredContent
}
func (s *Server) refreshCommunityModerationSummary(post domain.CommunityPost, stage string) domain.CommunityPost {
	agent := agentruntime.NewCommunityReviewAgent(agentruntime.CommunityReviewConfig{})
	result, err := agent.Run(context.Background(), agentruntime.CommunityReviewRequest{
		Post:         &post,
		Stage:        stage,
		ReviewerRole: domain.RoleInstructor,
	})
	if err != nil {
		s.auditCommunityReviewAgentRun(nil, nil, post.ID, result.Trace, result.Summary, "failed", err)
		return post
	}
	post.ModerationSummary = result.Summary
	updated := s.store.SaveCommunityPost(&post)
	s.auditCommunityReviewAgentRun(nil, nil, updated.ID, result.Trace, result.Summary, "completed", nil)
	return updated
}
func (s *Server) communityPostView(user *domain.User, post *domain.CommunityPost) domain.CommunityPost {
	if post == nil {
		return domain.CommunityPost{}
	}
	view := *post
	view.AuthorUsername = communityPostAuthorName(s.store, post)
	view.AIStructuredContent = prepareCommunityContentForView(view.AIStructuredContent, view.Title, view.Domain)
	if post.EditedStructuredContent != nil {
		edited := prepareCommunityContentForView(*post.EditedStructuredContent, view.Title, view.Domain)
		view.EditedStructuredContent = &edited
	}
	if len(view.ReviewHistory) > 0 {
		view.ReviewHistory = append([]domain.ReviewHistoryItem{}, view.ReviewHistory...)
		for i := range view.ReviewHistory {
			if view.ReviewHistory[i].Content == nil {
				continue
			}
			content := prepareCommunityContentForView(*view.ReviewHistory[i].Content, view.Title, view.Domain)
			view.ReviewHistory[i].Content = &content
		}
	}
	if post.ModerationSummary != nil {
		summary := *post.ModerationSummary
		summary.SafeLabels = append([]string{}, post.ModerationSummary.SafeLabels...)
		summary.Reasons = append([]string{}, post.ModerationSummary.Reasons...)
		summary.AgentTrace = nil
		view.ModerationSummary = &summary
	}
	if !hasAnyRole(user, domain.RoleInstructor, domain.RoleAdmin) {
		view.ModerationSummary = nil
	}
	return view
}
func communityPostAuthorName(dataStore store.Store, post *domain.CommunityPost) string {
	if dataStore == nil || post == nil || strings.TrimSpace(post.UserID) == "" {
		return "未知作者"
	}
	user, ok := dataStore.GetUser(post.UserID)
	if !ok || user == nil || strings.TrimSpace(user.Username) == "" {
		return "用户已注销"
	}
	return user.Username
}
func prepareCommunityContentForView(content domain.ScenarioContent, title, domainName string) domain.ScenarioContent {
	return ai.PrepareScenarioContent(content, domain.ScenarioQuestion{
		Title:   title,
		Domain:  domainName,
		Content: content,
	})
}
func validCommunityScenarioContent(content *domain.ScenarioContent) bool {
	if content == nil {
		return false
	}
	return strings.TrimSpace(content.RootCause) != "" &&
		len(content.KeyEvidence) > 0 &&
		len(content.StandardProcedure) > 0
}
func normalizeScenarioContent(content domain.ScenarioContent, fallback domain.ScenarioContent) domain.ScenarioContent {
	if strings.TrimSpace(content.RootCause) == "" {
		content.RootCause = fallback.RootCause
	}
	if len(content.RootCauseKeywords) == 0 {
		content.RootCauseKeywords = append([]string{}, fallback.RootCauseKeywords...)
	}
	if len(content.KeyEvidence) == 0 {
		content.KeyEvidence = append([]string{}, fallback.KeyEvidence...)
	}
	if len(content.StandardProcedure) == 0 {
		content.StandardProcedure = append([]string{}, fallback.StandardProcedure...)
	}
	if strings.TrimSpace(content.ArchitectureDiagram) == "" {
		content.ArchitectureDiagram = fallback.ArchitectureDiagram
	}
	if len(content.ReferenceLinks) == 0 {
		content.ReferenceLinks = append([]string{}, fallback.ReferenceLinks...)
	}
	if len(content.RevealStrategy.SurfaceClues) == 0 && len(content.RevealStrategy.DeepClues) == 0 && len(content.RevealStrategy.Distractors) == 0 {
		content.RevealStrategy = fallback.RevealStrategy
	}
	return content
}
func sanitizeScenarioContent(content domain.ScenarioContent) domain.ScenarioContent {
	content.RootCause = ai.SanitizeFields(content.RootCause)
	content.RootCauseKeywords = sanitizeTextSlice(content.RootCauseKeywords)
	content.KeyEvidence = sanitizeTextSlice(content.KeyEvidence)
	content.StandardProcedure = sanitizeTextSlice(content.StandardProcedure)
	content.ReferenceLinks = sanitizeTextSlice(content.ReferenceLinks)
	content.ArchitectureDiagram = ai.SanitizeFields(content.ArchitectureDiagram)
	content.ArchitectureDiagramSpec = ai.SanitizeScenarioDiagramSpec(content.ArchitectureDiagramSpec)
	content.RevealStrategy.SurfaceClues = append([]domain.Clue{}, content.RevealStrategy.SurfaceClues...)
	for i, clue := range content.RevealStrategy.SurfaceClues {
		clue.Content = ai.SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = ai.SanitizeFields(clue.RecommendedNextAsk)
		content.RevealStrategy.SurfaceClues[i] = clue
	}
	content.RevealStrategy.DeepClues = append([]domain.Clue{}, content.RevealStrategy.DeepClues...)
	for i, clue := range content.RevealStrategy.DeepClues {
		clue.Content = ai.SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = ai.SanitizeFields(clue.RecommendedNextAsk)
		content.RevealStrategy.DeepClues[i] = clue
	}
	content.RevealStrategy.Distractors = append([]domain.Clue{}, content.RevealStrategy.Distractors...)
	for i, clue := range content.RevealStrategy.Distractors {
		clue.Content = ai.SanitizeFields(clue.Content)
		clue.RecommendedNextAsk = ai.SanitizeFields(clue.RecommendedNextAsk)
		content.RevealStrategy.Distractors[i] = clue
	}
	return content
}
func sanitizeTextSlice(values []string) []string {
	items := append([]string{}, values...)
	for i, value := range items {
		items[i] = ai.SanitizeFields(value)
	}
	return items
}
func canViewCommunityPost(user *domain.User, post *domain.CommunityPost) bool {
	if user == nil || post == nil {
		return false
	}
	if post.UserID == user.ID {
		return true
	}
	if post.Status == "draft" {
		return false
	}
	return user.Role == domain.RoleInstructor || user.Role == domain.RoleAdmin
}
func canDeleteCommunityPost(user *domain.User, post *domain.CommunityPost) bool {
	if user == nil || post == nil {
		return false
	}
	if post.Status == "published" {
		return user.Role == domain.RoleAdmin
	}
	if user.Role == domain.RoleAdmin {
		return true
	}
	if post.UserID != user.ID {
		return false
	}
	return post.Status == "draft" || post.Status == "pending_review" || post.Status == "instructor_rejected" || post.Status == "final_rejected"
}
func reviewHistoryItem(actorID, action, fromStatus, toStatus, note string, content *domain.ScenarioContent) domain.ReviewHistoryItem {
	item := domain.ReviewHistoryItem{
		ID:         store.NewID(),
		ActorID:    actorID,
		Action:     action,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Note:       strings.TrimSpace(note),
		CreatedAt:  time.Now(),
	}
	if content != nil {
		copy := *content
		item.Content = &copy
	}
	return item
}
func communityPostCount(posts []domain.CommunityPost) int {
	return len(posts)
}
func (s *Server) communityPostsForUserHistory(userID string) []domain.CommunityPost {
	items := s.store.ListCommunityPosts()
	out := make([]domain.CommunityPost, 0, len(items))
	user := &domain.User{ID: userID, Role: domain.RoleStudent}
	for _, item := range items {
		if item.UserID != userID {
			continue
		}
		viewItem := s.communityPostView(user, &item)
		out = append(out, viewItem)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}
