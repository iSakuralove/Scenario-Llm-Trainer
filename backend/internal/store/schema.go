package store

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    token_version INT DEFAULT 0,
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
	state_revision INT NOT NULL DEFAULT 0,
	learner_state JSONB NOT NULL DEFAULT '{"collected_evidence":[],"ruled_out_hypotheses":[],"established_facts":[],"actions_taken":[],"recent_openings":[],"current_focus":"","effective_turns":0,"stalled_turns":0,"concept_mastery":{},"skill_mastery":{},"explanation_preferences":{"detail":"balanced","analogy":"medium","directness":"medium"},"hint_level":0,"last_hint":""}',
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

CREATE TABLE IF NOT EXISTS scenario_agent_turns (
	session_id TEXT NOT NULL REFERENCES scenario_sessions(id) ON DELETE CASCADE,
	request_id TEXT NOT NULL,
	request_fingerprint TEXT NOT NULL,
	expected_revision INT NOT NULL,
	committed_revision INT NOT NULL,
	result_snapshot JSONB NOT NULL,
	public_trace JSONB NOT NULL DEFAULT '[]',
	internal_verification JSONB NOT NULL,
	internal_audit JSONB NOT NULL,
	approval_audit JSONB NOT NULL DEFAULT '[]',
	created_at TIMESTAMPTZ DEFAULT NOW(),
	PRIMARY KEY (session_id, request_id)
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
	mode VARCHAR(32),
	resume_document_ids TEXT[] DEFAULT '{}',
	candidate_context TEXT,
	status VARCHAR(50) DEFAULT 'question_presented',
    current_round INT DEFAULT 1,
    max_rounds INT DEFAULT 3,
    smart_close BOOLEAN DEFAULT TRUE,
    end_reason TEXT,
    difficulty_level VARCHAR(16),
    focus_areas TEXT[] DEFAULT '{}',
    setup_notes TEXT,
    submissions JSONB DEFAULT '[]',
    evaluations JSONB DEFAULT '[]',
    follow_up_question TEXT,
    final_score INT,
    final_report TEXT,
    question_snapshot JSONB DEFAULT '{}',
    selected_atom_snapshots JSONB DEFAULT '[]',
    started_at TIMESTAMPTZ DEFAULT NOW(),
    ended_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS interview_knowledge_atoms (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    subject TEXT NOT NULL,
    domain VARCHAR(50) NOT NULL,
    difficulty VARCHAR(16),
	category VARCHAR(64),
	question_role VARCHAR(20),
	question_type VARCHAR(32),
	opening_question TEXT,
	stable_code VARCHAR(32),
	source_ref TEXT,
    tags TEXT[] DEFAULT '{}',
    principles JSONB DEFAULT '[]',
    pitfalls JSONB DEFAULT '[]',
    follow_up_paths JSONB DEFAULT '[]',
    status VARCHAR(20) DEFAULT 'draft',
    current_version INT DEFAULT 1,
    vector_status VARCHAR(20) DEFAULT 'pending',
    last_indexed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS interview_knowledge_atoms_stable_code_idx
    ON interview_knowledge_atoms(stable_code) WHERE stable_code IS NOT NULL AND stable_code <> '';

CREATE TABLE IF NOT EXISTS interview_knowledge_atom_versions (
    id TEXT PRIMARY KEY,
    atom_id TEXT NOT NULL REFERENCES interview_knowledge_atoms(id) ON DELETE CASCADE,
    version INT NOT NULL,
    version_type VARCHAR(32) NOT NULL CHECK (version_type IN ('content_update','duplicate_import','manual_edit','archive','restore_archived')),
    admin_id TEXT,
    change_note TEXT,
    snapshot JSONB NOT NULL,
    diff_summary JSONB DEFAULT '{}',
    no_content_change BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(atom_id, version)
);

CREATE INDEX IF NOT EXISTS interview_knowledge_atom_versions_atom_version_idx
    ON interview_knowledge_atom_versions(atom_id, version DESC);
CREATE INDEX IF NOT EXISTS interview_knowledge_atom_versions_type_created_idx
    ON interview_knowledge_atom_versions(version_type, created_at DESC);
CREATE INDEX IF NOT EXISTS interview_knowledge_atom_versions_admin_created_idx
    ON interview_knowledge_atom_versions(admin_id, created_at DESC);

CREATE TABLE IF NOT EXISTS interview_knowledge_batches (
    id TEXT PRIMARY KEY,
    source_ref TEXT,
    status VARCHAR(30) DEFAULT 'draft',
    mode VARCHAR(30) DEFAULT 'draft',
    atom_count INT DEFAULT 0,
    validation_report JSONB DEFAULT '{}',
    publish_note TEXT,
    admin_id TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS interview_retrieval_logs (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES interview_sessions(id) ON DELETE CASCADE,
    round INT NOT NULL,
    query_text TEXT,
    matched_atoms JSONB DEFAULT '[]',
    fallback_used BOOLEAN DEFAULT FALSE,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS interview_retrieval_logs_created_idx
    ON interview_retrieval_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS interview_retrieval_logs_fallback_created_idx
    ON interview_retrieval_logs(fallback_used, created_at DESC);
CREATE INDEX IF NOT EXISTS interview_retrieval_logs_session_round_idx
    ON interview_retrieval_logs(session_id, round);

CREATE TABLE IF NOT EXISTS interview_bank_ops_actions (
    id TEXT PRIMARY KEY,
    action_type VARCHAR(32) NOT NULL CHECK (action_type IN ('fill_gap','fix_atom','rebuild_index','review_archive','observe')),
    status VARCHAR(32) NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','watching','resolved','dismissed','reopened')),
    priority VARCHAR(2) NOT NULL CHECK (priority IN ('P0','P1','P2','P3')),
    source VARCHAR(32) NOT NULL CHECK (source IN ('retrieval_analytics','retrieval_log','health_diagnostic','index_status','manual')),
    dedupe_key TEXT NOT NULL,
    title TEXT NOT NULL,
    reason TEXT NOT NULL,
    domain VARCHAR(50),
    category VARCHAR(64),
    difficulty VARCHAR(16),
    atom_id TEXT,
    evidence JSONB DEFAULT '{}',
    created_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_status_updated_idx
    ON interview_bank_ops_actions(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_type_priority_idx
    ON interview_bank_ops_actions(action_type, priority);
CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_combo_idx
    ON interview_bank_ops_actions(domain, category, difficulty);
CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_atom_idx
    ON interview_bank_ops_actions(atom_id);

CREATE TABLE IF NOT EXISTS interview_bank_ops_action_history (
    id TEXT PRIMARY KEY,
    action_id TEXT NOT NULL REFERENCES interview_bank_ops_actions(id) ON DELETE CASCADE,
    entry_index INT NOT NULL,
    from_status VARCHAR(32) NOT NULL CHECK (from_status IN ('open','in_progress','watching','resolved','dismissed','reopened')),
    to_status VARCHAR(32) NOT NULL CHECK (to_status IN ('open','in_progress','watching','resolved','dismissed','reopened')),
    note TEXT,
    created_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS interview_bank_ops_action_history_action_created_idx
    ON interview_bank_ops_action_history(action_id, created_at DESC);
CREATE INDEX IF NOT EXISTS interview_bank_ops_action_history_to_status_created_idx
    ON interview_bank_ops_action_history(to_status, created_at DESC);

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
    validation_errors JSONB DEFAULT '[]',
    fallback_events JSONB DEFAULT '[]',
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
ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS token_version INT DEFAULT 0;
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
ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS state_revision INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS learner_state JSONB NOT NULL DEFAULT '{"collected_evidence":[],"ruled_out_hypotheses":[],"established_facts":[],"actions_taken":[],"recent_openings":[],"current_focus":"","effective_turns":0,"stalled_turns":0,"concept_mastery":{},"skill_mastery":{},"explanation_preferences":{"detail":"balanced","analogy":"medium","directness":"medium"},"hint_level":0,"last_hint":""}';
ALTER TABLE IF EXISTS scenario_sessions ALTER COLUMN learner_state SET DEFAULT '{"collected_evidence":[],"ruled_out_hypotheses":[],"established_facts":[],"actions_taken":[],"recent_openings":[],"current_focus":"","effective_turns":0,"stalled_turns":0,"concept_mastery":{},"skill_mastery":{},"explanation_preferences":{"detail":"balanced","analogy":"medium","directness":"medium"},"hint_level":0,"last_hint":""}';
ALTER TABLE IF EXISTS scenario_sessions ADD COLUMN IF NOT EXISTS conversation_summary TEXT DEFAULT '';

CREATE TABLE IF NOT EXISTS scenario_agent_turns (
	session_id TEXT NOT NULL REFERENCES scenario_sessions(id) ON DELETE CASCADE,
	request_id TEXT NOT NULL,
	request_fingerprint TEXT NOT NULL,
	expected_revision INT NOT NULL,
	committed_revision INT NOT NULL,
	result_snapshot JSONB NOT NULL,
	public_trace JSONB NOT NULL DEFAULT '[]',
	internal_verification JSONB NOT NULL,
	internal_audit JSONB NOT NULL,
	approval_audit JSONB NOT NULL DEFAULT '[]',
	created_at TIMESTAMPTZ DEFAULT NOW(),
	PRIMARY KEY (session_id, request_id)
);
ALTER TABLE IF EXISTS interview_questions ADD COLUMN IF NOT EXISTS reference_keywords TEXT[];
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS follow_up_question TEXT;
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS difficulty_level VARCHAR(16);
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS focus_areas TEXT[] DEFAULT '{}';
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS setup_notes TEXT;
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS question_snapshot JSONB DEFAULT '{}';
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS selected_atom_snapshots JSONB DEFAULT '[]';
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS smart_close BOOLEAN DEFAULT TRUE;
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS end_reason TEXT;
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS mode VARCHAR(32);
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS resume_document_ids TEXT[] DEFAULT '{}';
ALTER TABLE IF EXISTS interview_sessions ADD COLUMN IF NOT EXISTS candidate_context TEXT;
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
ALTER TABLE IF EXISTS ai_jobs ADD COLUMN IF NOT EXISTS validation_errors JSONB DEFAULT '[]';
-- provider 回退轨迹（如 "deepseek:auth → glm"），供前端展示切换过程。
ALTER TABLE IF EXISTS ai_jobs ADD COLUMN IF NOT EXISTS fallback_events JSONB DEFAULT '[]';

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
    validation_errors JSONB DEFAULT '[]',
    fallback_events JSONB DEFAULT '[]',
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

CREATE TABLE IF NOT EXISTS interview_knowledge_atoms (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    subject TEXT NOT NULL,
    domain VARCHAR(50) NOT NULL,
    difficulty VARCHAR(16),
	category VARCHAR(64),
	question_role VARCHAR(20),
	question_type VARCHAR(32),
	opening_question TEXT,
	stable_code VARCHAR(32),
	source_ref TEXT,
    tags TEXT[] DEFAULT '{}',
    principles JSONB DEFAULT '[]',
    pitfalls JSONB DEFAULT '[]',
    follow_up_paths JSONB DEFAULT '[]',
    status VARCHAR(20) DEFAULT 'draft',
    current_version INT DEFAULT 1,
    vector_status VARCHAR(20) DEFAULT 'pending',
    last_indexed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS interview_knowledge_atom_versions (
    id TEXT PRIMARY KEY,
    atom_id TEXT NOT NULL REFERENCES interview_knowledge_atoms(id) ON DELETE CASCADE,
    version INT NOT NULL,
    version_type VARCHAR(32) NOT NULL CHECK (version_type IN ('content_update','duplicate_import','manual_edit','archive','restore_archived')),
    admin_id TEXT,
    change_note TEXT,
    snapshot JSONB NOT NULL,
    diff_summary JSONB DEFAULT '{}',
    no_content_change BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(atom_id, version)
);

ALTER TABLE IF EXISTS interview_knowledge_atoms ADD COLUMN IF NOT EXISTS question_type VARCHAR(32);
ALTER TABLE IF EXISTS interview_knowledge_atoms ADD COLUMN IF NOT EXISTS opening_question TEXT;
ALTER TABLE IF EXISTS interview_knowledge_atoms ADD COLUMN IF NOT EXISTS stable_code VARCHAR(32);
CREATE UNIQUE INDEX IF NOT EXISTS interview_knowledge_atoms_stable_code_idx
	ON interview_knowledge_atoms(stable_code) WHERE stable_code IS NOT NULL AND stable_code <> '';

CREATE INDEX IF NOT EXISTS interview_knowledge_atom_versions_atom_version_idx
    ON interview_knowledge_atom_versions(atom_id, version DESC);
CREATE INDEX IF NOT EXISTS interview_knowledge_atom_versions_type_created_idx
    ON interview_knowledge_atom_versions(version_type, created_at DESC);
CREATE INDEX IF NOT EXISTS interview_knowledge_atom_versions_admin_created_idx
    ON interview_knowledge_atom_versions(admin_id, created_at DESC);

ALTER TABLE IF EXISTS interview_knowledge_atom_versions
    DROP CONSTRAINT IF EXISTS interview_knowledge_atom_versions_version_type_check;
ALTER TABLE IF EXISTS interview_knowledge_atom_versions
    ADD CONSTRAINT interview_knowledge_atom_versions_version_type_check
    CHECK (version_type IN ('content_update','duplicate_import','manual_edit','archive','restore_archived'));

CREATE TABLE IF NOT EXISTS interview_knowledge_batches (
    id TEXT PRIMARY KEY,
    source_ref TEXT,
    status VARCHAR(30) DEFAULT 'draft',
    mode VARCHAR(30) DEFAULT 'draft',
    atom_count INT DEFAULT 0,
    validation_report JSONB DEFAULT '{}',
    publish_note TEXT,
    admin_id TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS interview_retrieval_logs (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES interview_sessions(id) ON DELETE CASCADE,
    round INT NOT NULL,
    query_text TEXT,
    matched_atoms JSONB DEFAULT '[]',
    fallback_used BOOLEAN DEFAULT FALSE,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS interview_retrieval_logs_created_idx
    ON interview_retrieval_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS interview_retrieval_logs_fallback_created_idx
    ON interview_retrieval_logs(fallback_used, created_at DESC);
CREATE INDEX IF NOT EXISTS interview_retrieval_logs_session_round_idx
    ON interview_retrieval_logs(session_id, round);

CREATE TABLE IF NOT EXISTS interview_bank_ops_actions (
    id TEXT PRIMARY KEY,
    action_type VARCHAR(32) NOT NULL CHECK (action_type IN ('fill_gap','fix_atom','rebuild_index','review_archive','observe')),
    status VARCHAR(32) NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','watching','resolved','dismissed','reopened')),
    priority VARCHAR(2) NOT NULL CHECK (priority IN ('P0','P1','P2','P3')),
    source VARCHAR(32) NOT NULL CHECK (source IN ('retrieval_analytics','retrieval_log','health_diagnostic','index_status','manual')),
    dedupe_key TEXT NOT NULL,
    title TEXT NOT NULL,
    reason TEXT NOT NULL,
    domain VARCHAR(50),
    category VARCHAR(64),
    difficulty VARCHAR(16),
    atom_id TEXT,
    evidence JSONB DEFAULT '{}',
    created_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_status_updated_idx
    ON interview_bank_ops_actions(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_type_priority_idx
    ON interview_bank_ops_actions(action_type, priority);
CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_combo_idx
    ON interview_bank_ops_actions(domain, category, difficulty);
CREATE INDEX IF NOT EXISTS interview_bank_ops_actions_atom_idx
    ON interview_bank_ops_actions(atom_id);

CREATE TABLE IF NOT EXISTS interview_bank_ops_action_history (
    id TEXT PRIMARY KEY,
    action_id TEXT NOT NULL REFERENCES interview_bank_ops_actions(id) ON DELETE CASCADE,
    entry_index INT NOT NULL,
    from_status VARCHAR(32) NOT NULL CHECK (from_status IN ('open','in_progress','watching','resolved','dismissed','reopened')),
    to_status VARCHAR(32) NOT NULL CHECK (to_status IN ('open','in_progress','watching','resolved','dismissed','reopened')),
    note TEXT,
    created_by TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS interview_bank_ops_action_history_action_created_idx
    ON interview_bank_ops_action_history(action_id, created_at DESC);
CREATE INDEX IF NOT EXISTS interview_bank_ops_action_history_to_status_created_idx
    ON interview_bank_ops_action_history(to_status, created_at DESC);

`
