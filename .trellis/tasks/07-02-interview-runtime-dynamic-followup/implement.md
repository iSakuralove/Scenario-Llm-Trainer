# 实施计划：面试运行时动态追问 MVP

## 实施顺序

1. 个人档案与类型基础
   - 扩展 `UserProfile` 的 `resume_summary`、`project_summary`
   - 扩展 `/users/me/profile` 请求/响应
   - 更新 `ProfilePage.tsx` 与前端类型

2. 会话级输入与启动台
   - 扩展创建面试会话请求：
     - `difficulty_level`
     - `focus_areas[]`
     - `setup_notes`
   - 在 `InterviewsPage.tsx` 增加首期准备区
   - 保持现有轨道选择不变

3. 新题库开场题接入
   - 在 store / httpapi 中新增从 `InterviewKnowledgeAtom` 选择开场题的逻辑
   - 写入 `question_snapshot`
   - 兼容回退到旧 `InterviewQuestion`
   - 更新 launchpad / create session 响应语义

4. 动态追问链路
   - 在 `backend/internal/agent/interview.go` 中插入题库检索与策略步骤
   - 基于会话级输入增强 query
   - 索引失败或召回弱时回退现有规则追问
   - 保存轻量原子快照和每轮摘要字段

5. 报告页增强
   - 扩展报告接口结构
   - 在 `InterviewReportPage.tsx` 增加聚合摘要和逐轮摘要
   - 确保不展示版本号、原子正文、内部 query

6. 测试与文档
   - 后端 API / 运行时 / store 回归测试
   - 前端 lint/build
   - 更新 `docs/architecture.md`
   - 新增 `features/interview-runtime-dynamic-followup.md`
   - 需要时更新 `.trellis/spec/backend/store-schema-contracts.md`

## 验证命令

- `go test ./...`（在 `backend/` 目录）
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 高风险文件

- `backend/internal/domain/types.go`
- `backend/internal/httpapi/handlers_auth.go`
- `backend/internal/httpapi/handlers_interviews.go`
- `backend/internal/agent/interview.go`
- `backend/internal/store/interface.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/store/schema.go`
- `backend/migrations/001_schema.sql`
- `frontend/src/api/client.ts`
- `frontend/src/types/index.ts`
- `frontend/src/features/interviews/InterviewsPage.tsx`
- `frontend/src/features/interviews/InterviewReportPage.tsx`
- `frontend/src/features/profile/ProfilePage.tsx`

## 回滚点

- 如果新题库开场题接入不稳定，先保留旧 `InterviewQuestion` 启动主链路，只提交会话级输入与报告摘要基础。
- 如果动态检索步骤导致评分链路不稳定，先保留追问回退逻辑并关闭题库检索注入。
- 如果前端准备区交互风险过大，可先保留最小字段输入，不做复杂联动。

## 已确认实施约束

- `resume_summary`、`project_summary` 进入长期个人档案
- `difficulty_level`、`focus_areas[]`、`setup_notes` 只属于会话级输入
- `focus_areas[]` 固定对齐五维评分方向
- 会话级输入只影响追问检索和反馈生成，不参与首期开场题选择
- 报告页展示“聚合摘要 + 每轮的 subject / 是否回退 / 追问类型”
- 不展示原子正文、内部 query、命中片段、管理端标题细节或题库版本号

## 当前实现状态

- 已完成个人档案 `resume_summary`、`project_summary` 的前后端持久化。
- 已完成会话级 `difficulty_level`、`focus_areas[]`、`setup_notes` 创建、保存和启动台输入。
- 已完成正式题库开场题优先选择，并保留旧 `InterviewQuestion` 兜底。
- 已完成 Agent 追问检索步骤、规则回退、每轮追问元数据落盘。
- 已完成报告页聚合摘要与逐轮 `subject / fallback_used / follow_up_type` 展示。
- 已完成后端 `go test ./...`、前端 lint 和 build 验证。
