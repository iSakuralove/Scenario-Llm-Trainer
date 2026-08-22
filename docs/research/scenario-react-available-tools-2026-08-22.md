# Scenario ReAct：动态可用工具目录与状态动画（2026-08-22）

## 本阶段结论

本阶段把“题目声明了哪些工具”和“当前会话现在能调用哪些工具”拆成两个明确边界，并把后者投影到 Agent 上下文和学生侧界面。工具消费仍由 Runtime/Go 权威状态决定，前端不能自行推断工具是否可用。

同时，对照 AICSS To-do List 的状态切换思路，修正了工具状态图标的 React 挂载方式：同一个图标槽保留 pending、running、completed、failed 四个 glyph，仅切换 `data-state`，让状态通过 CSS 交叉淡入完成“等待调用 → 执行中高亮 → 完成勾选”的连续动画。

## 1. 两类目录的边界

### `action_catalog`

`action_catalog` 是题目 HiddenWorld/VirtualTools 声明的全量公开观察目录。它用于让模型理解题目有哪些可能的观察入口，但不是当前轮的授权列表，也不能绕过 Runtime 的消费状态和前置条件。

### `available_tools`

`available_tools` 是 Runtime 根据当前 `LearnerState` 和证据前置条件过滤后的本轮可调用目录。它只保留：

- 题目声明的公开观察工具；
- 尚未出现在 `ActionsTaken` 中的动作；
- 已满足 `EvidenceIDs` 前置条件的动作；
- 非 `internal`、`answer`、`answer_comparison`，且不是 `compare_answer` 的内部能力。

外发条目只包含 `action_id`、`catalog_version`、`tool_kind`、`title`，不暴露参数、证据 ID、授权细节或隐藏答案。

## 2. Go 侧实现

`backend/internal/httpapi/scenario_agent.go` 新增 `scenarioAvailableTools(...)`，从题目快照的 `VirtualTools` 目录生成当前公开工具快照，并完成已消费工具和前置条件过滤。没有 `VirtualTools` 的旧题目回退到公开 `Observations` 目录。

`backend/internal/httpapi/views.go` 在 `scenarioSessionResponse` 增加 `available_tools`，会话视图按：

```text
question_id:model_version
```

生成目录版本。现有 `next_actions` 仍表示 Agent 根据教学策略选出的少量推荐动作，没有被替换成全量目录。

## 3. Python Agent 上下文与提示词

`AgentContext` 新增 `available_tools` 字段。`project_agent_context(...)` 先建立全量 `action_catalog`，再根据 `tool_states` 只投影状态为 `available` 的条目。

动态提示词明确约束：

1. 工具调用只能从 `available_tools` 选择；
2. 不能使用 `action_catalog` 绕过已消费、阻断或不存在的工具；
3. 用户点名的工具不在 `available_tools` 时，不能假装调用；
4. `after_tool_call` 必须优先检查 `tool_results`、`action_history` 和 `tool_states`，避免把同一轮继续误判成新的用户问题；
5. 只有 `succeeded` 且有内容的结果才算形成公开观察，失败、超时、拒绝、未满足前置条件和此前已完成都不能被表述成“已经拿到观察”。

最终 prompt payload 还显式覆盖 `available_tools`，避免模型只看到 Pydantic 默认序列化结果而忽略当前目录。

## 4. 学生侧“当前可用工具”面板

新增 `frontend/src/features/scenarios/AvailableToolsPanel.tsx`，放在左侧题目快照的调查状态之后。面板展示当前目录中的完整公开工具，并复用现有结构化动作发送链路：点击条目会走 `handleQuickAction` / `sendStructuredAction`，不会新增一条不受 Runtime 授权控制的前端直通路径。

面板和回合末 `QuickActions` 的职责不同：

- 左侧面板：完整的当前可调用目录；
- 回合末 QuickActions：本轮教学策略推荐的少量动作。

当目录为空时，页面使用面向学生的自然语言空状态，不向学生暴露权限、Action ID 或内部状态名。

## 5. 工具状态动画

`ToolCallStatusIcon` 维护四个叠放在同一位置的图标：

| Runtime 状态 | 学生侧含义 | 视觉表现 |
| --- | --- | --- |
| `pending` | 等待调用 | 虚线圆淡入 |
| `running` | 正在执行 | Loader 旋转、工具行青绿色高亮 |
| `completed` / `already_completed` | 已完成 | 勾选图标弹入，标题降为完成态 |
| `failed` / `timeout` / 其他失败终态 | 没有形成成功观察 | 错误图标和暖色错误态 |

这次修正移除了 `TaskList` 和 `ToolChipRow` 上的 `key={state}`。如果按状态重建图标组件，React 会直接卸载旧 glyph，CSS transition 无法完成连续过渡；现在只更新 `data-state`，因此 pending/running/completed 在同一槽位内交叉淡入。运行和完成动画也改为只在对应 `data-state` 生效，状态离开时会停止。

同时移除了 Task List 中会覆盖失败图标颜色的全局 `li svg` 颜色规则，并把完成态删除线限定到标题文本，不再错误作用于状态图标。

## 6. 真实浏览器证据

浏览器页面：

```text
http://localhost:5173/scenarios/session/dbea8f45e84ea2ce3f1d687d9a624f51
```

### 动态目录

部署后初始面板显示 4 项：

```text
Nginx 回调访问日志
订单库磁盘 IO
订单库回调写入日志
MySQL 慢查询日志
```

点击“调用 Nginx 回调访问日志”后：

- 页面轮次从 `4/50` 推进到 `5/50`；
- 已形成证据和重要线索各增加 1；
- Nginx 工具从目录中移除；
- 满足新前置条件的“回调服务耗时日志”补位；
- 目录仍然显示 4 项。

因此“消费后数量没有减少”不是消费状态失效，而是“已消费工具被移除 + 新工具动态解锁补位”。

随后用同一真实页面点击“调用回调服务耗时日志”做一次额外闭环确认。完成后目录变为：

```text
订单库磁盘 IO
订单库回调写入日志
数据库锁等待指标
MySQL 慢查询日志
```

这再次证明目录数量可以保持不变，但集合会随着证据前置条件变化而滚动更新。已消费的 Nginx 工具没有重复出现。

### 状态图标和控制台

历史工具卡在 DOM 中均呈现 `data-state="completed"`、可访问标签“工具调用完成”和完成态勾选 glyph。触发额外工具调用时，左侧目录按钮立即进入禁用/处理中状态，回合结束后工具卡回放为完成态，目录同步更新。

真实浏览器控制台读取结果：错误 0，警告 0。

## 7. 部署与验证边界

已执行：

```powershell
docker compose up -d --build api agent
```

当前容器状态：

- `teaching-mvp-agent-1`：`Up (healthy)`；
- `teaching-mvp-api-1`：`Up`；
- Postgres、Redis：正常运行。

本阶段没有运行测试套件、额外编译校验或 Trellis 工作流；验收以真实浏览器交互、页面状态、DOM 状态属性和控制台结果为准。

## 8. 尚未实现的后续范围

动态目录和工具状态动画不等于完整的动态教学状态产品化。以下内容仍需独立阶段设计和浏览器验收：

- 学生瞬时状态的安全 UI 投影：confusion、confidence、frustration、humor/playfulness、urgency、off-topic；
- 长期掌握度的可解释投影：`concept_mastery`、`skill_mastery` 和解释深度权重；
- 当前方向偏离/支持不足的安全投影，让下一轮 Agent 能调整指导策略，但不泄露 HiddenWorld、CanonicalAnswer 或内部假设 ID；
- 掌握度和瞬时状态如何影响回复深度、语气与是否展示 QuickAction 的产品规则。
