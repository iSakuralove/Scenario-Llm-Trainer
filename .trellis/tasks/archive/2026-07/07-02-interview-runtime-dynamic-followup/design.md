# 设计：面试运行时动态追问 MVP

## 架构边界

本任务在现有 Go + React + PostgreSQL 单体内完成，不引入新的服务进程、消息队列、Qdrant 或独立画像系统。

后端边界：

- 面试运行时继续挂在 `backend/internal/httpapi/handlers_interviews.go`。
- 追问决策仍由 `backend/internal/agent/interview.go` 驱动，但要插入题库检索与策略决策步骤。
- 会话级准备参数仍保存在 `interview_sessions`，不单独新建 `interview_setup` 表。
- 个人长期背景仅扩展 `users.profile`，首期只增加 `resume_summary`、`project_summary`。
- 开场题选择优先来自 `InterviewKnowledgeAtom`，旧 `InterviewQuestion` 保留兼容兜底。

前端边界：

- 启动台继续使用 `InterviewsPage.tsx`，只加首期准备区，不重做整页结构。
- 个人档案继续使用 `ProfilePage.tsx`，扩展持久化字段。
- 报告页继续使用 `InterviewReportPage.tsx`，新增检索/追问摘要区块，不暴露管理端治理细节。

## 数据模型

### `UserProfile`

新增长期字段：

- `resume_summary string`
- `project_summary string`

用途：

- 作为启动面试时的默认背景摘要来源
- 让后续会话无需重复输入长期背景

### `InterviewSession`

在现有结构基础上增加会话级字段：

- `DifficultyLevel string`
- `FocusAreas []string`
- `SetupNotes string`

约束：

- `difficulty_level`、`focus_areas[]`、`setup_notes` 仅属于本场会话
- 不写回 `UserProfile`
- 不参与首期开场题选择
- 只影响追问检索与反馈生成
- 首期不再额外增加其他会话字段；报告摘要从每轮 evaluation 的追问元数据聚合得到

### `InterviewEvaluation`

建议增加每轮摘要字段：

- `FollowUpSubject string`
- `FallbackUsed bool`
- `RetrievedSubjects []string`

作用：

- 报告页可以按轮展示 `subject / 是否回退 / 追问类型`
- 不暴露原子正文、内部 query 或命中片段

### 会话级输入固定枚举

`focus_areas[]` 固定为：

- `technical_accuracy`
- `logical_completeness`
- `solution_feasibility`
- `depth_breadth`
- `expression_structure`

## 开场题选择

首期开场题选择规则：

1. 从 `InterviewKnowledgeAtom` 中筛选：
   - `status = published`
   - `question_role in (opening, mixed)`
   - `domain = 请求 domain`
   - `difficulty = 请求 difficulty`
2. 优先使用已开放组合中的题目
3. 若筛选为空，则回退到旧 `Store.FindInterviewQuestion(domain, difficulty, question_type)`

首期明确不做：

- `difficulty_level`、`focus_areas[]`、`setup_notes` 参与开场题排序或筛选
- 基于简历摘要/项目摘要的开场题个性化推荐

## 追问检索与回退

运行时在原有 `decide_follow_up` 之前增加题库检索/策略步骤：

1. 根据：
   - 开场题快照
   - 当前回答
   - 会话级输入：`difficulty_level`、`focus_areas[]`、`setup_notes`
   - 已命中过的原子
2. 构造检索 query
3. 召回 `followup / mixed` 且 `vector_status = indexed` 的题库原子
4. 生成策略：
   - `deepen`
   - `remedial`
   - `switch_topic`
   - `fallback_rule_only`
5. 把精选原子的轻量信息写入 session / evaluation

回退规则：

- 向量检索不可用
- 没有可用 indexed 原子
- 召回过弱或失败

则：

- `fallback_rule_only = true`
- 继续执行现有固定规则追问链路
- 面试不得失败

## 报告展示

普通用户报告页新增两层信息：

1. 聚合摘要：
   - 命中轮次数
   - 回退轮次数
   - 命中的知识点数量
2. 每轮摘要：
   - `subject`
   - `follow_up_type`
   - `fallback_used`

禁止展示：

- 原子正文
- 内部检索 query
- 命中片段
- 管理端题目标题细节
- 题库版本号

## 兼容与迁移

- `users.profile` 是 JSONB，新增 `resume_summary` / `project_summary` 不需要额外独立表，但要同步：
  - `backend/internal/domain/types.go`
  - `frontend/src/types/index.ts`
  - `ProfilePage.tsx`
  - `handlers_auth.go` / `api/client.ts`
- `interview_sessions` 已有 `question_snapshot` 和 `selected_atom_snapshots`，本任务在此基础上扩展会话级输入与检索摘要字段。
- 旧 `InterviewQuestion` 不删除，保留兼容兜底和历史会话读取。

## 风险

- 如果把会话级输入直接参与开场题选择，会把筛选和回退复杂度明显放大，首期不做。
- 如果把 `focus_areas[]` 做成自由文本，后端无法稳定用于检索增强和报告归类，首期必须固定枚举。
- 如果报告页展示过多检索细节，会泄露治理信息和内部实现，首期只展示摘要。
- 如果把长期画像和会话级输入混存，后续学习计划与推荐会被污染，必须严格分层。
