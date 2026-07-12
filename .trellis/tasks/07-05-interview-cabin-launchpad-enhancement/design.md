# 技术设计

## 架构边界

本任务只增强用户侧启动台聚合接口和对应前端页面。

涉及边界：

- `backend/internal/httpapi/handlers_interviews.go`
  扩展 `interviewLaunchpad()` 返回结构。
- `frontend/src/api/client.ts`
  扩展 `InterviewLaunchpadResponse` 类型。
- `frontend/src/features/interviews/InterviewsPage.tsx`
  新增状态区、推荐区、覆盖区。

不改：

- 面试创建接口 `POST /api/v1/interviews/sessions`
- 报告页接口
- 学习仪表盘接口

## API 设计

在当前 `InterviewLaunchpadResponse` 上追加：

- `recommended_tracks: InterviewLaunchpadRecommendation[]`
- `recent_sessions: InterviewLaunchpadRecentSession[]`
- `coverage.question_roles: string[]`
- `coverage.vector_status_summary: string[]`

### `InterviewLaunchpadRecommendation`

- 复用轨道主字段：
  - `id`
  - `title`
  - `domain`
  - `domain_label`
  - `difficulty`
  - `question_type`
  - `summary`
- 增加：
  - `reason`
  - `source_kind`

### `InterviewLaunchpadRecentSession`

- `id`
- `status`
- `domain`
- `difficulty`
- `question_title`
- `final_score`
- `started_at`
- `ended_at`
- `action_path`

## 推荐策略

首期不做复杂打分，只做确定性排序：

1. 遍历最近面试，优先拿未完成会话对应轨道。
2. 用用户 `preferred_domains` 匹配当前开放轨道。
3. 补齐剩余轨道，按当前开放顺序填满。
4. 最终去重并截断到 4 条。

推荐原因模板：

- 未完成会话：`你有一场未完成的 {domain}/{difficulty} 面试，可直接续练。`
- 偏好领域：`你的个人档案偏好包含 {domain}，建议优先补齐该方向训练。`
- 默认：`这是当前正式开放的训练入口，可直接开始完整面试。`

## 前端设计

- 顶部状态区读取：
  - `summary.open_track_count`
  - `summary.published_atom_count`
  - `summary.indexed_atom_count`
  - `summary.message`
  - `fallback_mode`
- 推荐区读取 `recommended_tracks`
- 覆盖区读取扩展后的 `coverage`
- 最近面试区优先使用 `recent_sessions`，现有历史列表保留为详细面板

## TDD 切法

1. RED：`launchpad` 返回 `recommended_tracks` 与 `recent_sessions`。
2. GREEN：补后端聚合逻辑与类型。
3. RED：前端构建失败，缺少新字段。
4. GREEN：扩展客户端类型和启动台组件。
5. 验证 fallback 模式下仍能渲染。

## 风险

- 不能让 `recommended_tracks` 返回不可启动轨道。
- 不能让前端在新增字段缺失时整页报错；必须继续走 fallback。
