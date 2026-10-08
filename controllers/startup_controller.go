package controllers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	mysqldriver "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database/migrations"
	"github.com/lnb/HRPAuth-Backend-Go/utils"

	"gopkg.in/yaml.v3"
)

type StartupController struct{}

const ConfigFileName = "config.yaml"
const schemaMigrationService = "HA"

func NewStartupController() *StartupController {
	return &StartupController{}
}

func (sc *StartupController) InitializeConfig() error {
	configPath := filepath.Join(config.ConfigFileDir, config.ConfigFileName)

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("Config file not found at %s, creating default config...", configPath)
		return sc.createDefaultConfig(configPath)
	}

	log.Printf("Config file found at %s", configPath)

	// Check config file version and migrate if necessary
	if err := sc.checkAndMigrateConfig(configPath); err != nil {
		return fmt.Errorf("failed to check/migrate config: %v", err)
	}

	return nil
}

func (sc *StartupController) buildDefaultConfig() map[string]interface{} {
	frontendURL := "https://auth.mcnb.dev/"
	return map[string]interface{}{
		"version": config.ConfigVersion,
		"site": map[string]interface{}{
			"name":           "HRPAuth",
			"implementation": "HRPAuth core service",
			"version":        "62526",
		},
		"server": map[string]interface{}{
			"port":        ":2778",
			"cors_origin": "",
		},
		"callback": map[string]interface{}{
			"url": "https://ha.mcnb.dev/",
		},
		"frontend": map[string]interface{}{
			"url": frontendURL,
		},
		"keygen": map[string]interface{}{
			"enable": 0,
		},
		"database": map[string]interface{}{
			"host":     "127.0.0.1",
			"db_name":  "hrpa",
			"user":     "hrpa",
			"password": "hrpa",
			"charset":  "utf8mb4",
		},
		"verification_code": map[string]interface{}{
			"code_ttl":    600,
			"storage_dir": "./cache/verification_codes",
		},
		"redis": map[string]interface{}{
			"host":     "127.0.0.1",
			"port":     6379,
			"password": "",
			"db":       0,
			"prefix":   "hrpauth_",
		},
		"smtp": map[string]interface{}{
			"host":       "127.0.0.1",
			"port":       25,
			"username":   "",
			"password":   "",
			"encryption": "tls",
			"from_email": "no-reply@mcnb.dev",
			"from_name":  "HRPAuth",
		},
		"manage": map[string]interface{}{
			"token": sc.generateManageToken(),
		},
		"security": map[string]interface{}{
			"password_cost":           10,
			"rate_limit_max_attempts": 10,
			"rate_limit_window_sec":   600,
			"enable_captcha":          true,
			"captcha_ttl":             300,
		},
		"webauthn": map[string]interface{}{
			"rp_display_name": "HRPAuth",
			"rp_id":           "auth.mcnb.dev",
			"rp_origins": []string{
				strings.TrimRight(frontendURL, "/"),
				"https://ha.mcnb.dev",
			},
			"session_ttl_sec": 300,
		},
		"oauth2": map[string]interface{}{
			"issuer":                     "https://ha.mcnb.dev/",
			"authorization_code_ttl_sec": 300,
			"access_token_ttl_sec":       3600,
			"refresh_token_ttl_sec":      2592000,
			"super_client_id":            "hrpauth-internal-super",
			"super_client_secret":        sc.generateManageToken(),
			"super_client_extra_scopes":  []string{},
			"public_client_id":           "hrpauth-webui",
			"public_redirect_uris": []string{
				frontendURL + "oauth/callback",
			},
		},
		"core_api": map[string]interface{}{
			"internal_key": utils.GenerateRandomToken(32),
		},
		"yggdrasil_api": map[string]interface{}{
                        "base_url":     "http://localhost:2770",
			"internal_key": utils.GenerateRandomToken(32),
		},
		"storage": map[string]interface{}{
			"orphan_file_expiry_days": 7,
		},
	}
}

func (sc *StartupController) createDefaultConfig(path string) error {
	defaultConfig := sc.buildDefaultConfig()

	data, err := yaml.Marshal(defaultConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal default config: %v", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	log.Printf("Default config file created at %s", path)
	log.Printf("Please edit the configuration file and restart the application")
	return nil
}

// checkAndMigrateConfig checks the config file's version and upgrades it step
// by step (chain migration, see config.MigrateConfig). The original file is
// backed up before any migration and is left untouched on failure.
func (sc *StartupController) checkAndMigrateConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	var currentConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &currentConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	currentVersion, _ := currentConfig["version"].(string)
	if currentVersion == config.ConfigVersion {
		log.Printf("Config file version %s is up-to-date", currentVersion)
		return nil
	}

	// A config file newer than the program supports must not be rewritten:
	// an older program cannot safely migrate a newer schema.
	if config.VersionMajor(currentVersion) > config.VersionMajor(config.ConfigVersion) {
		log.Printf("Warning: config file version %q is newer than supported version %q, continuing without migration",
			currentVersion, config.ConfigVersion)
		return nil
	}

	log.Printf("Config file version %q is older than %q, migrating...", currentVersion, config.ConfigVersion)

	// Backup the original file before any migration.
	if err := config.BackupConfigFile(path, currentVersion); err != nil {
		return fmt.Errorf("failed to backup config file: %v", err)
	}

	migrated, changed, err := config.MigrateConfig(currentConfig, sc.generateManageToken)
	if err != nil {
		return fmt.Errorf("failed to migrate config: %v", err)
	}
	if !changed {
		return nil
	}

	data, err = yaml.Marshal(migrated)
	if err != nil {
		return fmt.Errorf("failed to marshal migrated config: %v", err)
	}

	// Atomic write: write a temp file first, then rename, so a crash never
	// leaves a half-written config.yaml behind.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write migrated config: %v", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace config file: %v", err)
	}

	log.Printf("Config file migrated to version %s at %s", config.ConfigVersion, path)
	return nil
}

// generateManageToken produces a random 32-byte (64 hex chars) Manage Token.
// It is generated once at config-file creation time and persisted to
// config.yaml under `manage.token`.
func (sc *StartupController) generateManageToken() string {
	return utils.GenerateRandomToken(32)
}

// EnsureMigrations runs all pending database migrations via golang-migrate.
// It is idempotent — if the database is already at the latest version,
// migrate.ErrNoChange is silently ignored.
func (sc *StartupController) EnsureMigrations() error {
	cfg := config.AppConfig.Database
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s)/%s?charset=%s&parseTime=True&loc=Local&multiStatements=true",
		cfg.User, cfg.Password, cfg.Host, cfg.DBName, cfg.Charset,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database for migration check: %v", err)
	}
	defer db.Close()

	if bootstrapErr := sc.ensureSchemaMigrationTable(db); bootstrapErr != nil {
		return bootstrapErr
	}

	driver, err := mysqldriver.WithInstance(db, &mysqldriver.Config{
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %v", err)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("failed to create migration source from embedded files: %v", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, cfg.DBName, driver)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %v", err)
	}
	defer func() {
		_, dbErr := m.Close()
		if dbErr != nil {
			log.Printf("warning: migration close error: %v", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			return fmt.Errorf("failed to run migrations: database is dirty at version %d; fix the migration state and force version before restarting", dirtyErr.Version)
		}
		return fmt.Errorf("failed to run migrations: %v", err)
	}

	if err := sc.ensureSchemaMigrationServiceColumn(db); err != nil {
		return err
	}

	version, dirty, _ := m.Version()
	log.Printf("Database migration completed at version %d (dirty: %t)", version, dirty)
	return nil
}

func (sc *StartupController) ensureSchemaMigrationTable(db *sql.DB) error {
	query := "CREATE TABLE IF NOT EXISTS `schema_migrations` (" +
		"`version` bigint NOT NULL," +
		"`dirty` boolean NOT NULL," +
		"`service` varchar(16) NOT NULL DEFAULT '" + schemaMigrationService + "'," +
		"PRIMARY KEY (`service`)" +
		")"
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %v", err)
	}
	return nil
}

func (sc *StartupController) ensureSchemaMigrationServiceColumn(db *sql.DB) error {
	const tableExistsQuery = `
			SELECT COUNT(*)
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = DATABASE()
				AND TABLE_NAME = 'schema_migrations'
	`
	const columnExistsQuery = `
			SELECT COUNT(*)
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE()
				AND TABLE_NAME = 'schema_migrations'
				AND COLUMN_NAME = 'service'
	`

	var tableCount int
	if err := db.QueryRow(tableExistsQuery).Scan(&tableCount); err != nil {
		return fmt.Errorf("failed to check schema_migrations table: %v", err)
	}
	if tableCount == 0 {
		return nil
	}

	var columnCount int
	if err := db.QueryRow(columnExistsQuery).Scan(&columnCount); err != nil {
		return fmt.Errorf("failed to check schema_migrations service column: %v", err)
	}

	if columnCount == 0 {
		query := "ALTER TABLE `schema_migrations` ADD COLUMN `service` varchar(16) NOT NULL DEFAULT '" + schemaMigrationService + "' AFTER `dirty`"
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to add schema_migrations service column: %v", err)
		}
	}

	// Backfill any rows that don't have a service value.
	if _, err := db.Exec("UPDATE `schema_migrations` SET `service` = '" + schemaMigrationService + "' WHERE `service` IS NULL OR `service` = ''"); err != nil {
		return fmt.Errorf("failed to backfill schema_migrations service: %v", err)
	}

	// Keep the real table shape: PRIMARY KEY (`service`) only.
	pkQuery := `
		SELECT COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = 'schema_migrations'
			AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION
	`
	rows, err := db.Query(pkQuery)
	if err != nil {
		return fmt.Errorf("failed to check schema_migrations primary key: %v", err)
	}
	defer rows.Close()

	var pkColumns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return fmt.Errorf("failed to read schema_migrations primary key: %v", err)
		}
		pkColumns = append(pkColumns, column)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate schema_migrations primary key: %v", err)
	}

	if len(pkColumns) == 1 && pkColumns[0] == "service" {
		return nil
	}
	if len(pkColumns) > 0 {
		if _, err := db.Exec("ALTER TABLE `schema_migrations` DROP PRIMARY KEY"); err != nil {
			return fmt.Errorf("failed to drop schema_migrations primary key: %v", err)
		}
	}
	if _, err := db.Exec("ALTER TABLE `schema_migrations` ADD PRIMARY KEY (`service`)"); err != nil {
		return fmt.Errorf("failed to add schema_migrations primary key: %v", err)
	}

	return nil
}
