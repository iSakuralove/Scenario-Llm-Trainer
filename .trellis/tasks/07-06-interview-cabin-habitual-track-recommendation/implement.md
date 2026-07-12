# 面试舱常用训练轨道推荐执行顺序

1. 先补启动台推荐的失败测试，锁定 `habitual_track` 行为。
2. 在 `handlers_interviews.go` 的推荐逻辑里增加历史频次聚合。
3. 更新 Launchpad 契约与 feature 文档。
4. 运行定向后端测试，再跑 `go test ./...`、前端 lint/build/smoke。
