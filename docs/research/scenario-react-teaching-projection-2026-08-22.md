# Scenario ReAct：教学状态安全投影与掌握度面板（2026-08-22）

## 阶段结论

本阶段把已经存在于 Runtime/Agent 内部的结构化教学状态，转换为学生侧可以理解、但不会泄露内部裁判信息的 `teaching_projection`。

投影只回答四个产品问题：

1. 导师当前准备怎样承接这一轮；
2. 学生当前是在沿证据推进、探索中，还是需要收拢范围；
3. 回复应保持简洁、均衡还是详细；
4. 哪些概念和排查能力已经形成稳定掌握信号。

原始 `student_affect`、`hypothesis_id`、`evidence_id`、答案比较、solution coverage 和内部提议审批结果仍然留在 Runtime/Agent 边界内，不进入该投影。

## 1. 数据契约

后端新增 `domain.ScenarioTeachingProjection`，作为 `ScenarioMessage.ResponseMeta.TeachingProjection` 持久化。字段为：

```json
{
  "teaching_state": "normal_diagnosis",
  "progress_assessment": "partial",
  "direction_status": "exploring",
  "detail_level": "balanced",
  "focus": "data",
  "mastery": {
    "concepts_covered": 0,
    "concepts_total": 8,
    "skills_covered": 0,
    "skills_total": 3,
    "concepts": [],
    "skills": []
  }
}
```

### 方向信号

`direction_status` 不是“正确/错误”的答案裁决，而是安全的教学导航：

- `aligned`：本轮有公开进展，继续沿证据理解；
- `exploring`：暂未形成稳定进展，保持开放探索；
- `needs_refocus`：卡住、随机枚举、无进展或与已有方向冲突，需要收拢范围；
- `off_topic`：先回到当前故障，不暴露应该查哪个内部对象。

### 掌握度权重

Runtime 使用已有 `LearnerState.ConceptMastery` / `SkillMastery` 的 0–4 权威值，投影成：

```text
weight = level / 4
```

例如 `level=2` 会显示为 `50%`。这个数值只表示解释深度的教学权重，不表示根因概率、工具可用性或答案正确率；它也不会替代 `available_tools` 的消费/前置条件过滤。

概念标签来自题目公开教学模型，内部概念 ID 不外发。能力标签固定为“日志阅读”“因果推理”“跨层排查”。只有 `level > 0` 的条目才列出，避免把未展示过的概念清单当成答案提示。

## 2. Go 侧归约

`backend/internal/httpapi/scenario_agent.go` 新增 `scenarioTeachingProjection(...)`：

- 从 `TeachingDecision` 归约安全的教学状态和粗粒度焦点；
- 从 `TurnAssessment` 归约进展和方向信号；
- 从提交后的 `LearnerState` 归约概念/能力掌握度；
- 对模型输出的枚举进行白名单收窄，未知值回退到安全默认值；
- 不读取或外发 `InternalVerification`、假设 ID、证据 ID 和原始 affect。

投影写入 `ResponseMeta`，随 `scenario_messages.response_meta` 一起存储，因此浏览器刷新、历史回放和幂等重放都能使用同一份结果。MemoryStore 也新增了投影的深拷贝，避免历史记录共享可变 slice。

## 3. Agent 动态提示词约束

在 `agent/src/hiddenworld/agents/scenario_agent.py` 中补充了明确语义：

- `concept_mastery` / `skill_mastery` 是本会话的解释权重，只用于决定补不补概念、解释多深；
- 不得把权重当成根因概率、工具可用性或答案正确率；
- 不把内部数值原样说给学生；
- 瞬时状态和 `mentor_persona` 决定表达方式，掌握度和公开证据决定“教什么”。

这保持了用户原先提出的“瞬时状态影响语气、长期掌握度影响深度”的分工，同时避免把前端展示字段反向变成新的裁判来源。

## 4. 前端“学习状态”面板

新增：

```text
frontend/src/features/scenarios/TeachingStatePanel.tsx
```

页面从最近一条带有 `response_meta.teaching_projection` 的消息读取投影，在左侧题目快照中展示：

- 回应节奏：沿公开证据排查、先核对方向、补完整因果链等自然语言；
- 方向信号：沿证据推进、正在建立链路、需要收拢范围、先回到当前故障；
- 本轮进展：形成新的推进、已有部分推进、暂未形成新推进等；
- 解释深度：简洁、均衡、详细；
- 当前焦点：日志、指标、配置、变更、依赖、数据、资源；
- 概念掌握与排查能力的数量汇总及已出现条目的百分比进度条。

没有投影的旧会话显示兼容空状态：“完成一轮对话后，这里会显示导师当前的承接方式和解释深度。”不会伪造历史情绪或掌握度。

## 5. 真实浏览器证据

验证页面：

```text
http://localhost:5173/scenarios/session/dbea8f45e84ea2ce3f1d687d9a624f51
```

### 旧历史兼容

新代码首次打开已有会话时，旧消息没有 `teaching_projection`，面板正确显示空状态；题目快照、工具目录、线索和历史工具卡没有受到影响。

### 新回合投影

发送：

```text
我有点困惑，先用一句话说清楚目前的重点，不用查新日志。
```

回合完成后，投影显示：

```text
回应节奏：沿公开证据排查
方向信号：正在建立链路
本轮进展：已有部分推进
解释深度：均衡
当前焦点：数据
概念掌握：0/8
排查能力：0/3
```

页面没有新增工具调用，导师只生成了一句澄清回复，符合用户明确要求“不用查新日志”。

随后发送：

```text
请只用半句话确认当前重点，不要调用工具。
```

页面即时更新为：

```text
本轮进展：正在判断
解释深度：简洁
```

刷新页面后仍保留同一投影，证明它来自持久化 `response_meta`，不是前端临时状态。

### 关于掌握度验证

又发送了明确复述 HTTP 504 含义的消息。该轮的页面状态和回复正常，但模型没有提交被 Runtime 批准的概念掌握增量，因此概念汇总仍为 `0/8`。这说明面板没有伪造掌握度；只有 Agent 明确给出合法 `concept_mastery_signals` 且 Go 通过 `increment_concept_mastery` 闸门后，百分比条才会出现。

### 控制台和部署

真实浏览器控制台错误 0、警告 0。已执行：

```powershell
docker compose up -d --build api agent
```

结果：

- Go API 镜像构建成功；
- Agent Python 镜像构建成功；
- `teaching-mvp-agent-1` 为 `healthy`；
- `teaching-mvp-api-1` 已启动。

本阶段没有运行测试套件，也没有加载 Trellis 工作流；部署构建只用于让真实浏览器加载最新 Go/Agent 代码，验收依据是实际页面、接口回放、DOM 文本和浏览器控制台。

## 6. 当前边界与下一阶段

本阶段有意没有实现“Gateway 0.8、Nginx 0.1”这种工具级权重。原因是工具可用性由 `available_tools` 的消费状态和证据前置条件决定，而掌握度权重描述的是学生理解/排查能力；把两者混成一个数字会让 Agent 错误地把“更熟悉某组件”当成“应该调用某工具”。

如果后续确实需要工具推荐排序，应新增独立的、可解释的 `recommendation_score` 投影，并明确它只影响 QuickAction 排序，不改变 Runtime 授权或 `available_tools` 目录。
