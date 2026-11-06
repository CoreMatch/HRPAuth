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

- **Status** (`accepted` / `superseded` / ...)
- **Context** — the problem and constraints
- **Decision** — what was chosen
- **Consequences** — including costs and limitations
- **Evidence** — stable anchors, resolved from the repo root: symbol anchors
  `file.ext::Symbol`, field anchors `file.ext (field: key)`, or bare line
  anchors `file.ext:Lx-Ly` only as a last resort; plus protocol entries and
  commit hashes. Every piece of evidence must have been actually checked;
  anything that could not be verified is marked "not verified". Do not
  fabricate.

## Entries

| # | Title | Status |
|---|-------|--------|
| [0001](0001-generic-auth-core-yggdrasil-split-out.md) | Generic Auth Core: Yggdrasil and Skins Split Out | accepted |
| [0002](0002-account-model-with-four-second-factors.md) | Account Model with Four Second Factors | accepted |
| [0003](0003-oauth2-authorization-server-with-super-client.md) | OAuth2 Authorization Server with Built-in Super Client | accepted |
| [0004](0004-in-process-registries-and-relay-forwarding.md) | In-Process Registries and Relay/Orchestration Forwarding | accepted |
| [0005](0005-versioned-config-schema-and-migration-chain.md) | Versioned Config Schema and Migration Chain | accepted |
| [0006](0006-single-binary-and-container-deployment.md) | Two Deployment Forms: Single Binary and Container Image | accepted |
| [0007](0007-internal-verify-token-and-claim-user-endpoints.md) | Internal Endpoints for Yggdrasil-API: verify-token and claim-user | accepted |
| [0008](0008-microservice-extension-layer.md) | Microservice Extension Layer (presence/SDK/route/relay/hook/orchestration/auth levels) | accepted |
| [0009](0009-token-system.md) | Token System and Lifecycles | accepted |
| [0010](0010-data-models.md) | Data Models (Core + Yggdrasil-API) | accepted |
| [0011](0011-bs2ha-migration-contract.md) | BS2HA Migration Contract (BlessingSkin → HRPAuth) | accepted |
| [0012](0012-sdk-package-contract.md) | SDK Package Contract (Static Compile-Time Aggregation) | accepted |

Entries 0008–0012 were migrated back from HA-Contract on 2026-11-07
(source: HA-Contract@bab2a05); each file carries a provenance line.

## Archive

`archive/` holds historical documentation snapshots migrated back from
HA-Contract's `archive/docs/HRPAuth/` (pre-split era docs). Read-only
reference material — not ADR entries, may be stale; current design docs are
the numbered entries above.
