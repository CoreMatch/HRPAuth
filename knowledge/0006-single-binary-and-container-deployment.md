# ADR-0006: Two Deployment Forms — Single Binary and Container Image

- **Status**: accepted
- **Date**: 2026-11-04

## Context

HRPAuth must run both as a self-contained binary on a host and as a container
in the platform's compose orchestration. Database availability at boot cannot be
guaranteed (MySQL may start after the core), so startup must tolerate a missing
database without crash-looping.

## Decision

HRPAuth ships **two deployment forms**:

1. **Single binary** — built via `build.sh` (`go build -o ./build/HRPAuth`);
   prebuilt binaries for six platforms (darwin/linux/windows × amd64/arm64)
   are kept under `build/`.
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

- Single binary: `README.md:50-56` ("Single binary" + `build.sh`); prebuilt
  binaries present under `build/`; `README.zh-CN.md:50`.
- Container image: `Dockerfile:2-10` (builder `golang:${GO_VERSION}-alpine`,
  static build), `:12-14,22` (runtime `alpine:3.22`, non-root uid 10001),
  `:21` (`EXPOSE 2778`), `:23-24` (entrypoint/cmd); introduced by `7c391d8`.
- Config rendering for compose: `docker-entrypoint.sh:6-8` (`envsubst` when
  `CONFIG_TEMPLATE`/`RENDERED_CONFIG` set); compose orchestration described in
  `README.md:68-95`.
- Startup flow: `main.go:63` (`main`), `:65-71` (config load + async DB init),
  `:73-87` (OnConnect callback), `:89` (redis), `:259-261` (`r.Run`).
- DB retry logic: `database/database.go:16-18` (`Init` goroutine), `:20-57`
  (`connect` retry loop, `RetryIntervalSec`/`MaxRetries`/`NextRoundDelayMin`);
  defaults `config/config.go:237-270`; commits `8295bb4`, `b629c78`.
- 503 middleware: `main.go:48-61` (`DatabaseMiddleware`), mounted `main.go:175`.
- Not verified: none material; the six-platform binary set was observed in the
  working tree.
