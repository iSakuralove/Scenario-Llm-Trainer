# httpapi server.go 拆分认证收尾

## 目标

完成 `backend/internal/httpapi` 中 `server.go` 拆分后的收尾检查，恢复认证接口兼容性，避免登录限流误伤正常演示账号登录，并确认 `learning.go` 与 `stt.go` 的同包接线可以通过测试。

## 修改范围

- `backend/internal/httpapi/server.go`
- `backend/internal/httpapi/password_reset_test.go`

## 核心实现

- 恢复 `/api/v1/auth/password-reset` 分支，保持原有通过 `identifier` 和 `new_password` 重置密码后可重新登录的接口行为。
- 登录接口不再在密码校验前消耗账号级限流次数，避免 Redis 限流启用时多次成功登录也触发 429。
- 新增失败登录限流：失败后按 IP 和 identifier 计数，超过阈值返回 429。
- 修复拆分后 `interview_voice.go` 中残留的乱码中文提示，避免语音答案校验和 STT 提示对用户不可读。
- 补回未登录 password reset 的回归测试，并新增登录限流不会阻断连续成功登录、失败登录会被限流且正确密码可恢复登录的测试。

## 影响范围

- 前端现有 `/auth/login` 调用无需调整。
- 依赖旧 `/auth/password-reset` 的调试、演示或脚本恢复可继续使用。
- Redis 限流开启时，正常用户不会因为连续成功登录被账号级限流锁住。

## 验证方式

- `go test ./internal/httpapi`
- `go test ./...`

## 已知限制

- `/api/v1/auth/password-reset` 仍是演示兼容接口，没有短信、邮箱验证码或一次性 token 校验；真实生产部署应替换为带身份校验的重置流程。
- Postgres 种子账号仍会在服务启动时同步默认演示账号数据，固定演示账号的密码持久化策略需要单独评估。
