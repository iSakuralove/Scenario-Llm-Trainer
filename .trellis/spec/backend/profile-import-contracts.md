# Profile Resume Import Contracts

## Scenario: Multi-document Resume Import And Quality Gate

### 1. Scope / Trigger

- Trigger: 修改个人档案手动简历、`POST /api/v1/users/me/profile/import`、`GET /api/v1/users/me/resumes`、`PUT /api/v1/users/me/resumes/{id}`、文件资产保存或简历深挖前置校验。
- Applies to: `backend/internal/httpapi/profile_import.go`, `backend/internal/httpapi/handlers_auth.go`, `backend/internal/domain/types.go`, `backend/internal/store`, `frontend/src/features/profile`, `frontend/src/features/interviews`。

### 2. Signatures

- Import: `POST /api/v1/users/me/profile/import`, multipart field `file`, max `2 MiB`。
- List: `GET /api/v1/users/me/resumes` -> `{ list: ResumeDocument[], total: number }`。
- Edit: `PUT /api/v1/users/me/resumes/{id}` -> `ResumeDocument`。
- `ResumeDocument`: `{ id, name, source_type, format, asset_id?, content_url?, content?, extracted_text, parse_status, quality_status, quality_reason?, editable, created_at, updated_at }`。
- Manual profile save remains `PUT /api/v1/users/me/profile` with independent `resume_summary` and `project_summary` fields.

### 3. Contracts

- Supported upload formats are `TXT / MD / DOCX / PDF`; legacy `.doc` is not accepted until a deterministic conversion path exists.
- Uploaded files append a new `UserProfile.resume_documents` entry and must not overwrite existing resume documents.
- `TXT / MD` store editable text directly and do not require a duplicate binary asset.
- `DOCX / PDF` preserve the original file in the existing asset store, expose an authenticated `content_url`, and remain read-only.
- Manual `resume_summary + project_summary` are projected as one editable `source_type=manual` document while the two source fields remain independently persisted.
- Import and manual save reuse the same deterministic quality rules; model output is not the sole accept/reject authority.
- Every non-empty field/file is checked independently before combined-context validation. One valid field cannot hide another field containing obvious garbage.
- Combined usable context requires at least `60` Unicode letters/digits and at least two information classes among experience/role, skills, project/responsibility, outcomes, education.
- A non-empty field/file is rejected when symbol ratio, repeated-character ratio, or repeated-fragment ratio reaches `60%`.
- Failed validation never overwrites the previous profile and never appends a rejected resume document.

### 4. Validation & Error Matrix

| Condition | Behavior |
|---|---|
| Missing, empty, oversized file | HTTP `400`, no profile or asset mutation |
| Unsupported extension | HTTP `400 unsupported resume format` |
| Invalid DOCX/PDF | HTTP `400` with actionable parse error, no half-written document |
| Effective characters `< 60` | HTTP `400`, preserve previous profile |
| Fewer than two resume information classes | HTTP `400`, preserve previous profile |
| Symbol/repetition ratio `>= 60%` | HTTP `400`, preserve previous profile |
| Editing PDF/DOCX | HTTP `400`; only manual/TXT/MD are editable |
| Document missing or not owned by user | HTTP `404` |

### 5. Good/Base/Bad Cases

- Good: Uploading two PDFs produces two independent read-only documents and preserves both original assets.
- Good: A Markdown resume can be switched, rendered, edited, saved, and reused for resume deep dive.
- Base: Only `project_summary` is filled, but it passes field and combined-context checks, so the manual document remains usable.
- Bad: Uploading a second file replaces `resume_summary` and deletes the first file reference.
- Bad: A normal summary masks a `project_summary` made of repeated symbols.

### 6. Tests Required

- TXT/MD/DOCX/PDF import success and invalid file regression.
- Multiple imports append documents without replacing previous documents.
- Manual summary/project save creates or updates one manual document without merging the source fields irreversibly.
- All quality thresholds reject before persistence and preserve old profile data.
- Owned editable documents update; non-editable and foreign documents reject.
- Frontend lint/build and browser checks for empty, read-only, editable, long-content, and mobile document workspace states.

### 7. Wrong vs Correct

#### Wrong

```go
profile.ResumeSummary = extractedText
s.store.SaveUserProfile(user.ID, profile)
```

This destroys the previous resume and bypasses per-document quality and source metadata.

#### Correct

```go
document, err := buildResumeDocument(file)
if err := validateResumeDocument(document.ExtractedText); err != nil { return err }
profile.ResumeDocuments = append(profile.ResumeDocuments, document)
```

The file is validated before persistence and remains independently selectable.
