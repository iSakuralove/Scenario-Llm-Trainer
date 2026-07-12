# 简历结构化提取增强 PRD

## 目标

把当前“上传文件 -> 原文写入 `resume_summary`”推进成最小结构化提取：

- 尝试识别 `target_role`
- 提炼 `resume_summary`
- 提炼 `project_summary`

首版只做本地启发式提取，不依赖外部 LLM。

## 需求

1. 输入
   - 复用当前 `/users/me/profile/import`

2. 输出行为
   - 从简历文本中尽量提取：
     - `target_role`
     - `resume_summary`
     - `project_summary`
   - 仍返回更新后的 `User`

3. 解析约束
   - 有“求职意向 / 目标岗位 / 应聘岗位 / Position / Target Role”等字段时优先识别 `target_role`
   - 有“项目经历 / Projects / Project Experience”等字段时优先提取 `project_summary`
   - 其余内容归入 `resume_summary`

## 验收标准

- 上传包含“求职意向 + 项目经历”的文本后，`target_role` 和 `project_summary` 能被填充
- 原有 `resume_summary` 仍会被更新

## 不做

- 不做外部 LLM 总结
- 不做复杂简历 schema
