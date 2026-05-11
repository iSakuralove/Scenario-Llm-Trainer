package store

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"situational-teaching/backend/internal/ai"
	"situational-teaching/backend/internal/domain"
)

type PostgresStore struct {
	pool        *pgxpool.Pool
	vectorStore VectorStore
}

const promptTemplateListSelectSQL = `
	SELECT name, COALESCE(task, ''), COALESCE(default_content, ''), content, COALESCE(render_engine, 'go_template'),
	       COALESCE(updated_by, ''), updated_at, COALESCE(validator, '')
	FROM prompt_templates
	ORDER BY name ASC
`

func NewPostgresStore(ctx context.Context, databaseURL string, hashPassword func(string) string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &PostgresStore{pool: pool, vectorStore: NewMemoryVectorStore()}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, VectorSchemaSQL); err != nil {
		log.Printf("pgvector unavailable, using in-memory vector index: %v", err)
	} else {
		s.vectorStore = NewPostgresVectorStore(pool)
	}
	if err := s.Seed(ctx, hashPassword); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) VectorStore() VectorStore {
	if s == nil {
		return nil
	}
	return s.vectorStore
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, LegacyCompatibilitySQL); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, SchemaSQL)
	return err
}

func (s *PostgresStore) Seed(ctx context.Context, hashPassword func(string) string) error {
	now := time.Now()
	users := []*domain.User{
		{
			ID:           "user-demo",
			Username:     "demo",
			Email:        "demo@example.com",
			PasswordHash: hashPassword("demo123"),
			Role:         domain.RoleStudent,
			Profile:      defaultProfile(),
			CreatedAt:    now,
		},
		{
			ID:           "user-instructor",
			Username:     "instructor",
			Email:        "instructor@example.com",
			PasswordHash: hashPassword("instructor123"),
			Role:         domain.RoleInstructor,
			Profile:      defaultProfile(),
			CreatedAt:    now,
		},
		{
			ID:           "user-admin",
			Username:     "admin",
			Email:        "admin@example.com",
			PasswordHash: hashPassword("admin123"),
			Role:         domain.RoleAdmin,
			Profile:      defaultProfile(),
			CreatedAt:    now,
		},
	}
	for _, user := range users {
		if err := s.upsertUser(ctx, user); err != nil {
			return err
		}
	}
	for _, scenario := range seedDiagnosticScenarios(now) {
		s.AddScenario(scenario)
	}
	for _, question := range seedInterviewQuestions(now) {
		if err := s.upsertInterviewQuestion(ctx, question); err != nil {
			return err
		}
	}
	if err := s.seedAdminConfig(ctx); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) CreateUser(username, email, passwordHash string) (*domain.User, error) {
	usernameKey := strings.TrimSpace(username)
	emailKey := strings.TrimSpace(email)
	if usernameKey == "" || emailKey == "" || passwordHash == "" {
		return nil, errors.New("username, email and password are required")
	}
	user := &domain.User{
		ID:           NewID(),
		Username:     usernameKey,
		Email:        emailKey,
		PasswordHash: passwordHash,
		Role:         domain.RoleStudent,
		Profile:      defaultProfile(),
		CreatedAt:    time.Now(),
	}
	if err := s.insertUser(context.Background(), user); err != nil {
		if strings.Contains(err.Error(), "users_username_key") {
			return nil, errors.New("username already exists")
		}
		if strings.Contains(err.Error(), "users_email_key") {
			return nil, errors.New("email already exists")
		}
		return nil, err
	}
	return cloneUser(user), nil
}

func (s *PostgresStore) ListUsers() []domain.User {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, username, email, password_hash, role, profile, created_at
		FROM users
		ORDER BY lower(username) ASC
	`)
	if err != nil {
		return []domain.User{}
	}
	defer rows.Close()

	items := []domain.User{}
	for rows.Next() {
		user, ok := scanUser(rows)
		if ok {
			items = append(items, *user)
		}
	}
	return items
}

func (s *PostgresStore) FindUserByIdentifier(identifier string) (*domain.User, bool) {
	key := strings.ToLower(strings.TrimSpace(identifier))
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, username, email, password_hash, role, profile, created_at
		FROM users
		WHERE lower(username) = $1 OR lower(email) = $1
		LIMIT 1
	`, key)
	return scanUser(row)
}

func (s *PostgresStore) GetUser(id string) (*domain.User, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, username, email, password_hash, role, profile, created_at
		FROM users
		WHERE id = $1
	`, id)
	return scanUser(row)
}

func (s *PostgresStore) UpdateUserRole(userID string, role string) (*domain.User, error) {
	if !domain.ValidRole(role) {
		return nil, errors.New("invalid role")
	}
	tag, err := s.pool.Exec(context.Background(), `
		UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1
	`, userID, role)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errors.New("user not found")
	}
	user, ok := s.GetUser(userID)
	if !ok {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (s *PostgresStore) UpdateUserPassword(userID string, passwordHash string) (*domain.User, error) {
	if strings.TrimSpace(passwordHash) == "" {
		return nil, errors.New("password hash is required")
	}
	tag, err := s.pool.Exec(context.Background(), `
		UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1
	`, userID, passwordHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errors.New("user not found")
	}
	user, ok := s.GetUser(userID)
	if !ok {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (s *PostgresStore) UpdateProfile(userID string, targetLevel string, preferredDomains []string) (*domain.User, error) {
	user, ok := s.GetUser(userID)
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
	profileJSON, err := marshal(user.Profile)
	if err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(context.Background(), `
		UPDATE users SET profile = $2, updated_at = NOW() WHERE id = $1
	`, userID, profileJSON)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *PostgresStore) SaveUserProfile(userID string, profile domain.UserProfile) (*domain.User, error) {
	if _, ok := s.GetUser(userID); !ok {
		return nil, errors.New("user not found")
	}
	profile.UpdatedAt = time.Now()
	profileJSON, err := marshal(profile)
	if err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(context.Background(), `
		UPDATE users SET profile = $2, updated_at = NOW() WHERE id = $1
	`, userID, profileJSON)
	if err != nil {
		return nil, err
	}
	updated, ok := s.GetUser(userID)
	if !ok {
		return nil, errors.New("user not found")
	}
	return updated, nil
}

func (s *PostgresStore) ListScenarios(domainName, difficulty, tag string) []domain.ScenarioQuestion {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, title, description, domain, difficulty, scenario_type, tags, content,
		       status, source, created_by, version, created_at, updated_at
		FROM scenario_questions
		ORDER BY created_at DESC
	`)
	if err != nil {
		return []domain.ScenarioQuestion{}
	}
	defer rows.Close()

	items := []domain.ScenarioQuestion{}
	for rows.Next() {
		item, err := scanScenarioRows(rows)
		if err != nil {
			continue
		}
		if domainName != "" && item.Domain != domainName {
			continue
		}
		if difficulty != "" && item.Difficulty != difficulty {
			continue
		}
		if tag != "" && !containsFold(item.Tags, tag) {
			continue
		}
		items = append(items, item)
	}
	return items
}

func (s *PostgresStore) GetScenario(id string) (*domain.ScenarioQuestion, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, title, description, domain, difficulty, scenario_type, tags, content,
		       status, source, created_by, version, created_at, updated_at
		FROM scenario_questions
		WHERE id = $1
	`, id)
	return scanScenario(row)
}

func (s *PostgresStore) AddScenario(scenario domain.ScenarioQuestion) domain.ScenarioQuestion {
	scenario = ai.PrepareScenarioForPersistence(scenario)
	if scenario.ID == "" {
		scenario.ID = NewID()
	}
	now := time.Now()
	if scenario.CreatedAt.IsZero() {
		scenario.CreatedAt = now
	}
	scenario.UpdatedAt = now
	if scenario.Version == 0 {
		scenario.Version = 1
	}
	contentJSON, err := marshal(scenario.Content)
	if err != nil {
		return scenario
	}
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO scenario_questions
		    (id, title, description, domain, difficulty, scenario_type, tags, content, status, source, created_by, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
		    title = EXCLUDED.title,
		    description = EXCLUDED.description,
		    domain = EXCLUDED.domain,
		    difficulty = EXCLUDED.difficulty,
		    scenario_type = EXCLUDED.scenario_type,
		    tags = EXCLUDED.tags,
		    content = EXCLUDED.content,
		    status = EXCLUDED.status,
		    source = EXCLUDED.source,
		    created_by = EXCLUDED.created_by,
		    version = EXCLUDED.version,
		    updated_at = EXCLUDED.updated_at
	`, scenario.ID, scenario.Title, scenario.Description, scenario.Domain, scenario.Difficulty, scenario.ScenarioType,
		scenario.Tags, contentJSON, scenario.Status, scenario.Source, scenario.CreatedBy, scenario.Version,
		scenario.CreatedAt, scenario.UpdatedAt)
	s.indexScenario(scenario)
	return scenario
}

func (s *PostgresStore) indexScenario(scenario domain.ScenarioQuestion) {
	if s == nil || s.vectorStore == nil {
		return
	}
	docs := ai.BuildScenarioVectorDocuments(scenario)
	if len(docs) == 0 {
		_ = s.vectorStore.DeleteByQuestion(context.Background(), scenario.ID)
		return
	}
	_ = s.vectorStore.RebuildScenarioIndex(context.Background(), docs)
}

func (s *PostgresStore) CreateScenarioSession(userID, questionID string) (*domain.ScenarioSession, error) {
	question, ok := s.GetScenario(questionID)
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
	if err := s.insertScenarioSession(context.Background(), session); err != nil {
		return nil, err
	}
	return cloneScenarioSession(session), nil
}

func (s *PostgresStore) GetScenarioSession(id string) (*domain.ScenarioSession, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, user_id, question_id, status, current_turn, max_turns, revealed_clue_ids,
		       user_answer, evaluation_result, score, question_snapshot, hint_level,
		       no_new_clue_streak, COALESCE(conversation_summary, ''), started_at, last_active_at, ended_at
		FROM scenario_sessions
		WHERE id = $1
	`, id)
	return scanScenarioSession(row)
}

func (s *PostgresStore) SaveScenarioSession(session *domain.ScenarioSession) {
	_ = s.upsertScenarioSession(context.Background(), session)
}

func (s *PostgresStore) AddScenarioMessage(message domain.ScenarioMessage) domain.ScenarioMessage {
	if message.ID == "" {
		message.ID = NewID()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	metaJSON, err := marshal(message.ResponseMeta)
	if err != nil {
		return message
	}
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO scenario_messages
		    (id, session_id, turn_number, role, user_content, assistant_content, response_meta, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (id) DO UPDATE SET
		    turn_number = EXCLUDED.turn_number,
		    role = EXCLUDED.role,
		    user_content = EXCLUDED.user_content,
		    assistant_content = EXCLUDED.assistant_content,
		    response_meta = EXCLUDED.response_meta
	`, message.ID, message.SessionID, message.TurnNumber, message.Role, message.UserContent,
		message.AssistantContent, metaJSON, message.CreatedAt)
	return message
}

func (s *PostgresStore) ListScenarioMessages(sessionID string) []domain.ScenarioMessage {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, session_id, turn_number, role, user_content, assistant_content, response_meta, created_at
		FROM scenario_messages
		WHERE session_id = $1
		ORDER BY turn_number ASC, created_at ASC
	`, sessionID)
	if err != nil {
		return []domain.ScenarioMessage{}
	}
	defer rows.Close()

	items := []domain.ScenarioMessage{}
	for rows.Next() {
		var item domain.ScenarioMessage
		var metaJSON []byte
		if err := rows.Scan(&item.ID, &item.SessionID, &item.TurnNumber, &item.Role, &item.UserContent,
			&item.AssistantContent, &metaJSON, &item.CreatedAt); err != nil {
			continue
		}
		_ = unmarshal(metaJSON, &item.ResponseMeta)
		items = append(items, item)
	}
	return items
}

func (s *PostgresStore) ListScenarioSessionsForUser(userID string) []domain.ScenarioSession {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, user_id, question_id, status, current_turn, max_turns, revealed_clue_ids,
		       user_answer, evaluation_result, score, question_snapshot, hint_level,
		       no_new_clue_streak, COALESCE(conversation_summary, ''), started_at, last_active_at, ended_at
		FROM scenario_sessions
		WHERE user_id = $1
		ORDER BY started_at DESC
	`, userID)
	if err != nil {
		return []domain.ScenarioSession{}
	}
	defer rows.Close()

	items := []domain.ScenarioSession{}
	for rows.Next() {
		item, err := scanScenarioSessionRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) FindInterviewQuestion(domainName, difficulty, questionType string) (*domain.InterviewQuestion, bool) {
	domainName = strings.TrimSpace(domainName)
	difficulty = strings.TrimSpace(difficulty)
	questionType = strings.TrimSpace(questionType)
	if domainName == "" || difficulty == "" || questionType == "" {
		return nil, false
	}
	items := s.listInterviewQuestions()
	for _, question := range items {
		if question.Domain != domainName {
			continue
		}
		if question.Difficulty != difficulty {
			continue
		}
		if question.QuestionType != questionType {
			continue
		}
		item := question
		return &item, true
	}
	return nil, false
}

func (s *PostgresStore) GetInterviewQuestion(id string) (*domain.InterviewQuestion, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, title, description, domain, difficulty, question_type, reference_answer,
		       reference_keywords, evaluation_dimensions, follow_up_strategies, created_at
		FROM interview_questions
		WHERE id = $1
	`, id)
	return scanInterviewQuestion(row)
}

func (s *PostgresStore) CreateInterviewSession(userID string, question *domain.InterviewQuestion) *domain.InterviewSession {
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
	_ = s.upsertInterviewSession(context.Background(), session)
	return cloneInterviewSession(session)
}

func (s *PostgresStore) GetInterviewSession(id string) (*domain.InterviewSession, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, user_id, question_id, status, current_round, max_rounds, submissions,
		       evaluations, follow_up_question, final_score, final_report, started_at, ended_at
		FROM interview_sessions
		WHERE id = $1
	`, id)
	return scanInterviewSession(row)
}

func (s *PostgresStore) SaveInterviewSession(session *domain.InterviewSession) {
	_ = s.upsertInterviewSession(context.Background(), session)
}

func (s *PostgresStore) ListInterviewSessionsForUser(userID string) []domain.InterviewSession {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, user_id, question_id, status, current_round, max_rounds, submissions,
		       evaluations, follow_up_question, final_score, final_report, started_at, ended_at
		FROM interview_sessions
		WHERE user_id = $1
		ORDER BY started_at DESC
	`, userID)
	if err != nil {
		return []domain.InterviewSession{}
	}
	defer rows.Close()

	items := []domain.InterviewSession{}
	for rows.Next() {
		item, err := scanInterviewSessionRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) DeleteInterviewSession(id string) bool {
	commandTag, err := s.pool.Exec(context.Background(), `
		DELETE FROM interview_sessions
		WHERE id = $1
	`, id)
	if err != nil {
		return false
	}
	return commandTag.RowsAffected() > 0
}

func (s *PostgresStore) AddCommunityPost(post domain.CommunityPost) domain.CommunityPost {
	post = prepareCommunityPostForPersistence(post)
	if post.ID == "" {
		post.ID = NewID()
	}
	now := time.Now()
	if post.CreatedAt.IsZero() {
		post.CreatedAt = now
	}
	post.UpdatedAt = now
	contentJSON, err := marshal(post.AIStructuredContent)
	if err != nil {
		return post
	}
	editedJSON, err := marshalNullable(post.EditedStructuredContent)
	if err != nil {
		return post
	}
	moderationJSON, err := marshalNullable(post.ModerationSummary)
	if err != nil {
		return post
	}
	historyJSON, err := marshal(post.ReviewHistory)
	if err != nil {
		return post
	}
	sensitiveJSON, err := marshal(post.SensitiveCheck)
	if err != nil {
		return post
	}
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO community_posts
		    (id, user_id, title, raw_content, domain, tags, forked_from_scenario_id, ai_structured_content,
		     edited_structured_content, moderation_summary, review_history, sensitive_check, converted_question_id, status,
		     reviewed_by, reviewed_at, review_note, finalized_by, finalized_at, final_note, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		ON CONFLICT (id) DO UPDATE SET
		    title = EXCLUDED.title,
		    raw_content = EXCLUDED.raw_content,
		    domain = EXCLUDED.domain,
		    tags = EXCLUDED.tags,
		    forked_from_scenario_id = EXCLUDED.forked_from_scenario_id,
		    ai_structured_content = EXCLUDED.ai_structured_content,
		    edited_structured_content = EXCLUDED.edited_structured_content,
		    moderation_summary = EXCLUDED.moderation_summary,
		    review_history = EXCLUDED.review_history,
		    sensitive_check = EXCLUDED.sensitive_check,
		    converted_question_id = EXCLUDED.converted_question_id,
		    status = EXCLUDED.status,
		    reviewed_by = EXCLUDED.reviewed_by,
		    reviewed_at = EXCLUDED.reviewed_at,
		    review_note = EXCLUDED.review_note,
		    finalized_by = EXCLUDED.finalized_by,
		    finalized_at = EXCLUDED.finalized_at,
		    final_note = EXCLUDED.final_note,
		    updated_at = EXCLUDED.updated_at
	`, post.ID, post.UserID, post.Title, post.RawContent, post.Domain, post.Tags,
		emptyToNil(post.ForkedFromScenarioID), contentJSON, editedJSON, moderationJSON, historyJSON, sensitiveJSON,
		emptyToNil(post.ConvertedQuestionID), post.Status, emptyToNil(post.ReviewedBy), post.ReviewedAt,
		emptyToNil(post.ReviewNote), emptyToNil(post.FinalizedBy), post.FinalizedAt,
		emptyToNil(post.FinalNote), post.CreatedAt, post.UpdatedAt)
	return post
}

func (s *PostgresStore) GetCommunityPost(id string) (*domain.CommunityPost, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, user_id, title, raw_content, domain, tags, COALESCE(forked_from_scenario_id, ''),
		       ai_structured_content, edited_structured_content, moderation_summary, COALESCE(review_history, '[]'::jsonb),
		       COALESCE(sensitive_check, '{}'::jsonb), COALESCE(converted_question_id, ''), status,
		       reviewed_by, reviewed_at, review_note, finalized_by, finalized_at, final_note,
		       created_at, COALESCE(updated_at, created_at)
		FROM community_posts
		WHERE id = $1
	`, id)
	return scanCommunityPost(row)
}

func (s *PostgresStore) SaveCommunityPost(post *domain.CommunityPost) domain.CommunityPost {
	if post == nil {
		return domain.CommunityPost{}
	}
	return s.AddCommunityPost(*post)
}

func (s *PostgresStore) ListCommunityPosts() []domain.CommunityPost {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, user_id, title, raw_content, domain, tags, COALESCE(forked_from_scenario_id, ''),
		       ai_structured_content, edited_structured_content, moderation_summary, COALESCE(review_history, '[]'::jsonb),
		       COALESCE(sensitive_check, '{}'::jsonb), COALESCE(converted_question_id, ''), status,
		       reviewed_by, reviewed_at, review_note, finalized_by, finalized_at, final_note,
		       created_at, COALESCE(updated_at, created_at)
		FROM community_posts
		ORDER BY created_at DESC
	`)
	if err != nil {
		return []domain.CommunityPost{}
	}
	defer rows.Close()

	items := []domain.CommunityPost{}
	for rows.Next() {
		item, err := scanCommunityPostRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) DeleteCommunityPost(id string) bool {
	tag, err := s.pool.Exec(context.Background(), `DELETE FROM community_posts WHERE id = $1`, id)
	return err == nil && tag.RowsAffected() > 0
}

func (s *PostgresStore) CreateAsset(asset domain.Asset) (domain.Asset, error) {
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
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO assets (id, user_id, kind, filename, mime_type, size, storage_key, url, checksum, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
		    filename = EXCLUDED.filename,
		    mime_type = EXCLUDED.mime_type,
		    size = EXCLUDED.size,
		    storage_key = EXCLUDED.storage_key,
		    url = EXCLUDED.url,
		    checksum = EXCLUDED.checksum
	`, asset.ID, asset.UserID, asset.Kind, asset.Filename, asset.MimeType, asset.Size,
		asset.StorageKey, asset.URL, emptyToNil(asset.Checksum), asset.CreatedAt)
	if err != nil {
		return domain.Asset{}, err
	}
	return asset, nil
}

func (s *PostgresStore) GetAsset(id string) (*domain.Asset, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, user_id, kind, filename, mime_type, size, storage_key, url, COALESCE(checksum, ''), created_at
		FROM assets
		WHERE id = $1
	`, id)
	return scanAsset(row)
}

func (s *PostgresStore) ListAssetsForUser(userID string) []domain.Asset {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, user_id, kind, filename, mime_type, size, storage_key, url, COALESCE(checksum, ''), created_at
		FROM assets
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return []domain.Asset{}
	}
	defer rows.Close()
	items := []domain.Asset{}
	for rows.Next() {
		item, err := scanAssetRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) CreateAIJob(job domain.AIJob) (domain.AIJob, error) {
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
	if err := s.upsertAIJob(context.Background(), &job); err != nil {
		return domain.AIJob{}, err
	}
	return job, nil
}

func (s *PostgresStore) GetAIJob(id string) (*domain.AIJob, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT id, user_id, kind, status, stage, progress, error_message, provider, model,
		       validated, fallback_used, result_question_id, created_at, started_at,
		       completed_at, updated_at
		FROM ai_jobs
		WHERE id = $1
	`, id)
	return scanAIJob(row)
}

func (s *PostgresStore) SaveAIJob(job *domain.AIJob) (domain.AIJob, error) {
	if job == nil {
		return domain.AIJob{}, nil
	}
	if job.ID == "" {
		job.ID = NewID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()
	if err := s.upsertAIJob(context.Background(), job); err != nil {
		return domain.AIJob{}, err
	}
	return *job, nil
}

func (s *PostgresStore) ListAIJobs(limit int) []domain.AIJob {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, user_id, kind, status, stage, progress, error_message, provider, model,
		       validated, fallback_used, result_question_id, created_at, started_at,
		       completed_at, updated_at
		FROM ai_jobs
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return []domain.AIJob{}
	}
	defer rows.Close()
	items := []domain.AIJob{}
	for rows.Next() {
		item, ok := scanAIJob(rows)
		if ok {
			items = append(items, *item)
		}
	}
	return items
}

func (s *PostgresStore) CountAIJobs() int {
	var count int
	if err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ai_jobs`).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s *PostgresStore) ListPromptTemplates() []domain.PromptTemplate {
	rows, err := s.pool.Query(context.Background(), promptTemplateListSelectSQL)
	if err != nil {
		return []domain.PromptTemplate{}
	}
	defer rows.Close()
	items := []domain.PromptTemplate{}
	for rows.Next() {
		item, err := scanPromptTemplateRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) GetPromptTemplate(name string) (*domain.PromptTemplate, bool) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT name, COALESCE(task, ''), COALESCE(default_content, ''), content, COALESCE(render_engine, 'go_template'),
		       COALESCE(updated_by, ''), updated_at, COALESCE(validator, '')
		FROM prompt_templates
		WHERE name = $1
	`, name)
	return scanPromptTemplate(row)
}

func (s *PostgresStore) SavePromptTemplate(template domain.PromptTemplate) (domain.PromptTemplate, error) {
	if strings.TrimSpace(template.Name) == "" {
		return domain.PromptTemplate{}, errors.New("prompt name is required")
	}
	if strings.TrimSpace(template.Content) == "" {
		return domain.PromptTemplate{}, errors.New("prompt content is required")
	}
	existing, _ := s.GetPromptTemplate(template.Name)
	if existing != nil {
		if template.Default == "" {
			template.Default = existing.Default
		}
		if template.Task == "" {
			template.Task = existing.Task
		}
		if template.Validator == "" {
			template.Validator = existing.Validator
		}
		if template.RenderEngine == "" {
			template.RenderEngine = existing.RenderEngine
		}
	}
	if template.UpdatedAt.IsZero() {
		template.UpdatedAt = time.Now()
	}
	if strings.TrimSpace(template.RenderEngine) == "" {
		template.RenderEngine = "go_template"
	}
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO prompt_templates (name, task, default_content, content, render_engine, updated_by, updated_at, validator)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (name) DO UPDATE SET
		    task = EXCLUDED.task,
		    default_content = EXCLUDED.default_content,
		    content = EXCLUDED.content,
		    render_engine = EXCLUDED.render_engine,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = EXCLUDED.updated_at,
		    validator = EXCLUDED.validator
	`, template.Name, template.Task, template.Default, template.Content, emptyToNil(template.RenderEngine),
		emptyToNil(template.UpdatedBy), template.UpdatedAt, template.Validator)
	if err != nil {
		return domain.PromptTemplate{}, err
	}
	template.IsModified = template.Content != template.Default || template.RenderEngine != "go_template"
	return template, nil
}

func (s *PostgresStore) GetAIConfig() domain.AIConfig {
	row := s.pool.QueryRow(context.Background(), `
		SELECT provider, model, COALESCE(base_url, ''), COALESCE(temperature, 0.2), COALESCE(top_p, 0), COALESCE(top_k, 0), COALESCE(max_tokens, 0), stream_enabled, COALESCE(fallback_model, ''),
		       COALESCE(updated_by, ''), updated_at
		FROM ai_config
		WHERE id = 'default'
	`)
	config, ok := scanAIConfig(row)
	if !ok {
		return domain.AIConfig{Provider: "mock", Model: "mock", Temperature: 0.2, StreamEnabled: true, FallbackModel: "mock", UpdatedAt: time.Now()}
	}
	return *config
}

func (s *PostgresStore) SaveAIConfig(config domain.AIConfig) (domain.AIConfig, error) {
	if strings.TrimSpace(config.Provider) == "" {
		return domain.AIConfig{}, errors.New("provider is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return domain.AIConfig{}, errors.New("model is required")
	}
	if config.UpdatedAt.IsZero() {
		config.UpdatedAt = time.Now()
	}
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO ai_config (id, provider, model, base_url, temperature, top_p, top_k, max_tokens, stream_enabled, fallback_model, updated_by, updated_at)
		VALUES ('default',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
		    provider = EXCLUDED.provider,
		    model = EXCLUDED.model,
		    base_url = EXCLUDED.base_url,
		    temperature = EXCLUDED.temperature,
		    top_p = EXCLUDED.top_p,
		    top_k = EXCLUDED.top_k,
		    max_tokens = EXCLUDED.max_tokens,
		    stream_enabled = EXCLUDED.stream_enabled,
		    fallback_model = EXCLUDED.fallback_model,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = EXCLUDED.updated_at
	`, config.Provider, config.Model, emptyToNil(config.BaseURL), config.Temperature, config.TopP, config.TopK, config.MaxTokens, config.StreamEnabled,
		emptyToNil(config.FallbackModel), emptyToNil(config.UpdatedBy), config.UpdatedAt)
	if err != nil {
		return domain.AIConfig{}, err
	}
	return config, nil
}

func (s *PostgresStore) RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent {
	if event.ID == "" {
		event.ID = NewID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	metadataJSON, err := marshal(event.Metadata)
	if err != nil {
		return event
	}
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO audit_events
		    (id, actor_id, action, resource_type, resource_id, ip_address, user_agent, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, event.ID, emptyToNil(event.ActorID), event.Action, event.ResourceType,
		emptyToNil(event.ResourceID), emptyToNil(event.IPAddress), emptyToNil(event.UserAgent),
		metadataJSON, event.CreatedAt)
	return event
}

func (s *PostgresStore) ListAuditEvents(limit int) []domain.AuditEvent {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, COALESCE(actor_id, ''), action, resource_type, COALESCE(resource_id, ''),
		       COALESCE(ip_address, ''), COALESCE(user_agent, ''), COALESCE(metadata, '{}'::jsonb), created_at
		FROM audit_events
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return []domain.AuditEvent{}
	}
	defer rows.Close()
	items := []domain.AuditEvent{}
	for rows.Next() {
		item, err := scanAuditEventRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) RecordScenarioScore(userID, domainName string, score int) {
	s.recordScore(userID, domainName, score, true)
}

func (s *PostgresStore) RecordInterviewScore(userID, domainName string, score int) {
	s.recordScore(userID, domainName, score, false)
}

func (s *PostgresStore) upsertUser(ctx context.Context, user *domain.User) error {
	profileJSON, err := marshal(user.Profile)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO users (id, username, email, password_hash, role, profile, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (id) DO UPDATE SET
		    username = EXCLUDED.username,
		    email = EXCLUDED.email,
		    password_hash = EXCLUDED.password_hash,
		    role = EXCLUDED.role,
		    profile = EXCLUDED.profile,
		    updated_at = NOW()
	`, user.ID, user.Username, user.Email, user.PasswordHash, user.Role, profileJSON, user.CreatedAt)
	return err
}

func (s *PostgresStore) insertUser(ctx context.Context, user *domain.User) error {
	profileJSON, err := marshal(user.Profile)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO users (id, username, email, password_hash, role, profile, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
	`, user.ID, user.Username, user.Email, user.PasswordHash, user.Role, profileJSON, user.CreatedAt)
	return err
}

func (s *PostgresStore) insertScenarioSession(ctx context.Context, session *domain.ScenarioSession) error {
	return s.upsertScenarioSession(ctx, session)
}

func (s *PostgresStore) upsertScenarioSession(ctx context.Context, session *domain.ScenarioSession) error {
	evaluationJSON, err := marshalNullable(session.EvaluationResult)
	if err != nil {
		return err
	}
	scoreJSON, err := marshalNullable(session.Score)
	if err != nil {
		return err
	}
	snapshotJSON, err := marshal(session.QuestionSnapshot)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO scenario_sessions
		    (id, user_id, question_id, status, current_turn, max_turns, revealed_clue_ids, user_answer,
		     evaluation_result, score, question_snapshot, hint_level, no_new_clue_streak,
		     conversation_summary, started_at, last_active_at, ended_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (id) DO UPDATE SET
		    status = EXCLUDED.status,
		    current_turn = EXCLUDED.current_turn,
		    max_turns = EXCLUDED.max_turns,
		    revealed_clue_ids = EXCLUDED.revealed_clue_ids,
		    user_answer = EXCLUDED.user_answer,
		    evaluation_result = EXCLUDED.evaluation_result,
		    score = EXCLUDED.score,
		    question_snapshot = EXCLUDED.question_snapshot,
		    hint_level = EXCLUDED.hint_level,
		    no_new_clue_streak = EXCLUDED.no_new_clue_streak,
		    conversation_summary = EXCLUDED.conversation_summary,
		    last_active_at = EXCLUDED.last_active_at,
		    ended_at = EXCLUDED.ended_at
	`, session.ID, session.UserID, session.QuestionID, session.Status, session.CurrentTurn,
		session.MaxTurns, session.RevealedClueIDs, emptyToNil(session.UserAnswer), evaluationJSON,
		scoreJSON, snapshotJSON, session.HintLevel, session.NoNewClueStreak,
		session.ConversationSummary, session.StartedAt, session.LastActiveAt, session.EndedAt)
	return err
}

func (s *PostgresStore) upsertInterviewQuestion(ctx context.Context, question domain.InterviewQuestion) error {
	dimensionsJSON, err := marshal(question.EvaluationDimensions)
	if err != nil {
		return err
	}
	strategiesJSON, err := marshal(question.FollowUpStrategies)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO interview_questions
		    (id, title, description, domain, difficulty, question_type, reference_answer,
		     reference_keywords, evaluation_dimensions, follow_up_strategies, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
		    title = EXCLUDED.title,
		    description = EXCLUDED.description,
		    domain = EXCLUDED.domain,
		    difficulty = EXCLUDED.difficulty,
		    question_type = EXCLUDED.question_type,
		    reference_answer = EXCLUDED.reference_answer,
		    reference_keywords = EXCLUDED.reference_keywords,
		    evaluation_dimensions = EXCLUDED.evaluation_dimensions,
		    follow_up_strategies = EXCLUDED.follow_up_strategies
	`, question.ID, question.Title, question.Description, question.Domain, question.Difficulty,
		question.QuestionType, question.ReferenceAnswer, question.ReferenceKeywords,
		dimensionsJSON, strategiesJSON, question.CreatedAt)
	return err
}

func (s *PostgresStore) upsertAIJob(ctx context.Context, job *domain.AIJob) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ai_jobs
		    (id, user_id, kind, status, stage, progress, error_message, provider, model,
		     validated, fallback_used, result_question_id, created_at, started_at,
		     completed_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (id) DO UPDATE SET
		    status = EXCLUDED.status,
		    stage = EXCLUDED.stage,
		    progress = EXCLUDED.progress,
		    error_message = EXCLUDED.error_message,
		    provider = EXCLUDED.provider,
		    model = EXCLUDED.model,
		    validated = EXCLUDED.validated,
		    fallback_used = EXCLUDED.fallback_used,
		    result_question_id = EXCLUDED.result_question_id,
		    started_at = EXCLUDED.started_at,
		    completed_at = EXCLUDED.completed_at,
		    updated_at = EXCLUDED.updated_at
	`, job.ID, job.UserID, job.Kind, job.Status, job.Stage, job.Progress,
		emptyToNil(job.ErrorMessage), emptyToNil(job.Provider), emptyToNil(job.Model),
		job.Validated, job.FallbackUsed, emptyToNil(job.ResultQuestionID), job.CreatedAt,
		job.StartedAt, job.CompletedAt, job.UpdatedAt)
	return err
}

func (s *PostgresStore) seedAdminConfig(ctx context.Context) error {
	now := time.Now()
	scenarioGeneratePrompt := ai.DefaultPromptContent("scenario_generate")
	templates := []domain.PromptTemplate{
		{Name: "scenario_generate", Task: "情景题生成", Default: scenarioGeneratePrompt, Content: scenarioGeneratePrompt, Validator: "scenario_question", RenderEngine: "go_template", UpdatedAt: now},
		{Name: "community_structure", Task: "UGC 结构化", Default: "从真实故障案例中提取现象、根因、证据和排查步骤。", Content: "从真实故障案例中提取现象、根因、证据和排查步骤。", Validator: "scenario_content_preview", RenderEngine: "go_template", UpdatedAt: now},
		{Name: "interview_feedback", Task: "面试评估", Default: "按五个维度生成面试反馈、追问和最终报告。", Content: "按五个维度生成面试反馈、追问和最终报告。", Validator: "interview_feedback", RenderEngine: "go_template", UpdatedAt: now},
		{Name: "scenario_reply", Task: "排查回复改写", Default: "在不泄露答案的前提下改写渐进式排查回复。", Content: "在不泄露答案的前提下改写渐进式排查回复。", Validator: "scenario_reply", RenderEngine: "go_template", UpdatedAt: now},
	}
	for _, template := range templates {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO prompt_templates (name, task, default_content, content, render_engine, updated_at, validator)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (name) DO UPDATE SET
			    task = EXCLUDED.task,
			    default_content = EXCLUDED.default_content,
			    content = CASE
			        WHEN prompt_templates.content = prompt_templates.default_content THEN EXCLUDED.content
			        ELSE prompt_templates.content
			    END,
			    render_engine = CASE
			        WHEN prompt_templates.content = prompt_templates.default_content THEN EXCLUDED.render_engine
			        ELSE prompt_templates.render_engine
			    END,
			    validator = EXCLUDED.validator
		`, template.Name, template.Task, template.Default, template.Content, template.RenderEngine, template.UpdatedAt, template.Validator)
		if err != nil {
			return err
		}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ai_config (id, provider, model, temperature, top_p, top_k, max_tokens, stream_enabled, fallback_model, updated_at)
		VALUES ('default', 'mock', 'mock', 0.2, 0, 0, 0, TRUE, 'mock', $1)
		ON CONFLICT (id) DO NOTHING
	`, now)
	return err
}

func (s *PostgresStore) upsertInterviewSession(ctx context.Context, session *domain.InterviewSession) error {
	submissionsJSON, err := marshal(session.Submissions)
	if err != nil {
		return err
	}
	evaluationsJSON, err := marshal(session.Evaluations)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO interview_sessions
		    (id, user_id, question_id, status, current_round, max_rounds, submissions,
		     evaluations, follow_up_question, final_score, final_report, started_at, ended_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET
		    status = EXCLUDED.status,
		    current_round = EXCLUDED.current_round,
		    max_rounds = EXCLUDED.max_rounds,
		    submissions = EXCLUDED.submissions,
		    evaluations = EXCLUDED.evaluations,
		    follow_up_question = EXCLUDED.follow_up_question,
		    final_score = EXCLUDED.final_score,
		    final_report = EXCLUDED.final_report,
		    ended_at = EXCLUDED.ended_at
	`, session.ID, session.UserID, session.QuestionID, session.Status, session.CurrentRound,
		session.MaxRounds, submissionsJSON, evaluationsJSON, emptyToNil(session.FollowUpQuestion),
		zeroToNil(session.FinalScore), emptyToNil(session.FinalReport), session.StartedAt, session.EndedAt)
	return err
}

func (s *PostgresStore) listInterviewQuestions() []domain.InterviewQuestion {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, title, description, domain, difficulty, question_type, reference_answer,
		       reference_keywords, evaluation_dimensions, follow_up_strategies, created_at
		FROM interview_questions
		ORDER BY created_at DESC
	`)
	if err != nil {
		return []domain.InterviewQuestion{}
	}
	defer rows.Close()
	items := []domain.InterviewQuestion{}
	for rows.Next() {
		item, err := scanInterviewQuestionRows(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items
}

func (s *PostgresStore) recordScore(userID, domainName string, score int, scenario bool) {
	user, ok := s.GetUser(userID)
	if !ok {
		return
	}
	stats := &user.Profile.TotalStats
	total := stats.AverageScore * (stats.ScenariosSolved + stats.InterviewsTaken)
	total += score
	if scenario {
		stats.ScenariosSolved++
	} else {
		stats.InterviewsTaken++
	}
	count := stats.ScenariosSolved + stats.InterviewsTaken
	if count > 0 {
		stats.AverageScore = total / count
	}
	user.Profile.CapabilityRadar[domainName] = rolling(user.Profile.CapabilityRadar[domainName], score)
	user.Profile.UpdatedAt = time.Now()
	profileJSON, err := marshal(user.Profile)
	if err != nil {
		return
	}
	_, _ = s.pool.Exec(context.Background(), `UPDATE users SET profile = $2, updated_at = NOW() WHERE id = $1`, userID, profileJSON)
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanUser(row scanner) (*domain.User, bool) {
	var user domain.User
	var profileJSON []byte
	if err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.Role, &profileJSON, &user.CreatedAt); err != nil {
		return nil, false
	}
	if err := unmarshal(profileJSON, &user.Profile); err != nil {
		user.Profile = defaultProfile()
	}
	return &user, true
}

func scanScenario(row scanner) (*domain.ScenarioQuestion, bool) {
	var item domain.ScenarioQuestion
	var contentJSON []byte
	if err := row.Scan(&item.ID, &item.Title, &item.Description, &item.Domain, &item.Difficulty,
		&item.ScenarioType, &item.Tags, &contentJSON, &item.Status, &item.Source, &item.CreatedBy,
		&item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, false
	}
	_ = unmarshal(contentJSON, &item.Content)
	return &item, true
}

func scanScenarioRows(rows pgx.Rows) (domain.ScenarioQuestion, error) {
	var item domain.ScenarioQuestion
	var contentJSON []byte
	err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Domain, &item.Difficulty,
		&item.ScenarioType, &item.Tags, &contentJSON, &item.Status, &item.Source, &item.CreatedBy,
		&item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	_ = unmarshal(contentJSON, &item.Content)
	return item, nil
}

func scanScenarioSession(row scanner) (*domain.ScenarioSession, bool) {
	item, err := scanScenarioSessionScanner(row)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanScenarioSessionRows(rows pgx.Rows) (domain.ScenarioSession, error) {
	return scanScenarioSessionScanner(rows)
}

func scanScenarioSessionScanner(row scanner) (domain.ScenarioSession, error) {
	var item domain.ScenarioSession
	var userAnswer *string
	var endedAt *time.Time
	var evaluationJSON, scoreJSON, snapshotJSON []byte
	err := row.Scan(&item.ID, &item.UserID, &item.QuestionID, &item.Status, &item.CurrentTurn,
		&item.MaxTurns, &item.RevealedClueIDs, &userAnswer, &evaluationJSON, &scoreJSON,
		&snapshotJSON, &item.HintLevel, &item.NoNewClueStreak, &item.ConversationSummary, &item.StartedAt,
		&item.LastActiveAt, &endedAt)
	if err != nil {
		return item, err
	}
	if userAnswer != nil {
		item.UserAnswer = *userAnswer
	}
	item.EndedAt = endedAt
	if len(evaluationJSON) > 0 {
		var evaluation domain.ScenarioEvaluation
		if err := unmarshal(evaluationJSON, &evaluation); err == nil {
			item.EvaluationResult = &evaluation
		}
	}
	if len(scoreJSON) > 0 {
		var score domain.ScenarioScore
		if err := unmarshal(scoreJSON, &score); err == nil {
			item.Score = &score
		}
	}
	_ = unmarshal(snapshotJSON, &item.QuestionSnapshot)
	if item.RevealedClueIDs == nil {
		item.RevealedClueIDs = []string{}
	}
	return item, nil
}

func scanInterviewQuestion(row scanner) (*domain.InterviewQuestion, bool) {
	item, err := scanInterviewQuestionScanner(row)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanInterviewQuestionRows(rows pgx.Rows) (domain.InterviewQuestion, error) {
	return scanInterviewQuestionScanner(rows)
}

func scanInterviewQuestionScanner(row scanner) (domain.InterviewQuestion, error) {
	var item domain.InterviewQuestion
	var dimensionsJSON, strategiesJSON []byte
	err := row.Scan(&item.ID, &item.Title, &item.Description, &item.Domain, &item.Difficulty,
		&item.QuestionType, &item.ReferenceAnswer, &item.ReferenceKeywords, &dimensionsJSON,
		&strategiesJSON, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	_ = unmarshal(dimensionsJSON, &item.EvaluationDimensions)
	_ = unmarshal(strategiesJSON, &item.FollowUpStrategies)
	return item, nil
}

func scanInterviewSession(row scanner) (*domain.InterviewSession, bool) {
	item, err := scanInterviewSessionScanner(row)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanInterviewSessionRows(rows pgx.Rows) (domain.InterviewSession, error) {
	return scanInterviewSessionScanner(rows)
}

func scanInterviewSessionScanner(row scanner) (domain.InterviewSession, error) {
	var item domain.InterviewSession
	var submissionsJSON, evaluationsJSON []byte
	var followUpQuestion, finalReport *string
	var finalScore *int
	var endedAt *time.Time
	err := row.Scan(&item.ID, &item.UserID, &item.QuestionID, &item.Status, &item.CurrentRound,
		&item.MaxRounds, &submissionsJSON, &evaluationsJSON, &followUpQuestion, &finalScore,
		&finalReport, &item.StartedAt, &endedAt)
	if err != nil {
		return item, err
	}
	_ = unmarshal(submissionsJSON, &item.Submissions)
	_ = unmarshal(evaluationsJSON, &item.Evaluations)
	if followUpQuestion != nil {
		item.FollowUpQuestion = *followUpQuestion
	}
	if finalScore != nil {
		item.FinalScore = *finalScore
	}
	if finalReport != nil {
		item.FinalReport = *finalReport
	}
	item.EndedAt = endedAt
	if item.Submissions == nil {
		item.Submissions = []domain.InterviewSubmission{}
	}
	if item.Evaluations == nil {
		item.Evaluations = []domain.InterviewEvaluation{}
	}
	return item, nil
}

func scanCommunityPost(row scanner) (*domain.CommunityPost, bool) {
	item, err := scanCommunityPostScanner(row)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanCommunityPostRows(rows pgx.Rows) (domain.CommunityPost, error) {
	return scanCommunityPostScanner(rows)
}

func scanCommunityPostScanner(row scanner) (domain.CommunityPost, error) {
	var item domain.CommunityPost
	var contentJSON []byte
	var editedJSON []byte
	var moderationJSON []byte
	var historyJSON []byte
	var sensitiveJSON []byte
	var reviewedBy, reviewNote, finalizedBy, finalNote *string
	err := row.Scan(&item.ID, &item.UserID, &item.Title, &item.RawContent, &item.Domain,
		&item.Tags, &item.ForkedFromScenarioID, &contentJSON, &editedJSON, &moderationJSON, &historyJSON,
		&sensitiveJSON, &item.ConvertedQuestionID,
		&item.Status, &reviewedBy, &item.ReviewedAt, &reviewNote, &finalizedBy, &item.FinalizedAt, &finalNote,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	_ = unmarshal(contentJSON, &item.AIStructuredContent)
	if len(editedJSON) > 0 {
		var edited domain.ScenarioContent
		if err := unmarshal(editedJSON, &edited); err == nil {
			item.EditedStructuredContent = &edited
		}
	}
	if len(moderationJSON) > 0 && string(moderationJSON) != "null" {
		var moderation domain.ModerationSummary
		if err := unmarshal(moderationJSON, &moderation); err == nil {
			item.ModerationSummary = &moderation
		}
	}
	_ = unmarshal(historyJSON, &item.ReviewHistory)
	_ = unmarshal(sensitiveJSON, &item.SensitiveCheck)
	if reviewedBy != nil {
		item.ReviewedBy = *reviewedBy
	}
	if reviewNote != nil {
		item.ReviewNote = *reviewNote
	}
	if finalizedBy != nil {
		item.FinalizedBy = *finalizedBy
	}
	if finalNote != nil {
		item.FinalNote = *finalNote
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.ReviewHistory == nil {
		item.ReviewHistory = []domain.ReviewHistoryItem{}
	}
	if item.SensitiveCheck.Findings == nil {
		item.SensitiveCheck.Findings = []domain.SensitiveFinding{}
	}
	return item, nil
}

func scanAsset(row scanner) (*domain.Asset, bool) {
	item, err := scanAssetScanner(row)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanAssetRows(rows pgx.Rows) (domain.Asset, error) {
	return scanAssetScanner(rows)
}

func scanAssetScanner(row scanner) (domain.Asset, error) {
	var item domain.Asset
	err := row.Scan(&item.ID, &item.UserID, &item.Kind, &item.Filename, &item.MimeType,
		&item.Size, &item.StorageKey, &item.URL, &item.Checksum, &item.CreatedAt)
	return item, err
}

func scanPromptTemplate(row scanner) (*domain.PromptTemplate, bool) {
	item, err := scanPromptTemplateScanner(row)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanPromptTemplateRows(rows pgx.Rows) (domain.PromptTemplate, error) {
	return scanPromptTemplateScanner(rows)
}

func scanPromptTemplateScanner(row scanner) (domain.PromptTemplate, error) {
	var item domain.PromptTemplate
	err := row.Scan(&item.Name, &item.Task, &item.Default, &item.Content, &item.RenderEngine, &item.UpdatedBy, &item.UpdatedAt, &item.Validator)
	if err != nil {
		return item, err
	}
	item.IsModified = item.Content != item.Default || strings.TrimSpace(item.RenderEngine) != "" && item.RenderEngine != "go_template"
	return item, nil
}

func scanAIConfig(row scanner) (*domain.AIConfig, bool) {
	var item domain.AIConfig
	err := row.Scan(&item.Provider, &item.Model, &item.BaseURL, &item.Temperature, &item.TopP, &item.TopK, &item.MaxTokens, &item.StreamEnabled,
		&item.FallbackModel, &item.UpdatedBy, &item.UpdatedAt)
	if err != nil {
		return nil, false
	}
	return &item, true
}

func scanAuditEventRows(rows pgx.Rows) (domain.AuditEvent, error) {
	var item domain.AuditEvent
	var metadataJSON []byte
	err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.ResourceType, &item.ResourceID,
		&item.IPAddress, &item.UserAgent, &metadataJSON, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	_ = unmarshal(metadataJSON, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]string{}
	}
	return item, nil
}

func scanAIJob(row scanner) (*domain.AIJob, bool) {
	var item domain.AIJob
	var stage, errorMessage, provider, model, resultQuestionID *string
	err := row.Scan(&item.ID, &item.UserID, &item.Kind, &item.Status, &stage,
		&item.Progress, &errorMessage, &provider, &model, &item.Validated, &item.FallbackUsed,
		&resultQuestionID, &item.CreatedAt, &item.StartedAt, &item.CompletedAt,
		&item.UpdatedAt)
	if err != nil {
		return nil, false
	}
	if stage != nil {
		item.Stage = *stage
	}
	if errorMessage != nil {
		item.ErrorMessage = *errorMessage
	}
	if provider != nil {
		item.Provider = *provider
	}
	if model != nil {
		item.Model = *model
	}
	if resultQuestionID != nil {
		item.ResultQuestionID = *resultQuestionID
	}
	return &item, true
}

func marshal(value interface{}) ([]byte, error) {
	return json.Marshal(value)
}

func marshalNullable(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func unmarshal(data []byte, target interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

func emptyToNil(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func zeroToNil(value int) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

func sortedScenarios(items []domain.ScenarioQuestion) []domain.ScenarioQuestion {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}
