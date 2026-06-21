# 实施清单

## 实施步骤

1. 在后端 `handleInterviews` 中增加 `/launchpad` GET 分支。
2. 新增后端辅助函数生成 Launchpad 响应，复用现有 `FindInterviewQuestion` 判断轨道可用性。
3. 在前端 `client.ts` 增加 Launchpad 响应类型和 `api.interviewLaunchpad`。
4. 修改 `InterviewsPage.tsx`：
   - 增加加载、错误、兼容模式状态。
   - 使用接口轨道作为主数据源。
   - 接口失败或空返回时回退静态轨道。
   - 键盘导航、领域 chip、开始面试全部改用当前轨道列表。
5. 必要时微调 CSS，确保加载/降级提示不破坏现有响应式布局。
6. 更新 `features/` 文档记录本阶段实现。

## 验证命令

- `npm --prefix frontend run build`
- `npm --prefix frontend run lint`
- 后端按项目现有可用命令执行测试或构建，优先尝试 `go test ./...`。

## 风险点

- `InterviewsPage` 当前多处直接引用 `interviewLaunchTracks`，必须统一切到当前有效轨道列表，避免键盘索引和领域选择仍读静态数组。
- 如果后端仅按静态首期组合返回，但旧种子题缺少某个组合，前端会少显示轨道；这是正确行为，不能再前端补齐。
- 接口响应结构要保持简单，避免提前泄露管理端治理字段。

## 回滚点

- 前端保留静态 `interviewLaunchTracks` 兜底，若接口异常可立即恢复现有体验。
- 后端新增接口不改变现有 `POST /interviews/sessions` 行为，运行时风险较小。
