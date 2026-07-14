# 面试舱新发布题库驱动推荐

## 目标

让面试舱启动台的推荐训练不仅能根据未完成会话和历史低分维度给出建议，还能在题库某个方向刚刚更新后，显式提示用户“可以趁热训练”。

## 修改范围

- 后端 `launchpad` 推荐逻辑新增 `source_kind=fresh_content`。
- atom-backed 轨道聚合把最近更新时间保留在后端内部排序逻辑中。
- 前端推荐区继续直接消费后端返回的 `reason`，无需新增展示模块。

## 核心实现

- atom-backed 轨道继续在聚合阶段维护 `latestUpdate`。
- 推荐优先级更新为：
  1. `continue_session`
  2. `weak_dimension`
  3. `fresh_content`
  4. `preferred_domain`
  5. `default_open_track`
- `fresh_content` 推荐只出现在已有开放轨道上，不会推荐当前不能启动的组合。
- 推荐原因直接使用“题库最近更新，适合趁热进入一轮训练验证掌握情况”这类可解释文案。

## 影响范围

- `/api/v1/interviews/launchpad` 的 `recommended_tracks` 现在可能出现 `source_kind=fresh_content`。
- 前端推荐区会自动展示新的推荐原因，不需要额外接口。

## 验证方式

- `go test ./internal/httpapi -run TestInterviewLaunchpadRecommendsRecentlyUpdatedTrack -count=1`
- `go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 当前“最近更新”只用轨道最新 `UpdatedAt` 判定，不区分是小修还是大批量更新。
- 还没有做“常用训练轨道”这类行为型推荐来源。
