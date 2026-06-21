package httpapi

import (
	"context"
	"fmt"
	"net/http"
	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"sort"
	"strings"
	"time"
)

func (s *Server) handleInterviews(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 1 && parts[0] == "launchpad" && r.Method == http.MethodGet {
		writeOK(w, s.interviewLaunchpad())
		return
	}
	if len(parts) == 1 && parts[0] == "sessions" && r.Method == http.MethodPost {
		var req struct {
			Domain       string `json:"domain"`
			Difficulty   string `json:"difficulty"`
			QuestionType string `json:"question_type"`
		}
		if !decode(w, r, &req) {
			return
		}
		req.Domain = strings.TrimSpace(req.Domain)
		req.Difficulty = strings.TrimSpace(req.Difficulty)
		req.QuestionType = strings.TrimSpace(req.QuestionType)
		if req.Domain == "" || req.Difficulty == "" || req.QuestionType == "" {
			writeError(w, http.StatusBadRequest, "domain, difficulty and question_type are required")
			return
		}
		question, ok := s.store.FindInterviewQuestion(req.Domain, req.Difficulty, req.QuestionType)
		if !ok {
			writeError(w, http.StatusNotFound, "interview question not found")
			return
		}
		session := s.store.CreateInterviewSession(user.ID, question)
		writeOK(w, map[string]interface{}{
			"session_id": session.ID,
			"status":     session.Status,
			"question":   interviewQuestionView(question, user),
			"session":    session,
		})
		return
	}
	if len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodGet {
		sessionID := parts[1]
		session, ok := s.store.GetInterviewSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		question, ok := s.store.GetInterviewQuestion(session.QuestionID)
		if !ok {
			writeError(w, http.StatusNotFound, "interview question not found")
			return
		}
		hydrateInterviewSubmissionAssets(s.store, session)
		writeOK(w, map[string]interface{}{
			"session":  session,
			"question": interviewQuestionView(question, user),
		})
		return
	}
	if len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodDelete {
		sessionID := parts[1]
		session, ok := s.store.GetInterviewSession(sessionID)
		if !ok || session.UserID != user.ID {
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		if !s.store.DeleteInterviewSession(sessionID) {
			writeError(w, http.StatusNotFound, "interview session not found")
			return
		}
		writeOK(w, map[string]interface{}{
			"deleted": true,
			"id":      sessionID,
		})
		return
	}
	if len(parts) >= 3 && parts[0] == "sessions" {
		sessionID := parts[1]
		action := parts[2]
		if action == "submit" && r.Method == http.MethodPost {
			s.handleInterviewSubmission(w, r, user, sessionID)
			return
		}
		if action == "voice" && r.Method == http.MethodPost {
			s.handleInterviewVoice(w, r, user, sessionID)
			return
		}
		if len(parts) == 4 && action == "followup" && parts[3] == "answer" && r.Method == http.MethodPost {
			s.handleInterviewSubmission(w, r, user, sessionID)
			return
		}
		if action == "report" && r.Method == http.MethodGet {
			session, ok := s.store.GetInterviewSession(sessionID)
			if !ok || session.UserID != user.ID {
				writeError(w, http.StatusNotFound, "interview session not found")
				return
			}
			question, _ := s.store.GetInterviewQuestion(session.QuestionID)
			hydrateInterviewSubmissionAssets(s.store, session)
			writeOK(w, map[string]interface{}{
				"session":      session,
				"question":     interviewQuestionView(question, user),
				"radar_data":   radarData(session),
				"final_score":  session.FinalScore,
				"final_report": session.FinalReport,
			})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

type interviewLaunchpadTrack struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Domain              string   `json:"domain"`
	DomainLabel         string   `json:"domain_label"`
	Category            string   `json:"category"`
	Difficulty          string   `json:"difficulty"`
	QuestionType        string   `json:"question_type"`
	QuestionRole        string   `json:"question_role"`
	Summary             string   `json:"summary"`
	Details             []string `json:"details"`
	PublishedCount      int      `json:"published_count"`
	IndexedCount        int      `json:"indexed_count"`
	AvailabilityState   string   `json:"availability_state"`
	UnavailableReason   string   `json:"unavailable_reason,omitempty"`
	VectorStatusSummary string   `json:"vector_status_summary"`
}

type interviewLaunchpadDomain struct {
	Value          string `json:"value"`
	Label          string `json:"label"`
	Group          string `json:"group"`
	Note           string `json:"note"`
	OpenTrackCount int    `json:"open_track_count"`
}

func (s *Server) interviewLaunchpad() map[string]interface{} {
	tracks := []interviewLaunchpadTrack{}
	domainCounts := map[string]int{}
	difficulties := []string{}
	questionTypes := []string{}
	for _, seed := range interviewLaunchpadSeeds() {
		question, ok := s.store.FindInterviewQuestion(seed.Domain, seed.Difficulty, seed.QuestionType)
		if !ok {
			continue
		}
		track := interviewLaunchpadTrack{
			ID:                  seed.ID,
			Title:               seed.Title,
			Domain:              seed.Domain,
			DomainLabel:         seed.DomainLabel,
			Category:            seed.Domain,
			Difficulty:          seed.Difficulty,
			QuestionType:        seed.QuestionType,
			QuestionRole:        seed.QuestionRole,
			Summary:             firstNonEmpty(seed.Summary, question.Description),
			Details:             append([]string{}, seed.Details...),
			PublishedCount:      1,
			IndexedCount:        0,
			AvailabilityState:   "available",
			VectorStatusSummary: "compatibility_seed",
		}
		tracks = append(tracks, track)
		domainCounts[seed.Domain]++
		difficulties = append(difficulties, seed.Difficulty)
		questionTypes = append(questionTypes, seed.QuestionType)
	}
	domains := make([]interviewLaunchpadDomain, 0, len(domainCounts))
	for _, seed := range interviewLaunchpadDomainSeeds() {
		count := domainCounts[seed.Value]
		if count == 0 {
			continue
		}
		seed.OpenTrackCount = count
		domains = append(domains, seed)
	}
	return map[string]interface{}{
		"summary": map[string]interface{}{
			"open_track_count":     len(tracks),
			"published_atom_count": 0,
			"indexed_atom_count":   0,
			"fallback_mode":        true,
			"message":              "当前使用兼容题库轨道；索引增强未就绪时仍可完成开场训练。",
		},
		"domains":     domains,
		"open_tracks": tracks,
		"coverage": map[string]interface{}{
			"domains":        uniqueStrings(keysFromCounts(domainCounts)),
			"difficulties":   uniqueStrings(difficulties),
			"question_types": uniqueStrings(questionTypes),
		},
		"fallback_mode": true,
	}
}

type interviewLaunchpadSeed struct {
	ID           string
	Title        string
	Domain       string
	DomainLabel  string
	Difficulty   string
	QuestionType string
	QuestionRole string
	Summary      string
	Details      []string
}

func interviewLaunchpadSeeds() []interviewLaunchpadSeed {
	return []interviewLaunchpadSeed{
		{ID: "java-l2-principle", Title: "Java L2", Domain: "java", DomainLabel: "Java", Difficulty: "L2", QuestionType: "principle", QuestionRole: "opening", Summary: "面向初级岗位的 Java 基础语法、集合和面向对象问答。", Details: []string{"原理问答", "基础表达", "适合校招和 0-1 年经验"}},
		{ID: "java-l3-scenario", Title: "Java L3", Domain: "java", DomainLabel: "Java", Difficulty: "L3", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "面向进阶岗位的对象创建、异常处理和并发问题排查。", Details: []string{"情景分析", "并发基础", "适合校招后与 1 年左右经验"}},
		{ID: "database-l2-principle", Title: "数据库 L2", Domain: "database", DomainLabel: "数据库", Difficulty: "L2", QuestionType: "principle", QuestionRole: "opening", Summary: "面向初级岗位的索引、事务和表结构基础问答。", Details: []string{"原理问答", "事务基础", "适合 0-1 年经验"}},
		{ID: "database-l3-scenario", Title: "数据库 L3", Domain: "database", DomainLabel: "数据库", Difficulty: "L3", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "面向进阶岗位的慢查询、索引和回滚方案排查。", Details: []string{"情景分析", "慢查询定位", "回滚方案"}},
		{ID: "cache-l2-principle", Title: "缓存 L2", Domain: "cache", DomainLabel: "缓存", Difficulty: "L2", QuestionType: "principle", QuestionRole: "opening", Summary: "面向初级岗位的缓存命中、过期和基础一致性问答。", Details: []string{"原理问答", "缓存基础", "适合 0-1 年经验"}},
		{ID: "cache-l3-scenario", Title: "缓存 L3", Domain: "cache", DomainLabel: "缓存", Difficulty: "L3", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "面向进阶岗位的缓存击穿、穿透、雪崩与一致性排查。", Details: []string{"情景分析", "缓存治理", "热点流量"}},
		{ID: "ai-llm-l2-principle", Title: "AI / LLM L2", Domain: "ai_llm", DomainLabel: "AI / LLM", Difficulty: "L2", QuestionType: "principle", QuestionRole: "opening", Summary: "面向初级岗位的提示词、RAG 和模型使用基础问答。", Details: []string{"原理问答", "RAG 基础", "适合 0-1 年经验"}},
		{ID: "ai-llm-l3-scenario", Title: "AI / LLM L3", Domain: "ai_llm", DomainLabel: "AI / LLM", Difficulty: "L3", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "面向进阶岗位的 RAG 链路、提示词稳定性与模型应用治理分析。", Details: []string{"情景分析", "RAG 链路", "应用治理"}},
	}
}

func interviewLaunchpadDomainSeeds() []interviewLaunchpadDomain {
	return []interviewLaunchpadDomain{
		{Value: "java", Label: "Java", Group: "首期开放", Note: "L2 / L3 训练入口"},
		{Value: "database", Label: "数据库", Group: "首期开放", Note: "L2 / L3 训练入口"},
		{Value: "cache", Label: "缓存", Group: "首期开放", Note: "L2 / L3 训练入口"},
		{Value: "ai_llm", Label: "AI / LLM", Group: "首期开放", Note: "L2 / L3 训练入口"},
	}
}

func keysFromCounts(values map[string]int) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (s *Server) handleInterviewSubmission(w http.ResponseWriter, r *http.Request, user *domain.User, sessionID string) {
	if !s.allowAI(w, r, user, "interview-feedback", 30) {
		return
	}
	var req struct {
		Content             string `json:"content"`
		Type                string `json:"type"`
		Source              string `json:"source"`
		AssetID             string `json:"asset_id"`
		Transcript          string `json:"transcript"`
		DurationSeconds     int    `json:"duration_seconds"`
		ConfirmedTranscript bool   `json:"confirmed_transcript"`
	}
	if !decode(w, r, &req) {
		return
	}
	var writer *sseWriter
	if wantsSSE(r) {
		writer = newSSEWriter(w)
	}
	if req.Type == "" {
		req.Type = "text"
	}
	if req.Type == "voice" && strings.TrimSpace(req.Content) == "" {
		req.Content = req.Transcript
	}
	if strings.TrimSpace(req.Content) == "" {
		if writer != nil {
			writer.fail("content is required")
			return
		}
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	session, ok := s.store.GetInterviewSession(sessionID)
	if !ok || session.UserID != user.ID {
		if writer != nil {
			writer.fail("interview session not found")
			return
		}
		writeError(w, http.StatusNotFound, "interview session not found")
		return
	}
	if !interviewSessionAcceptsSubmission(session) {
		if writer != nil {
			writer.fail("interview session is already completed")
			return
		}
		writeError(w, http.StatusConflict, "interview session is already completed")
		return
	}
	question, ok := s.store.GetInterviewQuestion(session.QuestionID)
	if !ok {
		if writer != nil {
			writer.fail("interview question not found")
			return
		}
		writeError(w, http.StatusNotFound, "interview question not found")
		return
	}
	round := session.CurrentRound
	assetURL := ""
	var assetSnapshot *domain.Asset
	source := strings.TrimSpace(req.Source)
	transcript := strings.TrimSpace(req.Transcript)
	var voiceQuality *domain.VoiceQualityResult
	if req.AssetID != "" {
		asset, ok := s.store.GetAsset(req.AssetID)
		if !ok || asset.UserID != user.ID {
			if writer != nil {
				writer.fail("asset not found")
				return
			}
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		normalizedAsset := normalizeAssetURLs(*asset)
		assetURL = normalizedAsset.ContentURL
		assetSnapshot = &normalizedAsset
		if req.Type == "voice" {
			if !req.ConfirmedTranscript {
				if writer != nil {
					writer.fail("please confirm transcript before scoring")
					return
				}
				writeError(w, http.StatusBadRequest, "please confirm transcript before scoring")
				return
			}
			if err := validateVoiceAsset(asset.Filename, asset.MimeType, asset.Size); err != nil {
				if writer != nil {
					writer.fail(err.Error())
					return
				}
				writeAssetValidationError(w, err)
				return
			}
			validation := validateInterviewAnswer(question, req.Content, transcript, asset, 0.9)
			validation.Quality.TranscriptSuggestions = detectInterviewTermSuggestions(question, transcript)
			if len(validation.Quality.TranscriptSuggestions) > 0 && validation.Quality.Status == "draft_ready" {
				validation.Quality.Status = "needs_review"
				validation.Quality.Reasons = append(validation.Quality.Reasons, "检测到可能需要人工确认的技术术语转写")
			}
			if !validation.Valid {
				s.audit(r, user, "interview.voice_rejected", "interview_session", session.ID, map[string]string{
					"asset_id": req.AssetID,
					"reason":   validation.Message,
				})
				if writer != nil {
					writer.fail(validation.Message)
					return
				}
				writeInterviewValidationError(w, validation)
				return
			}
			voiceQuality = &validation.Quality
			if source == "" {
				source = inferSubmissionSource(req.Content, transcript)
			}
			if source == "text" || source == "voice_edited" {
				req.Type = "text"
			}
		}
	}
	if source == "" {
		source = "text"
	}
	submission := domain.InterviewSubmission{
		Round:           round,
		Content:         req.Content,
		Type:            req.Type,
		Source:          source,
		AssetID:         strings.TrimSpace(req.AssetID),
		AssetURL:        assetURL,
		Asset:           assetSnapshot,
		Transcript:      transcript,
		DurationSeconds: req.DurationSeconds,
		VoiceQuality:    voiceQuality,
		SubmittedAt:     time.Now(),
	}
	if decision := evaluateIrrelevantInterviewAnswer(question, req.Content, irrelevantInterviewSubmissionCount(session)); decision.Irrelevant {
		submission.QualityFlag = "irrelevant"
		evaluation := irrelevantInterviewEvaluation(round, decision)
		session.Submissions = append(session.Submissions, submission)
		session.Evaluations = append(session.Evaluations, evaluation)
		if decision.Final {
			now := time.Now()
			session.Status = "final_evaluated"
			session.FinalScore = 0
			session.FinalReport = "继续沉淀"
			session.FollowUpQuestion = ""
			session.EndedAt = &now
		} else {
			session.Status = fmt.Sprintf("follow_up_%d_presented", round)
			session.FollowUpQuestion = evaluation.FollowUpQuestion
			session.CurrentRound = round
		}
		s.store.SaveInterviewSession(session)
		s.audit(r, user, "interview.irrelevant_answer", "interview_session", session.ID, map[string]string{
			"attempt": fmt.Sprintf("%d", decision.Attempt),
			"final":   fmt.Sprintf("%t", decision.Final),
		})
		payload := map[string]interface{}{
			"evaluation":     evaluation,
			"session_status": session.Status,
			"session":        session,
		}
		if writer != nil {
			writer.stage("agent_intent", decision.Message)
			writer.deltaDisplay(decision.Message)
			writer.stage("completed", "本轮 Agent 面试完成")
			writer.finish(payload)
			return
		}
		writeOK(w, payload)
		return
	}
	if writer != nil {
		writer.stage("received", "已收到回答，正在准备评分")
	}
	interviewAgent := agentruntime.NewInterviewAgent(agentruntime.InterviewConfig{
		Feedback: func(ctx context.Context, feedbackReq ai.InterviewFeedbackRequest, _ func(string)) (ai.InterviewFeedback, ai.CallMeta, error) {
			return s.llmRouter().GenerateInterviewFeedbackStream(ctx, feedbackReq, nil)
		},
	})
	agentResult, agentErr := interviewAgent.Run(r.Context(), agentruntime.InterviewRequest{
		Session:  session,
		Question: question,
		Answer:   req.Content,
		OnStage: func(step, message string) {
			if writer != nil {
				writer.stage(step, message)
			}
		},
	})
	if agentErr != nil {
		s.auditInterviewAgentRun(r, user, session.ID, agentResult.Trace, agentResult, "failed", agentErr)
		if writer != nil {
			writer.fail("interview agent failed")
			return
		}
		writeError(w, http.StatusInternalServerError, "interview agent failed")
		return
	}
	evaluation := agentResult.Evaluation
	feedback := agentResult.Feedback
	needReport := agentResult.NeedReport
	if needReport && strings.TrimSpace(agentResult.FinalReport) != "" {
		session.FinalReport = agentResult.FinalReport
	}
	if writer != nil {
		streamInterviewFeedbackDisplay(writer, feedback, evaluation, needReport)
	}
	if writer != nil {
		writer.stage("saving", "正在整理评分结果")
	}
	session.Submissions = append(session.Submissions, submission)
	session.Evaluations = append(session.Evaluations, evaluation)
	if evaluation.FollowUpTriggered && round < session.MaxRounds {
		session.Status = fmt.Sprintf("follow_up_%d_presented", round)
		session.FollowUpQuestion = evaluation.FollowUpQuestion
		session.CurrentRound = round + 1
	} else {
		now := time.Now()
		session.Status = "final_evaluated"
		session.FinalScore = evaluation.TotalScore
		if strings.TrimSpace(session.FinalReport) == "" {
			session.FinalReport = ai.DefaultInterviewReport(evaluation)
		}
		session.EndedAt = &now
		s.store.RecordInterviewScore(user.ID, question.Domain, evaluation.TotalScore)
	}
	s.store.SaveInterviewSession(session)
	s.auditInterviewAgentRun(r, user, session.ID, agentResult.Trace, agentResult, "completed", nil)
	s.audit(r, user, "interview.submit", "interview_session", session.ID, map[string]string{"type": req.Type, "status": session.Status})
	payload := map[string]interface{}{
		"evaluation":     evaluation,
		"session_status": session.Status,
		"session":        session,
	}
	if writer != nil {
		writer.stage("completed", "本轮 Agent 面试完成")
		writer.finish(payload)
		return
	}
	writeOK(w, payload)
}
func interviewSessionAcceptsSubmission(session *domain.InterviewSession) bool {
	if session == nil {
		return false
	}
	status := strings.TrimSpace(session.Status)
	return status == "question_presented" ||
		status == "active" ||
		(strings.HasPrefix(status, "follow_up_") && strings.HasSuffix(status, "_presented"))
}
func (s *Server) handleInterviewVoice(w http.ResponseWriter, r *http.Request, user *domain.User, sessionID string) {
	var req struct {
		AssetID         string `json:"asset_id"`
		Transcript      string `json:"transcript"`
		DurationSeconds int    `json:"duration_seconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	session, ok := s.store.GetInterviewSession(sessionID)
	if !ok || session.UserID != user.ID {
		writeError(w, http.StatusNotFound, "interview session not found")
		return
	}
	asset, ok := s.store.GetAsset(req.AssetID)
	if !ok || asset.UserID != user.ID {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	normalizedAsset := normalizeAssetURLs(*asset)
	if err := validateVoiceAsset(asset.Filename, asset.MimeType, asset.Size); err != nil {
		writeAssetValidationError(w, err)
		return
	}
	question, ok := s.store.GetInterviewQuestion(session.QuestionID)
	if !ok {
		writeError(w, http.StatusNotFound, "interview question not found")
		return
	}
	sttResult, err := s.stt.Transcribe(r.Context(), STTRequest{
		Asset:    asset,
		Session:  session,
		Seed:     req.Transcript,
		Language: interviewSTTLanguageHint(question),
		Prompt:   buildInterviewSTTPrompt(question),
	})
	if err != nil {
		s.audit(r, user, "interview.voice_transcript_failed", "interview_session", session.ID, map[string]string{
			"asset_id": asset.ID,
			"error":    truncateText(err.Error(), 240),
		})
		writeSTTError(w, err)
		return
	}
	durationSeconds := req.DurationSeconds
	if durationSeconds == 0 {
		durationSeconds = sttResult.DurationSeconds
	}
	validation := validateInterviewAnswer(question, sttResult.Transcript, sttResult.Transcript, asset, sttResult.Confidence)
	if sttResult.DetectedLanguage != "" {
		validation.Quality.DetectedLanguage = sttResult.DetectedLanguage
	}
	if sttResult.Confidence > 0 {
		validation.Quality.STTConfidence = sttResult.Confidence
	}
	validation.Quality.TranscriptSuggestions = detectInterviewTermSuggestions(question, sttResult.Transcript)
	if len(validation.Quality.TranscriptSuggestions) > 0 && validation.Quality.Status == "draft_ready" {
		validation.Quality.Status = "needs_review"
		validation.Quality.Reasons = append(validation.Quality.Reasons, "检测到可能需要人工确认的技术术语转写")
	}
	if !validation.Valid {
		s.audit(r, user, "interview.voice_rejected", "interview_session", session.ID, map[string]string{
			"asset_id": asset.ID,
			"reason":   validation.Message,
			"stage":    "transcribe",
		})
	}
	status := validation.Quality.Status
	if status == "" {
		status = "draft_ready"
	}
	s.audit(r, user, "interview.voice_transcript", "interview_session", session.ID, map[string]string{"asset_id": asset.ID, "status": status})
	writeOK(w, map[string]interface{}{
		"asset":            normalizedAsset,
		"transcript":       sttResult.Transcript,
		"duration_seconds": durationSeconds,
		"status":           status,
		"quality":          validation.Quality,
	})
}
func evaluateInterview(question *domain.InterviewQuestion, answer string, round, maxRounds int) domain.InterviewEvaluation {
	return agentruntime.EvaluateInterview(question, answer, round, maxRounds)
}
func voiceTranscriptDraft(asset *domain.Asset, session *domain.InterviewSession) string {
	filename := "语音答案"
	if asset != nil && strings.TrimSpace(asset.Filename) != "" {
		filename = asset.Filename
	}
	round := 1
	if session != nil && session.CurrentRound > 0 {
		round = session.CurrentRound
	}
	return fmt.Sprintf("第 %d 轮 %s 转写草稿：我会先说明定位路径，再补充关键命令、验证指标、修复方案和回滚策略。", round, filename)
}

type irrelevantInterviewDecision struct {
	Irrelevant bool
	Final      bool
	Attempt    int
	Message    string
}

func evaluateIrrelevantInterviewAnswer(question *domain.InterviewQuestion, answer string, previousAttempts int) irrelevantInterviewDecision {
	trimmed := strings.TrimSpace(answer)
	if len([]rune(trimmed)) < 80 {
		return irrelevantInterviewDecision{}
	}
	relevance := interviewTopicRelevance(question, trimmed)
	hits := len(interviewKeywordHits(question, trimmed))
	if relevance >= 25 || hits > 0 {
		return irrelevantInterviewDecision{}
	}
	attempt := previousAttempts + 1
	decision := irrelevantInterviewDecision{
		Irrelevant: true,
		Attempt:    attempt,
		Message:    "请认真回答面试问题，围绕题目说明你的定位路径、关键命令、修复方案和回滚考虑。",
	}
	if attempt >= 4 {
		decision.Final = true
		decision.Message = "面试官认为你还没有准备好，请先继续沉淀，再重新开始本场面试。"
	}
	return decision
}
func irrelevantInterviewSubmissionCount(session *domain.InterviewSession) int {
	if session == nil {
		return 0
	}
	count := 0
	for _, submission := range session.Submissions {
		if submission.QualityFlag == "irrelevant" {
			count++
		}
	}
	return count
}
func irrelevantInterviewEvaluation(round int, decision irrelevantInterviewDecision) domain.InterviewEvaluation {
	evaluation := domain.InterviewEvaluation{
		Round:           round,
		TotalScore:      0,
		DimensionScores: map[string]int{},
		IsPassed:        false,
		Highlights:      []string{},
		Deficiencies:    []string{decision.Message},
		CreatedAt:       time.Now(),
	}
	if decision.Final {
		evaluation.FollowUpTriggered = false
		return evaluation
	}
	evaluation.FollowUpTriggered = true
	evaluation.FollowUpType = "guidance"
	evaluation.FollowUpQuestion = decision.Message
	return evaluation
}
func radarData(session *domain.InterviewSession) []map[string]interface{} {
	if session == nil || len(session.Evaluations) == 0 {
		return []map[string]interface{}{}
	}
	last := session.Evaluations[len(session.Evaluations)-1]
	data := make([]map[string]interface{}, 0, len(last.DimensionScores))
	for key, value := range last.DimensionScores {
		data = append(data, map[string]interface{}{"dimension": key, "score": value})
	}
	sort.Slice(data, func(i, j int) bool {
		return fmt.Sprint(data[i]["dimension"]) < fmt.Sprint(data[j]["dimension"])
	})
	return data
}
