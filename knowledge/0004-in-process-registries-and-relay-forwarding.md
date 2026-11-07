# ADR-0004: In-Process Registries and Relay/Orchestration Forwarding

- **Status**: accepted

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
  `relayAllowsAnonymous` helper was dead code with no production caller (it
  was referenced only by its own unit test); anonymity now comes from a
  service declaring `security_level=0`, which `RequireAuthLevel` passes
  through. The helper's historical provenance was unrecoverable — the author
  had no memory of introducing or re-introducing it (confirmed 2026-11-04) —
  so no design intent was inferred from it. It was deliberately kept at first,
  then deleted together with its pinning unit test in the author-approved
  dead-code cleanup batch of 2026-11-08.
- Cost: registries are Redis-backed in-process maps — they are restored from
  Redis at boot and lost if Redis is lost; they are not queryable via SQL.
- The SDK endpoint does not serve individual files from an archive; any
  "serve one file from the archive" behavior belongs to the consumer.

## Evidence

- Registry instantiation and Redis restore: `main.go::NewPresenceRegistry`,
  `main.go::NewRouteRegistry`, `main.go::NewRelayRegistry`,
  `main.go::NewHookRegistry` (instantiate), `main.go::main` (restore from
  Redis).
- PresenceRegistry: `registry/presence.go::PresenceRegistry` (struct),
  `registry/presence.go::Register`, `registry/presence.go::RegisterAuthKey`,
  `registry/presence.go::persist` (persist to Redis),
  `registry/presence.go::Get` (Get with lazy expiry); heartbeat
  `registry/heartbeat.go::CheckAll`, `registry/heartbeat.go::checkService`;
  entrypoints `controllers/presence_controller.go::Bonjour` (Bonjour),
  `controllers/presence_controller.go::AuthKey` (AuthKey); routes
  `main.go::Bonjour`, `main.go::AuthKey`.
- RouteRegistry: `controllers/route_controller.go::RouteRegistry` (struct),
  `controllers/route_controller.go::Upsert`,
  `controllers/route_controller.go::MatchPre` /
  `controllers/route_controller.go::MatchPost`,
  `controllers/route_controller.go::MatchPath` (wildcard),
  `controllers/route_controller.go::Register` (requires presence).
- RelayRegistry: `controllers/relay_controller.go::RelayRegistry` (struct),
  `controllers/relay_controller.go::Upsert`,
  `controllers/relay_controller.go::Match` (longest-prefix),
  `controllers/relay_controller.go::relayTTL` (30-day TTL), routes
  `main.go::relayCtrl`.
- HookRegistry: `controllers/hook_controller.go::HookRegistry` (struct),
  `controllers/hook_controller.go::HookPhaseAuthInput` (phases),
  `controllers/hook_controller.go::HookCapabilityObserve` (capabilities),
  `controllers/hook_controller.go::Match` (priority Match), route
  `main.go::hookCtrl`.
- Middleware mounting: `main.go::RelayMiddleware`,
  `main.go::OrchestrationMiddleware`. RelayMiddleware
  `controllers/relay_controller.go::RelayMiddleware` (skip `/services/*`,
  auth by security_level via `controllers/auth_level.go::RequireAuthLevel`,
  502 no-fallback). Orchestration
  `controllers/orchestration_middleware.go::OrchestrationMiddleware`
  (fail-open, async post on 2xx), header forwarding
  `controllers/orchestration_middleware.go::forwardTo`.
- Anonymous relay: the former `relayAllowsAnonymous` helper (then in
  controllers/relay_controller.go) and its only reference, the unit test
  `TestRelayAllowsAnonymous` (then in controllers/relay_controller_test.go),
  were deleted
  in the 2026-11-08 dead-code cleanup batch; anonymity is carried by the
  security-level passthrough `controllers/auth_level.go::SecurityLevelNone`,
  `controllers/auth_level.go::RequireAuthLevel`.
- SDK package store: `main.go::sdkPackageCtrl` (routes; comment "satellite
  上传，SDKHandler 拉取"); archive extraction
  `controllers/sdk_package_controller.go::validateAndExtractManifest`,
  `controllers/sdk_package_controller.go::extractFromTarGz`,
  `controllers/sdk_package_controller.go::extractFromZip`; path safety
  `controllers/sdk_package_controller.go::sanitizeArchivePath`;
  whole-archive download only
  `controllers/sdk_package_controller.go::Download`, atomic put
  `controllers/sdk_package_store.go::Put`.
- Commits: `1545da2` (extract presence registry, auth overhaul), `024bc3a`
  (anonymous relay + root endpoint), `1a3103a` (relay/forward logging),
  `7c11dd2` (sdk packages), `6d9324a` (directory entries in archives),
  `cb2ca9e` (multiple menu/dashboard entries), `353f084` (webhook hook system).
- Provenance of `relayAllowsAnonymous`: unrecoverable. The author confirmed on
  2026-11-04 having no memory of introducing or re-introducing it, so no design
  intent was inferred from it; it was recorded as dead code with no production
  caller, deliberately kept at first, then deleted in the 2026-11-08 cleanup
  batch.
- Not verified: the current SDK addressing scheme.
