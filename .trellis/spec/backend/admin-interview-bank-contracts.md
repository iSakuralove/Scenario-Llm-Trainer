# Admin Interview Bank Contracts

## Scenario: Interview Knowledge Atom Online Edit

### 1. Scope / Trigger

- Trigger: 修改管理员面试题库单题详情、版本历史、在线编辑接口，或前端管理页与这些接口的请求/响应合同。
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
