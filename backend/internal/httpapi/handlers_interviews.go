package httpapi

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	agentruntime "situational-teaching/backend/internal/agent"
	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
	"sort"
	"strings"
	"time"
	"unicode"
)

func (s *Server) handleInterviews(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 1 && parts[0] == "launchpad" && r.Method == http.MethodGet {
		writeOK(w, s.interviewLaunchpad(user))
		return
	}
	if len(parts) == 1 && parts[0] == "sessions" && r.Method == http.MethodPost {
		var req struct {
			Mode            string   `json:"mode"`
			QuestionID      string   `json:"question_id"`
			ResumeIDs       []string `json:"resume_ids"`
			Domain          string   `json:"domain"`
			Difficulty      string   `json:"difficulty"`
			QuestionType    string   `json:"question_type"`
			DifficultyLevel string   `json:"difficulty_level"`
			FocusAreas      []string `json:"focus_areas"`
			SetupNotes      string   `json:"setup_notes"`
			MaxRounds       int      `json:"max_rounds"`
			SmartClose      *bool    `json:"smart_close"`
		}
		if !decode(w, r, &req) {
			return
		}
		req.Mode = firstNonEmpty(strings.TrimSpace(req.Mode), "free")
		req.QuestionID = strings.TrimSpace(req.QuestionID)
		req.Domain = strings.TrimSpace(req.Domain)
		req.Difficulty = strings.TrimSpace(req.Difficulty)
		req.QuestionType = strings.TrimSpace(req.QuestionType)
		req.DifficultyLevel = strings.TrimSpace(req.DifficultyLevel)
		req.SetupNotes = truncateText(req.SetupNotes, 2000)
		maxRounds, err := normalizeInterviewMaxRounds(req.MaxRounds)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		focusAreas, err := normalizeInterviewFocusAreas(req.FocusAreas)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var question *domain.InterviewQuestion
		var questionSnapshot domain.InterviewQuestionSnapshot
		resumeDocumentIDs := []string{}
		candidateContext := ""
		ok := false
		switch req.Mode {
		case "resume_deep_dive":
			var contextErr error
			candidateContext, resumeDocumentIDs, contextErr = resumeInterviewContext(user.Profile, req.ResumeIDs)
			if contextErr != nil {
				writeError(w, http.StatusBadRequest, contextErr.Error())
				return
			}
			question = resumeInterviewOpeningQuestion(candidateContext)
			questionSnapshot = interviewQuestionSnapshotFromQuestion(question)
			ok = true
		case "free":
			if req.QuestionID != "" {
				question, questionSnapshot, ok = s.selectInterviewOpeningQuestionByID(req.QuestionID)
			} else if req.Domain != "" && req.Difficulty != "" && req.QuestionType != "" {
				question, questionSnapshot, ok = s.selectInterviewOpeningQuestion(req.Domain, req.Difficulty, req.QuestionType)
			} else {
				writeError(w, http.StatusBadRequest, "domain, difficulty and question_type are required")
				return
			}
		case "role":
			if req.Domain == "" || req.Difficulty == "" || req.QuestionType == "" {
				writeError(w, http.StatusBadRequest, "domain, difficulty and question_type are required")
				return
			}
			question, questionSnapshot, ok = s.selectInterviewOpeningQuestion(req.Domain, req.Difficulty, req.QuestionType)
		default:
			writeError(w, http.StatusBadRequest, "interview mode is invalid")
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "interview question not found")
			return
		}
		session := s.store.CreateInterviewSession(user.ID, question)
		session.Mode = req.Mode
		session.ResumeDocumentIDs = resumeDocumentIDs
		session.CandidateContext = candidateContext
		session.MaxRounds = maxRounds
		session.SmartClose = req.SmartClose == nil || *req.SmartClose
		session.DifficultyLevel = req.DifficultyLevel
		session.FocusAreas = focusAreas
		session.SetupNotes = req.SetupNotes
		session.QuestionSnapshot = questionSnapshot
		s.store.SaveInterviewSession(session)
		writeOK(w, map[string]interface{}{
			"session_id": session.ID,
			"status":     session.Status,
			"question":   interviewQuestionView(question, user),
			"session":    interviewSessionView(session),
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
		question, ok := s.resolveInterviewSessionQuestion(session)
		if !ok {
			writeError(w, http.StatusNotFound, "interview question not found")
			return
		}
		hydrateInterviewSubmissionAssets(s.store, session)
		writeOK(w, map[string]interface{}{
			"session":  interviewSessionView(session),
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
			question, _ := s.resolveInterviewSessionQuestion(session)
			hydrateInterviewSubmissionAssets(s.store, session)
			writeOK(w, map[string]interface{}{
				"session":           interviewSessionView(session),
				"question":          interviewQuestionView(question, user),
				"radar_data":        radarData(session),
				"final_score":       session.FinalScore,
				"final_report":      session.FinalReport,
				"retrieval_summary": buildInterviewReportRetrievalSummary(session),
			})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

func resumeInterviewContext(profile domain.UserProfile, requestedIDs []string) (string, []string, error) {
	documents := resumeDocumentsForProfile(profile)
	documentByID := map[string]domain.ResumeDocument{}
	for _, document := range documents {
		documentByID[document.ID] = document
	}
	selectedIDs := uniqueStrings(requestedIDs)
	if len(selectedIDs) == 0 {
		return "", nil, fmt.Errorf("请至少选择一份可用简历")
	}
	lines := []string{}
	seenLines := map[string]bool{}
	for _, id := range selectedIDs {
		document, ok := documentByID[id]
		if !ok {
			return "", nil, fmt.Errorf("所选简历不存在或无权访问")
		}
		if document.QualityStatus != "passed" {
			return "", nil, fmt.Errorf("简历「%s」未通过内容检查，请先完善个人档案", document.Name)
		}
		for _, line := range strings.Split(strings.ReplaceAll(document.ExtractedText, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			key := strings.ToLower(line)
			if line == "" || seenLines[key] {
				continue
			}
			seenLines[key] = true
			lines = append(lines, line)
		}
	}
	contextText := strings.Join(lines, "\n")
	_, resumeSummary, projectSummary := extractStructuredResumeFields(contextText)
	if ok, reason := resumeContentQuality(resumeSummary, projectSummary); !ok {
		return "", nil, fmt.Errorf("%s，请先完善个人档案", reason)
	}
	return contextText, selectedIDs, nil
}

func resumeInterviewOpeningQuestion(candidateContext string) *domain.InterviewQuestion {
	project := resumeInterviewProjectAnchor(candidateContext)
	hasResponsibility := strings.Contains(candidateContext, "负责") || strings.Contains(candidateContext, "职责") || strings.Contains(strings.ToLower(candidateContext), "responsible")
	description := fmt.Sprintf("请先说明你在「%s」中实际负责的部分。", project)
	if hasResponsibility {
		description = fmt.Sprintf("在「%s」中，你做过的哪项技术决策最关键？", project)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(candidateContext))
	return &domain.InterviewQuestion{
		ID:                   fmt.Sprintf("resume-deep-dive:%08x", hash.Sum32()),
		Title:                "简历深挖",
		Description:          description,
		Domain:               "resume",
		Difficulty:           "L3",
		QuestionType:         "resume_deep_dive",
		ReferenceKeywords:    []string{"个人职责", "技术决策", "结果复盘"},
		EvaluationDimensions: defaultInterviewEvaluationDimensions(),
		FollowUpStrategies:   []domain.FollowUpStrategy{},
		CreatedAt:            time.Now(),
	}
}

func resumeInterviewProjectAnchor(candidateContext string) string {
	_, _, projectSummary := extractStructuredResumeFields(candidateContext)
	if strings.TrimSpace(projectSummary) != "" {
		for _, line := range normalizeResumeLines(projectSummary) {
			parts := strings.FieldsFunc(line, func(r rune) bool {
				return r == '，' || r == ',' || r == '。' || r == ';' || r == '；'
			})
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				return truncateText(strings.TrimSpace(parts[0]), 28)
			}
		}
	}
	candidates := normalizeResumeLines(firstNonEmpty(projectSummary, candidateContext))
	for _, line := range candidates {
		cleaned := strings.TrimSpace(strings.TrimLeft(line, "#*-0123456789.、 "))
		if cleaned == "" {
			continue
		}
		if strings.Contains(cleaned, "项目") || strings.Contains(strings.ToLower(cleaned), "project") {
			return truncateText(cleaned, 28)
		}
	}
	return "最近一个项目"
}

type interviewLaunchpadTrack struct {
	ID                  string    `json:"id"`
	QuestionID          string    `json:"question_id"`
	StableCode          string    `json:"stable_code"`
	OpeningQuestion     string    `json:"opening_question"`
	Title               string    `json:"title"`
	Domain              string    `json:"domain"`
	DomainLabel         string    `json:"domain_label"`
	Category            string    `json:"category"`
	Difficulty          string    `json:"difficulty"`
	QuestionType        string    `json:"question_type"`
	QuestionRole        string    `json:"question_role"`
	Tags                []string  `json:"tags"`
	Summary             string    `json:"summary"`
	Details             []string  `json:"details"`
	PublishedCount      int       `json:"published_count"`
	IndexedCount        int       `json:"indexed_count"`
	AvailabilityState   string    `json:"availability_state"`
	UnavailableReason   string    `json:"unavailable_reason,omitempty"`
	VectorStatusSummary string    `json:"vector_status_summary"`
	LatestUpdatedAt     time.Time `json:"latest_updated_at,omitempty"`
	Practiced           bool      `json:"practiced"`
}

type interviewLaunchpadDomain struct {
	Value          string `json:"value"`
	Label          string `json:"label"`
	Group          string `json:"group"`
	Note           string `json:"note"`
	OpenTrackCount int    `json:"open_track_count"`
}

type interviewLaunchpadRecommendation struct {
	ID                  string   `json:"id"`
	QuestionID          string   `json:"question_id"`
	StableCode          string   `json:"stable_code"`
	OpeningQuestion     string   `json:"opening_question"`
	Title               string   `json:"title"`
	Domain              string   `json:"domain"`
	DomainLabel         string   `json:"domain_label"`
	Category            string   `json:"category"`
	Difficulty          string   `json:"difficulty"`
	QuestionType        string   `json:"question_type"`
	QuestionRole        string   `json:"question_role"`
	Tags                []string `json:"tags"`
	Summary             string   `json:"summary"`
	Details             []string `json:"details"`
	PublishedCount      int      `json:"published_count"`
	IndexedCount        int      `json:"indexed_count"`
	AvailabilityState   string   `json:"availability_state"`
	VectorStatusSummary string   `json:"vector_status_summary"`
	Reason              string   `json:"reason"`
	SourceKind          string   `json:"source_kind"`
	Practiced           bool     `json:"practiced"`
}

type interviewLaunchpadRecentSession struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Domain        string     `json:"domain"`
	Difficulty    string     `json:"difficulty"`
	QuestionTitle string     `json:"question_title"`
	FinalScore    int        `json:"final_score,omitempty"`
	WeakDimension string     `json:"weak_dimension,omitempty"`
	WeakScore     int        `json:"weak_score,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	ActionPath    string     `json:"action_path"`
}

type interviewLaunchpadCoverageStats struct {
	TotalOpenTracks       int      `json:"total_open_tracks"`
	PracticedOpenTracks   int      `json:"practiced_open_tracks"`
	CoveragePercent       int      `json:"coverage_percent"`
	CompletedSessions     int      `json:"completed_sessions"`
	PracticedDomains      []string `json:"practiced_domains"`
	PracticedDifficulties []string `json:"practiced_difficulties"`
	SubjectCount          int      `json:"subject_count"`
	TopSubjects           []string `json:"top_subjects"`
	UncoveredTrackIDs     []string `json:"uncovered_track_ids"`
}

func (s *Server) interviewLaunchpad(user *domain.User) map[string]interface{} {
	atomTracks := s.interviewLaunchpadAtomTracks()
	// 正式原子轨道与兼容种子题合并：少量正式题不应覆盖既有可开场题。
	seedTracks := s.interviewLaunchpadSeedTracks()
	tracks := mergeInterviewLaunchpadTracks(atomTracks, seedTracks)
	if user != nil {
		markInterviewLaunchpadPractice(tracks, s.store.ListInterviewSessionsForUser(user.ID))
	}
	sortInterviewLaunchpadTracks(user, tracks)
	domainCounts := map[string]int{}
	difficulties := []string{}
	questionTypes := []string{}
	for _, track := range tracks {
		domainCounts[track.Domain]++
		difficulties = append(difficulties, track.Difficulty)
		questionTypes = append(questionTypes, track.QuestionType)
	}
	// 原子统计只来自正式题库轨道，避免兼容种子把 published/indexed 计数抬高。
	publishedCount := 0
	indexedCount := 0
	for _, track := range atomTracks {
		publishedCount += track.PublishedCount
		indexedCount += track.IndexedCount
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
	// 补充正式题库里但未进入种子域列表的领域（如临时导入的 java）。
	for domainName, count := range domainCounts {
		found := false
		for _, domainItem := range domains {
			if domainItem.Value == domainName {
				found = true
				break
			}
		}
		if found {
			continue
		}
		domains = append(domains, interviewLaunchpadDomain{
			Value:          domainName,
			Label:          interviewLaunchpadDomainLabel(domainName),
			Group:          "题库开放",
			Note:           fmt.Sprintf("%d 个训练入口", count),
			OpenTrackCount: count,
		})
	}
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].Value < domains[j].Value
	})
	recentSessions := s.interviewLaunchpadRecentSessions(user)
	// 仅当没有正式原子、完全依赖种子题时才标记兼容模式。
	fallbackMode := len(atomTracks) == 0
	return map[string]interface{}{
		"summary": map[string]interface{}{
			"open_track_count":     len(tracks),
			"published_atom_count": publishedCount,
			"indexed_atom_count":   indexedCount,
			"fallback_mode":        fallbackMode,
			"state":                interviewLaunchpadSummaryState(fallbackMode, len(tracks), publishedCount, indexedCount),
			"message":              interviewLaunchpadSummaryMessage(fallbackMode),
		},
		"domains":            domains,
		"open_tracks":        tracks,
		"recommended_tracks": s.recommendInterviewLaunchpadTracks(user, tracks, recentSessions),
		"recent_sessions":    recentSessions,
		"coverage_stats":     s.interviewLaunchpadCoverageStats(user, tracks),
		"coverage": map[string]interface{}{
			"domains":               uniqueStrings(keysFromCounts(domainCounts)),
			"difficulties":          uniqueStrings(difficulties),
			"question_types":        uniqueStrings(questionTypes),
			"question_roles":        uniqueStrings(trackQuestionRoles(tracks)),
			"vector_status_summary": uniqueStrings(trackVectorStatusSummaries(tracks)),
		},
		"fallback_mode": fallbackMode,
	}
}

func (s *Server) interviewLaunchpadCoverageStats(user *domain.User, tracks []interviewLaunchpadTrack) interviewLaunchpadCoverageStats {
	stats := interviewLaunchpadCoverageStats{
		TotalOpenTracks:       len(tracks),
		PracticedDomains:      []string{},
		PracticedDifficulties: []string{},
		TopSubjects:           []string{},
		UncoveredTrackIDs:     []string{},
	}
	if len(tracks) == 0 {
		return stats
	}

	trackIDByQuestion := map[string]string{}
	for _, track := range tracks {
		questionID := strings.TrimSpace(track.QuestionID)
		if questionID == "" {
			continue
		}
		trackIDByQuestion[questionID] = track.ID
	}
	if user == nil {
		for _, track := range tracks {
			stats.UncoveredTrackIDs = append(stats.UncoveredTrackIDs, track.ID)
		}
		return stats
	}

	practicedQuestionIDs := map[string]bool{}
	practicedDomains := []string{}
	practicedDifficulties := []string{}
	subjectCounts := map[string]int{}
	sessions := s.store.ListInterviewSessionsForUser(user.ID)
	for _, session := range sessions {
		if session.Status != "final_evaluated" {
			continue
		}
		stats.CompletedSessions++

		domainValue := strings.TrimSpace(session.QuestionSnapshot.Domain)
		difficultyValue := strings.TrimSpace(session.QuestionSnapshot.Difficulty)
		fallbackSubject := firstNonEmpty(session.QuestionSnapshot.Subject, session.QuestionSnapshot.Title)
		if question, ok := s.resolveInterviewSessionQuestion(&session); ok && question != nil {
			domainValue = firstNonEmpty(domainValue, question.Domain)
			difficultyValue = firstNonEmpty(difficultyValue, question.Difficulty)
			fallbackSubject = firstNonEmpty(fallbackSubject, question.Title)
		}
		if domainValue != "" {
			practicedDomains = append(practicedDomains, domainValue)
		}
		if difficultyValue != "" {
			practicedDifficulties = append(practicedDifficulties, difficultyValue)
		}

		questionID := session.QuestionID
		if strings.HasPrefix(questionID, "interview-knowledge:") {
			questionID = strings.TrimPrefix(questionID, "interview-knowledge:")
			if versionIndex := strings.LastIndex(questionID, ":v"); versionIndex > 0 {
				questionID = questionID[:versionIndex]
			}
		}
		if _, ok := trackIDByQuestion[questionID]; ok {
			practicedQuestionIDs[questionID] = true
		}

		reportSummary := buildInterviewReportRetrievalSummary(&session)
		if len(reportSummary.Coverage) > 0 {
			for _, item := range reportSummary.Coverage {
				subject := strings.TrimSpace(item.Subject)
				if subject == "" {
					continue
				}
				increment := item.RoundCount
				if increment <= 0 {
					increment = 1
				}
				subjectCounts[subject] += increment
			}
			continue
		}
		if len(reportSummary.Subjects) > 0 {
			for _, subject := range reportSummary.Subjects {
				subject = strings.TrimSpace(subject)
				if subject == "" {
					continue
				}
				subjectCounts[subject]++
			}
			continue
		}
		fallbackSubject = strings.TrimSpace(fallbackSubject)
		if fallbackSubject != "" {
			subjectCounts[fallbackSubject]++
		}
	}

	stats.PracticedOpenTracks = len(practicedQuestionIDs)
	stats.PracticedDomains = uniqueStrings(practicedDomains)
	stats.PracticedDifficulties = uniqueStrings(practicedDifficulties)
	stats.SubjectCount = len(subjectCounts)
	if stats.TotalOpenTracks > 0 {
		stats.CoveragePercent = (stats.PracticedOpenTracks*100 + stats.TotalOpenTracks/2) / stats.TotalOpenTracks
	}

	type subjectStat struct {
		Subject string
		Count   int
	}
	subjects := make([]subjectStat, 0, len(subjectCounts))
	for subject, count := range subjectCounts {
		subjects = append(subjects, subjectStat{Subject: subject, Count: count})
	}
	sort.Slice(subjects, func(i, j int) bool {
		if subjects[i].Count == subjects[j].Count {
			return subjects[i].Subject < subjects[j].Subject
		}
		return subjects[i].Count > subjects[j].Count
	})
	for _, item := range subjects {
		stats.TopSubjects = append(stats.TopSubjects, item.Subject)
		if len(stats.TopSubjects) == 5 {
			break
		}
	}

	for _, track := range tracks {
		if practicedQuestionIDs[track.QuestionID] {
			continue
		}
		stats.UncoveredTrackIDs = append(stats.UncoveredTrackIDs, track.ID)
	}
	return stats
}

func (s *Server) interviewLaunchpadRecentSessions(user *domain.User) []interviewLaunchpadRecentSession {
	if user == nil {
		return []interviewLaunchpadRecentSession{}
	}
	sessions := s.store.ListInterviewSessionsForUser(user.ID)
	items := make([]interviewLaunchpadRecentSession, 0, len(sessions))
	for _, session := range sessions {
		domainValue := session.QuestionSnapshot.Domain
		difficultyValue := session.QuestionSnapshot.Difficulty
		titleValue := session.QuestionSnapshot.Title
		if question, ok := s.resolveInterviewSessionQuestion(&session); ok && question != nil {
			domainValue = firstNonEmpty(domainValue, question.Domain)
			difficultyValue = firstNonEmpty(difficultyValue, question.Difficulty)
			titleValue = firstNonEmpty(titleValue, question.Title)
		}
		actionPath := fmt.Sprintf("/interviews/session/%s", session.ID)
		if session.Status == "final_evaluated" {
			actionPath = fmt.Sprintf("/interviews/session/%s/report", session.ID)
		}
		weakDimension, weakScore := interviewLaunchpadWeakDimensionSummary(session.Evaluations)
		items = append(items, interviewLaunchpadRecentSession{
			ID:            session.ID,
			Status:        session.Status,
			Domain:        domainValue,
			Difficulty:    difficultyValue,
			QuestionTitle: titleValue,
			FinalScore:    session.FinalScore,
			WeakDimension: weakDimension,
			WeakScore:     weakScore,
			StartedAt:     session.StartedAt,
			EndedAt:       session.EndedAt,
			ActionPath:    actionPath,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	if len(items) > 5 {
		return items[:5]
	}
	return items
}

func interviewLaunchpadSummaryState(fallbackMode bool, openTrackCount, publishedAtomCount, indexedAtomCount int) string {
	if fallbackMode {
		return "compatibility_fallback"
	}
	if openTrackCount == 0 {
		return "empty"
	}
	if publishedAtomCount > 0 && indexedAtomCount == 0 {
		return "retrieval_degraded"
	}
	if publishedAtomCount > 0 && indexedAtomCount < publishedAtomCount {
		return "retrieval_partial"
	}
	return "ready"
}

func interviewLaunchpadSummaryMessage(fallbackMode bool) string {
	if fallbackMode {
		return "当前使用兼容题库轨道；索引增强未就绪时仍可完成开场训练。"
	}
	return "当前训练轨道来自正式题库与可启动题；索引未命中的追问会自动回退规则链路。"
}

func (s *Server) recommendInterviewLaunchpadTracks(user *domain.User, tracks []interviewLaunchpadTrack, recentSessions []interviewLaunchpadRecentSession) []interviewLaunchpadRecommendation {
	items := make([]interviewLaunchpadRecommendation, 0, 4)
	seen := map[string]bool{}
	add := func(track interviewLaunchpadTrack, reason, sourceKind string) {
		if seen[track.ID] {
			return
		}
		seen[track.ID] = true
		items = append(items, interviewLaunchpadRecommendation{
			ID:                  track.ID,
			QuestionID:          track.QuestionID,
			StableCode:          track.StableCode,
			OpeningQuestion:     track.OpeningQuestion,
			Title:               track.Title,
			Domain:              track.Domain,
			DomainLabel:         track.DomainLabel,
			Category:            track.Category,
			Difficulty:          track.Difficulty,
			QuestionType:        track.QuestionType,
			QuestionRole:        track.QuestionRole,
			Tags:                append([]string{}, track.Tags...),
			Summary:             track.Summary,
			Details:             append([]string{}, track.Details...),
			PublishedCount:      track.PublishedCount,
			IndexedCount:        track.IndexedCount,
			AvailabilityState:   track.AvailabilityState,
			VectorStatusSummary: track.VectorStatusSummary,
			Reason:              reason,
			SourceKind:          sourceKind,
			Practiced:           track.Practiced,
		})
	}
	findTrack := func(domainValue, difficultyValue string) *interviewLaunchpadTrack {
		for i := range tracks {
			track := &tracks[i]
			if track.Domain == domainValue && track.Difficulty == difficultyValue {
				return track
			}
		}
		return nil
	}
	for _, session := range recentSessions {
		if session.Status == "final_evaluated" || session.Status == "invalidated" {
			continue
		}
		track := findTrack(session.Domain, session.Difficulty)
		if track == nil {
			continue
		}
		add(*track, fmt.Sprintf("你有一场未完成的 %s / %s 面试，可直接续练。", track.DomainLabel, track.Difficulty), "continue_session")
	}
	for _, session := range recentSessions {
		if session.Status != "final_evaluated" || session.WeakDimension == "" || session.WeakScore <= 0 || session.WeakScore >= 75 {
			continue
		}
		track := findTrack(session.Domain, session.Difficulty)
		if track == nil {
			continue
		}
		add(*track, fmt.Sprintf("你上次在 %s / %s 面试里 %s 得分仅 %d，建议优先继续补强。", track.DomainLabel, track.Difficulty, session.WeakDimension, session.WeakScore), "weak_dimension")
	}
	if habitualTrack, habitualCount := s.habitualInterviewLaunchpadTrack(user, tracks); habitualTrack != nil {
		add(*habitualTrack, fmt.Sprintf("你最近最常练的是 %s / %s（已出现 %d 次），适合沿着熟悉轨道继续验证稳定性。", habitualTrack.DomainLabel, habitualTrack.Difficulty, habitualCount), "habitual_track")
	}
	freshTracks := append([]interviewLaunchpadTrack{}, tracks...)
	sort.SliceStable(freshTracks, func(i, j int) bool {
		if freshTracks[i].LatestUpdatedAt.Equal(freshTracks[j].LatestUpdatedAt) {
			return freshTracks[i].ID < freshTracks[j].ID
		}
		return freshTracks[i].LatestUpdatedAt.After(freshTracks[j].LatestUpdatedAt)
	})
	for _, track := range freshTracks {
		if track.LatestUpdatedAt.IsZero() {
			continue
		}
		// 兼容种子不参与“最近更新”推荐，避免压过正式题库新内容。
		if track.VectorStatusSummary == "compatibility_seed" {
			continue
		}
		add(track, fmt.Sprintf("%s / %s 题库最近更新，适合趁热进入一轮训练验证掌握情况。", track.DomainLabel, track.Difficulty), "fresh_content")
	}
	if user != nil {
		for _, preferredDomain := range user.Profile.PreferredDomains {
			for _, track := range tracks {
				if track.Domain == preferredDomain {
					add(track, fmt.Sprintf("你的个人档案偏好包含 %s，建议优先补齐该方向训练。", track.DomainLabel), "preferred_domain")
				}
			}
		}
	}
	for _, track := range tracks {
		add(track, "这是当前正式开放的训练入口，可直接开始完整面试。", "default_open_track")
		if len(items) >= 4 {
			break
		}
	}
	if len(items) > 4 {
		return items[:4]
	}
	return items
}

func (s *Server) habitualInterviewLaunchpadTrack(user *domain.User, tracks []interviewLaunchpadTrack) (*interviewLaunchpadTrack, int) {
	if user == nil || len(tracks) == 0 {
		return nil, 0
	}
	trackByKey := map[string]*interviewLaunchpadTrack{}
	for i := range tracks {
		track := &tracks[i]
		key := interviewLaunchpadCoverageKey(track.Domain, track.Difficulty)
		if key == "" {
			continue
		}
		if _, exists := trackByKey[key]; !exists {
			trackByKey[key] = track
		}
	}
	if len(trackByKey) == 0 {
		return nil, 0
	}

	type usageStat struct {
		Count    int
		LatestAt time.Time
	}
	usageByKey := map[string]usageStat{}
	sessions := s.store.ListInterviewSessionsForUser(user.ID)
	for _, session := range sessions {
		if session.Status == "invalidated" {
			continue
		}
		domainValue := strings.TrimSpace(session.QuestionSnapshot.Domain)
		difficultyValue := strings.TrimSpace(session.QuestionSnapshot.Difficulty)
		if question, ok := s.resolveInterviewSessionQuestion(&session); ok && question != nil {
			domainValue = firstNonEmpty(domainValue, question.Domain)
			difficultyValue = firstNonEmpty(difficultyValue, question.Difficulty)
		}
		key := interviewLaunchpadCoverageKey(domainValue, difficultyValue)
		if key == "" {
			continue
		}
		if _, ok := trackByKey[key]; !ok {
			continue
		}
		item := usageByKey[key]
		item.Count++
		if session.StartedAt.After(item.LatestAt) {
			item.LatestAt = session.StartedAt
		}
		usageByKey[key] = item
	}

	bestKey := ""
	best := usageStat{}
	for key, item := range usageByKey {
		if item.Count < 2 {
			continue
		}
		if bestKey == "" || item.Count > best.Count || (item.Count == best.Count && item.LatestAt.After(best.LatestAt)) {
			bestKey = key
			best = item
		}
	}
	if bestKey == "" {
		return nil, 0
	}
	return trackByKey[bestKey], best.Count
}

func interviewLaunchpadWeakDimensionSummary(evaluations []domain.InterviewEvaluation) (string, int) {
	lowestLabel := ""
	lowestScore := 0
	for _, evaluation := range evaluations {
		for _, key := range interviewFocusAreaOrder {
			score, ok := evaluation.DimensionScores[key]
			if !ok {
				continue
			}
			if lowestLabel == "" || score < lowestScore {
				lowestLabel = interviewFocusAreaLabel(key)
				lowestScore = score
			}
		}
		for key, score := range evaluation.DimensionScores {
			if containsString(interviewFocusAreaOrder, key) {
				continue
			}
			if lowestLabel == "" || score < lowestScore {
				lowestLabel = interviewFocusAreaLabel(key)
				lowestScore = score
			}
		}
	}
	return lowestLabel, lowestScore
}

func interviewLaunchpadCoverageKey(domainValue, difficultyValue string) string {
	domainValue = strings.TrimSpace(domainValue)
	difficultyValue = strings.TrimSpace(difficultyValue)
	if domainValue == "" || difficultyValue == "" {
		return ""
	}
	return domainValue + ":" + difficultyValue
}

func trackQuestionRoles(tracks []interviewLaunchpadTrack) []string {
	items := make([]string, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, track.QuestionRole)
	}
	return items
}

func trackVectorStatusSummaries(tracks []interviewLaunchpadTrack) []string {
	items := make([]string, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, track.VectorStatusSummary)
	}
	return items
}

func (s *Server) interviewLaunchpadAtomTracks() []interviewLaunchpadTrack {
	atoms := s.store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{Status: "published"})
	tracks := make([]interviewLaunchpadTrack, 0, len(atoms))
	for _, atom := range atoms {
		if atom.QuestionRole != "opening" && atom.QuestionRole != "mixed" {
			continue
		}
		category := firstNonEmpty(atom.Category, atom.Domain)
		if category == "" || atom.Difficulty == "" {
			continue
		}
		questionType := normalizeLaunchpadQuestionType(atom.QuestionType)
		openingQuestion := strings.TrimSpace(atom.OpeningQuestion)
		if openingQuestion == "" {
			openingQuestion = firstNonEmpty(atom.Title, atom.Subject)
		}
		indexed := 0
		if atom.VectorStatus == "indexed" {
			indexed = 1
		}
		tracks = append(tracks, interviewLaunchpadTrack{
			ID:                  atom.ID,
			QuestionID:          atom.ID,
			StableCode:          firstNonEmpty(atom.StableCode, legacyInterviewStableCode(category, atom.ID)),
			OpeningQuestion:     openingQuestion,
			Title:               firstNonEmpty(atom.Title, openingQuestion),
			Domain:              category,
			DomainLabel:         interviewLaunchpadDomainLabel(category),
			Category:            category,
			Difficulty:          atom.Difficulty,
			QuestionType:        questionType,
			QuestionRole:        atom.QuestionRole,
			Tags:                uniqueStrings(atom.Tags),
			Summary:             openingQuestion,
			Details:             []string{},
			PublishedCount:      1,
			IndexedCount:        indexed,
			AvailabilityState:   "available",
			VectorStatusSummary: firstNonEmpty(atom.VectorStatus, "pending"),
			LatestUpdatedAt:     atom.UpdatedAt,
		})
	}
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].LatestUpdatedAt.Equal(tracks[j].LatestUpdatedAt) {
			return tracks[i].ID < tracks[j].ID
		}
		return tracks[i].LatestUpdatedAt.After(tracks[j].LatestUpdatedAt)
	})
	return tracks
}

func (s *Server) interviewLaunchpadSeedTracks() []interviewLaunchpadTrack {
	tracks := []interviewLaunchpadTrack{}
	for _, seed := range interviewLaunchpadSeeds() {
		question, ok := s.store.FindInterviewQuestion(seed.Domain, seed.Difficulty, seed.QuestionType)
		if !ok {
			continue
		}
		tracks = append(tracks, interviewLaunchpadTrack{
			ID:                  seed.ID,
			QuestionID:          question.ID,
			StableCode:          legacyInterviewStableCode(seed.Domain, question.ID),
			OpeningQuestion:     question.Description,
			Title:               seed.Title,
			Domain:              seed.Domain,
			DomainLabel:         seed.DomainLabel,
			Category:            seed.Domain,
			Difficulty:          seed.Difficulty,
			QuestionType:        normalizeLaunchpadQuestionType(seed.QuestionType),
			QuestionRole:        seed.QuestionRole,
			Tags:                []string{},
			Summary:             question.Description,
			Details:             append([]string{}, seed.Details...),
			PublishedCount:      1,
			IndexedCount:        0,
			AvailabilityState:   "available",
			VectorStatusSummary: "compatibility_seed",
			LatestUpdatedAt:     question.CreatedAt,
		})
	}
	return tracks
}

func mergeInterviewLaunchpadTracks(atomTracks, seedTracks []interviewLaunchpadTrack) []interviewLaunchpadTrack {
	seenQuestions := map[string]bool{}
	tracks := make([]interviewLaunchpadTrack, 0, len(atomTracks)+len(seedTracks))
	for _, track := range atomTracks {
		seenQuestions[track.QuestionID] = true
		tracks = append(tracks, track)
	}
	for _, track := range seedTracks {
		if track.QuestionID != "" && seenQuestions[track.QuestionID] {
			continue
		}
		seenQuestions[track.QuestionID] = true
		tracks = append(tracks, track)
	}
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].Domain == tracks[j].Domain {
			if tracks[i].Difficulty == tracks[j].Difficulty {
				return tracks[i].ID < tracks[j].ID
			}
			return tracks[i].Difficulty < tracks[j].Difficulty
		}
		return tracks[i].Domain < tracks[j].Domain
	})
	return tracks
}

func normalizeLaunchpadQuestionType(value string) string {
	switch strings.TrimSpace(value) {
	case "principle", "troubleshooting", "architecture", "behavioral":
		return strings.TrimSpace(value)
	case "scenario_analysis":
		return "troubleshooting"
	default:
		return "principle"
	}
}

func legacyInterviewStableCode(domainValue, questionID string) string {
	prefixes := map[string]string{
		"java": "JAVA", "database": "DB", "cache": "CACHE", "middleware": "MID",
		"system_design": "SYS", "frontend": "FE", "ai_llm": "AI", "hr_soft_skill": "HR",
		"network": "NET", "os": "OS", "security": "SEC", "devops": "DEVOPS",
	}
	prefix := prefixes[strings.TrimSpace(domainValue)]
	if prefix == "" {
		prefix = "Q"
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.TrimSpace(questionID)))
	return fmt.Sprintf("%s-%09d", prefix, hash.Sum32())
}

func markInterviewLaunchpadPractice(tracks []interviewLaunchpadTrack, sessions []domain.InterviewSession) {
	practiced := map[string]bool{}
	for _, session := range sessions {
		if session.Status == "invalidated" {
			continue
		}
		questionID := session.QuestionID
		if strings.HasPrefix(questionID, "interview-knowledge:") {
			questionID = strings.TrimPrefix(questionID, "interview-knowledge:")
			if versionIndex := strings.LastIndex(questionID, ":v"); versionIndex > 0 {
				questionID = questionID[:versionIndex]
			}
		}
		practiced[questionID] = true
	}
	for index := range tracks {
		tracks[index].Practiced = practiced[tracks[index].QuestionID]
	}
}

func sortInterviewLaunchpadTracks(user *domain.User, tracks []interviewLaunchpadTrack) {
	resumeText := ""
	targetRoleText := ""
	if user != nil {
		targetRoleText = strings.ToLower(strings.TrimSpace(user.Profile.TargetRole))
		parts := make([]string, 0, len(user.Profile.ResumeDocuments)+1)
		for _, document := range resumeDocumentsForProfile(user.Profile) {
			if document.QualityStatus == "passed" {
				parts = append(parts, document.ExtractedText)
			}
		}
		resumeText = strings.ToLower(strings.Join(parts, "\n"))
	}
	sort.SliceStable(tracks, func(i, j int) bool {
		leftScore := interviewLaunchpadResumeScore(resumeText, tracks[i])
		rightScore := interviewLaunchpadResumeScore(resumeText, tracks[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		leftScore = interviewLaunchpadResumeScore(targetRoleText, tracks[i])
		rightScore = interviewLaunchpadResumeScore(targetRoleText, tracks[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if tracks[i].Practiced != tracks[j].Practiced {
			return !tracks[i].Practiced
		}
		if !tracks[i].LatestUpdatedAt.Equal(tracks[j].LatestUpdatedAt) {
			return tracks[i].LatestUpdatedAt.After(tracks[j].LatestUpdatedAt)
		}
		return tracks[i].ID < tracks[j].ID
	})
}

func interviewLaunchpadResumeScore(resumeText string, track interviewLaunchpadTrack) int {
	if strings.TrimSpace(resumeText) == "" {
		return 0
	}
	score := 0
	seen := map[string]bool{}
	terms := append([]string{track.Domain, track.DomainLabel, track.Category}, track.Tags...)
	terms = append(terms, strings.FieldsFunc(track.Title+" "+track.OpeningQuestion, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("，。！？、：:,.!?/()（）[]【】", r)
	})...)
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if len([]rune(term)) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		if strings.Contains(resumeText, term) {
			score++
		}
	}
	return score
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
		{ID: "database-l3-scenario", Title: "数据库 L3", Domain: "database", DomainLabel: "数据库", Difficulty: "L3", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "如何定位 MySQL 慢查询", Details: []string{"情景分析", "慢查询定位", "回滚方案"}},
		{ID: "network-l3-scenario", Title: "网络 L3", Domain: "network", DomainLabel: "网络", Difficulty: "L3", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "如何排查跨服务调用超时", Details: []string{"情景分析", "DNS 与负载均衡", "超时与降级"}},
		{ID: "os-l3-principle", Title: "操作系统 L3", Domain: "os", DomainLabel: "操作系统", Difficulty: "L3", QuestionType: "principle", QuestionRole: "opening", Summary: "load average 高但 CPU 不高怎么排查", Details: []string{"原理问答", "Linux 负载", "I/O 与 D 状态"}},
		{ID: "security-l4-scenario", Title: "安全 L4", Domain: "security", DomainLabel: "安全", Difficulty: "L4", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "访问密钥泄露后如何遏制风险", Details: []string{"情景分析", "密钥轮换", "影响面确认"}},
		{ID: "devops-l4-scenario", Title: "DevOps L4", Domain: "devops", DomainLabel: "DevOps", Difficulty: "L4", QuestionType: "scenario_analysis", QuestionRole: "opening", Summary: "发布失败后如何回滚并恢复流水线", Details: []string{"情景分析", "发布回滚", "流水线恢复"}},
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
		{Value: "network", Label: "网络", Group: "兼容题库", Note: "L3 训练入口"},
		{Value: "os", Label: "操作系统", Group: "兼容题库", Note: "L3 训练入口"},
		{Value: "security", Label: "安全", Group: "兼容题库", Note: "L4 训练入口"},
		{Value: "devops", Label: "DevOps", Group: "兼容题库", Note: "L4 训练入口"},
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
	question, ok := s.resolveInterviewSessionQuestion(session)
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
			"session":        interviewSessionView(session),
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
		Retrieve: s.retrieveInterviewFollowUpContext,
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
		"session":        interviewSessionView(session),
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
	question, ok := s.resolveInterviewSessionQuestion(session)
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
	hits := interviewSubstantialKeywordHits(question, trimmed)
	// 门槛放宽：中文认真作答常命中技术结构词而非英文关键词，不能只靠 25 分词面相似。
	// 领域名单独命中不算（“和数据库题无关”这类闲聊也会带上 domain）。
	if relevance >= 18 || len(hits) > 0 || interviewLooksLikeTechnicalAnswer(trimmed) || interviewChinesePhraseOverlap(question, trimmed) >= 2 {
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
