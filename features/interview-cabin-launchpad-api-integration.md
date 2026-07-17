# 面试舱 Launchpad 接口、启动台与开放组合筛选增强

## 目标

把面试舱用户侧启动台从“纯前端静态轨道列表”推进为“后端聚合接口驱动的启动台”，不仅返回开放轨道，还返回推荐训练、最近训练入口和覆盖摘要，为后续题库治理状态、最低题量门槛和索引增强接入预留稳定契约。

## 修改范围

- 后端新增 `GET /interviews/launchpad` 用户侧聚合接口。
- 前端 API 客户端新增 Launchpad 响应类型和请求方法。
- 后端扩展 `recommended_tracks`、`recent_sessions` 和覆盖摘要。
- 后端扩展 `open_tracks.tags`，供用户侧轻量筛选。
- 面试舱页面优先消费接口轨道，接口失败或返回空列表时回退本地兼容轨道。
- 面试舱页面新增顶部状态区、推荐训练区、覆盖区、开放组合筛选条，并保留现有开放轨道和历史区。
- 增加启动台数据源状态提示，区分后端开放组合、兼容轨道和加载状态。
- 兼容题库模式补齐数据库、网络、操作系统、安全和 DevOps 五道演示题，确保启动台不再只展示数据库 L3。

## 核心实现

- 后端暂时基于首期开放组合与现有 `InterviewQuestion` 可用性生成 `open_tracks`，避免前端展示实际无法启动的组合。
- 五道兼容轨道分别对应现有 `InterviewQuestion` 中可启动的 `database/L3/scenario_analysis`、`network/L3/scenario_analysis`、`os/L3/principle`、`security/L4/scenario_analysis` 和 `devops/L4/scenario_analysis`；卡片摘要直接展示题目标题，其中操作系统轨道显示“load average 高但 CPU 不高怎么排查”。
- 后端基于“未完成会话 -> 用户偏好领域 -> 当前开放轨道”的确定性顺序生成 `recommended_tracks`，保证推荐项仍然可直接启动。
- 后端返回 `recent_sessions` 轻量摘要，前端可直接渲染“继续训练 / 查看报告”入口，而不需要先额外取详情。
- 后端扩展 `coverage.question_roles` 和 `coverage.vector_status_summary`，补足用户侧覆盖摘要所需维度。
- 后端在 atom-backed 轨道模式下聚合 `tags`，前端据此支持 `category / difficulty / question_role / tags` 的本地筛选，不新增后端筛选接口。
- 前端把接口字段适配为现有 `InterviewLaunchTrack` 页面模型，减少 UI 改动面。
- 静态 `launchpadConfig.ts` 继续保留，但语义变为接口异常时的本地兜底。
- 轨道键盘导航、领域 chip、推荐区快捷选择、筛选结果和开始面试参数都统一使用当前有效轨道列表。

## 影响范围

- 普通用户面试舱会在进入页面时请求 Launchpad 接口。
- 创建面试会话接口未改动，仍使用 `domain / difficulty / question_type`。
- 未引入新数据库表、Store 接口或题库治理后台。
- 最近训练摘要和推荐训练只是一层聚合读模型，不改变会话主表结构。
- 开放组合筛选完全在前端本地完成，不改变现有 `GET /interviews/launchpad` 的查询参数。

## 验证方式

- `go test ./...`
- `go test ./internal/httpapi -run "TestInterviewLaunchpad(ReturnsOpenTracksFromAvailableQuestions|IncludesRecommendedTracksAndRecentSessions)" -count=1`
- `go test ./internal/httpapi -run TestInterviewLaunchpadAtomTracksIncludeTags -count=1`
- `npm --prefix frontend run build`
- `npm --prefix frontend run lint`
- 启动本地前后端后，用普通用户登录 `/interviews`，确认“启动台状态 / 推荐训练 / 覆盖摘要 / 可启动训练轨道”四块都能渲染。
- 普通用户登录 `/interviews` 后，确认筛选条存在、轨道卡显示可用性 badge，选择真实筛选项后页面不报错，清空筛选后轨道仍可见。
- 兼容题库模式下确认开放轨道恰好显示上述五道题；选择“操作系统”后只保留“操作系统 L3 / 原理问答 / load average 高但 CPU 不高怎么排查”，并可正常开始面试。

## 已知限制

- 当前后端 Launchpad 在没有正式开放原子组合时仍会回退兼容种子，因此 `published_atom_count / indexed_atom_count` 在 fallback 模式下仍然为 0，不代表最终题库原子统计。
- 推荐训练当前只使用未完成会话、用户偏好领域和开放轨道，不接学习计划与薄弱点回流。
- 动态 RAG 追问和报告追问摘要增强不在本阶段范围内。
- 当前用户侧只筛选后端已经开放的组合，不展示未开放组合灰态卡片，也不显示 `unavailable_reason` 详情页。
