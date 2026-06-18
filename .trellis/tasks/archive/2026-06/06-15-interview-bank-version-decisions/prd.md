# 记录面试题库版本表决策

## 目标

把用户已确认的面试题库版本表、审计、归档恢复和管理端治理边界同步到现有 AI-Interview 接入文档中，保持 PRD、技术方案和 features 记录一致。

## 已确认决策

- 版本历史单独建表，建议为 `interview_knowledge_atom_versions`。
- 审计信息首期与版本历史共表，不单独建审计表。
- 题目主表只保存当前生效版本，版本表追历史。
- 题目主表增加 `current_version`。
- 在线编辑提交必须携带 `base_version`，不带或不匹配则拒绝保存。
- 版本 diff 摘要使用 JSONB。
- 归档、恢复、编辑复用同一套版本写入函数。
- 批次导入生成的版本也写入同一版本表。
- `current_version` 从 `1` 开始，且每次版本事件单调递增。
- `duplicate_import` 即使内容不变，也推进 `current_version`。
- 版本表 `snapshot` 保存完整标准化题目内容。
- `diff_summary` 只用于摘要展示，不承担恢复能力。
- 版本表增加 `(atom_id, version DESC)` 索引。
- 版本表增加 `version_type / admin_id / created_at` 相关索引。
- 主表 `current_version` 与版本表最新版本不一致时，视为数据异常。
- 首期不做“回滚到历史版本”能力。
- `snapshot` 保存完整标准化字段：`id / title / subject / domain / difficulty / category / question_role / sourceRef / tags / principles / pitfalls / followUpPaths / status`。
- `snapshot` 不保存 `vector_status / last_indexed_at` 这类运行时索引字段。
- `base_version` 冲突提示保持轻量：“版本已更新，请刷新后重试”。
- 不做强制覆盖别人刚改内容的按钮。
- 版本详情页显示 `no_content_change` 标记。
- 版本历史列表默认按 `created_at DESC` 排序。
- 版本历史列表展示版本号。
- 首期不做批量在线编辑多题。

## 修改范围

- `docs/ai-interview-integration-prd.md`
- `docs/ai-interview-integration-tech-design.md`
- `features/2026-06-11-ai-interview-integration-proposal.md`

## 验收标准

- 三份文档都记录上述 8 项决策。
- 版本类型与版本表设计口径一致。
- 在线编辑、批次导入、归档恢复的版本写入路径不互相矛盾。
- 版本号、快照、索引和异常口径在三份文档里一致。
- 首期不做批量在线编辑多题。
- 不引入业务代码修改。
