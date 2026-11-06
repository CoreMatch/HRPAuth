# 用户资料管理

修改用户名与 Minecraft 角色名。

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [POST /change-username](#post-change-username) | `POST` | **Remember Token** |
| [POST /change-profile-name](#post-change-profile-name) | `POST` | **Remember Token** |

> 详细实现： [`controllers/user_profile_controller.go`](../../controllers/user_profile_controller.go)

> 两个端点所需 Token 均为 **Remember Token**（通过请求体 / 表单 / 查询参数中的 `remember_token` 字段传递）。
>
> **`auth_type` 声明**：可选，缺省即 `remember`。两个端点同样支持 **M.T. 运维代开**：`remember_token` 换成 M-T 并传 `"auth_type": "manage"`，同时提供 `uid` 或 `email`（二选一）指定目标用户。后端**不再**因 token 恰好等于 M-T 而自动升级为运维模式。

---

## POST /change-username

修改用户在本站的用户名（`users.username`）。

### 鉴权

**Remember Token**（或 **Manage Token** + `auth_type: "manage"` + uid/email）

### 请求体

```json
{
  "remember_token": "<Remember Token>",
  "username": "NewName"
}
```

运维代开（M.T.）：

```json
{ "remember_token": "<Manage Token>", "uid": "42", "auth_type": "manage", "username": "NewName" }
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | Remember Token 或 Manage Token |
| `username` | string | 是 | 新用户名（≥ 3 字符，需唯一） |
| `uid` | string | M.T. 必填，玩家模式忽略 | 目标用户 UID（与 `email` 二选一） |
| `email` | string | M.T. 与 `uid` 二选一，玩家模式忽略 | 目标用户邮箱 |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |

### 成功响应

```json
{
  "success": true,
  "message": "Username updated"
}
```

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 400 | `Username too short` | 用户名 < 3 字符 |
| 409 | `Username already taken` | 新用户名已被其他用户占用 |

---

## POST /change-profile-name

修改 Minecraft 角色名（`profiles.name`）。**注意：修改角色名会影响该角色在 Yggdrasil 体系中的显示**。

### 鉴权

**Remember Token**（或 **Manage Token** + `auth_type: "manage"` + uid/email）

### 请求体

```json
{
  "remember_token": "<Remember Token>",
  "profile_id": "uuid-xxx",
  "name": "NewPlayerName"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | Remember Token 或 Manage Token |
| `profile_id` | string | 是 | 要修改的角色 ID（UUID） |
| `name` | string | 是 | 新的 Minecraft 角色名 |
| `uid` | string | M.T. 必填，玩家模式忽略 | 目标用户 UID（与 `email` 二选一） |
| `email` | string | M.T. 与 `uid` 二选一，玩家模式忽略 | 目标用户邮箱 |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |

### 成功响应

```json
{
  "success": true,
  "message": "Profile name updated"
}
```

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 404 | `Profile not found` | `profile_id` 不存在或不属于该用户 |
| 409 | `Profile name already taken` | 新角色名已被其他角色占用 |

### 副作用

- 更新 `profiles.name`
- 由于 Yggdrasil API 通过 `name` 索引角色，**Minecraft 客户端需重启或重新登录**才能看到新角色名生效。
