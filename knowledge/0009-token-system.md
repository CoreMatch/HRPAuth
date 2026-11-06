> 来源：HA-Contract@bab2a05（2026-11-07），原路径 docs/dev/HRPAuth/tokens.md（契约仓知识库回迁）。

# Token System

This document clarifies all Tokens and their lifecycles involved in the project. **This is the most complex part of the project**, and it is strongly recommended that any integrator read it in full.

> Since the 2026-10 service split, the Yggdrasil token system (sections 1–5 and 6.1) is implemented by the [`HRPAuth-Yggdrasil-API`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API) service; the OAuth2 token system (sections 6.2–6.3) remains in the [`HRPAuth`](https://github.com/CoreMatch/HRPAuth) core.

> This document merges the sections "Token System Overview" and "Token State Machine and Lifecycle" from the original `API_DOC.md`.

## Contents

1. [Token Overview](#1-token-overview) — Field names, lengths, and purposes of various Tokens
2. [State Machine](#2-state-machine) — The three states of `tokens.state` and transition rules
3. [`/authenticate` Idempotency and Kicking](#3-authenticate-idempotency-and-kicking)
4. [`/refresh` Reclaiming and Kicking](#4-refresh-reclaiming-and-kicking)
5. [Handling of `temporarily_invalid` by Endpoints](#5-handling-of-temporarily_invalid-by-endpoints)
6. [Background Cleanup Tasks](#6-background-cleanup-tasks)
7. [Per-User Token Count Limit](#7-per-user-token-count-limit) — `max_tokens_per_user` quota and oldest-first revocation

---

## 1. Token Overview

| Token Name | Field Name (Request/Response) | Length | Purpose | Issuing Endpoint (Response Field) | Usage Endpoint (Request Field) |
|------------|-------------------------------|--------|---------|-----------------------------------|-------------------------------|
| **OAuth2 Access Token** (Site Business API) | `Authorization: Bearer <access_token>` | 32-byte random string | Site-side business API credential. Used by both user tokens and service tokens. | `POST /oauth/token`, `POST /oauth/login-ticket`, `POST /totp/verify` | `/logout`, `/user`, `/change-username`, `/totp/*`, `/user/security/*`, and other site business endpoints |
| **OAuth2 Refresh Token** | `refresh_token` | 32-byte random string | Refresh site-side user session | `POST /oauth/token` (`grant_type=authorization_code|refresh_token`), `POST /oauth/login-ticket`, `POST /totp/verify` | `POST /oauth/token` (`grant_type=refresh_token`) |
| **OAuth2 Login Ticket** | `login_ticket` | 24-byte random string | Short-lived password-proof ticket for first-party MFA; may be consumed by TOTP, WebAuthn, Email 2FA, or Recovery Key | `POST /oauth/login-ticket` | `POST /totp/verify`, `POST /webauthn/2fa/begin`, `POST /email-2fa/send`, `POST /user/security/recovery-key/verify` |
| **WebAuthn Flow ID** | `flow_id` | 24-byte random string | Server-side identifier for a WebAuthn begin/finish ceremony | `POST /webauthn/*/begin` | `POST /webauthn/*/finish` |
| **OAuth2 Authorization Code** | `code` | 32-byte random string | Authorization code for `authorization_code + PKCE` | `POST /oauth/authorize/decision` | `POST /oauth/token` (`grant_type=authorization_code`) |
| **Built-in Super Client Secret** | `client_secret` | 32-byte random string | Low-friction service-to-service credential for `client_credentials` | Generated into `config.yaml > oauth2.super_client_secret` | `POST /oauth/token` (`grant_type=client_credentials`) |
| **Yggdrasil Access Token** | `accessToken` | Random string (`utils.GenerateAccessToken`) | Access token for Yggdrasil API, carried by Minecraft client when joining a server | `POST /authserver/authenticate`, `POST /authserver/refresh` | `POST /authserver/refresh`, `POST /authserver/validate`, `POST /authserver/invalidate`, `POST /sessionserver/session/minecraft/join`, `POST /texture/get`, `POST /texture/upload/:uuid`, `DELETE /texture/delete/:uuid/:type` (via `Authorization: Bearer <accessToken>` header). The standard `PUT/DELETE /api/user/profile/:uuid/:textureType` write surface is not directly exposed by the current implementation. |
| **Yggdrasil Client Token** | `clientToken` | Random string (`utils.GenerateClientToken`, can be provided by client) | Client identifier for Yggdrasil API, must be paired with AccessToken | `POST /authserver/authenticate` (request can pass / response returns) | `POST /authserver/authenticate`, `POST /authserver/refresh`, `POST /authserver/validate`, `POST /authserver/invalidate` |
| **Email Verification Code** | `code` | 6-digit number | Verifies user email ownership, stored in Redis, valid for 10 minutes | `POST /email-verification` (`action=send-verification-code`, sent via email) | `POST /email-verification` (`action=verify-code`) |
| **Captcha Token** (Graphical Captcha ID) | `token` / `image_url` | 20-character random string | Identifies a graphical captcha session | `POST /captcha` (response `token` + `image_url`) | `POST /register` (`captcha_token` field, required only for anonymous user registration when `enable_captcha=true`) |
| **Captcha Code** (Graphical Captcha Answer) | `captcha_code` | 4-character string (alphanumeric, excluding `0/O/1/I/L`) | Answer filled in by the user after identifying it on the image | Image issued by `POST /captcha` | `POST /register` (`captcha_code` field) |
| **TOTP Secret** | `totpkey` (response) / `secret` (`/totpgen` query param) | 32-byte Base32 string | Shared TOTP seed key between user and server | `POST /totp/setup` (response `totpkey`) | `GET /totpgen?secret=<totpkey>` (debug interface) |
| **TOTP Passcode** | `passcode` | 6-digit number | One-time 6-digit dynamic passcode | `GET /totpgen?secret=<totpkey>` (response plaintext) | `POST /totp/verify` |
| **Maintenance Token** (M-T) | `Authorization: Bearer <token>` | 32-byte random (64 hex chars), generated at first startup | Legacy super credential; when presented as a Bearer token it satisfies ops (level 2) auth checks | Generated into `config.yaml > manage.token` | Any endpoint that requires ops (level 2) auth |

> ⚠️ **Important Distinctions**:
> 1. Site business APIs now authenticate with **OAuth2 Bearer tokens**, not `remember_token`.
> 2. `Manage Token (M-T)` is now a **legacy compatibility credential** and should not be used for new integrations.
> 3. Service-to-service proxy operations use `client_credentials` and endpoint-level scopes such as `user.claim.as-service` or `user.force-bind.as-service`. Registered consumers: HRPAuth-Yggdrasil-API (`core_api` credentials; claim / force-bind flows) and, since 2026-11-05, MatchCoreArena-Server (`auth.hrpauth.service_client_id/secret`; resolves match participants via `GET /services/users/{uuid}`, ops level — the gate is the auth level, the token carries the service scope set including `user.read.as-service`). (Former orphan scopes were removed from the registry on 2026-11-05 in two batches after confirming no endpoint consumed them: `register.manage`, `profile.change-name(.as-service)`, then `user.mojang-bind-enable(.as-service)`, `user.mojang-bind-disable.as-service`, `totp.status(.as-service)`, `email-2fa.status(.as-service)` — the Mojang bind flow lives on the Yggdrasil-API `/player` endpoints, which do not consume core scopes.)
> 4. `Yggdrasil Access Token` remains completely independent from site OAuth2 access tokens.
> 5. Database scopes (`db.line.read`, `db.line.write`, `db.line.delete`, `db.table.create`, `db.table.delete`) are **NOT** included in the built-in super client by default. They must be explicitly granted via `oauth2.super_client_extra_scopes` in `config.yaml`.

---

## 2. State Machine

The `tokens.state` field corresponds to the `models.Token.State` enum, with **3 possible values**.

| State | Meaning | Which Endpoints Accept |
|-------|---------|------------------------|
| `valid` | Fully valid | All |
| `temporarily_invalid` | Kicked by another client; can only be reclaimed via `/refresh`, `/validate` and `/join` are all rejected | Only `/authserver/refresh` |
| `invalid` | Permanently invalid | None |

### State Transition Diagram

```
                                /authenticate (same clientToken)
   ┌─────────────────────────────── valid ◄─────────────────────────────┐
   │                                  ▲                                 │
   │                                  │                                 │
   │ /authenticate (diff clientToken) │ /refresh success                │
   │ /refresh kicks other clients      │                                 │
   ▼                                  │                                 │
 temporarily_invalid ─────────────── /refresh reclaim ────► New row valid   │
                                                                        │
   ▼   ▼   ▼                                                           │
 invalid (set by /invalidate /signout /expiry check, physical delete by cleanup) ───┘
                                                                        │
                                            (cleanup) ────► DELETE FROM tokens
```

### State Setting Timing

| State | When it is set |
|-------|----------------|
| `valid` | Successful insertion of a new row by `/authenticate`; successful insertion of a new row by `/refresh`; old row → `invalid` (not `temporarily_invalid`) after `/refresh` reclaim. |
| `temporarily_invalid` | During `/authenticate`, a different `clientToken` kicks **other client** `valid` rows of the user to this state; after successful `/refresh`, **other client** `valid` rows are kicked to this state. |
| `invalid` | Active call to `/invalidate`; `/signout` revokes all tokens for the user; old accessToken → `invalid` during `/authserver/refresh`; hit by `expiry` check. |

---

## 3. `/authenticate` Idempotency and Kicking

### Reusing Old Rows (Same `clientToken`)

`/authserver/authenticate` reuses an old row (no new row inserted) when the following **three conditions are simultaneously met**:

1. A row exists in the database with `state='valid'` and `issued_at + expires_in_days*86400000 > now()`.
2. The `user_id` of that row matches the user logging in.
3. The `client_token` of that row matches the `clientToken` of the current request.

Reuse behavior:

- The `accessToken` in the response returns the old value directly.
- The `issued_at` of the old row is updated to `now()`, and the validity is extended by `expires_in_days` (default 15 days).
- `selectedProfile` uses the profile bound to the old row.

### Mutual Kicking (Different `clientToken`)

When reuse conditions are not met (different `clientToken` / no valid row / previous row expired):

- Transactional UPDATE: All rows for this user with `state='valid' AND client_token != ?` → `temporarily_invalid`.
- Insert a new row with `state='valid'`.

### Timeline Example

```text
T0  Client A logs in with clientToken=C-A → Inserts row#1 {access=tok1, client=C-A, state=valid}
T1  Client A logs in again with clientToken=C-A after restart
    → GetValidTokenByClientToken(U, C-A) hits row#1
    → Response accessToken=tok1 (not newly generated)
    → UPDATE row#1 SET issued_at=now() WHERE access_token=tok1
    → No new row added to database

T2  Client B logs in with clientToken=C-B (row#1 is still valid)
    → GetValidTokenByClientToken(U, C-B) returns nil
    → UPDATE tokens SET state='temporarily_invalid'
        WHERE user_id=U AND client_token != C-B AND state='valid'   ← row#1 is kicked
    → INSERT row#2 {access=tok2, client=C-B, state=valid}
    → Response accessToken=tok2
```

Later, if A wants to go online, it will be rejected by `/validate` and `/join` (`temporarily_invalid`), but A can call `/refresh` to reclaim — see below.

---

## 4. `/refresh` Reclaiming and Kicking

A client kicked to `temporarily_invalid` can still call `/authserver/refresh` to regain control:

- `ValidateTokenForRefresh` accepts `state IN ('valid', 'temporarily_invalid')`.
- Old accessToken → `state='invalid'`.
- **Other** client `state='valid'` rows for the current user → `temporarily_invalid`.
- Issue new accessToken → `state='valid'`.

> Note: Unlike `/authenticate`, during `/refresh`, the **caller's** old row goes directly to `invalid` (not `temporarily_invalid`), because the old token for the same client no longer needs to be "temporarily stored".

### Timeline Example

```text
T3  POST /authserver/refresh {accessToken: tok1, clientToken: C-A}
    → ValidateTokenForRefresh(tok1, C-A) hits row#1 (state=temporarily_invalid still passes)
    → InvalidateToken(tok1)                  → row#1 state=invalid
    → MarkOtherClientTokensTemporarilyInvalid(U, C-A)
        → row#2 (client=C-B, state=valid)   → state=temporarily_invalid
    → INSERT row#3 {access=tok3, client=C-A, state=valid}
    → Response accessToken=tok3
```

B will now also be rejected by `/validate` and `/join`, but retains the ability to reclaim via `/refresh`.

---

## 5. Handling of `temporarily_invalid` by Endpoints

| Endpoint | Accepted `state` | Behavior for `temporarily_invalid` |
|----------|------------------|------------------------------------|
| `POST /authserver/validate` | `valid` | 403 ForbiddenOperationException |
| `POST /sessionserver/session/minecraft/join` | `valid` | 403 ForbiddenOperationException |
| `POST /authserver/refresh` | `valid`, `temporarily_invalid` | Success, triggers "reclaim" process |
| `POST /authserver/invalidate` | `valid` | 403 (kicked tokens cannot be invalidated) |
| `POST /authserver/signout` | Based on username/password, unrelated to token state | Revokes all tokens for the user (including valid / temporarily_invalid / invalid). **Rate limited** by the same Redis counter as `/authserver/authenticate` (`security.rate_limit_max_attempts` / `security.rate_limit_window_sec`) to prevent password enumeration. |

---

## 6. Background Cleanup Tasks

### 6.1 Yggdrasil Token Cleanup

`runOnce` in [`controllers/token_cleanup_controller.go`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API/blob/main/controllers/token_cleanup_controller.go) is triggered at `main.go` startup + every 1 hour. Logic in [`services/auth_service.go`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API/blob/main/services/auth_service.go):

- DELETE rows where `state='invalid'`.
- DELETE rows where `issued_at + expires_in_days*86400000 < now()` (covers expired valid / temporarily_invalid rows).
- Deletion count is logged: `[TokenCleanup] removed N expired/invalid tokens`.

> **Why not directly DELETE expired `valid` rows?** Because GORM soft delete + state machine coordination is safer: set to `invalid` first, then physically delete during the next cleanup. This ensures no errors during auditing or concurrent race conditions.

### 6.2 OAuth2 Token Cleanup

`runOnce` in [`controllers/oauth2_cleanup_controller.go`](https://github.com/CoreMatch/HRPAuth/blob/main/controllers/oauth2_cleanup_controller.go) is triggered at `main.go` startup + every 24 hours. Logic in [`services/oauth2_service.go`](https://github.com/CoreMatch/HRPAuth/blob/main/services/oauth2_service.go):

**Access Token** (`oauth2_access_tokens`):
- DELETE rows where `expires_at < now()` — token 已过期。
- DELETE rows where `revoked_at IS NOT NULL` — token 已被撤销（如用户主动登出、刷新轮换）。
- 日志: `[OAuth2Cleanup] removed N expired/revoked access tokens`。

**Authorization Code** (`oauth2_authorization_codes`):
- DELETE rows where `expires_at < now()` — code 已过期（默认 TTL 5 分钟）。
- 已消费的 code 由 `consumed_at` 标记，但由于 TTL 极短（5 分钟），过期条件已完全覆盖消费场景，无需单独清理。
- 日志: `[OAuth2Cleanup] removed N expired authorization codes`。

### 6.3 OAuth2 Token 竞态防护

`ExchangeAuthorizationCode` 和 `RefreshUserToken` 采用原子 UPDATE 防止并发重复消费/刷新：

```sql
-- 授权码消费：原子标记 consumed_at，仅首个并发请求影响 1 行
UPDATE oauth2_authorization_codes
   SET consumed_at = ?
 WHERE code = ? AND consumed_at IS NULL AND expires_at > ?

-- Refresh Token 轮换：原子标记 revoked_at，仅首个并发请求影响 1 行
UPDATE oauth2_refresh_tokens
   SET revoked_at = ?
 WHERE refresh_token = ? AND revoked_at IS NULL AND expires_at > ?
```

两个方法在原子 UPDATE 后检查 `RowsAffected`：等于 0 则返回 `ErrOAuthInvalidGrant`。这避免了 check-then-act 竞态，且无应用层锁开销。

### 6.4 所有后台任务汇总

| 任务 | 控制器 | 周期 | 清理目标 |
|------|--------|------|----------|
| Yggdrasil Token 清理 | `TokenCleanupController` | 1 小时 | `tokens` 表中 `invalid` 或过期记录 |
| OAuth2 Token 清理 | `OAuth2CleanupController` | 24 小时 | `oauth2_access_tokens` (过期/撤销)、`oauth2_authorization_codes` (过期) |
| 代注册用户清理 | `BotUserCleanupController` | 24 小时 | 不活跃的 bot 用户及其级联数据 |
| Session 清理 | `SessionCleanupController` | 24 小时 | 过期 session 记录 |
| 纹理清理 | `TextureCleanupController` | 1 小时 | 无引用的纹理文件 |

---

## 7. Per-User Token Count Limit

Per the authlib-injector wiki (§令牌): *"a user can have multiple tokens simultaneously, but the server should also limit the number of tokens. When the token count exceeds the limit (e.g. 10), the oldest token should be revoked before the new one is issued."*

This is enforced in [`/home/lnb/Desktop/HA/HRPAuth-Yggdrasil-API/services/auth_service.go::EnforceTokenLimit`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API/blob/main/services/auth_service.go), called immediately before `CreateToken` in both `/authenticate` and `/refresh`.

### Algorithm

```
limit    := yggdrasil.security.max_tokens_per_user (default 10)
count    := SELECT COUNT(*) FROM tokens WHERE user_id = ? AND state = 'valid'
if count >= limit:
    revoke := count - limit + 1
    UPDATE tokens
       SET state = 'invalid'
     WHERE id IN (
           SELECT id FROM tokens
            WHERE user_id = ? AND state = 'valid'
            ORDER BY issued_at ASC
            LIMIT revoke
     )
INSERT new tokens row (state = 'valid')
```

Notes:

- Only `state = 'valid'` rows count toward the limit. `temporarily_invalid` and `invalid` rows are ignored.
- Revocation selects by `ORDER BY issued_at ASC LIMIT N`, so the *oldest* tokens are dropped first (largest grace period for the client to refresh and reclaim).
- Because the revoke runs **before** the new INSERT, the user is always left with exactly `limit` valid rows after the call.
- The limit is read from `yggdrasil.security.max_tokens_per_user` in the HRPAuth-Yggdrasil-API configuration. Setting it to `0` falls back to the default `10` (defensive default — see [`config/config.go`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API/blob/main/config/config.go)).

### Interaction with Mutual Kicking

The per-user limit is independent of mutual kicking:

- Mutual kicking moves rows from `valid` → `temporarily_invalid` (Section 3, 4).
- The per-user limit only counts and revokes `valid` rows.

A kicked token does not consume the user's quota; the kicked user can still reclaim via `/refresh`, which will allocate one fresh `valid` row and, if needed, revoke the oldest `valid` row.

### Timeline Example (with `limit = 2`)

```text
T0  Client A logs in   → row#1 {state=valid}              count=1
T1  Client B logs in   → row#1 → temporarily_invalid
                         row#2 {state=valid}              count=1
T2  Client C logs in   → row#2 → temporarily_invalid
                         row#3 {state=valid}              count=1
T3  Client A refreshes → ValidateTokenForRefresh(row#1) (temporarily_invalid OK)
                         row#1 → invalid
                         row#2 → temporarily_invalid
                         count = 1 (row#3), under limit, no extra revoke
                         row#4 {state=valid}              count=1
```

If client D then logs in, `count=1`, no revoke is needed. The cap only triggers when an `/authenticate` or `/refresh` would otherwise push the count above the configured limit.
