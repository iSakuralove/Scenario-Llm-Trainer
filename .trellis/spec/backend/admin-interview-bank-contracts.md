# Admin Interview Bank Contracts

## Scenario: Interview Knowledge Atom Online Edit

### 1. Scope / Trigger

- Trigger: 修改管理员面试题库单题详情、版本历史、在线编辑、归档/恢复接口，或前端管理页与这些接口的请求/响应合同。
- Applies to: `backend/internal/httpapi/handlers_interview_bank.go`, `backend/internal/httpapi/server.go`, `frontend/src/api/client.ts`, `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`。

### 2. Signatures

- API: `GET /api/v1/admin/interview-bank/atoms/{id}`
- API: `GET /api/v1/admin/interview-bank/atoms/{id}/versions`
- API: `PATCH /api/v1/admin/interview-bank/atoms/{id}`
- Auth: 管理员访问，走现有 admin 鉴权；非管理员不能读取或编辑。
- Store: `GetInterviewKnowledgeAtom`, `ListInterviewKnowledgeAtomVersions`, `SaveInterviewKnowledgeAtomVersioned`。
- CORS: 新增或使用非 `GET/POST/PUT/DELETE` 方法时，必须同步 `Access-Control-Allow-Methods` 并补安全测试断言。

### 3. Contracts

- Detail response: 返回当前 `InterviewKnowledgeAtom`，包括稳定 `id`、`current_version`、`vector_status`、`updated_at` 和结构化题目字段。
- Versions response: 返回 `InterviewKnowledgeAtomVersion[]`，按 `created_at DESC` 排序。
- Update request must include:
  - `base_version: number`
  - `change_note: string`
  - editable fields: `title`, `subject`, `domain`, `difficulty`, `category`, `question_role`, `sourceRef`, `tags`, `principles`, `pitfalls`, `followUpPaths`
- Update response: 返回保存后的当前 atom；保存成功后 `vector_status=pending`。
- Update must write a version with type `manual_edit`, operator admin id, and `change_note`。
- If payload content is unchanged, still create a `manual_edit` version and set `no_content_change=true` in version metadata.

### 4. Validation & Error Matrix

- Missing/invalid auth -> existing admin unauthorized/forbidden response.
- `base_version` missing -> reject.
- `base_version != current_version` -> reject with `版本已更新，请刷新后重试`。
- `change_note` empty or whitespace -> reject.
- Attempt to change stable `id` or normal edit `status` -> reject or ignore according to handler contract; never persist those fields from edit payload.
- Invalid enum, missing required core content, invalid array counts, or empty `sourceRef` -> reject using the same hard validation as import/publish flow.
- Successful edit -> increment `current_version`, append version, mark vector status pending.

### 5. Good/Base/Bad Cases

- Good: Admin opens detail, edits allowed fields with matching `base_version`, saves, sees `v2`, version history shows `manual_edit` note, and vector status is pending.
- Base: Admin saves without content changes but with a valid note; version is still appended with `no_content_change=true` for audit.
- Bad: Frontend sends a stale `base_version`; backend overwrites the newer content.
- Bad: Backend accepts `PATCH` but CORS preflight does not include `PATCH`, causing real browser saves to fail.

### 6. Tests Required

- Backend: detail and version history require admin auth and return expected data.
- Backend: successful edit increments version, appends `manual_edit`, stores operator/note, and sets vector pending.
- Backend: stale/missing `base_version`, empty `change_note`, invalid enum/content, and empty `sourceRef` are rejected.
- Backend: no-content edit still creates a version with `no_content_change=true`。
- Backend security: CORS allowed methods include `PATCH` when admin APIs use PATCH.
- Frontend: `npm --prefix frontend run lint` and `npm --prefix frontend run build` must pass after type/client/page changes.
- Browser: real Chrome flow should open detail, save edit after confirmation, and show the new version history row.

### 7. Wrong vs Correct

#### Wrong

```typescript
await fetch(`/api/v1/admin/interview-bank/atoms/${id}`, {
  method: "PATCH",
  body: JSON.stringify({ title, change_note }),
})
```

This omits `base_version`, so concurrent edits can overwrite newer published content.

#### Correct

```typescript
await api.updateInterviewKnowledgeAtom(token, id, {
  base_version: atom.current_version,
  change_note,
  title,
})
```

The client sends the version it edited from, and the backend rejects stale writes before saving a new audit version.

## Scenario: Interview Knowledge Atom Archive / Restore

### 1. Scope / Trigger

- Trigger: 修改管理员题库归档、恢复归档、状态流转、版本类型、向量清理或前端详情页治理动作。
- Applies to: `backend/internal/domain/interview_bank.go`, `backend/internal/httpapi/handlers_interview_bank.go`, `backend/internal/store/schema.go`, `backend/migrations/001_schema.sql`, `frontend/src/api/client.ts`, `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`。

### 2. Signatures

- API: `POST /api/v1/admin/interview-bank/atoms/{id}/archive`
- API: `POST /api/v1/admin/interview-bank/atoms/{id}/restore`
- Archive request: `{ reason: string }`
- Restore request: empty JSON body is accepted.
- Response: `{ atom: InterviewKnowledgeAtom, version: InterviewKnowledgeAtomVersion }`
- Version types: `archive`, `restore_archived`
- DB CHECK: `interview_knowledge_atom_versions.version_type` must allow both version types in runtime schema, legacy compatibility SQL, and Docker init migration.

### 3. Contracts

- Archive requires admin auth and non-empty `reason`.
- Archive changes `status` to `archived`, writes an `archive` version with `change_note=reason`, marks `vector_status=pending`, and deletes topic-bank vector documents for that atom when a vector store is available.
- Restore requires admin auth and only accepts atoms whose current `status=archived`.
- Restore changes `status` to `published`, validates the atom with the same hard validation as import/edit, writes a `restore_archived` version, and marks `vector_status=pending`.
- Runtime retrieval must not use archived atoms; either the vector documents are deleted or the retrieval path filters the current atom by `status=published` and `vector_status=indexed`. This project does both.
- Frontend detail panel shows archive action for non-archived atoms and restore action for archived atoms; successful actions refresh detail, versions, list, and summary.

### 4. Validation & Error Matrix

- Missing/invalid auth -> existing admin unauthorized/forbidden response.
- Archive missing atom -> `404`.
- Archive empty `reason` -> `400`, `reason is required`.
- Archive already archived atom -> `400`.
- Restore missing atom -> `404`.
- Restore non-archived atom -> `400`.
- Restore hard validation failure -> `400` with validation messages.
- Adding `archive` in Go constants but not DB CHECK -> persistent mode writes fail; schema tests must catch this.

### 5. Good/Base/Bad Cases

- Good: Admin archives a published indexed atom; current atom becomes `archived/pending`, version history gets `archive`, and old vector docs are gone.
- Good: Admin restores an archived atom; current atom becomes `published/pending`, version history gets `restore_archived`, and index rebuild remains manual.
- Base: Archived atom is requested in index rebuild; rebuild skips it and deletes old vector docs without embedding.
- Bad: UI hides archived status but backend still leaves vector docs searchable and retrieval does not filter current status.
- Bad: Store accepts `archive` in memory but Postgres CHECK rejects it because migrations were not updated.

### 6. Tests Required

- Backend API: admin-only archive/restore, empty reason rejection, duplicate archive rejection, non-archived restore rejection, hard validation failure on restore.
- Backend API: archive writes `archive`, restore writes `restore_archived`, versions are newest-first.
- Vector behavior: archive deletes topic-bank vector docs or retrieval filters them out; rebuild skips archived atoms.
- Store/schema: `archive` is a valid version type; runtime schema, legacy SQL, and Docker init schema include `archive`.
- Frontend: `npm --prefix frontend run lint` and `npm --prefix frontend run build`.
- Browser: real page can open detail, archive with a reason, restore, and observe final `published/pending` atom with expected version order.

### 7. Wrong vs Correct

#### Wrong

```go
const InterviewKnowledgeVersionArchive = "archive"
```

Only adding the Go constant works in memory mode but fails in Postgres when the existing CHECK constraint rejects `archive`.

#### Correct

```go
const InterviewKnowledgeVersionArchive = "archive"
// Also update SchemaSQL, LegacyCompatibilitySQL, migrations/001_schema.sql, and schema tests.
```

The domain constant, Store validation, runtime schema, migration schema, and legacy constraint upgrade stay aligned.
