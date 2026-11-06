# HRPAuth 纹理管理

本站业务系统的纹理（皮肤 / 披风）管理接口，**与 Yggdrasil API 独立**，使用 **Remember Token** 鉴权。

> Yggdrasil 兼容的纹理接口见 [yggdrasil.md](./yggdrasil.md)。

> 详细实现： [`controllers/texture_controller.go`](../../controllers/texture_controller.go) · [`services/texture_service.go`](../../services/texture_service.go)

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [POST /texture/upload](#post-textureupload) | `POST` | **Remember Token** |
| [POST /texture/delete](#post-texturedelete) | `POST` | **Remember Token** |
| [POST /texture/get](#post-textureget) | `POST` | **Remember Token** |
| [POST /texture/rewrite-callback](#post-texturerewrite-callback) | `POST` | **Manage Token** |
| [GET /mojang/profile/:uuid](#get-mojangprofileuuid) | `GET` | 无 |
| [GET /texture/mojang/:uuid](#get-texturemojanguuid) | `GET` | 无 |

> 三个端点所需 Token 均为 **Remember Token**（通过请求体 / 表单 / 查询参数中的 `remember_token` 字段传递）。
>
> **`auth_type` 声明**：可选，缺省即 `remember`。三个端点同样支持 **M.T. 运维代开**：`remember_token` 换成 M-T 并传 `auth_type: "manage"`，同时提供 `uid` 或 `email`（二选一）指定目标用户。后端**不再**因 token 恰好等于 M-T 而自动升级为运维模式。

---

## POST /texture/upload

上传皮肤或披风的 PNG 文件。

### 鉴权

**Remember Token**（或 **Manage Token** + `auth_type: "manage"` + uid/email）

### 请求格式

`multipart/form-data`

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | 用户登录令牌（或 M-T） |
| `profile_id` | string | 否 | 角色 ID，缺省时取用户的第一个角色 |
| `texture_type` | string | 是 | `skin`（皮肤）或 `cape`（披风） |
| `model` | string | 否 | 皮肤模型，`default`（默认）或 `slim`（纤细），仅 `texture_type=skin` 有效 |
| `uid` | string | M.T. 必填，玩家模式忽略 | 目标用户 UID（与 `email` 二选一） |
| `email` | string | M.T. 与 `uid` 二选一，玩家模式忽略 | 目标用户邮箱 |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |
| `file` | file | 是 | PNG 格式纹理文件 |

### 请求示例（curl）

```bash
curl -X POST http://localhost:8080/texture/upload \
  -F "remember_token=<Remember Token>" \
  -F "texture_type=skin" \
  -F "model=slim" \
  -F "file=@skin.png"
```

### 成功响应

```json
{
  "success": true,
  "message": "材质上传成功",
  "data": {
    "profile_id": "uuid-xxx",
    "texture_type": "skin"
  }
}
```

当分辨率超出限制或尺寸不符合标准时，响应中会附加 `warnings` 数组（上传仍然成功）：

```json
{
  "success": true,
  "message": "材质上传成功",
  "data": {
    "profile_id": "uuid-xxx",
    "texture_type": "cape",
    "warnings": [
      "cape size 128x64 exceeds standard size, but has a valid aspect ratio",
      "texture resolution 128x64 exceeds limit 1024x1024, but has a valid aspect ratio"
    ]
  }
}
```

| warnings 消息 | 含义 |
|---------------|------|
| `skin/cape size WxH exceeds standard size, but has a valid aspect ratio` | 尺寸超出标准（64x32/64x64 for skin, 64x32/22x17 for cape），但宽高比正确（2:1 或 1:1） |
| `skin/cape size WxH does not match standard proportions` | 尺寸不符合标准宽高比 |
| `texture resolution WxH exceeds limit MxN, but has a valid aspect ratio` | 分辨率超过配置上限，但宽高比正确 |
| `texture resolution WxH exceeds limit MxN and has non-standard proportions` | 分辨率超过配置上限且宽高比不符合标准 |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 400 | `无效的材质类型，只能是 skin 或 cape` | `texture_type` 不是 `skin`/`cape` |
| 400 | `Invalid PNG` | 文件不是合法 PNG |
| 413 | `File too large` | 文件超过大小限制 |
| 500 | `Upload failed` | 写文件失败 / 权限不足 |

### 副作用

- 文件保存到 `textures/` 目录，文件名为 hash
- `profile_properties` 表插入/更新对应记录
- 返回的 URL 由 [`GET /textures/:hash`](#-get-textureshash-由-yggdrasil-端点提供) 公开下载

---

## POST /texture/delete

删除皮肤或披风。

### 鉴权

**Remember Token**（或 **Manage Token** + `auth_type: "manage"` + uid/email）

### 请求体

```json
{
  "remember_token": "<Remember Token>",
  "profile_id": "uuid-xxx",
  "texture_type": "skin"
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | 用户登录令牌（或 M-T） |
| `profile_id` | string | 否 | 角色 ID，缺省时取用户的第一个角色 |
| `texture_type` | string | 是 | `skin`（皮肤）或 `cape`（披风） |
| `uid` | string | M.T. 必填，玩家模式忽略 | 目标用户 UID（与 `email` 二选一） |
| `email` | string | M.T. 与 `uid` 二选一，玩家模式忽略 | 目标用户邮箱 |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |

### 成功响应

```json
{
  "success": true,
  "message": "材质删除成功",
  "data": {
    "profile_id": "uuid-xxx",
    "texture_type": "skin"
  }
}
```

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 400 | `无效的材质类型，只能是 skin 或 cape` | `texture_type` 不合法 |
| 404 | `Texture not found` | 该角色没有此类型的纹理 |

---

## POST /texture/get

获取指定角色的纹理信息（含完整 URL）。

### 鉴权

**Remember Token**（或 **Manage Token** + `auth_type: "manage"` + uid/email）

### 请求体

```json
{
  "remember_token": "<Remember Token>",
  "profile_id": "uuid-xxx"
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | 用户登录令牌（或 M-T） |
| `profile_id` | string | 否 | 角色 ID，缺省时取用户的第一个角色 |
| `uid` | string | M.T. 必填，玩家模式忽略 | 目标用户 UID（与 `email` 二选一） |
| `email` | string | M.T. 与 `uid` 二选一，玩家模式忽略 | 目标用户邮箱 |
| `auth_type` | string | M.T. 必填 `"manage"` | 声明 token 类型，缺省 `remember` |

### 成功响应

```json
{
  "success": true,
  "message": "获取材质信息成功",
  "data": {
    "profile_id": "uuid-xxx",
    "textures": [
      {
        "texture_type": "skin",
        "url": "https://auth.example.com/textures/abc123...",
        "model": "slim"
      },
      {
        "texture_type": "cape",
        "url": "https://auth.example.com/textures/def456..."
      }
    ]
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `data.profile_id` | string | 角色 ID |
| `data.textures` | array | 纹理列表（可能为空、`skin` 存在、`cape` 存在，或两者都存在） |
| `data.textures[].texture_type` | string | `skin` 或 `cape` |
| `data.textures[].url` | string | 纹理文件绝对 URL（可直接用于 `<img src>`） |
| `data.textures[].model` | string | 仅 `skin` 有此字段，`default` 或 `slim` |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid token` | Remember Token 缺失或错误 |
| 404 | `Profile not found` | 角色不存在或不属于该用户 |

---

## POST /texture/rewrite-callback

批量重写所有已存储材质的 callback URL。把所有角色的 `skin` / `cape` 材质 URL 域名统一替换为**当前配置** `callback.url`，保留 `/textures/{hash}` 后缀，并对重写后的值重新签名。用于迁移域名 / 更换 `callback.url` 后修复旧数据。

### 鉴权

**Manage Token（M-T）**：必须提供 `manage_token`（等于 `config.yaml` 中 `manage.token`）。该端点为运维专用，不校验用户身份，也无需声明 `auth_type`。

### 请求体

```json
{
  "manage_token": "<M-T>",
  "dry_run": true
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `manage_token` | string | 是 | 运营级 M-T（也支持表单 / 查询参数传递） |
| `dry_run` | bool | 否 | `true` 时只预览统计，不写库（也支持表单 / 查询参数 `dry_run=true`） |

### 请求示例（curl）

```bash
curl -X POST http://localhost:8080/texture/rewrite-callback \
  -F "manage_token=<M-T>" \
  -F "dry_run=true"
```

### 成功响应

```json
{
  "success": true,
  "message": "材质 callback 重写完成",
  "data": {
    "callback_url": "https://auth.example.com",
    "dry_run": true,
    "total": 3,
    "rewritten": 2,
    "unchanged": 1,
    "failed": 0,
    "errors": []
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `data.callback_url` | string | 重写目标域名（`config.yaml` 的 `callback.url`，去尾斜杠） |
| `data.dry_run` | bool | 是否为预览模式 |
| `data.total` | int | 扫描到的 `profile_properties.textures` 记录总数 |
| `data.rewritten` | int | URL 实际发生变化并（非 dry-run 时）已写库的记录数 |
| `data.unchanged` | int | 未发生变化（已是目标域名）的记录数 |
| `data.failed` | int | 解码 / 重签名 / 写库失败的记录数 |
| `data.errors` | array | 失败明细（`profile <id>: <原因>`），无失败时为空数组 |

### 失败响应

| HTTP | message | 触发场景 |
|------|---------|----------|
| 401 | `Invalid manage token` | `manage_token` 缺失或与 `manage.token` 不匹配 |

> 注意：非 dry-run 时，重写会重新调用 `SignTextureValue` 对值签名，因此要求配置了 `yggdrasil.server.signature_private_key_path`；若签名失败，该条记录不会写库并计入 `failed`。

---

## GET /mojang/profile/:uuid

通过后端代理获取 Mojang 玩家 Profile 信息（不含材质数据）。前端不直接调用任何外部 Mojang API，所有外部数据均经由后端代理。

### 鉴权

**无**（公开端点）

### 路径参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `uuid` | string | 是 | Mojang UUID，支持带连字符或不带（如 `069a79f4-44e9-4726-a5be-fca90e38aaf5` 或 `069a79f444e94726a5befca90e38aaf5`） |

### 成功响应

```json
{
  "success": true,
  "message": "获取 Mojang Profile 成功",
  "id": "069a79f444e94726a5befca90e38aaf5",
  "name": "Notch",
  "has_cape": true
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | Mojang UUID（无连字符） |
| `name` | string | 玩家显示名 |
| `has_cape` | bool | 该玩家是否拥有披风 |

### 失败响应

| HTTP | code | message | 触发场景 |
|------|------|---------|----------|
| 400 | `invalid_mojang_uuid` | `无效的 Mojang UUID` | UUID 长度不是 32 位 |
| 404 | `user_not_found` | `该 UUID 在 Mojang 服务器上不存在` | Mojang Session Server 返回 204/404 |
| 502 | `texture_fetch_failed` | `无法连接 Mojang Session Server: ...` | 网络错误或 Mojang 服务异常 |

### 副作用

- 后端调用 `sessionserver.mojang.com`，下载量极小（仅 JSON 元数据）
- 不下载任何材质 PNG 文件

---

## GET /texture/mojang/:uuid

通过后端代理获取 Mojang 玩家的皮肤或披风材质 PNG 文件。前端不直接调用任何外部 Mojang API，所有外部数据均经由后端代理。

### 鉴权

**无**（公开端点）

### 路径参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `uuid` | string | 是 | Mojang UUID，支持带连字符或不带 |

### 查询参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `type` | string | 否 | `skin` | `skin`（皮肤）或 `cape`（披风） |

### 成功响应

直接返回 PNG 图片二进制流，`Content-Type: image/png`，`Cache-Control: public, max-age=300`。

### 失败响应

```json
{
  "success": false,
  "message": "该 UUID 在 Mojang 服务器上不存在",
  "code": "user_not_found"
}
```

| HTTP | code | message | 触发场景 |
|------|------|---------|----------|
| 400 | `invalid_mojang_uuid` | `无效的 Mojang UUID` | UUID 长度不是 32 位 |
| 400 | `invalid_texture_type` | `无效的材质类型，只能是 skin 或 cape` | `type` 参数不合法 |
| 404 | `user_not_found` | `该 UUID 在 Mojang 服务器上不存在` | Mojang Session Server 返回 204/404 |
| 404 | `texture_fetch_failed` | `该玩家没有皮肤材质` / `该玩家没有披风材质` | 玩家没有对应类型的材质 |
| 502 | `texture_fetch_failed` | `无法连接 Mojang Session Server: ...` / `下载材质文件失败: ...` | 网络错误或 Mojang CDN 异常 |

### 副作用

- 后端调用 `sessionserver.mojang.com` 获取 Profile
- 后端从 `textures.mojang.com` CDN 下载 PNG 文件并转发给调用方
- 返回的 PNG 带 5 分钟缓存（`Cache-Control: public, max-age=300`）

---

## 与 Yggdrasil 纹理 API 的关系

| 体系 | 端点 | 鉴权 | 用途 |
|------|------|------|------|
| 本站（HRPAuth） | `/texture/*` | Remember Token | 前端展示纹理列表、上传/删除纹理 |
| Yggdrasil | `/api/user/profile/:uuid/:textureType` | Yggdrasil Access Token | Minecraft 客户端在游戏中读皮肤 |

二者**操作同一份数据**：

- 本站 `/texture/upload` 成功后，Yggdrasil 的 `GET /sessionserver/session/minecraft/profile/:uuid` 也会反映新纹理
- 本站 `/texture/get` 返回的 `url` 与 Yggdrasil `GET /textures/:hash` 是同一文件

> 多数场景下，前端使用本站 `/texture/*` 即可。Minecraft 客户端在游戏中自动通过 Yggdrasil API 拉取。
