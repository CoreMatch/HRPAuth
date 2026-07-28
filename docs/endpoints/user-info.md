# 用户信息

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [POST /user](#post-user) | `POST` | **Remember Token** |
| [POST /user/mojang-bind-enable](#post-usermojang-bind-enable) | `POST` | **Remember Token** 或 **Manage Token** |

> 详细实现： [`controllers/user_info_controller.go`](../../controllers/user_info_controller.go)

---

## POST /user

获取当前登录用户的基础信息。

| 字段 | 值 |
|------|---|
| 方法 | `POST` |
| 鉴权 | **Remember Token** |
| 实现 | [`controllers/user_info_controller.go`](../../controllers/user_info_controller.go) |

> 虽以 `POST` 实现读操作，是为了方便前端把 Remember Token 放在请求体里（避免日志泄露）。

### Remember Token 传递方式

三种任选其一（后端依次识别）：

1. 请求体 JSON 字段 `remember_token`
2. 查询参数 `?remember_token=xxx`
3. 表单字段

### 请求体

```json
{
  "remember_token": "<Remember Token>",
  "uid": "1",
  "email": "user@example.com"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | Remember Token |
| `uid` | string | 否 | 用户 UID（与 `email` 联合校验，**任一匹配登录用户即可**，建议同时传） |
| `email` | string | 否 | 用户邮箱（同上） |

### 成功响应

```json
{
  "success": true,
  "message": "获取用户信息成功",
  "data": {
    "uid": 1,
    "email": "user@example.com",
    "username": "PlayerOne",
    "avatar": "",
    "verified": true
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `data.uid` | int | 用户 ID |
| `data.email` | string | 邮箱 |
| `data.username` | string | 用户名 |
| `data.avatar` | string | 头像 URL（当前版本未使用，留空） |
| `data.verified` | bool | 邮箱是否已验证 |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 404 | `User not found` | Token 有效但用户不存在（极端情况） |

### 用途

- 已登录态刷新时验证 Token 仍有效
- 拉取用户最新信息（用户名、邮箱、验证状态）
- 决定是否引导用户完成邮箱验证或开启 TOTP

---

## POST /user/mojang-bind-enable

开启指定用户的 **MBE（Mojang Bind Enabled）** 开关。MBE=1 后，未绑定的 Mojang 正版玩家撞名进服时，HA 端 `POST /register`（M.T. 路径）决策树 §3.4 2.a 会**允许绑定**（写 `mojang_uuid` + 更新 `last_sign_at`，**保留** WebUI 用户的 `password`/`email`/`cbh`）；MBE=0 时同名 Mojang 玩家收到 `409 username_already_bound`，被踢出（HA 优先）。

| 字段 | 值 |
|------|---|
| 方法 | `POST` |
| 鉴权 | **Remember Token**（玩家自开）或 **Manage Token**（运维代开，需附加 `uid` 或 `email`）|
| 幂等 | 是（已开启时返回 200）|

### Remember Token 传递方式

三种任选其一（后端依次识别）：

1. 请求体 JSON 字段 `remember_token`
2. 查询参数 `?remember_token=xxx`
3. 表单字段

### 请求体

**玩家自己开**（用 Remember Token）：

```json
{ "remember_token": "<Remember Token>" }
```

**运维代开**（用 Manage Token + uid）：

```json
{ "remember_token": "<Manage Token>", "uid": "42" }
```

或（用 Manage Token + email）：

```json
{ "remember_token": "<Manage Token>", "email": "user@example.com" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | Remember Token 或 Manage Token |
| `uid` | string | M.T. 必填，玩家模式忽略 | 目标用户 UID |
| `email` | string | M.T. 与 `uid` 二选一，玩家模式忽略 | 目标用户邮箱 |

> 玩家模式下 `uid` / `email` 字段被忽略，仅按 `remember_token` 定位自己。
> 运维（M.T.）模式下 `uid` 与 `email` 必须二选一，否则 400。

### 成功响应

`200 OK`

```json
{
  "success": true,
  "message": "Mojang bind enabled",
  "data": { "uid": 42, "mbe": 1 }
}
```

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 200 (业务失败) | `未登录或登录已过期` | Remember Token 缺失 |
| 200 (业务失败) | `Manage Token 需要指定 uid 或 email` | M.T. 路径下未指定目标用户 |
| 200 (业务失败) | `用户不存在或token无效` | Token 无效或对应用户不存在 |
| 500 | `Failed to enable mojang bind` | DB 异常 |

### 副作用

仅更新 `users.mbe = 1`（无其他字段被修改）。**绑定成功后 `mbe` 字段无意义**（`mojang_uuid` 一旦设置，§3.4 2.a 不会再触发），但本端点不会主动重置 `mbe`，便于后续手动查询当前授权状态。如需 disable，目前需直接 SQL 或扩展端点。
