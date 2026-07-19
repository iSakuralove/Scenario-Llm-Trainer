# 当前简历与面试链路

## 结论

当前系统只保存一份结构化后的简历摘要，不存在可供用户选择的多份简历实体。简历摘要也尚未进入面试会话创建或开场问题生成链路。

## 证据

- `backend/internal/domain/types.go:41`：`UserProfile` 只有 `ResumeSummary` 与 `ProjectSummary` 字符串，没有简历 ID、列表或状态。
- `backend/internal/store/schema.go:4`：用户档案整体存入 `users.profile` JSONB，没有独立简历表。
- `backend/internal/httpapi/profile_import.go:21`：导入接口只读取一个 `file`，解析后直接覆盖 `profile.ResumeSummary`，再保存整个档案。
- `frontend/src/features/profile/ProfilePage.tsx:143`：页面明确提示导入会覆盖“简历摘要”，文件输入只读取 `files[0]`。
- `frontend/src/api/client.ts:730`：前端导入请求只提交一个文件。
- `backend/internal/httpapi/handlers_interviews.go:21`：创建会话只接收题目与面试设置，不接收简历标识或摘要。
- `frontend/src/features/interviews/InterviewsPage.tsx:222`：启动面试只提交所选题目和面试设置。
- `backend/internal/httpapi/handlers_interviews.go:579`：启动台个性化目前只读取 `PreferredDomains`。

## 对本任务的影响

若本期支持多份简历选择，需要新增简历存储、单份导入与状态管理、选择接口，以及把所选简历接入面试上下文；这不只是启动页视觉调整。
