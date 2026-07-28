# 项目概述

## 项目简介

HRPAuth-Backend-Go 是一个基于 **Go + Gin** 框架的 Minecraft 认证后端服务，完整实现了 [Yggdrasil](https://github.com/yushijinhun/authlib-injector/wiki/Yggdrasil-%E6%8E%A5%E5%8F%A3%E8%AF%B4%E6%98%8E) API 规范，可作为 Authlib-Injector 的后端使用，使 Minecraft 客户端在第三方服务器完成"正版登录"。

除兼容 Yggdrasil 外，项目同时维护一套**本站业务系统**（Remember Token 体系），用于承载注册、登录、邮箱验证、TOTP、用户资料、纹理管理等功能。

## 技术栈

| 类别 | 选型 |
|------|------|
| 语言 | Go |
| Web 框架 | [Gin](https://github.com/gin-gonic/gin) |
| ORM | [GORM](https://gorm.io/) |
| 数据库 | MySQL |
| 缓存 | Redis（验证码、邮件验证码、限流计数等） |
| 加密 | bcrypt（密码）、RSA（Yggdrasil 签名） |
| 认证 | Yggdrasil API / Authlib-Injector |
| 验证码图片 | [mojocn/base64Captcha](https://github.com/mojocn/base64Captcha) |
| TOTP | Google Authenticator 兼容（Base32 共享密钥 + 6 位动态口令） |

## 模块划分

| 模块 | 说明 | 文档 |
|------|------|------|
| 服务状态 | 提供 `/status` 健康检查 | [endpoints/status.md](./endpoints/status.md) |
| 认证 | 注册 / 登录 / 登出 | [endpoints/auth.md](./endpoints/auth.md) |
| 图形验证码 | 注册前置校验 | [endpoints/captcha.md](./endpoints/captcha.md) |
| 用户信息 | 获取当前登录用户资料 | [endpoints/user-info.md](./endpoints/user-info.md) |
| 邮箱验证 | 6 位数字验证码下发与校验 | [endpoints/email-verification.md](./endpoints/email-verification.md) |
| TOTP | 两步验证的设置 / 校验 / 状态查询 | [endpoints/totp.md](./endpoints/totp.md) |
| 用户资料 | 用户名、Minecraft 角色名修改 | [endpoints/user-profile.md](./endpoints/user-profile.md) |
| 密钥生成 | 生成 2048 位 RSA 密钥对 | [endpoints/keygen.md](./endpoints/keygen.md) |
| 纹理管理 | 皮肤 / 披风的上传、删除、查询 | [endpoints/texture.md](./endpoints/texture.md) |
| Yggdrasil API | authserver / sessionserver / 资料 API | [endpoints/yggdrasil.md](./endpoints/yggdrasil.md) |

## 两套鉴权体系并存

本项目存在两套**完全独立**的鉴权体系，开发时务必区分：

| 体系 | 适用场景 | Token 名称 | 颁发端点 |
|------|---------|-----------|---------|
| 本站业务系统 | HRPAuth-WEBUI | **Remember Token** | `POST /login` |
| Yggdrasil | Minecraft 客户端 / Authlib-Injector | **Access Token + Client Token** | `POST /authserver/authenticate` |

二者**不能混用**：

- 本站所有 `/api/*`（除 Yggdrasil 之外）只接受 Remember Token。
- Yggdrasil 端点（`/authserver/*`、`/sessionserver/*`、`/api/user/profile/*`）只接受 Access Token + Client Token。

详细 Token 定义、字段、状态机见 [tokens.md](./tokens.md)。

## 目录结构

```
HRPAuth-Backend-Go/
├── main.go                 # 入口（路由注册、启动后台任务）
├── config/                 # 配置加载
├── controllers/            # HTTP 处理器（按模块拆文件）
├── services/               # 业务服务层
├── models/                 # GORM 数据模型
├── database/               # 数据库连接
├── redis/                  # Redis 连接
├── utils/                  # 工具函数（Token 生成、密码哈希等）
├── docs/                   # 本目录：开发文档
├── references/             # AI 简洁版文档
└── keys/                   # RSA 密钥对输出目录（由 /generate-key 生成）
```

## 启动

```bash
go build -o build/HRPAuth-Backend-Go
./build/HRPAuth-Backend-Go
```

首次启动会自动：

1. 在项目根目录生成 `config.yaml`（若不存在）
2. 读取配置、连接 MySQL / Redis
3. 启动 HTTP 服务（默认 `:8080`）
4. 启动 Token 后台清理任务（每小时一次）
5. 启动代注册用户清理任务（每天一次，见下文）

## 后台任务

启动时除了 HTTP 服务外，还会有两个 goroutine 周期跑清理：

| 任务 | 控制器 | 周期 | 清理目标 | 触发条件 |
|------|--------|------|----------|----------|
| Token 清理 | [`controllers/token_cleanup_controller.go`](../controllers/token_cleanup_controller.go) | 1h | 过期 / 失效的 Yggdrasil Token | 启动 + 每小时 |
| 代注册用户清理 | [`controllers/bot_user_cleanup_controller.go`](../controllers/bot_user_cleanup_controller.go) | 24h | 长期未活跃的 `cbh=0` 代注册用户 | 启动 + 每 24h + 每次 M.T. `/register` 成功后异步触发一次 |

### 代注册用户清理

对应 `references/HA-ROADMAP.md` §4 的"清理 routine"。**不暴露任何管理端点**。

**清理规则**：

```sql
SELECT id FROM users
WHERE cbh = 0
  AND register_at < NOW() - INTERVAL 30 DAY
  AND last_sign_at < NOW() - INTERVAL 30 DAY;
```

**触发**：

- HA 启动时立即跑一次
- 之后每 24h 跑一次
- 每次 M.T. `POST /register` 成功（4 个 success path 任一）后**异步**跑一次（不阻塞响应；包级 `sync.Mutex` + `TryLock` 防止与周期任务并发）

**级联删除顺序**（按依赖关系避免 FK 错误）：

1. `sessions`（按 `profile_id`）— 必须在 profile 删前完成
2. `profile_properties`（按 `profile_id`）
3. `profiles`（按 `user_id`）
4. `tokens`（按 `user_id`）
5. `users`

**异常处理**：

- 单个用户删除失败（外键约束等）→ `log.Printf` ERROR 并跳过该用户，不中断后续清理
- 周期任务与 M.T. 触发并发 → `TryLock` 失败的请求直接返回 0，不重试（避免堆积）
- 全表扫描一次性 `Find` 加载所有候选行；候选量过大时需要后续引入分批

**日志格式**（对应 §4.4）：

```
[cleanup] - uid=42 username=longgone_1 (created 2026-05-01, last_seen 2026-05-03)
[cleanup] - uid=58 username=longgone_2 (created 2026-05-02, last_seen 2026-05-04)
[cleanup] scanned 142 users, deleted 3
[BotUserCleanup] removed 3 inactive bot users
```

## 后续阅读

- [tokens.md](./tokens.md) — Token 体系是整个项目最复杂的部分，强烈建议先读。
- [configuration.md](./configuration.md) — 配置项说明。
- 各端点文档 — 按需查阅。
