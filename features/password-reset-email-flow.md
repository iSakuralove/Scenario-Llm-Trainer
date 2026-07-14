# 邮箱密码重置链路

## 目标

打通个人档案、登录页和后端 SMTP 邮箱重置密码流程。

## 修改范围

- 后端新增邮箱重置申请和令牌确认接口。
- 前端登录页增加“忘记密码”和带 token 的新密码页面。
- 个人档案增加已登录用户的密码更新入口。

## 核心实现

- 重置 token 使用签名 JWT，10 分钟过期并绑定用户 `TokenVersion`。
- 成功改密后 `UpdateUserPassword` 增加 TokenVersion，使旧登录令牌和旧重置 token 失效。
- SMTP 从环境变量读取；未配置时明确返回服务未配置，不降级为明文密码或可猜链接。

## 影响范围

- 不改变普通登录、注册和刷新令牌接口。
- 旧的匿名直接重置仅保留给显式开启的内存演示模式。

## 验证方式

- `go test ./internal/httpapi -run PasswordReset -count=1`
- `npm --prefix frontend run lint`
- `npm --prefix frontend run build`
- 浏览器确认登录页出现“忘记密码”，个人档案出现“密码安全”。

## 已知限制

- 未配置 SMTP 时无法实际发送邮件；需要部署环境设置 SMTP 变量后验证真实投递。
- Docker Compose 只引用系统环境变量，不在仓库文件中保存 SMTP 授权码；`SMTP_PASSWORD` 必须留空于示例文件。
