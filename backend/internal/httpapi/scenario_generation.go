package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
)

type scenarioGenerationPayload struct {
	Domain       string                                `json:"domain"`
	Difficulty   string                                `json:"difficulty"`
	ScenarioType string                                `json:"scenario_type"`
	Tags         []string                              `json:"tags"`
	Constraints  *scenarioGenerationConstraintsPayload `json:"constraints,omitempty"`
}

type scenarioGenerationConstraintsPayload struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TopicScope    []string `json:"topic_scope"`
	RootCauseHint string   `json:"root_cause_hint"`
	EvidenceHints []string `json:"evidence_hints"`
	ClueHints     []string `json:"clue_hints"`
}

type scenarioGenerationValidationError struct {
	status  int
	message string
}

func (e scenarioGenerationValidationError) Error() string {
	return e.message
}

var (
	allowedScenarioDifficulties = map[string]bool{"L1": true, "L2": true, "L3": true, "L4": true, "L5": true}
	allowedScenarioTypes        = map[string]bool{"troubleshooting": true, "design": true, "performance": true}
)

func normalizeScenarioGenerationPayload(req scenarioGenerationPayload) (scenarioGenerationPayload, error) {
	req.Domain = firstNonEmpty(strings.TrimSpace(req.Domain), "database")
	req.Difficulty = firstNonEmpty(strings.TrimSpace(req.Difficulty), "L2")
	req.ScenarioType = firstNonEmpty(strings.TrimSpace(req.ScenarioType), "troubleshooting")
	req.Tags = normalizeScenarioGenerationTags(req.Tags, req.Domain)
	req.Constraints = normalizeScenarioGenerationConstraints(req.Constraints)
	if !allowedScenarioDifficulties[req.Difficulty] {
		return scenarioGenerationPayload{}, fmt.Errorf("difficulty must be one of L1, L2, L3, L4, L5")
	}
	if !allowedScenarioTypes[req.ScenarioType] {
		return scenarioGenerationPayload{}, fmt.Errorf("scenario_type must be one of troubleshooting, design, performance")
	}
	return req, nil
}

func normalizeScenarioGenerationConstraints(input *scenarioGenerationConstraintsPayload) *scenarioGenerationConstraintsPayload {
	if input == nil {
		return nil
	}
	normalized := &scenarioGenerationConstraintsPayload{
		Title:         strings.TrimSpace(input.Title),
		Description:   strings.TrimSpace(input.Description),
		TopicScope:    normalizeScenarioGenerationHints(input.TopicScope, 6),
		RootCauseHint: strings.TrimSpace(input.RootCauseHint),
		EvidenceHints: normalizeScenarioGenerationHints(input.EvidenceHints, 6),
		ClueHints:     normalizeScenarioGenerationHints(input.ClueHints, 6),
	}
	if normalized.Title == "" &&
		normalized.Description == "" &&
		len(normalized.TopicScope) == 0 &&
		normalized.RootCauseHint == "" &&
		len(normalized.EvidenceHints) == 0 &&
		len(normalized.ClueHints) == 0 {
		return nil
	}
	return normalized
}
func normalizeScenarioGenerationHints(values []string, limit int) []string {
	items := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[strings.ToLower(trimmed)] {
			continue
		}
		seen[strings.ToLower(trimmed)] = true
		items = append(items, trimmed)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items
}
func (req scenarioGenerationPayload) toAIConstraints() ai.ScenarioGenerationConstraints {
	if req.Constraints == nil {
		return ai.ScenarioGenerationConstraints{}
	}
	return ai.ScenarioGenerationConstraints{
		Title:         req.Constraints.Title,
		Description:   req.Constraints.Description,
		TopicScope:    append([]string{}, req.Constraints.TopicScope...),
		RootCauseHint: req.Constraints.RootCauseHint,
		EvidenceHints: append([]string{}, req.Constraints.EvidenceHints...),
		ClueHints:     append([]string{}, req.Constraints.ClueHints...),
	}
}
func (s *Server) validateScenarioGenerationRequest(r *http.Request, user *domain.User, req scenarioGenerationPayload) error {
	if req.Constraints == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "title", value: req.Constraints.Title},
		{name: "description", value: req.Constraints.Description},
		{name: "root_cause_hint", value: req.Constraints.RootCauseHint},
	} {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, field.name, field.value); err != nil {
			return err
		}
	}
	for _, item := range req.Constraints.TopicScope {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, "topic_scope", item); err != nil {
			return err
		}
	}
	for _, item := range req.Constraints.EvidenceHints {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, "evidence_hints", item); err != nil {
			return err
		}
	}
	for _, item := range req.Constraints.ClueHints {
		if err := s.ensureScenarioGenerationConstraintSafe(r, user, "clue_hints", item); err != nil {
			return err
		}
	}
	if err := s.ensureScenarioGenerationNotDuplicate(req); err != nil {
		return err
	}
	return nil
}
func (s *Server) ensureScenarioGenerationConstraintSafe(r *http.Request, user *domain.User, field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	check := s.sensitiveCheck(r, user, field, value)
	if check.Blocked || check.Status == "risk" {
		return scenarioGenerationValidationError{
			status:  http.StatusBadRequest,
			message: fmt.Sprintf("sensitive content is not allowed in %s", field),
		}
	}
	return nil
}
func (s *Server) ensureScenarioGenerationNotDuplicate(req scenarioGenerationPayload) error {
	title := ""
	if req.Constraints != nil {
		title = strings.TrimSpace(req.Constraints.Title)
	}
	if title == "" {
		return nil
	}
	items := s.store.ListScenarios(req.Domain, req.Difficulty, "")
	for _, item := range items {
		if item.Status != "active" || item.ScenarioType != req.ScenarioType {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.Title), title) || ai.Similarity(item.Title, title) >= 0.92 {
			return scenarioGenerationValidationError{
				status:  http.StatusConflict,
				message: "duplicate scenario title detected",
			}
		}
	}
	return nil
}
func writeScenarioGenerationValidationError(w http.ResponseWriter, err error) {
	var validationErr scenarioGenerationValidationError
	if errors.As(err, &validationErr) {
		writeError(w, validationErr.status, validationErr.message)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
func normalizeScenarioGenerationTags(tags []string, domainName string) []string {
	cleaned := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		value := strings.TrimSpace(tag)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	if len(cleaned) == 0 {
		return []string{"AI生成", domainName}
	}
	return cleaned
}
func (s *Server) createScenarioGenerationJob(userID string, req scenarioGenerationPayload) (domain.AIJob, error) {
	now := time.Now()
	planned := s.llmRouter().PlannedProviderInfo(ai.RouterTaskScenarioGenerate)
	job, err := s.store.CreateAIJob(domain.AIJob{
		UserID:    userID,
		Kind:      domain.AIJobKindScenarioGeneration,
		Status:    domain.AIJobStatusQueued,
		Stage:     "queued",
		Progress:  5,
		Provider:  planned.Provider,
		Model:     planned.Model,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return domain.AIJob{}, err
	}
	go s.runScenarioGenerationJob(job.ID, userID, req)
	return job, nil
}
func (s *Server) runScenarioGenerationJob(jobID, userID string, req scenarioGenerationPayload) {
	job, ok := s.store.GetAIJob(jobID)
	if !ok {
		return
	}
	if job.Status == domain.AIJobStatusCanceled {
		return
	}
	startedAt := time.Now()
	planned := s.llmRouter().PlannedProviderInfo(ai.RouterTaskScenarioGenerate)
	job.Status = domain.AIJobStatusRunning
	job.Stage = "calling_model"
	job.Progress = 30
	if strings.TrimSpace(job.Provider) == "" {
		job.Provider = planned.Provider
	}
	if strings.TrimSpace(job.Model) == "" {
		job.Model = planned.Model
	}
	job.StartedAt = &startedAt
	if _, err := s.store.SaveAIJob(job); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	s.registerAIJobCancel(jobID, cancel)
	defer s.unregisterAIJobCancel(jobID)
	defer cancel()
	if canceled := s.loadCancelableAIJob(jobID); canceled != nil && canceled.Status == domain.AIJobStatusCanceled {
		return
	}
	question, llmMeta, err := s.llmRouter().GenerateScenario(ctx, ai.ScenarioGenerationRequest{
		Domain:       req.Domain,
		Difficulty:   req.Difficulty,
		ScenarioType: req.ScenarioType,
		Tags:         req.Tags,
		Constraints:  req.toAIConstraints(),
		UserID:       userID,
		Nonce:        fmt.Sprintf("%d", time.Now().UnixNano()),
	})
	if err != nil {
		if ctx.Err() == context.Canceled {
			if canceled := s.loadCancelableAIJob(jobID); canceled != nil && canceled.Status == domain.AIJobStatusCanceled {
				return
			}
		}
		completedAt := time.Now()
		job.Status = domain.AIJobStatusFailed
		job.Stage = scenarioGenerationFailureStage(llmMeta)
		job.Progress = 100
		job.ErrorMessage = scenarioGenerationErrorMessage(err, llmMeta)
		if strings.TrimSpace(llmMeta.Provider) != "" {
			job.Provider = llmMeta.Provider
		}
		if strings.TrimSpace(llmMeta.Model) != "" {
			job.Model = llmMeta.Model
		}
		job.Validated = llmMeta.Validated
		job.FallbackUsed = llmMeta.FallbackUsed
		job.CompletedAt = &completedAt
		_, _ = s.store.SaveAIJob(job)
		s.auditScenarioGenerationFailed(userID, jobID, *job, llmMeta, req, err)
		return
	}

	job.Stage = "validating_output"
	job.Progress = 75
	job.Provider = llmMeta.Provider
	job.Model = llmMeta.Model
	job.Validated = llmMeta.Validated
	job.FallbackUsed = llmMeta.FallbackUsed
	if _, err := s.store.SaveAIJob(job); err != nil {
		return
	}

	created := s.store.AddScenario(question)
	completedAt := time.Now()
	job.Status = domain.AIJobStatusCompleted
	job.Stage = "completed"
	job.Progress = 100
	job.ResultQuestionID = created.ID
	job.CompletedAt = &completedAt
	_, _ = s.store.SaveAIJob(job)
	s.auditScenarioGenerationCompleted(userID, jobID, created, llmMeta, req)
}
func (s *Server) cancelAIJob(job *domain.AIJob) (*domain.AIJob, error) {
	if job == nil {
		return nil, errors.New("ai job not found")
	}
	switch job.Status {
	case domain.AIJobStatusCompleted, domain.AIJobStatusFailed:
		return nil, errors.New("ai job already finished")
	case domain.AIJobStatusCanceled:
		return job, nil
	}
	completedAt := time.Now()
	job.Status = domain.AIJobStatusCanceled
	job.Stage = "canceled"
	job.Progress = 100
	job.CompletedAt = &completedAt
	job.ErrorMessage = ""
	saved, err := s.store.SaveAIJob(job)
	if err != nil {
		return nil, err
	}
	s.triggerAIJobCancel(job.ID)
	return &saved, nil
}
func scenarioGenerationErrorMessage(err error, meta ai.CallMeta) string {
	if meta.ErrorType == "timeout" || errors.Is(err, context.DeadlineExceeded) {
		return "模型响应超时，请稍后重试或检查当前模型服务状态。"
	}
	if meta.ErrorType == "auth" {
		return "模型鉴权失败，请检查 API Key 或模型服务配置。"
	}
	if meta.ErrorType == "rate_limit" {
		return "模型服务限流，请稍后重试。"
	}
	if meta.ErrorType == "validation" {
		return "模型返回结构未通过校验，请重新生成题目。"
	}
	return "题目生成失败，请稍后重试。"
}
func scenarioGenerationFailureStage(meta ai.CallMeta) string {
	if meta.ErrorType == "validation" || meta.ErrorType == "safety_blocked" {
		return "validating_output"
	}
	return "calling_model"
}
func (s *Server) auditScenarioGenerationFailed(userID, jobID string, job domain.AIJob, meta ai.CallMeta, req scenarioGenerationPayload, err error) {
	metadata := map[string]string{
		"job_id":        jobID,
		"provider":      firstNonEmpty(strings.TrimSpace(meta.Provider), strings.TrimSpace(job.Provider), "unknown"),
		"model":         firstNonEmpty(strings.TrimSpace(meta.Model), strings.TrimSpace(job.Model), "unknown"),
		"stage":         firstNonEmpty(strings.TrimSpace(job.Stage), scenarioGenerationFailureStage(meta)),
		"error_type":    firstNonEmpty(strings.TrimSpace(meta.ErrorType), "unknown"),
		"error_summary": truncateText(ai.Sanitize(err.Error()), 160),
		"difficulty":    req.Difficulty,
		"domain":        req.Domain,
		"scenario_type": req.ScenarioType,
		"store_mode":    s.storeMode(),
	}
	if raw := strings.TrimSpace(meta.RawOutput); raw != "" {
		metadata["raw_output_preview"] = truncateText(ai.Sanitize(raw), 500)
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      userID,
		Action:       "scenario.generate.failed",
		ResourceType: "ai_job",
		ResourceID:   jobID,
		Metadata:     metadata,
	})
}
func (s *Server) loadCancelableAIJob(jobID string) *domain.AIJob {
	job, ok := s.store.GetAIJob(jobID)
	if !ok {
		return nil
	}
	return job
}
func (s *Server) registerAIJobCancel(jobID string, cancel context.CancelFunc) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	s.jobStop[jobID] = cancel
}
func (s *Server) unregisterAIJobCancel(jobID string) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	delete(s.jobStop, jobID)
}
func (s *Server) triggerAIJobCancel(jobID string) {
	s.jobMu.Lock()
	cancel := s.jobStop[jobID]
	s.jobMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (s *Server) auditScenarioGenerationCompleted(userID, jobID string, question domain.ScenarioQuestion, meta ai.CallMeta, req scenarioGenerationPayload) {
	metadata := map[string]string{
		"provider":      meta.Provider,
		"model":         meta.Model,
		"validated":     strconv.FormatBool(meta.Validated),
		"fallback_used": strconv.FormatBool(meta.FallbackUsed),
		"difficulty":    question.Difficulty,
		"domain":        question.Domain,
		"scenario_type": question.ScenarioType,
		"store_mode":    s.storeMode(),
		"creator_role":  scenarioGenerationActorRole(userID, s.store),
	}
	fields := req.toAIConstraints().ActiveFields()
	metadata["has_constraints"] = strconv.FormatBool(len(fields) > 0)
	metadata["constraint_fields"] = strings.Join(fields, ",")
	metadata["duplicate_blocked"] = "false"
	if strings.TrimSpace(jobID) != "" {
		metadata["job_id"] = jobID
	}
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorID:      userID,
		Action:       "scenario.generate.completed",
		ResourceType: "scenario_question",
		ResourceID:   question.ID,
		Metadata:     metadata,
	})
}
func (s *Server) storeMode() string {
	if _, ok := s.store.(interface{ Ping(context.Context) error }); ok {
		return "postgres"
	}
	return "memory"
}
func scenarioGenerationActorRole(userID string, dataStore store.Store) string {
	if dataStore == nil || strings.TrimSpace(userID) == "" {
		return ""
	}
	user, ok := dataStore.GetUser(userID)
	if !ok || user == nil {
		return ""
	}
	return user.Role
}
func (s *Server) aiJobPayload(job *domain.AIJob, user *domain.User) map[string]interface{} {
	payload := map[string]interface{}{"job": job}
	if job != nil && job.ResultQuestionID != "" {
		if question, ok := s.store.GetScenario(job.ResultQuestionID); ok {
			payload["question_id"] = question.ID
			payload["question"] = scenarioView(question, user)
		}
	}
	return payload
}
func (s *Server) writeAIJobEvents(w http.ResponseWriter, r *http.Request, user *domain.User, jobID string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(job *domain.AIJob) bool {
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", mustJSON(s.aiJobPayload(job, user)))
		if flusher != nil {
			flusher.Flush()
		}
		return job.Status == domain.AIJobStatusCompleted || job.Status == domain.AIJobStatusFailed
	}

	if job, ok := s.store.GetAIJob(jobID); ok {
		if send(job) {
			return
		}
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
			job, ok := s.store.GetAIJob(jobID)
			if !ok || !canViewAIJob(job, user) {
				return
			}
			if send(job) {
				return
			}
		}
	}
}
