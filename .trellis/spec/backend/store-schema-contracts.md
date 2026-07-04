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
- Supported `version_type` values are `content_update`, `duplicate_import`, `manual_edit`, `archive`, `restore_archived`.
- `duplicate_import` must still create a version row and advance `current_version`, even when content is unchanged.
- `snapshot` must contain only stable content fields: `id`, `title`, `subject`, `domain`, `difficulty`, `category`, `question_role`, `sourceRef`, `tags`, `principles`, `pitfalls`, `followUpPaths`, `status`.
- `snapshot` must not contain runtime index fields such as `vector_status` or `last_indexed_at`.
- `diff_summary` is display-only summary data and must not be required for restore.
- `interview_sessions.question_snapshot` stores the opening-question snapshot.
- `interview_sessions.selected_atom_snapshots` stores lightweight follow-up atom metadata only.

### 4. Validation & Error Matrix

- Empty atom ID -> return error before writing.
- Unknown `version_type` -> return error before writing.
- Adding a new `version_type` -> update domain constants, Store validation, `SchemaSQL`, `LegacyCompatibilitySQL`, `backend/migrations/001_schema.sql`, and schema text tests in the same change.
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

## Scenario: Interview Knowledge Bank Vector Rebuild

### 1. Scope / Trigger

- Trigger: 新增或修改面试题库向量文档、索引重建 API、`vector_status` / `last_indexed_at` 更新逻辑。
- Applies to: `backend/internal/ai/interview_knowledge_vector.go`, `backend/internal/httpapi/handlers_interview_bank.go`, `backend/internal/store`, `backend/internal/store/vector.go`, `backend/internal/store/postgres_vector.go`, `backend/migrations/001_schema.sql`, `frontend/src/features/interviewBank`。

### 2. Signatures

- API: `POST /api/v1/admin/interview-bank/index/rebuild`。
- Request: `{ atom_ids?: string[], vector_status?: "pending"|"failed"|"pending_failed", limit?: number }`。
- Store status update: `UpdateInterviewKnowledgeAtomIndexStatus(atomID, vectorStatus string, lastIndexedAt *time.Time) (domain.InterviewKnowledgeAtom, error)`。
- Vector writes: `RebuildInterviewKnowledgeIndex(context.Context, []ai.InterviewKnowledgeVectorDocument) error`。
- DB table: `interview_knowledge_vector_documents(atom_id TEXT REFERENCES interview_knowledge_atoms(id) ON DELETE CASCADE, atom_version INT, doc_type TEXT, doc_key TEXT, embedding vector(1536), UNIQUE(atom_id, atom_version, doc_type, doc_key))`。

### 3. Contracts

- Rebuild is admin-only; non-admin callers receive `403`.
- `atom_ids` 非空时按 ID 精确重建；为空时按 `vector_status` 选择候选，默认 `pending_failed`。
- Single request limit is capped at 50 atoms.
- Only `status=published` atoms may call embedding and write active vector documents.
- `draft` / `archived` atoms must not call embedding; requested rebuild deletes old vector documents and returns `skipped`.
- Successful rebuild writes `interview_knowledge_vector_documents`, sets `vector_status=indexed`, and updates `last_indexed_at`.
- Failed rebuild sets `vector_status=failed` and must preserve the previous `last_indexed_at`.
- Index status updates are runtime state changes and must not create `interview_knowledge_atom_versions` rows.
- Topic-bank vector documents must stay in `interview_knowledge_vector_documents`; do not write them into `scenario_vector_documents`.
- Schema changes must stay aligned across `VectorSchemaSQL`, `backend/migrations/001_schema.sql`, and schema tests.

### 4. Validation & Error Matrix

- Invalid `vector_status` request value -> HTTP `400`.
- Missing atom ID in an explicit request -> per-item `failed`, does not abort other items.
- Missing embedding client -> per-item `failed` and atom status becomes `failed`.
- Embedding count mismatch, empty vector, or dimension mismatch -> per-item `failed`.
- Vector store write error -> per-item `failed`.
- Status update error after vector write -> per-item `failed`; caller receives the status update error summary.

### 5. Good/Base/Bad Cases

- Good: Rebuilding a published pending atom produces overview/principle/pitfall/follow_up documents and marks the atom indexed.
- Good: Rebuilding a failed atom with a previous `last_indexed_at` fails again but preserves the previous timestamp.
- Base: Rebuilding a draft atom returns skipped and does not call embedding.
- Bad: Updating `vector_status` through `SaveInterviewKnowledgeAtomVersioned` creates misleading version history.
- Bad: Writing topic-bank vectors into `scenario_vector_documents` couples unrelated retrieval semantics.

### 6. Tests Required

- AI unit: `BuildInterviewKnowledgeVectorDocuments` doc count, doc types, metadata, and non-published skip.
- Store unit: MemoryVectorStore topic-bank rebuild/delete and clone safety.
- Store unit: `UpdateInterviewKnowledgeAtomIndexStatus` does not create versions and preserves `last_indexed_at` on failure.
- HTTP API: admin-only, successful rebuild, missing/failed embedding, draft/archived skipped, explicit `atom_ids`, and `pending_failed` status selection.
- Frontend: run `npm --prefix frontend run lint` and `npm --prefix frontend run build` after API/type/page changes.

### 7. Wrong vs Correct

#### Wrong

```go
saved, _, err := store.SaveInterviewKnowledgeAtomVersioned(atom, domain.InterviewKnowledgeVersionContentUpdate, adminID, "index failed")
```

This treats runtime indexing state as a content version and pollutes the audit history.

#### Correct

```go
saved, err := store.UpdateInterviewKnowledgeAtomIndexStatus(atom.ID, "failed", nil)
```

The runtime status changes without creating a version snapshot, and a failed rebuild keeps the previous `last_indexed_at`.
