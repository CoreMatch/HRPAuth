> 来源：HA-Contract@bab2a05（2026-11-07），原路径 docs/dev/HRPAuth/bs2ha-migration.md（契约仓知识库回迁）。

# BS2HA Migration Contract

This document defines the intended contract for the offline migration path from **BlessingSkin** data to **HRPAuth** data.

The migration tool is expected to consume a BlessingSkin SQL dump and emit HRPAuth import artifacts. To ensure compatibility with the target HRPAuth database, the tool MAY utilize a baseline HA SQL template to wrap the imported data.

## Scope

The first supported migration path is:

- Input:
  - a BlessingSkin MySQL/MariaDB SQL dump
  - the BlessingSkin `textures/` storage directory (optional; skip texture migration if missing)
  - HRPAuth callback base URL
  - HRPAuth Yggdrasil signing private key (optional; defaults to auto-generating a new key)
  - a baseline HRPAuth core SQL file (`--ha-baseline-sql`, optional; provides table structure and constraints)
  - a baseline HRPAuth-Yggdrasil-API SQL file (`--ygg-baseline-sql`, optional)
- Output:
  - an HRPAuth **core** SQL import file (`--ha-out-sql`; `users` rows)
  - an HRPAuth **Yggdrasil-API** SQL import file (`--ygg-out-sql`; `accounts`, `profiles`, `profile_properties` rows)
  - a migration report describing transformed rows, skipped rows, generated placeholders, and texture/signature status

The tool is offline and file-based. It does not depend on direct access to the source BlessingSkin database.

## Table Coverage

Following the 2026-10 service split, the migration tool writes to two
databases:

- HRPAuth core: `users`
- HRPAuth-Yggdrasil-API: `accounts`, `profiles`, `profile_properties`

The tool does **not** import runtime-only tables such as:

- `sessions`
- `tokens`
- `oauth2_clients`
- `oauth2_access_tokens`
- `oauth2_refresh_tokens`
- `oauth2_authorization_codes`

These tables must start empty or be managed by HRPAuth itself after deployment.

## User Mapping

BlessingSkin `users.uid` is preserved as HRPAuth `users.uid`.

For each imported user:

- `users.uuid` MUST be newly generated as a 32-character lowercase hex string.
- `email`, `password`, `permission`, `ip`, `last_sign_at`, `register_at`, and `verified` are migrated when available.
- `regip` SHOULD default to the BlessingSkin `ip` value when no better registration IP source exists.
- In the Yggdrasil-API `accounts` row created for each imported user: `cbh` MUST be `1`, `mbe` MUST default to `0`, and `mojang_uuid` remains unset.
- `totp` and `2FA` MUST remain unset unless a later migration phase explicitly provides them.

## Imported Username

HRPAuth `users.username` MUST be imported directly from the canonical BlessingSkin username value carried by the source data.

The migration tool MUST treat the BlessingSkin username as authoritative identity data rather than deriving it from unrelated fields such as nickname or email.

For the currently supported BlessingSkin SQL dump format, the canonical BlessingSkin username is sourced from `users.nickname`.

Rules:

1. `users.username` in HRPAuth MUST equal the username value imported from BlessingSkin.
2. If the source dump does not provide a recoverable canonical BlessingSkin username for a user, the migration MUST fail that row and record it in the report.
3. The tool MUST NOT silently synthesize replacement usernames from `email`, `uid`, or any non-canonical fallback.

## Profile Selection Rules

BlessingSkin `players` are mapped into HRPAuth `profiles`, but the migration intentionally narrows the imported profile set.

For each BlessingSkin user:

1. If the user has exactly one player, import that player as the only HRPAuth profile.
2. If the user has multiple players, keep **only** the player whose `players.name` equals the imported BlessingSkin username.
3. If the user has multiple players and no player name matches the imported BlessingSkin username, treat the user as having no usable player.
4. If the user has no usable player after the above rules, create a placeholder profile whose name is the imported BlessingSkin username.

Additional rules:

- Imported or generated profile IDs MUST be newly generated 32-character lowercase hex strings.
- Imported or generated profile names MUST satisfy HRPAuth username/profile validation rules.
- The report MUST record whether the profile was imported from BlessingSkin or auto-generated as a placeholder.
- Non-selected BlessingSkin players MUST be listed in the migration report as dropped by rule, not silently ignored.

## Texture Mapping

The migration tool MUST support full texture cut-over by copying or reusing BlessingSkin texture files from the provided `textures/` directory.

Rules:

- BlessingSkin texture hashes are treated as the canonical texture file names.
- Imported profile textures MUST be emitted as `profile_properties.name = 'textures'`.
- `profile_properties.value` MUST contain the base64-encoded Yggdrasil textures payload expected by HRPAuth.
- Texture URLs inside the payload MUST point to the target HRPAuth callback domain, not the legacy BlessingSkin domain.
- `uploadableTextures` is runtime-derived by HRPAuth and does not need to be pre-inserted by the migration tool.

### Texture Signature Requirement

For production imports, the migration tool MUST sign imported `textures` properties using an HRPAuth Yggdrasil private key so that signed profile responses remain valid immediately after cut-over.

The tool MUST support the following key sources:
1. **Existing Key**: A file path to an existing PEM-encoded private key.
2. **Pasted Content**: Raw PEM-encoded private key content provided via CLI/TUI.
3. **Auto-generation**: If no key is provided and unsigned mode is not explicitly enabled, the tool MUST auto-generate a new RSA 4096-bit private key and save it for future use by the HRPAuth server.

If the signing key is absent and auto-generation is disabled, the tool MAY support an explicit non-production unsigned mode, but:

- the output report MUST mark the import as unsigned
- the unsigned mode MUST be opt-in
- unsigned output MUST NOT be the default contract for production cut-over

## Model Mapping

BlessingSkin texture model types map to HRPAuth profile model values as follows:

- `steve` -> `default`
- `alex` -> `slim`
- missing skin -> `default`

Cape data is represented only inside the `textures` profile property payload and does not affect `profiles.model`.

## Password Migration Contract

The migration tool MUST determine the BlessingSkin password method from the source SQL input.

Rules:

- If the source password method is `BCRYPT`, the existing BlessingSkin `users.password` hash MUST be copied directly into HRPAuth `users.password`.
- If the source password method is anything other than `BCRYPT`, the migration tool MUST NOT attempt legacy-password compatibility verification.
- For non-`BCRYPT` imports, HRPAuth `users.password` MUST be written as a fixed invalid marker that forces the user into the password reset flow.

The fixed invalid marker for non-`BCRYPT` imports is:

- `BS2HA$RESET_REQUIRED`

Operational consequences:

- Native HRPAuth password generation remains bcrypt.
- Imported non-`BCRYPT` users are expected to regain access through password reset, not direct password reuse.
- HRPAuth MAY detect the fixed marker to present a clearer reset-required message, but multi-verifier login support is not part of this migration contract.

## Output Ordering

The generated SQL MUST insert data in dependency order:

1. `users`
2. `profiles`
3. `profile_properties`

The SQL MUST be deterministic for the same input set and generation options.

## Reporting Requirements

The migration report MUST include at least:

- total imported users
- total imported profiles
- total auto-generated placeholder profiles
- total dropped BlessingSkin players
- total imported texture payloads
- total unsigned texture payloads, if any
- total password hashes copied directly from BlessingSkin bcrypt
- total users forced into password reset
- rows skipped because of unrecoverable conflicts or invalid source data

## Follow-up Expectations

This migration contract requires follow-up implementation work in:

- `HRPAuth`: optional reset-required UX for the fixed invalid password marker
- `BS2HA`: dump parser, mapper, password-method inspection, texture copier, SQL emitter, and reporting
