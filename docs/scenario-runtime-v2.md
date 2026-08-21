# 排查工坊 Runtime V2

排查工坊（Scenario Workshop）的单轮主链跨三层：Python `hiddenworld` 主链产出类型化结果，Go `httpapi` 独立复核并持久化，前端只消费公开事件。核心不变式：**Go 不信任 Python 的任何自报判断，只用自己持有的权威状态复核。**

```
前端(React/SSE) ←── V2 RunEvent(判别联合) ── Go httpapi(复核+持久化+SSE)
                        ↑ HTTP /turn(严格解码)
                  Python hiddenworld Runtime(确定性执行)
                        ↑ AgentContext(授权投影)
                  ScenarioAgent(单 LLM 节点，看不到答案)
```

## V2 事件协议（hiddenworld.v2）

外层：`request_id` / `sequence` / `schema_version` / `state_revision` / `kind` / `payload`。三者职责分离：

- **sequence**：事件排序/去重/断线恢复，**Go 唯一生成**，幂等重放不重编号
- **state_revision**：业务状态版本，每个正式事件必带
- **schema_version**：协议版本路由（旧 v1 事件不带此字段，前端 LegacyEventAdapter 识别）

| kind | payload | 触发时机 |
|---|---|---|
| turn_started | turn_id | 每轮开始 |
| task_upserted | task（ToolCallState 生命周期） | 工具任务状态变化 |
| tool_result | tool_result（只含执行终态） | 工具执行结束 |
| clue_published | clue（PublicContent） | 主动释放线索 |
| assistant_delta | phase=understanding/replying + markdown_ready_delta | 安全摘要或正文增量 |
| turn_completed | next_actions（QuickActions） | 轮次收束 |
| turn_failed | error_code + retryable | 轮次失败 |

已删除：`guard_passed`/`mentor_buffered`/`response_summary`/`proposal_approved`/旧 `tool_started`/`tool_completed`——内部过程事件不再外发，Go 也不再强制依赖它们。

## 安全边界

1. **用户动作授权（UserActionAuthorization）**：Observation Tool 只能执行学生明确提出、或 QuickAction 点击授权的检查；Agent 自主提出的观察被 Runtime 拒绝，不产生新的 WorldObservation。授权来源只有 `user_message` 与 `structured_user_action`。
2. **无参数 compare_answer**：答案比较不接受模型构造的参数，Runtime 自动绑定当前轮 AnswerAttempt，Agent 无法探测标准答案。
3. **CanonicalAnswer 物理隔离**：与题目同版本持久化的唯一权威答案，只进 Runtime；Agent 上下文只有白名单投影。`ScenarioContractValidator` 在生成/加载时校验唯一性、证据引用、根因/评分一致性。
4. **PublicContent 白名单**：前端只渲染 observation/clue 的 `markdown_ready` 与回复的 `markdown_ready_delta`；hidden_world、内部根因、内部 ID、发布策略不出现在任何公开事件；观察结果开头的实现术语前缀（如"模拟"）在投影层剥离。
5. **界面零身份文字**：对话区不出现"排查导师/Mentor/Agent"，只保留头像。

## QuickAction 流程

```
turn_completed(next_actions ← 题目 virtual_tools 目录，已收集自动过滤)
  → 前端渲染 QuickActions 按钮
  → 点击 POST structured_user_action(action_id, catalog_version)
  → Go 白名单校验 + 服务端绑定 state_revision → Python
  → 确定性构造意图分析（不调 LLM）→ 授权观察 → 正文
  → 消息记录保留动作标题，历史回放完整
```

## 关键实现位置

| 层 | 文件 | 职责 |
|---|---|---|
| Python 契约 | `agent/src/hiddenworld/contracts/` | authorization / dimensions / model_output / debug_trace / validator |
| Python 主链 | `agent/src/hiddenworld/runtime.py` | 授权门控、QuickAction 确定性分析、compare_answer 绑定 |
| Go 复核 | `backend/internal/httpapi/scenario_agent.go` | 判别联合校验、共享投影、Go 独占序号 |
| Go 传输 | `backend/internal/agentclient/types.go` | structured_user_action、授权类型、严格解码 |
| 前端适配 | `frontend/src/features/scenarios/agentrun/LegacyEventAdapter.ts` | v1/v2 → 统一 ViewModel |
| 前端渲染 | 同目录 `AgentRun.tsx` / `TaskList.tsx` / `QuickActions.tsx` | 事件驱动 UI |

## 测试入口

```powershell
cd agent; .venv\Scripts\python.exe -m pytest -q          # 130 个用例
cd backend; go test ./...                                 # 10 个包
cd frontend; pnpm lint; npx tsc -b; npx playwright test   # 78 个 e2e
```

双形状共存期说明：数据库中旧 v1 trace 只读兼容（前端经 LegacyEventAdapter 展示），新写入一律 V2，不做历史回填。
