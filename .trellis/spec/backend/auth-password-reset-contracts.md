# Password Reset Contracts

## Scenario: SMTP password reset

- `POST /api/v1/auth/password-reset/request` accepts `{ email }` and returns a generic accepted response when the account is unknown.
- A known account receives a signed `password_reset` token valid for 10 minutes. The token carries the current `TokenVersion`; successful reset increments that version and invalidates the token and existing sessions.
- `POST /api/v1/auth/password-reset` accepts `{ token, new_password }`. Legacy direct reset remains available only when the explicit in-memory demo flag is enabled.
- SMTP configuration uses `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, and `APP_PUBLIC_URL`.
- Missing SMTP configuration returns `503 邮件服务未配置`; plaintext passwords and reset tokens are never persisted.

## Scenario: Clickable reset email and public reset page

### 1. Scope / Trigger

- Trigger: any change to password-reset email content, `APP_PUBLIC_URL`, `/reset-password` routing, or reset-form submission behavior.
- This is a cross-layer contract: SMTP message → browser URL → React public page → reset API → token invalidation.

### 2. Signatures

- `POST /api/v1/auth/password-reset/request` with `{ "email": string }`.
- `POST /api/v1/auth/password-reset` with `{ "token": string, "new_password": string }`.
- Frontend public URL: `/reset-password?token=<signed-token>`.
- Mail builder: `buildPasswordResetMail(sender, recipient, link string) (passwordResetMail, error)`.

### 3. Contracts

- The email MUST use `multipart/alternative` and contain both `text/plain; charset=UTF-8` and `text/html; charset=UTF-8` parts.
- The HTML part MUST provide a visible `重置密码` `<a href="...">` button and a second clickable full-link fallback; never rely on client-side plaintext URL detection.
- Sender and recipient headers MUST reject CR/LF injection. The reset link MUST be an absolute `http` or `https` URL with a non-empty host.
- `/reset-password` is public even when local access/refresh tokens still exist. It MUST render outside `AppShell`, so the sidebar cannot hide the form.
- The reset form accepts a new password and confirmation. It rejects missing token, passwords shorter than 6 characters, and mismatched confirmation before sending the API request.
- A successful reset clears the local access/refresh session before returning to login because the backend increments `TokenVersion` and invalidates the old session.
- Required environment keys: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `APP_PUBLIC_URL`. Docker Compose may reference them but MUST NOT store credentials in repository files.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Unknown email | Return accepted response without revealing account existence |
| Missing SMTP host/from/public URL | `503 邮件服务未配置` |
| SMTP delivery failure | `503 重置邮件发送失败` |
| Invalid sender/recipient header or reset URL | Mail builder returns an error; no SMTP send |
| Missing reset token | Frontend blocks submit; backend also returns `400 重置令牌不能为空` |
| Expired or wrong token type | `400 重置链接无效或已过期` |
| Used token or stale `TokenVersion` | `400 重置链接无效或已使用` |
| Password shorter than 6 characters | Frontend blocks submit; backend returns `400 密码至少需要 6 位` |
| Confirmation mismatch | Frontend blocks submit without a network request |

### 5. Good / Base / Bad Cases

- Good: a registered user receives a QQ-compatible HTML email, clicks the button while already logged in, sees the standalone reset form, changes the password, and must log in again.
- Base: a mail client that ignores HTML still displays the plaintext absolute URL and 10-minute expiry notice.
- Bad: sending a plaintext-only message and placing `/reset-password` inside authenticated `AppShell` makes the link non-clickable or leaves only the sidebar visible.

### 6. Tests Required

- Unit: parse the built message with `net/mail` + `mime/multipart`; assert `multipart/alternative`, the HTML `href`, and the 10-minute expiry copy.
- Unit: reject CR/LF header injection.
- Backend: run `go test ./...` and retain token expiry/version/password-length regression coverage.
- Frontend: run lint and production build; verify `/reset-password?token=...` renders with and without an existing local session.
- Browser: at 1920×1080 assert no `.sidebar`, no horizontal overflow, both password inputs are present, and mismatch validation blocks submission.

### 7. Wrong vs Correct

#### Wrong

```text
Content-Type: text/plain

请打开：http://localhost:5173/reset-password?token=...
```

```tsx
return token ? <AppShell /> : <AuthPage />
```

#### Correct

```text
Content-Type: multipart/alternative; boundary="..."

text/plain fallback + text/html with <a href="...">重置密码</a>
```

```tsx
if (location.pathname === '/reset-password') return <AuthPage />
return token ? <AppShell /> : <AuthPage />
```
