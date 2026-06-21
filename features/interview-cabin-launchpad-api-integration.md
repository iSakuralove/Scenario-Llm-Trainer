# 面试舱 Launchpad 接口接入

## 目标

把面试舱用户侧启动台从纯前端静态轨道推进为后端开放组合接口驱动，为后续题库治理状态、最低题量门槛和索引增强接入预留稳定契约。

## 修改范围

- 后端新增 `GET /interviews/launchpad` 用户侧聚合接口。
- 前端 API 客户端新增 Launchpad 响应类型和请求方法。
- 面试舱页面优先消费接口轨道，接口失败或返回空列表时回退本地兼容轨道。
- 增加启动台数据源状态提示，区分后端开放组合、兼容轨道和加载状态。

## 核心实现

- 后端暂时基于首期开放组合与现有 `InterviewQuestion` 可用性生成 `open_tracks`，避免前端展示实际无法启动的组合。
- 前端把接口字段适配为现有 `InterviewLaunchTrack` 页面模型，减少 UI 改动面。
- 静态 `launchpadConfig.ts` 继续保留，但语义变为接口异常时的本地兜底。
- 轨道键盘导航、领域 chip 和开始面试参数都统一使用当前有效轨道列表。

## 影响范围

- 普通用户面试舱会在进入页面时请求 Launchpad 接口。
- 创建面试会话接口未改动，仍使用 `domain / difficulty / question_type`。
- 未引入新数据库表、Store 接口或题库治理后台。

## 验证方式

- `go test ./...`
- `npm --prefix frontend run build`
- `npm --prefix frontend run lint`

## 已知限制

- 当前后端 Launchpad 仍是兼容层，尚未接入正式 `InterviewKnowledgeAtom` 题库治理状态。
- `published_atom_count / indexed_atom_count` 暂以兼容数据源表达，不代表最终题库原子统计。
- 动态 RAG 追问和报告追问摘要增强不在本阶段范围内。
