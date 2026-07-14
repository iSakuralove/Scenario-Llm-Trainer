# 面试舱分区加载态与错误态

## 目标

把面试舱启动台的状态反馈从“整页文案式”收口为更明确的分区加载态和分区错误态。

## 修改范围

- `InterviewsPage.tsx` 增加：
  - launchpad 分区加载态
  - history 分区加载态
  - launchpad 分区降级提示
  - history 分区错误态
- `InterviewsPage.css` 增加 skeleton shimmer 样式。
- `frontend-smoke.mjs` 用延迟 mock 和失败 mock 验证骨架与错误隔离。

## 核心实现

- 启动台接口加载时：
  - 状态区显示统计骨架
  - 推荐区显示卡片骨架
  - 覆盖区和训练覆盖率区显示占位骨架
  - 轨道区显示筛选条骨架和轨道卡骨架
- 历史接口加载时：
  - 历史区显示独立骨架，不阻塞启动台主区域
- launchpad 接口失败时：
  - 仍回退兼容轨道
  - 状态区额外显示明确的分区降级提示
- history 接口失败时：
  - 只在历史区显示 `读取历史面试失败：...`
  - 启动台主区域仍可正常筛选、查看覆盖和开始训练

## 影响范围

- 不改后端接口结构。
- 不改推荐来源逻辑。
- 前端 smoke 现在会验证：
  - 启动台加载骨架
  - 历史区加载骨架
  - 历史失败不影响启动台主体交互

## 验证方式

- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前推荐区、覆盖区和状态区共用同一个 launchpad 加载信号，还没有拆成多接口级别的独立加载时钟。
- 当前错误隔离主要覆盖 launchpad 与 history；如果后续把推荐拆成独立接口，需要再补推荐区独立错误态。
