# 面试题库真实命中运营看板

## 目标

让管理员看到真实面试运行中的题库命中、规则回退和低效资源，补齐题库运营闭环。

## 用户价值

- 管理员能判断正式题库在真实面试中是否有效命中，而不是只看模拟检索。
- 管理员能定位高回退组合，优先补题、修题或重建索引。
- 管理员能识别热门命中原子和长期低命中原子，辅助后续运营决策。

## 已确认事实

- 管理端已有导入、编辑、归档、恢复、索引重建、健康诊断和模拟检索预览。
- `interview_retrieval_logs` 表已存在于 `SchemaSQL` 和 `backend/migrations/001_schema.sql`。
- `domain.InterviewRetrievalLog` 已存在，但 Store 接口、MemoryStore、PostgresStore 和运行时写入链路尚未补齐。
- 运行时追问检索已能得到 `MatchedAtoms`、`FallbackUsed`、错误/回退摘要和 query 文本。
- 管理端页面已有健康诊断和检索预览区域，可新增真实检索运营面板而不替换现有能力。

## 需求

1. 后端补齐真实检索日志：
   - 每次触发追问检索后写入 `InterviewRetrievalLog`。
   - 命中时记录 `matched_atoms`。
   - 回退或错误时记录 `fallback_used=true` 和可展示原因。
   - `query_text` 必须脱敏并截断到 500 字以内。
   - 不记录用户身份、完整回答、完整简历/项目背景原文。

2. Store 能力：
   - 新增保存检索日志方法。
   - 新增按条件读取最近检索日志方法。
   - 新增聚合运营摘要方法或等价内部聚合实现。
   - MemoryStore 与 PostgresStore 行为一致。

3. 管理员 API：
   - `GET /api/v1/admin/interview-bank/retrieval-logs`
     支持 `domain`、`category`、`difficulty`、`fallback_used`、`limit` 过滤。
   - `GET /api/v1/admin/interview-bank/retrieval-analytics`
     返回真实检索次数、命中率、回退率、热门命中原子、低/未命中原子、回退组合排行和最近回退原因。
   - 所有接口 admin-only，非管理员返回权限错误。

4. 前端管理页：
   - 在面试题库管理页增加“真实检索运营”面板。
   - 展示真实检索次数、命中率、回退率、最近回退原因。
   - 展示热门命中原子和低/未命中原子，可打开题目详情。
   - 展示回退组合排行，可套用到现有筛选和检索预览表单。
   - 保留健康诊断和模拟检索预览，不替换。

5. 文档与规范：
   - 更新 `docs/architecture.md`。
   - 新增 `features/interview-bank-retrieval-analytics.md`。
   - 更新 `.trellis/spec/backend/store-schema-contracts.md` 或新增相关 contract，明确检索日志隐私和接口契约。

## 验收标准

- 真实面试追问检索命中时，会生成日志并能在 admin API 中看到 matched atoms。
- 真实面试追问回退时，会生成 fallback 日志并能统计到回退率。
- `query_text` 被脱敏且长度不超过 500 字。
- 管理员可读取日志和分析摘要；非管理员不可访问。
- 聚合摘要能展示热门命中原子、低/未命中原子、回退组合排行。
- 前端管理页能展示运营面板并从回退组合套用筛选/检索预览。
- 不改变用户侧报告接口，不新增 LLM 调用，不自动改题或重建索引。

## 不做

- 不做自动归因、自动修题、自动归档或自动索引重建。
- 不展示用户身份、完整回答、完整简历或项目背景。
- 不改变普通用户面试报告接口。
- 不新增复杂异步任务队列。

## 风险与约束

- 检索日志属于运营数据，必须 admin-only。
- query 文本有隐私风险，必须脱敏截断。
- 运行时写日志不能影响面试流程；写入失败只应降级，不中断追问。
- 统计查询要限制默认范围和 limit，避免管理页大范围扫描造成 CPU/DB 压力。

## 开放问题

无阻塞问题。按“轻量日志 + 脱敏截断 + admin-only 看板”的边界实现。
