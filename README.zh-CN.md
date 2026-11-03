# HRPAuth

[English version](README.md)

HRPAuth 是一个通用认证核心服务，也是 HA 生态的 OAuth2 授权中心：面向用户提供账号、登录与多因素安全能力，并为生态内的其他服务提供 OAuth2 授权。

> **范围说明**：Yggdrasil 协议与皮肤/纹理能力**不在**本仓库中。它们已于 2026-10-06 拆分，现由独立微服务承载：[HRPAuth-Yggdrasil-API](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API)、[HASkinLib](https://github.com/CoreMatch/HASkinLib) 与 [HASkinProxy](https://github.com/CoreMatch/HASkinProxy)。

## 快速开始

### 1. 前置依赖

- Go 1.26 或更高版本
- MySQL
- Redis
- 一台 SMTP 邮件服务器（用于验证邮件）

### 2. 克隆并运行

```bash
git clone https://github.com/CoreMatch/HRPAuth.git
cd HRPAuth

# 首次运行：自动生成 config.yaml（只生成配置，不会生成任何密钥或证书）
go run main.go
```

### 3. 修改配置后重启

编辑生成的 `config.yaml` 中的必改字段（见[配置](#配置)），然后构建运行：

```bash
go build -o HRPAuth
./HRPAuth
```

## 配置

部署前**必须**调整的字段：

| 字段 | 用途 |
|---|---|
| `database` | MySQL 连接（主机、库名、用户、口令） |
| `redis` | Redis 连接 |
| `smtp` | 发送验证邮件的邮件服务器 |
| `server.port` | HTTP 监听端口 |
| `callback.url` | 本服务对外访问地址 |
| `frontend.url` | Web 前端（HRPAuth-WebUI）地址 |

其余字段均有可用默认值，配置结构在启动时自动迁移（当前版本：**11**）。

完整字段参考见 HA-Contract 的[配置文档](https://github.com/CoreMatch/HA-Contract/blob/main/docs/dev/HRPAuth/configuration.md)。

## 部署

### 单二进制

```bash
./build.sh   # = go build -o ./build/HRPAuth
```

[`build/`](build) 目录下保留了六个平台的预编译产物：darwin / linux / windows × amd64 / arm64。

### 容器镜像

仓库提供了多阶段构建的 [Dockerfile](Dockerfile)：

```bash
docker build -t hrpauth .
```

镜像以非 root 用户运行同一二进制，暴露端口 `2778`。[入口脚本](docker-entrypoint.sh)可在启动前用环境变量渲染配置模板（`CONFIG_TEMPLATE` / `RENDERED_CONFIG`）。

## 功能

- 注册、登录、登出，含邮箱验证
- 验证码保护
- TOTP 双因素认证
- WebAuthn / passkey 登录与二要素
- 邮箱二要素
- 恢复密钥
- 修改用户名与邮箱
- 账号删除

## 服务接入

HA 生态内的其他服务可以登记到本服务并经由其接入；机制细节见 HA-Contract 的[微服务文档](https://github.com/CoreMatch/HA-Contract/blob/main/docs/dev/HRPAuth/microservices.md)。

## API 文档

业务 API 契约（OpenAPI 定义与错误码）统一维护在 HA-Contract，见 [API 标准与规范](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/README.md)。

## 相关项目

**核心组件**

- [HRPAuth-Yggdrasil-API](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API)：从本项目拆分出的 Yggdrasil 协议微服务，承载 Minecraft 向的认证、会话与档案。
- [HRPAuth-WebUI](https://github.com/CoreMatch/HRPAuth-WebUI)：官方 Web 前端，提供登录、账户管理与二要素设置。
- [HA-Contract](https://github.com/CoreMatch/HA-Contract)：生态的 API 契约、OpenAPI 定义、错误码与文档中心。

**配套服务**

- [WinnerProxy](https://github.com/CoreMatch/WinnerProxy)：Yggdrasil 请求分流代理。
- [HASkinLib](https://github.com/CoreMatch/HASkinLib)：纹理上传、存储、检索与预览服务（与 HRPAuth-Yggdrasil-API 配套）。
- [HASkinProxy](https://github.com/CoreMatch/HASkinProxy)：让 CSL（CustomSkinLoader）客户端使用生态皮肤服务的代理（与 HRPAuth-Yggdrasil-API 配套）。
- [HA-WebUI-SDKHandler](https://github.com/CoreMatch/HA-WebUI-SDKHandler)：把各服务的 WebUI 扩展聚合进主界面的构建工具。
- [HRPAuth-Spigot](https://github.com/CoreMatch/HRPAuth-Spigot)：Velocity 服务端插件，把游戏内身份绑定到平台账户。

**工具与运维**

- [BS2HA](https://github.com/CoreMatch/BS2HA)：BlessingSkin → HRPAuth 的离线迁移工具。
- [HA-Services-Deployer](https://github.com/CoreMatch/HA-Services-Deployer)：manifest 驱动的服务部署后端。
- [HA-Services-MaintenanceView](https://github.com/CoreMatch/HA-Services-MaintenanceView)：项目初始化与部署管理的运维界面。

**文档与门户**

- [HRPAuth-Wiki](https://github.com/CoreMatch/HRPAuth-Wiki)：开发者 wiki。
- [MatchCore-Portal](https://github.com/CoreMatch/MatchCore-Portal)：更大生态的双语门户站点。

**客户端**

- [SamuelClient](https://github.com/CoreMatch/SamuelClient)：Minecraft 启动器，支持接入自建认证。

**历史档案**

- [HRPAuth-Backend-PHP](https://github.com/CoreMatch/HRPAuth-Backend-PHP)：早期 PHP 后端（已在 GitHub 归档，2026-08-14）。
- [HRPAuth-Legacy](https://github.com/CoreMatch/HRPAuth-Legacy)：原名 "HRPAuth" 的伞仓库（历史档案，2026-04-21 后停更）。
- [PHP-zggdrasil-mock](https://github.com/CoreMatch/PHP-zggdrasil-mock)：PHP 期 Yggdrasil 联调 mock（2026-06-19 后未更新）。
- [PHP-yggdrasil-api-debugger](https://github.com/CoreMatch/PHP-yggdrasil-api-debugger)：PHP 期 Yggdrasil 协议调试台（2026-06-19 后未更新）。

## 文档

所有 API 文档与开发规范统一收口在 HA-Contract：

- [API 标准与规范](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/README.md)
- [OpenAPI 定义](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/openapi/hrpauth-business.yaml)
- [错误码表](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/error-codes.md)

## 许可

本项目基于 [AGPL 3.0 许可](LICENSE)。
