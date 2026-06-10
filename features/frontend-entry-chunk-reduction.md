# 前端入口体积收缩

## 目标

在不破坏当前站点渲染与登录链路的前提下，先缩小未登录首页的主入口体积，让登录页不再提前加载完整工作台代码。

## 修改范围

- `frontend/src/app/App.tsx`
- `frontend/src/app/AppShell.tsx`

## 核心实现

- `AppShell` 从同步导入改为 `React.lazy` 动态加载。
- `AuthPage` 继续保留为同步加载，确保默认登录页首屏稳定可见。
- `AppShell` 外层增加最小 `Suspense` fallback，仅在已登录用户进入工作台时短暂显示“正在加载工作台...”。
- `AppShell` 内部只对非默认路由页面做懒加载：
  - `ScenariosPage`
  - `ScenarioSessionPage`
  - `ScenarioReviewPage`
  - `InterviewsPage`
  - `InterviewSessionRoute`
  - `InterviewReportPage`
  - `CommunityPage`
  - `ProfilePage`
  - `SystemPage`
- `DashboardPage` 继续保持同步加载，保证登录后默认落点稳定。
- `AppShell` 的 `Routes` 外层增加统一 `Suspense` fallback，用于非默认页面切换。

## 影响范围

- 未登录首页主入口不再提前打进整套工作台与仪表盘代码。
- 登录后会先加载工作台 chunk，再进入 `/dashboard`。
- 登录后切换到非默认页面时，会短暂显示统一的“加载页面...”占位。
- 当前优化优先保护登录页首屏和站点稳定性，不追求一次性把所有大 chunk 都拆完。

## 验证方式

- `pnpm test`
- `npm --prefix frontend run smoke`
- `pnpm --dir frontend exec playwright test e2e/login.spec.ts e2e/auth-refresh.spec.ts --project chromium`
- 观察 `vite build` 输出：
  - 登录页主入口 chunk 从接近 `1 MB` 降到约 `258 kB`
  - `AppShell` chunk 收缩到约 `16 kB`
  - 仍保留图形/图表相关大 chunk，最大约 `593 kB`

## 已知限制

- 当前仍存在大于 500 kB 的工作台 chunk 和图形相关 chunk，后续还需要继续拆分。
- `MarkdownPreview`、`MermaidRenderer`、`recharts`、`cytoscape` 相关重量依赖仍是主要体积来源。
- 后续若继续优化，必须先通过 `npm --prefix frontend run smoke`，确认没有白屏、错误边界或登录回归。
