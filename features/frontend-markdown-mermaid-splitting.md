# 前端 Markdown 与 Mermaid 依赖拆分

## 目标

继续收缩默认入口和常见页面的前端体积，把 Markdown 与 Mermaid 相关重依赖从公共路径中剥离出去，同时保证场景会话、面试作答和 Mermaid 查看器的运行态不回归。

## 修改范围

- `frontend/src/components/common/index.tsx`
- `frontend/src/features/interviews/InterviewSessionPage.tsx`
- `frontend/src/features/scenarios/ScenarioSessionPage.tsx`
- `frontend/src/components/common/MarkdownPreview.tsx`
- `frontend/src/components/common/MermaidLoading.tsx`
- `frontend/e2e/mermaid-viewer.spec.ts`
- `frontend/e2e/scenario-session-workspace.spec.ts`
- `frontend/e2e/student-core.spec.ts`

## 核心实现

- `MarkdownPreview` 和 `MarkdownComposer` 不再通过 `components/common/index.tsx` 这个公共 barrel export 暴露。
- `InterviewSessionPage` 和 `ScenarioSessionPage` 改为直接引用具体文件，避免把 Markdown 相关重依赖挂到更宽的公共 chunk 上。
- `MarkdownPreview` 内部把 `MermaidRenderer` 改成 `React.lazy` 按需加载。
- `ScenarioSessionPage` 的题目快照 Mermaid 也改为按需加载。
- 新增轻量 fallback 组件 `MermaidLoading`，在 Mermaid 组件异步加载阶段保持稳定的加载态。
- Mermaid 相关 E2E 不再依赖 `/scenarios` 列表页点击“开始排查”，而是直接进入会话页，避免假登录 token 与首页自动请求之间的副作用污染测试目标。

## 影响范围

- 登录页与公共基础 chunk 不再提前携带 Markdown/Mermaid 相关重依赖。
- `MarkdownPreview` 与 `MermaidRenderer` 被拆成更明确的异步 chunk。
- 场景会话和面试作答页在首次进入 Mermaid/Markdown 预览链路时会先显示轻量加载态。
- Mermaid 查看器、无效 Mermaid 回退、会话工作区和 Markdown 预览相关交互保持可用。

## 验证方式

- `npm --prefix frontend run smoke`
- `pnpm --dir frontend exec playwright test e2e/mermaid-viewer.spec.ts --project chromium`
- `pnpm --dir frontend exec playwright test e2e/student-core.spec.ts --grep "invalid mermaid" --project chromium`
- `pnpm --dir frontend exec playwright test e2e/scenario-session-workspace.spec.ts e2e/rich-interview-and-tags.spec.ts --project chromium`
- `pnpm test`
- 对比 `vite build` 输出：
  - `MarkdownPreview` 独立为约 `160 kB`
  - `MermaidRenderer` 独立为约 `37 kB`
  - 默认入口 `index` 继续压到约 `248 kB`

## 已知限制

- 当前仍有一个约 `593 kB` 的图形相关 chunk，以及 `cytoscape` 约 `435 kB` 的 chunk，没有完全拆完。
- Mermaid 与 Markdown 相关页面虽然已分块，但其内部第三方依赖仍较重，后续若继续优化，需要继续依赖烟测和 Mermaid 专项 E2E 做门禁。
