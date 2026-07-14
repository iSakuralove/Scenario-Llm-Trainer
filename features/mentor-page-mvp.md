# 独立 AI Mentor 页面

## 目标

补齐当前仓库缺失的独立 `AI Mentor` 页面，让用户可以在单独路由集中查看综合诊断、风险预警、行动建议和知识覆盖，而不必只从仪表盘或面试报告碎片里拼信息。

## 修改范围

- 新增 [MentorPage.tsx](/G:/计算机设计大赛/frontend/src/features/mentor/MentorPage.tsx) 和 [MentorPage.css](/G:/计算机设计大赛/frontend/src/features/mentor/MentorPage.css)
- 在 [AppShell.tsx](/G:/计算机设计大赛/frontend/src/app/AppShell.tsx) 增加 `/mentor` 路由和侧边导航入口
- 新增 `GET /api/v1/users/me/mentor` 聚合接口与前端 client
- 在 [frontend-smoke.mjs](/G:/计算机设计大赛/frontend/scripts/frontend-smoke.mjs) 增加 Mentor 页访问和四区块断言

## 核心实现

- 首版新增了轻量后端聚合接口 `GET /users/me/mentor`，它内部仍然复用：
  - `learningPlan()`
  - `interviewLaunchpad()`
- 页面包含四个核心区块：
  - `综合诊断`
  - `风险预警`
  - `建议行动`
  - `知识覆盖`
- 综合诊断基于当前学习计划摘要和 `domain_insights` 派生优势/待提升方向。
- 风险预警会聚焦：
  - 兼容轨道 / 追问增强降级
  - 开放轨道覆盖率偏低
  - 简历/项目摘要缺失
- 建议行动复用现有学习计划推荐，不新增第二套建议来源。
- 知识覆盖复用 launchpad 的 `coverage_stats`，展示高频知识点和待补方向。
- 即使还没有面试样本，页面也不会崩溃，而是显示空态提示，同时四个区块仍可渲染。

## 影响范围

- 新增了一个普通用户可见的 `/mentor` 路由。
- 新增了一个普通用户可访问的 Mentor 聚合接口。
- 不接 LLM Provider 配置，不新增后端 `mentor-insight` API。

## 验证方式

- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前 Mentor 页虽然已有后端聚合接口，但仍然不是 LLM 驱动的个性化洞察接口。
- 知识覆盖仍然复用 launchpad 统计视图，而不是独立知识覆盖存储。
