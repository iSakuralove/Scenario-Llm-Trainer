# 面试题库运营动作闭环 PRD

## Problem Statement

管理员现在已经可以看到面试题库的导入、编辑、归档、索引重建、健康诊断、模拟检索预览和真实检索运营数据，但这些数据仍停留在“观察”层：命中率、回退率、低命中原子、失败索引和阻断组合能被看见，却不能被稳定转化为可跟进、可解释、可关闭的题库建设动作。

从管理员视角看，当前痛点是：

- 看到某个 `domain + category + difficulty` 经常回退，但需要人工记住要补哪些题。
- 看到低/未命中的面试知识原子，但不知道该进入“观察、修题、归档候选”哪一种处理状态。
- 看到索引失败、健康诊断阻断和真实回退组合时，需要在多个面板之间来回切换，缺少统一的处理队列。
- 完成编辑、归档、恢复或重建索引后，没有一个轻量位置记录“这个问题已经处理过，后续观察即可”。
- 团队后续复盘时只能看最终题库状态，难以解释某次补题、修题或重建索引是由哪些真实运营信号触发的。

这个问题会阻断面试题库从“可维护”走向“可运营”。如果不补这一层，真实检索运营看板只能告诉管理员哪里有问题，不能帮助管理员稳定地把问题变成建设动作。

## Solution

新增 **题库运营动作** 能力，把真实检索日志、健康诊断、索引状态和低效资源信号转化为管理员可跟进的处理项。

首期解决方案是“显式生成 + 人工处理 + 轻量闭环”：

- 管理员在面试题库管理页查看真实检索运营数据后，可以显式生成题库运营动作。
- 系统基于确定性规则生成动作候选，不调用 LLM，不自动修题。
- 动作候选经过去重后进入运营动作队列，每条动作保留触发证据快照。
- 管理员可以打开相关面试知识原子、套用组合到健康诊断/检索预览、进入编辑/归档/重建索引入口。
- 管理员可以把动作标记为处理中、已解决、已忽略或观察中，并必须填写关闭/忽略备注。
- 动作详情展示触发原因、证据来源、建议处理方式、关联组合、关联原子、处理历史和最后更新时间。
- 完成动作不会自动修改题库内容；题库修改仍走现有在线编辑、归档/恢复和索引重建能力。

这不是一个通用工单系统，也不是 AI 自动运营系统。它只服务面试题库管理，把已有观测面板中的信号沉淀为最小可执行闭环。

## User Stories

1. As an admin, I want to generate operations actions from real retrieval analytics, so that high fallback combinations become follow-up work instead of screenshots or memory.
2. As an admin, I want to see a queue of open interview bank operations actions, so that I know what to fix first.
3. As an admin, I want each action to show whether it came from real retrieval logs, health diagnostics, index status, or low hit atoms, so that I can trust why it exists.
4. As an admin, I want fallback combinations to generate “补题” actions, so that I can add opening or follow-up resources for weak combinations.
5. As an admin, I want blocked health combinations to generate “修复组合” actions, so that launchpad-visible tracks stay backed by enough published resources.
6. As an admin, I want warning health combinations to generate lower-priority actions, so that I can decide whether to repair pending or failed index resources.
7. As an admin, I want vector failed atoms to generate “重建索引” actions, so that retrieval enhancement can recover without scanning the whole list.
8. As an admin, I want low hit atoms to generate “观察/修题” actions, so that long-term unused resources are reviewed instead of silently accumulating.
9. As an admin, I want archived atoms to stay out of action generation unless explicitly referenced by existing open actions, so that archived content does not create noise.
10. As an admin, I want action candidates to deduplicate against existing open actions, so that repeated page refreshes do not create duplicate work.
11. As an admin, I want to preview generated action candidates before saving them, so that I can avoid polluting the queue with obvious noise.
12. As an admin, I want to create selected candidates only, so that I can keep the queue small and intentional.
13. As an admin, I want each action to have a type, priority, status, source, reason, and evidence snapshot, so that later reviewers can understand it without recalculating old analytics.
14. As an admin, I want action priority to be deterministic, so that high-frequency fallbacks rank above low-severity observations.
15. As an admin, I want to filter actions by status, type, priority, domain, category, difficulty, and source, so that I can focus on one operational slice.
16. As an admin, I want to open the related atom from an action, so that I can edit, archive, restore, or inspect version history without searching again.
17. As an admin, I want to apply an action’s combination to the existing health diagnostic and retrieval preview filters, so that I can reproduce the problem quickly.
18. As an admin, I want to trigger the existing selected-atom index rebuild flow from a rebuild-related action, so that failed index recovery stays inside the current safe rebuild boundary.
19. As an admin, I want action status transitions to require a note when resolving or dismissing, so that closure decisions are auditable.
20. As an admin, I want to mark an action as “观察中”, so that weak signals can be tracked without implying immediate content changes.
21. As an admin, I want to reopen a resolved or dismissed action, so that a mistaken closure can be corrected.
22. As an admin, I want action history to show who changed status and when, so that operations decisions are traceable.
23. As an admin, I want actions to show the latest related atom status and vector status, so that I can see whether the underlying resource changed after action creation.
24. As an admin, I want actions to preserve their original evidence snapshot, so that later changes do not erase why the action was created.
25. As an admin, I want a newly generated action to include recent fallback reason text when safe, so that I can understand the issue without exposing full user answers.
26. As an admin, I want query text in evidence to follow the same sanitization and truncation policy as retrieval logs, so that operations actions do not leak sensitive content.
27. As an admin, I want action generation to use bounded analytics windows, so that refreshing the page does not create expensive database scans.
28. As an admin, I want to see empty states for “no candidate actions” and “no open actions”, so that a healthy topic bank is distinguishable from a loading failure.
29. As an admin, I want API errors to leave the existing analytics panel usable, so that operations action failure does not break read-only monitoring.
30. As an admin, I want non-admin users to be forbidden from all operations action APIs, so that internal governance evidence is not exposed.
31. As an instructor, I should not see operations actions in my normal workflow, so that case workshop review remains separate from interview bank governance.
32. As a student, I should never see operations actions, fallback evidence, source references, or version details, so that the interview experience remains focused and safe.
33. As a developer, I want the action candidate generator to be a deep module with a simple input and output, so that its rules can be tested without HTTP or database setup.
34. As a developer, I want Store behavior for operations actions to be consistent between memory and PostgreSQL modes, so that local tests and persistent deployments behave the same.
35. As a developer, I want operations action status transitions to be validated in one place, so that invalid state changes do not drift across handlers.
36. As a developer, I want action generation to avoid LLM calls, so that this feature remains deterministic, cheap, and testable.
37. As a developer, I want action evidence shape to avoid full atom body content, so that operations records do not duplicate large content snapshots.
38. As a developer, I want schema changes to stay aligned between runtime schema and initial migration, so that memory, local PostgreSQL, and Docker initialization do not drift.
39. As a maintainer, I want this feature to reuse existing edit, archive, restore, health, preview, analytics, and rebuild interfaces, so that it does not fork governance behavior.
40. As a maintainer, I want action generation rules to be documented in the PRD and tests, so that future automatic recommendations do not silently change operating policy.
41. As an admin, I want to know when an action is stale because its related atom was archived or removed from the active pool, so that I can close it intentionally.
42. As an admin, I want action detail to show the current version of a related atom when available, so that I can decide whether prior edits already addressed the issue.
43. As an admin, I want generated actions to avoid using old `InterviewQuestion` fallback as a normal repair target, so that operations stay focused on the new题库体系.
44. As an admin, I want the queue to highlight actions that block ordinary users from seeing a launchpad combination, so that user-facing availability issues rank first.
45. As an admin, I want the queue to separate “内容不足” from “索引失败”, so that I do not waste time editing content when a rebuild is enough.
46. As an admin, I want the queue to separate “长期未命中” from “低命中但仍有效”, so that archival candidates are reviewed carefully.
47. As an admin, I want to dismiss a generated candidate before it becomes a saved action, so that one-off noise does not create lasting work.
48. As an admin, I want action creation to record the analytics window used, so that counts such as fallback frequency remain interpretable.
49. As an admin, I want the action list to show counts and reasons compactly, so that it remains useful on smaller screens.
50. As a product owner, I want the feature to stop at human-operated actions, so that automatic content mutation can be evaluated later as a separate, higher-risk scope.

## Implementation Decisions

- The canonical term is **题库运营动作**. It means an admin-followed governance item created from interview bank operational signals. It does not mean automatic repair.
- The first release should be admin-only and live inside the existing 面试题库管理 surface.
- The feature should introduce a persisted operations action model rather than only rendering ephemeral suggestions. Without persisted status, there is no real closure loop.
- The persisted action should store a compact evidence snapshot. Evidence must be stable enough for later audit but must not duplicate full atom content, full answer text, resume text, or project background.
- Action types should be constrained to a small enum:
  - `fill_gap`: a combination appears under-supplied or repeatedly falls back.
  - `fix_atom`: an atom is low quality, low hit, or suspected ineffective.
  - `rebuild_index`: an atom or combination is blocked by pending/failed index state.
  - `review_archive`: a published atom is persistently low/unused and may be a归档候选.
  - `observe`: weak signal that should be watched before changing content.
- Action source should be constrained to:
  - `retrieval_analytics`
  - `retrieval_log`
  - `health_diagnostic`
  - `index_status`
  - `manual`
- Action status should be constrained to:
  - `open`
  - `in_progress`
  - `watching`
  - `resolved`
  - `dismissed`
  - `reopened`
- Closing states (`resolved`, `dismissed`) require a non-empty note.
- Reopening an action should preserve previous history and append a new transition, not create a duplicate action.
- Action priority should be deterministic:
  - P0: user-visible blocked combination or repeated fallback on an open combination.
  - P1: high fallback rate, no indexed follow-up resource, or repeated index failure.
  - P2: low/unused published atom, warning health combination, or stale pending index.
  - P3: observation-only candidates.
- The action candidate generator should be a deep module. It should accept current analytics, health diagnostics, atom summaries, and limited configuration, then return candidate actions with reasons and stable dedupe keys.
- Dedupe keys should be based on action type plus stable target:
  - combination target: `type + domain + category + difficulty`
  - atom target: `type + atom_id`
  - mixed target: `type + atom_id + domain + category + difficulty`
- Candidate generation must not write automatically. It returns a preview list. A separate save operation creates selected actions.
- Saved actions should store the dedupe key and refuse creating another active action with the same key unless the previous one is resolved or dismissed.
- Manual action creation should be allowed for admins, but it should require type, reason, and target scope.
- The feature should not add an independent generic task system. It is scoped only to interview bank operations.
- Store support should include:
  - create selected actions
  - list actions with filters
  - get action detail
  - update status with note
  - append transition history
  - find active action by dedupe key
- MemoryStore and PostgreSQL Store must have the same behavior for action creation, filtering, clone safety, and status transitions.
- The database model should include a main actions table and a compact history table if history would otherwise make the main record mutable and hard to reason about.
- The action evidence shape should be JSON-compatible and versioned lightly so future signal fields can be added without breaking existing records.
- API shape should stay simple:
  - `GET /admin/interview-bank/ops-actions`
  - `POST /admin/interview-bank/ops-actions/candidates`
  - `POST /admin/interview-bank/ops-actions`
  - `GET /admin/interview-bank/ops-actions/{id}`
  - `PATCH /admin/interview-bank/ops-actions/{id}`
- Candidate generation endpoint should be read-like in behavior but use POST because it accepts filters and a generation policy payload.
- Saving actions should accept a list of candidate keys or full candidate payloads from the candidate response. The backend must revalidate and normalize before persistence.
- The frontend should add a “运营动作” panel near the true retrieval operations panel, not replace health diagnostics or retrieval preview.
- The frontend should expose a candidate preview state, a saved action queue, and detail drawer/panel.
- The frontend should reuse existing atom detail opening behavior for action targets.
- The frontend should reuse existing combination filter application behavior for combination targets.
- The frontend should not submit edit/archive/rebuild operations implicitly when changing an action status.
- Action resolution should be a human decision. The system may display current related state, but it must not auto-resolve actions in this release.
- No new LLM call should be introduced.
- No automatic归因, automatic改题, automatic归档, automatic恢复, or automatic重建索引 should be included.
- The feature should not change user-side interview report shape.
- The feature should not expose source references or governance details to students.
- The feature should not use old `InterviewQuestion` as a normal operations target. Old questions remain compatibility fallback only.
- If a related atom no longer exists or is archived, the action should show that as current context and let the admin resolve or dismiss it.
- Analytics windows should be capped. Defaults should match existing retrieval analytics philosophy: small bounded windows, explicit limit caps, no unbounded scans.
- Candidate generation should use real logs and health diagnostics as inputs. It should not run vector search or embedding calls.
- This feature should be compatible with the current `admin-only` interview bank route boundary.
- The issue tracker label for this PRD is `ready-for-agent`.

## Testing Decisions

- 本任务后续实现必须走 TDD 的 red-green-refactor 循环，不允许“先写完全部实现，再补一批测试”的横切式做法。
- 每个实现 issue 都应按 tracer bullet 垂直切片推进：先写一个通过公开接口验证单一行为的失败测试，再写最小实现让它通过，然后继续下一个行为。
- 测试必须验证外部可观察行为，而不是私有函数、内部调用顺序或数据库细节。
- 首个 tracer bullet 已确认从“管理员手工创建一个题库运营动作，并能通过 admin API 列表读回”开始，因为它能同时验证权限、Store、schema、API 和最小领域模型。
- 候选生成相关逻辑可以作为 deep module 测试，但测试入口仍应表达业务行为，例如“高回退组合生成 fill_gap 候选”，不要测试内部排序 helper 或中间 map 结构。
- UI 相关实现也应先锁定用户可见行为：空状态、候选预览、保存结果、动作详情、状态切换错误提示，而不是快照测试整页 DOM。
- Tests should focus on externally visible behavior and state transitions, not implementation details such as private helper ordering.
- Candidate generator tests should be pure unit tests:
  - high fallback combination creates a fill-gap candidate
  - blocked health combination creates a high-priority candidate
  - failed index atom creates a rebuild-index candidate
  - low-hit published follow-up atom creates an observe or review candidate
  - archived/draft atoms do not create normal action candidates
  - duplicate signals produce one candidate with merged evidence
  - priority ordering is deterministic
- Store tests should cover MemoryStore and PostgreSQL-compatible behavior:
  - create action
  - list by status/type/priority/combination
  - active dedupe key prevents duplicate open action
  - resolved action allows a future new action with same key
  - status transition requires note for resolved/dismissed
  - history is appended and clone-safe
  - evidence JSON round-trips without mutating caller-owned slices/maps
- HTTP API tests should cover:
  - admin-only access
  - non-admin forbidden
  - candidate generation does not persist
  - selected candidate save persists normalized actions
  - duplicate active action is skipped or returned as existing according to API contract
  - status update validation
  - invalid enum rejection
  - limit/filter parsing
- Frontend validation should cover:
  - action panel empty state
  - candidate preview state
  - saved action list and filters
  - open related atom detail
  - apply related combination to existing filters
  - status update loading and error states
  - long reason/evidence text wrapping on narrow screens
- Regression tests should confirm:
  - retrieval preview still does not create formal retrieval logs
  - formal retrieval analytics remains read-only
  - action generation does not call embeddings or LLM providers
  - user-side report response does not gain admin evidence fields
- Required validation commands:
  - backend targeted HTTP/store tests
  - backend full test suite if runtime or shared store contracts change
  - frontend lint
  - frontend build
- Browser smoke should verify in a logged-in admin session:
  - generate candidates from current analytics
  - save selected candidates
  - open an action detail
  - open related atom detail
  - apply a combination to filters
  - resolve an action with a note

## Out of Scope

- 自动修题。
- 自动补题。
- 自动归档或自动恢复归档。
- 自动重建索引任务队列。
- LLM 自动归因或质量评审。
- 通用任务系统、多人协作工单系统或通知系统。
- 普通用户报告页展示运营动作。
- instructor 参与题库运营动作。
- 长周期趋势分析、图表报表、周报/月报。
- 基于运营动作自动生成学习计划或复训任务。
- 从案例工坊自动生成面试知识原子。
- 修改现有版本历史模型或回滚到历史版本。
- 引入 Qdrant、独立 embedding-service、MySQL 或第二套运行时。

## Further Notes

- 本 PRD 继承 ADR 0001 的架构决策：继续采用能力迁移接入，不做运行时并入。
- 本 PRD 依赖已经存在的面试题库管理、版本治理、归档/恢复、索引重建、健康诊断、检索预览和真实检索运营看板。
- 本 PRD 新增的关键术语是 **题库运营动作**，已纳入项目 `CONTEXT.md`。
- 当前最重要的设计约束是“动作闭环不等于自动化修复”。系统只提供证据、队列、跳转和状态记录，最终内容治理动作仍由管理员显式执行。
- 建议先实现 deterministic candidate generator 和 Store/API，再做前端面板；这样可先用后端测试锁定动作规则，降低 UI 返工。
- 如果未来要做自动修题或 LLM 归因，应另开 PRD 和 ADR，因为它会改变治理责任边界、隐私边界和错误风险。
