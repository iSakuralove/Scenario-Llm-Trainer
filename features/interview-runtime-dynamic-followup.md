# 面试运行时动态追问 MVP

## 目标

让技术面试运行时优先使用正式题库开场题，并在追问阶段结合本场会话输入做题库检索增强；同时把长期个人背景和会话级输入分层，避免画像被临时训练目标污染。

## 修改范围

- 后端面试会话创建、题库开场题选择、追问检索、反馈 prompt 和报告接口。
- 前端面试启动台、报告页、API 类型和个人档案摘要字段。
- MemoryStore / PostgresStore 的个人档案与面试会话字段读写。
- 后端 API / Agent 测试、前端 lint 和 build 验证。

## 核心实现

- 长期个人档案只持久化 `resume_summary` 与 `project_summary`。
- 面试会话首期只新增 `difficulty_level`、`focus_areas[]`、`setup_notes` 三个输入。
- `focus_areas[]` 固定为五维评分枚举：`technical_accuracy`、`logical_completeness`、`solution_feasibility`、`depth_breadth`、`expression_structure`。
- 开场题选择只使用轨道参数 `domain`、`difficulty`、`question_type`；优先 `published` 的 `opening/mixed` 题库原子，未命中再回退旧 `InterviewQuestion`。
- 追问阶段使用当前回答、开场题快照和三项会话输入构造检索上下文，只召回 `indexed` 的 `followup/mixed` 题库原子；不可用时回退规则追问。
- 报告接口返回聚合摘要和每轮 `subject / fallback_used / follow_up_type`，不返回原子正文、内部 query、命中片段或管理端标题细节。

## 影响范围

- 普通用户启动面试前可设置本场难度倾向、考察重点和准备备注。
- 个人档案页可保存简历摘要和项目摘要，作为后续会话备注的默认来源。
- 面试报告新增追问检索摘要面板。
- Agent trace 步骤数从 5 增加到 6，新增 `retrieve_followup_context`。

## 验证方式

- `cd backend; go test ./...`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 首期不会用 `difficulty_level`、`focus_areas[]`、`setup_notes` 影响开场题选择。
- 未接入异步索引重建触发；追问检索只使用已存在的 indexed 题库向量。
- 报告只展示检索摘要，不提供内部召回明细或原子内容溯源。
