# 实施计划

1. 确认前后端本地服务和 Chrome 管理员登录态。
2. 执行桌面端运营动作完整业务冒烟。
3. 执行备注校验、状态历史与重开验证。
4. 执行 390px 窄屏、长文本和空状态检查。
5. 发现问题时定位相关组件并做最小修复。
6. 运行 `npm --prefix frontend run lint` 与 `npm --prefix frontend run build`。
7. 更新 feature 文档中的浏览器验证结果。

## 风险文件

- `frontend/src/features/interviewBank/InterviewBankAdminPage.tsx`
- `frontend/src/features/interviewBank/InterviewBankAdminPage.css`
- `frontend/src/api/client.ts`
- `frontend/src/types/index.ts`
- `features/interview-bank-ops-actions.md`

## 完成条件

- PRD 验收项完成或明确记录阻塞条件。
- 所有代码修复通过 lint/build。
- 真实浏览器结果已记录，不把未执行检查描述为通过。
