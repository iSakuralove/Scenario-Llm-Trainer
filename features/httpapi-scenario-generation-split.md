# httpapi 场景生成拆分

## 目标

继续收敛 `backend/internal/httpapi/server.go` 的职责，把场景生成、异步 AI job、取消和进度事件相关逻辑拆到独立文件，降低后续维护和审查成本。

## 修改范围

- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/scenario_generation.go`
- `review/PROGRESS.md`

## 核心实现

- 新增 `scenario_generation.go`，承载场景生成 payload、约束归一化、参数校验、重复场景检查、AI job 创建和执行、取消处理、job 事件输出与相关审计辅助。
- 保留 `httpapi` 同包拆分方式，不改变现有路由、函数可见性和业务调用路径。
- 将 `allowedScenarioDifficulties`、`allowedScenarioTypes` 与场景生成归一化逻辑一起迁移，避免在 `server.go` 顶部留下只服务场景生成的孤立状态。
- `server.go` 继续保留路由分发和 handler 入口，场景生成细节由新文件承载。

## 影响范围

- `/api/v1/scenarios/generate`
- `/api/v1/scenarios/generate/jobs`
- `/api/v1/ai/jobs/{id}`
- `/api/v1/ai/jobs/{id}/events`
- `/api/v1/ai/jobs/{id}/cancel`

本次属于同包机械拆分，接口请求和响应结构不应变化。

## 验证方式

已运行：

```powershell
cd backend
go test ./internal/httpapi
go test ./...
```

结果：全部通过。

## 已知限制

- CodeGraph 显示场景生成校验和异步任务函数缺少专门覆盖测试；当前验证主要依赖现有 `httpapi` 包测试和全量后端测试。
- `scenario_generation.go` 仍包含较多职责，后续如继续细拆，可再按同步生成、异步 job、事件输出和审计辅助分组。
