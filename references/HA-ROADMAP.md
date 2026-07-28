# HRPAuth 改动路线图

> 本文档列出 HRPAuth（HA）为了让 WinnerProxy 落地需要做的全部改动。
> WinnerProxy 与 HA 的关系是**调用与被调用**：WinnerProxy 作为 HA 的客户端，HA 决定自己的内部行为。  
> 本路线图由 WinnerProxy 团队与 HA 团队联合维护。

> **文档版本**：当前 v0.2 (2026-07-09)，已与实际实现对齐。  
> v0.2 相对 v0.1 的关键偏差：新增 `mbe` 字段；§3.4 2.a 按 `mbe` 分支（mbe=0 → 409，mbe=1 → bind 且**只动 mojang_uuid + last_sign_at**，不覆盖 password/email/cbh）；§4 改用 `register_at` + `last_sign_at` 而非 `created_at` + `updated_at`；§4 周期任务与 M.T. 触发通过 `sync.Mutex.TryLock` 串行化。具体见 [§7 变更记录](#7-文档与变更记录)。

## 0. 背景与目标

当前 HA 是一个独立可用的 Yggdrasil 兼容 Minecraft 身份认证服务。  
新增 WinnerProxy 后，希望达成：

1. **HA 优先**：玩家先在 HA WebUI 注册、登录；Mojang 正版玩家通过 WinnerProxy 也能"以 HA 身份"进入下游 Minecraft 服。
2. **代注册 fallback**：未在 HA 注册的正版玩家进服时，WinnerProxy 自动代注册一个 HA 账号（`cbh=0`，占位 email），用完可清理。
3. **同身份一致性**：同一玩家无论用 HA 密码登录还是用 Mojang 正版登录，在下游服里看到的是同一个 UUID/Name（HA 身份）。

HA 端的所有"绑定/解绑/清理/数据库直写"行为都在 HA 内部完成，**不对外暴露管理端点**。WinnerProxy 只需调用 HA 现有的 Yggdrasil 公开端点 + `/register`。

## 1. 改动总览

| # | 类别 | 改动 | 风险 |
|---|---|---|---|
| 1.1 | 数据库 | `users` 表新增 2 列 + 1 唯一索引（cbh / mojang_uuid）| 低 |
| 1.2 | 数据库 | `users` 表新增 1 列（mbe，**v0.2 新增**）| 低 |
| 1.3 | `POST /register` | 接受 M.T. 时旁路 captcha | 中（需新增配置项 `manage.token`；如已存在，验证复用）|
| 1.4 | `POST /register` | body 接受可选字段 `mojang_uuid` | 低（向后兼容）|
| 1.5 | `POST /register` | 响应新增字段 `profile_id` | 低（向后兼容）|
| 1.6 | `POST /register` | 实现 upsert/绑定/重绑业务逻辑 | 中（需详细测试矩阵）|
| 1.7 | `POST /user/mojang-bind-enable` | **v0.2 新增**。开启 mbe 开关 | 低（幂等）|
| 1.8 | 后台 routine | 新增 `cbh=0 + 30+30` 清理 goroutine | 低（独立可观测）|

**不新增任何对外管理端点（除 `POST /user/mojang-bind-enable`）。** 不动 `POST /user`（除新增 enable 端点）、`POST /change-username`、`POST /change-profile-name`、任何 authserver 端点。

---

## 2. Phase 1 — 数据库迁移（HA 1.X+）

### 2.1 迁移脚本

```sql
-- 001_add_cbh_and_mojang_uuid.up.sql
ALTER TABLE users
  ADD COLUMN cbh TINYINT(1) NOT NULL DEFAULT 1
    COMMENT '1=由人在 WebUI 创建, 0=由 WinnerProxy 代注册创建',
  ADD COLUMN mojang_uuid VARCHAR(32) COLLATE ascii_bin NULL
    COMMENT 'Mojang 玩家 UUID (无连字符), 绑后唯一';

ALTER TABLE users
  ADD UNIQUE KEY uk_users_mojang_uuid (mojang_uuid);
```

```sql
-- 002_add_mbe.up.sql  (v0.2 新增)
ALTER TABLE users
  ADD COLUMN mbe TINYINT(1) NOT NULL DEFAULT 0
    COMMENT '1=允许同名 Mojang 玩家绑定, 0=HA 优先拒绝';
```

> 字段 `mbe` 必须在 Phase 2 的 `POST /user/mojang-bind-enable` 端点之前上线，否则 409 路径无法在 mbe=0 的默认 user 上运行。

### 2.2 字段语义

| 字段 | 类型 | 含义 |
|---|---|---|
| `cbh` | TINYINT(1) NOT NULL DEFAULT 1 | 1 = 由人在 WebUI 创建（或代注册账号已被"认领"）；0 = 由 WinnerProxy 代注册创建（"未认领"） |
| `mojang_uuid` | VARCHAR(32) ascii_bin NULL | 绑定的 Mojang UUID（无连字符的小写形式，如 `f7c77d999f154a66a87dc4a51ef30d19`），NULL 表示未绑；UNIQUE |
| `mbe` | TINYINT(1) NOT NULL DEFAULT 0 | **v0.2 新增**。1 = 允许同名 Mojang 玩家通过 M.T. `/register` 绑定到此 HA 用户；0 = HA 优先拒绝（同名 Mojang 玩家收 409） |

> 字段名 `cbh` = **C**reated **B**y **H**uman，`mbe` = **M**ojang **B**ind **E**nabled。  
> 老用户（迁移前已存在）全部为 `cbh=1`、`mbe=0`（默认），符合"由人创建、未授权 Mojang 绑定"的语义。

### 2.3 索引

- 主键：`users.uid`（保持不变；**v0.1 误写为 `id`**，实际项目用 `uid` 作为主键，由后端 `MAX(uid)+1` 分配，**非自增**）
- 新增唯一索引：`uk_users_mojang_uuid(mojang_uuid)`
- 不需要给 `cbh` 或 `mbe` 加索引（清理 routine 全表扫一次开销可接受；如未来量大再加）

### 2.4 验证

- 老用户不受影响（`cbh=1`, `mojang_uuid=NULL`）。
- 回滚脚本：先 `DROP INDEX uk_users_mojang_uuid`，再 `DROP COLUMN mojang_uuid` 和 `cbh`。

---

## 3. Phase 2 — `POST /register` 改造

### 3.1 已承诺：M.T. 旁路 captcha

按当前 HRPAuth tokens 设计，`config.yaml > manage.token` 已存在（`docs-HRPAuth/tokens.md` §M.T.）。  
实现位置：`controllers/auth_controller.go#Register`，在 captcha 校验之前增加：

```go
// 伪代码
if body.RememberToken != "" && body.RememberToken == cfg.ManageToken {
    // M.T. 路径：跳过 captcha、跳过 email 格式严格校验（占位 email 可非标准）
    // 仍校验 username/password 长度
} else {
    // 正常路径：保留所有现有校验
}
```

> 注：`POST /register` 原始 body 不含 `remember_token` 字段（注册尚未产生用户）。  
> 改造点：扩展 body 接受 `remember_token` 字段；为 M.T. 鉴权专用，正常注册路径忽略该字段。

### 3.2 接受可选 `mojang_uuid`

`POST /register` body 增加字段：

```jsonc
{
  "email": "alice@mojang-imported.invalid",   // 可省略：M.T. 路径下自动用占位
  "username": "Alice",
  "password": "<>= 6 字符>",
  "mojang_uuid": "f7c77d999f154a66a87dc4a51ef30d19",  // 新增，仅 M.T. 路径生效
  "remember_token": "<M.T.>"                          // 新增，M.T. 鉴权用
}
```

字段约束：
- `email`：M.T. 路径下若未提供，按规则 `{username-lowercased}@mojang-imported.invalid` 自动生成；正常路径仍必填。
- `password`：始终必填，长度沿用现有约束（≥6）。
- `mojang_uuid`：可选；M.T. 路径下若提供，必须是 32 位小写 hex（无连字符），否则 400。
- `remember_token`：可选；如提供且匹配 `cfg.ManageToken` 则走 M.T. 鉴权；正常注册路径忽略此字段。

### 3.3 响应增加 `profile_id`

现有响应：

```json
{ "success": true, "uid": 42, "message": "Register successful" }
```

新响应：

```json
{
  "success": true,
  "uid": 42,
  "message": "Register successful",
  "profile_id": "a1b2c3d4e5f67890abcdef1234567890"   // 新增：Profile 表的 UUID
}
```

`profile_id` 是 Profile 表的主键，对应 Yggdrasil `selectedProfile.id`。
- 正常注册路径：创建完 `users` 行后，**自动**调用现有的 profile 创建流程，返回新 profile 的 UUID。
- M.T. 路径：按 3.4 的 upsert 逻辑返回对应 profile 的 UUID（新建或已存在）。

### 3.4 业务逻辑（仅 M.T. 路径生效，**v0.2 按 mbe 分支**）

```
输入: username, password, mojang_uuid (可选), email (可选)
约束: M.T. 鉴权已通过

1. 若 mojang_uuid 不为空:
   1.1 在 users 表按 mojang_uuid 查找
       - 命中: 幂等返回 (uid, profile_id)
       - 未命中: 进入步骤 2
2. 在 users 表按 username 查找
   - 命中:
     a. 若 mojang_uuid 不为空 且 users[username].mojang_uuid IS NULL
        ├ users[username].mbe = 0 → 409 Conflict { "error": "username_already_bound" }
        │   (HA 优先, WinnerProxy 收到 409 后踢 Mojang 玩家)
        └ users[username].mbe = 1 → bind (见下)
            UPDATE users SET mojang_uuid=?, last_sign_at=NOW() WHERE id=?
            → **保留** cbh / password / email 不动
            → 返回 (uid, profile_id)
     b. 若 mojang_uuid 不为空 且 users[username].mojang_uuid = mojang_uuid
        → 幂等返回 (uid, profile_id)
     c. 若 mojang_uuid 不为空 且 users[username].mojang_uuid <> mojang_uuid
        → 409 Conflict { "error": "username_already_bound" }
     d. 若 mojang_uuid 为空
        → 400 Bad Request { "error": "mojang_uuid_required_for_existing_user" }
   - 未命中:
     → 创建 user
        email = (input.email OR "{username-lowercased}@mojang-imported.invalid")
        password = (input.password)
        cbh = (mojang_uuid 不为空 ? 0 : 1)   // 代注册 → cbh=0; 正常 WebUI → cbh=1
        mbe = 0  // 默认关闭, 玩家需主动调 /user/mojang-bind-enable 开启
        mojang_uuid = (input.mojang_uuid OR NULL)
     → 创建对应 profile 行（沿用现有 profile 创建流程，绑定 username）
     → 返回 (uid, profile_id)
```

> **v0.2 vs v0.1 的关键差异**：
> - 2.a **新增 `mbe` 判定**——v0.1 直接 bind 会覆盖 WebUI 用户的 password/email，v0.2 在 mbe=0 时返回 409（HA 优先），在 mbe=1 时只写 `mojang_uuid` + `last_sign_at`（保留凭据）。  
> - v0.1 提到 `email=?, password=?, updated_at=NOW()`，v0.2 实现里 email/password **不写**（保留 WebUI 凭据），时间字段改用 `last_sign_at`（与 Phase 3 对齐，无 `updated_at` 列）。

### 3.5 响应矩阵

| 输入 | 命中状态 | HTTP | 响应 |
|---|---|---|---|
| 正常 WebUI 注册（无 M.T.）| — | 200 | `{success, uid, message, profile_id}` |
| M.T. + 全新 username + mojang_uuid | 新建 | 200 | `{success, uid, message, profile_id, cbh: 0}` |
| M.T. + 已存在 username + mojang_uuid=已绑同值 | 幂等 | 200 | `{success, uid, message, profile_id}` |
| M.T. + 已存在 username + mojang_uuid=空位 + mbe=0 | 撞名（HA 优先）| 409 | `{error: "username_already_bound"}` |
| M.T. + 已存在 username + mojang_uuid=空位 + mbe=1 | 绑定现有空位 | 200 | `{success, uid, message, profile_id}` |
| M.T. + 已存在 username + mojang_uuid=已绑他值 | 撞名 | 409 | `{error: "username_already_bound"}` |
| M.T. + 已绑定 mojang_uuid 但 username 变了 | 幂等返回旧 uid | 200 | `{success, uid, message, profile_id}` |
| M.T. 路径下 mojang_uuid 格式错误 | — | 400 | `{error: "invalid_mojang_uuid"}` |

### 3.6 新增端点：`POST /user/mojang-bind-enable`（v0.2 新增）

- 玩家通过 Remember Token 自己开：`{ "remember_token": "<RT>" }` → `mbe=1`
- 运维通过 Manage Token 代开：需额外传 `uid` 或 `email`
- 幂等；不影响已绑定的 user
- 详见 [`../docs/endpoints/user-info.md`](../docs/endpoints/user-info.md#post-usermojang-bind-enable)

---

## 4. Phase 3 — 清理 routine（HA 内部）

> 这部分纯属 HA 内部行为，**不对外暴露任何端点**（除 `POST /user/mojang-bind-enable` 开启 mbe）。  
> 文档记录是为了让 HA 团队有明确目标。

### 4.1 触发

- HA 启动时启动一个 goroutine，循环间隔 24h。
- 每次 `POST /register`（M.T. 路径）成功后异步执行一次清理（不阻塞响应）。**v0.2 实际已实现该触发**（v0.1 仅写"也可"）。

### 4.2 规则

**v0.2 实现**改用 `register_at` + `last_sign_at`（项目无 `created_at` / `updated_at` 列）：

```sql
SELECT id FROM users
WHERE cbh = 0
  AND register_at < NOW() - INTERVAL 30 DAY
  AND last_sign_at < NOW() - INTERVAL 30 DAY;
```

> v0.1 写 `created_at` / `updated_at`，但项目实际无这两列，改用现有 `register_at`（账号年龄）+ `last_sign_at`（最后活跃）作为代理。

### 4.3 清理动作

按依赖顺序删除关联表，再删 users 行（**v0.2 修正：sessions 必须在 profile 之前删**）：

1. `DELETE FROM sessions WHERE profile_id IN (<该 user 的所有 profile_id>)`
2. `DELETE FROM profile_properties WHERE profile_id IN (<该 user 的所有 profile_id>)`
3. `DELETE FROM profiles WHERE user_id = ?`
4. `DELETE FROM tokens WHERE user_id = ?`
5. `DELETE FROM users WHERE id = ?`

> 实际表名以 HA 当前数据模型为准，参照 `docs/data-models.md`。
> v0.1 的步骤 4 写 `DELETE FROM sessions WHERE user_id = ?`，但 Session 模型没有 `user_id` 字段（只有 `profile_id`），v0.2 修正为先取 profile_id 再按 profile_id 删 sessions。

### 4.4 观测

清理 routine 跑完后输出日志（INFO 级）：

```
[cleanup] scanned 142 users, deleted 3:
  - uid=42 username=longgone_1 (created 2026-05-01, last_seen 2026-05-03)
  - uid=58 username=longgone_2 (created 2026-05-02, last_seen 2026-05-04)
  - uid=99 username=longgone_3 (created 2026-04-30, last_seen 2026-05-02)
```

### 4.5 异常处理

- 单个用户删除失败（外键约束等）：记录 ERROR，**不中断**后续用户清理。
- 全表扫描超时：拆分批次，每批 1000 行。**v0.2 暂未实现分批**，一次性 `Find` 加载所有候选行；如候选量过大需要后续引入分批。
- **v0.2 新增**并发安全：周期任务与 M.T. 触发通过包级 `sync.Mutex` + `TryLock` 串行化，并发的 M.T. 触发在已有清理运行时直接 `return 0`，不阻塞、不重试。

---

## 5. 验收测试

### 5.1 单元测试

| 用例 | 预期 |
|---|---|
| 正常 WebUI 注册（无 M.T.）| 行为与现状一致；响应新增 `profile_id` |
| M.T. + 全新 username + 合法 mojang_uuid | 新建 user (cbh=0, mbe=0)，响应含 profile_id + cbh:0 |
| M.T. + 重复 username + 同 mojang_uuid | 200 幂等返回 |
| M.T. + 重复 username + 不同 mojang_uuid | 409 |
| M.T. + 重复 username + mojang_uuid=NULL + mbe=0 | 409 (HA 优先) |
| M.T. + 重复 username + mojang_uuid=NULL + mbe=1 | 200 bind (写 mojang_uuid + last_sign_at, **不动** password/email/cbh) |
| M.T. + 已知 mojang_uuid + 已知 username | 200 幂等返回 |
| M.T. + 已知 mojang_uuid + 未知 username | 200 新建（但 username 必须满足 3.4 业务约束）|
| M.T. + 无 mojang_uuid 字段 | 走"未命中"路径，行为如正常注册（cbh=1, mbe=0）|
| 玩家调 /user/mojang-bind-enable（用自己 RT）| mbe 设为 1, 幂等 |
| 运维调 /user/mojang-bind-enable（M-T + uid）| 目标 user.mbe 设为 1 |

### 5.2 集成测试（与 WinnerProxy 联调）

| 场景 | 预期 |
|---|---|
| Mojang 玩家 X 首次进服 | WinnerProxy → HA /register (M.T.) → 新建 user → 返回 X 的 HA profile |
| Mojang 玩家 X 再次进服 | WinnerProxy → HA /register (M.T.) → 命中现有 user → 幂等返回 |
| Mojang 玩家 X 改名后进服 | WinnerProxy → HA /register (M.T., 新名) → 旧 user 仍存在，**新名被当作新用户**（按"HA 优先"语义）|
| HA 用户 Y（未绑 Mojang、mbe=0）被 Mojang 玩家 X 撞名 | WinnerProxy → HA /register (M.T.) → 409 → 204 + WARN |
| HA 用户 Y（未绑 Mojang、mbe=1）被 Mojang 玩家 X 撞名 | WinnerProxy → HA /register (M.T.) → 200 bind → Y.mojang_uuid=X, Y.password/email 不变 |
| HA 用户 Y（已绑 mojang_uuid=A）撞名另一个 Mojang 玩家 X(A 不同) | WinnerProxy → HA /register (M.T.) → 409 → 204 + WARN（Y.mbe 无关） |
| 已代注册用户 30+30 天未活动 | HA 清理 routine 删除该 user |

### 5.3 回归

- 所有现有 HA WebUI 注册/登录/换绑流程行为不变。
- 现有 Yggdrasil 公开端点（`/sessionserver/...`、`/authserver/...`、`/api/profiles/minecraft`）响应格式不变。
- M.T.T机制/wiwrnperproxyroxy/*` 之类的现有用法）如已有，端点行为不变。

---

## 6. 排期建议

| Phase | 内容 | 估时 |
|---|---|---|
| Phase 1 | 数据库迁移 | 0.5d |
| Phase 2 | `/register` 改造 + 单元测试 | 1.5d |
| Phase 3 | 清理 routine | 0.5d |
| Phase 4 | 集成测试 + 与 WinnerProxy 联调 | 1d |

总计约 3.5 个工作日（HA 团队 1 人可独立完成，不阻塞 WinnerProxy 开发）。

---

## 7. 文档与变更记录

| 版本 | 日期 | 变更 |
|---|---|---|
| 0.1 | 2026-07-08 | 初稿（基于 WinnerProxy ↔ HRPAuth 联合设计讨论）|
| 0.2 | 2026-07-09 | 与实际实现对齐。**主要差异**：(1) 新增 `mbe` 字段 + `POST /user/mojang-bind-enable` 端点（解决 §3.4 2.a 直接 bind 会覆盖 WebUI 用户凭据的安全/UX 问题）；(2) §3.4 2.a 改为按 mbe 分支：mbe=0 → 409（HA 优先），mbe=1 → bind 且**只动 mojang_uuid + last_sign_at**，保留 password/email/cbh；(3) §4 改用 `register_at` + `last_sign_at`（项目无 `created_at`/`updated_at` 列）；(4) §4 修正 sessions 删除顺序（先按 profile_id 删 sessions，再删 profile）；(5) §4.1 M.T. 触发已实现（v0.1 仅写"也可"），通过包级 `sync.Mutex.TryLock` 串行化；(6) §2.3 修正主键名为 `uid`（v0.1 误写为 `id`）。同步更新 [`../docs/data-models.md`](../docs/data-models.md)、[`../docs/endpoints/auth.md`](../docs/endpoints/auth.md)、[`../docs/endpoints/user-info.md`](../docs/endpoints/user-info.md)、[`../docs/overview.md`](../docs/overview.md) 与 [`API_DOC_FE.md`](./API_DOC_FE.md)。|
