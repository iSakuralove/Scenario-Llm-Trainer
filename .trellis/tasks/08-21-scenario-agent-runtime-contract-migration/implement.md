# Implement：排查工坊单 Agent + 工具调用 Runtime 重构

执行方式：主会话直接实现（不派 implement/check 子代理）。进入实现前必须加载 `trellis-before-dev`；完成后必须加载 `trellis-check`。

阶段划分原则：**每个阶段独立 commit、独立可回滚、结束时全仓测试绿**。Python → Go → 前端顺序（契约生产方先行，消费方后接），schema_version 双解析并存过渡。

---

## Phase A：Python 契约与新 Runtime（生产方）

### A1 新契约层（contracts/）
- 文件：`agent/src/hiddenworld/contracts/agent_context.py`（新）、`events.py`（重写）、`answer.py`（增独立 CanonicalAnswer 与无 terminal 的 AgentComparison）、`world.py`（simulated_output 注记 Runtime-only）、`transport.py`（TurnResult v2 形状）、`model_output.py`（新 AgentModelOutput）、`debug_trace.py`（新独立 DebugTraceEvent）
- 内容：AgentContext / TeachingNavigation / AgentTurnControlView / ActionCatalogEntry / ToolCallState / ToolResult / RunEvent 判别联合 / PublicContent / CanonicalAnswer / AgentComparison 白名单
- 测试：`agent/tests/test_contracts.py` 扩展——字段白名单测试（AgentContext 不含 ScenarioContract，仅暴露 turn_control.terminal；AgentComparison 不含 terminal）；AgentModelOutput 两分支与 public_summary 规则；ToolCallState/ToolResult 分层；正式 RunEvent 与独立 DebugTraceEvent parse/reject 测试
- ✅ 验收：`cd agent && python -m pytest tests/test_contracts.py -q`

### A2 工具层（agents/tools.py 重写）
- 文件：`agent/src/hiddenworld/agents/tools.py`（重写）、新增 `agent/src/hiddenworld/runtime/tool_executor.py`
- 内容：语义工具集（logs/metrics/config/database/dependency 查询工具，虚拟数据确定性执行，复用 HiddenWorldEngine.observe）；compare_answer 零参数化 + Runtime 绑定 AnswerAttempt；保留 `_resolve_declared_virtual_tool` 确定性映射作为执行入口校验
- 测试：`agent/tests/test_tools.py` 重写——无参数 compare_answer；no_answer_attempt 错误码；未声明查询 unsupported；幂等
- ✅ 验收：`cd agent && python -m pytest tests/test_tools.py -q`

### A3 AgentLoop 与调度器
- 文件：新增 `agent/src/hiddenworld/runtime/agent_loop.py`、`runtime/batch_scheduler.py`、`runtime/state_reducer.py`、`runtime/event_publisher.py`；改 `agents/scenario_agent.py`（新单 Agent）；`app.py`（入口接线）
- 内容：≤11 轮 / ≤10 次逻辑调用预算；所有业务工具共用预算，规范化重复调用合并，批内独立查询分别计数；用户超预算请求进入 pending 但不自动执行，Agent 自主超额调用直接拒绝，必要答案核验预留预算；批次依赖拆分（无依赖只读并行、依赖跨轮）；失败/超时/重复/预算耗尽确定性处理；StateReducer 编组现有 kernel（cluegate/evidence/verifier/antiguess/policy 不重写算法）；AgentModelOutput 的 public_summary 投影；正式 RunEvent 与独立 DebugTraceEvent 分流。
- 测试：`agent/tests/test_runtime.py` 重写——依赖批次调度（并行/串行/违规拆批）；预算耗尽收束；正式模式无 DebugTraceEvent；有 tool_calls 必有摘要、纯 final_reply 默认无摘要；摘要后必有后续实质事件；理解摘要流式保留
- ✅ 验收：`cd agent && python -m pytest -q`

### A4 Python 旧链路删除（独立 commit，可单独 revert）
- 文件：删 `runtime.py` 旧主链、`agents/interpreter.py`、`agents/mentor.py`、旧 Deps；CONTRACT_VERSION → hiddenworld.v2
- 风险点：Go golden 测试此刻必红 → **A4 与 B1 同一部署窗口内连续执行**，A4 单独 commit 但允许中间态 CI 红（见风险 §R1）
- ✅ 验收：`cd agent && python -m pytest -q && python -m mypy src 2>/dev/null || true`（类型检查按项目现状）

---

## Phase B：Go 迁移（复核方）

### B1 agentclient 契约 v2
- 文件：`backend/internal/agentclient/types.go`、`client.go`、`testdata/turn_result_golden.json`（由 Python 真实主链重生成）
- 内容：TurnResult v2（AgentModelOutput、ToolCallState/ToolResult 分层、新事件、schema_version/state_revision）；contract_version 拒绝 v1；终值 DisallowUnknownFields 保持
- 测试：golden 严格解码；未知字段拒绝
- ✅ 验收：`cd backend && go test ./internal/agentclient/`

### B2 scenario_agent.go 校验矩阵重写
- 文件：`backend/internal/httpapi/scenario_agent.go`（phaseByKind/toolStage/guard_passed 强制删除 → 判别联合矩阵）
- 内容：新事件校验（payload 错型拒绝、每个事件必有 state_revision、sequence 递增且重放稳定、task_upserted 承载 ToolCallState、tool_result 只承载执行终态、turn_completed 前最后实质事件非 understanding 摘要、compare_answer 仅答案尝试轮且无参数）；"排查导师"文案替换（含 handlers_scenarios.go 5 处）；v1 存量 trace 通过 LegacyEventAdapter 进入统一展示 ViewModel
- 测试：`scenario_agent_integration_test.go` 重写；`scenario_validation_mode_test.go` 迁移
- ✅ 验收：`cd backend && go test ./internal/httpapi/`

### B3 SSE / 持久化 / AllowedAction
- 文件：`backend/internal/httpapi/handlers_scenarios.go`、`sse.go`（如需）、`backend/internal/domain/scenario_agent.go`（ScenarioRunEvent v2）、新增 AllowedAction 生成与 StructuredUserAction handler
- 内容：schema_version 与 state_revision 写入每个正式 SSE 事件；正式 sequence 一旦确定即持久化，断线重放不得重编号；QuickAction 与自然语言共用预算/幂等/state_revision；ActionCatalog 采用题目动态实例 + 固定 ToolKind + Runtime 白名单
- 测试：SSE 端到端（httptest + event-stream）；QuickAction 计预算测试
- ✅ 验收：`cd backend && go test ./...`

### B4 agent/runtime.go 收敛（仅隔离）
- 文件：`backend/internal/agent/runtime.go`（注记 + 引用排查）
- 内容：确认 scenario 链路不再引用后，注记"非 scenario 专用"；类型化收敛另开后续任务（不改面试舱与 Community Review）
- ✅ 验收：`cd backend && go test ./...`

---

## Phase C：前端迁移（消费方）

### C1 类型与解析
- 文件：`frontend/src/types/agentRun.ts`（重写判别联合 + PublicContent + AgentModelOutput + ToolCallState/ToolResult + 双版本解析器）、`types/index.ts`（hidden_world 移除 + 引用收敛）、`api/client.ts`、新增 `LegacyEventAdapter.ts`
- 内容：v1 Event → LegacyEventAdapter → UnifiedViewModel；v2 直接进入同一 ViewModel；不把旧事件伪装成新 Runtime 事实
- ✅ 验收：`cd frontend && pnpm type-check`

### C2 AgentRun 渲染
- 文件：`frontend/src/features/scenarios/agentrun/AgentRun.tsx`、`ThinkingState.tsx`、`ThinkingReasoning.tsx`、`AgentRun.module.css`、新增 `TaskList.tsx`
- 内容：aria-label 去"排查导师"；Thinking State 只表示未完成，不推导固定步骤；模型 `public_summary` 只通过 `assistant_delta(phase=understanding)` 投影，DebugTraceEvent 走独立通道；Task List 内嵌 Agent 流，处理中展开、完成后保留摘要并折叠详情；PublicContent 分发渲染；工具图标按 Runtime `tool_kind` 固定映射
- 测试：UI 测试——不含"排查导师/Mentor/Agent"文字；事件驱动状态；TaskList/QuickActions
- ✅ 验收：`cd frontend && pnpm test`

### C3 QuickActions
- 文件：新增 `frontend/src/features/scenarios/agentrun/QuickActions.tsx`、`api/client.ts`（StructuredUserAction 提交）
- ✅ 验收：`cd frontend && pnpm lint && pnpm type-check && pnpm test`

### C4 前端 v1 解析分支删除（确认点后，独立 commit）
- 前提：用户验收 SSE 全链路、LegacyEventAdapter 稳定运行一个迭代周期；删除的是旧协议解析分支，不删除统一 AgentRun UI
- ✅ 验收：`cd frontend && pnpm lint && pnpm type-check && pnpm test`

---

## Phase D：跨层验收与收尾

### D1 端到端验证
- SSE / RunEvent 契约端到端（起 Python + Go + 前端冒烟）
- 跨层验收矩阵（下表）逐项过

### D2 spec 更新（trellis-update-spec）
- `.trellis/spec/backend/scenario-agent-contracts.md` 全篇重写为新契约（Scenario 1-6 保留仍有效的边界规则，如 Guard 数字禁词同步、流式字段首位、序号分离；删除已失效的 guard_passed 强制等）

### D3 提交
- 按 trellis Phase 3.4 分批 commit

---

## 测试与验收命令汇总

```bash
# Python
cd agent && python -m pytest -q

# Go
cd backend && go test ./...
cd backend && go vet ./...

# 前端
cd frontend && pnpm lint
cd frontend && pnpm type-check
cd frontend && pnpm test

# golden 重生成（A4/B1 时，由 Python 真实主链产出）
# 见 backend/internal/agentclient/testdata/ 生成脚本/说明

# 端到端冒烟（D1）
# docker-compose up litellm + agent + backend；前端 dev；走一轮完整对话 + 答案尝试轮 + QuickAction
```

## 跨层验收矩阵

| # | 验收项 | Python | Go | 前端 | 端到端 |
|---|---|---|---|---|---|
| 1 | AgentContext 无 ScenarioContract | test_contracts 字段白名单 + prompt 渲染断言 | — | — | 抓包/日志抽查 |
| 2 | RunEvent 判别联合契约 | parse/reject 测试 | 校验矩阵测试 + golden；每个事件必有 state_revision；ToolCallState/ToolResult 分层 | 类型测试 | SSE 抓包对比 |
| 3 | sequence 严格递增/去重/恢复 | event_publisher 测试 | 校验测试；重放不重编号 | dedupeEvents 测试（现状保留） | 断线重连冒烟 |
| 4 | state_revision 并发控制 | 提议测试 | 审批+409 测试；事件外层必带 revision | — | 双开冲突冒烟 |
| 5 | tool_calls 依赖批次 | batch_scheduler 测试 | — | — | 并行查询轮事件序 |
| 6 | 10 次逻辑调用预算 | agent_loop 测试 | 复核测试 | — | 连续提问冒烟 |
| 7 | compare_answer 无参数+绑定 | test_tools | 无参数校验测试 | — | 答案尝试轮冒烟 |
| 8 | DebugTraceEvent 非生产边界 | 独立协议与生产禁用测试 | 正式 RunEvent 未知 debug kind 拒绝；独立调试通道测试 | 调试面板仅消费独立通道 | 生产配置抽查 |
| 9 | 前端无"排查导师"文字 | — | 文案替换 | UI 测试 | 页面走查 |
| 10 | thinking 事件驱动非固定 | 摘要后必有后续事件测试 | 完整性校验测试 | thinkingLabel 测试 | 走查 |
| 11 | PublicContent 白名单 | 投影测试 | validateScenarioReply 等价校验 | 只渲染 markdown_ready | 走查 |
| 12 | QuickAction 共用预算/幂等 | — | handler 测试 | 交互测试 | 点击冒烟 |
| 13 | schema_version / LegacyEventAdapter 路由 | CONTRACT_VERSION v2 | 拒 v1 新写入 + 存量适配 | 双解析器汇聚同一 ViewModel | 旧会话打开冒烟 |

## 风险点

- **R1 A4/B1 中间态**：Python 删旧链路后、Go 契约升级前，跨服务 golden 与集成测试红。缓解：A4+B1 在同一工作窗口连续完成，中间不部署；或先 B1（Go 同时接受 v1/v2 事件）再 A4——**采用后者**：B1 实现为"v2 优先、v1 兼容读"，A4 删除时 Go 已就绪。
- **R2 题库 JSON 含 simulated_output**：代码不读该字段即可，题库数据不动；生成器侧加 deprecation 注记。
- **R3 现有意图/摘要能力回归**：A3 必须先写"摘要能力保留"的对照测试（复用 test_runtime 现有断言改写），再动主链。
- **R4 DisallowUnknownFields 联动**（spec Common Mistake）：Python 每次契约增删字段必须同步 types.go 并重生成 golden——A/B 阶段任何契约改动重复该检查。
- **R5 前端断线恢复语义**：dedupeEvents 按 request_id+sequence；正式 sequence 持久化后不可因重连重编号，LegacyEventAdapter 对旧 trace 使用稳定序号来源。
- **R6 范围膨胀**：QuickActions/TaskList 是全新 UI 能力，若 C2/C3 超期可拆后续任务（prd F3.4 独立成段的原因）；agent/runtime.go 收敛明确只隔离不重写。

## 回滚点

| 阶段 | 回滚方式 |
|---|---|
| A1-A3 | 纯新增，revert commit 即净删除 |
| A4 | 独立 commit，revert 恢复旧主链（Go 仍兼容 v1，服务可用） |
| B1-B3 | 各自独立 commit；B1 兼容读设计使 revert B2/B3 不影响 A4 |
| B4/C4 | 独立 commit，单独 revert |
| C1-C3 | 前端独立部署，revert 即回旧 UI（Go SSE 双版本输出窗口期） |
| 数据库 | 无破坏性变更；新增列（如 schema_version 落库需要）均 ADD COLUMN IF NOT EXISTS，回滚代码即可 |

## 进入实现前检查

- [ ] 用户确认 prd/design/implement 三份文档
- [x] design §11 五项产品决策已冻结：摘要条件、Task List 阈值、动态 ActionCatalog、LegacyEventAdapter、agent/runtime.go 隔离
- [ ] `python ./.trellis/scripts/task.py start .trellis/tasks/08-21-scenario-agent-runtime-contract-migration`
- [ ] 加载 `trellis-before-dev`
