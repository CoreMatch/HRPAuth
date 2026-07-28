# 功能开关（Feature Flags）

Yggdrasil 协议层的功能开关，对应 `yggdrasil.feature_flags.*` 配置项。

> HRPAuth 自身的开关（`security.*`）见 [configuration.md](./configuration.md)。

| 配置项 | 默认 | 说明 |
|--------|------|------|
| `non_email_login` | `true` | 允许 `POST /authserver/authenticate` 凭 **Minecraft 角色名** 登录（除邮箱外）。实现见 [`../services/auth_service.go::VerifyCredentials`](../services/auth_service.go)；当 `true` 时用户可输入 `email` 或 `profiles.name`。仅影响 Yggdrasil 端点，`POST /login`（本站）仍只接受邮箱 |
| `legacy_skin_api` | `false` | 启用旧版皮肤 API（`/skins/MinecraftSkins/...`）。新版客户端不再使用 |
| `no_mojang_namespace` | `false` | 不使用 Mojang 命名空间。开启后角色 properties 的 namespace 不带 `minecraft:` 前缀 |
| `enable_mojang_anti_features` | `false` | 启用 Mojang 反作弊特性。具体行为由客户端解释 |
| `enable_profile_key` | `false` | 启用资料密钥（Yggdrasil 1.1+）。Mojang 1.19+ 客户端会请求 `/player/certificates` 之类的新端点 |
| `username_check` | `true` | 启用用户名检查（限制 Minecraft 角色名格式）。**强烈建议保持 `true`** |

## 在响应中的位置

`GET /`（Yggdrasil 元信息）的 `meta` 段会原样回传部分开关：

```json
{
  "meta": {
    "serverName": "HRPAuth",
    "implementationName": "HRPAuth",
    "implementationVersion": "1.0.0"
  },
  "feature.non_email_login": false,
  "feature.legacy_skin_api": false,
  "feature.no_mojang_namespace": false,
  "feature.enable_mojang_anti_features": false,
  "feature.enable_profile_key": false,
  "feature.username_check": true
}
```

> 具体回传字段取决于实现，可在 [`controllers/yggdrasil_controller.go`](../../controllers/yggdrasil_controller.go) 中查看。
