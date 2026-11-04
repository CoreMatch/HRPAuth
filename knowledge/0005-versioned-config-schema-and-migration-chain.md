# ADR-0005: Versioned Config Schema and Migration Chain

- **Status**: accepted
- **Date**: 2026-11-04

## Context

HRPAuth's `config.yaml` schema has changed repeatedly as features were added and
Minecraft concerns were removed. Deployments keep old config files across
upgrades, so the core must accept older schemas and bring them forward without
manual editing, while database schema changes need their own mechanism.

## Decision

Two separate versioning mechanisms exist:

1. **Config file schema** — carries a `version` field (current constant
   `ConfigVersion = "11"`). On startup the core loads the config, checks the
   version, and runs a **stepwise migration chain** (v1→2→…→11) that rewrites
   the file in place after making a backup and writing atomically
   (tmp + rename). A version mismatch warns but does not block startup.
2. **Database schema** — uses golang-migrate with SQL embedded via `embed.FS`,
   applied by an `EnsureMigrations` step triggered after a successful DB
   connect. The migration history was **squashed into a single baseline**
   (`000001_baseline.up.sql`, 7 tables) matching the current core schema, with
   `schema_migrations` normalized back to the standard version layout.

## Consequences

- The config migration chain is retained in code as history (v1→v11), so older
  deployments upgrade automatically; each step records what it added or removed.
- The v7→8 step is where Minecraft/Yggdrasil config keys were dropped, matching
  the split in ADR-0001.
- Cost/limitation: a minor inconsistency remains — the freshly built default
  config still writes `internal_key`, while the v9→10 migration removes that
  key. Full config reference and DB migration rules are documented externally
  (SamuelWiki), not in this repo.
- New databases get all tables in one baseline apply; existing databases rely
  on the `schema_migrations` version to avoid re-running.

## Evidence

- Version constant and load: `config/config.go:144` (`ConfigVersion = "11"`),
  `:148` (`Load()`), `:163-170` (version check warns, does not block),
  `:13-30` (`Config` struct with `Version`).
- Startup migration entry: `main.go:65-68` → `controllers/startup_controller.go:31`
  (`InitializeConfig`) → `:167` (`checkAndMigrateConfig`); backup
  `config/migrations.go:195` (`BackupConfigFile`); atomic write
  `controllers/startup_controller.go:214-220`.
- Migration engine: `config/migrations.go:59` (`MigrateConfig`), refuses
  rewrite when file version is newer `:73-77`; chain registration
  `config/migrations.go:35-48`; individual steps `:215` (v1→2), `:251` (v2→3),
  `:316` (v3→4), `:362` (v4→5), `:381` (v5→6), `:397` (v6→7), `:412` (v7→8),
  `:137` (v8→9 sdk_packages), `:157` (v9→10 drop internal_key), `:172`
  (v10→11 DB retry params).
- DB migration mechanism: `database/migrations/migrations.go:5-6` (embedded FS);
  `controllers/startup_controller.go:236-287` (`EnsureMigrations`), `:289-346`
  (`schema_migrations` handling); trigger on connect `main.go:73-77`.
- Baseline squash: commit `b8a745c` ("Collapse the migration chain into a
  single baseline…"); single baseline file
  `database/migrations/000001_baseline.up.sql` (7 tables: users,
  oauth2_clients, oauth2_authorization_codes, oauth2_access_tokens,
  oauth2_refresh_tokens, deleted_accounts, webauthn_credentials).
- Commits: `8295bb4` (v11 + DB retries), `087030f` (v7→8), `7c11dd2` (v9),
  `1545da2` (v10), `b8a745c` (baseline squash).
- Not verified: the exact introducing commit for each of the v2→v7 steps (the
  migration framework itself is attributed to `c0f8cc5`); whether the baseline
  fully absorbed the removed `000012_remove_minecraft_logic` columns.
