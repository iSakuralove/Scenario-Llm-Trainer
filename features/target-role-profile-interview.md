# 目标岗位字段接入面试舱

## 目标

把 PRD 中明确提到的“目标岗位”从个人档案接入到面试舱准备区，不再只用目标职级和偏好领域代替。

## 修改范围

- 后端 `UserProfile` 新增 `target_role`
- `PUT /users/me/profile` 支持保存 `target_role`
- 个人档案页新增目标岗位输入
- 面试舱准备区显示目标岗位
- 增加后端回归测试与前端 smoke 断言

## 核心实现

- `UserProfile` 在前后端 JSON 契约里新增 `target_role`，仍然走现有 `profile` JSON 持久化，不需要数据库 schema 迁移。
- Profile 页面现在可编辑：
  - 目标职级
  - 目标岗位
  - 偏好专业域
  - 简历/项目摘要
- 面试舱准备区现在显式展示：
  - 目标职级
  - 目标岗位
  - 偏好领域
- 如果目标岗位为空，页面只显示 `待补充`，不阻断训练。

## 影响范围

- 不改会话创建接口结构。
- 不改推荐算法。
- 后端只是在现有 profile JSON 上扩一层字段。

## 验证方式

- `go test ./internal/httpapi -run TestProfileUpdatePersistsTargetRole -count=1`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- `npm --prefix frontend run smoke`

## 已知限制

- 当前目标岗位只做展示和档案保存，还没有进入推荐算法或题目筛选策略。
