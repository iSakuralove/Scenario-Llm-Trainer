# 技术设计

## 边界

- 后端只增强 `launchpad.summary.state`
- 前端只增强启动台状态说明和空状态分支
- 不改会话创建、报告和学习计划接口

## 状态机

- `ready`: `open_tracks>0` 且 `indexed_atom_count >= published_atom_count > 0`
- `retrieval_partial`: `open_tracks>0` 且 `indexed_atom_count > 0 && indexed_atom_count < published_atom_count`
- `retrieval_degraded`: `open_tracks>0` 且 `published_atom_count > 0 && indexed_atom_count == 0`
- `compatibility_fallback`: `fallback_mode=true`

## 前端策略

- API 请求失败 -> 兼容轨道 fallback
- API 成功且 `fallback_mode=true` -> 兼容轨道 fallback
- API 成功且 `fallback_mode=false` -> 使用后端返回的真实轨道，即使 `open_tracks=[]`
