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

## Scenario: Interview Retrieval Log Operations

### 1. Scope / Trigger

- Trigger: 新增或修改真实面试追问检索日志、运营聚合、`interview_retrieval_logs` schema 或管理端日志查询。
- Applies to: `backend/internal/domain/interview_bank.go`, `backend/internal/httpapi/interview_runtime.go`, `backend/internal/httpapi/handlers_interview_bank.go`, `backend/internal/store`, `backend/internal/store/schema.go`, `backend/migrations/001_schema.sql`, `frontend/src/features/interviewBank`。

### 2. Signatures

- Store write: `SaveInterviewRetrievalLog(log domain.InterviewRetrievalLog) domain.InterviewRetrievalLog`。
- Store list: `ListInterviewRetrievalLogs(filter domain.InterviewRetrievalLogFilter) []domain.InterviewRetrievalLog`。
- Store analytics: `InterviewRetrievalAnalytics(filter domain.InterviewRetrievalLogFilter) domain.InterviewRetrievalAnalytics`。
- API: `GET /api/v1/admin/interview-bank/retrieval-logs`。
- API: `GET /api/v1/admin/interview-bank/retrieval-analytics`。
- DB table: `interview_retrieval_logs(id TEXT PRIMARY KEY, session_id TEXT, round INT, query_text TEXT, matched_atoms JSONB, fallback_used BOOLEAN, error_message TEXT, created_at TIMESTAMPTZ)`。

### 3. Contracts

- Runtime only writes logs for real interview follow-up retrieval; admin retrieval preview must not create logs.
- Log writes are best-effort. Store or DB failure must not fail the user interview submission.
- `query_text` must be sanitized and capped at 500 runes before persistence; Store layer should keep the same cap as a defensive boundary.
- `error_message` is display-only and should be capped before persistence.
- Logs must not contain user id, complete answer text, full resume, full project background, or atom body content.
- `matched_atoms` contains only lightweight atom metadata: id/version/title/subject/domain/category.
- MemoryStore and PostgresStore must implement save, list, analytics and clone behavior with the same filtering semantics.
- List endpoints default to a small limit and cap the maximum limit; analytics must aggregate from a bounded window.
- Schema changes must stay aligned across `SchemaSQL` and `backend/migrations/001_schema.sql`; indexes for recent logs, fallback logs, and session/round lookup belong in both places.

### 4. Validation & Error Matrix

- Missing/invalid auth for admin APIs -> existing admin unauthorized/forbidden response.
- Invalid `limit` -> clamp to the documented default/max instead of scanning unbounded history.
- Invalid `fallback_used` query value -> ignore as unset or reject consistently with the handler contract; do not silently invert the filter.
- Empty `session_id` on runtime write -> still keep retrieval telemetry only if the caller has a real session context; do not invent user identity.
- Empty matched atoms with no fallback -> counts as a non-hit log unless the retrieval result explicitly contains a matched atom.

### 5. Good/Base/Bad Cases

- Good: Runtime vector retrieval hits two atoms, writes one sanitized log with two lightweight snapshots, and analytics counts one hit.
- Good: Runtime retrieval falls back because embedding is unavailable, writes `fallback_used=true`, and analytics counts one fallback plus the inferred combination.
- Base: Admin preview calls retrieval-preview repeatedly and analytics does not change.
- Bad: Storing the full candidate answer or resume text in `query_text` creates a privacy leak.
- Bad: Postgres analytics scans the entire table without a limit, causing admin page refreshes to become a DB hot path.

### 6. Tests Required

- Runtime: hit and fallback retrieval write logs without changing the main submission response.
- Store unit: MemoryStore and PostgresStore save/list/filter/analytics behavior, clone safety, query truncation, and aggregation counts.
- HTTP API: admin-only logs and analytics, filter parsing, limit cap, and response shape.
- Schema text: runtime schema and Docker init migration both contain the retrieval log indexes.
- Frontend: `npm --prefix frontend run lint` and `npm --prefix frontend run build`.

### 7. Wrong vs Correct

#### Wrong

```go
log.QueryText = candidateAnswer + "\n" + resumeText
store.SaveInterviewRetrievalLog(log)
```

This persists sensitive user content and turns an operations log into a privacy risk.

#### Correct

```go
log.QueryText = truncateStringRunes(ai.Sanitize(query), 500)
_ = store.SaveInterviewRetrievalLog(log)
```

The runtime stores only a bounded, sanitized retrieval query and treats logging as non-blocking telemetry.

## Scenario: Interview Bank Ops Action Queue

### 1. Scope / Trigger

- Trigger: 新增或修改题库运营动作、动作列表、动作候选、动作状态流转、`interview_bank_ops_actions` schema 或管理端运营动作面板。
- Applies to: `backend/internal/domain/interview_bank.go`, `backend/internal/httpapi/handlers_interview_bank.go`, `backend/internal/store`, `backend/internal/store/schema.go`, `backend/migrations/001_schema.sql`, `frontend/src/api/client.ts`, `frontend/src/types/index.ts`, `frontend/src/features/interviewBank`。

### 2. Signatures

- Store create: `CreateInterviewBankOpsAction(action domain.InterviewBankOpsAction) (domain.InterviewBankOpsAction, error)`。
- Store list: `ListInterviewBankOpsActions(filter domain.InterviewBankOpsActionFilter) []domain.InterviewBankOpsAction`。
- API list: `GET /api/v1/admin/interview-bank/ops-actions`。
- API candidates: `POST /api/v1/admin/interview-bank/ops-actions/candidates`。
- API candidate save: `POST /api/v1/admin/interview-bank/ops-actions/candidates/save`。
- API manual create: `POST /api/v1/admin/interview-bank/ops-actions`。
- Candidate request: `{ sources?: ["health_diagnostic"|"index_status"|"retrieval_analytics"], domain?: string, category?: string, difficulty?: "L1"|"L2"|"L3"|"L4"|"L5", limit?: number }`。
- Candidate response: `{ list: InterviewBankOpsActionCandidate[], total: number, skipped_existing: number, policy: { sources: string[], limit: number } }`。
- Candidate save request: `{ candidates: InterviewBankOpsActionCandidate[] }`。
- Candidate save response: `{ list: InterviewBankOpsAction[], saved: number, total: number, skipped_existing: number }`。
- DB table: `interview_bank_ops_actions(id TEXT PRIMARY KEY, action_type TEXT, status TEXT, priority TEXT, source TEXT, dedupe_key TEXT, title TEXT, reason TEXT, domain TEXT, category TEXT, difficulty TEXT, atom_id TEXT, evidence JSONB, created_by TEXT, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)`。

### 3. Contracts

- All operations action APIs are admin-only; non-admin users receive the existing forbidden response.
- Manual create must normalize `source=manual`, default `status=open`, and record `created_by` from the authenticated admin.
- `action_type` is limited to `fill_gap`, `fix_atom`, `rebuild_index`, `review_archive`, `observe`。
- `status` is limited to `open`, `in_progress`, `watching`, `resolved`, `dismissed`, `reopened`。
- `priority` is limited to `P0`, `P1`, `P2`, `P3` and should be stored uppercase.
- `source` is limited to `retrieval_analytics`, `retrieval_log`, `health_diagnostic`, `index_status`, `manual`。
- `dedupe_key` must never be empty. Manual actions may derive it from action type plus target scope; if target scope is insufficient, use an ID-scoped manual key.
- `evidence` must be compact JSON metadata. It must not contain full user answers, full resume text, project background, full atom body content, secrets, tokens, or raw provider payloads.
- Creating or listing an action must not edit atoms, change atom status, rebuild vectors, write retrieval logs, or create LLM/embedding calls.
- Candidate generation is read-only. It must not call `CreateInterviewBankOpsAction`, edit atoms, change atom status, rebuild vectors, write retrieval logs, or create LLM/embedding calls.
- Candidate save is an explicit admin write path. It may call `CreateInterviewBankOpsAction` for selected generated candidates, but must not edit atoms, change atom status, rebuild vectors, write retrieval logs, or create LLM/embedding calls.
- Candidate generation accepts `health_diagnostic`, `index_status`, and `retrieval_analytics` sources; omitting `sources` includes all three.
- Health diagnostic candidate rules:
  - `blocked` combination -> `fill_gap/P0`, combo dedupe key.
  - `warning` combination -> `rebuild_index/P1` when failed resources exist, otherwise `rebuild_index/P2`, combo dedupe key.
- Index status candidate rules:
  - `status=published + vector_status=failed` atom -> `rebuild_index/P1`, atom dedupe key.
  - `status=published + vector_status=pending` atom -> `rebuild_index/P2`, atom dedupe key.
  - `draft` and `archived` atoms do not create normal index-status candidates.
- Retrieval analytics candidate rules:
  - fallback combination from real retrieval analytics -> `fill_gap/P0` when count >= 3, otherwise `fill_gap/P1`, combo dedupe key.
  - low-hit atom from real retrieval analytics with `hit_count=0` -> `observe/P3`, atom-only dedupe key.
  - low-hit atom with `hit_count>0` must not create `observe` or `review_archive` candidates in this slice.
- Candidate generation must skip candidates whose `dedupe_key` already belongs to an active action (`open`, `in_progress`, `watching`, `reopened`). `resolved` and `dismissed` do not block future candidates.
- Candidate save accepts only generated sources (`health_diagnostic`, `index_status`, `retrieval_analytics`), preserves the candidate `dedupe_key`, forces `status=open`, records `created_by`, and skips active `dedupe_key` matches instead of creating duplicate open work.
- Candidate evidence must remain compact metadata: status/counts/reasons/actions for health candidates; atom id/title/subject/domain/category/difficulty/question_role/vector_status/current_version for index candidates; fallback count/reason/rate/window metadata and light atom hit metadata for retrieval candidates. It must not include `query_text`, `principles`, `pitfalls`, `follow_up_paths`, full answers, resume text, project background, or provider payloads.
- MemoryStore and PostgresStore must keep the same filtering, defaulting, sorting, limit cap, and clone-safety semantics.

### 4. Validation & Error Matrix

- Missing title or reason -> HTTP `400`, no Store write.
- Unknown `action_type`, `priority`, `source`, or `status` -> HTTP `400`, no Store write.
- Missing auth -> existing unauthorized response.
- Authenticated non-admin -> existing forbidden response.
- Invalid or missing `limit` -> clamp to the documented default/max instead of scanning unbounded history.
- Candidate source outside `health_diagnostic|index_status|retrieval_analytics` -> HTTP `400`, no Store write.
- Candidate `limit <= 0` -> default `50`; `limit > 200` -> clamp to `200`.
- Candidate request with duplicate sources -> dedupe sources in response policy.
- Candidate save with empty `candidates`, more than 50 candidates, invalid generated source, empty `dedupe_key`, or missing target scope -> HTTP `400`.
- Candidate save with active duplicate `dedupe_key` -> HTTP `200`, increments `skipped_existing`, and does not create a new action.
- Store evidence marshal error -> return create error before persisting a partial action.
- Empty list result -> return `list: []` and `total: 0`, not an error.

### 5. Good/Base/Bad Cases

- Good: Admin manually creates a `fill_gap` action for `backend/cache/L3`; listing `status=open` returns the created action with `source=manual` and `created_by` set.
- Good: Listing by `priority=P1` and `difficulty=L3` returns only matching actions ordered by `updated_at DESC` with a stable ID tie-breaker.
- Good: Admin generates candidates for a blocked `backend/database/L2` health combination and receives one `fill_gap/P0` candidate; listing actions immediately afterward is still empty.
- Good: Admin generates candidates for a published failed atom and receives one `rebuild_index/P1` atom candidate without full atom body content in evidence.
- Good: Admin generates candidates from real fallback combinations and receives `retrieval_analytics + fill_gap` candidates without full query text in evidence.
- Good: Admin generates candidates from real low-hit analytics and receives only `hit_count=0` `observe/P3` atom candidates; hit atoms are ignored.
- Good: Admin saves a selected generated candidate and then sees it in the open queue with the generated source and compact evidence preserved.
- Good: Saving a candidate whose `dedupe_key` matches an open action skips it; saving one whose previous matching action is resolved creates a new open action.
- Good: Existing open action with the same `dedupe_key` increments `skipped_existing` and suppresses the duplicate candidate; existing resolved action does not suppress it.
- Base: No saved actions returns an empty open queue and lets the existing analytics/health panels continue rendering.
- Base: Candidate generation with no matching health/index signals returns `list: []`, `total: 0`, and the normalized policy.
- Bad: Creating an action from the frontend also edits the related atom or starts index rebuild; action creation is governance bookkeeping only.
- Bad: Candidate generation persists actions or calls embedding/vector rebuild just because it found `vector_status=failed`.
- Bad: Storing full candidate answers in `evidence` leaks user content into an admin operations record.

### 6. Tests Required

- HTTP API: admin create/list success, non-admin forbidden, invalid enum and missing required fields rejected.
- HTTP API: candidate generation is admin-only, rejects invalid source, clamps limit, returns blocked health `fill_gap`, warning health `rebuild_index`, failed/pending published atom `rebuild_index`, real retrieval fallback `fill_gap/P0/P1`, zero-hit retrieval atom `observe/P3`, skips draft/archived atoms, skips active dedupe keys, and does not persist actions.
- HTTP API: candidate save is admin-only, persists selected generated candidates, preserves source/dedupe/evidence, skips active duplicates, allows resolved duplicates, rejects invalid candidate payloads, and refreshes the open action list.
- Store unit: MemoryStore create/list filters, default values, sorting, clone safety, and evidence round-trip.
- Schema text: `SchemaSQL`, `LegacyCompatibilitySQL`, and `backend/migrations/001_schema.sql` all contain the action table and indexes.
- Postgres behavior: create/list query must use the same filters and limit cap as MemoryStore.
- Frontend: `npm --prefix frontend run lint` and `npm --prefix frontend run build` after adding API/types/page contracts.
- Browser smoke: logged-in admin can create a manual action and see it in the open queue.

### 7. Wrong vs Correct

#### Wrong

```go
action.Source = r.URL.Query().Get("source")
action.Status = r.URL.Query().Get("status")
store.CreateInterviewBankOpsAction(action)
```

This lets a manual create spoof generated sources or closed statuses and weakens audit meaning.

#### Correct

```go
action.Source = domain.InterviewBankOpsActionSourceManual
action.Status = domain.InterviewBankOpsActionStatusOpen
action.CreatedBy = user.ID
created, err := store.CreateInterviewBankOpsAction(action)
```

Manual actions have a fixed source/status boundary, and future generated actions can use a separate candidate/save path with its own validation.

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
