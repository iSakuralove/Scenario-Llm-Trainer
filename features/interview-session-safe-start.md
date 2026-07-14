# 面试会话安全启动

## 目标

避免用户点击“开始面试”后，会话页面模块加载失败但后端会话已经写入历史记录。

## 修改范围

- 调整面试舱启动顺序：先加载会话页模块，再调用创建面试会话 API。
- 新增浏览器回归用例，模拟会话页模块网络加载失败。

## 核心实现

- `InterviewsPage` 在调用 `POST /api/v1/interviews/sessions` 前动态导入 `InterviewSessionRoute`。
- 模块加载失败时留在面试舱，展示“本次未创建面试记录”，不发送创建会话请求。
- 模块加载成功后继续复用现有创建、题目匹配校验和导航逻辑。

## 影响范围

- 仅影响新面试启动时序，不改变继续面试、提交回答、评分报告或历史删除接口。
- 不新增后端接口和数据库结构。

## 验证方式

- `npm --prefix frontend run test:e2e -- interviews-launchpad.spec.ts`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`

## 已知限制

- 已创建会话后的浏览器崩溃、断电或用户主动关闭标签页仍会保留可继续的历史会话，这是正常的持久化行为。
- 开发服务器运行期间不应删除 `frontend/node_modules/.vite`；如需清理，应先停止 Vite，再清理并重新启动。
