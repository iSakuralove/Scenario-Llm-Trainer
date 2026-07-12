# 面试覆盖率统计执行顺序

1. 后端先补失败测试，锁定 `launchpad.coverage_stats` 的返回行为。
2. 在 `handlers_interviews.go` 增加覆盖率聚合逻辑，复用现有：
   - `interviewLaunchpadAtomTracks`
   - `interviewLaunchpadRecentSessions`
   - `buildInterviewReportRetrievalSummary`
3. 更新前端 API 类型与 `InterviewsPage.tsx` 展示。
4. 更新架构/feature 文档。
5. 运行后端测试、前端 lint/build/smoke。
