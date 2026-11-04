# ADR-0004: In-Process Registries and Relay/Orchestration Forwarding

- **Status**: accepted
- **Date**: 2026-11-04

## Context

HRPAuth sits at the center of a set of sibling microservices (Yggdrasil-API,
skin services, SDK handler) that must discover each other, register forwarding
rules, and hook into the auth flow. The core also distributes WebUI "SDK
packages" (archives) that a separate handler pulls. A choice was needed between
persisting this coordination in the database or keeping it lightweight.

## Decision

Coordination state lives in **four in-process registries**, persisted to Redis
(not the DB), plus two forwarding middlewares and an SDK-package store:

- **PresenceRegistry** (`registry/presence.go`): which services are online,
  their `security_level`, root metadata, and internal auth keys. Fed by
  `POST /services/presence` ("Bonjour") and heartbeats.
- **RouteRegistry** (`controllers/route_controller.go`): pre/post orchestration
  routing rules; registration requires a prior presence entry to prevent
  impersonation.
- **RelayRegistry** (`controllers/relay_controller.go`): destination-keyed
  forwarding rules, longest-prefix match, 30-day TTL.
- **HookRegistry** (`controllers/hook_controller.go`): auth hooks at phases
  `auth.input` / `auth.risk` / `auth.post_verify` with capabilities
  observe/patch/veto.

Two middlewares are mounted globally: **RelayMiddleware** (skips `/services/*`,
matches a relay rule, authorizes by the target's `security_level`, forwards,
and returns 502 on failure without fallback) and **OrchestrationMiddleware**
(pre-rules fan out and short-circuit on first success, fail-open if all fail;
post-rules dispatch asynchronously only on 2xx).

SDK packages are stored on disk as `<name>/archive` + `.meta.json`; the core
serves **whole-archive byte downloads only** — extraction is done by the
pulling side (SDKHandler), not here.

## Consequences

- Anonymous relay is no longer a special case: the legacy
  `relayAllowsAnonymous` helper is dead code; anonymity now comes from a service
  declaring `security_level=0`, which `RequireAuthLevel` passes through.
- Cost: registries are Redis-backed in-process maps — they are restored from
  Redis at boot and lost if Redis is lost; they are not queryable via SQL.
- The SDK endpoint does not serve individual files from an archive; any
  "serve one file from the archive" behavior belongs to the consumer.

## Evidence

- Registry instantiation and Redis restore: `main.go:96-100` (instantiate),
  `:104-116` (restore from Redis).
- PresenceRegistry: `registry/presence.go:44-53` (struct), `:55-81` (Register),
  `:83-98` (RegisterAuthKey), `:125-150` (persist to Redis), `:189-203` (Get
  with lazy expiry); heartbeat `registry/heartbeat.go:35-77`; entrypoints
  `controllers/presence_controller.go:34` (Bonjour), `:57-80` (AuthKey); routes
  `main.go:230-231`.
- RouteRegistry: `controllers/route_controller.go:38-45` (struct), `:49-55`
  (Upsert), `:106-113` (MatchPre/Post), `:138-154` (MatchPath wildcard),
  `:172-194` (Register requires presence `:180-183`).
- RelayRegistry: `controllers/relay_controller.go:37-44` (struct), `:48-67`
  (Upsert), `:172-199` (longest-prefix Match), `:20` (30-day TTL), routes
  `main.go:234-236`.
- HookRegistry: `controllers/hook_controller.go:69-76` (struct), `:21-23`
  (phases), `:25-27` (capabilities), `:134-155` (priority Match), route
  `main.go:233`.
- Middleware mounting: `main.go:128-129`. RelayMiddleware
  `controllers/relay_controller.go:283-316` (skip `/services/*` `:286-289`,
  auth by security_level `:297`, 502 no-fallback `:313`). Orchestration
  `controllers/orchestration_middleware.go:48-98` (fail-open `:59-73`, async
  post on 2xx `:82-96`), header forwarding `:134-139`.
- Anonymous relay: `controllers/relay_controller.go:272-277`
  (`relayAllowsAnonymous`, now unreferenced by the middleware); security-level
  passthrough `controllers/auth_level.go:13,52-55`.
- SDK package store: `main.go:238-243` (routes; comment "satellite 上传，
  SDKHandler 拉取"); archive extraction `controllers/sdk_package_controller.go:255-265,289-339,342-377`;
  path safety `:269-286`; whole-archive download only
  `services/sdk_package_store.go:143-161`, atomic put `:187-241`.
- Commits: `1545da2` (extract presence registry, auth overhaul), `024bc3a`
  (anonymous relay + root endpoint), `1a3103a` (relay/forward logging),
  `7c11dd2` (sdk packages), `6d9324a` (directory entries in archives),
  `cb2ca9e` (multiple menu/dashboard entries), `353f084` (webhook hook system).
- Not verified: the re-introduction intent of `relayAllowsAnonymous` dead code;
  current SDK addressing after `sdk_url` was removed (see `ca02ad8` then
  `cb2ca9e`).
