> 来源：HA-Contract@bab2a05（2026-11-07），原路径 docs/dev/HRPAuth/data-models.md（契约仓知识库回迁）。

# Data Models

HRPAuth is split into two services: the **Core** service (General Auth) and the **Yggdrasil-API** service (Minecraft logic).

## Core Service Models

Table name: `users`, `oauth2_clients`, `oauth2_access_tokens`, `oauth2_refresh_tokens`, `oauth2_authorization_codes`, `webauthn_credentials`.

### User

Table name: `users`

| Field | Type | Description |
|-------|------|-------------|
| `uid` | uint | Primary key, **non-auto-increment**, allocated by backend `MAX(uid)+1` |
| `uuid` | string(32) | Internal unique identifier for the user |
| `email` | string(255) | Email address |
| `username` | string(255) | Username |
| `password` | string(255) | bcrypt hash |
| `ip` | string(255) | Most recent login IP |
| `permission` | int | Permission bits |
| `verified` | tinyint(1) | Whether email is verified |
| `2FA` | tinyint(1) | Two-Factor Authentication enabled |
| ... | ... | ... |

*(Minecraft-specific fields like `mbe`, `mojang_uuid`, `cbh` have been moved to the Yggdrasil-API service)*

## Yggdrasil-API Service Models

These models handle Minecraft-specific authentication and profile management.

### Account (1:1 fp=1ae2bfff Mapping)

Table name: `accounts`

| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Primary key |
| `core_user_id` | string(32) | Foreign key to Core `users.uuid` (NULL for proxy accounts) |
| `mojang_uuid` | string(32) | Bound Mojang UUID (unique) |
| `password` | string(255) | Local password hash (proxy accounts only) |
| `mbe` | tinyint(1) | Mojang Bind Enabled flag |
| `cbh` | tinyint(1) | Created By Human: `1` = regular; `0` = proxy registration |
| `last_sign_at` | timestamp | Last time the user signed in to Yggdrasil |

### Profile

Table name: `profiles`

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | UUID (primary key) |
| `account_id` | int | Reference to `accounts.id` |
| `name` | string | Minecraft profile name (inherits from Core `username`) |
| `model` | enum | `default` or `slim` |

### Other Yggdrasil Models

- **ProfileProperty**: Textures and other signed properties.
- **Token**: Yggdrasil protocol access tokens.
- **Session**: Game server join sessions.
- **ProfileKey**: Minecraft 1.19+ chat-signing keys.
- **Texture Files / Preview Files**: Stored on disk under the Yggdrasil service's configured texture storage for current profile management. They are not modeled as per-account texture-library tables.

## Field Semantics

### `cbh` (Created By Human)

- **Values**: `1` (default) = Created by human (WebUI registration or claimed proxy registration); `0` = Robot user created by WinnerProxy proxy registration and **not yet claimed**.
- **Write Rules**:
  - WebUI `/register` (including captcha enabled) → always `1`.
  - Service proxy registration (`POST /internal/proxy-register`, HRPAuth-Yggdrasil-API):
    - Hit existing account (idempotent by `mojang_uuid` / `mbe=1` bind) → **do not change** `cbh` (keep original value).
    - New account with `mojang_uuid` provided → `0` (proxy registration).
- **Cleanup Basis**: Users with `cbh=0` and both `register_at` / `last_sign_at` exceeding 30 days are deleted by `BotUserCleanupController` (see `references/HA-ROADMAP.md` §4).
- **Claiming Mechanism**:
  - Via WebUI / MBE: whether `cbh` flips to 1 when a proxy-registered user subsequently registers and binds via WebUI is decided by the business layer; the current implementation keeps the original value.
  - Via operator endpoint `POST /admin/claim-user`: when an operator assigns credentials to a `cbh=0` user, the handler **flips `cbh` to 1** as part of the same transaction, removing the user from the bot-user cleanup eligibility. See "Operator Claiming" below.

### Operator Claiming (`POST /admin/claim-user`)

Proxy-registered users are normally unable to log in to the business system because their email is a placeholder (`<username>@mojang-imported.invalid`) and their password is a WinnerProxy-generated random string. The operator claiming endpoint gives them a real WebUI credential pair.

| Field | Value |
|---|---|
| Authentication | OAuth2 Bearer Service Token, scope `user.claim.as-service` |
| Request body | `username` (≥ 3 chars), `email` (must be unique), `password` (≥ 6 chars) |
| Target | User with `username` equal to the request value AND `cbh=0` |

Implementation status: implemented on 2026-11-05. HRPAuth-Yggdrasil-API
serves `POST /admin/claim-user`; the core user provisioning happens through
the HRPAuth core internal endpoint `POST /internal/claim-user`
(`X-Internal-Key`). Steps:

1. Reject when no game account exists for the profile name (`404 user_not_found`) or the account has `cbh=1` (`409 user_not_claimable`).
2. Provision the core user through the core: write `email`, `password` (bcrypt), `verified = 1`. Email uniqueness is enforced against the core user table (`409 email_already_registered`); a username owned by another core user yields `409 username_already_bound`. The core call is idempotent by (username, email), so a player retry completes the link instead of stranding the provisioned user.
3. Link `core_user_id` and flip `cbh = 1` on the game account.
4. Return `{uid, username, email}`.

Side effects: after flipping `cbh` to 1, the user is no longer eligible for `BotUserCleanupController`, so operator claiming is the only way to permanently remove a `cbh=0` row without the user having to interact via MBE.

### Force Bind (`POST /admin/force-bind`)

Players who have both a proxy-registered account (cbh=0, created by WinnerProxy) and a manually registered account can use this endpoint to transfer the proxy account's `mojang_uuid` to the manual account and delete the proxy account.

| Field | Value |
|---|---|
| Authentication | OAuth2 Bearer Service Token, scope `user.force-bind.as-service` |
| Request body | `username` (proxy account username, ≥ 3 chars), `email` (target account email), `password` (target account password) |
| Source account | User with `username` equal to the request value AND `cbh=0` |
| Target account | User matching `email` + `password` (bcrypt verified) |

Implementation status: implemented on 2026-11-05; authentication switched
from the retired `manage.token` to the service-token tier on 2026-11-05.
HRPAuth-Yggdrasil-API serves `POST /admin/force-bind`; the target
credentials are verified through the HRPAuth core internal endpoint
`POST /internal/verify-credentials` (`X-Internal-Key`). Steps:

1. Require a service token carrying `user.force-bind.as-service` (same tier as `/admin/claim-user`).
2. Find source account by profile name where `cbh=0`; reject if not found or has no `mojang_uuid`.
3. Authenticate target account by `email` + `password` via the core; reject if credentials are invalid (`403 invalid_credentials`; unknown email and wrong password are merged).
4. Transfer `mojang_uuid` from source to target and delete the source account in a single local transaction on the Yggdrasil side (the core call is read-only, so no cross-database rollback is needed). If the target core user has no game account yet, one is created (`cbh=1`) in the same transaction.
5. Return success.

Side effects: the proxy-registered account is permanently deleted. A target account already bound to a *different* `mojang_uuid` is rejected with `409 mojang_uuid_already_bound` (no overwrite); re-running with the same `mojang_uuid` is idempotent.

### `mojang_uuid`

- **Format**: 32-bit lowercase hex (UUID without hyphens), isomorphic with Yggdrasil `selectedProfile.id`.
- **Constraint**: `UNIQUE` index `uk_users_mojang_uuid` (`NULL` does not participate in unique constraint, so multiple unbound users can exist).
- **Write Sources**:
  - `POST /internal/proxy-register` decision tree 1 (hit by `mojang_uuid`) → idempotent return.
  - `POST /internal/proxy-register` decision tree 2.a `mbe=1` → written during bind.
  - `POST /admin/force-bind` → transfers `mojang_uuid` from a proxy-registered account to a target account (see "Force Bind" below).
  - **Not** written via WebUI `/register`.
- **Relationship with `users.uuid`**: `users.uuid` is the internal UUID for the user within the HA / Yggdrasil system (unrelated to Mojang); `mojang_uuid` is the bound Mojang authentic UUID. Both can coexist and be different.

### `mbe` (Mojang Bind Enabled)

- **Values**: `0` (default) = Prohibit Mojang players with the same name from binding via `POST /internal/proxy-register` (**HA priority**, Mojang players receive 409 and are kicked); `1` = Allow binding.
- **Write Endpoints** (HRPAuth-Yggdrasil-API, OAuth2 Bearer of the account owner):
  - `POST /player/mojang-bind-enable` → Sets `mbe = 1` and starts the auto-disable countdown. Idempotent.
  - `POST /player/mojang-bind-disable` → Sets `mbe = 0`. Idempotent.
  - (The former core endpoints `POST /user/mojang-bind-enable` / `-disable` were removed from the core in 2026-10.)
- **Query**: `mbe` is returned in the HRPAuth-Yggdrasil-API `GET /player` response (`data.mbe`). The core `POST /user` response does not include it.
- **Only effective in the `POST /internal/proxy-register` collision branch (decision tree 2.a)**: Checked when a colliding account is hit by profile name and its `mojang_uuid IS NULL`.
- **Once `mojang_uuid` is written, the semantics of `mbe` disappear** (subsequent same-name Mojang players will not trigger 2.a). On successful bind, `mbe` is **automatically reset to 0** together with the `mojang_uuid` write, so the field cleanly reflects the current authorization status.
- **Auto-disable timeout**: After `POST /player/mojang-bind-enable` sets `mbe = 1`, a 15-minute countdown begins. If no bind (`mojang_uuid` write) occurs within that window, the system automatically resets `mbe = 0`. The timeout is cancelled on successful bind (which also resets `mbe = 0`), explicit disable, or re-enable (which restarts the timer). Implementation: HRPAuth-Yggdrasil-API `controllers/mbe_timeout_controller.go` — an in-memory event stack scanned every 30 seconds.

### `password`

- **Native HRPAuth write format**: bcrypt.
- **BlessingSkin migration contract**:
  - source method = `BCRYPT` -> copy the existing bcrypt hash directly
  - source method != `BCRYPT` -> write the fixed invalid marker `BS2HA$RESET_REQUIRED`
- **Operational meaning**: non-bcrypt BlessingSkin imports do not log in with their old password and must use password reset.
- **Configuration interaction**: `security.password_cost` applies to newly generated bcrypt hashes, including post-reset passwords.

## Profile

Table name: `profiles`

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | UUID (primary key) |
| `user_id` | string | Owner internal UUID (corresponds to `users.uuid`) |
| `name` | string | Minecraft profile name (unique) |
| `model` | enum | Skin model: `default` or `slim` |
| `created_at` / `updated_at` | datetime | Automatically maintained by GORM |

> A User can have multiple Profiles (multiple characters), but the current registration process only creates the first one. For multiple characters, call extension interfaces outside of Yggdrasil `/api/profiles/minecraft`.
>
> BlessingSkin migration intentionally narrows imported profiles according to [`bs2ha-migration.md`](0011-bs2ha-migration-contract.md): keep the single player when only one exists, otherwise keep only the player matching the imported BlessingSkin username, and create a placeholder profile when no usable BlessingSkin player remains.

## ProfileProperty

Table name: `profile_properties`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `profile_id` | string | Profile UUID (foreign key) |
| `name` | string | Property name (e.g., `textures`) |
| `value` | string | Property value (base64 encoded JSON) |
| `signature` | string | RSA signature of `value` using private key (nullable, but expected for production `textures` payloads) |

## Token

Table name: `tokens`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `user_id` | string | Owner internal UUID (corresponds to `users.uuid`) |
| `access_token` | string | Yggdrasil Access Token (unique) |
| `client_token` | string | Yggdrasil Client Token |
| `selected_profile_id` | string | Associated Profile UUID (foreign key, nullable) |
| `issued_at` | int64 | Unix millisecond timestamp |
| `expires_in_days` | int | Validity in days (default 15) |
| `state` | string | State: `valid` / `temporarily_invalid` / `invalid` |
| `created_at` | datetime | Creation time (`DEFAULT CURRENT_TIMESTAMP`) |

> See [tokens.md](0009-token-system.md) for detailed state machine.

## OAuth2Client

Table name: `oauth2_clients`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `client_id` | string | Client identifier (unique) |
| `client_secret` | string | Client secret (nullable) |
| `name` | string | Client name |
| `type` | enum | `public` or `confidential` |
| `grant_types` | text | Allowed grant types (comma-separated) |
| `redirect_uris` | text | Whitelisted redirect URIs |
| `scopes` | text | Allowed scopes |
| `is_internal` | bool | Whether client is internal to HA system |
| `is_super` | bool | Whether client has elevated permissions |
| `is_active` | bool | Whether client is active |
| `created_at` / `updated_at` | datetime | Timestamps |

## OAuth2AccessToken

Table name: `oauth2_access_tokens`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `access_token` | string | The Bearer access token (unique) |
| `client_id` | string | Issuing client ID |
| `user_id` | string | Associated internal UUID (nullable for service mode) |
| `scopes` | text | Granted scopes |
| `subject_type` | enum | `user` or `service` |
| `target_uid` | uint | Explicit target UID for service mode (nullable) |
| `target_email` | string | Explicit target email for service mode (nullable) |
| `expires_at` | datetime | Expiration time |
| `revoked_at` | datetime | Revocation time (nullable) |
| `created_at` | datetime | Creation time |

## OAuth2RefreshToken

Table name: `oauth2_refresh_tokens`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `refresh_token` | string | The refresh token (unique) |
| `access_token_id` | uint | Linked access token ID |
| `client_id` | string | Issuing client ID |
| `user_id` | string | Associated internal UUID |
| `scopes` | text | Granted scopes |
| `expires_at` | datetime | Expiration time |
| `revoked_at` | datetime | Revocation time (nullable) |
| `created_at` | datetime | Creation time |

## Session

Table name: `sessions`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `server_id` | string | serverId passed by Minecraft server |
| `profile_id` | string | Associated Profile UUID |
| `ip` | string | Client IP (optional) |
| `created_at` | datetime | Creation time (`DEFAULT CURRENT_TIMESTAMP`) |
| `expires_at` | datetime | Expiration time |

> Sessions are written by `POST /sessionserver/session/minecraft/join` and read by `GET /sessionserver/session/minecraft/hasJoined`.

## ProfileKey

Table name: `profile_keys`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `user_id` | string(32) | Owner internal UUID (`users.uuid`); `UNIQUE` index `uk_profile_keys_user_id` |
| `public_key` | text | PEM-encoded RSA public key (`BEGIN RSA PUBLIC KEY`) |
| `private_key` | text | PEM-encoded RSA private key (`BEGIN RSA PRIVATE KEY`) |
| `public_key_signature` | text | Base64 SHA1withRSA signature over `<expiresAtMillis><publicKeyPEM>` produced by the Yggdrasil signature private key |
| `expires_at` | datetime | Validity expiry (issued + 48h) |
| `refreshed_after` | datetime | Earliest moment the client may request a new key pair (issued + 40h) |
| `created_at` / `updated_at` | datetime | GORM-managed |

> See [`0013-profile-keys.md`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API/blob/main/knowledge/0013-profile-keys.md) for the full flow and [`services/profile_key_service.go`](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API/blob/main/services/profile_key_service.go) for the service implementation. The `profile_keys` table is owned by HRPAuth-Yggdrasil-API (its `database/migrations/000001_init.up.sql`).

## WebAuthnCredential

Table name: `webauthn_credentials`

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint | Primary key |
| `user_id` | string(32) | Owner internal UUID (`users.uuid`) |
| `name` | string(255) | User-visible label for this credential |
| `credential_id` | string(1024) | Base64url-encoded credential ID |
| `credential_id_hash` | char(64) | SHA-256 hash of raw credential ID, used for stable lookup and uniqueness |
| `credential_json` | mediumtext | Serialized `go-webauthn` credential record used for future verification |
| `last_used_at` | datetime | Last successful assertion time |
| `created_at` / `updated_at` | datetime | Timestamps |

This table is introduced by `database/migrations/000009_add_webauthn_support.up.sql`.
