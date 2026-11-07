# ADR-0006: Two Deployment Forms — Single Binary and Container Image

- **Status**: accepted

## Context

HRPAuth must run both as a self-contained binary on a host and as a container
in the platform's compose orchestration. Database availability at boot cannot be
guaranteed (MySQL may start after the core), so startup must tolerate a missing
database without crash-looping.

## Decision

HRPAuth ships **two deployment forms**:

1. **Single binary** — built via `build.sh` (`go build -o ./build/HRPAuth`);
   prebuilt binaries for six platforms (darwin/linux/windows × amd64/arm64)
   are kept under `build/` — locally only: `build/*` is gitignored
   (`.gitignore:L3`), so the binaries are not committed and a fresh clone
   must build them itself via `build.sh`.
2. **Container image** — a multi-stage `Dockerfile`: builder stage
   `golang:1.26.4-alpine` producing a static binary, runtime stage `alpine:3.22`
   running as a non-root user (uid 10001), `EXPOSE 2778`, with an entrypoint
   that can render `config.yaml` from environment variables via `envsubst`
   (`CONFIG_TEMPLATE`/`RENDERED_CONFIG`) to support compose orchestration.

Database connection is **asynchronous with retries**: `database.Init()` runs in
a goroutine, retrying every `RetryIntervalSec` (default 3s) up to `MaxRetries`
(default 5), then waiting `NextRoundDelayMin` minutes (default 5) before the
next round. Until the DB is ready, an API middleware returns `503
database_not_ready` instead of failing.

## Consequences

- The container form is designed for compose orchestration where config is
  rendered from `.env`; the binary form suits standalone hosts.
- The core does not crash when MySQL is down at boot — it keeps retrying and
  serves 503 for DB-backed routes, then runs migrations once connected.
- Cost: behavior differs between forms only in how `config.yaml` is produced;
  operators must know which form renders config and which reads it directly.

## Evidence

- Single binary: `README.md:L50-L56 ~ Single binary` ("Single binary" + `build.sh`); prebuilt
  binaries present under `build/` (working tree only — `build/*` ignored per
  `.gitignore:L3`); `README.zh-CN.md:L50-L50 ~ 单二进制`.
- Container image: `Dockerfile:L2-L10` (builder `golang:${GO_VERSION}-alpine`,
  static build), `Dockerfile:L12-L14` + `Dockerfile:L22` (runtime
  `alpine:3.22`, non-root uid 10001), `Dockerfile:L21` (`EXPOSE 2778`),
  `Dockerfile:L23-L24` (entrypoint/cmd); introduced by `7c391d8`.
- Config rendering for compose: `docker-entrypoint.sh::envsubst` (`envsubst`
  when `CONFIG_TEMPLATE`/`RENDERED_CONFIG` set); compose orchestration
  described in `README.md:L68-L95 ~ Platform orchestration (Compose)`.
- Startup flow: `main.go::main`, `main.go::main` (config load + async DB
  init), `main.go::RegisterOnConnect` (OnConnect callback), `main.go::main`
  (redis), `main.go::main` (`r.Run`).
- DB retry logic: `database/database.go::Init` (`Init` goroutine),
  `database/database.go::connect` (`connect` retry loop,
  `RetryIntervalSec`/`MaxRetries`/`NextRoundDelayMin`); defaults
  `config/config.go::parseDatabaseConfig`; commits `8295bb4`, `b629c78`.
- 503 middleware: `main.go::DatabaseMiddleware` (definition and mount on the
  API group).
- Not verified: none material; the six-platform binary set was observed in the
  working tree.
