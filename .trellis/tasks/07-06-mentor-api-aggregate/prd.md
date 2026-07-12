# AI Mentor 聚合接口 PRD

## 目标

为独立 Mentor 页面补一个稳定的后端聚合接口，避免前端继续并行拼接 `dashboard + launchpad`。

首版不做 LLM 洞察，只聚合当前已有数据。

## 需求

1. 新接口
   - `GET /api/v1/users/me/mentor`

2. 返回内容
   - 综合概览
   - 优势 / 待提升
   - 风险预警
   - 建议行动
   - 知识覆盖
   - 目标画像上下文

3. 实现约束
   - 复用当前 `learningPlan()` 和 `interviewLaunchpad()`
   - 不新增数据库表
   - 不调用外部 LLM

## 验收标准

- 接口返回 Mentor 页面需要的聚合字段
- 前端 Mentor 页面改为优先使用该接口

## 不做

- 不做 `mentor-insight/refresh`
- 不做用户自定义 Provider 检查
