# 面试舱前端页面方案收口

## 目标

将面试舱前端页面设计收口到首期已确认的开放组合，并同步源码里的启动台文案，避免页面设计、启动配置和实际展示范围继续分叉。

## 修改范围

- 收口 `docs/interview-cabin-frontend-page-design.md`。
- 收口 `frontend/src/features/interviews/launchpadConfig.ts`。
- 同步 `frontend/src/features/interviews/InterviewsPage.tsx` 的首屏文案。

## 核心实现

- 将用户侧开放组合限定为 `java / database / cache / ai_llm` 的 L2/L3 组合。
- 去除面试启动台中旧的 L3-L5 话术，改为首期开放范围。
- 将页面设计文档从“更宽泛的探索页”收口为“题库驱动的启动台 + 能力覆盖 + 历史表现”。

## 影响范围

- 影响面试舱启动台的首屏展示、可启动组合和页面设计说明。
- 不改变后端接口，也不改数据库 schema。

## 验证方式

- 检查 `launchpadConfig.ts` 仅保留首期开放组合。
- 检查 `InterviewsPage.tsx` 首屏文案不再使用 L3-L5 全量话术。
- 检查 `docs/interview-cabin-frontend-page-design.md` 不再描述未开放组合为用户侧入口。

## 已知限制

- 这仍然是静态配置与文案收口，后端开放组合接口尚未接入。
- 未来如果开放范围变化，页面文案和配置需要跟随后端重新对齐。
