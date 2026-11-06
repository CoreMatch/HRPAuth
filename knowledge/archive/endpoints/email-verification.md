# 邮箱验证

提供 6 位数字邮箱验证码的下发与校验。验证码存于 Redis，**10 分钟有效**。

> 详细实现： [`controllers/email_verification_controller.go`](../../controllers/email_verification_controller.go) · [`services/email_service.go`](../../services/email_service.go)

## 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| [POST /email-verification](#post-email-verification) | `POST` | 通过 `action` 字段区分 3 种子操作 |

---

## POST /email-verification

通过 `action` 字段分发的多操作端点。

### 子操作总览

| action | 所需 Token | 说明 |
|--------|-----------|------|
| `send-test-email` | 无 | 直接发测试邮件，仅需提供 `to`/`subject`/`message` |
| `send-verification-code` | 无 | 服务器生成 6 位数字验证码并存入 Redis（10 分钟有效），再发邮件给用户 |
| `verify-code` | **Email Verification Code**（6 位数字） | 由 `send-verification-code` 通过邮件下发的 6 位验证码，通过请求体 `code` 字段提交 |

---

### action: `send-test-email`

调试用。直接发送一封指定主题与正文的邮件到指定地址，不涉及 Redis。

#### 请求体

```json
{
  "action": "send-test-email",
  "to": "user@example.com",
  "subject": "Test",
  "message": "Hello from HRPAuth"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `to` | string | 是 | 收件人邮箱 |
| `subject` | string | 是 | 邮件主题 |
| `message` | string | 是 | 邮件正文（纯文本） |

#### 成功响应

```json
{
  "success": true,
  "message": "Test email sent"
}
```

#### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 500 | `SMTP send failed` | SMTP 配置错误 / 网络异常 |

---

### action: `send-verification-code`

下发 6 位数字验证码到指定邮箱，存入 Redis（key = `{prefix}email:code:<email>`，TTL = 600 秒）。

#### 请求体

```json
{
  "action": "send-verification-code",
  "email": "user@example.com"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | 是 | 接收验证码的邮箱 |

#### 成功响应

```json
{
  "success": true,
  "message": "Verification code sent"
}
```

#### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 400 | `Invalid email` | 邮箱格式不合法 |
| 500 | `SMTP send failed` | SMTP 发送失败 |

---

### action: `verify-code`

校验用户输入的 6 位数字验证码。

#### 请求体

```json
{
  "action": "verify-code",
  "email": "user@example.com",
  "code": "123456"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | 是 | 注册时使用的邮箱 |
| `code` | string | 是 | 6 位数字验证码（来自 `send-verification-code` 邮件） |

#### 成功响应

```json
{
  "success": true,
  "message": "Email verified"
}
```

#### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 400 | `Invalid verification code` | 验证码错误或已过期 |

> 验证码 10 分钟有效，**校验通过后从 Redis 删除（单次使用）**。

#### 成功副作用

成功后会将 `users.verified` 字段置为 `true`，并被 `POST /user` 的 `verified` 字段反映。

---

## 典型流程

```text
1. 用户在 UI 输入邮箱 → POST /email-verification { action: "send-verification-code" }
2. 用户收到邮件，读取 6 位数字
3. 用户在 UI 输入验证码 → POST /email-verification { action: "verify-code", code: "xxxxxx" }
4. 成功 → 后端置 users.verified = true
5. 后续 POST /user 响应中 data.verified = true
```
