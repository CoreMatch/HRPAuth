# HRPAuth 纹理管理

本站业务系统的纹理（皮肤 / 披风）管理接口，**与 Yggdrasil API 独立**，使用 **Remember Token** 鉴权。

> Yggdrasil 兼容的纹理接口见 [yggdrasil.md](./yggdrasil.md)。

> 详细实现： [`controllers/texture_controller.go`](../../controllers/texture_controller.go) · [`services/texture_service.go`](../../services/texture_service.go)

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [POST /texture/upload](#post-textureupload) | `POST` | **Remember Token** |
| [POST /texture/delete](#post-texturedelete) | `POST` | **Remember Token** |
| [POST /texture/get](#post-textureget) | `POST` | **Remember Token** |

> 三个端点所需 Token 均为 **Remember Token**（通过请求体 / 表单 / 查询参数中的 `remember_token` 字段传递）。

---

## POST /texture/upload

上传皮肤或披风的 PNG 文件。

### 鉴权

**Remember Token**

### 请求格式

`multipart/form-data`

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | 用户登录令牌 |
| `profile_id` | string | 否 | 角色 ID，缺省时取用户的第一个角色 |
| `texture_type` | string | 是 | `skin`（皮肤）或 `cape`（披风） |
| `model` | string | 否 | 皮肤模型，`default`（默认）或 `slim`（纤细），仅 `texture_type=skin` 有效 |
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

**Remember Token**

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
| `remember_token` | string | 是 | 用户登录令牌 |
| `profile_id` | string | 否 | 角色 ID，缺省时取用户的第一个角色 |
| `texture_type` | string | 是 | `skin`（皮肤）或 `cape`（披风） |

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

**Remember Token**

### 请求体

```json
{
  "remember_token": "<Remember Token>",
  "profile_id": "uuid-xxx"
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `remember_token` | string | 是 | 用户登录令牌 |
| `profile_id` | string | 否 | 角色 ID，缺省时取用户的第一个角色 |

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

## 与 Yggdrasil 纹理 API 的关系

| 体系 | 端点 | 鉴权 | 用途 |
|------|------|------|------|
| 本站（HRPAuth） | `/texture/*` | Remember Token | 前端展示纹理列表、上传/删除纹理 |
| Yggdrasil | `/api/user/profile/:uuid/:textureType` | Yggdrasil Access Token | Minecraft 客户端在游戏中读皮肤 |

二者**操作同一份数据**：

- 本站 `/texture/upload` 成功后，Yggdrasil 的 `GET /sessionserver/session/minecraft/profile/:uuid` 也会反映新纹理
- 本站 `/texture/get` 返回的 `url` 与 Yggdrasil `GET /textures/:hash` 是同一文件

> 多数场景下，前端使用本站 `/texture/*` 即可。Minecraft 客户端在游戏中自动通过 Yggdrasil API 拉取。
