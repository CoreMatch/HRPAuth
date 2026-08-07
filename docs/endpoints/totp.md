# TOTP 两步验证

基于 [TOTP（RFC 6238）](https://datatracker.ietf.org/doc/html/rfc6238) 的两步验证，使用与 Google Authenticator / Microsoft Authenticator 兼容的 32 字节 Base32 共享密钥 + 6 位动态口令。

> 详细实现： [`controllers/totp_controller.go`](../../controllers/totp_controller.go)

## 端点

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [GET /totpgen](#get-totpgen-secrettoken) | `GET` | **TOTP Secret**（通过 `?secret=` 传入） |
| [POST /totp/setup](#post-totpsetup) | `POST` | **Remember Token** |
| [POST /totp/verify](#post-totpverify) | `POST` | **TOTP Passcode**（6 位数字） |
| [POST /totp/hasbeenenabled](#post-totphasbeenenabled) | `POST` | **Remember Token** |

> ⚠️ `/totpgen` 是**后端调试接口**，仅用于根据 TOTP Secret 直接生成动态口令。**生产前端不要调用**此接口 —— 用户应从已安装的 Authenticator 应用读取 6 位动态口令。

## Token 流概览

```
POST /totp/setup ──→ 响应 totpkey（TOTP Secret）
        │
        ▼
   用户扫码或手动输入 totpkey 至 Authenticator App
        │
        ▼
   Authenticator App 显示 6 位动态口令（TOTP Passcode）
        │
        ▼
POST /totp/verify { email, passcode }
        │
        ▼
   响应 rt（Remember Token，可在两因素登录后使用）
```

---

## GET /totpgen?secret=xxx

> **仅供调试 / 测试**。

根据传入的 TOTP Secret（Base32 字符串）即时生成当前 6 位动态口令。

### 鉴权

无（但需要知道 `secret` 才能生成正确结果）

### 请求

```http
GET /totpgen?secret=<Base32 TOTP Secret>
```

### 响应

`200 OK`，body 为 6 位数字文本（明文）

```
123456
```

### 用途

- 后端开发自测
- 自动化测试
- 前端在 setup 后立即演示"用这个 secret 现在能算出什么口令"（不推荐生产前端使用）

---

## POST /totp/setup

为已登录用户生成 TOTP Secret，写入 `users.totp` 字段。**响应中的 `totpkey` 字段即为 TOTP Secret**。

### 鉴权

**Remember Token**（通过请求体 `remtoken` 字段提交；与 `email` 字段联合校验）

> M.T. 运维代开：`remtoken` 换成 M-T 并传 `"auth_type": "manage"`，即可为指定 `email` 的用户配置 TOTP（跳过 `remtoken` 归属校验）。后端**不再**因 token 恰好等于 M-T 而自动升级。

### 请求体

```json
{
  "email": "user@example.com",
  "remtoken": "<Remember Token>"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | 是 | 用户邮箱 |
| `remtoken` | string | 是 | Remember Token（字段名 `remtoken`，不是 `remember_token`）；M.T. 路径填 M-T |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |

### 成功响应

```json
{
  "success": true,
  "totpkey": "<TOTP Secret，32 字节 Base32 串>"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `totpkey` | string | TOTP 共享密钥，前端需展示给用户（建议同时展示二维码） |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 400 | `Email mismatch` | `email` 与 Remember Token 对应的用户不匹配 |

### 副作用

- 写入 `users.totp` 字段
- **注意：本接口不要求用户验证一次 passcode**，因此服务端单方面"认为"已开启；如需"二次确认"流程，前端应在拿到 `totpkey` 后立刻让用户输入一次 passcode 并调 `/totp/verify`。

### 前端处理

1. 拿到 `totpkey` 后，在 UI 上展示为 Base32 字符串 + 二维码（前端用 qrcode.js 等库将 `otpauth://totp/...` 编码为二维码）
2. 用户用 Authenticator 扫码添加账号
3. 引导用户输入 Authenticator 显示的 6 位动态口令，调 `/totp/verify` 完成绑定

---

## POST /totp/verify

校验 6 位动态口令。**校验成功后，响应中的 `rt` 字段为该用户的 Remember Token**（如该用户原本没有，服务端会新签发一个并写入数据库）。

### 鉴权

**TOTP Passcode**（6 位数字，通过请求体 `passcode` 字段提交）

### 请求体

```json
{
  "email": "user@example.com",
  "passcode": "123456"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | 是 | 用户邮箱 |
| `passcode` | string | 是 | 6 位 TOTP 动态口令 |

### 成功响应

```json
{
  "success": true,
  "email": "user@example.com",
  "rt": "<Remember Token>"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `rt` | string | Remember Token；如该用户原本没有则新签发 |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 400 | `Invalid passcode` | 6 位动态口令错误或已过期（默认 TOTP 步长 30 秒，容差 ±1 步） |
| 400 | `TOTP not enabled` | 该用户尚未调用 `/totp/setup` |

### 用途

- 已开启 TOTP 的用户在 `/login` 后强制二次校验（前端跳转到 TOTP 校验页）
- 用户首次绑定 TOTP 时，校验一次以完成"确认开启"

---

## POST /totp/hasbeenenabled

查询指定用户是否已开启 TOTP 两步验证。

### 鉴权

**Remember Token**（通过请求体 `rt` 字段提交；与 `uid` 字段联合校验 `users.remember_token`）

> M.T. 运维代开：`rt` 换成 M-T 并传 `"auth_type": "manage"`，即可查询任意 `uid` 用户（跳过 `rt` 归属校验）。后端**不再**因 token 恰好等于 M-T 而自动升级。

### 请求体

```json
{
  "uid": "1",
  "rt": "<Remember Token>"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `uid` | string | 是 | 用户 UID |
| `rt` | string | 是 | Remember Token（字段名 `rt`，不是 `remember_token`）；M.T. 路径填 M-T |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |

### 成功响应

```json
{
  "success": true,
  "enabled": 1
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | int | `1` = 已开启 TOTP（`users.totp` 列非空）；`0` = 未开启（`users.totp` 列为空） |

### 失败响应

```json
{
  "success": false,
  "message": "Invalid uid or rt"
}
```

### 用途

前端可在登录后 / 进入用户中心时探测，决定是否展示"开启 TOTP"入口或"输入 TOTP"入口。
