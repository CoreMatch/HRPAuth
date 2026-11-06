# 服务状态 (已弃用)

> [!WARNING]
> 该接口已标记为 **已弃用 (Deprecated)**。访问 `/status` 将会临时重定向 (307 Redirect) 到根路径 `/`。
> 请改用根路径 `/` 获取服务状态与元数据信息。

## GET /status

获取后端服务的基础信息，供前端 / 监控探测服务是否在线。

| 字段 | 值 |
|------|---|
| 方法 | `GET` |
| 路径 | `/status` |
| 鉴权 | 无（公开） |
| 实现 | [`controllers/startup_controller.go`](../../controllers/startup_controller.go) |
| 路由 | [`main.go`](../../main.go) |

### 请求

无请求体。

### 响应

`200 OK`

```json
{
  "status": "online",
  "success": true,
  "message": "HRPAuth Backend is running.",
  "site": {
    "name": "HRPAuth",
    "url": "https://auth.example.com",
    "version": "1.0.0",
    "go_version": "go1.26",
    "server_time": "2026-06-27 15:04:05"
  },
  "yggdrasil": {
    "meta": {
      "serverName": "HRPAuth",
      "implementationName": "HRPAuth-Backend-Go",
      "implementationVersion": "1.0.0",
      "links": {
        "homepage": "https://auth.example.com",
        "register": "https://auth.example.com/register"
      },
      "feature.non_email_login": false,
      "feature.legacy_skin_api": false,
      "feature.no_mojang_namespace": true,
      "feature.enable_mojang_anti_features": false,
      "feature.enable_profile_key": true,
      "feature.username_check": false
    },
    "skinDomains": [
      "example.com",
      ".example.com"
    ],
    "signaturePublickey": "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"
  },
  "meta": {
    "request_id": "..."
  }
}
```

### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 固定为 `online` |
| `success` | bool | 是否成功 |
| `message` | string | 固定描述文本 |
| `site.name` | string | 服务名（来自 `config.site.name`） |
| `site.url` | string | 服务对外 URL（来自 `config.site.url`） |
| `site.version` | string | 实现版本号（来自 `config.site.version`） |
| `site.go_version` | string | 编译时 Go 版本（`runtime.Version()`） |
| `site.server_time` | string | 服务器当前时间（格式 `YYYY-MM-DD HH:MM:SS`） |
| `yggdrasil` | object | Yggdrasil 体系元数据 |
| `meta.request_id` | string | 请求唯一标识符 |

### 用途

- 部署后探活
- 前端启动时验证后端可达
- 监控平台抓取版本号
