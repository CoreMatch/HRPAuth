# 认证模块

本站业务系统的注册、登录、登出入口。涉及 **Remember Token** 的颁发与回收。

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [POST /login](#post-login) | `POST` | 无 |
| [POST /register](#post-register) | `POST` | 无（WebUI 路径开启 captcha 时需 `captcha_token`+`captcha_code`；M.T. 路径见下文）|
| [GET /logout](#get-logout) | `GET` | **Remember Token** |

> 详细实现： [`controllers/auth_controller.go`](../../controllers/auth_controller.go)

---

## POST /login

用户登录。**成功响应中的 `token` 字段即为 Remember Token**。

| 字段 | 值 |
|------|---|
| 鉴权 | 无 |
| 颁发 | Remember Token |

### 请求体

```json
{
  "email": "user@example.com",
  "password": "password123"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | 是 | 邮箱 |
| `password` | string | 是 | 明文密码（传输需 HTTPS） |

### 成功响应

`200 OK`

```json
{
  "success": true,
  "message": "Login successful",
  "token": "<Remember Token，32 字节随机串>",
  "uid": 1,
  "totp": 0
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `token` | string | **Remember Token**，前端需保存（建议 localStorage / Pinia / Redux） |
| `uid` | int | 用户 ID |
| `totp` | int | `1` = 该用户已开启 TOTP，登录后应引导 TOTP 校验；`0` = 未开启 |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 400 | `Invalid email or password` | 邮箱 / 密码错误 |
| 429 | `Too many requests` | 触发登录限流（配置 `security.rate_limit_*`） |

### 流程

1. 校验请求参数非空
2. 查 users 表：邮箱匹配 + bcrypt 验证密码
3. 颁发 Remember Token（32 字节随机），写库 `users.remember_token`
4. 返回 token + uid + totp 状态

---

## POST /register

用户注册。本端点支持**两条路径**：
- **WebUI 路径**：玩家在 WebUI 表单注册，开启 captcha 时需先调 [`POST /captcha`](./captcha.md)。
- **M.T. 路径**（Manage Token）：由 WinnerProxy 调用，用于代注册/绑定未在 HA 注册的 Mojang 正版玩家（详见 `references/HA-ROADMAP.md` §3）。

| 字段 | 值 |
|------|---|
| 鉴权 | 无（WebUI 路径）；**Manage Token**（M.T. 路径，使用 `remember_token` 字段携带 + 显式声明 `auth_type: "manage"`，值必须匹配 `config.yaml > manage.token`）|
| 副作用 | `users` 表插入/更新；事务中为该用户创建或获取默认 Profile；**M.T. 路径**还会异步触发代注册用户清理 routine |

### 路径分流的判定

后端解析可选的 `auth_type` 字段（JSON / 表单 / 查询参数）：
- 未声明或 `auth_type=remember` → **WebUI 路径**（默认），此时 `remember_token` 字段被忽略——即使 token 恰好等于 M-T 也不会升级。
- `auth_type=manage` 且 `remember_token` 等于 `config.AppConfig.Manage.Token` → **M.T. 路径**。
- `auth_type` 为未知值，或声明 `manage` 但 token 与配置 M-T 不符 → `400 Invalid auth type or token`。

判别实现见 [`controllers/auth_controller.go`](../../controllers/auth_controller.go) 的 `isManageRequest`。

### WebUI 路径请求体

```json
{
  "email": "user@example.com",
  "username": "PlayerOne",
  "password": "password123",
  "captcha_token": "<来自 POST /captcha 响应的 token>",
  "captcha_code": "<用户在图上识别出的字符>"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | 是 | 邮箱（业务层判重）|
| `username` | string | 是 | 长度 ≥ 3 |
| `password` | string | 是 | 长度 ≥ 6 |
| `captcha_token` | string | 开启 captcha 时 | 来自 `POST /captcha` 响应 |
| `captcha_code` | string | 开启 captcha 时 | 用户识别字符（不区分大小写，自动 trim）|

`mojang_uuid` 和 `remember_token` 字段在 WebUI 路径下被忽略。

### M.T. 路径请求体

```json
{
  "email": "alice@mojang-imported.invalid",
  "username": "PlayerOne",
  "password": "<任意 ≥ 6 字符>",
  "mojang_uuid": "f7c77d999f154a66a87dc4a51ef30d19",
  "remember_token": "<Manage Token>",
  "auth_type": "manage"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `auth_type` | string | 是 | `"manage"`，声明走 M.T. 路径 |
| `remember_token` | string | 是 | **Manage Token**（M.T.），与 `config.yaml > manage.token` 匹配 |
| `username` | string | 是 | 长度 ≥ 3 |
| `password` | string | 是 | 长度 ≥ 6（M.T. 路径下必填，但实际值由 WinnerProxy 自定）|
| `mojang_uuid` | string | 见规则 | 32 位小写 hex（无连字符）；提供时进入绑定流程 |
| `email` | string | 否 | 缺省时自动用 `{username-lowercased}@mojang-imported.invalid` 占位 |

M.T. 路径下 `captcha_token` / `captcha_code` 不要求；`email` 格式不严格校验（占位可非标准）。

### WebUI 路径校验顺序

1. 解析 JSON
2. 邮箱格式 (`mail.ParseAddress`)
3. 用户名长度 ≥ 3
4. 密码长度 ≥ 6
5. **captcha 校验**（开启时）
6. 邮箱业务层判重
7. 用户名业务层判重
8. 写库事务（user + 默认 profile）

### M.T. 路径决策树（§3.4）

```
1. 若 mojang_uuid 不为空 → 按 mojang_uuid 查 users
   ├ 命中  → 幂等 200
   └ 未中 → 步骤 2

2. 按 username 查 users
   ├ 命中:
   │   a. mojang_uuid 有 && user.mojang_uuid = NULL
   │        ├ user.mbe = 0 → 409 username_already_bound  ← HA 优先, Mojang 玩家被踢
   │        └ user.mbe = 1 → bind 200
   │           (UPDATE: mojang_uuid + last_sign_at, **保留** password/email/cbh)
   │   b. mojang_uuid 有 && user.mojang_uuid = mojang_uuid → 幂等 200
   │   c. mojang_uuid 有 && user.mojang_uuid ≠ mojang_uuid → 409 username_already_bound
   │   d. mojang_uuid 空 → 400 mojang_uuid_required_for_existing_user
   └ 未中:
       → 新建 user
           mojang_uuid 有 → cbh=0 (代注册)
           mojang_uuid 无 → cbh=1 (M.T. 路径但未提供 mojang_uuid, 走 WebUI 同等流程)
           mbe 保持默认 0
```

> **M.T. 路径下 2.a 的"保留"语义**：当 mbe=1 允许绑定时，**只更新 `mojang_uuid` 和 `last_sign_at`**，WebUI 用户的 `password` / `email` / `cbh` 均不被覆盖。

### 成功响应

`200 OK`，**所有路径**都返回 `profile_id`（Profile 表主键，对应 Yggdrasil `selectedProfile.id`）：

```json
{
  "success": true,
  "uid": 1,
  "message": "Register successful",
  "profile_id": "a1b2c3d4e5f67890abcdef1234567890"
}
```

M.T. 路径**新建代注册**用户时（M.T. + 新 username + mojang_uuid），响应额外包含：

```json
{ "success": true, "uid": 1, "message": "Register successful", "profile_id": "...", "cbh": 0 }
```

`cbh: 0` 仅在 cbh=0 时出现，cbh=1 路径不返回此字段（避免冗余）。

> 注册成功**不会**自动颁发 Remember Token（Yggdrasil 玩家由 WinnerProxy 后续流程颁发；WebUI 用户需引导跳登录页）。

### 失败响应汇总

| HTTP | message / error | 触发场景 | 路径 |
|------|----------------|----------|------|
| 400 | `Invalid request body` | JSON 解析失败 | 通用 |
| 400 | `Invalid auth type or token` | `auth_type` 为未知值，或声明 `manage` 但 token 与配置 M-T 不符 | 通用 |
| 400 | `Username too short` | 用户名 < 3 字符 | 通用 |
| 400 | `Password too short` | 密码 < 6 字符 | 通用 |
| 400 | `Invalid email` | WebUI 路径下邮箱格式不合法 | WebUI |
| 400 | `Invalid or expired captcha` | 缺失 / 错误 / 过期的 captcha | WebUI（开启 captcha 时）|
| 400 | `invalid_mojang_uuid` | M.T. 路径下 `mojang_uuid` 格式错误（非 32 位小写 hex）| M.T. |
| 400 | `mojang_uuid_required_for_existing_user` | M.T. 路径下用户已存在但未提供 `mojang_uuid` | M.T. |
| 409 | `Email already registered` | 邮箱已被注册 | WebUI |
| 409 | `Username already taken` | 用户名已被占用 | WebUI |
| 409 | `username_already_bound` | 见决策树 2.a(mbe=0) / 2.c | M.T. |
| 500 | `Password hashing failed` / `Failed to create user profile` / `Failed to bind mojang_uuid` / `Failed to load profile` / `Failed to create user` | 服务端异常 | 通用 |

---

## GET /logout

注销当前 Remember Token。

| 字段 | 值 |
|------|---|
| 鉴权 | **Remember Token** |

### Remember Token 传递方式（三种任选其一，后端依次识别）

1. **请求体 JSON 字段** `remember_token`
2. **查询参数** `?remember_token=xxx`
3. **表单字段**

### 请求示例

```http
GET /logout?remember_token=<token>
```

或（推荐）：

```http
POST /logout
Content-Type: application/json

{ "remember_token": "<token>" }
```

> 虽以 `GET` 注册，但实际接受 `POST` 调用方式以避免日志中泄露 Token。

### 成功响应

```json
{
  "success": true,
  "message": "Logged out"
}
```

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |

### 后端行为

清空 `users.remember_token` 字段（置空字符串），**不删除用户记录**。客户端应同时清除本地存储的 Token。
