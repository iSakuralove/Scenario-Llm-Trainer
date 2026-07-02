# 实施计划：面试题库治理管理端 MVP

## 实施顺序

1. 读取相关规范和现有实现
   - `docs/ai-interview-integration-prd.md`
   - `docs/ai-interview-integration-tech-design.md`
   - `interview-cabin-restructure.md`
   - `docs/interview-cabin-frontend-page-design.md`
   - `.trellis/spec/backend/index.md`
   - `.trellis/spec/frontend/index.md`

2. 后端 Store 能力
   - 扩展 Store 接口，补题库列表、批次记录、摘要能力。
   - 实现 MemoryStore。
   - 实现 PostgresStore 与 migration。
   - 补充 store 单元测试和 PostgreSQL 集成测试。

3. 后端 Admin API
   - 在 `handlers_admin.go` 接入 `/admin/interview-bank/*`。
   - 实现导入校验、预览、发布、列表、批次、摘要。
   - 复用同一套导入校验逻辑。
   - 补 admin 权限、非 admin 禁止、预览不写入、发布写入版本的回归测试。

4. 前端 API 与状态
   - 在 `frontend/src/api/client.ts` 增加题库治理类型和接口。
   - 新增独立题库治理 store 或局部页面状态。
   - 避免把导入状态塞进 `systemStore`。

5. 前端页面与路由
   - 新增 admin 可见导航入口。
   - 新增 Interview Bank 页面。
   - 实现列表、筛选、导入输入、校验结果、发布结果、加载/空/错误状态。
   - `vector_status=failed` 只做筛选；不要实现真实重建索引按钮动作。
   - 保持现有前端视觉语言，不重做面试舱用户侧页面。

6. 系统状态摘要
   - 后端 `systemStatus()` 增加题库治理摘要。
   - 前端 `SystemPage` 展示摘要，不增加管理动作。

7. 文档与验证
   - 新增 `features/interview-bank-admin-mvp.md`。
   - 必要时更新 `docs/architecture.md`。
   - 运行后端测试、前端 lint/build。
   - 如本地服务可用，做 admin 页面浏览器冒烟验证。

## 验证命令

- `go test ./...` 或在 `backend/` 下运行等价测试命令。
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 高风险文件

- `backend/internal/store/interface.go`
- `backend/internal/store/memory.go`
- `backend/internal/store/postgres.go`
- `backend/internal/httpapi/handlers_admin.go`
- `frontend/src/api/client.ts`
- `frontend/src/app/AppShell.tsx`
- `frontend/src/features/system/SystemPage.tsx`

## 回滚点

- Store 接口扩展会触及 MemoryStore 和 PostgresStore，失败时应整体回退该能力，不保留半套接口。
- Admin API 路由若影响现有 `/admin/users`、`/admin/prompts`、`/admin/ai-config`，必须优先修复现有功能。
- 前端路由新增如果影响非 admin 导航或登录后默认页面，必须回退路由改动。
