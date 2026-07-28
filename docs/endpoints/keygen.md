# 密钥生成

## POST /generate-key

生成 2048 位 RSA 密钥对并保存到 `./keys/` 目录。

| 字段 | 值 |
|------|---|
| 方法 | `POST` |
| 鉴权 | **无**（管理类端点；建议通过反向代理限制访问） |
| 实现 | [`controllers/keygen_controller.go`](../../controllers/keygen_controller.go) |

### 用途

Yggdrasil API 在 `GET /` 响应中需要暴露 `signaturePublickey`（公钥 PEM），并在 `POST /sessionserver/session/minecraft/join` 时使用私钥签发 `serverId` 的签名。首次部署项目时必须先调用本接口生成密钥对。

### 请求

无请求体。

### 成功响应

```json
{
  "success": true,
  "message": "RSA key pair generated",
  "public_key_path": "./keys/public_key.pem",
  "private_key_path": "./keys/private_key.pem"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `public_key_path` | string | 公钥文件相对路径 |
| `private_key_path` | string | 私钥文件相对路径 |

### 文件说明

| 文件 | 内容 | 用途 |
|------|------|------|
| `keys/public_key.pem` | PEM 格式 RSA 公钥 | 在 Yggdrasil `/` 响应中作为 `signaturePublickey` 返回给客户端 |
| `keys/private_key.pem` | PEM 格式 RSA 私钥 | 在 `/sessionserver/session/minecraft/join` 中对 `serverId` 签名 |

### 注意事项

- **私钥保密**：请勿将 `keys/private_key.pem` 提交到 Git；已在 `.gitignore` 中默认忽略。
- **私钥丢失**：必须重新生成密钥对；重新生成后已发放的 AccessToken 在新服务器加入时会签名校验失败。
- **重新生成**：本接口可重复调用，会覆盖原文件。已登录客户端需重新登录。
- **部署建议**：将 `/generate-key` 端点放在内网或管理入口后，对公网关闭。
