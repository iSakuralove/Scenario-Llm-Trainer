# 前端开发缓存安全收口

## 目标

减少前端开发环境因为 Vite 依赖预编译缓存失效而出现白屏或运行时异常的概率，并把前端测试产物从 Git 工作树中隔离出去。

## 修改范围

- `frontend/package.json`
- `frontend/scripts/frontend-smoke.mjs`
- `.gitignore`

## 核心实现

- `frontend` 的开发命令从 `vite` 调整为 `vite --force`。
  - 每次启动前端开发服务时强制重新预编译依赖。
  - 避免 `node_modules/.vite` 中的旧缓存与当前依赖状态不一致，触发类似 `require_isUnsafeProperty is not a function` 的白屏问题。
- 新增 `frontend/scripts/frontend-smoke.mjs` 与 `npm --prefix frontend run smoke`。
  - 访问登录页并检查 `.auth-layout` 可见。
  - 拒绝 `pageerror` 和 `页面渲染失败` 错误边界。
  - 用 `demo / demo123` 走一遍最小登录链路，确认可进入 `/dashboard`。
  - 登录后继续检查 `/profile`、`/scenarios` 和学生访问 `/system` 被重定向回 `/dashboard`。
- 工作区根目录新增 `npm run test:frontend:smoke`，便于在不切目录的情况下执行同一条前端健康闸门。
- `.gitignore` 新增：
  - `frontend/playwright-report/`
  - `frontend/test-results/`
  - 避免前端 E2E 测试输出污染 Git 工作树。

## 影响范围

- 前端开发服务启动时会比之前多一次依赖重建，首启略慢，但能降低缓存污染导致的假性运行时故障。
- 浏览器渲染问题排查时，可以先通过清理 `frontend/node_modules/.vite` 与重启 dev server 验证是否为缓存问题。
- 后续所有前端优化都可以先跑 `npm --prefix frontend run smoke` 作为站点健康闸门。
- Playwright 运行结果不会再作为无关文件持续出现在 Git 状态中。

## 验证方式

- 重启前端开发服务后访问 `http://localhost:5173/`
- `npm --prefix frontend run smoke`
- `npm run test:frontend:smoke`
- 浏览器脚本确认：
  - 登录页 `.auth-layout` 可见
  - 无新的 `pageerror`
  - `demo / demo123` 可登录并进入 `/dashboard`

## 已知限制

- `vite --force` 只能降低缓存不一致问题，不能替代真实的前端运行时回归检查。
- 当前这轮没有继续推进高风险前端包体优化；后续如需继续，必须先过站点健康闸门。
