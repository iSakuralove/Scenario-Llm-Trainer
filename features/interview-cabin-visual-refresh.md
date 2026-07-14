# 面试舱前端页面美化方案

## 背景

面试舱启动台（`/interviews`）当前已有一套深色"命令屏"设计（青绿信号色 + 深色渐变 + 细网格），但整屏观感偏"糙"：视觉层级平、信号色滥用、卡片规格不统一、背景装饰过重、字重过猛。本方案在**不改动信息架构、不改后端接口、不改交互逻辑**的前提下，对视觉表现做系统性收敛与提升。

本轮只做 CSS 与少量 JSX class/结构微调，保留现有 `launchpadConfig.ts`、`InterviewsPage.tsx` 的数据流与状态机。

## 现状问题诊断

| 编号 | 问题 | 具体表现 | 位置 |
| --- | --- | --- | --- |
| P1 | 视觉层级偏平 | Hero、命令网格、历史面板都用几乎相同的深色面板 + 青绿边框，缺少主次对比，整屏一个色调到底 | `InterviewsPage.css` 全局 |
| P2 | 青绿信号色滥用 | 标题、边框、标签、按钮、图标全是 `--teal`，信号色失去"信号"意义，主操作（开始面试）反而不够突出 | 全局 |
| P3 | 卡片规格不统一 | `track-card`(13px)、`domain-chip`(12px)、`history-item`(13px)、面板(18px) 圆角/内边距/阴影各不相同，拼一起显杂乱 | `.track-card` / `.domain-chip` / `.interview-history-item` |
| P4 | 背景装饰过重 | 细网格 + 两层 radial 光晕 + 渐变四层叠加，深底上显脏、有噪点感 | `.interview-launchpad::before` |
| P5 | 字重/字号过猛 | 多处 `font-weight: 850`，主标题 `clamp(30px,3vw,42px)`，挤压感强、缺少呼吸 | `.interview-title-line h1` 等 |
| P6 | 状态徽标语义弱 | 可训练/增强中/兼容轨道三态色差小，用户难一眼区分 | `.track-availability` |

## 设计原则

沿用 `docs/interview-cabin-frontend-page-design.md` 第 2.3 节的设计系统约束，本轮进一步收敛：

1. **一套令牌，统一规格**：把散落的圆角、间距、阴影、边框色收敛为 CSS 变量（`--r-card`、`--r-panel`、`--pad-panel`、`--shadow-panel` 等），消除"每个卡片一个规格"。
2. **信号色克制**：青绿只保留给"可操作/已选中/主按钮"三类语义；面板边框、次要文本、装饰改用中性冷灰蓝，让青绿重新成为"信号"。
3. **层级三档**：背景 < 面板 < 焦点卡（开始面试）。焦点卡用更亮的表面 + 明确阴影抬升，其余面板压平、减阴影。
4. **背景做减法**：网格降到极弱或去除，光晕收成单层、低透明度，避免噪脏。
5. **字重回归**：标题字重降到 750-800，正文 500，主标题字号收到 `clamp(26px,2.4vw,34px)`，增加行高与留白。
6. **状态色分明**：可训练=青绿、增强中=天蓝、兼容/降级=琥珀，三态拉开明度与色相差。

## 修改范围

- **主要**：`frontend/src/features/interviews/InterviewsPage.css` — 令牌收敛 + 各模块样式重写。
- **次要**：`frontend/src/features/interviews/InterviewsPage.tsx` — 仅在需要挂钩新样式时补充/调整 className 与包裹元素，不动逻辑、状态、数据流。
- **不改**：`launchpadConfig.ts`、`api/client.ts`、后端、任何交互行为与文案含义（文案措辞如需微调会单列）。

## 分模块方案

### M1 设计令牌层（新增，`.interview-launchpad` 作用域）

在 `.interview-launchpad` 根上补充统一令牌：

- 圆角：`--r-panel: 16px`、`--r-card: 12px`、`--r-chip: 10px`、`--r-pill: 999px`
- 间距：`--pad-panel: 22px`、`--pad-card: 16px`、`--gap-grid: 18px`
- 表面三档：`--surface-1`（背景）、`--surface-2`（面板）、`--surface-3`（焦点卡，更亮）
- 边框：`--line-soft`（中性冷灰蓝，替换大量青绿边框）、`--line-teal`（仅焦点/选中）
- 阴影：`--shadow-panel`（弱）、`--shadow-focus`（焦点卡抬升）
- 文本：`--ink-1`（标题）、`--ink-2`（正文）、`--ink-3`（次要）
- 信号：保留 `--teal` / `--teal-soft`，新增 `--sky`（增强中）、`--amber`（降级）

### M2 背景做减法（P4）

- 移除或将细网格透明度降到 `0.012` 以下（近乎不可见）。
- radial 光晕收成单层，透明度降到 `0.10` 左右，位置固定右上。
- 底色改用更干净的双色纵向渐变，去掉噪点观感。

### M3 顶部 Hero（P1/P5）

- 主标题字重 800、字号收敛，副标题行高提到 1.6，颜色用 `--ink-3`。
- 标题图标块弱化：去掉重 inset 光晕，改为柔和青绿底 + 细边。
- 状态徽标（题库已连接/兼容模式）三态色对齐 M6 语义色。

### M4 开始面试焦点卡（P1/P2）

这是全页第一操作，作为唯一"焦点卡"处理：

- 用 `--surface-3` 更亮表面 + `--shadow-focus` 抬升，与其余面板拉开层级。
- 主按钮"开始面试"保留青绿实心 + 阴影，是全页唯一高饱和主按钮。
- 五维评分标签、难度分段控件收敛为统一 chip 规格；segmented control 选中态用青绿，未选中用中性表面。
- 三个数据事实块（可面试题目/可启动组合/预计用时）统一规格，数字用 tabular-nums。

### M5 训练轨道 / 专业领域卡片（P3）

- `track-card`、`domain-chip` 统一到 `--r-card` / `--pad-card` / `--line-soft`。
- 未选中卡片：中性表面 + 中性边框，hover 才浮现青绿；选中态青绿边框 + 左侧强调条。
- `track-card` 左侧强调条只在 hover/active 显示青绿，默认中性，减少"全是绿条"的杂感。
- 领域 chip 的"已收录不可启动"态用低透明度 + 中性色，和"可启动"态明确区分。

### M6 状态徽标语义色（P6）

- `available`（可训练）：青绿。
- `indexing`（增强中）：天蓝 `--sky`。
- `fallback`（兼容轨道/降级）：琥珀 `--amber`。
- 三态统一 pill 规格，只有色相/明度不同，保证一眼可辨。

### M7 历史面试面板（P3）

- item 圆角、内边距对齐令牌。
- 指标 pill（轮次/分数）与状态标签统一规格。
- 主/次/删除按钮层级：继续面试=青绿主按钮，题目=中性 ghost，删除=玫红 ghost（仅 hover 显色）。

### M8 骨架屏与空状态

- 骨架块圆角、底色对齐令牌，脉冲动画保留。
- 空状态虚线框颜色改用 `--line-soft`，文案居左、行高 1.6。

## 验证方式

1. `cd frontend; npm run lint` — 无新增 lint 错误。
2. `cd frontend; npm run build` — tsc + vite 构建通过。
3. 本地 `http://127.0.0.1:5173/interviews` 目视核对：
   - 三档层级明确（背景 < 面板 < 开始面试焦点卡）。
   - 青绿只出现在可操作/选中/主按钮，背景与次要文本为中性冷灰。
   - 三态状态徽标一眼可辨（青绿/天蓝/琥珀）。
   - 卡片圆角、内边距、阴影视觉统一。
4. `cd frontend; npm run e2e -- interviews-launchpad` — 现有 Playwright 用例仍通过（class 改名需同步 selector；优先保留 `data-testid`）。
5. 响应式：1280px / 760px / 520px 三个断点无错位。

## 风险与约束

- **E2E 依赖**：`frontend/e2e/interviews-launchpad.spec.ts` 可能按 class 选择元素。改 class 前先核对该文件，优先通过 `data-testid` 定位，避免误伤用例。
- **不改交互**：本轮纯视觉，若发现需要调整 DOM 结构才能实现的层级，会限定在"包一层容器"级别，不动状态机与数据流。
- **深色系统一致性**：新令牌值需与全局 `App.css` 侧边栏、`index.css` 的冷色调协调，避免面试舱与其余页面割裂。
- **可访问性**：文本对比度需 ≥ 4.5:1（正文）/ 3:1（大字），青绿主按钮上的深色文字保持现有高对比。

## 落地步骤

1. Phase A：在 `.interview-launchpad` 作用域落地 M1 令牌 + M2 背景，先把地基铺好。
2. Phase B：改 M3 Hero、M4 焦点卡，确立三档层级与信号色克制。
3. Phase C：改 M5 卡片、M6 状态徽标、M7 历史、M8 骨架/空态，统一规格。
4. Phase D：lint + build + e2e + 三断点目视核对，收口。

## 暂不纳入

- 信息架构调整、模块增删（属于 `interview-cabin-*` 系列既有 feature）。
- 组件拆分（`InterviewLaunchpadSummary` 等，属页面设计文档第 11 节）。
- 动效/微交互大改（仅保留现有骨架脉冲与 hover 过渡）。
- 浅色主题适配。
