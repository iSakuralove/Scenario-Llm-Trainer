# Interview Launchpad Contracts

## Scenario: User Launchpad Open Track API

### 1. Scope / Trigger

- Trigger: 修改面试舱用户侧启动台、开放组合 API、前端轨道数据源或创建面试前置选择逻辑。
- Applies to: `backend/internal/httpapi/handlers_interviews.go`, `frontend/src/api/client.ts`, `frontend/src/features/interviews/InterviewsPage.tsx`, `frontend/src/features/interviews/launchpadConfig.ts`。

### 2. Signatures

- API: `GET /api/v1/interviews/launchpad`
- Auth: 普通登录用户可访问，走现有 `withUser` 鉴权。
- Frontend client: `api.interviewLaunchpad(token) -> InterviewLaunchpadResponse`
- Runtime start remains: `POST /api/v1/interviews/sessions` with `domain / difficulty / question_type`。
- Frontend session-page readiness check: `import('./InterviewSessionRoute')` must resolve before the frontend sends `POST /api/v1/interviews/sessions`.

### 3. Contracts

- Response must include:
  - `summary.open_track_count: number`
  - `summary.published_atom_count: number`
  - `summary.indexed_atom_count: number`
  - `summary.fallback_mode: boolean`
  - `summary.message: string`
  - `domains: Array<{ value, label, group, note, open_track_count? }>`
  - `open_tracks: Array<{ id, title, domain, domain_label, category, difficulty, question_type, question_role, tags, summary, details, published_count, indexed_count, availability_state, vector_status_summary }>`
  - `recommended_tracks: Array<{ id, title, domain, domain_label, category, difficulty, question_type, question_role, summary, details, published_count, indexed_count, availability_state, vector_status_summary, reason, source_kind }>`
  - `recent_sessions: Array<{ id, status, domain, difficulty, question_title, final_score?, weak_dimension?, weak_score?, started_at, ended_at?, action_path }>`
  - `coverage_stats: { total_open_tracks, practiced_open_tracks, coverage_percent, completed_sessions, practiced_domains, practiced_difficulties, subject_count, top_subjects, uncovered_track_ids }`
  - `coverage.domains / coverage.difficulties / coverage.question_types / coverage.question_roles / coverage.vector_status_summary`
  - `fallback_mode: boolean`
- `open_tracks` is the frontend source of truth for visible user-side launch entries.
- `open_tracks[*].tags` is the frontend source of truth for lightweight tag filtering; if no tags exist, return `[]`, not `null`.
- `recommended_tracks` is the frontend source of truth for the recommendation panel; each item must still be startable by the existing session-start contract.
- `recommended_tracks.source_kind` may be `continue_session`, `weak_dimension`, `habitual_track`, `fresh_content`, `preferred_domain`, or `default_open_track`.
- `recent_sessions` is a lightweight launchpad read model for “continue training / view report” shortcuts; it must not require the frontend to fetch question detail before rendering the summary card.
- `recent_sessions.weak_dimension / weak_score` are optional and only appear when a completed session has a meaningful weakest-dimension signal for user display.
- `coverage_stats` is the frontend source of truth for “我的训练覆盖率”; it is user-scoped history aggregation, not题库开放范围统计.
- `coverage_stats.total_open_tracks` uses the current `open_tracks` list as the denominator.
- `coverage_stats.practiced_open_tracks` only counts completed user sessions whose `domain + difficulty` match current `open_tracks`.
- `coverage_stats.coverage_percent` is the rounded percentage of practiced open tracks over total open tracks.
- `coverage_stats.subject_count / top_subjects` come from historical report-safe retrieval coverage aggregation and must not expose atom body, internal query text, selected atom snapshots, vector score, or full retrieval diagnostics.
- `coverage_stats.uncovered_track_ids` only returns launch track IDs so the frontend can map them to visible labels without a second endpoint.
- Every returned track must be startable by the current `POST /interviews/sessions` contract.
- While the backend uses compatibility seed questions instead of formal `InterviewKnowledgeAtom`, it must return `fallback_mode=true` and must not claim non-zero atom/index statistics.
- The memory/Postgres compatibility baseline exposes exactly five startable demo tracks: `database/L3/scenario_analysis`, `network/L3/scenario_analysis`, `os/L3/principle`, `security/L4/scenario_analysis`, and `devops/L4/scenario_analysis`. Their card summaries use the corresponding seeded question titles so users can distinguish the five questions before starting.
- `launchpadConfig.ts` may remain as local fallback only; it must not be treated as the authoritative open-track source.

### 4. Validation & Error Matrix

- Missing/invalid auth -> existing `withUser` unauthorized response.
- No compatible backend tracks -> return `open_tracks=[]`; frontend may use local fallback for demo stability.
- No recommendation signal -> still return a non-empty `recommended_tracks` list built from current open tracks, unless `open_tracks=[]`.
- Launchpad request fails -> frontend uses local fallback and shows a non-blocking compatibility notice.
- Track returned by API but no matching session question -> invalid backend state; regression tests must prevent this.
- Compatibility mapping exists but `FindInterviewQuestion(domain, difficulty, question_type)` misses -> omit that track instead of rendering an unstartable card; the five-track baseline test must fail until the mapping and seed are aligned.
- Session page module load fails before create -> stay on `/interviews`, show a retryable error, and send no session-create request.

### 5. Good/Base/Bad Cases

- Good: API returns only tracks that `FindInterviewQuestion(domain, difficulty, question_type)` can start.
- Good: User has an unfinished `database / L3` interview, and launchpad returns that track first in `recommended_tracks` with a non-empty `reason`.
- Good: User recently finished a `database / L3` interview with `technical_accuracy=55`; launchpad returns a `source_kind=weak_dimension` recommendation whose reason names `技术准确性`.
- Good: User最近两次都练 `database / L3`，且当前没有 unfinished/weak-dimension 抢占时，launchpad returns one `source_kind=habitual_track` recommendation whose reason明确说明这是最近最常练的轨道。
- Good: Atom-backed `cache / L3` track is the most recently updated published track; launchpad returns a `source_kind=fresh_content` recommendation whose reason mentions `最近更新` or `新发布题库`.
- Good: `recent_sessions[0]` points to an unfinished session with `action_path=/interviews/session/:id`, while a finished session points to `/interviews/session/:id/report`.
- Good: A completed recent session returns `weak_dimension / weak_score`, allowing the frontend to show “最低维度”摘要 in the recent-session card.
- Good: Atom-backed `java / L3` launch track returns normalized `tags`, so the frontend can render tag filters without guessing.
- Good: A user who only completed one of two open tracks receives `coverage_stats.total_open_tracks=2`, `practiced_open_tracks=1`, `coverage_percent=50`, and one `uncovered_track_id`.
- Good: `coverage_stats.top_subjects` only contains report-safe subject names such as `慢查询定位`, not atom internals or retrieval payloads.
- Base: Compatibility mode returns startable tracks, `fallback_mode=true`, `published_atom_count=0`, `indexed_atom_count=0`.
- Good: The compatibility launchpad returns five tracks, and `os-l3-principle` displays `操作系统 L3 / load average 高但 CPU 不高怎么排查 / 原理问答` while remaining startable through the existing session API.
- Bad: Frontend renders hard-coded tracks as the primary list after API integration.
- Bad: A new `InterviewQuestion` is added to storage seeds but omitted from `interviewLaunchpadSeeds` or `interviewLaunchpadDomainSeeds`, leaving a startable question invisible in the user launchpad.
- Bad: Compatibility API reports formal atom counts before the formal knowledge-bank source is connected.
- Bad: Launchpad returns a recommendation for a track that is not present in `open_tracks`.
- Bad: Frontend creates a session first and only then loads the session route chunk; a chunk failure leaves an unusable history record.

### 6. Tests Required

- Backend: `GET /interviews/launchpad` returns startable tracks, tags, recommendation items, recent session summaries, coverage stats, and compatibility metadata.
- Backend compatibility regression: assert the five exact track IDs, `open_track_count=5`, every track resolves through `FindInterviewQuestion`, and the operating-system track exposes its expected title, summary, role metadata, and launch parameters.
- Frontend: `npm --prefix frontend run build` verifies response types and page adaptation.
- Frontend: `npm --prefix frontend run lint` catches hook/data-fetching regressions.
- Frontend E2E: abort `InterviewSessionRoute.tsx` loading, assert the page remains on `/interviews`, the error states that no record was created, and `POST /interviews/sessions` count remains zero.

### 7. Wrong vs Correct

#### Wrong

```typescript
interviewLaunchTracks.map((track) => <TrackCard track={track} />)
```

This treats local static config as the permanent source of open tracks and can show entries the backend cannot start.

#### Correct

```typescript
const tracks = response.open_tracks.map(launchpadTrackToView)
setLaunchTracks(tracks.length > 0 ? tracks : fallbackInterviewLaunchTracks)
```

The backend response drives normal rendering; local config is only a compatibility fallback.

#### Wrong: create before route readiness

```typescript
const session = await api.createInterview(token, payload)
navigate(`/interviews/session/${session.session_id}`)
```

If the lazy session chunk fails after creation, history is polluted by a session the user never entered.

#### Correct: route readiness before create

```typescript
await import('./InterviewSessionRoute')
const session = await api.createInterview(token, payload)
navigate(`/interviews/session/${session.session_id}`)
```

The session is persisted only after the required UI module is available.

#### Wrong: seed exists but launchpad mapping is missing

```go
// Only adding the question makes direct POST /interviews/sessions work,
// but users still cannot select it from the launchpad.
seedInterviewQuestions(now)
```

#### Correct: keep the compatibility read model startable and visible

```go
interviewLaunchpadSeed{
    ID: "os-l3-principle", Domain: "os", Difficulty: "L3", QuestionType: "principle",
    Title: "操作系统 L3", Summary: "load average 高但 CPU 不高怎么排查",
}
```

The launchpad seed and domain mapping must be added together and covered by a test that calls `FindInterviewQuestion` for the returned tuple.
