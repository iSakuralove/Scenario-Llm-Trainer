\set ON_ERROR_STOP on

BEGIN;

LOCK TABLE scenario_messages, scenario_sessions, scenario_vector_documents, scenario_questions
    IN ACCESS EXCLUSIVE MODE;
LOCK TABLE interview_questions, interview_sessions IN SHARE MODE;

CREATE TEMP TABLE reset_interview_counts ON COMMIT DROP AS
SELECT
    (SELECT count(*) FROM interview_questions) AS questions,
    (SELECT count(*) FROM interview_sessions) AS sessions;

\echo '=== scenario rows before reset ==='
SELECT 'scenario_messages' AS table_name, count(*) AS row_count FROM scenario_messages
UNION ALL
SELECT 'scenario_sessions', count(*) FROM scenario_sessions
UNION ALL
SELECT 'scenario_vector_documents', count(*) FROM scenario_vector_documents
UNION ALL
SELECT 'scenario_questions', count(*) FROM scenario_questions
ORDER BY table_name;

\echo '=== interview rows before reset (must remain unchanged) ==='
SELECT 'interview_questions' AS table_name, count(*) AS row_count FROM interview_questions
UNION ALL
SELECT 'interview_sessions', count(*) FROM interview_sessions
ORDER BY table_name;

-- 外键依赖顺序固定，禁止换序或使用 CASCADE。
DELETE FROM scenario_messages;
DELETE FROM scenario_sessions;
DELETE FROM scenario_vector_documents;
DELETE FROM scenario_questions;

DO $$
DECLARE
    expected_questions bigint;
    expected_sessions bigint;
    actual_questions bigint;
    actual_sessions bigint;
    remaining_scenario_rows bigint;
BEGIN
    SELECT questions, sessions
      INTO expected_questions, expected_sessions
      FROM reset_interview_counts;

    SELECT count(*) INTO actual_questions FROM interview_questions;
    SELECT count(*) INTO actual_sessions FROM interview_sessions;

    IF actual_questions <> expected_questions OR actual_sessions <> expected_sessions THEN
        RAISE EXCEPTION
            'interview tables changed during scenario reset: expected questions=%, sessions=%; actual questions=%, sessions=%',
            expected_questions, expected_sessions, actual_questions, actual_sessions;
    END IF;

    SELECT
        (SELECT count(*) FROM scenario_messages) +
        (SELECT count(*) FROM scenario_sessions) +
        (SELECT count(*) FROM scenario_vector_documents) +
        (SELECT count(*) FROM scenario_questions)
      INTO remaining_scenario_rows;

    IF remaining_scenario_rows <> 0 THEN
        RAISE EXCEPTION 'scenario reset incomplete: % rows remain', remaining_scenario_rows;
    END IF;
END
$$;

\echo '=== scenario rows after reset ==='
SELECT 'scenario_messages' AS table_name, count(*) AS row_count FROM scenario_messages
UNION ALL
SELECT 'scenario_sessions', count(*) FROM scenario_sessions
UNION ALL
SELECT 'scenario_vector_documents', count(*) FROM scenario_vector_documents
UNION ALL
SELECT 'scenario_questions', count(*) FROM scenario_questions
ORDER BY table_name;

\echo '=== interview rows after reset (asserted unchanged) ==='
SELECT 'interview_questions' AS table_name, count(*) AS row_count FROM interview_questions
UNION ALL
SELECT 'interview_sessions', count(*) FROM interview_sessions
ORDER BY table_name;

COMMIT;
