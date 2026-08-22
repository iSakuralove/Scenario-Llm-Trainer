# Scenario ReAct 工具调用详情与状态动画阶段记录

日期：2026 年 8 月 22 日  
范围：支付回调故障排查教学 Agent 的工具卡、任务列表与公开工具详情。

## 结论

本阶段把工具调用的“过程”和“结果”拆开呈现：

- `pending`：等待调用，显示低强调的虚线圆。
- `running`：Runtime 已发出工具调用并正在执行，显示旋转图标，同时整行高亮。
- `completed` / `already_completed`：真实工具结果成功到达，显示勾选并做一次轻微入场动画。
- `failed`、`timeout`、`rejected`、`unsupported`、`expired`：显示错误或跳过状态，不伪装成完成。

状态只由任务事件和工具结果事件驱动，不由前端定时器猜测工具是否完成。当前后端观察工具通常直接从 `agent_tool_started` 投影为 `running`，所以真实页面最常见的闭环是“执行中高亮 → 完成打勾”；只有 Runtime 发出排队态时才会出现 `pending`。

## 代码改动

### 安全 JSON 详情

后端公开内容新增固定白名单 `ScenarioPublicContentDetails`，前端展开工具卡时以标准 JSON 展示：

```json
{
  "tool_id": "inspect:metrics.service",
  "tool_kind": "metrics",
  "result_status": "succeeded",
  "duration_ms": 0,
  "source_kind": "teaching_simulation",
  "source_label": "教学模拟",
  "summary": "公开 Observation 摘要"
}
```

详情不包含原始工具参数、授权 ID、隐藏证据 ID、`compare_answer`、CanonicalAnswer、内部错误细节或原始 Thought。旧事件缺少 `details` 时，前端从已有公开字段构造同一安全形状。

`duration_ms` 为 `0` 时表示当前工具源没有提供可用耗时；页面和文档都不把它伪装成真实耗时。

### 状态动画

新增 `ToolCallStatusIcon`，在固定尺寸容器内叠放 pending、running、completed、failed 四种 glyph，通过透明度和缩放过渡切换，保留 AICSS Task List 的“同一位置平滑换图标”思路。

- 单工具：`ToolChipRow` 同时保留工具类型图标与状态图标；运行中增加左侧强调线、渐变高亮和微光。
- 多工具：`TaskList` 复用同一状态图标；完成后仍保留任务行与勾选，不再把列表整体隐藏，方便学生回看调用闭环。
- `prefers-reduced-motion: reduce` 下关闭旋转、勾选入场和状态过渡。

### 结果归约护栏

修复前端状态归约把所有 `tool_result` 都强制改成 `completed` 的问题：

- `succeeded` → `completed`。
- `failed` / `timeout` → `failed`，文案分别显示“失败”或“超时”。
- 未匹配任务的结果也按 `result_status` 投影，避免失败工具卡出现绿色完成勾。

## 真实浏览器证据

验收页面：`http://localhost:5173/scenarios/session/dbea8f45e84ea2ce3f1d687d9a624f51`

在 2026 年 8 月 22 日的真实会话中完成了四次公开工具调用：

1. CPU 指标：工具卡显示“工具调用完成”、绿色勾选、公开 Observation 与安全 JSON 详情。
2. 回调访问日志：同样显示完成勾选，页面轮次推进到 `2/50`，形成第二条公开证据。
3. 网关 VIP 发布记录：显示完成勾选，页面轮次推进到 `3/50`，形成第三条公开证据。
4. 网关切换前后配置差异：显示完成勾选，页面轮次推进到 `4/50`，形成第四条公开证据。

刷新会话后，三个完成态工具卡和公开 Observation 仍能从持久化事件回放；浏览器控制台未发现 error 或 warning。工具返回很快，未在浏览器采样窗口中截到持续的 `running` DOM 快照，但运行态由真实 `task_upserted(running)` 事件驱动，代码未添加人为延时或假进度。

## 未在本阶段做的事

- 没有新增 Runtime 的 `pending` 事件，也没有用前端 `setTimeout` 伪造调用阶段。
- 没有修改题库结构、隐藏答案或工具授权策略。
- 没有运行测试套件、编译校验或 Trellis 工作流；本阶段以真实浏览器加载、交互、刷新和控制台日志为验收依据。
