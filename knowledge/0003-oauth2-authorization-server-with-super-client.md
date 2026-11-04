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
  `authorize/decision`, `token`, `revoke` (`main.go:177-181`). Token grants:
  `authorization_code`, `refresh_token`, `client_credentials`
  (`controllers/oauth2_controller.go:248-277`). PKCE S256 is enforced
  (`services/oauth2_service.go:204`, `verifyPKCE:568-572`).
- Two built-in clients are ensured at startup (`main.go:78` →
  `EnsureBuiltInClients`): a **public** client "HRPAuth WebUI" and an internal
  **super client** "HRPAuth Internal Super Client"
  (`services/oauth2_service.go:58-163`). The super client allows only
  `client_credentials`, is flagged `is_super`/`is_internal`, and its secret is
  held in config, not the DB (DB stores placeholder `"(config)"`;
  verification compares against `config.AppConfig.OAuth2.SuperClientSecret`,
  `services/oauth2_service.go:171-177`).
- Scope registry is split into four groups: `publicSiteScopes`,
  `serviceSiteScopes`, `targetedServiceScopes` (`*.as-service` variants), and
  `databaseScopes` (`services/oauth2_service.go:506-566`). Database scopes are
  grantable only through `super_client_extra_scopes` config delegation.

## Consequences

- The `user.mojang-bind-*` scopes are **protocol residue**: they remain in the
  scope lists but have no handler or enforcement point in this repo (the
  Mojang-bind/Yggdrasil behavior they relate to was split out, see ADR-0001).
  They are kept for compatibility, not active use.
- Deprecated `keygen` and `texture.*` scopes were removed (`f170607`).
- Cost: super-client secret lives in config, so secret rotation is a config
  change, not a DB update; `db.*` scopes have no in-repo consumer (delegated to
  an external database service — not verified).

## Evidence

- OAuth2 endpoints: `main.go:177-181`; handlers `controllers/oauth2_controller.go`
  (`Authorize:154`, `AuthorizeDecision:185`, `Token:229`, `Revoke:280`).
- Client model: `models/models.go:33-51` (`type enum('public','confidential')`,
  `is_internal`, `is_super`, `is_active`); DDL
  `database/migrations/000001_baseline.up.sql:26-42`.
- Built-in client seeding: `main.go:78`; `services/oauth2_service.go:49-56`
  (`EnsureBuiltInClients`), `:58-105` (public WebUI client), `:107-163`
  (`upsertBuiltInSuperClient`, name at `:132`, `client_credentials`-only `:126`,
  `is_super` `:138`).
- Super client config default: `config/migrations.go:340-343`
  (`super_client_id = "hrpauth-internal-super"`, auto-generated secret);
  config field `config/config.go:135-137`.
- Super client auth via config secret: `services/oauth2_service.go:171-177`;
  placeholder `"(config)"` at `:131,147`; header-forwarding change in commit
  `e5f468f`.
- Scope lists: `services/oauth2_service.go:506-524` (public), `:526-532`
  (service), `:534-553` (targeted `.as-service`), `:558-566` (database).
- `mojang-bind` occurrences: `services/oauth2_service.go:509,537,538` only; no
  consuming handler found (grep across repo). Related dead error codes:
  `controllers/api_response.go:22-23,29`.
- Commits: `e5f468f` (super client auth + header forwarding), `f170607` (remove
  deprecated keygen/texture scopes).
- Not verified: whether `db.*` scopes are consumed by an external database
  service; whether `user.mojang-bind-*` is intentionally retained for a future
  Yggdrasil-API callback.
