# Scenario Teaching-ReAct 阶段实施记录（2026-08-22）

> 本文记录本阶段已经发生的代码、镜像和真实浏览器证据。没有通过浏览器或命令实际确认的内容，统一标为“待验收”，不把路线图写成完成结论。

## 1. 阶段目标

本阶段先收口两个阻断点，再验证支付回调排查题的最小 Teaching-ReAct 闭环：

1. 保持 Python Agent 的结构化 JSON 契约不变，同时允许本地调试环境通过开关展示原始思考旁路。
2. 修复 Go Hint 审批分支和错误日志导入，使当前源码可以生成包含 `repair_status` 字段的 API 镜像。
3. 用新建的真实浏览器会话验证“用户消息 → 思考调试区 → 工具卡 → Observation → 导师回复 → 状态推进”。

## 2. 已发生的代码改动

### Python Agent

- `agent/src/hiddenworld/agents/scenario_agent.py` 增加 `HIDDENWORLD_TEST_STREAM_RAW_REASONING` 开关读取。
- 开关关闭时继续使用原始思维链禁令；开关开启时改用调试思考通道说明，但仍禁止把 `reasoning`、隐藏答案、未公开证据或内部字段放进结构化 JSON。
- Prompt 继续只投影安全 `AgentContext`；`hidden_world`、`canonical_answer`、`completion_allowed` 不进入上下文载荷。
- `agent/tests/test_scenario_agent_loop.py` 增加开关关闭/开启的 Prompt 回归断言。

### Go API

- `backend/internal/httpapi/scenario_agent.go` 的 `scenarioHintTransitionAllowed` 改为显式的 `assessment == nil` 分支，消除原先跨行括号写法导致的语法错误。
- 同一文件补充标准库 `log` 导入；第 277 行的未分类错误日志现在可以正常编译。

## 3. 之前的失败证据与边界

在旧 API 镜像仍运行时，真实浏览器请求曾得到：

```text
decode streamed result: json: unknown field "repair_status"
request_id=44c9f6f1-3e14-467a-8254-a3e8645b3a2c
页面：排查服务返回了无效结果
```

这不是本轮 Prompt 判断失败，而是 Python → Go 最终严格解码时，运行中的旧镜像不认识源码已经使用的 `repair_status` 字段。该证据只能说明旧镜像契约不匹配，不能证明在线修复已经生效。

## 4. 镜像重建证据

执行命令：

```powershell
docker compose up -d --build agent api
```

结果：`teaching-mvp-agent` 与 `teaching-mvp-api` 均成功构建并重新启动；API 构建阶段执行 `go build -o /out/mvp-api ./cmd/server` 成功。随后由只读构建审查确认：从 `backend` 目录执行 `go test -run '^$' ./...` 全部 Go 包编译通过，退出码为 0。

普通测试没有作为本阶段验收依据：只读审查中发现既有错误文案断言不匹配，以及无关的 bcrypt 初始化长耗时；这些问题不影响当前源码编译结论，也没有在本阶段扩展修复范围。

## 5. 新会话真实浏览器验收

入口：`http://localhost:5173/scenarios`

新建会话：

```text
session_id=6ee66b162422956748ae646705316766
题目=支付回调在网关切换后间歇性超时
```

发送用户原话：`先看看 CPU`。

已观察到的完整链路：

1. 页面先出现“正在处理本轮内容”，输入框和发送按钮暂时禁用。
2. 原始思考调试区出现，并在结束后显示耗时约 32 秒；它没有被作为学生消息写入正式历史。
3. 出现 `Callback Pod 的 CPU 与内存` 工具卡，状态从处理中变为“已返回”。
4. Observation 显示：`Callback 的 6 个 Pod 资源平稳：CPU 和内存均低于 45%，没有排队或重启峰值。`
5. 页面轮次从 `0/50` 推进到 `1/50`，已形成证据变为 `1`。
6. 调查状态把“回调服务资源打满”标记为已降低优先级，并保留“资源平稳”这一公开事实。
7. 页面出现导师回复后的可检查项：订单库磁盘 IO、回调访问日志、网关 VIP 发布记录。
8. 点击工具卡后，卡片进入展开状态；本次页面文本没有额外显示可见 JSON 代码块，因此“可展开安全 JSON 详情”仍需单独补验。
9. 刷新同一会话后，轮次仍为 `1/50`、Observation 和工具摘要仍在，但原始思考调试区不再出现，证明本次刷新没有回放 Thought。

本轮没有再次提交旧失败请求，也没有复用旧会话 `dbeb5b07c4da28049fe92df04e7b7c12`。

## 6. 当前结论

本阶段已确认：

- Python Agent 的调试思考旁路可以工作，且结构化输出边界仍保持。
- Go 源码的语法阻断和 `log` 缺失导入已修复。
- 新 API/Agent 镜像已经重建，真实浏览器已不再复现旧的 `repair_status` 无效结果错误。
- 首轮工具调用、Observation 回灌和状态推进能够完成。

本阶段尚未确认：

- 关闭 `HIDDENWORLD_TEST_STREAM_RAW_REASONING=0` 后的完整浏览器闭环，以及刷新后不回放 Thought 的完整证据。
- 工具详情点击后是否能展示标准安全 JSON；当前验收只观察到展开状态。
- 动态 `tools_list`、`action_history`、`tool_state_view`、阶段标记、用户瞬时/长期状态、掌握度权重和方向偏离提示的完整实现。
- 提交排查结论、断流续接、失败回收和最终 `repair_status` 在线提交。

## 7. 下一阶段入口

下一阶段应先沿现有 Runtime 权威边界实现安全上下文投影：`phase`、`turn_id`、`action_history`、`tool_state_view`、当前 Observation 与历史 Observation 隔离；再同步 Go 严格解码、SSE 事件和前端安全 JSON 投影。原始 Thought 继续只走调试旁路，不能进入正式学生历史、正式 V2 事件或 CanonicalAnswer。
