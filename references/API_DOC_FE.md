# HRPAuth 前端集成 API 文档（AI 简洁版）

> 配套 HRPAuth-Web 前端项目使用。
> 完整规范（含状态机、实现细节、配置结构）请见 [`../docs/`](../docs/) 目录下的开发文档。
>
> 本文档侧重"**如何调用**"，不展开内部实现。修改 API 端点时请同步更新本文件与 `../docs/`。

## 1. 鉴权总览

本站业务系统**仅使用 Remember Token**（由 `POST /login` 颁发），与 Minecraft Yggdrasil 体系完全独立。

| Token | 字段名 | 获取方式 | 用途 |
|-------|-------|---------|------|
| **Remember Token** | `token`（登录响应）/ `remember_token` / `remtoken` / `rt` | `POST /login` 成功响应 | 本站所有登录态接口的鉴权凭证 |
| **Manage Token（M-T）** | 同上字段名（任意 remtoken 字段均可） | 首次启动时随机生成，存于 `config.yaml` 的 `manage.token` | **运维超级 remtoken**，等价于"以任意用户身份操作"。必须额外**声明 `auth_type: "manage"`** 并提供 `uid` 或 `email` 指定目标用户 |
| 邮箱验证码 | `code`（6 位数字） | `POST /email-verification` (action=send-verification-code) 邮件下发 | 邮箱验证 |
| 图形验证码 Token | `captcha_token` | `POST /captcha` 响应 | 注册时与 `captcha_code` 配对提交 |
| 图形验证码答案 | `captcha_code`（4 位字符） | 用户在 `/captcha/image/:token` 图片上识别 | 注册时与 `captcha_token` 配对提交 |
| TOTP Secret | `totpkey` | `POST /totp/setup` 响应 | 写入 Google Authenticator 等应用 |
| TOTP Passcode | `passcode`（6 位数字） | 用户从 Authenticator 应用读取 | `/totp/verify` 提交 |

> ⚠️ **关键区分**：本站 Token 体系（Remember Token）与 Minecraft Yggdrasil 体系（accessToken / clientToken）完全独立。前端**不直接调用** Yggdrasil 端点（`/authserver/*`、`/sessionserver/*`），这部分由 Minecraft 客户端处理。

### Remember Token 传递方式

所有需要鉴权的接口，Remember Token 可通过以下任意一种方式提交（后端依次识别）：

1. **请求体 JSON 字段** `remember_token` 或 `rt` 或 `remtoken`（按端点约定，见各接口说明）
2. **查询参数** `?remember_token=xxx`
3. **表单字段**

**建议**：统一使用请求体 JSON，便于统一拦截器和错误处理。

### auth_type（Token 类型声明）

每个接受 remtoken 的接口还支持**可选的 `auth_type` 字段**，用于显式声明提交的 token 类型（JSON / 查询参数 / 表单均可，后端依次识别）：

| `auth_type` 值 | 含义 |
|---------------|------|
| 缺省 / `remember` | **默认**。按 Remember Token 处理：查数据库 `remember_token` 定位当前用户 |
| `manage` | 按 Manage Token 处理：**必须**提交 `config.yaml > manage.token` 对应的值，且必须再提供 `uid` 或 `email` 指定目标用户 |

> ⚠️ **重要**：后端**不再**通过"token 恰好等于 M-T"来自动升级为运维模式。未声明 `auth_type`（或声明 `remember`）时，即使 token 恰好等于 M-T，也一律按 Remember Token 走数据库校验。声明 `auth_type="manage"` 但 token 与配置 M-T 不符，或 `auth_type` 为其他未知值，后端直接拒绝（返回 `Invalid auth type or token` / `无效的鉴权类型或token`）。

---

## 2. 通用约定

### 请求 / 响应格式

- 全部使用 `application/json`（纹理上传除外，使用 `multipart/form-data`）
- 请求方法：优先 `POST`（含读操作，如 `POST /user`）
- 响应统一结构：

```json
{
  "success": true,
  "message": "可选的成功描述",
  "data": { /* 业务数据，可选 */ }
}
```

### 常见错误响应

| HTTP | success | 典型 message | 触发场景 |
|------|---------|--------------|----------|
| 400 | false | `Invalid or expired captcha` | 注册时图形验证码错误或已使用 |
| 400 | false | `Invalid email or password` | 登录失败 |
| 400 | false | `Invalid verification code` | 邮箱验证码错误或过期 |
| 401 | false | `Invalid token` / `Unauthorized` | Remember Token 缺失或错误 |
| 403 | false | `Captcha is disabled` | 后端未启用图形验证码时调用 `/captcha` |
| 404 | false | `Captcha not found or expired` | `/captcha/image/:token` 的 token 不存在 |
| 409 | false | `Email already exists` | 注册邮箱已被占用 |
| 429 | false | `Too many requests` | 触发限流（见配置 `rate_limit_*`） |
| 500 | false | `Internal server error` | 服务端异常 |

---

## 3. 端点索引

| 模块 | 方法 | 路径 | 是否需要 Remember Token |
|------|-----|------|------------------------|
| 服务状态 | GET | `/status` | 否 |
| 认证 | POST | `/login` | 否 |
| 认证 | POST | `/loginbymt` | **Manage Token** |
| 认证 | POST | `/register` | 否（WebUI 路径开启 captcha 时需 captcha_token+code；M.T. 路径见下）|
| 认证 | GET | `/logout` | **是** |
| 认证 | GET | `/captcha/enabled` | 否 |
| 认证 | POST | `/captcha` | 否 |
| 认证 | GET | `/captcha/image/:token` | 否 |
| 用户 | POST | `/user` | **是** |
| 用户 | POST | `/user/declare-email` | **Manage Token** |
| 用户 | POST | `/user/mojang-bind-enable` | **是**（或 Manage Token + uid/email）|
| 邮箱 | POST | `/email-verification` | 视 action 而定 |
| TOTP | POST | `/totp/setup` | **是** |
| TOTP | POST | `/totp/verify` | 否（凭 passcode） |
| TOTP | POST | `/totp/hasbeenenabled` | **是** |
| 资料 | POST | `/change-username` | **是** |
| 资料 | POST | `/change-profile-name` | **是** |
| 纹理 | POST | `/texture/upload` | **是** |
| 纹理 | POST | `/texture/delete` | **是** |
| 纹理 | POST | `/texture/get` | **是** |

---

## 4. 认证模块

### 4.1 POST /login

**所需 Token：** 无

**请求体：**
```json
{ "email": "user@example.com", "password": "password123" }
```

**成功响应：**
```json
{
  "success": true,
  "message": "Login successful",
  "token": "<Remember Token>",
  "uid": 1,
  "totp": 0
}
```

**字段说明：**
- `token`：Remember Token，前端需保存（建议 localStorage / Pinia / Redux）
- `uid`：用户 ID
- `totp`：`1` = 该用户已开启 TOTP，登录后应跳转 TOTP 校验页；`0` = 未开启

**失败响应（401）：**
```json
{ "success": false, "message": "Invalid email or password" }
```

### 4.2 POST /loginbymt

**所需 Token：** **Manage Token**

使用 Manage Token 直接为指定用户签发 Remember Token。

**请求体：**
```json
{ "uid": 1, "email": "user@example.com", "manage_token": "<Manage Token>" }
```

**成功响应：**
```json
{
  "success": true,
  "message": "Login successful",
  "token": "<Remember Token>",
  "uid": 1,
  "email": "user@example.com",
  "totp": 0
}
```

### 4.3 POST /register

**所需 Token：** 无（WebUI 路径）；**Manage Token**（M.T. 路径，WinnerProxy 使用）

`POST /register` 按声明的 `auth_type` 分流到两条路径：
- **WebUI 路径**（默认，未声明或 `auth_type=remember`）：玩家在 WebUI 表单注册，开 captcha 时需 `captcha_token`+`captcha_code`。
- **M.T. 路径**（`auth_type="manage"` + 配置的 Manage Token）：WinnerProxy 代注册 / 绑定未在 HA 注册的 Mojang 正版玩家，跳过 captcha 与邮箱格式校验，详见 `references/HA-ROADMAP.md` §3。

> 未声明 `auth_type` 时一律走 WebUI 路径；声明了 `auth_type="manage"` 但 token 与配置 M-T 不符（或未知 `auth_type` 值）返回 `400 Invalid auth type or token`。

#### WebUI 路径请求体

未开启图形验证码：
```json
{ "email": "user@example.com", "username": "PlayerOne", "password": "password123" }
```

开启图形验证码（常见情况）：
```json
{
  "email": "user@example.com",
  "username": "PlayerOne",
  "password": "password123",
  "captcha_token": "<来自 POST /captcha 响应的 token>",
  "captcha_code": "<用户输入的字符>"
}
```

#### M.T. 路径请求体

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

| 字段 | WebUI 路径 | M.T. 路径 | 说明 |
|------|-----------|-----------|------|
| `email` | 必填 | 可省略（自动用 `{username}@mojang-imported.invalid`）| |
| `username` | 必填，≥ 3 字符 | 必填，≥ 3 字符 | |
| `password` | 必填，≥ 6 字符 | 必填，≥ 6 字符 | |
| `captcha_token`/`captcha_code` | 开启 captcha 时必填 | 忽略 | |
| `mojang_uuid` | 忽略 | 32 位小写 hex，可选 | 不传走 WebUI 同等流程（cbh=1）；传了走代注册/绑定 |
| `remember_token` | 忽略 | **必填**，M.T. | |
| `auth_type` | 忽略（缺省即 remember） | **必填** `"manage"` | 声明 token 类型 |

#### 成功响应

**所有路径**都返回 `profile_id`（Profile 表主键，对应 Yggdrasil `selectedProfile.id`）：

```json
{ "success": true, "uid": 1, "message": "Register successful", "profile_id": "a1b2c3d4..." }
```

M.T. 路径**新建代注册**用户时（M.T. + 新 username + mojang_uuid），响应额外包含 `cbh: 0`：

```json
{ "success": true, "uid": 1, "message": "Register successful", "profile_id": "...", "cbh": 0 }
```

> 注册成功**不会**自动颁发 Remember Token，需引导用户跳转登录页（WebUI）；M.T. 路径由 WinnerProxy 后续流程处理。

#### 失败响应

| HTTP | message / error | 触发场景 | 路径 |
|------|----------------|----------|------|
| 400 | `Invalid auth type or token` | `auth_type` 为未知值，或声明 `manage` 但 token 与配置 M-T 不符 | 通用 |
| 400 | `Invalid or expired captcha` | 验证码错误或已使用 | WebUI（开启 captcha 时）|
| 400 | `Invalid email` | 邮箱格式不合法 | WebUI |
| 400 | `Username too short` | 用户名 < 3 字符 | 通用 |
| 400 | `Password too short` | 密码 < 6 字符 | 通用 |
| 400 | `invalid_mojang_uuid` | M.T. 路径下 `mojang_uuid` 格式错误 | M.T. |
| 400 | `mojang_uuid_required_for_existing_user` | M.T. 路径下用户已存在但未传 `mojang_uuid` | M.T. |
| 409 | `Email already registered` | 邮箱已被注册 | WebUI |
| 409 | `Username already taken` | 用户名已被占用 | WebUI |
| 409 | `username_already_bound` | M.T. 路径下决策树 2.a(mbe=0) / 2.c 撞名 | M.T. |

#### M.T. 路径决策树概要

- 1. 按 `mojang_uuid` 查 → 命中即幂等返回
- 2. 按 `username` 查：
  - 命中 + 有 `mojang_uuid` + `mojang_uuid=NULL`：
    - **`mbe=0` → 409**（HA 优先，WinnerProxy 收到 409 后踢 Mojang 玩家）
    - `mbe=1` → bind（写 `mojang_uuid` + `last_sign_at`，**保留** WebUI 用户 `password`/`email`/`cbh`）
  - 命中 + `mojang_uuid` 与已存相同 → 幂等返回
  - 命中 + `mojang_uuid` 与已存不同 → 409
  - 命中 + 无 `mojang_uuid` → 400
  - 未命中 → 新建 user（`mojang_uuid` 有则 `cbh=0`，否则 `cbh=1`；`mbe=0` 默认）

> WebUI 用户在 WebUI 个人设置里点"允许 Mojang 绑定"会调 `POST /user/mojang-bind-enable`，把 `mbe` 置为 1。

### 4.4 POST /user/declare-email

**所需 Token：** **Manage Token**（字段名 `mt`）

**请求体：**
```json
{
  "mt": "<Manage Token>",
  "email": "player@example.com",
  "playername": "PlayerOne"
}
```

**成功响应：**
```json
{
  "success": true,
  "message": "Email declared successfully",
  "data": {
    "uid": 1,
    "email": "player@example.com",
    "username": "PlayerOne"
  }
}
```

**失败响应：**
```json
{ "success": false, "message": "invalid manage token" }
```

> 该接口仅更新用户邮箱字段，不修改 `cbh` 状态。

### 4.5 GET /logout

**所需 Token：** **Remember Token**

```http
GET /logout?remember_token=<token>
```
或（推荐）：
```http
POST /logout
Content-Type: application/json
{ "remember_token": "<token>" }
```

**成功响应：**
```json
{ "success": true, "message": "Logged out" }
```

**前端处理：** 成功后清除本地保存的 Remember Token 与用户信息，跳转登录页。

### 4.4 图形验证码

> ⚠️ **重要变更（迁移须知）**：
> 图形验证码**完全由后端颁发**。前端**必须**使用后端图片（即把 `image_url` 拼成绝对 URL 放进 `<img src>`），并把用户在图上看到的字符作为 `captcha_code` 提交。
> **不要**再用 Canvas 自行绘制 / 自行生成 / 自行校验验证码——后端会在 `POST /register` 时强制比对前端提交的 `captcha_code` 与 Redis 中存储的正确答案，不一致直接返回 `400`，且该 captcha 立即失效。

**总开关** `security.enable_captcha`，默认 `true`。前端应在注册页加载时调用 `GET /captcha/enabled` 获取状态（`1` 启用 / `0` 关闭）以决定是否展示验证码输入框与是否调用 `/captcha`。

#### GET /captcha/enabled

**所需 Token：** 无

**响应：**
```json
{ "enabled": 1 }
```
- `enabled`：`1` 表示后端已开启图形验证码，`0` 表示关闭

#### POST /captcha

**所需 Token：** 无

**响应：**
```json
{
  "success": true,
  "token": "AbCdEfGh12345678WxYz",
  "image_url": "/captcha/image/AbCdEfGh12345678WxYz",
  "expires_in": 300
}
```
- `token`：本次验证码标识，注册时与 `captcha_code` 配对提交
- `image_url`：相对路径，前端需拼接后端 origin，如 `https://auth.example.com/captcha/image/<token>`，放进 `<img src>`
- `expires_in`：有效期（秒）

**未启用时响应（403）：**
```json
{ "success": false, "message": "Captcha is disabled" }
```

#### GET /captcha/image/:token

**所需 Token：** 无

**响应：** `200 OK` `Content-Type: image/png`（图片字节流）

**失败响应（404）：**
```json
{ "success": false, "message": "Captcha not found or expired" }
```

#### 注册流程时序

```text
T0  注册页加载 → POST /captcha → { token, image_url, expires_in }
T1  将 image_url 拼成绝对 URL 放入 <img src> → 浏览器 GET /captcha/image/<token> → image/png
T2  用户在 input 中输入图上 4 位字符
T3  用户点击注册按钮 → POST /register { email, username, password, captcha_token, captcha_code }
T4  后端校验通过 → 删 Redis 中该 token
T5  失败：后端返回 400 "Invalid or expired captcha" → 前端应重新调 /captcha 刷新图片
```

**字符集：** 后端生成时已剔除视觉易混淆字符 `0/O/1/I/L`，比对时大小写不敏感。

**前端必传字段**（开启图形验证码时，`POST /register` 请求体）：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `captcha_token` | string | 是 | `POST /captcha` 响应的 `token` |
| `captcha_code` | string | 是 | 用户在图上识别的字符（不区分大小写，前后空格会被自动 trim） |

> `POST /register` 的完整错误码矩阵（含 M.T. 路径）见 [§4.2](#42-post-register) "失败响应"小节。

---

## 5. 用户信息

### POST /user

**所需 Token：** **Remember Token**

**请求体：**
```json
{ "remember_token": "<Remember Token>", "uid": "1", "email": "user@example.com" }
```

> `uid` 与 `email` 联合校验（任意一个匹配登录用户即可，建议同时传）

**运维代开**（M.T. 需额外声明 `auth_type` + 指定目标用户，玩家模式下 `uid`/`email` 被忽略）：
```json
{ "remember_token": "<Manage Token>", "uid": "42", "auth_type": "manage" }
```

**成功响应：**
```json
{
  "success": true,
  "message": "获取用户信息成功",
  "data": { "uid": 1, "email": "user@example.com", "username": "PlayerOne", "avatar": "", "verified": true }
}
```
- `verified`：邮箱是否已验证（前端可据此引导用户完成验证）

**失败响应（401）：**
```json
{ "success": false, "message": "Invalid token" }
```

---

### POST /user/mojang-bind-enable

**所需 Token：** **Remember Token**（玩家自开）；**Manage Token + uid/email**（运维代开）

开启用户的 **MBE（Mojang Bind Enabled）** 开关。`mbe=1` 后，未绑定的 Mojang 正版玩家撞名进服时，HA `/register`（M.T. 路径）会**允许绑定**（保留 WebUI 用户的 `password`/`email`/`cbh`，只写 `mojang_uuid` + `last_sign_at`）；`mbe=0` 时 Mojang 玩家撞名收到 `409 username_already_bound`，被踢出（HA 优先）。**幂等**。

**请求体（玩家自开）：**
```json
{ "remember_token": "<Remember Token>" }
```

**请求体（运维代开，用 uid）：**
```json
{ "remember_token": "<Manage Token>", "uid": "42", "auth_type": "manage" }
```

或（用 email）：
```json
{ "remember_token": "<Manage Token>", "email": "user@example.com", "auth_type": "manage" }
```

**成功响应：**
```json
{ "success": true, "message": "Mojang bind enabled", "data": { "uid": 42, "mbe": 1 } }
```

**失败响应：**
- `无效的鉴权类型或token` — 声明了 `auth_type="manage"` 但 token 与配置 M-T 不符（或未知 `auth_type` 值）
- `未登录或登录已过期` — Remember Token 缺失
- `Manage Token 需要指定 uid 或 email` — M.T. 路径下未指定目标用户
- `用户不存在或token无效` — Token 无效或对应用户不存在

> 玩家绑定成功后 `mbe` 字段意义消失（`mojang_uuid` 一旦设置，§3.4 2.a 不再触发），但本端点不主动重置 `mbe`，便于查询当前授权状态。

---

## 6. 邮箱验证

### POST /email-verification

**所需 Token：** 视 action 而定

| action | 所需 Token | 说明 |
|--------|-----------|------|
| `send-test-email` | 无 | 调试用，直接发邮件，需 `to`/`subject`/`message` |
| `send-verification-code` | 无 | 服务器生成 6 位验证码，存 Redis 10 分钟，发邮件给用户 |
| `verify-code` | **Email Verification Code**（6 位数字） | 校验用户输入的验证码 |

**发送验证码：**
```json
{ "action": "send-verification-code", "email": "user@example.com" }
```
成功响应：`{ "success": true, "message": "Verification code sent" }`

**校验验证码：**
```json
{ "action": "verify-code", "email": "user@example.com", "code": "123456" }
```
成功响应：`{ "success": true, "message": "Email verified" }`
失败响应（400）：`{ "success": false, "message": "Invalid verification code" }`

> 验证码 10 分钟有效，校验通过后从 Redis 删除（单次使用）。

---

## 7. TOTP 两步验证

> `/totpgen`（`GET /totpgen?secret=xxx`）是**后端调试接口**，用于根据 TOTP Secret 直接生成动态口令。**生产前端不要调用**此接口 —— 用户应从已安装的 Authenticator 应用（Google Authenticator / Microsoft Authenticator / Authy 等）读取 6 位动态口令。

### POST /totp/setup

**所需 Token：** **Remember Token**（字段名 `remtoken`）

**请求体：**
```json
{ "email": "user@example.com", "remtoken": "<Remember Token>" }
```

> 运维代开（M.T.）：`{ "email": "<目标邮箱>", "remtoken": "<Manage Token>", "auth_type": "manage" }`

**成功响应：**
```json
{ "success": true, "totpkey": "<TOTP Secret，Base32 字符串>" }
```

**前端处理：**
1. 拿到 `totpkey` 后，在 UI 上展示为 Base32 字符串 + 二维码（前端用 qrcode.js 等库将 `otpauth://totp/...` 编码为二维码）
2. 用户用 Authenticator 扫码添加账号
3. 引导用户输入 Authenticator 显示的 6 位动态口令，调 `/totp/verify` 完成绑定

### POST /totp/verify

**所需 Token：** **TOTP Passcode**（6 位数字，字段名 `passcode`）

**请求体：**
```json
{ "email": "user@example.com", "passcode": "123456" }
```

**成功响应：**
```json
{ "success": true, "email": "user@example.com", "rt": "<Remember Token>" }
```

> 响应中的 `rt` 字段为该用户的 Remember Token（如该用户原本没有，服务端会新签发一个）。

**失败响应：**
```json
{ "success": false, "message": "Invalid passcode" }
```

### POST /totp/hasbeenenabled

**所需 Token：** **Remember Token**（字段名 `rt`）

查询指定用户是否已开启 TOTP。

**请求体：**
```json
{ "uid": "1", "rt": "<Remember Token>" }
```

> 运维代开（M.T.）：`{ "uid": "<目标 uid>", "rt": "<Manage Token>", "auth_type": "manage" }`

**成功响应：**
```json
{ "success": true, "enabled": 1 }
```
- `enabled`：`1` = 已开启；`0` = 未开启

---

## 8. 用户资料管理

### POST /change-username

**所需 Token：** **Remember Token**（字段名 `remember_token`）

**请求体：**
```json
{ "remember_token": "<Remember Token>", "username": "NewName" }
```

> 运维代开（M.T.）：`{ "remember_token": "<Manage Token>", "uid": "42", "auth_type": "manage", "username": "NewName" }`（uid/email 二选一）

**成功响应：**
```json
{ "success": true, "message": "Username updated" }
```

### POST /change-profile-name

**所需 Token：** **Remember Token**（字段名 `remember_token`）

**请求体：**
```json
{ "remember_token": "<Remember Token>", "profile_id": "<uuid>", "name": "NewPlayerName" }
```

> 运维代开（M.T.）：同上加 `"auth_type": "manage"` 并指定 `uid` 或 `email`（二选一）。

**成功响应：**
```json
{ "success": true, "message": "Profile name updated" }
```

---

## 9. 纹理管理

> 三个端点均使用 **Remember Token** 鉴权。纹理 URL 由后端托管，前端只需展示。
>
> 运维代开（M.T.）同样支持：请求中加 `"auth_type": "manage"` 并指定 `uid` 或 `email`（二选一），即可代操作指定用户的纹理。

### POST /texture/upload

**所需 Token：** **Remember Token**

**请求格式：** `multipart/form-data`

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | 用户登录令牌 |
| `profile_id` | string | 否 | 角色 ID，默认取用户的第一个角色 |
| `texture_type` | string | 是 | `skin` 或 `cape` |
| `model` | string | 否 | `default`（默认）或 `slim`（纤细），仅皮肤有效 |
| `file` | file | 是 | PNG 格式纹理文件 |

**curl 示例：**
```bash
curl -X POST https://auth.example.com/texture/upload \
  -F "remember_token=<Remember Token>" \
  -F "texture_type=skin" \
  -F "model=slim" \
  -F "file=@/path/to/skin.png"
```

**成功响应：**
```json
{
  "success": true,
  "message": "材质上传成功",
  "data": { "profile_id": "<uuid>", "texture_type": "skin" }
}
```

**失败响应：**
```json
{ "success": false, "message": "无效的材质类型，只能是 skin 或 cape" }
```

### POST /texture/delete

**所需 Token：** **Remember Token**

**请求体：**
```json
{ "remember_token": "<Remember Token>", "profile_id": "<uuid>", "texture_type": "skin" }
```

**成功响应：**
```json
{ "success": true, "message": "材质删除成功", "data": { "profile_id": "<uuid>", "texture_type": "skin" } }
```

### POST /texture/get

**所需 Token：** **Remember Token**

**请求体：**
```json
{ "remember_token": "<Remember Token>", "profile_id": "<uuid>" }
```

> `profile_id` 缺省时取用户的第一个角色。

**成功响应：**
```json
{
  "success": true,
  "message": "获取材质信息成功",
  "data": {
    "profile_id": "<uuid>",
    "textures": [
      { "texture_type": "skin", "url": "https://auth.example.com/textures/abc123...", "model": "slim" },
      { "texture_type": "cape", "url": "https://auth.example.com/textures/def456..." }
    ]
  }
}
```
- `textures[].texture_type`：`skin` 或 `cape`
- `textures[].url`：纹理文件绝对 URL
- `textures[].model`：仅皮肤有此字段，`default` 或 `slim`

---

## 10. 典型前端流程

### 登录（含 TOTP）

```text
1. 用户输入 email + password
2. POST /login
3. 成功且 totp=0 → 保存 token，跳转首页
4. 成功且 totp=1 → 保存 uid/email（暂不存 token），跳转 TOTP 校验页
5. 用户输入 Authenticator 6 位动态口令
6. POST /totp/verify { email, passcode }
7. 成功 → 拿到 rt（Remember Token），保存并跳转首页
```

### 注册

```text
1. 注册页加载 → POST /captcha 拿 token + image_url
2. 显示 <img src="<origin><image_url>">
3. 用户填写 email/username/password/captcha_code
4. POST /register 携带 captcha_token + captcha_code
5. 失败且 message 含 "captcha" → 重新调 /captcha 刷新
6. 成功 → 引导用户跳转登录页
```

### 已登录态刷新

```text
页面刷新 / 路由切换 →
  - 若 localStorage 有 Remember Token：直接带 token 调 /user 验证有效性
  - 401 → 清除 token，跳转登录页
  - 200 → 拉取最新用户信息
```

### 头像/皮肤展示

```text
1. POST /texture/get 拿 textures 数组
2. 遍历 textures，按 texture_type 渲染：
   - skin → <img src="textures[i].url">，Minecraft 客户端会自动从 Yggdrasil 拉取
   - cape → <img src="textures[i].url">
```

---

## 11. 不在本文档范围的端点

以下端点由 **Minecraft 客户端 / Authlib-Injector** 直接调用，**前端不要**直接调用：

- `/authserver/*`（authenticate / refresh / validate / invalidate / signout）
- `/sessionserver/*`（minecraft/join / hasJoined / profile/:uuid）
- `/api/profiles/minecraft`
- `/api/user/profile/:uuid/:textureType`
- `/textures/:hash`（纹理公开下载）

如需了解这些端点的内部行为，参见 [`../docs/endpoints/yggdrasil.md`](../docs/endpoints/yggdrasil.md)。
