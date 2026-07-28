# HRPAuth-Backend-Go 开发文档

本目录为面向开发人员的详细 API 文档，每个功能模块单独成文，便于按需查阅。

> 配套的 **AI 简洁版**（仅说明调用方式）见 [`../references/API_DOC_FE.md`](../references/API_DOC_FE.md)。两份文档需保持同步修改。

## 目录结构

### 总体说明

| 文档 | 内容 |
|------|------|
| [overview.md](./overview.md) | 项目概述、技术栈、整体架构 |
| [tokens.md](./tokens.md) | Token 体系总览、状态机、生命周期、后台清理 |
| [data-models.md](./data-models.md) | 数据模型（User / Profile / Token / Session 等） |
| [feature-flags.md](./feature-flags.md) | 功能开关说明 |
| [configuration.md](./configuration.md) | 配置文件结构与全部字段 |

### 端点（按业务模块）

| 模块 | 文档 | 鉴权方式 |
|------|------|----------|
| 服务状态 | [endpoints/status.md](./endpoints/status.md) | 公开 |
| 认证（注册 / 登录 / 登出） | [endpoints/auth.md](./endpoints/auth.md) | 部分 Remember Token |
| 图形验证码 | [endpoints/captcha.md](./endpoints/captcha.md) | 公开 |
| 用户信息 | [endpoints/user-info.md](./endpoints/user-info.md) | Remember Token |
| 邮箱验证 | [endpoints/email-verification.md](./endpoints/email-verification.md) | 按 action 而定 |
| TOTP 两步验证 | [endpoints/totp.md](./endpoints/totp.md) | Remember Token / Passcode |
| 用户资料 | [endpoints/user-profile.md](./endpoints/user-profile.md) | Remember Token |
| 密钥生成 | [endpoints/keygen.md](./endpoints/keygen.md) | 公开（管理类） |
| 纹理管理（HRPAuth） | [endpoints/texture.md](./endpoints/texture.md) | Remember Token |
| Yggdrasil API | [endpoints/yggdrasil.md](./endpoints/yggdrasil.md) | Yggdrasil Access Token / Client Token |

## 阅读建议

- **新接入项目**：先看 [overview.md](./overview.md) 与 [tokens.md](./tokens.md) 理解整体鉴权体系，再按需查阅各端点文档。
- **对接前端**：推荐直接读 `../references/API_DOC_FE.md`（精简版）。
- **调试 Token 行为**：阅读 [tokens.md](./tokens.md) 中"状态机与生命周期"小节。

## 文档维护约定

- 任何对 API 端点（路径、请求 / 响应字段、错误码）的修改，**必须**同步更新 `docs/` 与 `references/` 两个版本。
- 新增端点：先在 `endpoints/` 新建独立文件，再在 `README.md` 索引中加入链接，最后在 `references/API_DOC_FE.md` 同步精简版。
- 删除端点：从 `endpoints/` 删除文件，并清理本索引与 `references/API_DOC_FE.md`。
