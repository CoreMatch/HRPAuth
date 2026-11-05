# ADR-0007: Internal Endpoints for Yggdrasil-API — verify-token and claim-user

- **Status**: accepted

## Context

After the Yggdrasil split (ADR-0001), HRPAuth-Yggdrasil-API is a separate
service that must (a) resolve OAuth2 access tokens issued by this core and
(b) provision core users for the claim flow restored from the monolith
(`POST /admin/claim-user`, `POST /admin/force-bind`). The core exposes two
internal endpoints for this purpose; this entry records their current
implementation and their relationship to the Yggdrasil-API admin endpoints.

## Decision

Both endpoints are registered under the API group at
`main.go:L208-L209` and are gated by
`RequireAuthLevel(c, SecurityLevelOps)` as their first action
(`controllers/auth_controller.go::VerifyToken`,
`controllers/auth_controller.go::ClaimUserInternal`).

**Auth tier** (`controllers/auth_level.go::resolveAuthLevel`,
`controllers/auth_level.go::RequireAuthLevel`): no bearer → `401
oauth_login_required`; admin token M-T (`config.AppConfig.Manage.Token`) or
an OAuth2 service token (`SubjectType == "service"`, i.e. issued via
`client_credentials` / `.as-service`) → level 2 (ops), accepted; a plain
user token → `403 insufficient_auth_level`.

**`POST /internal/verify-token`** — body `{"token": ...}`; resolves the
token via `services/oauth2_service.go::ResolveAccessToken` (unknown,
revoked, or expired → `401 oauth_invalid_grant`). Responses:

- service token → `data: {is_service: true, scopes: [...]}` (no user
  fields);
- user token whose user row no longer resolves → `401 oauth_invalid_grant`
  ("Token is not associated with a user");
- user token → `data: {uid, uuid, email, username, is_service: false,
  scopes: [...]}`.

**`POST /internal/claim-user`** — body `{username, email, password}`;
validation: username ≥ 3 chars (`username_too_short`), email parseable via
`net/mail` (`invalid_email`), password ≥ 6 chars (`password_too_short`).
Then, in order:

1. **Idempotency by (username, email)**: if the username already belongs
   to a user with the *same* email, that user's `{uid, uuid}` is returned
   again; the same username with a *different* email → `409
   username_already_bound`.
2. **Email uniqueness across all core users** (restored pre-split rule):
   email already taken → `409 email_already_registered`.
3. Otherwise a core user is created with `verified = true`, an unsigned
   UUID (`utils.GenerateUnsignedUUID`), and `uid = MAX(uid) + 1`; response
   is `{uid, uuid}`.

The core endpoint only provisions the user; the game-account link and the
`cbh` flip happen on the Yggdrasil-API side (see below).

**Scopes** `user.claim.as-service` and `user.force-bind.as-service` are
the first two entries of `services/oauth2_service.go::serviceSiteScopes`,
which is the scope set granted to the built-in super client
(`services/oauth2_service.go::upsertBuiltInSuperClient`,
`client_credentials` only; ADR-0003). A guard test pins both entries in the
registry (`services/oauth2_scopes_test.go`). The core itself does not
enforce either scope on any endpoint — its gate for the internal endpoints
is the ops auth level above; the scope checks are performed by
HRPAuth-Yggdrasil-API (below).

**Relationship with HRPAuth-Yggdrasil-API** (consumer side):

- Yggdrasil-API authenticates its server-to-core calls with its own
  `client_credentials` token obtained from this core's `/oauth/token`
  [xref] (`HRPAuth-Yggdrasil-API/utils/core_api_client.go::GetCoreAPIClient`),
  which resolves as a service token and therefore passes the ops gate.
- `POST /admin/claim-user`
  [xref] (`HRPAuth-Yggdrasil-API/controllers/admin_controller.go::ClaimUser`):
  requires a service token with scope `user.claim.as-service` (bearer
  resolved via this core's `/internal/verify-token` through
  [xref] `HRPAuth-Yggdrasil-API/clients/core_client.go::ResolveToken`),
  looks up the proxy-registered game account (`cbh = 0` required, else
  `409 user_not_claimable`), calls this core's `/internal/claim-user`
  ([xref] `HRPAuth-Yggdrasil-API/clients/core_client.go::ClaimUser`, which
  maps the core envelope codes `email_already_registered` /
  `username_already_bound` to typed errors), then links the account to the
  returned core-user UUID
  ([xref] `HRPAuth-Yggdrasil-API/services/admin_service.go::ClaimProxyAccount`).
- `POST /admin/force-bind`
  [xref] (`HRPAuth-Yggdrasil-API/controllers/admin_controller.go::ForceBind`):
  requires a service token with scope `user.force-bind.as-service`; the
  target core user is authenticated by email + password via this core's
  `/internal/verify-credentials` (not verify-token); the `mojang_uuid`
  transfer and the deletion of the source proxy account happen in
  Yggdrasil-API's own database
  ([xref] `HRPAuth-Yggdrasil-API/services/admin_service.go::ForceBindTransfer`).
- Both admin endpoints reach Yggdrasil-API through this core's relay under
  the `/yggdrasil-api` prefix (`controllers/relay_controller.go:L276`;
  ADR-0004).

## Consequences

- The two endpoints are the only core surface that provisions users outside
  the public `/register` flow; claim-created users start `verified = true`
  while `/register` users start `verified = false`
  (`controllers/auth_controller.go::Register`).
- Claim idempotency is keyed on (username, email): a retried claim after a
  game-side link failure re-resolves the same core user instead of
  tripping the email-uniqueness check.
- `user.claim.as-service` / `user.force-bind.as-service` exist only in the
  super-client scope registry and in Yggdrasil-API's scope checks; no core
  endpoint evaluates them.

## Evidence

- Routes: `main.go:L208-L209`.
- Handlers: `controllers/auth_controller.go::VerifyToken` (incl. the
  `is_service` / `scopes` response branches),
  `controllers/auth_controller.go::ClaimUserInternal` (incl. the
  idempotency and email-uniqueness order).
- Auth tier: `controllers/auth_level.go::resolveAuthLevel`,
  `controllers/auth_level.go::RequireAuthLevel`,
  `controllers/auth_level.go::SecurityLevelOps`; error codes
  `controllers/api_response.go::CodeInsufficientAuthLevel`,
  `controllers/api_response.go::CodeUsernameAlreadyBound`,
  `controllers/api_response.go::CodeEmailAlreadyRegistered`.
- Token resolution: `services/oauth2_service.go::ResolveAccessToken`
  (`IsService` from `SubjectType == "service"`).
- Scope registry: `services/oauth2_service.go::serviceSiteScopes` (lines
  `services/oauth2_service.go:L524-L525`),
  `services/oauth2_scopes_test.go::TestServiceSiteScopesCarryAdminFlowScopes`
  (guard); super
  client grant `services/oauth2_service.go::upsertBuiltInSuperClient`.
- Tests: `controllers/verify_token_test.go::TestVerifyTokenResolvesServiceToken`,
  `controllers/verify_token_test.go::TestVerifyTokenResolvesUserToken`,
  `controllers/claim_user_internal_test.go::TestClaimUserInternalIsIdempotentForSameUsernameAndEmail`,
  `controllers/claim_user_internal_test.go::TestClaimUserInternalRequiresOpsAuth`.
- Consumer-side citations are marked [xref] above (HRPAuth-Yggdrasil-API
  repository); its own record of the same flow is
  [xref] `HRPAuth-Yggdrasil-API/knowledge/0008-claim-force-bind-implementation.md`.
