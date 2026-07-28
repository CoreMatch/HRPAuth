# Yggdrasil API

兼容 [Yggdrasil](https://github.com/yushijinhun/authlib-injector/wiki/Yggdrasil-%E6%8E%A5%E5%8F%A3%E8%AF%B4%E6%98%8E) 规范的 Minecraft 认证端点。**与本站业务系统（Remember Token）完全独立**，使用 **Yggdrasil Access Token + Yggdrasil Client Token** 体系。

> 详细实现： [`controllers/yggdrasil_controller.go`](../../controllers/yggdrasil_controller.go) · [`services/auth_service.go`](../../services/auth_service.go)
>
> Token 状态机与生命周期见 [../tokens.md](../tokens.md)。

> ⚠️ **这些端点由 Minecraft 客户端 / Authlib-Injector 调用**，**前端不要直接调用**。本站前端只对接 Remember Token 体系（见其他端点文档）。

## 端点总览

### 元信息

| 方法 | 路径 | 描述 | 鉴权 |
|------|------|------|------|
| `GET` | [`/`](#get-) | 获取服务器元信息 | 无 |

### 认证服务器 (authserver)

| 方法 | 路径 | 描述 | 鉴权 |
|------|------|------|------|
| `POST` | [`/authserver/authenticate`](#post-authserverauthenticate) | 用户认证（颁发 AccessToken + ClientToken） | 凭 username + password |
| `POST` | [`/authserver/refresh`](#post-authserverrefresh) | 刷新令牌 | AccessToken + ClientToken |
| `POST` | [`/authserver/validate`](#post-authservervalidate) | 验证令牌 | AccessToken + ClientToken |
| `POST` | [`/authserver/invalidate`](#post-authserverinvalidate) | 使令牌失效 | AccessToken + ClientToken |
| `POST` | [`/authserver/signout`](#post-authserversignout) | 账号登出（吊销该用户所有 token） | 凭 username + password |

### 会话服务器 (sessionserver)

| 方法 | 路径 | 描述 | 鉴权 |
|------|------|------|------|
| `POST` | [`/sessionserver/session/minecraft/join`](#post-sessionserversessionminecraftjoin) | 加入服务器会话 | AccessToken（请求体） |
| `GET` | [`/sessionserver/session/minecraft/hasJoined`](#get-sessionserversessionminecrafthasjoined) | 检查玩家是否在服务器 | 仅查询参数 |
| `GET` | [`/sessionserver/session/minecraft/profile/:uuid`](#get-sessionserversessionminecraftprofileuuid) | 查询玩家资料 | 无（公开） |

### 资料 / 纹理 API

| 方法 | 路径 | 描述 | 鉴权 |
|------|------|------|------|
| `POST` | [`/api/profiles/minecraft`](#post-apiprofilesminecraft) | 批量查询玩家资料 | 无（公开） |
| `PUT` | [`/api/user/profile/:uuid/:textureType`](#put-apiuserprofileuuidtexturetype) | 上传纹理 | AccessToken（Bearer） |
| `DELETE` | [`/api/user/profile/:uuid/:textureType`](#delete-apiuserprofileuuidtexturetype) | 删除纹理 | AccessToken（Bearer） |
| `GET` | [`/textures/:hash`](#get-textureshash) | 下载纹理文件 | 无（公开） |

---

## GET /

获取 Yggdrasil 服务元信息。

### 响应

```json
{
  "meta": {
    "serverName": "HRPAuth",
    "implementationName": "HRPAuth",
    "implementationVersion": "1.0.0",
    "links": {
      "homepage": "https://github.com/yourname/HRPAuth-Backend-Go"
    }
  },
  "skinDomains": ["example.com"],
  "signaturePublickey": "<PEM 公钥>"
}
```

| 字段 | 说明 |
|------|------|
| `signaturePublickey` | 由 `POST /generate-key` 生成的 2048 位 RSA 公钥，Authlib-Injector 与 Mojang 客户端据此校验 `serverId` 签名 |

---

## POST /authserver/authenticate

颁发 AccessToken + ClientToken。**同 `clientToken` 幂等**：复用旧行并刷新 `issued_at`；**不同 `clientToken` 互踢**：其他 client 的 `valid` token → `temporarily_invalid`。

### 请求体

```json
{
  "username": "user@example.com",
  "password": "password123",
  "clientToken": "<可由客户端自传;不传则服务端生成>",
  "requestUser": false
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `username` | string | 是 | 邮箱（**不是用户名**，本项目用邮箱登录） |
| `password` | string | 是 | 明文密码 |
| `clientToken` | string | 否 | 客户端令牌；不传则服务端生成 |
| `requestUser` | bool | 否 | 是否在响应中返回用户信息（默认 false） |

### 成功响应

```json
{
  "accessToken": "<新颁发的 AccessToken 或同 clientToken 复用的旧值>",
  "clientToken": "<回传的 clientToken>",
  "selectedProfile": {
    "id": "uuid-xxx",
    "name": "PlayerOne"
  },
  "availableProfiles": [
    { "id": "uuid-xxx", "name": "PlayerOne" }
  ]
}
```

### 失败响应

| HTTP | error | 触发场景 |
|------|-------|----------|
| 403 | `ForbiddenOperationException` | 邮箱或密码错误 |
| 403 | `ForbiddenOperationException` | 用户无 profile（未生成 Minecraft 角色） |

### 幂等与踢人详解

详见 [../tokens.md#3-authenticate-幂等与踢人](../tokens.md#3-authenticate-幂等与踢人)。

---

## POST /authserver/refresh

刷新 AccessToken。**接受 `state IN ('valid', 'temporarily_invalid')`**，被踢的 client 可借 `/refresh` 抢回会话。

### 请求体

```json
{
  "accessToken": "<旧 AccessToken>",
  "clientToken": "<对应的 clientToken>",
  "requestUser": false
}
```

### 成功响应

同 `/authserver/authenticate`，但 `accessToken` 必为新生成。

### 失败响应

| HTTP | error | 触发场景 |
|------|-------|----------|
| 403 | `ForbiddenOperationException` | accessToken 与 clientToken 不匹配 / token 已 `invalid` |
| 403 | `ForbiddenOperationException` | token `state` 既不是 `valid` 也不是 `temporarily_invalid` |

### 抢回与踢人详解

详见 [../tokens.md#4-refresh-抢回与踢人](../tokens.md#4-refresh-抢回与踢人)。

---

## POST /authserver/validate

验证 AccessToken + ClientToken 是否有效（**仅接受 `state='valid'`**）。

### 请求体

```json
{
  "accessToken": "<AccessToken>",
  "clientToken": "<clientToken>"
}
```

### 成功响应

`204 No Content`（无 body）

### 失败响应

`403 ForbiddenOperationException` — token 不存在、不匹配 clientToken、或 `state != 'valid'`

---

## POST /authserver/invalidate

使单个 AccessToken 失效（**仅接受 `state='valid'`**；已被踢到 `temporarily_invalid` 的 token 不能再 invalidate）。

### 请求体

```json
{
  "accessToken": "<AccessToken>",
  "clientToken": "<clientToken>"
}
```

### 成功响应

`204 No Content`

### 失败响应

`403 ForbiddenOperationException` — token 状态不允许 invalidate

### 副作用

该行 `state` → `invalid`；下一次后台 cleanup 任务（每小时）会物理删除。

---

## POST /authserver/signout

账号登出，**吊销该用户的所有 token**（valid / temporarily_invalid / invalid 都会被处理；新签发的 AccessToken 也会被删除）。

### 请求体

```json
{
  "username": "user@example.com",
  "password": "password123"
}
```

### 成功响应

`204 No Content`

### 失败响应

`403 ForbiddenOperationException` — 邮箱或密码错误

### 用途

用户在 Minecraft 启动器点击"退出登录"时调用，吊销该用户在本服务上的所有会话。

---

## POST /sessionserver/session/minecraft/join

由 Minecraft 客户端在加入服务器时调用。后端校验 AccessToken（`state='valid'`），并使用**私钥**对 `serverId` 签名。

### 请求体

```json
{
  "accessToken": "<AccessToken>",
  "selectedProfile": "uuid-xxx",
  "serverId": "<Mojang/Authlib 计算的 SHA-1 哈希>"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `accessToken` | string | 是 | AccessToken |
| `selectedProfile` | string | 是 | 角色 UUID（不含连字符） |
| `serverId` | string | 是 | 客户端计算的服务端 ID 哈希 |

### 成功响应

`204 No Content`

### 失败响应

| HTTP | error | 触发场景 |
|------|-------|----------|
| 403 | `ForbiddenOperationException` | AccessToken 缺失 / 无效 / `state != 'valid'` / 角色不匹配 |

### 副作用

- 将 (serverId, accessToken, profileId) 写入 `sessions` 表，供后续 `hasJoined` 查询

---

## GET /sessionserver/session/minecraft/hasJoined

由 Minecraft **服务端**（不是客户端）调用，校验某玩家是否真的在指定服务器上加入了。

### 查询参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `username` | string | 是 | 玩家用户名 |
| `serverId` | string | 是 | 服务端在 join 时拿到的 serverId |
| `ip` | string | 否 | 可选，校验 IP 一致性 |

### 成功响应

`200 OK`

```json
{
  "id": "uuid-xxx",
  "name": "PlayerOne",
  "properties": [
    {
      "name": "textures",
      "value": "<base64 编码的纹理 JSON>",
      "signature": "<用私钥对该 value 的 RSA 签名>"
    }
  ]
}
```

### 失败响应

`204 No Content` — 玩家未在该 serverId 上 join，或 IP 不匹配

---

## GET /sessionserver/session/minecraft/profile/:uuid

查询指定角色的公开资料（含纹理签名）。

### 路径参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `uuid` | string | 角色 UUID（不含连字符） |

### 成功响应

```json
{
  "id": "uuid-xxx",
  "name": "PlayerOne",
  "properties": [
    {
      "name": "textures",
      "value": "<base64 编码的纹理 JSON>",
      "signature": "<用私钥对该 value 的 RSA 签名>"
    }
  ]
}
```

### 失败响应

`404 Not Found` — 角色不存在

> `signature` 字段对于 Minecraft 客户端判断"皮肤是否官方未篡改"至关重要。私钥必须保密（见 [keygen.md](./keygen.md)）。

---

## POST /api/profiles/minecraft

批量按用户名查角色 UUID。

### 请求体

```json
["PlayerOne", "PlayerTwo"]
```

### 响应

```json
[
  { "id": "uuid-xxx", "name": "PlayerOne" }
]
```

> 未找到的用户名不会出现在响应数组中（不是 404）。

---

## PUT /api/user/profile/:uuid/:textureType

通过 Yggdrasil Access Token 上传纹理。**前端通常不需要直接调用**（本站有更易用的 [`POST /texture/upload`](./texture.md#post-textureupload)）。

### 路径参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `uuid` | string | 角色 UUID |
| `textureType` | string | `skin` 或 `cape` |

### 请求头

```
Authorization: Bearer <Yggdrasil AccessToken>
```

### 请求体

`multipart/form-data`：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `file` | file | 是 | PNG 文件 |
| `model` | string | 否 | 皮肤模型 `default` 或 `slim` |

### 成功响应

```json
{ "id": "uuid-xxx", "name": "PlayerOne", "properties": [...] }
```

### 失败响应

| HTTP | 触发场景 |
|------|----------|
| 401 | AccessToken 缺失或无效 |
| 403 | AccessToken `state != 'valid'` 或不属于该角色 |
| 400 | PNG 非法 / 尺寸超限（`yggdrasil.security.max_texture_width/height`） |

---

## DELETE /api/user/profile/:uuid/:textureType

通过 Yggdrasil Access Token 删除纹理。

### 路径参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `uuid` | string | 角色 UUID |
| `textureType` | string | `skin` 或 `cape` |

### 请求头

```
Authorization: Bearer <Yggdrasil AccessToken>
```

### 成功响应

`204 No Content`

### 失败响应

| HTTP | 触发场景 |
|------|----------|
| 401 | AccessToken 缺失或无效 |
| 403 | AccessToken `state != 'valid'` 或不属于该角色 |

---

## GET /textures/:hash

下载纹理文件（公开，无鉴权）。Minecraft 客户端在拿到 textures 签名后会直接 `GET` 此 URL。

### 路径参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `hash` | string | 纹理文件 hash（响应中 textures.url 的最后一段） |

### 成功响应

`200 OK`，`Content-Type: image/png`，body 为 PNG 字节

### 失败响应

`404 Not Found` — 文件不存在
