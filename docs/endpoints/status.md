# 服务状态

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
  "backend": {
    "name": "HRPAuth",
    "url": "https://auth.example.com",
    "version": "1.0.0",
    "go_version": "go1.26",
    "server_time": "2026-06-27 15:04:05"
  },
  "message": "HRPAuth Backend is running."
}
```

### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 固定为 `online` |
| `backend.name` | string | 服务名（来自 `config.site.name`） |
| `backend.url` | string | 服务对外 URL（来自 `config.site.url`） |
| `backend.version` | string | 实现版本号（来自 `config.site.version`） |
| `backend.go_version` | string | 编译时 Go 版本（`runtime.Version()`） |
| `backend.server_time` | string | 服务器当前时间（格式 `YYYY-MM-DD HH:MM:SS`） |
| `message` | string | 固定描述文本 |

### 用途

- 部署后探活
- 前端启动时验证后端可达
- 监控平台抓取版本号
