# 面试题库健康诊断与检索预览

## 目标

让管理员在用户真实开启面试前，判断题库组合是否具备开场题、追问题和可检索索引，并能用模拟文本预览追问召回效果。

## 修改范围

- 后端新增题库健康诊断和检索预览管理接口。
- 题库向量检索查询补充可选 `domain` 过滤，避免跨领域误召回。
- 前端题库管理页新增健康诊断面板和检索预览面板。
- 前端 API 类型和 client 方法补齐。
- 架构文档补充新增接口边界。

## 核心实现

- `GET /api/v1/admin/interview-bank/health` 按 `domain + category + difficulty` 聚合题库组合，返回 `open / warning / blocked` 状态、原因和建议动作。
- 组合只有同时具备可用开场题、追问题和至少一个已索引追问资源时才视为可开放。
- `POST /api/v1/admin/interview-bank/retrieval-preview` 使用管理员输入的模拟文本执行题库追问检索预览，只返回轻量命中信息和诊断数据。
- 检索预览要求 embedding 可用；不可用时返回明确 fallback 原因，不用文本相似度伪造向量命中。
- 检索预览不创建面试会话、不写正式检索日志、不修改题目版本或索引状态。

## 影响范围

- 管理员可以在题库页直接看到组合健康矩阵、待索引、索引失败和阻断原因。
- 管理员可以从健康诊断定位对应组合筛选，并执行异常索引重建。
- 普通用户面试创建、动态追问和报告链路保持不变。
- 向量检索支持 `domain` 过滤后，管理端预览能严格按三维组合诊断。

## 验证方式

- `go test ./internal/httpapi -run AdminInterviewBank`
- `go test ./internal/store`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 不包含异步索引任务队列。
- 不包含报告知识点分布。
- 不包含历史版本回滚。
- `tags` 仍不进入健康诊断聚合维度，后续等标签规范化成熟后再扩展。
