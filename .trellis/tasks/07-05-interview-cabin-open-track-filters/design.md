# 技术设计

## 架构边界

本任务只增强 `launchpad` 轨道字段和面试舱页面本地筛选，不改会话创建和报告接口。

涉及边界：

- `backend/internal/httpapi/handlers_interviews.go`
  补齐 `open_tracks.tags` 与更明确的 availability 文案来源。
- `frontend/src/api/client.ts`
  扩展 `InterviewLaunchpadTrack` 类型。
- `frontend/src/features/interviews/InterviewsPage.tsx`
  增加本地筛选状态、筛选 UI、badge 和空状态。

## 数据模型

`InterviewLaunchpadTrack` 新增：

- `tags: string[]`

可用性展示继续使用：

- `availability_state`
- `vector_status_summary`

## 可用性映射

前端显示标签：

- `available` -> `可训练`
- `indexing` -> `追问增强准备中`
- fallback 兼容模式（由 `launchpadSource === "fallback"` 或 `vector_status_summary === "compatibility_seed"` 推导）-> `兼容轨道`

## 前端筛选策略

新增本地筛选状态：

- `selectedCategory`
- `selectedDifficulty`
- `selectedQuestionRole`
- `selectedTag`

过滤顺序：

1. category
2. difficulty
3. question_role
4. tag

所有筛选都对当前 `launchTracks` 本地过滤，不新增后端查询参数。

## TDD 切法

1. RED：后端测试要求 `open_tracks[*].tags` 存在。
2. GREEN：补 tags 聚合。
3. RED：前端类型和页面消费 `tags` / `availability_state` 后构建失败或缺渲染。
4. GREEN：补筛选器、badge、空状态和清空筛选。

## 风险

- 不能把推荐区和开放组合区的“已选择轨道”状态搞乱。
- fallback 模式下 tags 可能为空，前端必须兼容。
