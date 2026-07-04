# 技术设计

## 架构边界

本任务只修改面试报告聚合层和报告页展示层：

- 后端：`backend/internal/httpapi/interview_runtime.go` 中扩展 `buildInterviewReportRetrievalSummary` 的返回结构与规则聚合逻辑。
- 后端测试：优先在 `backend/internal/httpapi/interview_session_test.go` 或新增同包测试覆盖辅助函数。
- 前端：`frontend/src/types/index.ts` 增加类型字段，`InterviewReportPage.tsx/css` 增加展示。

不修改 `Store` 接口、不改数据库 schema、不改变面试提交或追问检索写入逻辑。

## 数据流

1. 面试提交时，现有逻辑已经将每轮评价写入 `session.Evaluations`。
2. 报告接口读取 session 后调用 `buildInterviewReportRetrievalSummary(session)`。
3. 聚合函数基于每轮 evaluation 生成：
   - 旧摘要字段：命中轮次、回退轮次、subjects、rounds。
   - 新覆盖字段：按 subject 聚合的覆盖分布。
   - 新建议字段：按低分维度、fallback 和覆盖不足生成复训建议。
4. 前端报告页读取 `retrieval_summary` 并在现有摘要面板中分区展示。

## 后端契约

新增 JSON 字段：

```json
{
  "retrieval_summary": {
    "coverage": [
      {
        "subject": "Redis 缓存穿透",
        "round_count": 2,
        "hit_count": 1,
        "fallback_count": 1,
        "average_score": 68,
        "lowest_score": 55,
        "weak_dimensions": ["技术准确性", "逻辑完整性"]
      }
    ],
    "retraining_suggestions": [
      {
        "id": "retrain-redis-缓存穿透",
        "subject": "Redis 缓存穿透",
        "priority": 1,
        "reason": "最低分 55，薄弱项：技术准确性、逻辑完整性",
        "actions": ["复盘本场低分轮次", "围绕该知识点重新完成一轮中等难度面试"],
        "target_score": 75,
        "source_rounds": [1, 3]
      }
    ]
  }
}
```

字段保持只读展示语义，不承诺被长期学习计划直接消费。

## 聚合规则

- subject 选择顺序：
  1. `evaluation.FollowUpSubject`
  2. `evaluation.RetrievedSubjects` 第一项
  3. `session.QuestionSnapshot.Subject`
  4. `session.QuestionSnapshot.Title`
- 每轮 subject 进入覆盖分布；`RetrievedSubjects` 中的其他 subject 只计入命中覆盖，不重复制造轮次。
- `hit_count`：该 subject 所在轮次有 `RetrievedSubjects` 时加 1。
- `fallback_count`：该 subject 所在轮次 `FallbackUsed=true` 时加 1。
- `average_score`：该 subject 关联轮次 `TotalScore` 的四舍五入平均值。
- `lowest_score`：该 subject 关联轮次最低 `TotalScore`。
- `weak_dimensions`：该 subject 关联轮次中低于 70 的维度，映射为中文标签并去重。
- 排序：优先 `lowest_score` 升序，其次 `fallback_count` 降序，其次 subject 字典序，保证稳定输出。

## 建议生成规则

- 对 `lowest_score < 75` 的 subject 生成高优先级建议。
- 对 `fallback_count > 0` 的 subject 生成补齐基础/重练建议。
- 对整场只有 0 或 1 个 subject 的报告生成覆盖扩展建议。
- 同一 subject 最多一个建议，合并原因和动作。
- 建议最多返回 5 条，按 `priority`、`lowest_score`、subject 稳定排序。

## 兼容性

- 新字段为追加字段，旧前端和旧报告数据不受影响。
- 没有 evaluation 时返回空 `coverage` 和空 `retraining_suggestions`。
- 不依赖 selected atom 快照展示，遵守现有安全边界。

## 回滚点

- 后端改动集中在 `interview_runtime.go`，可单文件回滚。
- 前端改动集中在报告页和类型文件，失败时可隐藏新分区但保留接口字段。
