# Password Reset Contracts

## Scenario: SMTP password reset

- `POST /api/v1/auth/password-reset/request` accepts `{ email }` and returns a generic accepted response when the account is unknown.
- A known account receives a signed `password_reset` token valid for 10 minutes. The token carries the current `TokenVersion`; successful reset increments that version and invalidates the token and existing sessions.
- `POST /api/v1/auth/password-reset` accepts `{ token, new_password }`. Legacy direct reset remains available only when the explicit in-memory demo flag is enabled.
- SMTP configuration uses `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, and `APP_PUBLIC_URL`.
- Missing SMTP configuration returns `503 邮件服务未配置`; plaintext passwords and reset tokens are never persisted.
