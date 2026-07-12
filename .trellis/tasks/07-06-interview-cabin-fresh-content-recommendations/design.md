# 技术设计

## 边界

- 只改 `handlers_interviews.go` 推荐逻辑
- 可选更新接口契约文档
- 前端只被动消费新的 `source_kind` 与 `reason`

## 设计

- 复用 `interviewLaunchpadAtomTracks()` 里已有的 `latestUpdate`
- 在 `recommendInterviewLaunchpadTracks()` 中，遍历轨道按 `latestUpdate DESC` 排序追加推荐
- 对已经被更高优先级信号占用的轨道继续去重

## TDD

1. RED：最近更新的 atom-backed 轨道产生 `fresh_content` 推荐。
2. GREEN：补推荐逻辑与排序。
