# Knowledge Base

## What This Is

This knowledge base is **HRPAuth's memo to itself**. It serves self-hosting and
maintenance of this repository: implementation orientation, internal boundaries,
and why things are the way they are. It is maintained alongside the code and is
not meant to make external projects accommodate this one.

## Order of Authority

This knowledge base **does not define outward behavior**. Outward behavior of the
HA ecosystem is defined by [HA-Contract](https://github.com/CoreMatch/HA-Contract)
(OpenAPI specs under `docs/api/openapi/` and contract documents under `docs/`).
If an entry here ever appears to contradict a contract, **the contract wins and
the entry is stale — fix the entry, never the contract**, and surface the
conflict to the owner.

## Format

Each entry is one page in ADR style:

- **Status** (`accepted` / `superseded` / ...), **Date**
- **Context** — the problem and constraints
- **Decision** — what was chosen
- **Consequences** — including costs and limitations
- **Evidence** — `file:line`, protocol entries, commit hashes. Every piece of
  evidence must have been actually checked; anything that could not be verified
  is marked "not verified". Do not fabricate.

## Entries

| # | Title | Status | Date |
|---|-------|--------|------|
| [0001](0001-generic-auth-core-yggdrasil-split-out.md) | Generic Auth Core: Yggdrasil and Skins Split Out | accepted | 2026-11-04 |
| [0002](0002-account-model-with-four-second-factors.md) | Account Model with Four Second Factors | accepted | 2026-11-04 |
| [0003](0003-oauth2-authorization-server-with-super-client.md) | OAuth2 Authorization Server with Built-in Super Client | accepted | 2026-11-04 |
| [0004](0004-in-process-registries-and-relay-forwarding.md) | In-Process Registries and Relay/Orchestration Forwarding | accepted | 2026-11-04 |
| [0005](0005-versioned-config-schema-and-migration-chain.md) | Versioned Config Schema and Migration Chain | accepted | 2026-11-04 |
| [0006](0006-single-binary-and-container-deployment.md) | Two Deployment Forms: Single Binary and Container Image | accepted | 2026-11-04 |
