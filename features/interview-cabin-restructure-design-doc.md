# 面试舱重构设计文档完善

## 目标

基于现有 AI-Interview 接入 PRD、技术方案和面试题库版本治理数据层，完善 `interview-cabin-restructure.md`，让后续前端实现具备清晰的信息架构、交互状态、视觉方向和工程边界。

## 修改范围

- 重写根目录 `interview-cabin-restructure.md`。
- 新增 Trellis 任务规划文件，记录本次文档任务的目标、引用决策和验收标准。

## 核心实现

- 将面试舱拆分为用户端训练启动台、管理员题库治理台和报告复盘面板。
- 明确普通用户侧只展示后端返回的可用组合，未开放组合不显示。
- 对齐题库治理决策：`admin` 权限、三段式导入、版本历史、`base_version`、`vector_status`、归档/恢复和审计边界。
- 补充前端模块拆分、状态管理、API 边界、响应式布局、空状态、错误状态、键盘操作和可访问性要求。
- 保留当前深色命令屏视觉语言，提出 Interview Signal Deck 作为后续设计方向。

## 影响范围

- 影响后续面试舱前端重构、管理端题库治理页面和报告页增强的实现依据。
- 不修改 React、Go、数据库 schema 或运行时逻辑。
- 不改变现有面试页面行为。

## 验证方式

- 读取并对齐 `docs/ai-interview-integration-prd.md`。
- 读取并对齐 `docs/ai-interview-integration-tech-design.md`。
- 读取并对齐 `docs/architecture.md` 与 `features/interview-bank-version-storage.md`。
- 使用 CodeGraph 检查当前面试启动台和面试会话入口结构。
- 检查新文档中的关键约束：未开放组合不显示、组合状态后端返回、普通用户不展示治理信息、管理端承载版本与索引治理。

## 已知限制

- 本次只完善设计文档，不实现接口、页面或样式。
- Topic 覆盖分析仍定位为后续增强，首期用户端不硬做 Topic 导航。
- 后续实现前仍需根据实际 API 响应结构补充类型定义和交互测试。
