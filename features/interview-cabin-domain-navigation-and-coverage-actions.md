# 面试舱领域导航与覆盖查看交互

## 目标

补齐面试舱启动台两个前端交互缺口：

- `Domain` 作为主导航过滤入口
- 推荐卡提供 `查看覆盖` 次按钮

## 修改范围

- `InterviewsPage.tsx` 复用现有筛选状态实现领域导航过滤。
- 推荐卡从单一整卡点击改为“主内容选择 + 次按钮查看覆盖”。
- `InterviewsPage.css` 增加领域激活态和推荐卡动作区样式。
- `frontend-smoke.mjs` 增加启动台交互断言，并对 launchpad 接口做局部 mock。

## 核心实现

- 没有新造第二套筛选状态，直接复用已有 `trackFilters.category` 作为领域主导航驱动值。
- 点击 `Domain chip` 时：
  - 若当前未激活，则写入对应领域并选中该领域下首个可见轨道
  - 若当前已激活，则取消领域过滤回到“全部”
- 推荐卡新增 `查看覆盖` 按钮：
  - 点击后会把筛选切到该推荐轨道所属领域
  - 同步选中该轨道
  - 滚动到覆盖摘要区域形成明确反馈
- smoke 为了稳定验证，不依赖本地后端当前题库状态，而是只对 `/api/v1/interviews/launchpad` 做定向 mock。

## 影响范围

- 不改后端接口结构。
- 不改开始面试主流程。
- 不影响已有难度/题目角色/标签筛选，只是在其上叠加领域导航入口。

## 验证方式

- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前“查看覆盖”只会聚焦现有覆盖摘要和轨道筛选，不会打开独立覆盖详情页。
- 当前领域导航仍然通过 `category` 过滤状态承载，后续如果 `domain` 与 `category` 彻底分离，需要再拆出独立筛选字段。
