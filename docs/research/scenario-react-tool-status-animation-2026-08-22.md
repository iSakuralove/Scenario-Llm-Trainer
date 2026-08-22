# Scenario ReAct：工具调用状态动画（2026-08-22）

## 阶段结论

工具列表与 Agent 流中的工具卡现在共用一条事件驱动的视觉语义：

```text
pending  →  running  →  completed
调用排队     执行中高亮     完成勾选
```

状态由 Runtime 的公开任务事件和工具结果决定，前端不使用定时器猜测“什么时候完成”。因此动画可以表达过程，但不会把尚未收到的结果伪装成成功。

## 与 AICSS 参考的对应关系

参考页面采用固定图标槽位叠放多个 SVG，通过透明度、缩放和过渡在同一位置切换状态。本项目保留了这个核心结构：`ToolCallStatusIcon` 一次渲染待调用、执行中、完成和失败四个 glyph，只通过 `data-state` 选择当前可见图标。

本阶段把视觉链路收敛为：

- `pending`：虚线圆环以较慢速度旋转，表达“调用已进入队列，尚未收到执行开始事件”；
- `running`：Loader 旋转并使用青绿色高亮，同时工具行出现左侧强调线和浅色背景；
- `completed` / `already_completed`：勾选图标交叉淡入并做一次轻微弹性放大；
- `failed`、`unsupported`、`rejected`、`expired`：统一进入错误色终态，不使用成功勾选。

图标树不按状态改变 React `key`，所以状态变化不会重建槽位，交叉淡入可以连续完成。

## 修改文件

- `frontend/src/features/scenarios/agentrun/AgentRun.module.css`

具体变化：

1. 为 `pending` 增加低对比度颜色与 `taskSpin 1.6s` 慢速旋转；
2. 保留 `running` 的 `taskSpin 1.1s` 高亮执行态；
3. 保留完成态 `toolStatusPop`，让勾选从小到大落位；
4. 为工具行加入背景与阴影过渡，使运行中高亮和完成后的普通态切换更自然；
5. 在 `prefers-reduced-motion: reduce` 下同时关闭待调用、执行中和完成态动画，保留静态状态颜色与图标差异。

## 真实浏览器证据

浏览器页面：

`http://localhost:5173/scenarios/session/6c87e16b7c99e59cee7c3afa55c3e149`

已完成以下真实交互：

1. 点击“调用 inspect:resource.ephemeral_storage”；
2. 页面立即进入“正在处理本轮内容”，工具目录按钮变为禁用，输入框变为禁用；
3. 工具结果返回后，页面出现“工具调用完成”状态，公开结果卡可展开查看安全 JSON；
4. 通过公开 DOM 读取确认工具行终态为 `data-tool-state="completed"`，状态图标的 `aria-label` 为“工具调用完成”；
5. 再点击“调用 inspect:metrics.memory”，页面完成第二次工具调用，并显示“终止前内存低于 limit，事件不是 OOMKilled”的公开观察。

工具执行时间很短，`running` 事件在浏览器中是瞬时状态；源码已经保证该状态由 `task_upserted`/运行事件驱动，而不是前端计时器。浏览器可稳定捕获的是调用期间的禁用/处理中状态和完成后的勾选终态。

## 失败与兼容边界

- 工具失败、超时、拒绝和不支持不会被映射为完成；`toolResultToTaskState` 只把 `succeeded` 映射为 `completed`。
- 旧事件经过 `LegacyEventAdapter` 后仍使用同一 `ToolCallStatusIcon`，不需要复制一套动画。
- 历史消息没有 task 事件但有工具结果时，结果卡会以完成或失败终态呈现；不会显示虚假的执行中动画。
- 页面减弱动效时不会删除状态图标，只关闭旋转和弹性过渡，避免用户看不到工具是否完成。

## 验收边界

- 已按真实浏览器交互验证工具调用进入处理中、完成勾选和结果详情展示；控制台未发现新增错误。
- 未运行测试套件或额外编译校验，符合本阶段“真实浏览器优先”的验收约束。
- Docker 重建用于加载跨轮方向回注的 Go/Python 服务；前端本地页面由现有开发服务器提供，CSS 热更新后完成浏览器交互。
