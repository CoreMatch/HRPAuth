# ADR-0003: OAuth2 Authorization Server with Built-in Super Client

- **Status**: accepted

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
  `authorize/decision`, `token`, `revoke`, `register`
  (`main.go:L188-L193`). Token grants:
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
  ops (level 2) auth checks. Entries whose `client_secret` equals the super
  client secret are **skipped with a warning** before any create/lookup
  (constant-time comparison, `services/oauth2_service.go::isSuperClientSecret`):
  the root credential is never copied into an ordinary client row; the
  affected service is expected to self-register via `POST /oauth/register`.
- Satellite services may also obtain their client credentials through
  **self-service registration** at `POST /oauth/register`
  (`controllers/oauth2_controller.go::RegisterClient`), an RFC 7591
  *protected registration* endpoint: the initial access credential is the
  built-in super client, authenticated with `client_secret_basic`
  (RFC 6749 §2.3.1); user tokens are rejected. The requested `client_name`
  must already be presence-registered, and granted scopes are bounded by the
  per-service allowlist `oauth2.service_scope_allowlist`
  (`services/oauth2_service.go::ServiceRegistrationScopes`) intersected with
  the known service scope set — super privileges and `db.*` scopes can never
  be granted this way. Issued rows are fixed to `type=confidential`,
  `grant_types=["client_credentials"]`, `is_super=false`, `is_active=true`.
  Re-registering an existing service returns its existing credentials
  unchanged: a **get-or-create extension** on top of RFC 7591 (which issues
  a fresh client per registration), registered as such in the contract.
  One exception: an existing row whose stored secret equals the super
  client secret (legacy poisoned data) is **refused**, never returned —
  handing it out would distribute the root credential
  (`services/oauth2_service.go::ErrOAuthRegistrationConflict`, surfaced as
  `400 invalid_client_metadata` with an operator-actionable message). The
  response is `201` with `client_id`, `client_secret`, `client_id_issued_at`
  and `client_secret_expires_at = 0` (the secret never expires). Config
  seeding and self-registration coexist: a config-seeded row wins, and a
  later self-registration for the same name just returns it. RFC 7591 §3
  requires TLS for the registration endpoint; running it over HTTP is an
  operational exception limited to the isolated dev intranet. Every call is
  audit-logged (`[CLIENT-REG]` lines: caller, service, requested scopes,
  outcome, timestamp).
- Scope registry is split into four groups: `publicSiteScopes`,
  `serviceSiteScopes`, `targetedServiceScopes` (`*.as-service` variants), and
  `databaseScopes` (`services/oauth2_service.go::publicSiteScopes`,
  `services/oauth2_service.go::serviceSiteScopes`,
  `services/oauth2_service.go::targetedServiceScopes`,
  `services/oauth2_service.go::databaseScopes`). Database scopes are grantable
  only through `super_client_extra_scopes` config delegation.

## Consequences

- The `user.mojang-bind-*` scopes are **protocol residue**: the
  Mojang-bind/Yggdrasil behavior they relate to lives outside this repo (see
  ADR-0001), with no consumer anywhere in the ecosystem. The guard test
  `services/oauth2_scopes_test.go::TestOrphanScopesStayRemoved` keeps them —
  and the never-wired `totp.status*` / `email-2fa.status*` scopes — out of the
  registry unless a consumer appears.
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
  therefore each need their own client credentials — obtained either from
  the `oauth2.service_clients` config seed or from self-service
  registration. The registration endpoint is **credential issuance, not a
  token exchange**: the super client's credentials are only the gate for
  creating/fetching a per-service low-privilege client
  (`controllers/oauth2_controller.go::RegisterClient`); no issued access
  token is ever traded or downgraded. The deployment model built on this:
  operators only configure the super credentials per satellite; the
  satellite exchanges them for its own client credentials at startup and
  discards the super secret from memory afterwards (the config file keeps
  it — the program never modifies its own config). Because get-or-create
  returns the same stable `client_id`/`client_secret` on every call, a
  restart can re-derive the credentials without persisting them.

## Evidence

- OAuth2 endpoints: `main.go:L188-L193`; handlers
  `controllers/oauth2_controller.go::Authorize`,
  `controllers/oauth2_controller.go::AuthorizeDecision`,
  `controllers/oauth2_controller.go::Token`,
  `controllers/oauth2_controller.go::Revoke`,
  `controllers/oauth2_controller.go::RegisterClient`.
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
- Self-service registration:
  `services/oauth2_service.go::RegisterServiceClient` (get-or-create),
  `services/oauth2_service.go::ServiceRegistrationScopes` (allowlist
  intersection); Basic-only credential parsing
  `controllers/oauth2_controller.go::basicClientCredentials`; audit line
  `controllers/oauth2_controller.go::auditClientRegistration`; allowlist
  config field `config/config.go::ServiceScopeAllowlist`
  (`config/config.go::parseServiceScopeAllowlist`), schema version 14 via
  `config/migrations.go::migrateV13ToV14` (empty default, backward
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
- No consuming handler for the `mojang-bind` scopes found (grep across repo).
- Commits: `e5f468f` (super client auth + header forwarding), `266c65c` (drop
  orphan `user.mojang-bind-*`, `totp.status*`, `email-2fa.status*` scopes).
- Not verified: whether `db.*` scopes are consumed by an external database
  service.
