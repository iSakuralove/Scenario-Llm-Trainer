# 面试舱首屏信息收口

## 目标

让面试舱首屏优先服务真实用户动作：直接开始面试、查看可面试题量、查看历史面试，减少大面积空白和说明型内容。

## 修改范围

- `frontend/src/features/interviews/InterviewsPage.tsx`
- `frontend/src/features/interviews/InterviewsPage.css`
- `frontend/e2e/interviews-launchpad.spec.ts`
- `frontend/scripts/frontend-smoke.mjs`

## 核心实现

- 删除海报式英文标牌、大号 `INTERVIEW` 文案和下方说明型面板。
- 将历史面试移动到首屏，与本轮配置并排展示。
- 将启动台状态指标改为优先展示可面试题目、可启动组合和已索引题目。
- 保留推荐训练、覆盖摘要、训练覆盖率、可启动轨道和专业领域筛选这些可操作区域。

## 影响范围

- 仅调整面试舱前端展示和前端验收断言。
- 后端接口、题库数据结构和面试启动 API 不变。

## 验证方式

- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`
- `npx playwright test frontend/e2e/interviews-launchpad.spec.ts`

## 已知限制

- 当前改动不改变题库数量计算逻辑，只调整已有 `launchpad` 汇总字段的展示优先级。
