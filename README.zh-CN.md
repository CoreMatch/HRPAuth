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

首次启动时服务会生成带默认值的 `config.yaml`。投入生产前至少调整
`database`、`redis`、`smtp`、`server.port`、`callback.url`、`frontend.url`，
然后重启。

- 完整配置参考（schema 版本、全部字段、默认值）：
  [HRPAuth-Wiki — HRPAuth 配置](https://wiki.mcnb.dev/deploy/hrpauth)
- 数据库迁移（命令、baseline 布局、规则）：
  [HRPAuth-Wiki — HRPAuth 数据形态与迁移](https://wiki.mcnb.dev/dev/hrpauth)

## 部署

### 单二进制

```bash
./build.sh   # = go build -o ./build/HRPAuth
```

[`build/`](build) 目录下保留了六个平台的预编译产物：darwin / linux / windows × amd64 / arm64。

### 容器镜像

镜像已发布到 Docker Hub：[`limeight/ha-hrpauth`](https://hub.docker.com/r/limeight/ha-hrpauth)——`linux/amd64` + `linux/arm64` 多架构镜像（manifest list）；tag 为 `<版本>-<短SHA>`，并列 `latest`，生效引用以 [`deploy-manifest.yaml`](deploy-manifest.yaml) 登记的 manifest list digest 为准。镜像以非 root 用户运行同一二进制，暴露端口 `2778`。[入口脚本](docker-entrypoint.sh)可在启动前用环境变量渲染配置模板（`CONFIG_TEMPLATE` / `RENDERED_CONFIG`）。

多阶段 [Dockerfile](Dockerfile) 保留用于本地构建（`docker build -t hrpauth .`）；部署引用以已发布镜像为准。

### 平台编排（Compose）

HRPAuth 是 HA 平台的核心，但在实际部署中它只是其中一个服务。整个平台由 [HA-Services-Deployer/deploy/docker-compose.yml](https://github.com/CoreMatch/HA-Services-Deployer/blob/main/deploy/docker-compose.yml) 里的 Compose 模板一条命令拉起：

```bash
git clone https://github.com/CoreMatch/HA-Services-Deployer.git
cd HA-Services-Deployer/deploy
cp .env.example .env   # 数据库 / Redis 地址、OAuth 客户端、密钥、上游 URL
docker compose up -d --build
```

| 服务 | 镜像 | 端口 | 职责 |
|---|---|---|---|
| **HRPAuth** | `ha/hrpauth` | 2778 | 账号与 OAuth2 授权核心 |
| HRPAuth-Yggdrasil-API | `ha/hrpauth-yggdrasil-api` | 2770 | Yggdrasil 协议提供方 |
| HASkinLib | `ha/haskinlib` | 2701 | 材质上传、存储与预览 |
| HASkinProxy | `ha/haskinproxy` | 2702 | 面向 CustomSkinLoader 客户端的皮肤服务代理 |
| WinnerProxy | `ha/winnerproxy` | 2777 | Yggdrasil 请求路由代理 |
| HA-WebUI-SDKHandler | `ha/ha-webui-sdkhandler` | 2704 / 2705 | WebUI SDK 聚合与编译 |
| MatchCoreArena-Server | `ha/mca-server` | 9178 | 对局核心服务 |
| HA-Services-Deployer | `ha/ha-services-deployer` | 18080 | 基于清单的部署后端 |
| HA-Services-MaintenanceView | `ha/ha-services-maintenance-view` | 18081 | 运维操作界面 |

编排的接线方式：

- MySQL 与 Redis **不在**编排内启动，各服务通过 `.env` 中的 `DB_*` / `REDIS_*` 变量连接外部实例。
- 每个服务的 `config.yaml` 在容器启动时由 `deploy/config/` 下的模板渲染而成，数据库地址、上游 URL 与密钥全部由 `.env` 决定。
- 服务间调用使用容器名（例如 `http://hrpauth:2778`），镜像内不写死宿主机地址。
- 上表的 `ha/*` 镜像名是该模板本地构建产出的标签；已发布的 registry 镜像在 Docker Hub `limeight/ha-*` 命名空间（多架构，生效引用钉 manifest list digest）。
- 一条命令触发之后，容器的日常生命周期（重启策略、健康检查）由 docker compose 自己管理。HA-Services-Deployer 的设计意图是**在不使用容器的情况下编排微服务**——裸金属编排是主线；容器路径是一键触发，另有已实现的容器级管理能力（按 digest 拉取、容器健康探测、按 digest 钉死回滚）作为可选能力。

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

- [HRPAuth-Wiki](https://wiki.mcnb.dev/zh/)：开发者 wiki。
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
