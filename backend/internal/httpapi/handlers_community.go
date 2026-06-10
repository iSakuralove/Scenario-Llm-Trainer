package httpapi

import (
	"net/http"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"strings"
	"time"
)

func (s *Server) handleCommunity(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	if suffix == "/posts" && r.Method == http.MethodGet {
		query := r.URL.Query()
		writeOK(w, map[string]interface{}{"list": s.visibleCommunityPosts(user, query.Get("status"), query.Get("view"))})
		return
	}
	if suffix == "/posts" && r.Method == http.MethodPost {
		if !s.allowAI(w, r, user, "community-structure", 20) {
			return
		}
		var req struct {
			Title      string   `json:"title"`
			RawContent string   `json:"raw_content"`
			Domain     string   `json:"domain"`
			Tags       []string `json:"tags"`
		}
		if !decode(w, r, &req) {
			return
		}
		var writer *sseWriter
		if wantsSSE(r) {
			writer = newSSEWriter(w)
			writer.stage("received", "case received")
		}
		structureReq := ai.CommunityStructureRequest{
			Title:      req.Title,
			RawContent: req.RawContent,
			Domain:     req.Domain,
			Tags:       req.Tags,
		}
		var structured domain.ScenarioContent
		var err error
		if writer != nil {
			writer.stage("llm", "generating structured preview")
			structured, _, err = s.llmRouter().StructureCommunityPostStream(r.Context(), structureReq, nil)
		} else {
			structured, _, err = s.llmRouter().StructureCommunityPost(r.Context(), structureReq)
		}
		if err != nil {
			if writer != nil {
				writer.fail("AI structure preview failed, please retry")
				return
			}
			writeError(w, http.StatusBadGateway, "AI structure preview failed, please retry")
			return
		}
		if writer != nil {
			writer.stage("schema_validated", "structured preview schema validated")
			writer.stage("rule_sensitive_check", "running rule sensitive check")
			writer.stage("model_sensitive_check", "running model sensitive check")
		}
		check := s.sensitiveCheck(r, user, "raw_content", strings.Join([]string{req.Title, req.RawContent, strings.Join(req.Tags, " ")}, "\n"))
		structured = sanitizeScenarioContent(structured)
		if writer != nil {
			if check.FallbackUsed {
				writer.stage("fallback_sensitive_check", "rule fallback sensitive check used")
			}
			writer.stage("sanitized", "sensitive fields sanitized")
			writer.stage("saving", "saving community preview")
		}
		post := s.store.AddCommunityPost(domain.CommunityPost{
			UserID:              user.ID,
			Title:               ai.Sanitize(req.Title),
			RawContent:          ai.Sanitize(req.RawContent),
			Domain:              req.Domain,
			Tags:                req.Tags,
			AIStructuredContent: structured,
			ReviewHistory:       []domain.ReviewHistoryItem{},
			SensitiveCheck:      check,
			Status:              "pending_review",
		})
		post = s.refreshCommunityModerationSummary(post, "instructor_review")
		s.audit(r, user, "community.create", "community_post", post.ID, map[string]string{"status": post.Status})
		if writer != nil {
			writer.stage("completed", "structured preview completed")
			writer.finish(s.communityPostView(user, &post))
			return
		}
		writeOK(w, s.communityPostView(user, &post))
		return
	}

	parts := split(suffix)
	if len(parts) == 2 && parts[0] == "posts" && r.Method == http.MethodGet {
		post, ok := s.store.GetCommunityPost(parts[1])
		if !ok || !canViewCommunityPost(user, post) {
			writeError(w, http.StatusNotFound, "community post not found")
			return
		}
		writeOK(w, s.communityPostView(user, post))
		return
	}
	if len(parts) == 2 && parts[0] == "posts" && r.Method == http.MethodDelete {
		s.handleCommunityPostDelete(w, r, user, parts[1])
		return
	}
	if len(parts) == 2 && parts[0] == "posts" && r.Method == http.MethodPut {
		s.handleCommunityPostDraftUpdate(w, r, user, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "submit" && r.Method == http.MethodPost {
		s.handleCommunityPostSubmit(w, r, user, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "instructor-review" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleInstructorReview(w, r, user, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "final-review" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleFinalReview(w, r, user, parts[1])
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}
func (s *Server) handleCommunityPostDraftUpdate(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	var req struct {
		Title             string                  `json:"title"`
		RawContent        string                  `json:"raw_content"`
		Domain            string                  `json:"domain"`
		Tags              []string                `json:"tags"`
		StructuredContent *domain.ScenarioContent `json:"structured_content"`
	}
	if !decode(w, r, &req) {
		return
	}
	post, ok := s.store.GetCommunityPost(postID)
	if !ok || post.UserID != user.ID {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "draft" && post.Status != "pending_review" {
		writeError(w, http.StatusBadRequest, "post is not editable")
		return
	}
	checkTitle := post.Title
	checkRawContent := post.RawContent
	checkTags := append([]string{}, post.Tags...)
	if strings.TrimSpace(req.Title) != "" {
		checkTitle = req.Title
		post.Title = ai.Sanitize(req.Title)
	}
	if strings.TrimSpace(req.RawContent) != "" {
		checkRawContent = req.RawContent
		post.RawContent = ai.Sanitize(req.RawContent)
	}
	if strings.TrimSpace(req.Domain) != "" {
		post.Domain = strings.TrimSpace(req.Domain)
	}
	if req.Tags != nil {
		checkTags = append([]string{}, req.Tags...)
		post.Tags = req.Tags
	}
	if req.StructuredContent != nil {
		edited := normalizeScenarioContent(sanitizeScenarioContent(*req.StructuredContent), post.AIStructuredContent)
		post.EditedStructuredContent = &edited
	}
	post.SensitiveCheck = s.sensitiveCheck(r, user, "community_post", strings.Join([]string{checkTitle, checkRawContent, strings.Join(checkTags, " ")}, "\n"))
	if post.Status == "pending_review" {
		post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "author_update", "pending_review", "pending_review", "作者更新待审草稿", post.EditedStructuredContent))
	}
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "instructor_review")
	s.audit(r, user, "community.draft_update", "community_post", updated.ID, map[string]string{"status": updated.Status})
	writeOK(w, s.communityPostView(user, &updated))
}
func (s *Server) handleCommunityPostSubmit(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	post, ok := s.store.GetCommunityPost(postID)
	if !ok || post.UserID != user.ID {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "draft" {
		writeError(w, http.StatusBadRequest, "post is not a draft")
		return
	}
	if !validCommunityScenarioContent(effectiveCommunityContent(post)) {
		writeError(w, http.StatusBadRequest, "structured content is incomplete")
		return
	}
	fromStatus := post.Status
	post.Status = "pending_review"
	post.SensitiveCheck = s.sensitiveCheck(r, user, "community_post", strings.Join([]string{post.Title, post.RawContent, strings.Join(post.Tags, " ")}, "\n"))
	post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "author_submit", fromStatus, post.Status, "提交讲师初审", effectiveCommunityContent(post)))
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "instructor_review")
	s.audit(r, user, "community.submit", "community_post", updated.ID, map[string]string{"status": updated.Status})
	writeOK(w, s.communityPostView(user, &updated))
}
func (s *Server) handleCommunityPostDelete(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	post, ok := s.store.GetCommunityPost(postID)
	if !ok {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if !canDeleteCommunityPost(user, post) {
		writeError(w, http.StatusForbidden, "post cannot be deleted")
		return
	}
	if user.Role == domain.RoleAdmin && post.Status == "published" && strings.TrimSpace(post.ConvertedQuestionID) != "" {
		if scenario, ok := s.store.GetScenario(post.ConvertedQuestionID); ok {
			scenario.Status = "archived"
			s.store.AddScenario(*scenario)
		}
	}
	if !s.store.DeleteCommunityPost(post.ID) {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	s.audit(r, user, "community.delete", "community_post", post.ID, map[string]string{"status": post.Status})
	writeOK(w, map[string]interface{}{"deleted": true, "id": post.ID})
}
func (s *Server) handleInstructorReview(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	if !hasAnyRole(user, domain.RoleInstructor, domain.RoleAdmin) {
		writeError(w, http.StatusForbidden, "instructor role required")
		return
	}
	var req struct {
		Decision          string                  `json:"decision"`
		Note              string                  `json:"note"`
		StructuredContent *domain.ScenarioContent `json:"structured_content"`
	}
	if !decode(w, r, &req) {
		return
	}
	decision := strings.TrimSpace(req.Decision)
	if decision != "approve" && decision != "reject" {
		writeError(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	post, ok := s.store.GetCommunityPost(postID)
	if !ok {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "pending_review" {
		writeError(w, http.StatusBadRequest, "post is not pending instructor review")
		return
	}
	now := time.Now()
	fromStatus := post.Status
	post.ReviewedBy = user.ID
	post.ReviewedAt = &now
	post.ReviewNote = strings.TrimSpace(req.Note)
	if decision == "approve" {
		post.Status = "instructor_approved"
		if req.StructuredContent != nil {
			edited := normalizeScenarioContent(sanitizeScenarioContent(*req.StructuredContent), post.AIStructuredContent)
			post.EditedStructuredContent = &edited
		}
	} else {
		post.Status = "instructor_rejected"
	}
	post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "instructor_"+decision, fromStatus, post.Status, post.ReviewNote, post.EditedStructuredContent))
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "final_review")
	s.audit(r, user, "community.instructor_review", "community_post", post.ID, map[string]string{"decision": decision, "status": post.Status})
	writeOK(w, s.communityPostView(user, &updated))
}
func (s *Server) handleFinalReview(w http.ResponseWriter, r *http.Request, user *domain.User, postID string) {
	if !hasAnyRole(user, domain.RoleAdmin) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	decision := strings.TrimSpace(req.Decision)
	if decision != "publish" && decision != "reject" {
		writeError(w, http.StatusBadRequest, "decision must be publish or reject")
		return
	}
	post, ok := s.store.GetCommunityPost(postID)
	if !ok {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if post.Status != "instructor_approved" {
		writeError(w, http.StatusBadRequest, "post is not pending final review")
		return
	}
	now := time.Now()
	fromStatus := post.Status
	post.FinalizedBy = user.ID
	post.FinalizedAt = &now
	post.FinalNote = strings.TrimSpace(req.Note)
	if decision == "reject" {
		post.Status = "pending_review"
		post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "final_reject", fromStatus, post.Status, post.FinalNote, nil))
		updated := s.store.SaveCommunityPost(post)
		updated = s.refreshCommunityModerationSummary(updated, "instructor_review")
		s.audit(r, user, "community.final_review", "community_post", post.ID, map[string]string{"decision": decision, "status": post.Status})
		writeOK(w, map[string]interface{}{"post": s.communityPostView(user, &updated)})
		return
	}

	content := effectiveCommunityContent(post)
	if !validCommunityScenarioContent(content) {
		writeError(w, http.StatusBadRequest, "structured content is incomplete")
		return
	}
	scenario := s.scenarioFromCommunityPost(post, user.ID)
	created := s.store.AddScenario(scenario)
	post.Status = "published"
	post.ConvertedQuestionID = created.ID
	post.ReviewHistory = append(post.ReviewHistory, reviewHistoryItem(user.ID, "final_publish", fromStatus, post.Status, post.FinalNote, effectiveCommunityContent(post)))
	updated := s.store.SaveCommunityPost(post)
	updated = s.refreshCommunityModerationSummary(updated, "final_review")
	s.audit(r, user, "community.final_review", "community_post", post.ID, map[string]string{"decision": decision, "status": post.Status, "scenario_id": created.ID})
	writeOK(w, map[string]interface{}{
		"post":     s.communityPostView(user, &updated),
		"question": scenarioView(&created, user),
	})
}
