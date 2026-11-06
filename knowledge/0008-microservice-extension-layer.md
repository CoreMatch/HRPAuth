> 来源：HA-Contract@bab2a05（2026-11-07），原路径 docs/dev/HRPAuth/microservices.md（契约仓知识库回迁）。

# Microservice Extension Layer

This document describes the microservice extension mechanism of HRPAuth. It covers service registration, compile-time SDK aggregation, route rules, relay rules, webhook hook rules, request orchestration, and the three-level auth model.

## Design Positioning

HRPAuth is the **core standard service**: its request parameters, response structures, status codes, and semantics strictly follow external standards and **cannot be extended in the core flow**.

Therefore, all microservice extensions happen **outside the main service flow**:

- **Pre-flow routing**: decides, for each incoming request, whether it should be forwarded to a microservice before (or instead of) reaching the main service.
- **Post-flow routing**: after the main service produces a response, dispatches the result to designated microservices for side processing.

The extension layer is an **edge orchestrator / router**, not a plugin system. It never changes HRPAuth's internal business decisions. The standard response is passed through unchanged by default; post-processing is executed out-of-band.

```
Frontend
  -> Extension Layer (relay / orchestration middlewares)
       -> HRPAuth core (standard, unmodified)
       -> Microservices (pre routing / post routing)
       -> Specialized Business Services (e.g., Yggdrasil-API)
```

## Specialized Business Services

Certain domain-specific logic is moved into independent services to keep the HRPAuth core clean and generic:

### Yggdrasil API Provider (`HRPAuth-Yggdrasil-API`)

- **Role**: Implements Minecraft Yggdrasil protocols and game-specific account management.
- **Business Scope**: Owns current-profile texture management (`/texture/get`, `/texture/upload/:uuid`, `DELETE /texture/delete/:uuid/:type`) and the generated preview surface (`GET /previews/:fileName`) for that management UI.
- **Boundary**: Does **not** own texture discovery, texture-library history, or generic asset browsing semantics; those belong to dedicated asset-library services.
- **Mapping**: Establishes a 1:1 mapping between HRPAuth core users and game accounts via `core_user_id`.
- **Inheritance**: Game accounts inherit the core service's `username` at registration.
- **Sync**: Pulls user data (like `username`) lazily from HRPAuth core via internal APIs and caches it in Redis; no longer relies on real-time push events to maintain consistency.

## Service Registration (`POST /services/presence`)

Every extension participant registers its presence here. On the frontend side, **individual frontend instances no longer need to register presence**: the frontend consumes SDKs through the compile-time SDK aggregation pipeline (see [SDK Package Contract](0012-sdk-package-contract.md)) instead of runtime discovery. It announces its existence with a "bonjour" handshake; HRPAuth replies "ca va très bien, merci".

### Request

```json
{
  "name": "texture-service",
  "ttl_seconds": 120,
  "scope": {
    "name": "texture-processing",
    "frontend_areas": ["skin", "user"]
  },
  "security_level": 1,
  "root_metadata": {
    "yggdrasil": {
      "serverName": "My Auth Server"
    }
  },
  "interacts_with": ["audit-service"]
}
```

| Field | Description |
| --- | --- |
| `name` | Unique service name. |
| `ttl_seconds` | Optional self-declared lifetime in seconds. `<= 0` or omitted means **never expires**; the record is kept until the HRPAuth process exits. |
| `scope` | Optional scope declaration. |
| `scope.name` | Scope name of this service. |
| `scope.frontend_areas` | Frontend mount areas this service covers. **Non-empty means the service is visible to the WebUI host**; omitted/empty means frontend-invisible (internal only). For first-party frontend integration, these areas are mount zones inside one WebUI, not multiple independent WebUIs or SDK hosts. |
| `sdk_url` | **Removed.** This field was the legacy URL of a runtime JS usage file under the old SDK relay model. It is no longer part of `PresenceRequest` / `PresenceRecord`; SDKs are now delivered as source packages (see [SDK Package Contract](0012-sdk-package-contract.md)) and aggregated into the frontend at compile time, so no runtime URL is recorded anymore. |
| `security_level` | Auth level of this service: `0` none / `1` user / `2` ops. Default `0`, clamped to `0..2`. Used by the universal relay to enforce authentication. |
| `root_metadata` | Optional JSON object to be injected into the HRPAuth Core root (`/`) endpoint response. Allows microservices to provide top-level metadata (e.g., Yggdrasil server information). |
| `interacts_with` | Optional list of other services this service interacts with. By default a service is implicitly treated as interacting with the main service only. |

### Response

```json
{
  "success": true,
  "message": "ca va très bien, merci",
  "data": {
    "service": "texture-service",
    "first_seen": "...",
    "last_seen": "...",
    "expires_at": null
  }
}
```

Repeated heartbeats keep `first_seen`, refresh `last_seen`, and may update all optional fields. Expired records are cleaned lazily on access.

## Internal Service Authentication

HRPAuth uses a dual-layered authentication mechanism for microservice communication:

1.  **OAuth2 (Standard Auth)**: Used for all microservice -> HRPAuth Core API calls. Microservices must provide a Bearer token obtained via the `client_credentials` grant.
2.  **Internal Key (Temporary Secret)**: Used for HRPAuth Core -> microservice callback calls. This key is dynamic, short-lived, and exists only in memory.

### Service Client Registration

A microservice's `client_credentials` identity is a row in the core's
`oauth2_clients` table. Rows come from two coexisting sources — config
seeding takes precedence, self-service registration is the fallback:

1. **Config seeding (`oauth2.service_clients`)** — service clients declared
   in the HRPAuth core config under `oauth2.service_clients` — a list of
   `{ client_id, client_secret, name? }` entries — are seeded idempotently
   at core startup:
   - A missing row is created with `type=confidential`,
     `grant_types=["client_credentials"]`, `is_super=false`, `is_active=true`.
   - An existing row is never modified (a manually rotated secret stays put);
     a config/DB mismatch only logs a warning.
   - An entry whose `client_secret` equals the super client secret is
     skipped with a warning (constant-time comparison): the root credential
     is never copied into an ordinary client row; the affected service
     obtains its own credentials through self-service registration instead.
   - An empty (or absent) list seeds nothing.

2. **Self-service registration (`POST /oauth/register`)** — a satellite
   holding the super client credentials obtains its own low-privilege
   client without any operator-managed secret. The endpoint is an RFC 7591
   *protected registration*: the super client authenticates with
   `client_secret_basic` (RFC 6749 §2.3.1); user tokens are rejected. The
   request body carries `client_name` (a presence-registered service name,
   which becomes the `client_id`) and an optional space-separated `scope`
   bounded by the core config `oauth2.service_scope_allowlist` entry of
   that service (super and `db.*` scopes are never grantable). Success
   returns `201` with `client_id`, `client_secret`, `client_id_issued_at`
   and `client_secret_expires_at = 0` (never expires). Idempotency is a
   deployment extension, not RFC 7591 behavior: re-registering returns the
   same `client_id` + `client_secret` unchanged (no new row, no rotation),
   so a satellite can re-derive its credentials after every restart
   instead of persisting them. Self-registering a config-seeded name
   returns the seeded row. Every call is audit-logged.

The built-in super client (`oauth2.super_client_id` /
`oauth2.super_client_secret`) remains available as the shared service
identity. Tokens issued to any of these clients resolve as service tokens
and satisfy ops (level 2) auth checks on core endpoints such as
`GET /services/users/{uuid}`.

### Temporary Key Acquisition (`POST /services/auth-key`)

After a microservice has registered its presence, it must request a temporary internal key from HRPAuth.

- **Auth**: Bearer token (OAuth2 Client Credentials).
- **Request Body**:
  ```json
  { "name": "texture-service" }
  ```
- **Logic**:
    - HRPAuth verifies the service name matches the `client_id` or is a known registered service.
    - HRPAuth generates a new 64-character random token.
    - The token is stored in the `PresenceRegistry` (memory + Redis) associated with the service.
    - **Overwriting**: If a key already exists for the service name, it is overwritten by the new one, immediately invalidating the old instance's key.
- **Response**:
  ```json
  {
    "success": true,
    "message": "Internal key generated/updated",
    "data": { "internal_key": "..." }
  }
  ```

## Internal Service APIs

These endpoints are provided by HRPAuth Core for microservices to query core data.

### Get User Info (`GET /services/users/:uuid`)

Retrieves basic user information by UUID.

- **Auth**: Bearer token (OAuth2 Client Credentials) with **Ops (Level 2)** permissions.
- **Response**:
  ```json
  {
    "success": true,
    "message": "success",
    "data": {
      "uid": 123,
      "uuid": "...",
      "email": "user@example.com",
      "username": "CurrentName"
    }
  }
  ```

## Heartbeat & Key Revocation

HRPAuth Core runs a background worker to ensure microservices are alive and holding the correct key.

- **Interval**: Every 10 minutes.
- **Action**: HRPAuth sends a `GET /internal/heartbeat` request to each microservice that has an active `InternalKey`.
- **Validation**: The request includes the `X-Internal-Key` header. The microservice must respond with `200 OK`.
- **Revocation**: If the request fails (timeout, connection refused, or `401 Unauthorized`), HRPAuth **immediately revokes the key** in the registry. The service must re-register and re-fetch a key to resume operations.

## Compile-time SDK Aggregation

The runtime JS SDK model is **removed**. The legacy endpoints `GET /services/list` (frontend SDK discovery) and `GET /services/sdk/:name` (SDK relaying) are gone, and the frontend (HRPAuth-Web) no longer discovers or loads SDK modules at runtime — there is no `window['<service-name>-sdk']` contract anymore. Frontend SDKs are delivered as **source packages** and aggregated into a generated frontend build at compile time. The authoritative contract is the [SDK Package Contract](0012-sdk-package-contract.md).

### Data Flow

```
Satellite service                    HRPAuth (main)                   HA-WebUI-SDKHandler
--------------                       ----------------                 ---------------------
POST /services/sdk-packages  ──►     validate + store (fs + Redis)
                                      GET /services/sdk-packages   ◄──  poll (ETag)
                                      GET /services/sdk-packages/{name}/download ◄─ download archive
                                                                         copy template
                                                                         extract + inject SDKs
                                                                         merge npm deps
                                                                         tsc + vite build
                                                                         publish to artifacts dir ──► HRPAuth-Web (dist)
```

### Upload

A satellite service uploads a self-contained SDK package (a `tar.gz` or `zip` archive with exactly one root-level `manifest.json`) to HRPAuth via `POST /services/sdk-packages` (multipart/form-data, ops level 2 auth). HRPAuth validates the archive and the manifest, records the metadata (`name`/`version`/`sha256`/`size`/`routes`/`menu`/`dashboard`/`critical_operations`/`dependencies`/`run_scripts`), and stores the package under `StorageDir/<name>/archive` with a `.meta.json` sidecar. Uploading a package whose `name` already exists is **rejected with `409 sdk_package_conflict`**; replacing a package requires deleting it first (`DELETE /services/sdk-packages/{name}`).

### Pull & Build

The SDK handler (`HA-WebUI-SDKHandler`) talks to **HRPAuth only**: it polls `GET /services/sdk-packages` for the current package set and downloads each archive via `GET /services/sdk-packages/{name}/download`. HRPAuth signs the list response with an `ETag` (sha256 of the package list) and answers `304 Not Modified` on a matching `If-None-Match`. The handler sends its own fingerprint of the previously parsed list (sha256 over the parsed package metadata) as `If-None-Match`, and after every `200` it recomputes that fingerprint from the parsed response rather than storing the server-sent `ETag` header; a build is triggered when the fingerprint changes. For every build it copies the current frontend template, injects all SDK sources into `src/generated/services/<name>/`, merges npm dependencies, and compiles (`npm run build`, or `tsc -b` + `vite build` when the template has no build script; dependencies are installed once at template setup, not per build). Publishing replaces the configured fixed artifacts directory (`.tmp` staging, remove-then-rename); see `sdk-package.md` Build Policy for the exact current behavior.

### Trust Boundary

- Satellite services **absolutely trust HRPAuth**: all authentication is performed by HRPAuth (ops level 2) and satellite services never expose their own auth entry points to the WebUI.
- The SDK handler pulls packages **from HRPAuth only**; it never reaches satellite services directly.
- HRPAuth is the **single trusted broker** for SDK packages.

### Package API

| Endpoint | Method | Auth | Purpose |
| --- | --- | --- | --- |
| `/services/sdk-packages` | POST | ops (level 2) | Upload a SDK package (multipart/form-data) |
| `/services/sdk-packages` | GET | ops (level 2) | List stored package metadata |
| `/services/sdk-packages/{name}` | GET | ops (level 2) | Fetch single package metadata |
| `/services/sdk-packages/{name}/download` | GET | ops (level 2) | Download a package archive (binary, ETag) |
| `/services/sdk-packages/{name}` | DELETE | ops (level 2) | Delete a package |

All package endpoints are part of the Business API (`docs/api/openapi/hrpauth-business.yaml`); error codes are registered in `docs/api/error-codes.md`.

### manifest.json Example

Each package declares its frontend integration surface declaratively in a root-level `manifest.json`, replacing the old runtime `window['<service-name>-sdk']` global. Example (full field reference in [sdk-package.md](0012-sdk-package-contract.md)):

```json
{
  "schema_version": 1,
  "name": "texture-service",
  "version": "1.0.0",
  "routes": [
    {
      "path": "/texture",
      "module": "src/pages/TexturePage",
      "title": "Texture Studio"
    }
  ],
  "menu": [
    {
      "label": "Texture Studio",
      "path": "/texture"
    }
  ],
  "dashboard": [
    {
      "label": "Texture Studio",
      "path": "/texture"
    }
  ],
  "profile_cards": [
    {
      "title": "Texture Studio",
      "module": "src/profile/TextureStudioCard",
      "priority": 100
    }
  ],
  "dependencies": {
    "some-lib": "^1.0.0"
  },
  "run_scripts": false
}
```

### Removed Runtime Endpoints

The following endpoints are **removed** and must not be used:

- `GET /services/list` — runtime frontend SDK discovery.
- `GET /services/sdk/:name` — runtime JS SDK relaying.

The frontend has no runtime SDK module loader anymore; frontend integration surfaces (routes, navbar menu, dashboard items, Critical Operations links, Profile cards) are declared in `manifest.json` and materialized into the compiled bundle by the SDK handler.

`routes` are required for every package: HRPAuth core upload validation rejects
a missing or empty `routes` array even when `profile_cards` is declared.

## Route Rules (`POST /services/route`)

A microservice declares its routing rules. Registration requires the service to be registered via `/services/presence` first.

### Request

```json
{
  "name": "texture-service",
  "rules": [
    {
      "scope": "texture-processing",
      "paths": ["/texture/*", "/texture/upload"],
      "pre_url": "http://texture-service:8080",
      "post_url": "http://texture-service:8080/post"
    }
  ]
}
```

| Field | Description |
| --- | --- |
| `scope` | Scope name this rule belongs to. |
| `paths` | Exact paths and `*` prefix wildcards this rule matches. |
| `pre_url` | Non-empty declares a **pre-route**: requests matching the path are forwarded here. |
| `post_url` | Non-empty declares a **post-route**: successful main responses are dispatched here. |

Rules are replaced as a whole per service (last registration wins).

## Relay Rules (`POST` / `DELETE` / `GET /services/relay`)

A microservice asks HRPAuth to act as a relay for a URL mapping: requests hitting `dest` (a public path on the main service) are forwarded to `source` (the microservice address). Registration requires prior presence registration.

### Register

```json
{
  "name": "texture-service",
  "relays": [
    { "dest": "/serviceone", "source": "http://127.0.0.1:2703/serviceone" }
  ]
}
```

### Path Mapping (prefix join)

`dest` acts as a prefix; the remaining path is appended to `source`:

```
dest=/serviceone, source=http://127.0.0.1:2703/serviceone
GET /serviceone       -> http://127.0.0.1:2703/serviceone
GET /serviceone/foo   -> http://127.0.0.1:2703/serviceone/foo
GET /serviceonex      -> not matched (boundary isolation)
```

Longest `dest` prefix wins. `dest` is normalized to start with `/` and have no trailing slash.

### Delete

```json
{ "name": "texture-service", "dest": "/serviceone" }
```

### List

```
GET /services/relay          # all rules
GET /services/relay?name=    # filtered by service
```

## Hook Rules (`POST /services/hook`)

A microservice can register webhook hooks that are invoked during the OAuth2 authorization flow. Registration requires the service to be registered via `/services/presence` first. Rules are stored per service (Redis-backed, 30-day TTL) and replacing the rule set for a service overwrites the previous one.

```json
{
  "name": "my-service",
  "hooks": [
    {
      "paths": ["/oauth/token"],
      "phase": "auth.input",
      "capability": "observe",
      "priority": 10,
      "fail_policy": "ignore",
      "patch_allowlist": [],
      "url": "https://my-service.example.com/hooks/auth"
    }
  ]
}
```

- `paths`: request paths to match; a trailing `*` means prefix match.
- `phase`: one of `auth.input`, `auth.risk`, `auth.post_verify`.
- `capability`: one of `observe` (notification only), `patch` (may patch allowed fields), `veto` (may reject the flow).
- `fail_policy`: `ignore` (default), `fail_open`, or `fail_closed` — behavior when the hook callback fails.
- `priority`: lower values run first; ties are ordered by service name.

The hook callback receives a JSON payload describing the request (`id`, `path`, `method`), the auth context (e.g. `email`, `user_uuid`, `available_factors`), and the policy context (`patch_allowlist`, `capabilities`), and answers with `{"result": "...", "patch": {...}}`.

## Request Orchestration

Two middlewares are mounted before all main-service routes; controllers are untouched.

### Pre-Routing (orchestration)

When an incoming request path matches rules with `pre_url`:

1. Compute the required auth level = **dynamic max** of the `security_level` of all matched pre services (strictest wins when multiple services participate).
2. Verify the caller's credential level (see [Auth Levels](#auth-levels)); insufficient → `401/403`, request aborted.
3. Fan out: forward the request to each pre service in rule order (method, query, Content-Type, `request_id` passed through).
4. First successful forward short-circuits and returns the microservice's response.
5. **All fail → fail-open**, the request continues to the main service.

### Post-Routing (orchestration)

After the main service responds with a **2xx** status, HRPAuth asynchronously POSTs `{path, method, request_id, status, response}` to every `post_url`. Non-2xx does not trigger. Post dispatch is out-of-band (side semantics); failures are ignored and do not affect the main response.

### Relay (independent mechanism)

Relay is independent from pre/post orchestration and is mounted before it. On a `dest` hit, the caller's auth level is checked against the service's `security_level`, then the request is forwarded. **Relay failure returns `502` and never falls back to the main service** (the path belongs to the microservice). `/services/*` system endpoints never participate in relay or orchestration.

## Auth Levels

Three credential levels are defined:

| Level | Meaning | Accepted Credentials |
| --- | --- | --- |
| `0` | None | No credentials required |
| `1` | User | OAuth2 **user** Bearer token |
| `2` | Ops | Manage Token (M-T) **or** OAuth2 service token (`client_credentials` / `.as-service` scope) |

Resolution order: no `Authorization: Bearer` → `0`; token equals M-T → `2`; resolvable OAuth2 token → `2` if service token, else `1`; unresolvable → `0`.

Enforcement:

- **Single service (implicit "main-only" interaction)**: required level = that service's `security_level`.
- **Multiple services involved in one request** (e.g. pre fan-out): required level = `max` of their `security_level` (strictest wins), computed dynamically at request time.

Insufficient credentials: no credential → `401 oauth_login_required`; credential present but too weak → `403 insufficient_auth_level`.

Post-routing dispatch is initiated by HRPAuth itself and is not subject to caller auth-level checks.
