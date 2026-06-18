# Store And Schema Contracts

## Scenario: Interview Knowledge Bank Version Storage

### 1. Scope / Trigger

- Trigger: 新增或修改面试题库知识原子、版本历史、批次、检索日志或面试会话快照的数据层。
- Applies to: `backend/internal/domain`, `backend/internal/store`, `backend/internal/store/schema.go`, `backend/migrations/001_schema.sql`。

### 2. Signatures

- Store write: `SaveInterviewKnowledgeAtomVersioned(atom domain.InterviewKnowledgeAtom, versionType, adminID, changeNote string) (domain.InterviewKnowledgeAtom, domain.InterviewKnowledgeAtomVersion, error)`。
- Store read current: `GetInterviewKnowledgeAtom(id string) (*domain.InterviewKnowledgeAtom, bool)`。
- Store read versions: `ListInterviewKnowledgeAtomVersions(atomID string) []domain.InterviewKnowledgeAtomVersion`。
- DB current table: `interview_knowledge_atoms(id TEXT PRIMARY KEY, current_version INT DEFAULT 1, vector_status VARCHAR(20) DEFAULT 'pending', ...)`。
- DB version table: `interview_knowledge_atom_versions(atom_id TEXT, version INT, version_type VARCHAR(32), snapshot JSONB, diff_summary JSONB, no_content_change BOOLEAN, UNIQUE(atom_id, version))`。

### 3. Contracts

- `current_version` starts at `1` for a new atom and increments by one for every version event.
- Supported `version_type` values are `content_update`, `duplicate_import`, `manual_edit`, `restore_archived`.
- `duplicate_import` must still create a version row and advance `current_version`, even when content is unchanged.
- `snapshot` must contain only stable content fields: `id`, `title`, `subject`, `domain`, `difficulty`, `category`, `question_role`, `sourceRef`, `tags`, `principles`, `pitfalls`, `followUpPaths`, `status`.
- `snapshot` must not contain runtime index fields such as `vector_status` or `last_indexed_at`.
- `diff_summary` is display-only summary data and must not be required for restore.
- `interview_sessions.question_snapshot` stores the opening-question snapshot.
- `interview_sessions.selected_atom_snapshots` stores lightweight follow-up atom metadata only.

### 4. Validation & Error Matrix

- Empty atom ID -> return error before writing.
- Unknown `version_type` -> return error before writing.
- Missing existing atom -> create atom with version `1`.
- Existing atom -> lock/read current row in Postgres and write atom update plus version insert in one transaction.
- Existing atom with empty incoming `vector_status` -> preserve existing runtime index status.

### 5. Good/Base/Bad Cases

- Good: Saving the same atom twice with `duplicate_import` returns versions `1` and `2`, and second version has `no_content_change=true`.
- Base: Saving a new published atom with no `vector_status` persists `vector_status=pending`.
- Bad: Writing only `interview_knowledge_atoms` without inserting `interview_knowledge_atom_versions` creates an audit gap.
- Bad: Adding a schema column only to `SchemaSQL` but not `backend/migrations/001_schema.sql` creates Docker/init drift.

### 6. Tests Required

- Unit: MemoryStore version creation, duplicate import, snapshot field shape, and clone safety.
- Integration: PostgresStore versioned save/read/list with `POSTGRES_TEST_URL`.
- Schema text: runtime schema and Docker init schema both contain new tables, indexes, and session snapshot fields.

### 7. Wrong vs Correct

#### Wrong

```go
atom.CurrentVersion++
store.InterviewKnowledgeAtoms[atom.ID] = &atom
```

This updates the current record but loses version history, audit metadata, and content snapshot.

#### Correct

```go
saved, version, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionDuplicateImport, adminID, note)
```

The shared Store method advances `current_version`, writes the current atom, writes a version snapshot, and keeps MemoryStore/PostgresStore behavior aligned.
