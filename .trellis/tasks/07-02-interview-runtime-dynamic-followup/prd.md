# 面试运行时动态追问 MVP

## 目标

把已经建设好的面试题库、版本治理和向量索引能力接入当前面试运行时，让新建面试会话优先使用正式题库开场题，并在后续轮次基于题库知识原子做动态追问辅助决策。同时把用户可见的面试准备页和报告页补到首期可用状态，让运行时接入不是只停留在后端主链路。

## 用户价值

- 普通用户的面试过程开始真正消费新题库，而不是只有管理端可维护、运行时不生效。
- 追问可以围绕当前回答和题库上下文做更贴近真实技术面试的深挖或补救。
- 管理端建设的 `published / vector_status / question_role / 开放组合` 这些治理规则开始进入用户主链路，形成闭环。
- 启动面试前，用户能看到并填写与本场训练直接相关的准备信息，而不只是选一个轨道就开始。
- 启动面试前，用户既能使用本场准备参数，也能把长期有价值的背景摘要沉淀到个人档案，减少重复输入。
- 面试结束后，报告页能展示“为什么这样追问 / 有没有命中新题库上下文 / 是否发生了回退”这类解释性摘要。

## 已确认事实

- 已完成题库基础设施任务：
  - `interview-bank-version-storage`
  - `interview-bank-admin-mvp`
  - `interview-bank-vector-reindex`
- 当前项目已有：
  - `InterviewKnowledgeAtom` 主表、版本表、批次表和向量文档表
  - 管理端题库导入、发布、归档、恢复、索引状态治理
  - `POST /api/v1/admin/interview-bank/index/rebuild`
- 当前运行时现状：
  - 创建面试会话时仍通过 `Store.FindInterviewQuestion(domain, difficulty, question_type)` 读取旧 `InterviewQuestion`
  - `backend/internal/httpapi/handlers_interviews.go` 的 launchpad 仍返回兼容种子轨道，`fallback_mode = true`
  - `frontend/src/features/interviews/InterviewsPage.tsx` 当前只有轨道选择和启动动作，没有真正的准备参数输入区
  - `frontend/src/features/interviews/InterviewReportPage.tsx` 当前只展示分数、轮次、评语和作答记录，没有检索摘要或追问依据说明
  - `UserProfile` 当前只有 `target_level` 和 `preferred_domains`
  - `frontend/src/features/profile/ProfilePage.tsx` 当前也只维护目标职级和偏好专业域，没有简历摘要 / 项目经历摘要等持久化字段
  - `backend/internal/agent/interview.go` 运行步骤仍是：
    - `analyze_answer_intent`
    - `evaluate_dimensions`
    - `decide_follow_up`
    - `generate_feedback`
    - `safety_rewrite`
  - 当前并没有“检索题库知识原子 -> 选择追问策略 -> 记录检索证据”的运行时步骤
- 已确认的产品路线来自 `docs/ai-interview-integration-prd.md` 和 `docs/ai-interview-integration-tech-design.md`：
  - 首期新题库应进入开场题与追问主链路
  - 索引失败时必须降级：开场题普通筛选仍可用，追问检索回退到现有规则链路
  - 会话创建时必须保存开场题快照
  - 每轮实际命中的追问知识原子需保存轻量快照

## 初始范围

### 后端

- 用新题库 `published` 记录替代旧 `InterviewQuestion` 作为主链路开场题来源。
- 让面试会话保存开场题快照，并在运行时保留与旧题兜底兼容边界。
- 在面试 Agent/运行时中加入题库检索与追问策略决策步骤。
- 当索引可用时，按当前回答和上下文召回 `followup / mixed` 原子。
- 当索引不可用或召回弱时，回退现有 `decide_follow_up` 规则链路。
- 记录每轮追问命中的轻量题库快照和必要的检索摘要。
- 扩展 `users.profile` 和个人档案接口，首期只持久化 `resume_summary`、`project_summary` 两个长期背景字段。
- `focus_areas`、本场难度倾向、目标重点等仅作为会话级输入，不写入长期用户画像。
- `focus_areas`、本场难度倾向、目标重点等会话级输入首期只影响追问检索和反馈生成，不参与开场题选择。
- 会话级输入字段首期固定为 `difficulty_level`、`focus_areas[]`、`setup_notes`，不再额外新增更多会话字段。

### 前端

- 面试启动台增加首期准备区，允许用户在启动前补充与本场会话有关的准备参数。
- 个人档案页增加 `resume_summary`、`project_summary` 编辑入口，并与启动台形成读取/回填关系。
- 保持现有启动台骨架和开放组合结构，不做大范围视觉重做。
- 报告页增加首期检索/追问摘要展示，帮助用户理解追问依据与回退情况。
- 报告页对普通用户展示“聚合摘要 + 每轮的考察点 `subject` / 是否回退 / 追问类型”。
- 报告页不展示原子正文、内部检索 query、命中片段或管理端标题细节。
- 前端只展示训练和报告解释需要的最小信息，不直接暴露管理端治理明细。

### 验证

- 增加后端 API / 运行时 / store 测试，覆盖：
  - 新题库开场题选择
  - 旧 `InterviewQuestion` 兼容兜底
  - 索引失败回退
  - 追问命中轻量快照写入
- 增加前端最小验证，覆盖启动台新增准备参数流转和报告页新增摘要渲染。

## 暂不纳入本任务

- AI Mentor 独立页面
- 用户自定义模型配置
- 新的向量基础设施或 Qdrant 接入
- 大范围重做启动台 UI
- 完整的简历文件上传、PDF 解析和结构化抽取

## 初始验收标准

1. 新建面试会话优先从 `published` 的新题库中选择开场题，而不是默认走旧 `InterviewQuestion`。
2. 当新题库不满足条件时，系统仍可回退到旧 `InterviewQuestion` 完成面试启动。
3. 至少一个运行时步骤会基于当前回答和题库原子做追问辅助决策。
4. 向量索引不可用或召回失败时，追问仍可继续，不让整场面试失败。
5. 会话中能保存开场题快照，后续轮次能保存命中的轻量题库原子快照。
6. 启动台能承载本场准备参数，并参与后端追问检索或反馈决策，但不改变首期开场题选择逻辑。
7. 个人档案页能持久化 `resume_summary`、`project_summary`，并在后续启动面试时被读取复用。
8. 报告页能展示聚合摘要，以及每轮的 `subject` / 是否回退 / 追问类型，而不是只有最终分数和评语。
9. 相关后端测试、前端 lint/build 通过，现有面试链路不回归。

## 当前阻塞问题

无。
