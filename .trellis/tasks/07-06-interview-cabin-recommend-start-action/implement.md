# 面试舱推荐卡开始训练按钮执行顺序

1. 先补 smoke 断言，锁定推荐卡主按钮存在并可触发会话创建。
2. 在 `InterviewsPage.tsx` 中把现有 `start()` 抽成可接收指定轨道的启动入口。
3. 调整推荐卡按钮布局与禁用态样式。
4. 更新 feature / architecture 文档。
5. 运行 lint / build / smoke。
