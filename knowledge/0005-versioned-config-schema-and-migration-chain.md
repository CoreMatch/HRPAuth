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
   `ConfigVersion = "12"`). On startup the core loads the config, checks the
   version, and runs a **stepwise migration chain** (v1→2→…→12) that rewrites
   the file in place after making a backup and writing atomically
   (tmp + rename). A version mismatch warns but does not block startup.
2. **Database schema** — uses golang-migrate with SQL embedded via `embed.FS`,
   applied by an `EnsureMigrations` step triggered after a successful DB
   connect. The migration history was **squashed into a single baseline**
   (`000001_baseline.up.sql`, 7 tables) matching the current core schema, with
   `schema_migrations` normalized back to the standard version layout.

## Consequences

- The config migration chain is retained in code as history (v1→v12), so older
  deployments upgrade automatically; each step records what it added or removed.
- The v7→8 step is where Minecraft/Yggdrasil config keys were dropped, matching
  the split in ADR-0001.
- Cost/limitation: full config reference and DB migration rules are documented
  externally (SamuelWiki), not in this repo. The former inconsistency — the
  freshly built default config writing `internal_key` while the v9→10 migration
  removes that key — no longer exists: commit `7fd591d` rewrote the default
  generator (`controllers/startup_controller.go::buildDefaultConfig`) to emit
  the current schema directly.
- New databases get all tables in one baseline apply; existing databases rely
  on the `schema_migrations` version to avoid re-running.

## Evidence

- Version constant and load: `config/config.go::ConfigVersion`
  (`ConfigVersion = "12"`), `config/config.go::Load` (`Load()`; version check
  warns, does not block), `config/config.go::Config` (`Config` struct with
  `Version`).
- Startup migration entry: `main.go::InitializeConfig` →
  `controllers/startup_controller.go::InitializeConfig` →
  `controllers/startup_controller.go::checkAndMigrateConfig`; backup
  `config/migrations.go::BackupConfigFile`; atomic write (tmp + rename) inside
  `controllers/startup_controller.go::checkAndMigrateConfig`.
- Migration engine: `config/migrations.go::MigrateConfig` (refuses rewrite
  when file version is newer); chain registration
  `config/migrations.go::configMigrations`; individual steps
  `config/migrations.go::migrateV1ToV2` (v1→2),
  `config/migrations.go::migrateV2ToV3` (v2→3),
  `config/migrations.go::migrateV3ToV4` (v3→4),
  `config/migrations.go::migrateV4ToV5` (v4→5),
  `config/migrations.go::migrateV5ToV6` (v5→6),
  `config/migrations.go::migrateV6ToV7` (v6→7),
  `config/migrations.go::migrateV7ToV8` (v7→8),
  `config/migrations.go::migrateV8ToV9` (v8→9 sdk_packages),
  `config/migrations.go::migrateV9ToV10` (v9→10 drop internal_key),
  `config/migrations.go::migrateV10ToV11` (v10→11 DB retry params),
  `config/migrations.go::migrateV11ToV12` (v11→12 optional database.port).
- DB migration mechanism: `database/migrations/migrations.go::FS` (embedded
  FS); `controllers/startup_controller.go::EnsureMigrations`,
  `controllers/startup_controller.go::ensureSchemaMigrationTable` /
  `controllers/startup_controller.go::normalizeSchemaMigrationsTable`
  (`schema_migrations` handling); trigger on connect
  `main.go::RegisterOnConnect`.
- Baseline squash: commit `b8a745c` ("Collapse the migration chain into a
  single baseline…"); single baseline file
  `database/migrations/000001_baseline.up.sql` (7 tables: users,
  oauth2_clients, oauth2_authorization_codes, oauth2_access_tokens,
  oauth2_refresh_tokens, deleted_accounts, webauthn_credentials).
- Commits: `8295bb4` (v11 + DB retries), `8c96c60` (v12 optional
  `database.port`), `087030f` (v7→8), `7c11dd2` (v9),
  `1545da2` (v10), `b8a745c` (baseline squash), `7fd591d` (default generator
  emits the current schema).
- Not verified: the exact introducing commit for each of the v2→v7 steps (the
  migration framework itself is attributed to `c0f8cc5`); whether the baseline
  fully absorbed the removed `000012_remove_minecraft_logic` columns.
