# 面试舱聊天时间线实施计划

> **For agentic workers:** 本计划在当前会话内按步骤执行，不拆分子智能体。

**Goal:** 将面试会话页改造成可回看历史、支持锚点导航的聊天式时间线，同时保留可展开 Markdown 编辑器。

**Architecture:** 复用现有会话详情和提交接口，在前端将题目、提交、追问和评分映射为稳定的 timeline message；桌面使用主聊天流 + 右侧导航，移动端将导航折叠为顶部抽屉。保持现有 API 和状态管理，不新增数据库结构。

**Tech Stack:** React 19、TypeScript、React Router、现有 MarkdownPreview、CSS 响应式布局、Lucide 图标。

---

### Task 1: 梳理会话详情字段并建立时间线模型

**Files:**
- Modify: `frontend/src/features/interviews/InterviewSessionPage.tsx`
- Test: `frontend/e2e/interviews-session.spec.ts`（如不存在则新增）

- [ ] 定义 `TimelineMessage`、`TimelineAnchor` 类型，明确 `interviewer/user/system` 角色、round、anchorId 和历史可用状态。
- [ ] 从现有 session detail 的 question、submissions、follow-up 字段构造消息；缺失历史字段时只显示可确认内容和空状态。
- [ ] 为题目、用户回答、追问、评分节点生成稳定 DOM id，例如 `round-1-question`、`round-1-answer`。

### Task 2: 替换会话主体为聊天流

**Files:**
- Modify: `frontend/src/features/interviews/InterviewSessionPage.tsx`
- Modify: `frontend/src/features/interviews/InterviewSessionPage.css`

- [ ] 保留题目摘要信息，但改为聊天流顶部的面试官气泡。
- [ ] 增加 AI/用户头像、左右气泡、轮次分隔线、时间和来源标签。
- [ ] 将现有回答编辑器改成底部轻量输入区；默认普通文本输入，展开后显示现有 Markdown 工具栏、预览和全屏能力。
- [ ] 提交成功后复用现有 API 和状态更新，把新回答加入时间线并清空输入区。

### Task 3: 增加右侧锚点导航和移动端抽屉

**Files:**
- Modify: `frontend/src/features/interviews/InterviewSessionPage.tsx`
- Modify: `frontend/src/features/interviews/InterviewSessionPage.css`

- [ ] 根据 timeline messages 生成导航节点，区分已完成、当前和未解锁。
- [ ] 点击节点调用 `scrollIntoView`、更新 `window.history.replaceState` hash，并在 IntersectionObserver 中同步当前节点。
- [ ] 桌面固定右侧导航；390px 下折叠为顶部按钮和抽屉，确保不遮挡输入区。
- [ ] 支持 Tab/Enter/Esc 和 `prefers-reduced-motion`。

### Task 4: 浏览器和自动化验证

**Files:**
- Modify: `frontend/e2e/interviews-session.spec.ts`
- Modify: `features/interview-session-chat-timeline.md`

- [ ] 添加历史回答可见、锚点跳转、展开编辑器和提交后时间线更新的回归用例。
- [ ] 使用真实浏览器在 1920×1080 和 390px 视口检查无横向溢出、导航可操作和输入区可达。
- [ ] 运行 `npm --prefix frontend run lint`、`npm --prefix frontend run build`。
- [ ] 更新功能文档并记录旧会话缺失字段的兼容行为。

### Task 5: 提交

- [ ] 检查 `git diff --check`、敏感文件和临时构建产物。
- [ ] 使用连续编号提交：`29实现：面试舱聊天时间线与锚点导航`。
