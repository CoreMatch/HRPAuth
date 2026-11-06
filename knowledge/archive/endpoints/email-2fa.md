# 邮件验证码两步验证 (Email 2FA)

邮件验证码作为与 TOTP 和安全密钥（WebAuthn）平级的双因素认证手段。

> 详细实现： [`controllers/email_2fa_controller.go`](../../controllers/email_2fa_controller.go)

## 端点

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [POST /email-2fa/send](#post-email-2fasend) | `POST` | **Login Ticket** |
| [POST /email-2fa/verify](#post-email-2faverify) | `POST` | **Login Ticket** + **Verification Code** |
| [POST /email-2fa/toggle](#post-email-2fatoggle) | `POST` | **Bearer Token** |

---

## POST /email-2fa/send

在持有登录凭证（Login Ticket）的情况下，向用户的注册邮箱发送 2FA 验证码。

### 请求体

```json
{
  "login_ticket": "<Login Ticket>"
}
```

### 成功响应

```json
{
  "success": true,
  "message": "Verification code sent to your email"
}
```

---

## POST /email-2fa/verify

校验邮件验证码。校验成功后，将发放正式的 OAuth2 令牌。

### 请求体

```json
{
  "login_ticket": "<Login Ticket>",
  "code": "123456"
}
```

### 成功响应

```json
{
  "success": true,
  "message": "Email 2FA verified successfully",
  "data": {
    "access_token": "...",
    "refresh_token": "...",
    "expires_in": 3600,
    "token_type": "Bearer"
  }
}
```

---

## POST /email-2fa/toggle

开启或关闭邮件双因素认证。要求用户的邮箱必须已通过基础验证（`verified=true`）。

### 鉴权

**Bearer Token**（要求 scope `email-2fa.toggle`）

### 请求体

```json
{
  "enabled": true
}
```

### 成功响应

```json
{
  "success": true,
  "message": "Email 2FA status updated successfully",
  "data": {
    "enabled": true
  }
}
