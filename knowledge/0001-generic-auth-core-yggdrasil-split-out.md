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

- The route table exposes no Yggdrasil/texture endpoints.

## Evidence

- Positioning statement: `README.md:L5` ("general-purpose authentication core
  service and the OAuth2 authorization center"), `README.md:L7` (split-out note
  with date 2026-10-06 and target services).
- Current route table has no yggdrasil/texture/mojang endpoint:
  `main.go:L185-L256`.
