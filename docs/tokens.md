# Token 体系

本文档梳理项目内涉及的全部 Token 及其生命周期。**这是本项目最复杂的部分**，强烈建议任何接入方都先读完。

> 本文档合并了原 `API_DOC.md` 中 "Token 体系总览" 与 "Token 状态机与生命周期" 两节。

## 目录

1. [Token 总览](#1-token-总览) — 各种 Token 的字段名、长度、用途
2. [状态机](#2-状态机) — `tokens.state` 的三种状态及转移规则
3. [`/authenticate` 幂等与踢人](#3-authenticate-幂等与踢人)
4. [`/refresh` 抢回与踢人](#4-refresh-抢回与踢人)
5. [各端点对 `temporarily_invalid` 的处理](#5-各端点对-temporarily_invalid-的处理)
6. [后台清理任务](#6-后台清理任务)

---

## 1. Token 总览

| Token 名称 | 字段名（请求/响应） | 长度 | 用途 | 颁发端点（响应字段） | 使用端点（请求字段） |
|-----------|--------------------|------|------|--------------------|--------------------|
| **Remember Token**（本站会话令牌） | `remember_token` / `remtoken` / `rt` / 登录响应的 `token` | 32 字节随机串 | 本站业务系统的登录会话凭证 | `POST /login`（响应 `token`） | `GET /logout`、`POST /user`、`POST /change-username`、`POST /change-profile-name`、`POST /totp/setup`、`POST /totp/hasbeenenabled`；`POST /totp/verify` 成功后会在响应中回传该 token（`rt` 字段） |
| **Manage Token（M-T）**（运维超级 remtoken） | 任意 remtoken 字段名 | 32 字节随机串（`utils.GenerateRandomToken(32)`） | 由 `config.yaml` 中 `manage.token` 持久化；可作为任意用户的 remtoken，但调用方必须额外声明 `auth_type: "manage"` 并提供 `uid` 或 `email` 指定目标用户 | 首次启动时由 [`controllers/startup_controller.go`](../controllers/startup_controller.go) 的 `generateManageToken` 随机生成 | 与 Remember Token 相同的所有站点端点；`isManage` 分支会跳过 `WHERE remember_token=?` 校验 |
| **Yggdrasil Access Token** | `accessToken` | 随机串（`utils.GenerateAccessToken`） | Yggdrasil API 的访问令牌，由 Minecraft 客户端在加入服务器时携带 | `POST /authserver/authenticate`、`POST /authserver/refresh` | `POST /authserver/refresh`、`POST /authserver/validate`、`POST /authserver/invalidate`、`POST /sessionserver/session/minecraft/join`、`PUT/DELETE /api/user/profile/:uuid/:textureType`（通过 `Authorization: Bearer <accessToken>` 请求头传递） |
| **Yggdrasil Client Token** | `clientToken` | 随机串（`utils.GenerateClientToken`，可由客户端自传） | Yggdrasil API 的客户端标识，必须与 AccessToken 配对使用 | `POST /authserver/authenticate`（请求可传 / 响应回传） | `POST /authserver/authenticate`、`POST /authserver/refresh`、`POST /authserver/validate`、`POST /authserver/invalidate` |
| **Email Verification Code**（邮箱验证码） | `code` | 6 位数字 | 校验用户邮箱所有权，存于 Redis，10 分钟有效 | `POST /email-verification`（`action=send-verification-code`，通过邮件发送） | `POST /email-verification`（`action=verify-code`） |
| **Captcha Token**（图形验证码标识） | `token` / `image_url` | 20 字符随机串 | 标识一次图形验证码会话 | `POST /captcha`（响应 `token` + `image_url`） | `POST /register`（`captcha_token` 字段，仅在 `enable_captcha=true` 时必填） |
| **Captcha Code**（图形验证码答案） | `captcha_code` | 5 位字符（字母+数字，已剔除 `0/O/1/I/L`） | 用户在图上识别后回填的答案 | 由 `POST /captcha` 颁发的图片 | `POST /register`（`captcha_code` 字段） |
| **TOTP Secret** | `totpkey`（响应）/ `secret`（`/totpgen` 查询参数） | 32 字节 Base32 串 | 用户与服务器共享的 TOTP 种子密钥 | `POST /totp/setup`（响应 `totpkey`） | `GET /totpgen?secret=<totpkey>`（调试接口） |
| **TOTP Passcode** | `passcode` | 6 位数字 | 一次性 6 位动态口令 | `GET /totpgen?secret=<totpkey>`（响应明文） | `POST /totp/verify` |

> ⚠️ **重要区分**：
> 1. `Remember Token`（本站）≠ `Yggdrasil Access Token`（Minecraft）。两者完全独立。
> 2. `remember_token` / `remtoken` / `rt` 是同一类 Token 的不同字段名，含义相同。
> 3. 邮箱验证码（`code`）和 TOTP Passcode（`passcode`）都是 6 位数字，但用途完全不同。
> 4. **Manage Token（M-T）** 存于 `config.yaml`，**不是用户级 remember_token**。使用 M-T 调任何需要 remtoken 的端点时，**必须**额外声明 `auth_type: "manage"` 并传 `uid` 或 `email` 指定目标用户；否则后端返回 `Manage Token 需要指定 uid 或 email`。

### auth_type 声明（Token 类型判别）

每个接受 remtoken 的站点端点都支持**可选的 `auth_type` 字段**（JSON / 查询参数 / 表单均可，按 `remember_token` 相同的收集顺序识别）。它显式声明提交的 token 归属：

| `auth_type` 值 | 后端处理 |
|---------------|---------|
| 缺省 / `remember` | **默认**。按 Remember Token 处理：`WHERE remember_token = ?` 查库定位用户 |
| `manage` | 按 Manage Token 处理：token 必须等于 `config.AppConfig.Manage.Token`，且必须再提供 `uid` 或 `email` 指定目标用户 |

判别逻辑集中在 [`controllers/auth_controller.go`](../controllers/auth_controller.go) 的 `isManageRequest`：声明 `manage` 且 token 与配置 M-T 相符 → `isManage=true`；否则（未知值，或 `manage` 但 token 不符）→ 直接拒绝。**后端不再**通过"token 恰好等于 M-T"自动升级为运维模式——未声明 `auth_type` 时，即使 token 恰好等于 M-T，也一律走 remember-token 数据库校验（查无此行则报"用户不存在或token无效"）。

---

## 2. 状态机

`tokens.state` 字段对应 `models.Token.State` 枚举，共 **3 个值**。

| 状态 | 含义 | 哪些端点接受 |
|------|------|-------------|
| `valid` | 完全有效 | 全部 |
| `temporarily_invalid` | 被另一个 client 踢下；只能 `/refresh` 抢回，`/validate` `/join` 全部拒绝 | 仅 `/authserver/refresh` |
| `invalid` | 永久失效 | 无 |

### 状态转移图

```
                                /authenticate（同 clientToken）
   ┌─────────────────────────────── valid ◄─────────────────────────────┐
   │                                  ▲                                 │
   │                                  │                                 │
   │ /authenticate（不同 clientToken）│ /refresh 成功                   │
   │ /refresh 后踢其他 client         │                                 │
   ▼                                  │                                 │
 temporarily_invalid ─────────────── /refresh 抢回 ────► 新行 valid     │
                                                                        │
   ▼   ▼   ▼                                                           │
 invalid (由 /invalidate /signout /expiry 检查置位，cleanup 物理删除) ───┘
                                                                        │
                                            (cleanup) ────► DELETE FROM tokens
```

### 状态置位时机

| 状态 | 何时被置位 |
|------|-----------|
| `valid` | `/authenticate` 成功插入新行；`/refresh` 成功插入新行；`/refresh` 抢回后旧行 → `invalid`（不是 `temporarily_invalid`） |
| `temporarily_invalid` | `/authenticate` 时不同 `clientToken` 把该用户**其他 client** 的 `valid` 行踢到此状态；`/refresh` 成功后把**其他 client** 的 `valid` 行踢到此状态 |
| `invalid` | `/invalidate` 主动调用；`/signout` 吊销该用户所有 token；`/authserver/refresh` 中旧 accessToken → `invalid`；`expiry` 检查命中 |

---

## 3. `/authenticate` 幂等与踢人

### 复用旧行（同 `clientToken`）

`/authserver/authenticate` 在以下**三条同时满足**时复用旧行（不插入新行）：

1. 数据库存在一行 `state='valid'` 且 `issued_at + expires_in_days*86400000 > now()`
2. 该行 `user_id` 与本次登录用户一致
3. 该行 `client_token` 与本次请求 `clientToken` 一致

复用行为：

- 响应 `accessToken` 直接返回旧值
- 旧行 `issued_at` 拨到 `now()`，有效期顺延 `expires_in_days`（默认 15 天）
- `selectedProfile` 沿用旧行绑定的 profile

### 互踢（不同 `clientToken`）

不满足复用条件时（不同 `clientToken` / 无有效行 / 上一行已过期）：

- 事务性 UPDATE：该用户所有 `state='valid' AND client_token != ?` 的行 → `temporarily_invalid`
- 插入新行 `state='valid'`

### 时序示例

```text
T0  客户端 A 用 clientToken=C-A 登录 → 插入 row#1 {access=tok1, client=C-A, state=valid}
T1  客户端 A 重启后再用 clientToken=C-A 登录
    → GetValidTokenByClientToken(U, C-A) 命中 row#1
    → 响应 accessToken=tok1（不是新生成的）
    → UPDATE row#1 SET issued_at=now() WHERE access_token=tok1
    → 数据库无新增行

T2  客户端 B 用 clientToken=C-B 登录（row#1 仍是 valid）
    → GetValidTokenByClientToken(U, C-B) 返回 nil
    → UPDATE tokens SET state='temporarily_invalid'
        WHERE user_id=U AND client_token != C-B AND state='valid'   ← row#1 被踢
    → INSERT row#2 {access=tok2, client=C-B, state=valid}
    → 响应 accessToken=tok2
```

之后 A 想上线会被 `/validate` 与 `/join` 拒绝（`temporarily_invalid`），但 A 可调 `/refresh` 抢回——见下条。

---

## 4. `/refresh` 抢回与踢人

被踢到 `temporarily_invalid` 的 client 仍可调用 `/authserver/refresh` 取回控制权：

- `ValidateTokenForRefresh` 接受 `state IN ('valid', 'temporarily_invalid')`
- 旧 accessToken → `state='invalid'`
- 当前用户**其他** client 的 `state='valid'` 行 → `temporarily_invalid`
- 签发新 accessToken → `state='valid'`

> 注意：与 `/authenticate` 不同，`/refresh` 时**调用方**的旧行直接进入 `invalid`（不是 `temporarily_invalid`），因为同 client 的旧 token 不再需要"暂存"。

### 时序示例

```text
T3  POST /authserver/refresh {accessToken: tok1, clientToken: C-A}
    → ValidateTokenForRefresh(tok1, C-A) 命中 row#1（state=temporarily_invalid 仍通过）
    → InvalidateToken(tok1)                  → row#1 state=invalid
    → MarkOtherClientTokensTemporarilyInvalid(U, C-A)
        → row#2 (client=C-B, state=valid)   → state=temporarily_invalid
    → INSERT row#3 {access=tok3, client=C-A, state=valid}
    → 响应 accessToken=tok3
```

B 这时也会被 `/validate` `/join` 拒绝，但保留 `/refresh` 抢回的能力。

---

## 5. 各端点对 `temporarily_invalid` 的处理

| 端点 | 接受的 `state` | `temporarily_invalid` 的行为 |
|------|---------------|------------------------------|
| `POST /authserver/validate` | `valid` | 403 ForbiddenOperationException |
| `POST /sessionserver/session/minecraft/join` | `valid` | 403 ForbiddenOperationException |
| `POST /authserver/refresh` | `valid`, `temporarily_invalid` | 成功，触发"抢回"流程 |
| `POST /authserver/invalidate` | `valid` | 403（已被踢的 token 不能 invalidate） |
| `POST /authserver/signout` | 凭 username/password，与 token 状态无关 | 吊销该用户所有 token（含 valid / temporarily_invalid / invalid） |

---

## 6. 后台清理任务

`main.go` 启动时 + 每 1 小时触发一次 [`controllers/token_cleanup_controller.go`](../controllers/token_cleanup_controller.go) 的 `runOnce`，逻辑见 [`services/auth_service.go`](../services/auth_service.go)：

- DELETE `state='invalid'` 的行
- DELETE `issued_at + expires_in_days*86400000 < now()` 的行（涵盖 valid / temporarily_invalid 的过期行）
- 删除数量写入日志：`[TokenCleanup] removed N expired/invalid tokens`

> **为什么不直接 DELETE `valid` 的过期行？** 因为 GORM 软删除 + 状态机配合更安全：先置 `invalid`，下一次 cleanup 才物理删除，这样审计、并发竞态下都不会出错。
