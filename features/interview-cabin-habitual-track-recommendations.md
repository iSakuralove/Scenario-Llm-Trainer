# 面试舱常用训练轨道推荐

## 目标

补齐面试舱推荐训练区缺失的“常用训练轨道”信号，让用户在已有推荐面板里看到自己最近最常练的开放轨道。

## 修改范围

- 后端扩展 `GET /api/v1/interviews/launchpad` 的推荐生成逻辑。
- Launchpad 契约新增 `source_kind=habitual_track`。
- 增加回归测试验证推荐顺序和推荐文案。

## 核心实现

- 复用用户历史面试会话，不新增画像表或长期缓存。
- 统计当前仍在 `open_tracks` 内的 `domain + difficulty` 使用频次。
- 至少出现 2 次才视为“常用训练轨道”。
- 同频时按最近一次训练时间决胜。
- 推荐顺序插在 `weak_dimension` 之后、`fresh_content` 之前，避免被纯兜底推荐吞没。
- 仍复用现有去重逻辑；如果同一轨道已经被更高优先级来源命中，则不重复推荐。

## 影响范围

- 面试舱推荐区在已有结构下会多出现一种来源，不需要新增前端组件。
- 不改面试主流程，不改会话存储结构，不增加额外请求。

## 验证方式

- `go test ./internal/httpapi -run TestInterviewLaunchpadRecommendsHabitualTrack -count=1`
- `go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前“常用”只按 `domain + difficulty` 粒度统计，不细分题型和标签。
- 当前要求至少 2 次历史出现才触发，单次训练不会形成 habitual 推荐。
- 若同一轨道已经被 `continue_session` 或 `weak_dimension` 占用，habitual 推荐会因去重而不显示。
