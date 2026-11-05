# ADR-0003: OAuth2 Authorization Server with Built-in Super Client

- **Status**: accepted
- **Date**: 2026-11-04

## Context

HRPAuth is the OAuth2 authorization center of the HA ecosystem. It must issue
tokens to a first-party web frontend, to internal microservices calling each
other, and support a privileged internal client used for service-to-service
operations. Scopes need to distinguish user-facing, service-facing, and
database-level access.

## Decision

HRPAuth embeds a full OAuth2 authorization server and seeds built-in clients at
boot:

- Endpoints under `/oauth/...`: `authorize`, `login-ticket`,
  `authorize/decision`, `token`, `revoke` (`main.go:L188-L192`). Token grants:
  `authorization_code`, `refresh_token`, `client_credentials`
  (`controllers/oauth2_controller.go::Token`). PKCE S256 is enforced
  (`services/oauth2_service.go::ValidateAuthorizationRequest`,
  `services/oauth2_service.go::verifyPKCE`).
- Two built-in clients are ensured at startup (`main.go::EnsureBuiltInClients`):
  a **public** client "HRPAuth WebUI" and an internal **super client**
  "HRPAuth Internal Super Client"
  (`services/oauth2_service.go::upsertBuiltInPublicClient`,
  `services/oauth2_service.go::upsertBuiltInSuperClient`). The super client
  allows only `client_credentials`, is flagged `is_super`/`is_internal`, and
  its secret is held in config, not the DB (DB stores placeholder
  `"(config)"`; verification compares against
  `config.AppConfig.OAuth2.SuperClientSecret`,
  `services/oauth2_service.go::AuthenticateClient`).
- Satellite service clients are declared in config (`oauth2.service_clients`,
  each entry `client_id` / `client_secret` / optional `name`) and seeded
  idempotently by the same startup hook
  (`services/oauth2_service.go::ensureConfiguredServiceClients`): a missing
  row is created as `type=confidential`, `grant_types=["client_credentials"]`,
  `is_super=false`, `is_active=true`; an existing row is never modified
  (config drift only logs a warning). An empty list seeds nothing. Their
  `client_credentials` tokens resolve as service tokens and therefore satisfy
  ops (level 2) auth checks.
- Scope registry is split into four groups: `publicSiteScopes`,
  `serviceSiteScopes`, `targetedServiceScopes` (`*.as-service` variants), and
  `databaseScopes` (`services/oauth2_service.go::publicSiteScopes`,
  `services/oauth2_service.go::serviceSiteScopes`,
  `services/oauth2_service.go::targetedServiceScopes`,
  `services/oauth2_service.go::databaseScopes`). Database scopes are grantable
  only through `super_client_extra_scopes` config delegation.

## Consequences

- The `user.mojang-bind-*` scopes were **protocol residue**: they had no
  handler or enforcement point in this repo (the Mojang-bind/Yggdrasil
  behavior they relate to was split out, see ADR-0001), and were formerly kept
  in the scope lists for compatibility rather than active use. Having been
  confirmed to have no consumer anywhere in the ecosystem, they were removed
  from the registry in commit `266c65c`; the guard test
  `services/oauth2_scopes_test.go::TestOrphanScopesStayRemoved` keeps them out
  unless a consumer appears.
- Deprecated `keygen` and `texture.*` scopes were removed (`f170607`).
- Cost: super-client secret lives in config, so secret rotation is a config
  change, not a DB update; `db.*` scopes have no in-repo consumer (delegated to
  an external database service — not verified).
- There is **no token exchange / downgrade mechanism**: no RFC 8693-style
  grant, no endpoint that trades a higher-privilege token for a
  lower-privilege one (`/oauth/token` supports exactly
  `authorization_code` / `refresh_token` / `client_credentials`). The two
  adjacent primitives are not exchanges: `POST /services/auth-key`
  (`controllers/presence_controller.go::AuthKey`) requires ops-level auth and
  issues an `X-Internal-Key` — an internal credential for
  internal-key-gated endpoints, **not** an OAuth2 access token; and the
  `scope` parameter of `client_credentials` only narrows scopes **at
  issuance** against the client's own credentials
  (`services/oauth2_service.go::resolveRequestedScopes`). Satellite services
  therefore each need their own client credentials (built-in super client or
  a config-seeded service client).

## Evidence

- OAuth2 endpoints: `main.go:L188-L192`; handlers
  `controllers/oauth2_controller.go::Authorize`,
  `controllers/oauth2_controller.go::AuthorizeDecision`,
  `controllers/oauth2_controller.go::Token`,
  `controllers/oauth2_controller.go::Revoke`.
- Client model: `models/models.go::OAuth2Client`
  (`type enum('public','confidential')`, `is_internal`, `is_super`,
  `is_active`); DDL
  `database/migrations/000001_baseline.up.sql::oauth2_clients`.
- Built-in client seeding: `main.go::EnsureBuiltInClients`;
  `services/oauth2_service.go::EnsureBuiltInClients`,
  `services/oauth2_service.go::upsertBuiltInPublicClient` (public WebUI
  client), `services/oauth2_service.go::upsertBuiltInSuperClient` (name
  "HRPAuth Internal Super Client", `client_credentials`-only, `is_super`).
- Config-driven service-client seeding:
  `services/oauth2_service.go::ensureConfiguredServiceClients`; config
  fields `config/config.go::ServiceClientConfig`
  (`config/config.go::parseServiceClients`); schema version 13 via
  `config/migrations.go::migrateV12ToV13` (empty default, backward
  compatible).
- Super client config default: `config/migrations.go::migrateV3ToV4`
  (`super_client_id = "hrpauth-internal-super"`, auto-generated secret);
  config fields `config/config.go::SuperClientID`,
  `config/config.go::SuperClientSecret`.
- Super client auth via config secret:
  `services/oauth2_service.go::AuthenticateClient`; placeholder `"(config)"`
  in `services/oauth2_service.go::upsertBuiltInSuperClient`;
  header-forwarding change in commit `e5f468f`.
- Scope lists: `services/oauth2_service.go::publicSiteScopes` (public),
  `services/oauth2_service.go::serviceSiteScopes` (service),
  `services/oauth2_service.go::targetedServiceScopes` (targeted
  `.as-service`), `services/oauth2_service.go::databaseScopes` (database).
- `mojang-bind` occurrences: `services/oauth2_service.go:L509-L538 @ 543e8f5`
  only; no consuming handler found (grep across repo). Related dead error
  codes: `controllers/api_response.go::CodeInvalidMojangUUID`,
  `controllers/api_response.go::CodeMojangUUIDRequired`,
  `controllers/api_response.go::CodeNoMojangUUID`.
- Commits: `e5f468f` (super client auth + header forwarding), `f170607` (remove
  deprecated keygen/texture scopes), `266c65c` (drop orphan `user.mojang-bind-*`,
  `totp.status*`, `email-2fa.status*` scopes).
- Not verified: whether `db.*` scopes are consumed by an external database
  service. (The open question whether `user.mojang-bind-*` was intentionally
  retained for a future Yggdrasil-API callback is closed: the scopes were
  removed in commit `266c65c` after confirming no consumer anywhere in the
  ecosystem.)
