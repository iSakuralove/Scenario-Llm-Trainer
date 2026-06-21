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

### 3. Contracts

- Response must include:
  - `summary.open_track_count: number`
  - `summary.published_atom_count: number`
  - `summary.indexed_atom_count: number`
  - `summary.fallback_mode: boolean`
  - `summary.message: string`
  - `domains: Array<{ value, label, group, note, open_track_count? }>`
  - `open_tracks: Array<{ id, title, domain, domain_label, category, difficulty, question_type, question_role, summary, details, published_count, indexed_count, availability_state, vector_status_summary }>`
  - `coverage.domains / coverage.difficulties / coverage.question_types`
  - `fallback_mode: boolean`
- `open_tracks` is the frontend source of truth for visible user-side launch entries.
- Every returned track must be startable by the current `POST /interviews/sessions` contract.
- While the backend uses compatibility seed questions instead of formal `InterviewKnowledgeAtom`, it must return `fallback_mode=true` and must not claim non-zero atom/index statistics.
- `launchpadConfig.ts` may remain as local fallback only; it must not be treated as the authoritative open-track source.

### 4. Validation & Error Matrix

- Missing/invalid auth -> existing `withUser` unauthorized response.
- No compatible backend tracks -> return `open_tracks=[]`; frontend may use local fallback for demo stability.
- Launchpad request fails -> frontend uses local fallback and shows a non-blocking compatibility notice.
- Track returned by API but no matching session question -> invalid backend state; regression tests must prevent this.

### 5. Good/Base/Bad Cases

- Good: API returns only tracks that `FindInterviewQuestion(domain, difficulty, question_type)` can start.
- Base: Compatibility mode returns startable tracks, `fallback_mode=true`, `published_atom_count=0`, `indexed_atom_count=0`.
- Bad: Frontend renders hard-coded tracks as the primary list after API integration.
- Bad: Compatibility API reports formal atom counts before the formal knowledge-bank source is connected.

### 6. Tests Required

- Backend: `GET /interviews/launchpad` returns startable tracks and compatibility metadata.
- Frontend: `npm --prefix frontend run build` verifies response types and page adaptation.
- Frontend: `npm --prefix frontend run lint` catches hook/data-fetching regressions.

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
