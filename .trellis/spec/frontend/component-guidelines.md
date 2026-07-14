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
- 390px 下锚点导航必须折叠，不得产生页面级横向滚动。
