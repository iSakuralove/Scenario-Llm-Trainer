# Scenario Agent Contracts (Python ↔ Go ↔ Frontend)

排查工坊的单轮主链跨三层：Python `hiddenworld` 主链产出类型化结果，Go `httpapi`
独立复核并持久化，前端只消费公开事件。修改 `agent/src/hiddenworld/**`、
`backend/internal/httpapi/scenario_agent.go`、`backend/internal/agentclient/**` 或
`frontend/src/types/agentRun.ts` 时必须读本篇。

核心不变式：**Go 不信任 Python 的任何自报判断，只用自己持有的权威状态复核。**

---

## Scenario 1: Guard 数字禁词门槛必须两侧同步

### 1. Scope / Trigger
修改 `agent/src/hiddenworld/kernel/guard.py` 的 `_sensitive_tokens` 或
`backend/internal/httpapi/scenario_agent.go` 的 `extractScenarioSensitiveTokens`。

### 2. Signatures
```python
# agent/src/hiddenworld/kernel/guard.py
def _is_distinctive_number(token: str) -> bool
```
```go
// backend/internal/httpapi/scenario_agent.go
func scenarioIsDistinctiveNumber(token string) bool
```

### 3. Contracts
两个函数必须返回完全一致的判定：含 `.` 或 `,` → true；否则长度 ≥ 3 → true。

### 4. Validation & Error Matrix
| 条件 | 结果 |
|---|---|
| Go 比 Python 严 | Python Guard 放行的回复在 `validateScenarioReply` 被拒 → 整轮 `reply_guard_rejected`，学生看到失败 |
| Python 比 Go 严 | Mentor 被 `ModelRetry` 打回，`retries=1` 用尽后整轮失败 |
| 两侧一致 | 正常 |

### 5. Good/Base/Bad Cases
- Good：`3.8`、`2,400,000`、`v3.4`、`240` 仍是禁词
- Base：`idx_user_created`、`product:list:`、中文组件词不受此规则影响
- Bad：把裸的 `8` / `10` / `45` 列为禁词——实测固定题库 4 道题里 3 道命中，
  导师连「10 分钟」都写不出来（见 `.trellis/tasks/08-19-scenario-agent-real-process/research/`）

### 6. Tests Required
- `agent/tests/test_kernel_guard.py::test_forbidden_entities_skip_bare_small_integers`
  断言禁词表中不存在长度 ≤ 2 的纯数字，且 `240` / `3.8` / `idx_user_created` 仍在
- `backend/internal/httpapi/scenario_agent_guard_test.go::TestScenarioReplyGuardIgnoresBareSmallIntegersButKeepsPreciseValues`
  断言普通小数字放行、精确取值仍被拒
- 复现脚本：`agent/tools/audit_forbidden_entities.py`

### 7. Wrong vs Correct
#### Wrong
```python
tokens.extend(match.group(1) for match in _NUMBER.finditer(text))
```
#### Correct
```python
tokens.extend(
    match.group(1) for match in _NUMBER.finditer(text) if _is_distinctive_number(match.group(1))
)
```

---

## Scenario 2: `release_evidence_on_stall` 双侧审批

### 1. Scope / Trigger
新增或修改 `ProposalKind`，或修改 `approveScenarioProposals` 的任一分支。

### 2. Signatures
```python
# agent/src/hiddenworld/kernel/cluegate.py
def approve_on_stall(self, world: HiddenWorld, *, collected_evidence: Collection[str]) -> str
```
```go
// backend/internal/httpapi/scenario_agent.go
const scenarioStallUnlockThreshold = 2   // 必须 == runtime.py 的 STALL_UNLOCK_THRESHOLD
```

### 3. Contracts
`Proposal{kind: "release_evidence_on_stall", evidence_id: str}`。

Go 侧复核条件（全部成立才批准）：
- 本轮该 kind 至多 1 条
- `session.LearnerState.StalledTurns >= scenarioStallUnlockThreshold`
- `evidence_id` 存在于 HiddenWorld 且未被收集
- `len(node.Prerequisites) == 0`

**该分支不置 `progress`**：兜底释放是系统给的，不是学生挣来的，
因此 `stalled_turns` 继续累加、`effective_turns` 不推进。

### 4. Validation & Error Matrix
| 条件 | reject code |
|---|---|
| 本轮已有一条 stall 释放 | `stall_release_limit_exceeded` |
| `StalledTurns` 未达阈值 | `stall_threshold_not_met` |
| 证据不存在或已收集 | `invalid_evidence` |
| 节点有前置依赖 | `stall_release_requires_no_prerequisite` |

任一 reject 都导致整轮 `proposal_rejected`，不写任何业务状态。

### 5. Good/Base/Bad Cases
- Good：连卡 2 轮的学生说「我什么都不知道」→ 释放一条无前置的入口级证据
- Base：常规释放仍走 `release_evidence`，要求 `intersects(actions, node.ObtainedBy)`
- Bad：**用模型自报的 `is_stuck` 作为放行依据**。`is_stuck` 是 LLM 输出，
  可被诱导；`StalledTurns` 由 Go 持有且逐轮校验，只能用后者。

### 6. Tests Required
- `TestScenarioStallReleaseUnlocksEntryEvidenceForAStuckStudent`：断言 200、
  `CollectedEvidence` 增加 1 条、`StalledTurns == 3`、`EffectiveTurns == 0`
- `TestScenarioStallReleaseRejectedBeforeThresholdAndForGatedEvidence`：
  阈值不足与前置未满足两种子用例都断言 `proposal_rejected` 且不写证据
- `agent/tests/test_runtime.py::test_stuck_student_gets_a_stall_release_after_the_threshold`：
  断言发出的是 `release_evidence_on_stall` 而非 `release_evidence`

### 7. Wrong vs Correct
#### Wrong
```go
case "release_evidence":
    if result.TurnAnalysis.IsStuck { /* 放宽 evidence_not_requested */ }
```
#### Correct
```go
case "release_evidence_on_stall":
    if session.LearnerState.StalledTurns < scenarioStallUnlockThreshold {
        return reject("stall_threshold_not_met")
    }
```

---

## Scenario 3: 流式字段必须声明在 schema 第一位

### 1. Scope / Trigger
给 `TurnAnalysis` / `MentorAction` 增删字段，或调整字段顺序。

### 2. Contracts
`StreamingFieldExtractor(field)` 顺序扫描模型吐出的 JSON 文本。模型按 schema
声明顺序生成字段，因此**要流式外发的字段必须声明在最前**：

- `TurnAnalysis.public_summary` → 第 1 位（驱动 `reasoning_summary_delta`）
- `MentorAction.rationale` → 排在 `reply` 之前

`rationale` 在 `reply` 之后时模型会把它写成事后追认（实测退化为「用户需要信息」
这类空短语），因为正文已经生成完毕，该字段写什么都不影响输出。

### 3. Validation & Error Matrix
| 条件 | 结果 |
|---|---|
| 流式字段不在首位 | 增量要等前面所有字段生成完才开始，等同于不流式 |
| `rationale` 在 `reply` 之后 | 模型先说后想，`rationale` 退化为无信息量短语 |
| 新增字段无 `default` | 所有测试夹具必须同步补齐（`extra="forbid"` + 全 required） |

### 4. Tests Required
- `agent/tests/test_runtime.py::test_public_reasoning_summary_comes_from_the_model_not_a_constant`：
  两轮不同输入必须产生不同摘要，回归「每轮重复固定步骤」的观感问题

### 5. Wrong vs Correct
#### Wrong
```python
class MentorAction(BaseModel):
    reply: str = ...
    rationale: str = ...   # 事后追认，模型会敷衍
```
#### Correct
```python
class MentorAction(BaseModel):
    rationale: str = ...   # 先想
    reply: str = ...       # 后说
```

---

## Scenario 4: 实时序号与落库序号必须分开

### 1. Scope / Trigger
修改 `runtime.py` 的 `_emit_public_trace` / `_public_trace_before_mentor` /
`_public_trace_after_mentor`，或新增外发事件种类。

### 2. Contracts
- 落库 `AgentTurnResult.public_trace`：从 1 重新编号，**不含 `reasoning_summary_delta`**
- 实时通道：`_StreamSequencer` 独立单调编号，含增量

原因：Go `validateScenarioPublicTrace` 限制 `len(result.PublicTrace) > 64` 整轮拒绝，
一条摘要能拆出几十个增量；而 Go 的流式校验要求 `trace.Sequence` 严格递增，
共用落库序号会在增量之后回退。

### 3. Validation & Error Matrix
| 条件 | 结果 |
|---|---|
| 增量写入落库 trace | 超过 64 条 → `public trace exceeds event limit` → 整轮拒绝 |
| 实时序号复用落库序号 | 增量后回退 → `public trace sequence is not strictly increasing` |
| 少发 `guard_passed` | `public trace must contain exactly one guard_passed event` → 整轮拒绝 |

> **Warning**：`mentor_buffered` / `guard_passed` / `proposal_approved` 即使前端不再渲染，
> 也**不能从协议中删除**——Go 强制要求每轮恰好一条 `guard_passed`。
> 「不展示」和「不发送」是两件事。
>
> 前端已于任务 `08-20-scenario-chat-layout` 停止渲染这四类状态行
> （`AgentRun.tsx` 不再有 `publicStatusLabel`），后端契约保持不变。

### 4. Tests Required
- `agent/tests/test_runtime.py::test_runtime_streams_analysis_and_public_trace_before_returning_result`：
  断言 `reasoning_summary_delta` 不在落库 trace 中，且去掉增量后与落库序列逐项相等

---

## Scenario 5: `MentorDeps` 字段集合是安全边界白名单

### 1. Scope / Trigger
给 `agent/src/hiddenworld/contracts/deps.py` 的 `MentorDeps` 增加任何字段。

### 2. Contracts
`agent/tests/test_contracts.py::test_mentor_deps_field_boundary` 把该 dataclass 的
字段名集合断言成固定白名单。测试失败**不是让你更新白名单**，而是要求你先论证
新字段不会把隐藏信息带给唯一能对学生说话的组件。

`guard_only` 是唯一例外：它对 prompt 不可见，`build_mentor_prompt` 绝不读取。

### 3. Good/Base/Bad Cases
- Good：新字段的数据类与已有字段同源（如 `released_evidence` 已给全原文）
- Base：信息 Mentor 已能从 `transcript` 推出 → **不加字段**
- Bad：为了传「学生大概想看什么」而加 `tentative_actions`——该信息 Mentor
  能从学生原话直接读到，为此拓宽显式守卫的边界不划算（本任务已放弃该改动）

---

## Common Mistake: 给 Python 契约加字段忘了同步 Go 结构体

**Symptom**：推理摘要正常显示、导师正文正常流完，紧接着整轮报「排查导师返回了无效结果」。
Python 容器日志显示 `POST /turn/stream 200 OK`——它成功了。

**Cause**：`agentclient/client.go:287` 对最终 `result` 事件用 `DisallowUnknownFields()`。
Python 侧给 `TurnAnalysis` / `AgentTurnResult` 任何一层加字段而 Go 结构体没声明，
严格解码就报未知字段，`TurnStream` 返错，`classifyScenarioAgentError` 落到兜底分支
`agent_invalid_response`。

失败为什么发生在最后：流式事件（`turn_analysis` / `public_trace` / `reply_delta`）走的是
宽松的 `json.Unmarshal`，只有终值走严格解码。所以过程全部正常，终点突然失败。

**为什么单侧测试抓不到**：Go 测试自己构造 `TurnResult`，Python 测试不碰 Go。
两边单测都是绿的。

**Fix / Prevention**：
- Python 契约增删字段时，同步 `backend/internal/agentclient/types.go`
- 重新生成 `backend/internal/agentclient/testdata/turn_result_golden.json`
  （由 agent 侧真实主链产出，不要手写）
- `TestGoldenPythonPayloadSurvivesStrictDecode` 会拿真实 payload 过一遍严格解码
- **不要**为了图省事去掉 `DisallowUnknownFields()`——那是"Go 不信任 Python"的一部分

---

## Common Mistake: 以为改 Python 就够了

**Symptom**：Python 侧测试全绿，实际请求整轮 `proposal_rejected` 或 `public_trace_rejected`。

**Cause**：Go 对每一条提议和每一个公开事件都做独立复核，且默认拒绝未知形状。

**Fix / Prevention**：任何 `ProposalKind`、`RunEventKind`、Guard 判定规则的改动，
先在 `scenario_agent.go` 找到对应的校验分支，两侧同时改、同时加测试。

## Scenario 6: 题目级虚拟工具目录必须闭环

### 1. Scope / Trigger
新增日志、配置、指标、数据库或依赖类模拟查询，或修改生成题目 JSON 结构。

### 2. Signatures
```json
{
  "tool_id": "tool.logs.callback",
  "kind": "logs",
  "target": "回调访问日志",
  "aliases": ["看回调日志"],
  "query_patterns": ["SELECT * FROM callback_access_log"],
  "redacted_parameters": ["time_range"],
  "simulated_output": "仅用于题目虚拟数据的模拟输出",
  "observation_action": "inspect:logs.callback_timeout",
  "evidence_ids": ["E_CALLBACK_TIMEOUT"]
}
```

### 3. Contracts
- `virtual_tools` 是 `HiddenWorld` 的可选字段；旧题目为空时继续使用 `observations`。
- Interpreter 只允许把自然语言或 `SELECT/SHOW/DESCRIBE/EXPLAIN/WITH` 只读查询映射到题目声明的唯一 `observation_action`。
- SQL、Shell、HTTP 和外部 API 永远不执行；题目未声明的表、字段或命令返回 `unsupported`。
- `observation_result` 仍是跨层事件，前端集中渲染为线索释放卡片并按 `action + result` 去重。

### 4. Validation & Error Matrix
| 条件 | 结果 |
|---|---|
| 工具引用不存在的 observation/evidence | 生成题校验失败 |
| 同一自然语言命中多个不同动作 | 不释放观察，标记 `ambiguous` |
| 未声明的 SQL 或外部命令 | 不执行，标记 `unsupported` |
| 重复请求已公开动作 | 不重复释放相同线索 |

### 5. Good/Base/Bad Cases
- Good：`看一下数据库日志` 命中 database 工具并显示题目声明的订单写入模拟记录。
- Base：没有 `virtual_tools` 的旧题目仍可按规范 action 查询。
- Bad：把“数据库日志”静默替换成网关日志，或提示学生登录真实服务器。

### 6. Tests Required
- 生成题 Schema/Validator 断言工具字段完整且引用有效动作。
- Agent 运行时断言自然语言、只读 SQL、重复请求和未声明查询的四种路径。
- Go 严格解码断言 `virtual_tools` 与 `observation_result` 可跨层传输。
- 前端断言历史线索恢复不播放动画，新线索只播放一次且不在聊天流重复显示。

### 7. Wrong vs Correct
#### Wrong
```text
学生：看一下数据库日志
导师：数据库日志这个动作没有开放，请去服务器查看。
```
#### Correct
```text
学生：看一下数据库日志
系统：在题目虚拟数据上模拟订单写入日志，并把状态码、连接池和超时字段放入线索卡。
```
