# ADR-0001: Generic Auth Core — Yggdrasil and Skins Split Out

- **Status**: accepted
- **Date**: 2026-11-04

## Context

HRPAuth historically carried the Minecraft-specific Yggdrasil authentication
protocol and the skin/texture pipeline in addition to general-purpose account
and OAuth2 logic. That coupling made the core heavy and forced every deployment
to carry Minecraft concerns it may not need.

## Decision

HRPAuth is positioned as a **general-purpose authentication core** only:
accounts, login, multi-factor security, and OAuth2 issuance. The Yggdrasil
protocol and skin/texture features were **split out on 2026-10-06** into
dedicated microservices: HRPAuth-Yggdrasil-API, HASkinLib, and HASkinProxy.
They are not part of this repository.

## Consequences

- The route table exposes no Yggdrasil/texture endpoints; Minecraft-specific
  controllers and services were deleted outright.
- Remaining Yggdrasil references in this repo are **integration seams, not
  features**: a config pointer to the external Yggdrasil-API service, a
  heartbeat call, and an anonymous-relay prefix for `/yggdrasil-api`.
- Some identifiers are protocol residue kept for compatibility (see ADR-0003
  for the dead Mojang error codes; the `user.mojang-bind-*` scopes were
  removed from the registry in commit `266c65c`).
- Cost: behavior that used to live here (profile/texture serving, session
  join/hasJoined) must now be understood across repository boundaries; the
  split rationale itself is recorded in HA-Contract/Wiki, not here.

## Evidence

- Positioning statement: `README.md:L5` ("general-purpose authentication core
  service and the OAuth2 authorization center"), `README.md:L7` (split-out note
  with date 2026-10-06 and target services).
- Removal commits (verified via `git show --stat`):
  - `9ab4e64` "strip legacy Yggdrasil stack" — deleted
    `controllers/yggdrasil_controller.go` (-735),
    `controllers/texture_controller.go` (-593),
    `services/texture_service.go` (-856),
    `services/profile_key_service.go` (-282), ~-4050 lines total.
  - `087030f` "remove Minecraft logic" — added migration
    `000012_remove_minecraft_logic` and config v7→8 migration
    (`config/migrations.go::migrateV7ToV8`, which runs
    `delete(cfg, "yggdrasil")`).
  - `39d380d` — deleted `clients/yggdrasil_client.go` (-168) and tests.
- Current route table has no yggdrasil/texture/mojang endpoint:
  `main.go:L185-L256`.
- Integration seams still present: `config/config.go::YggdrasilAPIConfig`
  (`YggdrasilAPIConfig{BaseURL}`, held by `config/config.go::Config`),
  `registry/heartbeat.go::checkService` (heartbeat to Yggdrasil-API
  `/internal/heartbeat`), `controllers/relay_controller.go::relayAllowsAnonymous`
  (anonymous relay for `/yggdrasil-api` prefix).
- Not verified: the exact set of columns removed by migration `000012` (the file
  is no longer in the current tree after the baseline squash; its prior
  existence is confirmed only via `087030f` stat).
