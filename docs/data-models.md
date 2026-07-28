# 数据模型

> 详细实现： [`models/models.go`](../../models/models.go)

本项目使用 GORM 维护以下数据模型。

## 模型总览

| 模型 | 表名 | 描述 |
|------|------|------|
| [User](#user) | `users` | 用户信息 |
| [Profile](#profile) | `profiles` | Minecraft 角色资料 |
| [ProfileProperty](#profileproperty) | `profile_properties` | 角色属性（如纹理） |
| [Token](#token) | `tokens` | Yggdrasil 认证令牌 |
| [Session](#session) | `sessions` | 服务器会话 |

## User

表名：`users`

| 字段 | 类型 | 说明 |
|------|------|------|
| `uid` | uint | 主键，**非自增**，由后端 `MAX(uid)+1` 分配 |
| `uuid` | string(32) | Yggdrasil UUID（与 Yggdrasil `selectedProfile.id` 对应） |
| `email` | string(255) | 邮箱（**无数据库层唯一约束**，业务层判重）|
| `avatar` | string(255) | 头像 URL（当前未启用） |
| `username` | string(255) | 用户名（**无数据库层唯一约束**，业务层判重）|
| `password` | string(255) | bcrypt 哈希（`NOT NULL`）|
| `ip` | string(255) | 最近一次登录 IP |
| `permission` | int | 权限位（默认 0）|
| `last_sign_at` | datetime | 最近活动（**业务/清理用**，代注册清理 routine 用此字段判断活跃度）|
| `register_at` | datetime | 注册时间（**业务/清理用**，代注册清理 routine 用此字段判断账号年龄）|
| `verified` | tinyint(1) | 邮箱是否已验证（默认 0）|
| `remember_token` | string(100) | 本站业务系统会话令牌 |
| `regip` | string(40) | 注册时 IP |
| `totp` | string(32) | TOTP 共享密钥（Base32）|
| `cbh` | tinyint(1) | **Created By Human**：1 = WebUI 注册或已认领的代注册；0 = WinnerProxy 代注册未认领（默认 1）|
| `mbe` | tinyint(1) | **Mojang Bind Enabled**：1 = 允许同名 Mojang 玩家通过 M.T. `/register` 绑定；0 = HA 优先拒绝（默认 0）|
| `mojang_uuid` | string(32) | 绑定的 Mojang UUID（无连字符小写 hex，`NULL`=未绑；`UNIQUE` 索引 `uk_users_mojang_uuid`）|

## 字段语义

### `cbh`（Created By Human）

- **取值**：`1`（默认）= 人类创建（WebUI 注册或已认领的代注册用户）；`0` = 由 WinnerProxy 代注册且**未被认领**的机器人用户。
- **写入规则**：
  - WebUI `/register`（含开启 captcha）→ 总是 `1`。
  - M.T. `/register`：
    - 命中已存在用户（幂等 / `mbe=1` bind）→ **不改** `cbh`（保留原值）。
    - 新建用户且传 `mojang_uuid` → `0`（代注册）。
    - 新建用户且未传 `mojang_uuid` → `1`（与 WebUI 等同）。
- **清理依据**：`cbh=0` 且 `register_at` / `last_sign_at` 均超过 30 天的用户由 `BotUserCleanupController` 删除（见 `references/HA-ROADMAP.md` §4）。
- **认领机制**：代注册用户后续通过 WebUI 注册并 bind 时，`cbh` 是否翻转为 1 由业务层决定；当前实现为保持原值。

### `mojang_uuid`

- **格式**：32 位小写 hex（去掉连字符的 UUID），与 Yggdrasil `selectedProfile.id` 同构。
- **约束**：`UNIQUE` 索引 `uk_users_mojang_uuid`（`NULL` 不参与唯一约束，因此未绑定用户可多个并存）。
- **写入来源**：
  - M.T. `/register` 决策树 1（按 `mojang_uuid` 命中）→ 幂等返回。
  - M.T. `/register` 决策树 2.a `mbe=1` → bind 时写入。
  - **不**通过 WebUI `/register` 写入。
- **与 `users.uuid` 关系**：`users.uuid` 是该用户在 HA / Yggdrasil 体系中的内部 UUID（与 Mojang 无关）；`mojang_uuid` 是绑定的 Mojang 正版 UUID。两者可共存不同。

### `mbe`（Mojang Bind Enabled）

- **取值**：`0`（默认）= 禁止同名 Mojang 玩家通过 M.T. `/register` 绑定（**HA 优先**，Mojang 玩家收到 409 被踢）；`1` = 允许绑定。
- **写入端点**：
  - `POST /user/mojang-bind-enable` → 玩家自开（Remember Token）或运维代开（Manage Token + `uid`/`email`）。
- **仅在 M.T. `/register` 决策树 2.a 中生效**：命中同名 WebUI 用户且其 `mojang_uuid IS NULL` 时检查。
- **一旦 `mojang_uuid` 被写入，`mbe` 的语义即消失**（后续同名 Mojang 玩家不会触发 2.a）；但 `mbe` 字段不被自动重置，便于查询授权状态。

## Profile

表名：`profiles`

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | UUID（主键） |
| `user_id` | uint | 所属用户 ID（外键） |
| `name` | string | Minecraft 角色名（唯一） |
| `created_at` / `updated_at` | datetime | GORM 自动维护 |

> 一个 User 可以有多个 Profile（多角色），但当前注册流程只创建第一个。如需多角色，调用 Yggdrasil `/api/profiles/minecraft` 之外的扩展接口。

## ProfileProperty

表名：`profile_properties`

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint | 主键 |
| `profile_id` | string | 角色 UUID（外键） |
| `name` | string | 属性名（如 `textures`） |
| `value` | string | 属性值（base64 编码的 JSON） |
| `signature` | string | 用私钥对 `value` 的 RSA 签名（可空）|

## Token

表名：`tokens`

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint | 主键 |
| `user_id` | uint | 用户 ID |
| `access_token` | string | Yggdrasil Access Token（唯一） |
| `client_token` | string | Yggdrasil Client Token |
| `state` | string | 状态：`valid` / `temporarily_invalid` / `invalid` |
| `profile_id` | string | 关联的角色 UUID（外键，可空） |
| `issued_at` | int64 | Unix 毫秒时间戳 |
| `expires_in_days` | int | 有效期天数（默认 15） |
| `created_at` | datetime | 创建时间（`DEFAULT CURRENT_TIMESTAMP`）|

> 详细状态机见 [tokens.md](./tokens.md)。

## Session

表名：`sessions`

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint | 主键 |
| `server_id` | string | Minecraft 服务端传入的 serverId |
| `profile_id` | string | 关联的角色 UUID |
| `ip` | string | 客户端 IP（可选） |
| `created_at` | datetime | 创建时间（`DEFAULT CURRENT_TIMESTAMP`）|
| `expires_at` | datetime | 过期时间 |

> Session 由 `POST /sessionserver/session/minecraft/join` 写入，由 `GET /sessionserver/session/minecraft/hasJoined` 读取。
