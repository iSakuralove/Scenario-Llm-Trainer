package store

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(20) DEFAULT 'student',
    profile JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scenario_questions (
    id TEXT PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    description TEXT NOT NULL,
    domain VARCHAR(50) NOT NULL,
    difficulty VARCHAR(5) CHECK (difficulty IN ('L1','L2','L3','L4','L5')),
    scenario_type VARCHAR(30) CHECK (scenario_type IN ('troubleshooting','design','performance')),
    tags TEXT[],
    content JSONB NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    source VARCHAR(30) DEFAULT 'llm_generated',
    created_by TEXT REFERENCES users(id),
    version INT DEFAULT 1,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scenario_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    question_id TEXT NOT NULL REFERENCES scenario_questions(id),
    status VARCHAR(30) DEFAULT 'active',
    current_turn INT DEFAULT 0,
    max_turns INT DEFAULT 50,
    revealed_clue_ids TEXT[],
    user_answer TEXT,
    evaluation_result JSONB,
    score JSONB,
    question_snapshot JSONB NOT NULL,
    hint_level INT DEFAULT 1,
    no_new_clue_streak INT DEFAULT 0,
    conversation_summary TEXT DEFAULT '',
    started_at TIMESTAMPTZ DEFAULT NOW(),
    last_active_at TIMESTAMPTZ DEFAULT NOW(),
    ended_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS scenario_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES scenario_sessions(id) ON DELETE CASCADE,
    turn_number INT NOT NULL,
    role VARCHAR(20) NOT NULL,
    user_content TEXT,
    assistant_content TEXT,
    response_meta JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS interview_questions (
    id TEXT PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    description TEXT NOT NULL,
    domain VARCHAR(50) NOT NULL,
    difficulty VARCHAR(5),
    question_type VARCHAR(30),
    reference_answer TEXT,
    reference_keywords TEXT[],
    evaluation_dimensions JSONB NOT NULL,
    follow_up_strategies JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS interview_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    question_id TEXT NOT NULL REFERENCES interview_questions(id),
    status VARCHAR(50) DEFAULT 'question_presented',
    current_round INT DEFAULT 1,
    max_rounds INT DEFAULT 3,
    submissions JSONB DEFAULT '[]',
    evaluations JSONB DEFAULT '[]',
    follow_up_question TEXT,
    final_score INT,
    final_report TEXT,
    started_at TIMESTAMPTZ DEFAULT NOW(),
    ended_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS community_posts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    title VARCHAR(200) NOT NULL,
    raw_content TEXT NOT NULL,
    domain VARCHAR(50),
    tags TEXT[],
    forked_from_scenario_id TEXT,
    ai_structured_content JSONB,
    edited_structured_content JSONB,
    moderation_summary JSONB,
    review_history JSONB DEFAULT '[]',
    sensitive_check JSONB DEFAULT '{}',
    converted_question_id TEXT REFERENCES scenario_questions(id),
    status VARCHAR(20) DEFAULT 'draft',
    reviewed_by TEXT,
    reviewed_at TIMESTAMPTZ,
    review_note TEXT,
    finalized_by TEXT,
    finalized_at TIMESTAMPTZ,
    final_note TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS assets (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    kind VARCHAR(30) NOT NULL,
    filename TEXT,
    mime_type TEXT,
    size BIGINT DEFAULT 0,
    storage_key TEXT,
    url TEXT,
    checksum TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_jobs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    kind VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL,
    stage VARCHAR(50),
    progress INT DEFAULT 0,
    error_message TEXT,
    provider VARCHAR(50),
    model VARCHAR(100),
    validated BOOLEAN DEFAULT FALSE,
    fallback_used BOOLEAN DEFAULT FALSE,
    result_question_id TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS prompt_templates (
    name TEXT PRIMARY KEY,
    task TEXT,
    default_content TEXT,
    content TEXT NOT NULL,
    render_engine TEXT DEFAULT 'go_template',
    updated_by TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    validator TEXT
);

CREATE TABLE IF NOT EXISTS ai_config (
    id TEXT PRIMARY KEY DEFAULT 'default',
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    base_url TEXT,
    temperature DOUBLE PRECISION DEFAULT 0.2,
    top_p DOUBLE PRECISION DEFAULT 0,
    top_k INT DEFAULT 0,
    max_tokens INT DEFAULT 0,
    stream_enabled BOOLEAN DEFAULT TRUE,
    fallback_model TEXT,
    updated_by TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    ip_address TEXT,
    user_agent TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
`

const LegacyCompatibilitySQL = `
ALTER TABLE IF EXISTS scenario_questions DROP CONSTRAINT IF EXISTS scenario_questions_created_by_fkey;
ALTER TABLE IF EXISTS scenario_sessions DROP CONSTRAINT IF EXISTS scenario_sessions_user_id_fkey;
ALTER TABLE IF EXISTS scenario_sessions DROP CONSTRAINT IF EXISTS scenario_sessions_question_id_fkey;
ALTER TABLE IF EXISTS scenario_messages DROP CONSTRAINT IF EXISTS scenario_messages_session_id_fkey;
ALTER TABLE IF EXISTS interview_sessions DROP CONSTRAINT IF EXISTS interview_sessions_user_id_fkey;
ALTER TABLE IF EXISTS interview_sessions DROP CONSTRAINT IF EXISTS interview_sessions_question_id_fkey;
ALTER TABLE IF EXISTS community_posts DROP CONSTRAINT IF EXISTS community_posts_user_id_fkey;
ALTER TABLE IF EXISTS community_posts DROP CONSTRAINT IF EXISTS community_posts_converted_question_id_fkey;

ALTER TABLE IF EXISTS users ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS scenario_questions ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS scenario_questions ALTER COLUMN created_by TYPE TEXT USING created_by::text;
ALTER TABLE IF EXISTS scenario_sessions ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS scenario_sessions ALTER COLUMN user_id TYPE TEXT USING user_id::text;
ALTER TABLE IF EXISTS scenario_sessions ALTER COLUMN question_id TYPE TEXT USING question_id::text;
ALTER TABLE IF EXISTS scenario_messages ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS scenario_messages ALTER COLUMN session_id TYPE TEXT USING session_id::text;
ALTER TABLE IF EXISTS interview_questions ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS interview_sessions ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS interview_sessions ALTER COLUMN user_id TYPE TEXT USING user_id::text;
ALTER TABLE IF EXISTS interview_sessions ALTER COLUMN question_id TYPE TEXT USING question_id::text;
ALTER TABLE IF EXISTS community_posts ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE IF EXISTS community_posts ALTER COLUMN user_id TYPE TEXT USING user_id::text;
ALTER TABLE IF EXISTS community_posts ALTER COLUMN converted_question_id TYPE TEXT USING converted_question_id::text;

ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS question_snapshot JSONB;
ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS hint_level INT DEFAULT 1;
ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS no_new_clue_streak INT DEFAULT 0;
ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS conversation_summary TEXT DEFAULT '';
ALTER TABLE IF EXISTS interview_questions ADD COLUMN IF NOT EXISTS reference_keywords TEXT[];
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS follow_up_question TEXT;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS reviewed_by TEXT;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS review_note TEXT;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS finalized_by TEXT;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS finalized_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS final_note TEXT;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS edited_structured_content JSONB;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS moderation_summary JSONB;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS review_history JSONB DEFAULT '[]';
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS forked_from_scenario_id TEXT;
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS sensitive_check JSONB DEFAULT '{}';
ALTER TABLE IF EXISTS community_posts ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE IF EXISTS prompt_templates ADD COLUMN IF NOT EXISTS render_engine TEXT DEFAULT 'go_template';
ALTER TABLE IF EXISTS ai_config ADD COLUMN IF NOT EXISTS temperature DOUBLE PRECISION DEFAULT 0.2;
ALTER TABLE IF EXISTS ai_config ADD COLUMN IF NOT EXISTS top_p DOUBLE PRECISION DEFAULT 0;
ALTER TABLE IF EXISTS ai_config ADD COLUMN IF NOT EXISTS top_k INT DEFAULT 0;
ALTER TABLE IF EXISTS ai_config ADD COLUMN IF NOT EXISTS max_tokens INT DEFAULT 0;
ALTER TABLE IF EXISTS ai_jobs ADD COLUMN IF NOT EXISTS model VARCHAR(100);

DO $$
BEGIN
    IF to_regclass('public.community_posts') IS NOT NULL THEN
        UPDATE community_posts
        SET status = 'pending_review', updated_at = NOW()
        WHERE status = 'final_rejected';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS ai_jobs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    kind VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL,
    stage VARCHAR(50),
    progress INT DEFAULT 0,
    error_message TEXT,
    provider VARCHAR(50),
    model VARCHAR(100),
    validated BOOLEAN DEFAULT FALSE,
    fallback_used BOOLEAN DEFAULT FALSE,
    result_question_id TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS assets (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    kind VARCHAR(30) NOT NULL,
    filename TEXT,
    mime_type TEXT,
    size BIGINT DEFAULT 0,
    storage_key TEXT,
    url TEXT,
    checksum TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS prompt_templates (
    name TEXT PRIMARY KEY,
    task TEXT,
    default_content TEXT,
    content TEXT NOT NULL,
    render_engine TEXT DEFAULT 'go_template',
    updated_by TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    validator TEXT
);

CREATE TABLE IF NOT EXISTS ai_config (
    id TEXT PRIMARY KEY DEFAULT 'default',
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    base_url TEXT,
    temperature DOUBLE PRECISION DEFAULT 0.2,
    top_p DOUBLE PRECISION DEFAULT 0,
    top_k INT DEFAULT 0,
    max_tokens INT DEFAULT 0,
    stream_enabled BOOLEAN DEFAULT TRUE,
    fallback_model TEXT,
    updated_by TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    ip_address TEXT,
    user_agent TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
`
