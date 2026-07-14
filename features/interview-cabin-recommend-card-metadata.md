# 面试舱推荐卡信息密度补齐

## 目标

让推荐训练卡片在不开启额外详情页的情况下，直接展示足够的决策信息：

- 适合对象
- 预计耗时
- 题库状态

## 修改范围

- `InterviewsPage.tsx` 为推荐卡增加元信息推导与展示。
- `InterviewsPage.css` 增加推荐卡元信息布局。
- `frontend-smoke.mjs` 增加推荐卡元信息断言。

## 核心实现

- `适合对象` 复用 `launchpadConfig.ts` 里的 `interviewLevels` 映射，根据轨道难度显示对应受众。
- `预计耗时` 基于 `questionType + difficulty` 做轻量前端估算，不依赖后端统计。
- `题库状态` 复用现有 `availabilityState / vectorStatusSummary`，区分：
  - 兼容轨道
  - 追问增强准备中
  - 已索引 / 其他索引摘要

## 影响范围

- 不改后端接口。
- 不改推荐来源算法。
- 不影响推荐卡主按钮“开始训练”和次按钮“查看覆盖”。

## 验证方式

- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前预计耗时是前端估算值，不是用户真实耗时统计。
- 当前适合对象仍按难度级别映射，不是按用户个人完整画像动态生成。
