# Scenario ReAct：评估字段一致性归一化（2026-08-22）

## 发现的问题

真实浏览器中验证闲聊/偏题回合时，页面出现“本轮处理失败，请重试”。容器日志给出的确定性原因是：

```text
turn assessment disagrees with turn analysis
assessment requested_action=""
analysis requested_action="闲聊"
```

根因不是工具调用动画，也不是 Docker 服务不可用，而是模型在结构化 `TurnAssessment` 中只填写了 `requested_action`，没有填写 `requested_action_raw`。Runtime 的 `TurnAnalysis` 为了兼容旧模型，会使用 `requested_action` 作为 `requested_action_raw` 的兜底，于是两份跨层对象出现不一致，Go 的严格校验正确地拒绝了整轮。

## 修复

修改文件：

`agent/src/hiddenworld/scenario_runtime/turn_runtime.py`

在 `_assessment_from_single_agent(...)` 完成 Pydantic 结构化解析后增加最小归一化：

- `requested_action` 非空且 `requested_action_raw` 为空时，将前者复制到后者；
- 不改写用户消息，不猜测工具，不新增授权；
- 保留 Go 对 `TurnAssessment` 与 `TurnAnalysis` 的严格一致性校验；
- 只修复字段缺省造成的契约冲突，不放宽隐藏状态、假设或证据校验。

这样，模型说“闲聊”或表达其他非工具动作时，两份对象会共享同一安全动作摘要，偏题回合可以继续进入 `direction_status=off_topic` 的确定性归约，而不会因为字段漏填被整轮丢弃。

## 为什么必须在 Runtime 修复

不在 Go 侧直接放宽比较，是因为：

1. `TurnAnalysis` 是 Runtime 根据最终评估生成的派生对象，字段同步应在生成源头完成；
2. 放宽 Go 校验会让真正的动作、假设或证据不一致也可能混过去；
3. 该修复不改变用户可见内容，不泄露模型内部字段，只让同一语义的两个公开安全摘要保持一致。

## 浏览器验证边界

修复前的真实页面证据：

- 偏题消息请求已进入 API；
- Agent 容器健康检查正常，`POST /turn/stream` 返回 200；
- API 在 proposal validation 阶段以 502 拒绝，页面显示“本轮处理失败，请重试”；
- 日志明确指出 requested action 字段不一致。

修复后已完成：

1. Docker 重建 `agent` 与 `api`，两个容器重新启动且 Agent 健康检查通过；
2. 在同一真实浏览器会话重试偏题消息，`POST /turn/stream` 返回 200，页面没有再次出现“本轮处理失败”；
3. 学习状态显示“方向信号：先回到当前故障”，页面没有显示 `requested_action_raw`、内部假设或校验错误；
4. 下一轮发送“回到故障、继续看资源问题”后，页面恢复为“方向信号：正在建立链路”，焦点回到“资源”，回复继续承接已有公开事实；
5. 最终浏览器控制台错误与警告均为 0。

本阶段未运行测试套件或额外编译校验，仍以真实浏览器交互为主要验收依据。修复前的 502 已被真实浏览器复现，修复后的偏题 → 回到故障闭环也已在同一会话中完成。
