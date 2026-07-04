# Interview Report Contracts

## Scenario: Interview report knowledge coverage and retraining suggestions

### 1. Scope / Trigger

- Trigger: `GET /api/v1/interviews/sessions/{session_id}/report` exposes cross-layer report fields consumed by the React report page.
- Applies when changing report aggregation, `retrieval_summary`, knowledge coverage, retraining suggestions, or frontend report types.
- Does not cover admin interview-bank APIs or database schema migrations.

### 2. Signatures

- API: `GET /api/v1/interviews/sessions/{session_id}/report`
- Backend aggregation function:

```go
func buildInterviewReportRetrievalSummary(session *domain.InterviewSession) interviewReportRetrievalSummary
```

- Frontend consumer:

```ts
api.interviewReport(token, sessionId)
```

### 3. Contracts

`retrieval_summary` must keep existing fields compatible:

- `summary_text: string`
- `hit_rounds: number`
- `fallback_rounds: number`
- `subject_count: number`
- `subjects: string[]`
- `rounds: Array<{ round, subject, fallback_used, follow_up_type }>`

Additional fields:

- `coverage: InterviewReportKnowledgeCoverage[]`
  - `subject: string`
  - `round_count: number`
  - `hit_count: number`
  - `fallback_count: number`
  - `average_score: number`
  - `lowest_score: number`
  - `weak_dimensions: string[]`
- `retraining_suggestions: InterviewReportRetrainingSuggestion[]`
  - `id: string`
  - `subject: string`
  - `priority: number`
  - `reason: string`
  - `actions: string[]`
  - `target_score: number`
  - `source_rounds: number[]`

The report must not expose interview-bank atom internals: no atom body, principles, pitfalls, follow-up paths, internal retrieval query, vector score, or `selected_atom_snapshots`.

### 4. Validation & Error Matrix

- Session missing or not owned by the current user -> existing `404 interview session not found`.
- No evaluations -> return empty `rounds`, `coverage`, and `retraining_suggestions`; `summary_text` remains `本场暂无追问检索记录。`.
- Missing `FollowUpSubject` -> fall back to first `RetrievedSubjects`, then `QuestionSnapshot.Subject`, then `QuestionSnapshot.Title`.
- Unknown dimension key below threshold -> keep the key as a label instead of dropping the weak signal.
- Long subject/action text -> frontend must wrap text and avoid layout overflow.

### 5. Good/Base/Bad Cases

- Good: multiple evaluations share one subject; coverage aggregates round count, score average, lowest score, hit count, fallback count, and weak dimensions.
- Base: historical sessions only have `QuestionSnapshot.Subject`; coverage still renders a single subject without atom internals.
- Bad: report serializes `selected_atom_snapshots`, `query_text`, atom `principles`, or vector scores to the student-facing response.

### 6. Tests Required

- Backend test for multi-round subject aggregation and stable counters.
- Backend test for weak dimension label mapping.
- Backend test for fallback-derived retraining suggestions.
- Backend test for empty evaluation compatibility.
- Frontend lint and build must pass after type changes.

### 7. Wrong vs Correct

#### Wrong

```go
// Leaks internal atom details into student report.
response["selected_atom_snapshots"] = session.SelectedAtomSnapshots
```

#### Correct

```go
response["retrieval_summary"] = buildInterviewReportRetrievalSummary(session)
```

The summary is a derived, student-safe view. It can mention knowledge-point names and aggregate counters, but it must not expose internal atom content or retrieval diagnostics.
