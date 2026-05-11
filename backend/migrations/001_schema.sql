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

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS scenario_vector_documents (
    id TEXT PRIMARY KEY,
    question_id TEXT NOT NULL,
    source_version INT NOT NULL,
    doc_type TEXT NOT NULL,
    doc_key TEXT NOT NULL,
    doc_text TEXT NOT NULL,
    text_hash TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    embedding_model TEXT,
    embedding_dim INT,
    embedding vector,
    status TEXT DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(question_id, source_version, doc_type, doc_key)
);

CREATE INDEX IF NOT EXISTS scenario_vector_documents_question_idx
    ON scenario_vector_documents(question_id, doc_type, status);

CREATE INDEX IF NOT EXISTS scenario_vector_documents_embedding_hnsw
    ON scenario_vector_documents USING hnsw (embedding vector_cosine_ops);

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

CREATE TABLE IF NOT EXISTS ai_jobs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    kind VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL,
    stage VARCHAR(50),
    progress INT DEFAULT 0,
    error_message TEXT,
    provider VARCHAR(50),
    validated BOOLEAN DEFAULT FALSE,
    fallback_used BOOLEAN DEFAULT FALSE,
    result_question_id TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
