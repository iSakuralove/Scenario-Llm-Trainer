# 技术设计

## 架构边界

- 后端继续复用 `backend/internal/httpapi/handlers_interview_bank.go` 承载题库管理接口。
- Store 层优先复用现有题库列表、摘要、向量检索和索引状态读写能力；缺口只补最小聚合查询或内存聚合函数。
- 前端继续扩展现有 `InterviewBankAdminPage`，避免新增独立后台入口。
- 本任务只新增管理端观测和预检能力，不改变用户侧面试创建、追问、报告流程。

## 后端接口草案

### `GET /api/v1/admin/interview-bank/health`

返回：

- 总体摘要：总题量、published、archived、pending、failed、indexed。
- 组合列表：`domain`、`category`、`difficulty`、opening 数、followup 数、mixed 数、indexed followup 数、pending 数、failed 数、状态、原因。
- 建议动作：补开场题、补追问题、重建索引、修复失败索引等轻量提示。

### `POST /api/v1/admin/interview-bank/retrieval-preview`

请求：

- `domain`
- `category`
- `difficulty`
- `query`
- 可选 `limit`

返回：

- `matched_count`
- `fallback_used`
- `fallback_reason`
- `results[]`：只返回管理端可见的轻量题目信息和命中摘要，不返回内部 embedding 向量。
- `diagnostics`：候选题数量、indexed 候选数量、过滤原因统计。

## 数据与性能

- 健康诊断优先使用数据库聚合，内存模式使用现有列表做 O(n) 聚合。
- 聚合维度固定，避免按自由 tags 造成高基数统计。
- 检索预览单次限制 `limit`，默认 5，最大 20，避免管理员输入导致大范围召回。
- 预览接口不写入正式面试日志，避免影响运营数据。

## 兼容性

- 新接口均为 admin-only，不影响现有普通用户 API。
- 若 embedding provider 不可用，检索预览应返回明确 fallback 诊断，而不是伪造命中。
- 现有 `summary` 接口保持兼容；健康诊断作为新增接口承载更细粒度信息。

## 风险

- 如果直接复制运行时追问逻辑，可能引入重复算法分叉；实现时应抽出或复用现有检索 helper。
- 如果健康规则过重，会变成审批系统；MVP 只做诊断和建议，不自动改变组合开放策略。
