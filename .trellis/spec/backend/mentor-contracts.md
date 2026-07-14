# Mentor Contracts

## Scenario: User Mentor Aggregate API

### 1. Scope / Trigger

- Trigger: 新增或修改独立 `AI Mentor` 页面、`GET /api/v1/users/me/mentor`、Mentor 聚合字段或前端 Mentor 页的数据契约。
- Applies to: `backend/internal/httpapi/handlers_auth.go`, `backend/internal/httpapi/mentor.go`, `frontend/src/api/client.ts`, `frontend/src/features/mentor`。

### 2. Signatures

- API: `GET /api/v1/users/me/mentor`
- Auth: 普通登录用户可访问，走现有 `withUser` 鉴权。
- Frontend client: `api.mentor(token) -> MentorSnapshot`

### 3. Contracts

- Response must include:
  - `generated_at`
  - `overview`
  - `strengths: string[]`
  - `weaknesses: string[]`
  - `risks: Array<{ level, title, message }>`
  - `actions: Array<{ title, detail, action_label, action_path }>`
  - `coverage: { coverage_percent, completed_sessions, subject_count, top_subjects, uncovered_tracks }`
  - `profile: { target_level, target_role, preferred_domains, has_resume_summary, has_project_summary }`
  - `sample_ready: boolean`
- The endpoint must reuse existing internal aggregates such as `learningPlan()` and `interviewLaunchpad()`.
- The endpoint must not call external LLM providers.
- `coverage.uncovered_tracks` is a display-safe label list, not raw track IDs.
- The endpoint must not leak interview-bank atom body, raw retrieval query text, selected atom snapshots, or internal vector diagnostics.

### 4. Validation & Error Matrix

- Missing/invalid auth -> existing unauthorized response.
- Aggregation source unavailable -> existing server error path; do not return partial malformed payload.
- No interview samples -> return `sample_ready=false`, but still return all top-level fields with safe empty arrays / zero counters.

### 5. Good/Base/Bad Cases

- Good: User has at least one completed interview; response returns strengths, weaknesses, actions, and non-zero coverage counters.
- Base: User has zero completed interviews; response still returns `overview`, empty-safe lists, and `sample_ready=false`.
- Bad: Returning raw `open_track` objects or retrieval internals directly from Mentor aggregate.

### 6. Tests Required

- Backend test for non-empty mentor aggregate with completed interview sample.
- Frontend lint / build / smoke after Mentor page uses the new endpoint.

### 7. Wrong vs Correct

#### Wrong

```go
writeOK(w, map[string]interface{}{
  "dashboard": s.learningPlan(user),
  "launchpad": s.interviewLaunchpad(user),
})
```

This leaks internal aggregate shapes into the page and forces the frontend to keep stitching them manually.

#### Correct

```go
writeOK(w, s.mentorSnapshot(user))
```

The backend owns the Mentor read model, and the frontend consumes one stable page-oriented contract.
