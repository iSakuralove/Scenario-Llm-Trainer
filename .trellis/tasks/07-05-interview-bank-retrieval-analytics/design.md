# 技术设计

## 架构边界

本任务补齐已存在 `interview_retrieval_logs` 表的写入、查询和管理端展示链路：

- `domain` 增加检索日志过滤、分析响应、命中排行和回退组合类型。
- `store` 增加检索日志保存、查询和聚合接口，MemoryStore/PostgresStore 同步实现。
- `httpapi/interview_runtime.go` 在追问检索结束后写入轻量日志。
- `httpapi/handlers_interview_bank.go` 增加 admin-only 日志和分析接口。
- 前端 `InterviewBankAdminPage` 增加真实检索运营面板。

不改变用户侧报告接口和面试提交流程。

## 数据流

1. 用户提交答案后，现有评分链路触发追问检索。
2. 检索函数生成 query、执行向量检索或回退规则。
3. 运行时将 session、round、脱敏 query、matched atoms、fallback 状态和原因写入 Store。
4. 管理端 API 从 Store 查询最近日志并聚合命中/回退指标。
5. 前端管理页展示摘要、排行和最近日志，支持打开原子详情或套用组合筛选。

## 日志字段

复用 `domain.InterviewRetrievalLog`：

- `id`
- `session_id`
- `round`
- `query_text`
- `matched_atoms`
- `fallback_used`
- `error_message`
- `created_at`

查询过滤新增：

- `domain`
- `category`
- `difficulty`
- `fallback_used`
- `limit`

过滤中的 `domain/category/difficulty` 基于 `matched_atoms` 和 session question snapshot 聚合判断；Postgres 可先取有限日志后在 Go 内过滤，首期避免复杂 JSONB 查询。

## 安全策略

- `query_text` 使用现有脱敏逻辑后截断到 500 字。
- 写日志失败只记录内部降级，不影响用户面试。
- 管理端接口只返回 admin 可见数据，不返回用户 ID。
- 最近日志默认最多 50 条，最大 200 条；分析默认从最近 500 条日志聚合。

## 分析口径

`InterviewRetrievalAnalytics` 返回：

- `total_logs`
- `hit_logs`
- `fallback_logs`
- `hit_rate`
- `fallback_rate`
- `top_hit_atoms`
- `low_hit_atoms`
- `fallback_combinations`
- `recent_fallbacks`

首期低/未命中原子口径：

- 在当前 `published` 且 `followup|mixed` 原子集合中，最近分析窗口内命中次数为 0 或较低的原子。
- 不自动判定质量，只作为运营线索。

## 兼容性

- `interview_retrieval_logs` 表已存在；本任务只补必要索引。
- 若历史库没有索引，不影响功能，只影响查询性能；schema 与 migration 同步补齐。
- MemoryStore 测试和 PostgresStore 代码保持同一语义。

## 回滚点

- 运行时日志写入可通过移除调用快速回滚，不影响面试主流程。
- 管理端 API 和前端面板是新增能力，可单独隐藏或回滚。
