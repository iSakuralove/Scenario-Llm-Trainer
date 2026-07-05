# 面试题库运营动作真实检索候选生成 PRD

## 目标

在已有健康诊断/索引状态候选预览基础上，补齐第三个 TDD 切片：管理员可以通过同一个候选生成 API，从真实面试检索运营数据生成题库运营动作候选。

本切片仍只做候选预览，不保存动作，不做候选选择保存，不改题、不归档、不重建索引。

## 用户价值

- 管理员看到真实回退组合后，可以直接得到补题候选，而不是人工复制运营看板信息。
- 管理员看到真实检索窗口内长期未命中的题库原子后，可以得到观察候选，后续再判断是否修题或归档。
- 真实检索候选复用现有动作候选 API，和健康诊断、索引状态候选共享去重、limit 和轻量 evidence 边界。

## 已确认事实

- 上一切片已经完成 `POST /api/v1/admin/interview-bank/ops-actions/candidates`，支持 `health_diagnostic` 与 `index_status`。
- `InterviewRetrievalAnalytics` 已存在，包含 `fallback_combinations`、`low_hit_atoms`、`recent_fallbacks`、`total_logs`、`fallback_rate` 等字段。
- 真实检索日志保存策略已完成脱敏和截断；候选 evidence 仍不应复制完整 `query_text`。
- 现有动作来源枚举已经包含 `retrieval_analytics`，但候选 API 当前未允许该来源。
- 当前任务顺序是“真实检索运营数据生成动作候选”。

## 需求

1. 扩展候选生成 API：
   - `POST /api/v1/admin/interview-bank/ops-actions/candidates`
   - `sources` 支持 `retrieval_analytics`。
   - 未传 `sources` 时，默认包含 `health_diagnostic`、`index_status`、`retrieval_analytics`。

2. 真实回退组合候选：
   - 从 `InterviewRetrievalAnalytics.FallbackCombinations` 生成 `fill_gap` 候选。
   - `count >= 3` 生成 `P0`；`count < 3` 生成 `P1`。
   - dedupe key 使用 `fill_gap|combo|domain|category|difficulty`。
   - reason 使用回退次数和最近安全原因摘要，不展示完整用户回答。

3. 低/未命中原子候选：
   - 从 `InterviewRetrievalAnalytics.LowHitAtoms` 生成候选。
   - 首轮仅对 `hit_count=0` 的 published followup/mixed 原子生成 `observe/P3` 候选，避免把低频但有效资源误判为归档候选。
   - dedupe key 使用 `observe|atom|atom_id`。
   - evidence 只包含 atom 轻量元数据、hit_count、analytics_window_total_logs 和 last_hit_at。

4. 过滤与限制：
   - 请求中的 `domain/category/difficulty/limit` 同时作用于 retrieval analytics 查询窗口和候选输出。
   - 继续复用 active 动作 `dedupe_key` 去重；active 状态跳过，resolved/dismissed 不阻止未来生成。
   - 同一次生成中同一 dedupe key 只返回一个候选。

5. 安全与副作用：
   - 候选生成不得写入 `interview_bank_ops_actions`。
   - 不修改 atom、版本、索引状态或检索日志。
   - 不调用 LLM、embedding 或向量检索。
   - evidence 不包含完整 query、完整回答、简历、项目背景、完整 atom 正文、密钥或 provider payload。

## 验收标准

- 管理员调用候选 API，真实回退组合会返回 `retrieval_analytics + fill_gap` 候选。
- 回退次数 `>=3` 的组合候选优先级为 `P0`，低于 3 次为 `P1`。
- 真实检索窗口内 `hit_count=0` 的 published followup/mixed atom 会返回 `observe/P3` 候选。
- 已命中的 atom 不生成 observe 候选。
- 候选 API 不持久化动作；调用后动作列表不新增。
- 已存在 active 动作时，同 key retrieval 候选被跳过。
- 非管理员不能访问候选 API。
- 响应 evidence 不包含完整 query 或 atom 正文。

## 不做

- 不保存候选为动作。
- 不实现候选选择保存。
- 不生成 `review_archive` 候选。
- 不实现动作详情、状态流转、历史记录或重开。
- 不触发编辑、归档、恢复或索引重建。
- 不改普通用户报告接口。
- 不新增 LLM、embedding 或后台任务。

## 风险与约束

- 低命中不等于低质量，首轮只生成 `observe`，不自动归档或修题。
- 回退组合 evidence 只能使用已脱敏运营摘要，不能把 retrieval log 的完整 query_text 复制进动作候选。
- retrieval analytics 查询必须受 limit 限制，避免管理页刷新造成无界扫描。
- 本切片应尽量复用上一切片候选生成入口，避免出现多套候选 API。

## 开放问题

无阻塞问题。按“真实检索运营候选预览 + conservative observe + 不落库”的边界实现。
