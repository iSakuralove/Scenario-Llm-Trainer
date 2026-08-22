# 支付回调故障排查教学 Agent：Harness 加固长期实施路线图

> 版本：2026-08-22
>
> 目的：把 `验收标准/支付回调故障排查教学Agent_对话原文整理.md` 中的学生体验转化为可执行的状态、动作、事件和安全验收契约。附件中的请求 ID、固定数字和示例句子只作为行为样例，不作为全局模板。

## 当前执行基线

本路线图在分支 `codex/codex-harness-adoption` 上执行。当前工作区已有一批来自前序 Scenario Agent/V2 迁移的未提交修改，执行原则是保留并收束，不覆盖式清理。

已确认的基线事实：

- Python 定向测试在 `agent/.venv` 中运行；当前前序迁移基线为 204 passed、8 deselected、2 failed。
- 失败项是 `MentorDeps` 字段白名单未同步，以及旧测试仍期待卡住时自动释放证据；新行为以 Hint 阶梯和 `hint_level` 为准。
- Go 测试必须从 `backend/` 目录运行；当前 `internal/httpapi`、`internal/agentclient`、`internal/store` 定向测试已通过。
- 前端基线中的 `client.ts` 类型收窄和 `AgentRun.tsx` 未使用局部变量问题已在 Phase 0 收束。
- 正式流式事件当前可能在 Go 提案审批、回复 Guard 和 CAS 提交前外发；该问题列为最高优先级安全风险。

## 不变的架构边界

1. 继续使用单一 ScenarioAgent；不引入第二套可写案件状态。
2. `LearnerState`、`GuidanceState`、`TurnControl` 由 `StateReducer` 确定性归约，Go 继续独立复核 Python 结果。
3. `HiddenWorld`、`CanonicalAnswer`、证据 ID、假设 ID、完整答案和 raw reasoning 不进入学生侧。
4. `TeachingPolicy` 只编译安全约束；回复正文前增加 `ResponseBriefBuilder`，但它不生成自然语言。
5. `hiddenworld.v2` 保持兼容，新增字段优先可选；V1 只读兼容，不再新增 V1 写入。
6. 每条学生消息是一个教学 Turn，Turn 内允许受控的多个工具子循环，默认最多 5 个工具调用并尽早停止。
7. 正式回复、观察、线索和提示在 Go 提案审批、Guard、CAS 和持久化成功前不发送；提交前最多发送不入历史的结构化进度。

## 阶段状态与门槛

### Phase 0：基线和行为验收矩阵（已完成，2026-08-22）

交付物：

- 当前未提交改动分类和跨层契约漂移清单。
- `ResponseBriefBuilder`、`InvestigationView` 的最小安全类型。
- 三类学生的单轮行为矩阵，以及 12/18 Turn 轨迹的结构化断言。
- Python、Go、前端基线测试记录。

验收重点：不再以整段固定回复文本作为唯一金标；每轮至少检查主教学任务、工具动作、公开观察、线索/提示变化、状态变化和禁止泄露项。当前已建立 `test_response_brief.py`、`test_scenario_behavior.py` 和 prompt 投影断言。

基线证据：Python 全量 `246 passed, 8 skipped`；Go 从 `backend/` 运行 `go test ./...` 通过；前端 `npm run build` 通过。

### Phase 1：自适应教学回复简报（已完成最小内核，2026-08-22）

实现要求：

- 缺失概念才解释；已掌握概念只引用含义，不重复开课。
- 工具卡展示事实，导师正文解释事实能证明什么、还不能证明什么。
- 同一轮只选择一个主任务：解释概念、解释证据、确认进展、纠偏、提示、拉回主线或收束。
- 明确区分直接触发因素、潜在性能问题和完整因果链。
- Hint 只表示帮助等级，不计入学生独立证据；学生有新证据或正确因果后 Hint 可回落。

门槛：三类学生在同一题目上拥有不同解释长度和工具节奏；不出现内部 Guard、Proposal、state revision 或 raw reasoning 文案。`ResponseBriefBuilder` 已接入 ScenarioAgent 安全 prompt，正文生成仍由 Agent 自然语言输出负责。

### Phase 2：统一动作准入与受控多工具循环（已完成最小运行时收束，2026-08-22）

实现要求：

- QuickAction 与普通消息使用同一服务端授权入口。
- Runtime 维护动作指纹，拒绝重复动作、未授权动作和超预算动作。
- 单轮默认上限为 5；工具结果逐个回灌，单个失败不丢弃同批其他结果。
- 低收益方向允许一次低成本验证，随后把调查焦点拉回主线。

门槛：Callback trace → DB trace 可以在一个 Turn 内完成；证据足够后自动停止；不存在无限工具调用。当前默认工具上限为 5，单项执行异常会收束为失败终态，QuickAction 与普通动作共用 `BatchScheduler.authorize_action`。

### Phase 3：类型化运行帧与正式事件投影（已完成 Python 最小帧类型，Go 投影持续收束）

实现要求：

- Python 内部产出类型化运行帧，不生成正式公开序号。
- Go 是正式 V2 事件序号唯一来源；落库事件和实时事件使用同一投影。
- `run_progress` 只展示处理阶段，不携带观察正文、线索、提示或回复，也不进入历史。
- 前端区分临时进度、正式工具结果、线索、提示、回复和失败状态。

门槛：正式事件序号连续、无重复；raw reasoning 永不进入正式事件；工具卡、线索板和导师正文不重复表达同一事实。Python 已增加 `ScenarioRunFrame` 内部类型，正式公开序号仍只由 Go 生成。

### Phase 4：回复提交屏障与安全流式（已完成 Go 最小屏障，2026-08-22）

实现要求：

- Python 缓冲最终回复候选和公开 trace。
- Go 在提案审批、回复 Guard、CAS 和数据库提交成功前只保留内存 pending 数据。
- 提交成功后以持久化 `ResponseMeta.RunEvents` 为唯一正式发送源，顺序固定为正式 Turn 事件、工具/线索/提示、回复分片、`turn_completed`。
- 任何失败都清空 pending 数据，只发送结构化失败状态；不得依赖前端隐藏已经发出的正文来补救。

当前实现：Go SSE 流式回调只写入内存缓存，`CommitScenarioAgentTurn` 成功后从持久化 `ResponseMeta.RunEvents` 回放；`go test ./internal/httpapi` 与全量 Go 测试通过。

关键指标：

```text
public_body_before_commit = 0
clue_before_commit = 0
hint_before_commit = 0
replay_reply_mismatch = 0
```

### Phase 5：长会话、上下文隔离和重放

实现要求：

- 同一 `request_id` + 同一指纹只返回同一结果；不同内容返回冲突。
- 已提交请求重放不重新调用模型或工具。
- 模型每轮只接收当前消息、最近 2～4 个完整回合、结构化 LearnerStateView、InvestigationView、公开线索/提示/观察和安全简报。
- 12/18 Turn 轨迹中，解释递减、调查主线不丢、触发因素和潜在问题不混淆。

当前收束进度（2026-08-22）：

- `project_agent_context` 已固定只向模型投影最近 4 个完整回合（最多 8 条 user/mentor 消息），当前学生消息独立保留。
- GuidanceState、Hint 等级和上一轮 terminal 切片会在重放/重新投影时保持稳定；completion 判定、答案比较和内部 ID 不进入模型上下文。
- 新增 `agent/tests/test_phase5_long_session.py`，覆盖 12/18 轮解释递减、Hint 1→4 逐级提升、进展后回落、上下文深拷贝隔离和 18 轮 request/state_revision 递增。
- 定向验证：Phase 5/支付回调行为/评测矩阵共 `20 passed`；Python 全量当前 `222 passed, 8 deselected`。

### Phase 6：效果回归和旧路径退役

建立行为快照而非全文快照，覆盖：基础学生、工程学生、知识碎片化学生、未提供数据、跑偏、卡住、过早结论、止血/治本区分、修复验证、重复请求、断线重连、CAS 冲突、未授权工具、伪造结果和 raw reasoning 旁路。

当前收束进度（2026-08-22）：

- 前端不再把 `debug_trace/reasoning_raw_delta` 传入学生侧 `AgentRun`；学生只看到安全的理解摘要、通用处理中状态、公开工具结果和导师正文。
- 新增前端 E2E 回归：即使调试旁路携带内部 action/authorization 文本，学生消息区也不得渲染原始思维内容。
- 真实浏览器已验证：普通消息在 150ms 采样时只有处理中状态，模型阶段结束后才出现工具卡；QuickAction 同样先显示处理中，再出现查询结果和导师解释。
- V2 多工具回合恢复任务摘要卡；学生侧 QuickAction 只展示少量当前候选，不把完整工具目录伪装成推荐清单。
- Go 固定题资产已通过 `go generate ./internal/store` 与 Agent 源题库同步，避免题目观察/工具目录跨层漂移。

### Phase 7：离线 AIJob（后置）

只有 Phase 0～6 全部通过后，才引入题库生成、导入和批量评测租约。离线调度不得进入学生实时 Turn、QuickAction 或普通排查对话。

## 行为验收矩阵

| 场景 | 必须观察到的行为 | 不允许出现 |
|---|---|---|
| 基础薄弱 | 先补当前所需概念，再回到网关变更前后 | 一次性完整网络课程、直接公布答案 |
| 有工程经验 | 直接查询配置、Nginx、Gateway、Callback、DB | 重复解释已掌握概念 |
| 知识碎片化 | 只解释 JSON 中缺失的 Gateway/504/DB lock 等概念 | 重新解释已掌握 Callback/P99 |
| 说中 timeout 变化 | 明确缩小网关等待边界，继续要求证明请求持续时间 | 直接宣告完整根因 |
| 误判数据库 | 区分 DB lock 潜在问题与 Gateway timeout 直接触发 | 把单一组件说成完整答案 |
| 随机查资源 | 允许一次低成本检查后说明收益低并拉回主线 | 无限枚举、标记学生为噪声 |
| 连续卡住 | Hint 0→4 逐级释放，进展后回落 | 跳级、把 Hint 当学生证据 |
| 修复后 | 区分表面成功、P99/lock wait 是否真正改善、幂等与重试闭环 | 只说“改回 10 秒就好了” |

## 证据与文案边界

- 观察是题目世界返回并经 Runtime/Go 复核的事实。
- 假设是待验证解释，不能写成确认事实。
- 线索是学生发现或证据充分支持的公开事实，进入线索板但不等于答案。
- Hint 是系统帮助，不计入学生独立证据。
- 最终结论必须同时覆盖直接触发、潜在问题、完整因果链和修复验证。
- 题目没有的数据返回明确不可用；允许模拟时统一标记 `【教学模拟日志】`，不得伪装生产连接。

## 每阶段执行清单

1. 先运行 CodeGraph/符号影响检查，确认 Python、Go、前端调用链。
2. 使用 `apply_patch` 做最小逻辑修改，不覆盖用户已有未提交改动。
3. 运行受影响 Python、Go、前端测试；跨层字段同步更新 Go 严格解码和 golden fixture。
4. Phase 3、5、6 执行真实浏览器关键路径，记录事件顺序、失败回收和重连结果。
5. 检查 diff，排除密钥、临时文件、构建产物和无关改动。
6. 每阶段建立独立中文 Conventional Commit，编号保持 `01`、`02` 顺序。
7. 将测试命令、结果、未完成项和下一阶段入口回写本文件。

## 当前下一步

- 将普通消息与 QuickAction 的授权准入进一步收束为同一 `ScenarioActionGate`，并补充 QuickAction 伪造动作测试。
- 将 Python 内部事件收束为 `ScenarioRunFrame`，统一 Go 正式事件投影和临时 `run_progress`。
- 完成真实浏览器的提交失败、CAS 冲突、断线重连和长会话关键路径验收。
- 继续运行 12/18 Turn 行为轨迹和最终答案闭环评测；当前 Python、Go、前端 build/lint 与场景流式 E2E 的相关门槛已通过，再评估 Phase 5/6 的整体完成门槛。
- 保持 raw reasoning 仅在测试调试旁路存在，禁止重新接回学生侧页面。
