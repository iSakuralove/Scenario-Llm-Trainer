# 面试舱常用训练轨道推荐 PRD

## 目标

补齐面试舱“推荐训练”里的缺失信号“常用训练轨道”，让启动台能基于用户历史训练频次推荐其最常练的开放轨道。

本任务只增强现有 `GET /api/v1/interviews/launchpad` 的推荐生成逻辑，不新增独立接口、不改训练主流程、不引入新的画像存储。

## 已确认事实

- 当前推荐来源已经包含：
  - `continue_session`
  - `weak_dimension`
  - `fresh_content`
  - `preferred_domain`
  - `default_open_track`
- 前端推荐卡已经能渲染任意 `source_kind`，不需要为新增来源单独改 UI 结构。
- 前端设计文档明确把“常用训练轨道”列为推荐原因之一。

## 需求

1. 新增推荐来源
   - 后端新增 `source_kind=habitual_track`。
   - 推荐依据为用户历史面试里出现频次最高、且当前仍在 `open_tracks` 内的 `domain + difficulty` 组合。

2. 统计口径
   - 可使用历史面试会话统计，不要求只统计已完成会话，但必须至少要求出现 2 次以上才视为“常用”。
   - 如果同频，优先最近练过的轨道。
   - 只推荐当前仍可启动的开放轨道，不能返回不在 `open_tracks` 的组合。

3. 推荐顺序
   - `habitual_track` 的优先级低于：
     - `continue_session`
     - `weak_dimension`
   - 但应高于纯兜底的 `default_open_track`。
   - 与 `fresh_content`、`preferred_domain` 冲突时允许按当前实现的去重规则保留先命中的来源。

4. 推荐文案
   - 推荐原因必须明确说明这是用户最近最常练的轨道，不能是泛文案。

## 验收标准

- 当用户历史里某个开放轨道出现至少 2 次且没有更高优先级信号抢占时，`recommended_tracks` 中包含一条 `source_kind=habitual_track`。
- 该推荐项仍然对应当前 `open_tracks` 内的可启动轨道。
- 推荐文案能明确表达“最近最常练/常用训练轨道”。

## 不做

- 不新增前端独立推荐分组。
- 不新增新的用户画像字段。
- 不做复杂的时间衰减或多维协同推荐算法。
