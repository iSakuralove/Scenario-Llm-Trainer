# 技术设计

## 架构边界

本切片在现有题库运营动作模型上增加候选保存入口，不新增表，不新增通用任务系统。

涉及边界：

- `domain`
  新增候选保存请求/响应类型。
- `httpapi`
  新增 `POST /api/v1/admin/interview-bank/ops-actions/candidates/save` handler 和候选到动作的转换校验。
- `store`
  复用 `CreateInterviewBankOpsAction` 与 `ListInterviewBankOpsActions`，不新增 Store 接口。
- `frontend`
  新增 API client/type，并在现有 `OpsActionPanel` 内加入候选生成、选择、保存。

不改 schema、不改真实检索日志、不改题库版本和索引重建。

## API 契约

新增：

```json
POST /api/v1/admin/interview-bank/ops-actions/candidates/save
{
  "candidates": [
    {
      "candidate_key": "health_diagnostic|fill_gap|combo|backend|cache|L3",
      "action_type": "fill_gap",
      "priority": "P0",
      "source": "health_diagnostic",
      "dedupe_key": "fill_gap|combo|backend|cache|L3",
      "title": "补齐 backend/cache/L3 题库资源",
      "reason": "组合 blocked",
      "domain": "backend",
      "category": "cache",
      "difficulty": "L3",
      "evidence": {}
    }
  ]
}
```

响应：

```json
{
  "list": [],
  "saved": 0,
  "total": 0,
  "skipped_existing": 0
}
```

## 后端转换规则

对每个候选：

- trim `dedupe_key/title/reason/source/action_type/atom_id/domain/category`。
- difficulty uppercase。
- priority uppercase。
- source 必须是 `health_diagnostic|index_status|retrieval_analytics`。
- status 固定为 `open`。
- created_by 固定为当前 admin ID。
- evidence nil 时保存 `{}`。
- 复用现有 Store 校验语义：title/reason/enum/target/dedupe key 都必须合法。

## 去重与错误策略

- `activeInterviewBankOpsActionKeys()` 作为保存前 active key 集合。
- active 命中或同请求重复 key：不创建，计入 `skipped_existing`。
- 非法请求整体返回 400；实现时先完成轻量预校验，避免部分写入。
- Store 写入失败返回 400；已写入项不回滚，本切片不引入事务封装。

## 前端交互

在 `OpsActionPanel` 内增加候选区：

- “生成候选”按钮调用 `generateInterviewBankOpsActionCandidates`，默认使用当前题库筛选里的 `domain/category/difficulty` 和全部 sources。
- 返回候选后默认全选。
- 候选项展示 checkbox、标题、类型/优先级/source/目标和 reason。
- “保存选中候选”按钮调用 `saveInterviewBankOpsActionCandidates`。
- 保存成功后刷新 open 队列，并显示 `saved/skipped_existing`。
- 没有候选时展示空状态，不阻断手工创建。

## 兼容与迁移

- 不新增 migration。
- 现有手工创建接口语义不变，仍固定 `source=manual`。
- 现有候选生成接口仍只读。
- 现有 open 队列读取接口不变。

## 回滚点

如候选保存出现问题，可隐藏前端保存按钮或移除新增 save 路由；已保存动作仍是普通 `interview_bank_ops_actions` 记录，可以继续通过 open 队列查看。
