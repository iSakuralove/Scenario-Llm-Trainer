# 面试舱启动台聚合增强 PRD

## 目标

把当前“技术面试舱”从“开放轨道列表页”升级成真正的启动台：进入页面后，用户能同时看到当前可用题库状态、推荐训练入口、开放组合列表、覆盖摘要和最近训练入口，而不是只看到一组可启动轨道。

本任务只做启动台聚合接口和 UI 落地，不把学习闭环回流、Mentor 或 PDF 简历画像混进来。

## 已确认事实

- 当前 `GET /api/v1/interviews/launchpad` 已返回：
  - `summary.open_track_count`
  - `summary.published_atom_count`
  - `summary.indexed_atom_count`
  - `summary.fallback_mode`
  - `summary.message`
  - `domains`
  - `open_tracks`
  - `coverage.domains / difficulties / question_types`
- 当前 [InterviewsPage.tsx](G:\计算机设计大赛\frontend\src\features\interviews\InterviewsPage.tsx) 只把它当作“轨道列表源”，没有落地状态区、推荐区、覆盖区。
- 当前页面已有：
  - `difficulty_level`
  - `focus_areas`
  - `setup_notes`
  - 历史面试列表
- 现有 `launchpadConfig.ts` 仍是兼容兜底，不应继续承担主数据源角色。

## 需求

1. 增强 `GET /api/v1/interviews/launchpad`
   - 保持现有 `open_tracks` 可用。
   - 新增 `recommended_tracks`。
   - 新增 `recent_sessions`。
   - 扩展 `coverage`，补足用户侧展示所需维度。

2. `recommended_tracks`
   - 最多返回 4 条。
   - 必须包含明确推荐原因。
   - 首期推荐信号优先级：
     1. 未完成面试所在轨道
     2. 用户 `preferred_domains`
     3. 当前开放轨道中的高价值组合
   - 如果没有个性化信号，仍然返回可解释的默认推荐，而不是空数组。

3. `recent_sessions`
   - 返回最近面试的轻量摘要：
     - `id`
     - `status`
     - `domain`
     - `difficulty`
     - `question_title`
     - `final_score`
     - `started_at`
     - `ended_at`
   - 前端可直接用它渲染“继续面试/查看报告”入口。

4. `coverage`
   - 在现有 `domains / difficulties / question_types` 基础上，新增：
     - `question_roles`
     - `vector_status_summary`
   - 首期只做摘要统计，不做复杂图表。

5. 前端启动台
   - 新增顶部状态区：展示开放入口数、已发布题量、已索引题量、当前模式。
   - 新增推荐训练区：展示推荐轨道卡和推荐原因。
   - 新增覆盖区：展示 domain / difficulty / role / index 状态摘要。
   - 保留现有开放轨道区和历史区。
   - 仍保留 `launchpadConfig.ts` 作为接口失败兜底。

## 验收标准

- `launchpad` 响应包含 `recommended_tracks`、`recent_sessions` 和扩展后的 `coverage`。
- 前端面试舱能展示顶部状态区、推荐训练区、覆盖区。
- 接口失败时仍会回退到现有兼容轨道，不阻断面试启动。
- 用户可从最近面试入口直接继续未完成会话或查看历史报告。

## 不做

- 不做学习计划回流。
- 不做独立 Mentor 页。
- 不做 PDF 简历解析。
- 不做用户侧不可用组合灰态展示。
