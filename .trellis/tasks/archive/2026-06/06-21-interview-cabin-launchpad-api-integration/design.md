# 面试舱 Launchpad 接口接入设计

## 架构边界

本阶段采用兼容层设计：后端先提供用户侧 Launchpad 聚合接口，接口内部暂时基于现有 `InterviewQuestion` 种子题和首期开放组合生成轨道；未来题库治理表接入后，只替换后端计算来源，前端契约保持稳定。

## 数据流

1. 前端进入 `/interviews`。
2. `InterviewsPage` 调用 `api.interviewLaunchpad(token)`。
3. 后端 `handleInterviews` 识别 `/launchpad`，聚合当前可启动轨道。
4. 前端把响应适配成 `InterviewLaunchTrack[]`。
5. 用户选择轨道后，仍调用 `POST /interviews/sessions` 创建面试。
6. 如果 Launchpad 接口失败，前端回退到静态 `interviewLaunchTracks`。

## 后端契约

新增接口：

- `GET /interviews/launchpad`

响应包含：

- `summary`：开放轨道数量、兼容模式、索引增强摘要。
- `domains`：当前可见能力域。
- `open_tracks`：可启动训练轨道。
- `coverage`：领域、难度、题型覆盖摘要。
- `fallback_mode`：是否处于兼容数据源。

首期计算规则：

- 只考虑已确认首期组合：`java/database/cache/ai_llm` + `L2/L3`。
- 每个轨道必须能在现有题库中找到对应 `domain + difficulty + question_type` 的 `InterviewQuestion`。
- `L2` 默认对应 `principle`，`L3` 默认对应 `scenario_analysis`，保持当前前端轨道语义。
- 没有可用题目的组合不返回，避免展示后启动失败。

## 前端契约

新增类型：

- `InterviewLaunchpadResponse`
- `InterviewLaunchpadTrack`
- `InterviewLaunchpadSummary`

适配策略：

- 接口字段使用 snake_case。
- 页面内部继续使用现有 camelCase `InterviewLaunchTrack`，减少 UI 修改面。
- 静态配置保留为 `fallbackInterviewLaunchTracks` 或继续使用现有导出，但语义变为兜底。

## 兼容与降级

- Launchpad 接口失败：前端使用静态配置并提示“当前使用兼容轨道配置”。
- Launchpad 返回空：前端同样使用静态配置，避免演示不可用；后续题库治理稳定后可调整为空状态。
- 创建会话失败：继续展示后端错误；若题库状态变化导致不一致，提示刷新后重试。
- 索引状态异常：不阻断开场训练，只在摘要中温和说明追问增强可能回退。

## 影响范围

- 后端：`backend/internal/httpapi/handlers_interviews.go`。
- 前端 API：`frontend/src/api/client.ts`。
- 前端页面：`frontend/src/features/interviews/InterviewsPage.tsx`。
- 前端配置：`frontend/src/features/interviews/launchpadConfig.ts` 仅保留兜底与标签说明。
- 类型：按需要扩展 `frontend/src/types.ts` 或局部类型。

## 取舍

- 不直接接新题库主表：当前任务目标是先稳定前端契约，避免一次性扩大到题库治理后台。
- 保留静态兜底：符合比赛演示稳定性要求，但页面需要明确其为兼容模式，不再把它当最终事实。
- 不拆复杂组件：本阶段重点是数据来源切换，避免同时做大规模 UI 重构。
