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

## Scenario: Interview Bank Admin Import MVP

### 1. Scope / Trigger

- Trigger: 新增或修改面试题库治理管理端 API、导入校验、发布写入、批次记录或系统状态摘要。
- Applies to: `backend/internal/httpapi/handlers_interview_bank.go`, `backend/internal/httpapi/handlers_admin.go`, `backend/internal/store`, `frontend/src/api/client.ts`, `frontend/src/features/interviewBank`。

### 2. Signatures

- Summary: `GET /api/v1/admin/interview-bank/summary` -> `domain.InterviewKnowledgeSummary`。
- Atom list: `GET /api/v1/admin/interview-bank/atoms?status=&domain=&difficulty=&category=&question_role=&vector_status=` -> `{ list, total, filters }`。
- Batch list: `GET /api/v1/admin/interview-bank/batches?limit=30` -> `{ list: []InterviewKnowledgeBatch }`。
- Validate: `POST /api/v1/admin/interview-bank/import/validate` -> `InterviewKnowledgeImportReport`。
- Publish: `POST /api/v1/admin/interview-bank/import/publish` -> `{ report: InterviewKnowledgeImportReport, batch: InterviewKnowledgeBatch }`。
- Store list: `ListInterviewKnowledgeAtoms(filter domain.InterviewKnowledgeAtomFilter) []domain.InterviewKnowledgeAtom`。
- Store batch: `SaveInterviewKnowledgeBatch(batch domain.InterviewKnowledgeBatch) domain.InterviewKnowledgeBatch`。
- Store summary: `InterviewKnowledgeSummary() domain.InterviewKnowledgeSummary`。

### 3. Contracts

- All `/admin/interview-bank/*` routes require `domain.RoleAdmin`; non-admin users receive `403`.
- Import payload accepts `items` or `atoms`.
- Top-level defaults may include `batch_id`/`batchId`, `source_ref`/`sourceRef`, `domain`, `category`, `difficulty`, `question_role`/`questionRole`, `status`, `vector_status`/`vectorStatus`, and `tags`.
- Each item must map to `InterviewKnowledgeAtom` fields and accepts snake_case/camelCase aliases for `question_role`, `source_ref`, `follow_up_paths`, and `vector_status`.
- `validate` must not write atoms, versions, or batches.
- `publish` must reuse the same validation logic as `validate`.
- Successful `publish` must call `SaveInterviewKnowledgeAtomVersioned` per valid atom and then save one `InterviewKnowledgeBatch`.
- Same-content re-import must use `domain.InterviewKnowledgeVersionDuplicateImport` and still create a version row.
- `vector_status=failed` is filter/display-only in this MVP; do not trigger index rebuild, background job creation, retry, or vector store writes.

### 4. Validation & Error Matrix

- Missing `items`/`atoms` or empty list -> report error, no write.
- Missing `id`, `title`, `subject`, `domain`, `category`, `difficulty`, `question_role`, or `source_ref` -> item error.
- `difficulty` not in `L1`-`L5` -> item error.
- `category` not in `java`, `database`, `cache`, `middleware`, `system_design`, `frontend`, `ai_llm`, `hr_soft_skill` -> item error.
- `question_role` not in `opening`, `followup`, `mixed` -> item error.
- `status` not in `draft`, `published`, `archived` -> item error.
- `vector_status` not in `pending`, `indexed`, `failed` -> item error.
- Fewer than 2 `principles`, `pitfalls`, or `follow_up_paths` -> item error.
- Duplicate ID inside one import batch -> item error.
- Publish with any validation error -> HTTP `400` with the same report in `data`.

### 5. Good/Base/Bad Cases

- Good: Admin validates a well-formed payload and receives `valid_count=1`, while `GetInterviewKnowledgeAtom(id)` still returns false.
- Good: Admin publishes the same payload twice; the second publish returns `duplicate_count=1` and atom versions contain `duplicate_import`.
- Base: Listing atoms with `vector_status=failed` returns failed atoms only and does not mutate store state.
- Bad: Implementing separate validate and publish validators can let preview pass while publish fails.
- Bad: Adding a “rebuild index” action inside this MVP violates the filter-only `vector_status=failed` scope.

### 6. Tests Required

- Store unit: filter by `vector_status`, batch clone safety, and summary counts/timestamps.
- HTTP API: non-admin forbidden, validate invalid payload does not write, publish writes atom and batch, duplicate publish creates second version, failed-vector filter returns only failed atoms.
- System status: response includes `interview_bank` and `counts.interview_knowledge_atoms`.
- Frontend: `npm --prefix frontend run lint` and `npm --prefix frontend run build` after adding API/types/page contracts.

### 7. Wrong vs Correct

#### Wrong

```go
if r.URL.Query().Get("vector_status") == "failed" {
    go rebuildInterviewKnowledgeIndex()
}
```

This couples the MVP list filter to future indexing infrastructure and creates hidden side effects from a read path.

#### Correct

```go
items := store.ListInterviewKnowledgeAtoms(domain.InterviewKnowledgeAtomFilter{
    VectorStatus: r.URL.Query().Get("vector_status"),
})
```

The admin list endpoint stays read-only; real index rebuild must be introduced later as an explicit write API with tests and observable job state.
