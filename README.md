# HRPAuth

[中文说明](README.zh-CN.md)

HRPAuth is a general-purpose authentication core service and the OAuth2 authorization center of the HA ecosystem: accounts, login, and multi-factor security for your users, plus OAuth2 issuance for the services around it.

> **Scope note:** The Yggdrasil protocol and skin/texture features are **not** part of this repository. They were split out on 2026-10-06 and now live in dedicated microservices: [HRPAuth-Yggdrasil-API](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API), [HASkinLib](https://github.com/CoreMatch/HASkinLib) and [HASkinProxy](https://github.com/CoreMatch/HASkinProxy).

## Quick Start

### 1. Prerequisites

- Go 1.26 or higher
- MySQL
- Redis
- An SMTP server (for verification emails)

### 2. Clone and run

```bash
git clone https://github.com/CoreMatch/HRPAuth.git
cd HRPAuth

# First run: config.yaml is generated automatically (configuration only — no keys or certificates are generated)
go run main.go
```

### 3. Configure and restart

Edit the required fields in the generated `config.yaml` (see [Configuration](#configuration)), then build and run:

```bash
go build -o HRPAuth
./HRPAuth
```

## Configuration

Fields you **must** adjust before deploying:

| Field | Purpose |
|---|---|
| `database` | MySQL connection (host, database name, user, password) |
| `redis` | Redis connection |
| `smtp` | Mail server for verification emails |
| `server.port` | HTTP listen port |
| `callback.url` | Public URL of this service |
| `frontend.url` | URL of the web frontend (HRPAuth-WebUI) |

All other fields have working defaults, and the configuration schema is migrated automatically on startup (current version: **11**).

For the complete field reference, see the [configuration guide in HA-Contract](https://github.com/CoreMatch/HA-Contract/blob/main/docs/dev/HRPAuth/configuration.md). Note: that document is still labeled schema version "10"; the actual current version in code is **11**.

## Deployment

HRPAuth ships as a single static binary — there is no Docker image.

```bash
./build.sh   # = go build -o ./build/HRPAuth
```

Pre-built binaries for six platforms are kept under [`build/`](build): darwin / linux / windows × amd64 / arm64.

## Features

- Registration, login and logout with email verification
- Captcha protection
- TOTP two-factor authentication
- WebAuthn / passkey login and second factor
- Email-based second factor
- Recovery keys
- Username and email changes
- Account deletion

## Service Integration

Other services in the HA ecosystem can register with this service and be reached through it; for how that works, see the [microservices guide in HA-Contract](https://github.com/CoreMatch/HA-Contract/blob/main/docs/dev/HRPAuth/microservices.md).

## API Documentation

The business API contract (OpenAPI definition and error codes) is maintained in HA-Contract — see [API Standards & Specifications](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/README.md).

## Related Projects

**Core components**

- [HRPAuth-Yggdrasil-API](https://github.com/CoreMatch/HRPAuth-Yggdrasil-API): Yggdrasil protocol microservice split out of this project, serving Minecraft-oriented authentication, sessions and profiles.
- [HRPAuth-WebUI](https://github.com/CoreMatch/HRPAuth-WebUI): Official web frontend for login, account management and second-factor setup.
- [HA-Contract](https://github.com/CoreMatch/HA-Contract): API contracts, OpenAPI definitions, error codes and the documentation center of the ecosystem.

**Companion services**

- [WinnerProxy](https://github.com/CoreMatch/WinnerProxy): Yggdrasil request routing proxy.
- [HASkinLib](https://github.com/CoreMatch/HASkinLib): Texture upload, storage, retrieval and preview service (pairs with HRPAuth-Yggdrasil-API).
- [HASkinProxy](https://github.com/CoreMatch/HASkinProxy): Proxy that makes the ecosystem's skin service usable by CustomSkinLoader clients (pairs with HRPAuth-Yggdrasil-API).
- [HA-WebUI-SDKHandler](https://github.com/CoreMatch/HA-WebUI-SDKHandler): Aggregates the services' WebUI extensions into the main web interface.
- [HRPAuth-Spigot](https://github.com/CoreMatch/HRPAuth-Spigot): Velocity plugin that binds in-game identities to platform accounts.

**Tools & operations**

- [BS2HA](https://github.com/CoreMatch/BS2HA): Offline migration tool from BlessingSkin to HRPAuth.
- [HA-Services-Deployer](https://github.com/CoreMatch/HA-Services-Deployer): Manifest-driven deployment backend.
- [HA-Services-MaintenanceView](https://github.com/CoreMatch/HA-Services-MaintenanceView): Operations interface for project setup and deployment management.

**Documentation & portal**

- [HRPAuth-Wiki](https://github.com/CoreMatch/HRPAuth-Wiki): Developer wiki (content frozen since 2026-10-01, predating the service split).
- [MatchCore-Portal](https://github.com/CoreMatch/MatchCore-Portal): Bilingual portal site of the wider ecosystem.

**Client**

- [SamuelClient](https://github.com/CoreMatch/SamuelClient): Minecraft launcher with support for self-hosted authentication.

**Historical**

- [HRPAuth-Backend-PHP](https://github.com/CoreMatch/HRPAuth-Backend-PHP): Early PHP backend (archived on GitHub, 2026-08-14).
- [HRPAuth-Legacy](https://github.com/CoreMatch/HRPAuth-Legacy): The original "HRPAuth" umbrella repository (historical archive, inactive since 2026-04-21).
- [PHP-zggdrasil-mock](https://github.com/CoreMatch/PHP-zggdrasil-mock): Yggdrasil mock server from the PHP era (inactive since 2026-06-19).
- [PHP-yggdrasil-api-debugger](https://github.com/CoreMatch/PHP-yggdrasil-api-debugger): Yggdrasil protocol debugging workbench from the PHP era (inactive since 2026-06-19).

## Documentation

All API documentation and development specifications are centralized in HA-Contract:

- [API Standards & Specifications](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/README.md)
- [OpenAPI Definition](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/openapi/hrpauth-business.yaml)
- [Error Codes](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/error-codes.md)

## License

This project is licensed under the [AGPL 3.0 License](LICENSE).
