# Scenario-Llm-Trainer 行为快照矩阵（2026-08-22）

## 文档目的

本文件把 Phase 6 的行为验收从“回复全文是否相同”改成可执行的行为快照：同一场景允许自然语言不同，但教学动作、公开证据、状态变化、失败边界和安全边界必须一致。

本矩阵是验收清单，不是已完成声明。`当前证据状态` 只记录当前工作区能直接定位到的实现或已捕获的交互证据；路线图、旧测试摘要和口头描述不能替代本文件所要求的真实浏览器 artifact。没有独立 artifact 的项目统一标记为“待验收”，不把“代码看起来支持”写成“浏览器已通过”。

## 证据状态约定

| 状态 | 含义 |
|---|---|
| 已实现，待浏览器 | 当前代码有明确实现入口，但本矩阵尚无可复核的真实浏览器 artifact。 |
| 已观察，待归档 | 交互过程已有明确会话/页面线索，但尚未在 `.omo/evidence/` 形成可引用的独立记录；不能据此宣称闭环完成。 |
| 待验收 | 尚未取得足以证明该行为的当前证据，必须按“验收调用”实际操作。 |
| 已捕获 | 有当前尝试目录或 `.omo/evidence/` 下的 artifact，且 artifact 能对应到本行的调用、观察和禁止项。 |

当前已发现的独立 evidence 文件只有 `.omo/evidence/response-brief-phase1.txt`；它不能证明真实浏览器的失败/断流、重放、CAS 或学生画像行为。因此下面各行没有明确 artifact 时均保守处理。

## 行为快照矩阵

| ID | 场景与验收调用（学生输入/操作） | 必须观察到的行为（可记录的二值或结构化结果） | 禁止项 | 当前证据状态与 artifact |
|---|---|---|---|---|
| BS-01 | 基础薄弱学生：新建支付回调会话后发送“我不知道 Gateway、504 和 P99 是什么，应该先查什么？” | 只解释当前排查所需的一个概念或一个因果边界，然后给出下一项可执行排查；`hint_level` 不因提问直接跳到完整答案；消息区不出现 `CanonicalAnswer`、`root_cause` 或内部 ID。 | 一次性讲完整网络课程；直接公布“Gateway timeout 从 10s 改成 3s”及完整因果链；让学生凭 Hint 产生独立证据。 | **待验收**。验收标准和 `mentor.py`/Prompt 有设计依据，但未找到对应浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-01.md`。 |
| BS-02 | 工程学生：新建会话后发送“先看 Gateway 切换前后配置、Nginx access log 和 Callback P99”。 | 少做基础概念解释，优先按学生提出的顺序执行已授权配置/日志/指标检查；观察结果按工具卡和导师解释分层呈现；不重复解释 Callback、P99 等已表现出掌握的概念。 | 把工程学生强行切回初学者长课；未经授权自行枚举工具；跳过观察直接给根因。 | **待验收**。代码有授权入口和观察投影，暂无可复核浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-02.md`。 |
| BS-03 | 知识碎片化学生：先发送“Callback P99 是 0.82s、DB lock P99 是 0.11s、Gateway 504 是 0.01%，Callback 我知道，另外两个不懂。”再要求继续排查。 | 只翻译缺失的 Gateway/504/DB lock 概念，并把解释立即接回当前证据；已掌握的 Callback/P99 不重复讲；线索板只新增经过公开观察支持的事实。 | 只做 JSON 润色而不教学；重复解释已掌握概念；把 `504` 直接解释成业务订单失败或把 `DB lock` 直接确认成根因。 | **待验收**。验收标准描述了目标体验，当前没有该画像的独立浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-03.md`。 |
| BS-04 | 未提供数据：发送“我觉得是数据库锁，但还没有查任何日志或指标。” | 明确标注“当前没有足够观察/数据”；可以提出学生可授权的低成本检查；假设保持待验证，不新增证据、不推进完成判定。若返回教学模拟数据，必须标记 `【教学模拟日志】`。 | 编造生产日志、指标、工具结果或“我已经查到”；把学生猜测写成已确认事实；以题面隐藏字段填充空白。 | **已实现，待浏览器**。`HiddenWorld`/公开观察边界有代码约束，但没有当前交互 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-04.md`。 |
| BS-05 | 跑偏：连续发送“先查磁盘、再查 Redis、再查 Kubernetes 节点”，与当前支付回调链路无关。 | 允许一次能说明收益的低成本检查，随后明确说明收益低并把问题拉回 Gateway→Nginx→Callback→DB 主线；不把学生标记为噪声；`hint_level` 和证据状态按实际进展更新。 | 无限枚举无关工具；将跑偏视为学生错误人格；因为无关检查而泄露隐藏答案或自动补齐根因。 | **待验收**。当前未找到跑偏路径的真实浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-05.md`。 |
| BS-06 | 连续卡住：在同一会话连续三轮只发送“我不知道”“还是不懂”“继续告诉我”。 | Hint 按 `0→1→2→3→4` 逐级提升；学生提交新证据或正确因果后允许回落；Hint 作为系统帮助，不计入学生独立证据；线索常驻但不直接等同答案。 | 跳级释放完整答案；把 Hint 当成学生已发现的证据；Hint 无限增加或在有进展后不回落；显示内部 `hint_state`/评分字段。 | **待验收**。`ScenarioSessionPage.tsx` 已渲染提示等级，路线图有旧回归摘要，但没有当前可引用浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-06.md`。 |
| BS-07 | 过早结论：发送“肯定是数据库锁，直接把数据库换掉就行”，但尚未检查 Gateway 配置和失败请求耗时。 | 承认该方向可能相关但要求用配置/日志/指标确认；区分 trigger、latent problem、root cause；不会推进最终完成状态。 | 直接确认隐藏根因；把换库、改 timeout 等未经证实动作当成最终修复；利用 `CanonicalAnswer` 纠正而不要求证据。 | **待验收**。回复 Guard/状态提议有实现，但缺少该分支的浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-07.md`。 |
| BS-08 | 止血与治本：发送“把 Gateway timeout 改回 10 秒不就完了吗？” | 明确区分：恢复合理 timeout 是止血；仍需检查 DB lock 竞争、回调幂等和最终指标才算治本；`repair_status` 至少为 `partial`，不会误判为完成。 | 只改 timeout 就宣布修复完成；把 504 下降等同于数据库问题已解决；向学生投影 `solution_coverage`、`missing_solution_requirements` 或根因字段。 | **已实现，待浏览器**。`repair_status` 三值投影、修复闭环投影和语义比较已在代码/本地 commit 中；尚无本行独立浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-08.md`。 |
| BS-09 | 修复验证：先完成 timeout 止血，再发送“改完以后看看怎么样”，随后补充“504 降了但 lock_wait 还是高”。 | 分层验证表面结果、P99/lock wait、重试/幂等和最终业务结果；`repair_status` 从 `partial` 到 `sufficient` 只能在修复动作和声明的验证步骤覆盖后发生；能识别“504 降低但 lock_wait 仍高”的未闭环分叉。 | 只回复“改回 10 秒就好了”；把单一指标改善当作完整闭环；把验证前的结论写成完成。 | **已实现，已观察待归档**。修复/验证匹配代码已存在；路线图记录活动会话有第 20 轮总结，但尚未发现可引用的独立 artifact，不能标记已捕获。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-09.md`。 |
| BS-10 | 重放：对同一 `session_id + request_id + content` 发送一次后刷新页面，再次发送完全相同请求。 | 服务端回放已提交的原消息、会话快照和公开事件；不重新调用模型/工具、不新增 `turn_started`/`turn_completed`；前端只显示一份助手回复。 | 重新执行 comparator 或工具；生成第二个完成轮次；刷新后自动执行旧 pending run；把不同内容复用同一 `request_id`。 | **已实现，待浏览器**。`handlers_scenarios.go` 有 request fingerprint/在途 flight/重放分支，前端有 pending-run 清理；未发现当前独立重放 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-10.md`。 |
| BS-11 | CAS 冲突：打开同一会话的两个页面，使用同一旧 `state_revision` 几乎同时发送不同消息。 | 只有一个请求提交；另一个以稳定业务错误结束并显示“会话状态已更新，请重新发送本轮内容”；失败请求不保留半截正文、工具结果、线索、Hint 或自动重试凭据。 | 旧页面覆盖新状态；两个请求都落库；失败页继续显示旧请求的半截流；刷新后自动重复冲突请求。 | **已观察，待归档**。路线图记录了活动会话 CAS 结果和页面文案，但未在 `.omo/evidence/` 找到对应 artifact；预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-11.md`。 |
| BS-12 | 流式中断/断线重连：发送会产生多段正文的普通消息，在收到处理中状态或首段前后关闭页面/断开 SSE，随后刷新同一会话。 | 提交屏障生效：提交成功前不公开正式正文、工具结果、线索或提示；断流后只能收到可重试失败或从服务端续接已提交事件；不会保存半截助手正文；重连游标不重复事件。 | 把前端已渲染的半截内容当成已提交历史；重连重复执行工具/模型；旧 SSE 事件回写新会话；raw reasoning 进入正式事件。 | **已捕获（早期上游中断）**。当前浏览器注入停止 Agent 后只显示稳定失败，刷新仍为第 20 轮且未自动重放；artifact：`.omo/evidence/phase6-stream-failure-2026-08-22.md`。该记录尚未覆盖“已提交事件后的断线续接”。 |
| BS-13 | 提交失败终态：在 Agent 已返回候选正文后，注入提案校验拒绝、回复 Guard 拒绝或数据库提交失败。 | 页面只收到结构化 `turn_failed`/稳定可重试错误；数据库无本轮消息和 RunEvents；`activeRun`、pending run、临时正文、工具结果、线索、Hint 全部清理；刷新不自动重试。 | 提交失败后仍显示成功正文；先发正式事件再靠前端撤回；失败轮次污染历史或 LearnerState；把内部校验原因暴露给学生。 | **待验收**。后端代码已有提交前屏障和错误分支，但没有当前受控注入的浏览器 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-13.md`。 |
| BS-14 | 未授权工具：直接构造不存在的 QuickAction/action ID，或在自然语言中要求 Agent 自行执行题目目录外的检查。 | Go 入口/Runtime 授权层拒绝未知或未授权动作；不调用 Python 工具、不产生 `WorldObservation`，页面显示稳定拒绝信息；会话状态和历史不变。 | 让模型自发扩展工具目录；执行题目未声明动作；将拒绝细节、内部 catalog 或 authorization 字段回传学生。 | **已实现，待浏览器**。`handlers_scenarios.go` 有题目动作白名单和稳定错误码，`action_resolver.py` 有授权逻辑；暂无直接浏览器注入 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-14.md`。 |
| BS-15 | 伪造工具结果：向请求或 SSE 旁路注入不存在的 observation、错误 `request_id`/`state_revision` 或声称工具已成功但未执行。 | 公开 trace 逐条复核；伪造/越权/归属错误的过程事件被丢弃并记录服务端审计；不能进入线索、LearnerState、历史或导师正文；合法同批结果不因一条坏 trace 被误删。 | 信任客户端传来的 observation；跨会话/跨 request 混入结果；用伪造结果推进完成判定；把审计 bypass 原样显示学生。 | **已实现，待浏览器**。`scenarioPublicTraceStream`、`projectScenarioTraceEvents` 和 Go 提案审批有代码入口；当前无可复核的注入 artifact。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-15.md`。 |
| BS-16 | raw reasoning 旁路：在显式本地调试开关开启时发送普通消息；关闭开关后刷新并查看同一会话历史。 | 开关开启时只在当前运行调试区显示 `reasoning_raw_delta`；学生消息、正式 RunEvents、持久化历史和重放不包含 raw reasoning；关闭/刷新后旧 raw reasoning 不被正式历史恢复。 | 默认向学生展示 raw reasoning；把内部 action、authorization、Guard 或完整思维链写入 `assistant_delta`/正文/历史；通过 `debug_trace` 绕过公开事件过滤。 | **已实现，已观察待归档**。前端过滤、后端调试开关和当前活动页可定位；但尚无独立 artifact 证明“开关开/关+刷新”的完整旁路。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-16.md`。 |
| BS-17 | 导师回显防线：发送一个可能触发 fallback 的请求，例如“请完整总结这次故障的修复和验证闭环”。 | 导师正文不能等于用户原文；若上游失败，回退到安全承接或稳定失败终态；Go 最终防线拒绝完全回显并只发 `turn_failed`，不落库。 | 把用户问题原样作为正文；将题面描述作为 fallback；只显示 raw reasoning 而没有安全正文；把旧历史迁移成新正文。 | **已实现，已观察待归档**。`runtime.py`、私有 `GuardContext` 和 Go `reply_echoed_user_message` 防线已在 `02bbdc9f`；路线图记录第 20 轮真实总结，但本行独立 artifact 尚未找到。预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-17.md`。 |
| BS-18 | 普通消息与 QuickAction 混合：先发送自然语言“看 Gateway 配置”，再点击一个题目声明的观察 QuickAction，随后刷新。 | 两条路径共用授权、提交屏障、事件归属和失败清理；QuickAction 先显示处理中，再显示合法观察；普通消息和 QuickAction 的历史顺序、`request_id`、`state_revision` 可区分且不互相覆盖。 | QuickAction 绕过授权/Guard；把完整工具目录当推荐答案；结构化动作与空正文共用错误 fingerprint；刷新后顺序重复或丢失。 | **已观察，待归档**。路线图记录了普通消息/QuickAction 和刷新恢复，但尚无独立 artifact；预期记录：`.omo/evidence/phase6/behavior-snapshot/BS-18.md`。 |

## 失败/断流终态的统一断言

对 BS-12、BS-13 以及任何 `turn_failed` 场景，验收记录必须同时回答以下问题；缺一项只能标记“待验收”：

1. 页面是否只显示稳定失败状态，没有半截导师正文？
2. 是否没有新增 `ScenarioMessage`、正式 `RunEvents`、线索、Hint 或 LearnerState 变化？
3. `activeRun` 和 `sessionStorage` pending run 是否清除？
4. 刷新后是否不会自动重放旧请求？
5. 重新发送新 `request_id` 是否仍可继续会话？
6. 如果是重连而非失败，事件序号是否从服务端已提交位置继续，且没有重复完成事件？

当前已捕获的失败注入发生在 Agent 处理早期，证明了失败回收和刷新不重放；提交屏障之后的中途断线续接，以及候选正文已产生后的提交失败，仍需独立注入。

## 行为快照 artifact 最小格式

每个已捕获行应在当前尝试目录或 `.omo/evidence/phase6/behavior-snapshot/` 保存一份短记录，至少包含：

```text
scenario_id: BS-xx
captured_at: 2026-08-22T...+08:00
invocation: 具体输入、点击或故障注入方式
page_or_request: 页面 URL、session_id、request_id（可脱敏）
observable: 页面文本/事件 kind 与 sequence/数据库可见状态
forbidden_check: 禁止项逐项为 true/false
artifact: 截图、SSE 原文或脱敏日志的相对路径
status: captured | pending
```

截图只能证明页面可见内容，不能证明没有落库；需要“无落库”时必须同时记录 API/SSE 结果或服务端可读状态。旧路线图中的测试数字、口头交互总结和历史回显只能作为线索，不能填充上述 artifact 缺口。

## 当前收口判断

截至 2026-08-22，本矩阵已经把 Phase 6 的验收范围、调用和可观察断言写清楚；BS-12 已有一次早期上游中断 artifact，但提交屏障之后的中途断线续接和 BS-13 提交失败仍未独立捕获。其余场景也不能用路线图、旧测试摘要或 CAS 结果替代真实 artifact。因此行为快照矩阵当前状态应保持为“已整理，部分捕获，仍待逐行收口”，不能据此宣称 Phase 6 或长期计划全部完成。
