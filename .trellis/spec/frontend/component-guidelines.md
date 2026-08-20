# Component Guidelines

> How components are built in this project.

---

## Overview

<!--
Document your project's component conventions here.

Questions to answer:
- What component patterns do you use?
- How are props defined?
- How do you handle composition?
- What accessibility standards apply?
-->

(To be filled by the team)

---

## Component Structure

<!-- Standard structure of a component file -->

(To be filled by the team)

---

## Props Conventions

<!-- How props should be defined and typed -->

(To be filled by the team)

---

## Styling Patterns

<!-- How styles are applied (CSS modules, styled-components, Tailwind, etc.) -->

(To be filled by the team)

---

## Accessibility

<!-- A11y requirements and patterns -->

(To be filled by the team)

---

## Common Mistakes

<!-- Component-related mistakes your team has made -->

(To be filled by the team)

### Interview conversation timeline

- 面试会话 UI 必须从 `InterviewSession.submissions` 与 `evaluations` 构造历史消息，不允许只渲染当前题目而隐藏上一轮回答。
- 消息节点使用稳定 id：`round-{n}-question`、`round-{n}-answer`、`round-{n}-followup`，供右侧导航和 URL hash 复用。
- 默认回答区保持轻量文本输入；Markdown、代码块和 Mermaid 工具栏应按需展开，避免首屏被编辑器占满。
- AI 与用户消息同时使用头像、左右位置和文字标签区分，不能只依赖颜色。
- 文字标签可以省略成头像上的 `aria-label`，但**不能连 `aria-label` 一起省**。排查工坊
  (`features/scenarios/agentrun/AgentRun.tsx`) 删掉了可见的「你 / 导师」标签，代价是头像必须
  带 `role="img"` + `aria-label`，图标本身 `aria-hidden`。约定的实质是"不只依赖颜色"，
  头像形状 + 左右位置 + 无障碍标签三者共同满足；纯靠配色区分角色仍然不允许。
- 390px 下锚点导航必须折叠，不得产生页面级横向滚动。

### Interview history actions

- `/interviews` 历史列表只展示最近记录时，批量删除必须先通过 `api.history` 重新读取全量 `interviews`，不能只操作当前渲染的切片。
- 清空历史继续复用 `api.deleteInterviewSession`；除非后端提供批量删除接口，不要新增前端专用协议。
- 批量删除进行中必须禁用单条删除入口，避免同一会话被并发删除两次。

### Interview launchpad v2

- `/interviews` uses one shared page for `free / role / resume` modes; switching modes must preserve each mode's selection and the shared interview settings.
- Free-mode cards render one real opening question, stable code, domain, difficulty, and question type. Unselected cards have no top-right action copy; selected cards show only a check mark.
- The start button stays disabled until the current mode has a valid selection. Card clicks select only; they never create sessions.
- Desktop keeps common settings in one compact bar. At `<= 760px`, show one settings summary plus `调整`; the dialog contains the common settings and the current mode's advanced settings.
- Role mode keeps a search field above the role list and matches both role names and controlled technical scopes.
- Resume mode separates preview state from interview-selection checkboxes. Desktop uses a bounded `260px + 1fr` document workspace; mobile uses a document select and one scrollable reader.
- `html/body` must not impose a `320px` minimum width; 320px viewport plus browser zoom must not create page-level horizontal scrolling.
- When a successful launchpad response contains only an older incompatible track shape, render the five startable local fallback questions with a neutral update notice instead of an empty page.
