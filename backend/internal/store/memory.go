package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

type MemoryStore struct {
	mu sync.RWMutex

	Users              map[string]*domain.User
	UsersByUsername    map[string]string
	UsersByEmail       map[string]string
	Scenarios          map[string]*domain.ScenarioQuestion
	ScenarioSessions   map[string]*domain.ScenarioSession
	ScenarioMessages   map[string][]domain.ScenarioMessage
	InterviewQuestions map[string]*domain.InterviewQuestion
	InterviewSessions  map[string]*domain.InterviewSession
	CommunityPosts     map[string]*domain.CommunityPost
	Assets             map[string]*domain.Asset
	AIJobs             map[string]*domain.AIJob
	PromptTemplates    map[string]*domain.PromptTemplate
	AIConfig           domain.AIConfig
	AuditEvents        []domain.AuditEvent
	vectorStore        VectorStore
}

func NewMemoryStore(hashPassword func(string) string) *MemoryStore {
	s := &MemoryStore{
		Users:              map[string]*domain.User{},
		UsersByUsername:    map[string]string{},
		UsersByEmail:       map[string]string{},
		Scenarios:          map[string]*domain.ScenarioQuestion{},
		ScenarioSessions:   map[string]*domain.ScenarioSession{},
		ScenarioMessages:   map[string][]domain.ScenarioMessage{},
		InterviewQuestions: map[string]*domain.InterviewQuestion{},
		InterviewSessions:  map[string]*domain.InterviewSession{},
		CommunityPosts:     map[string]*domain.CommunityPost{},
		Assets:             map[string]*domain.Asset{},
		AIJobs:             map[string]*domain.AIJob{},
		PromptTemplates:    map[string]*domain.PromptTemplate{},
		AuditEvents:        []domain.AuditEvent{},
		vectorStore:        NewMemoryVectorStore(),
	}
	s.seedAdminConfig()
	s.seed(hashPassword)
	return s
}

func (s *MemoryStore) SetVectorStore(vectorStore VectorStore) {
	s.mu.Lock()
	s.vectorStore = vectorStore
	scenarios := make([]domain.ScenarioQuestion, 0, len(s.Scenarios))
	for _, scenario := range s.Scenarios {
		scenarios = append(scenarios, *scenario)
	}
	s.mu.Unlock()

	for _, scenario := range scenarios {
		indexScenarioWithVectorStore(vectorStore, scenario)
	}
}

func (s *MemoryStore) VectorStore() VectorStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vectorStore
}

func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func (s *MemoryStore) CreateUser(username, email, passwordHash string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	usernameKey := strings.ToLower(strings.TrimSpace(username))
	emailKey := strings.ToLower(strings.TrimSpace(email))
	if usernameKey == "" || emailKey == "" || passwordHash == "" {
		return nil, errors.New("username, email and password are required")
	}
	if _, ok := s.UsersByUsername[usernameKey]; ok {
		return nil, errors.New("username already exists")
	}
	if _, ok := s.UsersByEmail[emailKey]; ok {
		return nil, errors.New("email already exists")
	}

	user := &domain.User{
		ID:           NewID(),
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         domain.RoleStudent,
		Profile:      defaultProfile(),
		CreatedAt:    time.Now(),
	}
	s.Users[user.ID] = user
	s.UsersByUsername[usernameKey] = user.ID
	s.UsersByEmail[emailKey] = user.ID
	return cloneUser(user), nil
}

func (s *MemoryStore) ListUsers() []domain.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]domain.User, 0, len(s.Users))
	for _, user := range s.Users {
		items = append(items, *cloneUser(user))
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Username) < strings.ToLower(items[j].Username)
	})
	return items
}

func (s *MemoryStore) FindUserByIdentifier(identifier string) (*domain.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := strings.ToLower(strings.TrimSpace(identifier))
	if id, ok := s.UsersByUsername[key]; ok {
		return cloneUser(s.Users[id]), true
	}
	if id, ok := s.UsersByEmail[key]; ok {
		return cloneUser(s.Users[id]), true
	}
	return nil, false
}

func (s *MemoryStore) GetUser(id string) (*domain.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.Users[id]
	return cloneUser(user), ok
}

func (s *MemoryStore) UpdateUserRole(userID string, role string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !domain.ValidRole(role) {
		return nil, errors.New("invalid role")
	}
	user, ok := s.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	user.Role = role
	return cloneUser(user), nil
}

func (s *MemoryStore) UpdateUserPassword(userID string, passwordHash string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(passwordHash) == "" {
		return nil, errors.New("password hash is required")
	}
	user, ok := s.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	user.PasswordHash = passwordHash
	return cloneUser(user), nil
}

func (s *MemoryStore) UpdateProfile(userID string, targetLevel string, preferredDomains []string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	if targetLevel != "" {
		user.Profile.TargetLevel = targetLevel
	}
	if preferredDomains != nil {
		user.Profile.PreferredDomains = preferredDomains
	}
	user.Profile.UpdatedAt = time.Now()
	return cloneUser(user), nil
}

func (s *MemoryStore) SaveUserProfile(userID string, profile domain.UserProfile) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	profile.UpdatedAt = time.Now()
	user.Profile = profile
	return cloneUser(user), nil
}

func (s *MemoryStore) ListScenarios(domainName, difficulty, tag string) []domain.ScenarioQuestion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]domain.ScenarioQuestion, 0, len(s.Scenarios))
	for _, scenario := range s.Scenarios {
		if domainName != "" && scenario.Domain != domainName {
			continue
		}
		if difficulty != "" && scenario.Difficulty != difficulty {
			continue
		}
		if tag != "" && !containsFold(scenario.Tags, tag) {
			continue
		}
		items = append(items, *scenario)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (s *MemoryStore) GetScenario(id string) (*domain.ScenarioQuestion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	scenario, ok := s.Scenarios[id]
	if !ok {
		return nil, false
	}
	copy := *scenario
	return &copy, true
}

func (s *MemoryStore) AddScenario(scenario domain.ScenarioQuestion) domain.ScenarioQuestion {
	s.mu.Lock()
	scenario = ai.PrepareScenarioForPersistence(scenario)
	existing, hasExisting := s.Scenarios[scenario.ID]
	if scenario.ID == "" {
		scenario.ID = NewID()
	}
	now := time.Now()
	if scenario.CreatedAt.IsZero() {
		if hasExisting && existing != nil && !existing.CreatedAt.IsZero() {
			scenario.CreatedAt = existing.CreatedAt
		} else {
			scenario.CreatedAt = now
		}
	}
	scenario.UpdatedAt = now
	if scenario.Version == 0 {
		scenario.Version = 1
	}
	s.Scenarios[scenario.ID] = &scenario
	vectorStore := s.vectorStore
	s.mu.Unlock()

	indexScenarioWithVectorStore(vectorStore, scenario)
	return scenario
}

func (s *MemoryStore) indexScenarioLocked(scenario domain.ScenarioQuestion) {
	indexScenarioWithVectorStore(s.vectorStore, scenario)
}

func indexScenarioWithVectorStore(vectorStore VectorStore, scenario domain.ScenarioQuestion) {
	if vectorStore == nil {
		return
	}
	docs := ai.BuildScenarioVectorDocuments(scenario)
	if len(docs) == 0 {
		_ = vectorStore.DeleteByQuestion(context.Background(), scenario.ID)
		return
	}
	_ = vectorStore.RebuildScenarioIndex(context.Background(), docs)
}

func (s *MemoryStore) CreateScenarioSession(userID, questionID string) (*domain.ScenarioSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	question, ok := s.Scenarios[questionID]
	if !ok {
		return nil, errors.New("scenario not found")
	}
	questionSnapshot := ai.PrepareScenarioForPersistence(*question)
	now := time.Now()
	session := &domain.ScenarioSession{
		ID:               NewID(),
		UserID:           userID,
		QuestionID:       questionID,
		Status:           "active",
		CurrentTurn:      0,
		MaxTurns:         50,
		RevealedClueIDs:  []string{},
		QuestionSnapshot: questionSnapshot,
		HintLevel:        1,
		StartedAt:        now,
		LastActiveAt:     now,
	}
	s.ScenarioSessions[session.ID] = session
	return cloneScenarioSession(session), nil
}

func (s *MemoryStore) GetScenarioSession(id string) (*domain.ScenarioSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.ScenarioSessions[id]
	return cloneScenarioSession(session), ok
}

func (s *MemoryStore) SaveScenarioSession(session *domain.ScenarioSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *session
	s.ScenarioSessions[session.ID] = &copy
}

func (s *MemoryStore) AddScenarioMessage(message domain.ScenarioMessage) domain.ScenarioMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if message.ID == "" {
		message.ID = NewID()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	s.ScenarioMessages[message.SessionID] = append(s.ScenarioMessages[message.SessionID], message)
	return message
}

func (s *MemoryStore) ListScenarioMessages(sessionID string) []domain.ScenarioMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	messages := s.ScenarioMessages[sessionID]
	out := make([]domain.ScenarioMessage, len(messages))
	copy(out, messages)
	return out
}

func (s *MemoryStore) ListScenarioSessionsForUser(userID string) []domain.ScenarioSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []domain.ScenarioSession{}
	for _, session := range s.ScenarioSessions {
		if session.UserID == userID {
			items = append(items, *session)
		}
	}
	return items
}

func (s *MemoryStore) FindInterviewQuestion(domainName, difficulty, questionType string) (*domain.InterviewQuestion, bool) {
	domainName = strings.TrimSpace(domainName)
	difficulty = strings.TrimSpace(difficulty)
	questionType = strings.TrimSpace(questionType)
	if domainName == "" || difficulty == "" || questionType == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, question := range s.InterviewQuestions {
		if question.Domain != domainName {
			continue
		}
		if question.Difficulty != difficulty {
			continue
		}
		if question.QuestionType != questionType {
			continue
		}
		copy := *question
		return &copy, true
	}
	return nil, false
}

func (s *MemoryStore) GetInterviewQuestion(id string) (*domain.InterviewQuestion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	question, ok := s.InterviewQuestions[id]
	if !ok {
		return nil, false
	}
	copy := *question
	return &copy, true
}

func (s *MemoryStore) CreateInterviewSession(userID string, question *domain.InterviewQuestion) *domain.InterviewSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := &domain.InterviewSession{
		ID:           NewID(),
		UserID:       userID,
		QuestionID:   question.ID,
		Status:       "question_presented",
		CurrentRound: 1,
		MaxRounds:    3,
		Submissions:  []domain.InterviewSubmission{},
		Evaluations:  []domain.InterviewEvaluation{},
		StartedAt:    time.Now(),
	}
	s.InterviewSessions[session.ID] = session
	return cloneInterviewSession(session)
}

func (s *MemoryStore) GetInterviewSession(id string) (*domain.InterviewSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.InterviewSessions[id]
	return cloneInterviewSession(session), ok
}

func (s *MemoryStore) SaveInterviewSession(session *domain.InterviewSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *session
	s.InterviewSessions[session.ID] = &copy
}

func (s *MemoryStore) ListInterviewSessionsForUser(userID string) []domain.InterviewSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []domain.InterviewSession{}
	for _, session := range s.InterviewSessions {
		if session.UserID == userID {
			items = append(items, *session)
		}
	}
	return items
}

func (s *MemoryStore) DeleteInterviewSession(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.InterviewSessions[id]; !ok {
		return false
	}
	delete(s.InterviewSessions, id)
	return true
}

func (s *MemoryStore) AddCommunityPost(post domain.CommunityPost) domain.CommunityPost {
	s.mu.Lock()
	defer s.mu.Unlock()
	post = prepareCommunityPostForPersistence(post)
	if post.ID == "" {
		post.ID = NewID()
	}
	now := time.Now()
	if post.CreatedAt.IsZero() {
		post.CreatedAt = now
	}
	post.UpdatedAt = now
	if post.ReviewHistory == nil {
		post.ReviewHistory = []domain.ReviewHistoryItem{}
	}
	if post.SensitiveCheck.Findings == nil {
		post.SensitiveCheck.Findings = []domain.SensitiveFinding{}
	}
	if post.ModerationSummary != nil {
		copySummary := *post.ModerationSummary
		copySummary.SafeLabels = append([]string{}, post.ModerationSummary.SafeLabels...)
		copySummary.Reasons = append([]string{}, post.ModerationSummary.Reasons...)
		if post.ModerationSummary.AgentTrace != nil {
			trace := *post.ModerationSummary.AgentTrace
			trace.Steps = append([]domain.AgentStep{}, post.ModerationSummary.AgentTrace.Steps...)
			copySummary.AgentTrace = &trace
		}
		post.ModerationSummary = &copySummary
	}
	s.CommunityPosts[post.ID] = &post
	return post
}

func (s *MemoryStore) GetCommunityPost(id string) (*domain.CommunityPost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	post, ok := s.CommunityPosts[id]
	if !ok {
		return nil, false
	}
	return cloneCommunityPost(post), true
}

func (s *MemoryStore) SaveCommunityPost(post *domain.CommunityPost) domain.CommunityPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	prepared := prepareCommunityPostForPersistence(*post)
	post = &prepared
	if post.ID == "" {
		post.ID = NewID()
	}
	now := time.Now()
	if post.CreatedAt.IsZero() {
		post.CreatedAt = now
	}
	post.UpdatedAt = now
	copy := *post
	copy.Tags = append([]string{}, post.Tags...)
	copy.ReviewHistory = append([]domain.ReviewHistoryItem{}, post.ReviewHistory...)
	copy.SensitiveCheck.Findings = append([]domain.SensitiveFinding{}, post.SensitiveCheck.Findings...)
	if post.EditedStructuredContent != nil {
		edited := *post.EditedStructuredContent
		copy.EditedStructuredContent = &edited
	}
	if post.ModerationSummary != nil {
		summary := *post.ModerationSummary
		summary.SafeLabels = append([]string{}, post.ModerationSummary.SafeLabels...)
		summary.Reasons = append([]string{}, post.ModerationSummary.Reasons...)
		if post.ModerationSummary.AgentTrace != nil {
			trace := *post.ModerationSummary.AgentTrace
			trace.Steps = append([]domain.AgentStep{}, post.ModerationSummary.AgentTrace.Steps...)
			summary.AgentTrace = &trace
		}
		copy.ModerationSummary = &summary
	}
	s.CommunityPosts[copy.ID] = &copy
	return copy
}

func (s *MemoryStore) ListCommunityPosts() []domain.CommunityPost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.CommunityPost, 0, len(s.CommunityPosts))
	for _, post := range s.CommunityPosts {
		items = append(items, *cloneCommunityPost(post))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (s *MemoryStore) DeleteCommunityPost(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.CommunityPosts[id]; !ok {
		return false
	}
	delete(s.CommunityPosts, id)
	return true
}

func (s *MemoryStore) CreateAsset(asset domain.Asset) (domain.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if asset.ID == "" {
		asset.ID = NewID()
	}
	if asset.CreatedAt.IsZero() {
		asset.CreatedAt = time.Now()
	}
	if asset.StorageKey == "" {
		asset.StorageKey = asset.ID
	}
	if asset.URL == "" {
		asset.URL = "/api/v1/assets/" + asset.ID
	}
	copy := asset
	s.Assets[copy.ID] = &copy
	return copy, nil
}

func (s *MemoryStore) GetAsset(id string) (*domain.Asset, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.Assets[id]
	if !ok {
		return nil, false
	}
	copy := *asset
	return &copy, true
}

func (s *MemoryStore) ListAssetsForUser(userID string) []domain.Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []domain.Asset{}
	for _, asset := range s.Assets {
		if asset.UserID == userID {
			items = append(items, *asset)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (s *MemoryStore) CreateAIJob(job domain.AIJob) (domain.AIJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.ID == "" {
		job.ID = NewID()
	}
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	copy := job
	s.AIJobs[copy.ID] = &copy
	return copy, nil
}

func (s *MemoryStore) GetAIJob(id string) (*domain.AIJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.AIJobs[id]
	if !ok {
		return nil, false
	}
	return cloneAIJob(job), true
}

func (s *MemoryStore) SaveAIJob(job *domain.AIJob) (domain.AIJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.ID == "" {
		job.ID = NewID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()
	copy := *job
	s.AIJobs[copy.ID] = &copy
	return copy, nil
}

func (s *MemoryStore) ListAIJobs(limit int) []domain.AIJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.AIJob, 0, len(s.AIJobs))
	for _, job := range s.AIJobs {
		items = append(items, *job)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

func (s *MemoryStore) CountAIJobs() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.AIJobs)
}

func (s *MemoryStore) ListPromptTemplates() []domain.PromptTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.PromptTemplate, 0, len(s.PromptTemplates))
	for _, template := range s.PromptTemplates {
		items = append(items, *template)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func (s *MemoryStore) GetPromptTemplate(name string) (*domain.PromptTemplate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	template, ok := s.PromptTemplates[name]
	if !ok {
		return nil, false
	}
	copy := *template
	return &copy, true
}

func (s *MemoryStore) SavePromptTemplate(template domain.PromptTemplate) (domain.PromptTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.PromptTemplates[template.Name]
	if existing == nil {
		existing = &domain.PromptTemplate{Name: template.Name, Default: template.Default, Task: template.Task, Validator: template.Validator}
	}
	if strings.TrimSpace(template.Content) == "" {
		return domain.PromptTemplate{}, errors.New("prompt content is required")
	}
	existing.Content = template.Content
	if template.Default != "" {
		existing.Default = template.Default
	}
	if template.Task != "" {
		existing.Task = template.Task
	}
	if template.Validator != "" {
		existing.Validator = template.Validator
	}
	if strings.TrimSpace(template.RenderEngine) != "" {
		existing.RenderEngine = template.RenderEngine
	} else if strings.TrimSpace(existing.RenderEngine) == "" {
		existing.RenderEngine = "go_template"
	}
	existing.UpdatedBy = template.UpdatedBy
	existing.UpdatedAt = time.Now()
	existing.IsModified = existing.Content != existing.Default || existing.RenderEngine != "go_template"
	copy := *existing
	s.PromptTemplates[copy.Name] = &copy
	return copy, nil
}

func (s *MemoryStore) GetAIConfig() domain.AIConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AIConfig
}

func (s *MemoryStore) SaveAIConfig(config domain.AIConfig) (domain.AIConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(config.Provider) == "" {
		return domain.AIConfig{}, errors.New("provider is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return domain.AIConfig{}, errors.New("model is required")
	}
	config.UpdatedAt = time.Now()
	s.AIConfig = config
	return config, nil
}

func (s *MemoryStore) RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.ID == "" {
		event.ID = NewID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	s.AuditEvents = append([]domain.AuditEvent{event}, s.AuditEvents...)
	if len(s.AuditEvents) > 200 {
		s.AuditEvents = s.AuditEvents[:200]
	}
	return event
}

func (s *MemoryStore) ListAuditEvents(limit int) []domain.AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.AuditEvents) {
		limit = len(s.AuditEvents)
	}
	out := make([]domain.AuditEvent, limit)
	copy(out, s.AuditEvents[:limit])
	return out
}

func (s *MemoryStore) RecordScenarioScore(userID, domainName string, score int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := s.Users[userID]
	if user == nil {
		return
	}
	stats := &user.Profile.TotalStats
	total := stats.AverageScore*(stats.ScenariosSolved+stats.InterviewsTaken) + score
	stats.ScenariosSolved++
	stats.AverageScore = total / (stats.ScenariosSolved + stats.InterviewsTaken)
	user.Profile.CapabilityRadar[domainName] = rolling(user.Profile.CapabilityRadar[domainName], score)
	user.Profile.UpdatedAt = time.Now()
}

func (s *MemoryStore) RecordInterviewScore(userID, domainName string, score int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := s.Users[userID]
	if user == nil {
		return
	}
	stats := &user.Profile.TotalStats
	total := stats.AverageScore*(stats.ScenariosSolved+stats.InterviewsTaken) + score
	stats.InterviewsTaken++
	stats.AverageScore = total / (stats.ScenariosSolved + stats.InterviewsTaken)
	user.Profile.CapabilityRadar[domainName] = rolling(user.Profile.CapabilityRadar[domainName], score)
	user.Profile.UpdatedAt = time.Now()
}

func (s *MemoryStore) seed(hashPassword func(string) string) {
	now := time.Now()
	demo := &domain.User{
		ID:           "user-demo",
		Username:     "demo",
		Email:        "demo@example.com",
		PasswordHash: hashPassword("demo123"),
		Role:         domain.RoleStudent,
		Profile:      defaultProfile(),
		CreatedAt:    now,
	}
	instructor := &domain.User{
		ID:           "user-instructor",
		Username:     "instructor",
		Email:        "instructor@example.com",
		PasswordHash: hashPassword("instructor123"),
		Role:         domain.RoleInstructor,
		Profile:      defaultProfile(),
		CreatedAt:    now,
	}
	admin := &domain.User{
		ID:           "user-admin",
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: hashPassword("admin123"),
		Role:         domain.RoleAdmin,
		Profile:      defaultProfile(),
		CreatedAt:    now,
	}
	for _, user := range []*domain.User{demo, instructor, admin} {
		s.Users[user.ID] = user
		s.UsersByUsername[strings.ToLower(user.Username)] = user.ID
		s.UsersByEmail[strings.ToLower(user.Email)] = user.ID
	}
	for _, scenario := range seedDiagnosticScenarios(now) {
		item := scenario
		s.Scenarios[item.ID] = &item
		s.indexScenarioLocked(item)
	}
	for _, question := range seedInterviewQuestions(now) {
		item := question
		s.InterviewQuestions[item.ID] = &item
	}
}

func (s *MemoryStore) seedAdminConfig() {
	now := time.Now()
	s.AIConfig = domain.AIConfig{
		Provider:      "mock",
		Model:         "mock",
		Temperature:   0.2,
		TopP:          0,
		TopK:          0,
		MaxTokens:     0,
		StreamEnabled: true,
		FallbackModel: "mock",
		UpdatedAt:     now,
	}
	scenarioGeneratePrompt := ai.DefaultPromptContent("scenario_generate")
	defaults := []domain.PromptTemplate{
		{Name: "scenario_generate", Task: "情景题生成", Default: scenarioGeneratePrompt, Content: scenarioGeneratePrompt, Validator: "scenario_question"},
		{Name: "community_structure", Task: "UGC 结构化", Default: "从真实故障案例中提取现象、根因、证据和排查步骤。", Content: "从真实故障案例中提取现象、根因、证据和排查步骤。", Validator: "scenario_content_preview"},
		{Name: "interview_feedback", Task: "面试评估", Default: "按五个维度生成面试反馈、追问和最终报告。", Content: "按五个维度生成面试反馈、追问和最终报告。", Validator: "interview_feedback"},
		{Name: "scenario_reply", Task: "排查回复改写", Default: "在不泄露答案的前提下改写渐进式排查回复。", Content: "在不泄露答案的前提下改写渐进式排查回复。", Validator: "scenario_reply"},
		{Name: "sensitive_check", Task: "敏感信息检测", Default: "识别真实 IP、密码、密钥、公司名、人名、客户名和内部服务名。", Content: "识别真实 IP、密码、密钥、公司名、人名、客户名和内部服务名。", Validator: "sensitive_check"},
	}
	for _, template := range defaults {
		item := template
		item.UpdatedAt = now
		item.RenderEngine = "go_template"
		s.PromptTemplates[item.Name] = &item
	}
}

func defaultProfile() domain.UserProfile {
	return domain.UserProfile{
		TargetLevel:      "intermediate",
		PreferredDomains: []string{"database", "network", "os"},
		CapabilityRadar: map[string]int{
			"database": 72,
			"network":  64,
			"os":       58,
			"security": 50,
			"devops":   60,
		},
		WeakPoints: []domain.WeakPoint{
			{Domain: "os", Topic: "Linux 资源定位", LastScore: 55, SuggestedQuestions: []string{"scenario-os-cpu"}},
		},
		TotalStats: domain.TotalStats{ScenariosSolved: 0, InterviewsTaken: 0, AverageScore: 0, StreakDays: 0},
		UpdatedAt:  time.Now(),
	}
}

func seedScenarios(now time.Time) []domain.ScenarioQuestion {
	return []domain.ScenarioQuestion{
		{
			ID:           "scenario-db-index",
			Title:        "订单查询接口突然变慢",
			Description:  "业务反馈订单查询接口在午高峰从 200ms 上升到 4s 左右，应用实例 CPU 与内存没有明显升高，数据库连接数稳定。请通过提问逐步定位根因。",
			Domain:       "database",
			Difficulty:   "L3",
			ScenarioType: "performance",
			Tags:         []string{"MySQL", "索引", "慢查询"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now,
			UpdatedAt:    now,
			Content: domain.ScenarioContent{
				RootCause:           "订单表新增状态筛选后未建立联合索引，导致慢查询全表扫描。",
				RootCauseKeywords:   []string{"联合索引", "全表扫描", "慢查询", "状态筛选", "orders"},
				KeyEvidence:         []string{"慢查询日志 rows_examined 超过 500 万", "执行计划 type=ALL", "where 条件包含 status 与 created_at", "现有索引只覆盖 user_id"},
				StandardProcedure:   []string{"确认接口耗时分布", "检查数据库慢查询日志", "查看 SQL 执行计划", "核对现有索引覆盖情况", "补充联合索引并灰度验证", "准备回滚脚本"},
				ArchitectureDiagram: "graph TD\nA[Web API] --> B[Order Service]\nB --> C[(MySQL orders)]\nB --> D[Redis Cache]\nC --> E[Slow Query Log]",
				ReferenceLinks:      []string{"MySQL EXPLAIN", "联合索引最左前缀原则"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "c1", TriggerKeywords: []string{"CPU", "内存", "负载", "实例"}, Content: "应用实例 CPU 在 45%-55%，内存水位稳定，接口慢不由应用资源瓶颈直接引起。", RecommendedNextAsk: "继续询问数据库连接数或慢查询。"},
						{ClueID: "c2", TriggerKeywords: []string{"慢查询", "日志", "slow log"}, Content: "慢查询日志中同一条订单查询 SQL 频繁出现，rows_examined 约 500 万。", RecommendedNextAsk: "继续追问执行计划或索引覆盖。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "c3", TriggerKeywords: []string{"执行计划", "explain", "索引"}, PrerequisiteClues: []string{"c2"}, Content: "EXPLAIN 显示 type=ALL，possible_keys 为空，where 条件包含 status、created_at 与 user_id。", RecommendedNextAsk: "继续询问现有索引或最近变更。"},
						{ClueID: "c4", TriggerKeywords: []string{"变更", "发布", "状态"}, PrerequisiteClues: []string{"c3"}, Content: "最近一次发布给订单列表增加了 status 筛选，但订单表没有对应联合索引。", RecommendedNextAsk: "可以整理根因并提交答案。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "d1", TriggerKeywords: []string{"网络", "ping", "延迟"}, Content: "服务到数据库内网延迟低于 1ms，网络层暂无异常。", IsDistractor: true},
					},
				},
			},
		},
		{
			ID:           "scenario-network-timeout",
			Title:        "跨机房调用间歇性超时",
			Description:  "支付回调服务跨机房调用库存接口，近两小时出现少量超时。单机日志未见异常，重试后大多成功。请通过提问定位问题链路。",
			Domain:       "network",
			Difficulty:   "L3",
			ScenarioType: "troubleshooting",
			Tags:         []string{"网络", "DNS", "跨机房"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now.Add(-time.Minute),
			UpdatedAt:    now.Add(-time.Minute),
			Content: domain.ScenarioContent{
				RootCause:           "跨机房 DNS 解析命中异常 VIP，部分请求被路由到未完全健康的库存实例。",
				RootCauseKeywords:   []string{"DNS", "异常 VIP", "路由", "健康检查", "跨机房"},
				KeyEvidence:         []string{"超时集中在特定 VIP", "DNS 解析结果在不同机房不一致", "健康检查延迟剔除异常实例"},
				StandardProcedure:   []string{"确认超时比例与时间窗口", "按目标 IP 聚合失败日志", "核对 DNS 解析结果", "检查负载均衡健康检查", "临时摘除异常 VIP", "修复解析配置并观察"},
				ArchitectureDiagram: "graph TD\nA[Payment Callback] --> B[Internal DNS]\nB --> C[VIP A]\nB --> D[VIP B]\nC --> E[Inventory Pool]\nD --> F[Degraded Instance]",
				ReferenceLinks:      []string{"DNS 缓存与解析", "负载均衡健康检查"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "c1", TriggerKeywords: []string{"比例", "时间", "集中"}, Content: "超时比例约 2%，集中在跨机房调用，单机维度没有固定热点。", RecommendedNextAsk: "继续按目标 IP 或 VIP 维度聚合。"},
						{ClueID: "c2", TriggerKeywords: []string{"IP", "VIP", "目标地址"}, Content: "失败请求大多落在同一个 VIP，其他 VIP 的成功率正常。", RecommendedNextAsk: "继续询问 DNS 解析或健康检查。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "c3", TriggerKeywords: []string{"DNS", "解析", "域名"}, PrerequisiteClues: []string{"c2"}, Content: "两个机房解析同一域名时结果不一致，异常机房会返回已灰度下线的 VIP。", RecommendedNextAsk: "继续确认负载均衡健康状态。"},
						{ClueID: "c4", TriggerKeywords: []string{"健康检查", "剔除", "负载均衡"}, PrerequisiteClues: []string{"c3"}, Content: "健康检查剔除有 90 秒延迟，异常实例在窗口内仍可能被访问。", RecommendedNextAsk: "可以提交根因判断。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "d1", TriggerKeywords: []string{"CPU", "内存", "GC"}, Content: "库存服务实例资源水位稳定，GC 暂无明显尖刺。", IsDistractor: true},
					},
				},
			},
		},
		{
			ID:           "scenario-os-cpu",
			Title:        "Linux 主机负载高但 CPU 使用率不高",
			Description:  "一台批处理主机 load average 持续升高，但 top 中 CPU 使用率不高，业务任务完成时间显著变长。请逐步排查。",
			Domain:       "os",
			Difficulty:   "L2",
			ScenarioType: "troubleshooting",
			Tags:         []string{"Linux", "IO", "负载"},
			Status:       "active",
			Source:       "seed",
			CreatedBy:    "user-admin",
			Version:      1,
			CreatedAt:    now.Add(-2 * time.Minute),
			UpdatedAt:    now.Add(-2 * time.Minute),
			Content: domain.ScenarioContent{
				RootCause:           "磁盘 IO 等待过高导致大量进程处于 D 状态，推高系统负载。",
				RootCauseKeywords:   []string{"IO wait", "D 状态", "磁盘", "iostat", "负载"},
				KeyEvidence:         []string{"wa 指标超过 45%", "iostat 显示 await 上升", "多进程处于 D 状态"},
				StandardProcedure:   []string{"区分 CPU 使用率与 load average", "查看 vmstat wa", "使用 iostat 定位磁盘等待", "检查 D 状态进程", "定位批处理读写热点", "限速或迁移任务"},
				ArchitectureDiagram: "graph TD\nA[Batch Jobs] --> B[Local Disk]\nA --> C[Process Queue]\nB --> D[High await]\nD --> C",
				ReferenceLinks:      []string{"Linux load average", "iostat await"},
				RevealStrategy: domain.RevealStrategy{
					SurfaceClues: []domain.Clue{
						{ClueID: "c1", TriggerKeywords: []string{"top", "CPU", "使用率"}, Content: "CPU user/sys 不高，但 wa 指标接近 45%。", RecommendedNextAsk: "继续询问 IO 或磁盘指标。"},
						{ClueID: "c2", TriggerKeywords: []string{"进程", "状态", "D"}, Content: "ps 中有大量批处理进程处于 D 状态。", RecommendedNextAsk: "继续查看 iostat 或磁盘等待。"},
					},
					DeepClues: []domain.Clue{
						{ClueID: "c3", TriggerKeywords: []string{"iostat", "await", "磁盘"}, PrerequisiteClues: []string{"c1"}, Content: "iostat 显示目标磁盘 await 明显升高，util 接近 100%。", RecommendedNextAsk: "可以整理 IO 等待链路并提交答案。"},
					},
					Distractors: []domain.Clue{
						{ClueID: "d1", TriggerKeywords: []string{"网络", "丢包"}, Content: "主机网络无丢包，业务慢与网络关系不大。", IsDistractor: true},
					},
				},
			},
		},
	}
}

func seedInterviewQuestions(now time.Time) []domain.InterviewQuestion {
	dimensions := []domain.EvaluationDimension{
		{Name: "technical_accuracy", Weight: 0.30, Criteria: "原理、命令与判断准确"},
		{Name: "logical_completeness", Weight: 0.25, Criteria: "排查路径覆盖主要分支"},
		{Name: "solution_feasibility", Weight: 0.20, Criteria: "方案可落地并考虑回滚"},
		{Name: "depth_breadth", Weight: 0.15, Criteria: "触及底层原理与边界情况"},
		{Name: "expression_structure", Weight: 0.10, Criteria: "表达有层次，术语规范"},
	}
	return []domain.InterviewQuestion{
		{
			ID:                   "interview-db-slow-query",
			Title:                "如何定位 MySQL 慢查询",
			Description:          "线上接口突然变慢，你怀疑是 MySQL 查询问题。请说明你的定位路径、关键命令、可能修复方案和回滚考虑。",
			Domain:               "database",
			Difficulty:           "L3",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应从链路耗时、慢查询日志、EXPLAIN、索引覆盖、执行计划变化、灰度建索引与回滚方案等方面回答。",
			ReferenceKeywords:    []string{"慢查询", "EXPLAIN", "索引", "执行计划", "回滚", "灰度"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "technical_accuracy < 60", QuestionTemplate: "你提到了慢查询，能具体说明如何用 EXPLAIN 判断是否命中索引吗？", Type: "deep"},
				{TriggerCondition: "solution_feasibility < 60", QuestionTemplate: "如果不能长时间锁表，你会如何灰度修复并准备回滚？", Type: "pressure"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-network-timeout",
			Title:                "如何排查跨服务调用超时",
			Description:          "微服务之间出现间歇性超时，重试后成功。请给出从应用到网络基础设施的排查路径。",
			Domain:               "network",
			Difficulty:           "L3",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应覆盖日志聚合、目标 IP/VIP、DNS、负载均衡、健康检查、连接池、超时配置与降级。",
			ReferenceKeywords:    []string{"日志", "IP", "VIP", "DNS", "负载均衡", "健康检查", "连接池", "降级"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "logical_completeness < 60", QuestionTemplate: "如果失败集中在一个 VIP，你下一步如何验证 DNS 和负载均衡配置？", Type: "supplement"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-os-load",
			Title:                "load average 高但 CPU 不高怎么排查",
			Description:          "Linux 主机 load average 很高，但 CPU 使用率并不高。请说明可能原因、验证命令和处理策略。",
			Domain:               "os",
			Difficulty:           "L3",
			QuestionType:         "principle",
			ReferenceAnswer:      "应解释 load 与可运行/不可中断进程关系，使用 vmstat、iostat、ps 定位 IO wait 与 D 状态进程。",
			ReferenceKeywords:    []string{"load", "D 状态", "IO wait", "vmstat", "iostat", "ps"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "depth_breadth < 60", QuestionTemplate: "为什么 CPU 不高时 load 仍可能升高？请解释 D 状态进程的影响。", Type: "deep"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-security-ak-leak",
			Title:                "访问密钥泄露后如何遏制风险",
			Description:          "研发同学把云平台访问密钥提交到了公开仓库。请说明你的风险遏制顺序、密钥轮换方式、影响面确认和事后加固措施。",
			Domain:               "security",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应优先冻结高风险权限、轮换 AK/SK、追查访问日志与调用来源、确认受影响资源，并补齐仓库扫描、最小权限和发布前密钥审计。",
			ReferenceKeywords:    []string{"AK/SK", "轮换", "访问日志", "最小权限", "仓库扫描", "影响面"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "logical_completeness < 60", QuestionTemplate: "如果无法立即停机，你会如何一边轮换密钥一边保证线上业务继续运行？", Type: "pressure"},
				{TriggerCondition: "solution_feasibility < 60", QuestionTemplate: "你会如何验证泄露密钥是否已被实际利用？", Type: "deep"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-devops-release-rollback",
			Title:                "发布失败后如何回滚并恢复流水线",
			Description:          "一次生产发布导致服务健康检查持续失败，流水线卡在发布阶段。请给出排查顺序、回滚策略和流水线修复方案。",
			Domain:               "devops",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应覆盖变更范围确认、健康检查与日志对比、版本回滚、数据库兼容检查、流水线锁释放、发布闸门与回滚演练完善。",
			ReferenceKeywords:    []string{"健康检查", "回滚", "变更范围", "发布闸门", "流水线", "兼容性"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "technical_accuracy < 60", QuestionTemplate: "如果数据库已经执行了部分变更，你如何保证应用回滚不会产生新的不一致？", Type: "deep"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-backend-idempotency",
			Title:                "下单重试导致重复扣减库存如何处理",
			Description:          "用户重复提交订单后库存被扣减两次。请说明你会如何定位幂等缺失点，并给出修复和补偿策略。",
			Domain:               "backend",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应识别请求去重键、幂等表/状态机、消息重复消费、库存补偿和事务边界，说明修复前后的兼容与补数方案。",
			ReferenceKeywords:    []string{"幂等", "去重键", "补偿", "事务", "消息重复", "库存"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "solution_feasibility < 60", QuestionTemplate: "如果已经出现重复扣减，你会如何补数并避免再次触发？", Type: "pressure"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-distributed-tcc",
			Title:                "分布式事务补偿失败如何排查",
			Description:          "订单服务和支付服务通过消息最终一致，但补偿任务频繁失败。请说明排查路径、补偿幂等和恢复策略。",
			Domain:               "distributed",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应覆盖消息轨迹、补偿状态机、幂等校验、重试退避、死信队列、人工兜底和账务对账机制。",
			ReferenceKeywords:    []string{"补偿", "幂等", "死信队列", "重试", "状态机", "对账"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "depth_breadth < 60", QuestionTemplate: "如何避免补偿任务在网络抖动时反复重入并放大故障？", Type: "deep"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-cloudnative-rollout",
			Title:                "Kubernetes 滚动发布卡住如何定位",
			Description:          "Deployment 滚动发布后新 Pod 持续重启，旧版本又迟迟不退出。请说明从镜像、配置、探针到调度层面的排查顺序。",
			Domain:               "cloud-native",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应检查镜像版本、配置差异、健康探针、资源限制、节点事件、调度失败、回滚策略和发布窗口控制。",
			ReferenceKeywords:    []string{"Deployment", "探针", "镜像", "调度", "回滚", "节点事件"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "logical_completeness < 60", QuestionTemplate: "如果只有一个可用副本，你会如何降低回滚过程中的可用性风险？", Type: "pressure"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-mq-cache-backlog",
			Title:                "缓存击穿伴随消息积压如何联合排查",
			Description:          "热点 key 同时失效后数据库流量暴涨，消息队列消费也开始积压。请说明你会怎样判断根因先后与止损顺序。",
			Domain:               "mq-cache",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应覆盖热点 key、缓存重建、限流降级、消息积压监控、消费者扩缩容与重放策略，说明先止损再恢复的顺序。",
			ReferenceKeywords:    []string{"缓存击穿", "热点 key", "消息积压", "限流", "降级", "扩容"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "technical_accuracy < 60", QuestionTemplate: "如果重启消费者会导致重复消费，你会怎样处理积压恢复？", Type: "deep"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-observability-alert-storm",
			Title:                "告警风暴中如何快速定位真实故障",
			Description:          "一次区域性故障触发了上百条告警，值班同学无法快速定位真正的根因。请说明你的告警去噪、指标定位和链路分析方法。",
			Domain:               "observability",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应说明告警聚合、核心 SLI 识别、指标时间窗对齐、Trace 采样、日志关联和降噪规则修复。",
			ReferenceKeywords:    []string{"告警聚合", "SLI", "Trace", "采样", "日志关联", "去噪"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "solution_feasibility < 60", QuestionTemplate: "告警风暴过去后，你会如何改造规则避免同类噪音再次淹没真实故障？", Type: "supplement"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-performance-p99",
			Title:                "P99 延迟抖动如何做性能定位",
			Description:          "平均响应时间正常，但 P99 延迟在高峰期持续抖动。请说明你会如何从线程池、锁竞争、GC 和热点流量定位瓶颈。",
			Domain:               "performance",
			Difficulty:           "L4",
			QuestionType:         "scenario_analysis",
			ReferenceAnswer:      "应覆盖分位值监控、线程池饱和、锁竞争、GC 停顿、热点请求画像和压测复现手段。",
			ReferenceKeywords:    []string{"P99", "线程池", "锁竞争", "GC", "热点流量", "压测"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "depth_breadth < 60", QuestionTemplate: "平均值正常但 P99 抖动时，你为什么优先看线程池和锁竞争而不是先改机器配置？", Type: "deep"},
			},
			CreatedAt: now,
		},
		{
			ID:                   "interview-architecture-multi-active",
			Title:                "多活架构下如何处理跨地域一致性",
			Description:          "公司计划把核心交易系统升级为双活架构。请说明你会如何划分一致性等级、处理流量切换和设计演进路径。",
			Domain:               "architecture",
			Difficulty:           "L5",
			QuestionType:         "principle",
			ReferenceAnswer:      "应说明核心链路与边缘链路的一致性分级、冲突解决、流量调度、容灾演练、成本权衡与渐进式演进策略。",
			ReferenceKeywords:    []string{"双活", "一致性分级", "流量切换", "冲突解决", "容灾演练", "演进"},
			EvaluationDimensions: dimensions,
			FollowUpStrategies: []domain.FollowUpStrategy{
				{TriggerCondition: "logical_completeness < 60", QuestionTemplate: "如果业务方要求所有链路都强一致，你会如何解释代价并推动分级设计？", Type: "pressure"},
			},
			CreatedAt: now,
		},
	}
}

func cloneUser(user *domain.User) *domain.User {
	if user == nil {
		return nil
	}
	copy := *user
	copy.Profile.PreferredDomains = append([]string{}, user.Profile.PreferredDomains...)
	copy.Profile.CapabilityRadar = map[string]int{}
	for k, v := range user.Profile.CapabilityRadar {
		copy.Profile.CapabilityRadar[k] = v
	}
	copy.Profile.WeakPoints = append([]domain.WeakPoint{}, user.Profile.WeakPoints...)
	copy.Profile.CheckinDates = append([]string{}, user.Profile.CheckinDates...)
	return &copy
}

func cloneCommunityPost(post *domain.CommunityPost) *domain.CommunityPost {
	if post == nil {
		return nil
	}
	copy := *post
	copy.Tags = append([]string{}, post.Tags...)
	copy.ReviewHistory = append([]domain.ReviewHistoryItem{}, post.ReviewHistory...)
	copy.SensitiveCheck.Findings = append([]domain.SensitiveFinding{}, post.SensitiveCheck.Findings...)
	if post.EditedStructuredContent != nil {
		edited := *post.EditedStructuredContent
		copy.EditedStructuredContent = &edited
	}
	if post.ModerationSummary != nil {
		summary := *post.ModerationSummary
		summary.SafeLabels = append([]string{}, post.ModerationSummary.SafeLabels...)
		summary.Reasons = append([]string{}, post.ModerationSummary.Reasons...)
		if post.ModerationSummary.AgentTrace != nil {
			trace := *post.ModerationSummary.AgentTrace
			trace.Steps = append([]domain.AgentStep{}, post.ModerationSummary.AgentTrace.Steps...)
			summary.AgentTrace = &trace
		}
		copy.ModerationSummary = &summary
	}
	return &copy
}

func cloneAIJob(job *domain.AIJob) *domain.AIJob {
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func prepareCommunityPostForPersistence(post domain.CommunityPost) domain.CommunityPost {
	post.AIStructuredContent = prepareCommunityContentForPersistence(post.AIStructuredContent, post.Title, post.Domain)
	if post.EditedStructuredContent != nil {
		edited := prepareCommunityContentForPersistence(*post.EditedStructuredContent, post.Title, post.Domain)
		post.EditedStructuredContent = &edited
	}
	for i := range post.ReviewHistory {
		if post.ReviewHistory[i].Content == nil {
			continue
		}
		content := prepareCommunityContentForPersistence(*post.ReviewHistory[i].Content, post.Title, post.Domain)
		post.ReviewHistory[i].Content = &content
	}
	return post
}

func prepareCommunityContentForPersistence(content domain.ScenarioContent, title, domainName string) domain.ScenarioContent {
	return ai.PrepareScenarioContent(content, domain.ScenarioQuestion{
		Title:   title,
		Domain:  domainName,
		Content: content,
	})
}

func cloneScenarioSession(session *domain.ScenarioSession) *domain.ScenarioSession {
	if session == nil {
		return nil
	}
	copy := *session
	copy.RevealedClueIDs = append([]string{}, session.RevealedClueIDs...)
	return &copy
}

func cloneInterviewSession(session *domain.InterviewSession) *domain.InterviewSession {
	if session == nil {
		return nil
	}
	copy := *session
	copy.Submissions = append([]domain.InterviewSubmission{}, session.Submissions...)
	copy.Evaluations = append([]domain.InterviewEvaluation{}, session.Evaluations...)
	return &copy
}

func containsFold(items []string, needle string) bool {
	for _, item := range items {
		if strings.EqualFold(item, needle) {
			return true
		}
	}
	return false
}

func rolling(current, next int) int {
	if current == 0 {
		return next
	}
	return (current*2 + next) / 3
}
