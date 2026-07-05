# 面试题库运营动作健康索引候选生成 PRD

## 目标

在已有“题库运营动作”手工队列基础上，补齐第二个 TDD 切片：管理员可以通过 admin API 从题库健康诊断和索引状态中生成运营动作候选预览。

本切片只做候选生成预览，不保存动作，不做真实检索日志候选，不做状态流转。

## 用户价值

- 管理员看到健康诊断中的阻断/警告组合后，不需要手工复制组合信息，就能得到可跟进的 `fill_gap` 或 `rebuild_index` 候选。
- 管理员看到已发布但索引失败的题目后，可以得到 `rebuild_index` 候选，后续再选择保存或触发治理入口。
- 候选生成是确定性规则，不调用 LLM、不写队列，适合在管理页反复预览。

## 已确认事实

- 上一切片已经完成：管理员可通过 `POST /api/v1/admin/interview-bank/ops-actions` 手工创建动作，并通过 `GET /api/v1/admin/interview-bank/ops-actions` 读回。
- 现有 PRD 的下一项实施顺序是“健康诊断与索引状态生成动作候选”。
- 现有健康诊断接口已经能按 `domain + category + difficulty` 返回 `open/warning/blocked` 组合、原因和题目数量。
- `InterviewKnowledgeAtom` 已有 `status`、`question_role`、`vector_status` 等字段，可识别已发布的 failed/pending/indexed 资源。
- Store 已有动作列表读取能力，可用于候选生成时排除已有 active 动作。

## 需求

1. 新增候选生成 API：
   - `POST /api/v1/admin/interview-bank/ops-actions/candidates`
   - admin-only，非管理员返回现有权限错误。
   - 返回候选列表、总数和生成策略摘要。

2. 候选来源仅限：
   - `health_diagnostic`
   - `index_status`

3. 健康诊断候选规则：
   - `blocked` 组合生成 `fill_gap` 候选，优先级 `P0`。
   - `warning` 组合中如存在 failed/pending 索引资源，生成 `rebuild_index` 候选，优先级 `P1` 或 `P2`。
   - 候选必须包含 `domain`、`category`、`difficulty`、`title`、`reason`、`dedupe_key` 和 compact evidence。

4. 索引状态候选规则：
   - `status=published` 且 `vector_status=failed` 的 atom 生成 `rebuild_index` 候选，优先级 `P1`。
   - `status=published` 且 `vector_status=pending` 的 atom 可生成 `rebuild_index` 候选，优先级 `P2`。
   - draft/archived atom 不生成普通索引候选。

5. 去重与边界：
   - 同一次生成中相同 `dedupe_key` 只返回一个候选，并合并 evidence 计数/原因摘要。
   - 已存在 active 动作（`open`、`in_progress`、`watching`、`reopened`）时，不重复返回同一 `dedupe_key` 候选；响应中可记录 skipped 数量。
   - 关闭态（`resolved`、`dismissed`）不阻止未来重新生成同 key 候选。

6. 安全与副作用：
   - 候选生成不得写入 `interview_bank_ops_actions`。
   - 不修改 atom、版本、索引状态或检索日志。
   - 不调用 LLM、embedding 或向量检索。
   - evidence 不包含完整题目正文、用户回答、简历、项目背景、密钥或 provider payload。

7. 默认限制：
   - 候选数量默认上限 50，最大 200。
   - 允许请求按 `domain`、`category`、`difficulty`、`sources` 和 `limit` 限制生成范围。

## 验收标准

- 管理员调用候选 API，健康诊断 blocked 组合会返回 `fill_gap/P0` 候选。
- 管理员调用候选 API，已发布 failed atom 会返回 `rebuild_index/P1` 候选。
- draft/archived atom 不会生成索引候选。
- 同一 `dedupe_key` 的重复信号只返回一个候选。
- 已存在 active 动作时，候选 API 不再返回同 key 候选。
- 候选 API 不持久化动作；调用后动作列表仍不新增。
- 非管理员不能访问候选 API。
- 响应 evidence 只包含轻量元数据和计数。

## 不做

- 不保存候选为动作。
- 不实现真实检索运营数据生成候选。
- 不做动作详情、状态流转、历史记录或重开。
- 不触发编辑、归档、恢复或索引重建。
- 不改普通用户报告接口。
- 不新增 LLM、embedding 或后台任务。

## 风险与约束

- 候选规则必须稳定、可测试，避免隐式业务魔法。
- 生成逻辑应做成较深模块，HTTP handler 只负责鉴权、解析请求和返回响应。
- 健康诊断和索引候选会跨 domain/store/httpapi，MemoryStore 与 PostgresStore 的动作读取语义必须保持一致。
- 若后续要前端保存候选，必须重新校验候选，不应盲信前端传回的 payload。

## 开放问题

无阻塞问题。按“只读候选预览 API + 确定性规则 + active 动作去重”的边界实现。
