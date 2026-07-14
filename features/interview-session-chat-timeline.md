# 面试会话聊天时间线与锚点导航

## 目标

让面试问答以连续聊天方式呈现，支持回看上一轮题目、回答和追问，并通过右侧锚点快速定位。

## 修改范围

- 面试会话页新增 AI/用户左右聊天气泡与头像。
- 根据会话 `submissions`、`evaluations` 和当前题目构造历史时间线。
- 桌面端新增右侧锚点导航，移动端折叠为“对话锚点”按钮。
- 默认使用轻量回答输入，Markdown/代码/Mermaid 编辑器按需展开。

## 核心实现

- 消息节点使用稳定的轮次 id，点击锚点后调用 `scrollIntoView` 并更新 URL hash。
- 旧会话没有提交记录时显示明确空状态，不伪造历史回答。
- 参考 `tmp/AI-Interview-ref/frontend/src/views/Interview.vue` 的角色消息流模式，但继续复用当前 React Store 和 API。

## 影响范围

- 不修改后端接口、数据库结构和评分流程。
- 语音上传、转写确认、Markdown 工具栏、提交和报告跳转保持原有行为。

## 验证方式

- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- 1920×1080 真实浏览器：聊天气泡与右侧锚点正常渲染。
- 390px 模拟视口：`scrollWidth === innerWidth`，锚点折叠按钮可见。

## 已知限制

- 当前锚点高亮由点击状态驱动，尚未接入 IntersectionObserver 自动跟随滚动。
- 现有数据模型只保存用户提交与评估追问，不包含更细粒度的流式消息事件。
