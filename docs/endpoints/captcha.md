# 图形验证码

服务端颁发的图形验证码体系，用于在 `POST /register` 提交时验证请求方不是自动化脚本。

> 详细实现： [`services/captcha_service.go`](../../services/captcha_service.go) · [`controllers/captcha_controller.go`](../../controllers/captcha_controller.go)

## 端点

| 端点 | 方法 | 鉴权 |
|------|------|------|
| [GET /captcha/enabled](#get-captchaenabled) | `GET` | 无 |
| [POST /captcha](#post-captcha) | `POST` | 无（仅在 `enable_captcha=true` 时开放） |
| [GET /captcha/image/:token](#get-captchaimagetoken) | `GET` | 无（token 必须是 `POST /captcha` 返回的有效 token） |

## 开关与 TTL

| 配置项 | 默认值 | 说明 |
|--------|-------|------|
| `security.enable_captcha` | `true` | 总开关。`true` 时 `/register` 强制校验、`/captcha` 开放 |
| `security.captcha_ttl` | `300` 秒 | 验证码在 Redis 中的有效期 |

## 存储位置

Redis 键 `{prefix}captcha:code:<token>`，值为本次验证码字符串，TTL 由 `captcha_ttl` 决定。

## 关键设计

- 验证码文本由后端生成并绘制为 PNG；**前端只拿到图片地址，不持有正确答案**。
- **字符数：4 位**（`Length: 4` in `services/captcha_service.go`），字符集为字母+数字，且已剔除视觉易混淆字符 `0/O/1/I/L`（见 [mojocn/base64Captcha const.go](https://github.com/mojocn/base64Captcha) 的 `TxtNumbers` / `TxtAlphabet`）。
- 比对时**大小写不敏感**（`strings.EqualFold`），且 `TrimSpace` 后再比较。
- **单次使用**：`POST /register` 校验通过后该 token 立即从 Redis 删除；同一 token 第二次提交会被判为过期。
- `/captcha/image/:token` 每次请求都会按存储的文本**重新渲染** PNG，因此不会因为"图片只生成一次"导致缓存泄露。
- **服务端强制校验**：`POST /register` 在 [`controllers/auth_controller.go`](../../controllers/auth_controller.go) 中显式调用 `captchaService.Verify(req.CaptchaToken, req.CaptchaCode)`，未通过则直接返回 `400 Invalid or expired captcha`，不会进入 email/username/password 校验与建档流程。

---

## GET /captcha/enabled

查询后端图形验证码总开关状态。**前端应在注册页加载时调用**，避免对已关闭验证码的后端调用 `/captcha` 后再根据 403 错误回退。

| 字段 | 值 |
|------|---|
| 鉴权 | 无 |

### 请求

无请求体。

### 响应

`200 OK`

```json
{
  "enabled": 1
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | int | `1` = 图形验证码开启（`security.enable_captcha=true`），`0` = 关闭 |

### 实现细节

- 直接读取 `config.AppConfig.Security.EnableCaptcha`（[`config/config.go`](../../config/config.go)）并以 `int`（0/1）形式返回，无任何副作用。

---

## POST /captcha

申请一组新的图形验证码。

| 字段 | 值 |
|------|---|
| 鉴权 | 无 |
| 副作用 | Redis 写入 `{prefix}captcha:code:<token>`，TTL = `captcha_ttl` |

### 请求

无请求体。

### 成功响应

`200 OK`

```json
{
  "success": true,
  "token": "AbCdEfGh12345678WxYz",
  "image_url": "/captcha/image/AbCdEfGh12345678WxYz",
  "expires_in": 300
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `token` | string | 本次验证码标识，调用 `/register` 时需与 `captcha_code` 配对使用 |
| `image_url` | string | 图形验证码 PNG 图片的相对地址（前端需自行拼接后端 origin，如 `https://auth.example.com/captcha/image/<token>`） |
| `expires_in` | int | 有效期（秒），与 `security.captcha_ttl` 一致 |

### 失败响应

`403 Forbidden`（后端未开启 captcha）：

```json
{
  "success": false,
  "message": "Captcha is disabled"
}
```

---

## GET /captcha/image/:token

获取图形验证码的 PNG 图片。

| 字段 | 值 |
|------|---|
| 鉴权 | 无（path 上的 token 必须是 `POST /captcha` 返回的有效 token） |

### 成功响应

- `200 OK`
- `Content-Type: image/png`
- Body：图片字节

### 失败响应

`404 Not Found`（token 未知或已过期）：

```json
{
  "success": false,
  "message": "Captcha not found or expired"
}
```

---

## 与 `/register` 的联用流程

开启图形验证码后（默认），`POST /register` 的完整处理流程：

```text
T0  浏览器/前端打开注册页
    → POST /captcha
    ← { token, image_url, expires_in }
T1  浏览器/前端将 image_url 拼成绝对 URL 放入 <img src>
    → 浏览器 GET /captcha/image/<token>
    ← image/png
T2  用户在 <input> 中输入图上识别出的 5 位字符
    → POST /register
        {
          email, username, password,
          captcha_token: <T0 拿到的 token>,
          captcha_code:  <用户输入的字符>
        }
T3  后端 services.CaptchaService.Verify(token, code)
    → Redis GET captcha:code:<token>
    → strings.EqualFold(stored, code)  // 大小写不敏感
    → Redis DEL captcha:code:<token>  // 单次使用
    → 成功则继续原有的 email / username / password 校验与建档流程
    → 失败则返回 400 "Invalid or expired captcha"
```

### `POST /register` 错误码汇总（开启 captcha 时）

| HTTP | message | 触发场景 |
|------|---------|----------|
| 400 | `Invalid or expired captcha` | 缺失 `captcha_token`/`captcha_code`、答案错误、token 已过期/已使用 |
| 400 | `Invalid email` | 邮箱格式不合法 |
| 400 | `Username too short` | 用户名 < 3 字符 |
| 400 | `Password too short` | 密码 < 6 字符 |
| 409 | `Email already registered` | 邮箱已被注册 |
| 409 | `Username already taken` | 用户名已被占用 |

### 关闭方式

将 `security.enable_captcha` 设为 `false`。此时：

- `/captcha` 与 `/captcha/image/:token` 关闭
- `/register` 不再校验 `captcha_token` / `captcha_code`
- 前端可保持现有无验证码版本无感运行
