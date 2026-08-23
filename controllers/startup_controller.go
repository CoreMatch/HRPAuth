package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

func (sc *StartupController) buildDefaultConfig(publicKeyPath, privateKeyPath string) map[string]interface{} {
	frontendURL := "https://auth.mcnb.dev/"
	return map[string]interface{}{
		"version": config.ConfigVersion,
		"site": map[string]interface{}{
			"name":           "HRPAuth",
			"implementation": "HRPAuth zggdrasil-api service",
			"version":        "62526",
		},
		"server": map[string]interface{}{
			"port":        ":2778",
			"cors_origin": "*",
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
		"oauth2": map[string]interface{}{
			"issuer":                     "https://ha.mcnb.dev/",
			"authorization_code_ttl_sec": 300,
			"access_token_ttl_sec":       3600,
			"refresh_token_ttl_sec":      2592000,
			"super_client_id":            "hrpauth-internal-super",
			"super_client_secret":        sc.generateManageToken(),
			"public_client_id":           "hrpauth-webui",
			"public_redirect_uris": []string{
				frontendURL + "oauth/callback",
			},
		},
		"yggdrasil": map[string]interface{}{
			"server": map[string]interface{}{
				"name":                       "HRPAuth",
				"implementation":             "HRPAuth zggdrasil-api service",
				"version":                    "5526",
				"signature_public_key_path":  publicKeyPath,
				"signature_private_key_path": privateKeyPath,
				"textures_storage":           "./",
				"links": map[string]interface{}{
					"homepage": "",
					"register": "",
				},
				"skin_domains": []string{},
			},
			"security": map[string]interface{}{
				"token_expiry_days":      15,
				"session_expiry_seconds": 28800,
				"max_texture_width":      1024,
				"max_texture_height":     1024,
			},
			"feature_flags": map[string]interface{}{
				"non_email_login":             true,
				"legacy_skin_api":             true,
				"no_mojang_namespace":         false,
				"enable_mojang_anti_features": false,
				"enable_profile_key":          false,
				"username_check":              true,
			},
		},
	}
}

func (sc *StartupController) createDefaultConfig(path string) error {
	cfgDir := filepath.Dir(path)

	publicKeyPath := filepath.Join(cfgDir, "public_key.pem")
	privateKeyPath := filepath.Join(cfgDir, "private_key.pem")

	if err := sc.generateKeyPair(publicKeyPath, privateKeyPath); err != nil {
		log.Printf("Warning: Failed to generate RSA key pair: %v", err)
		log.Printf("Falling back to pseudo-random keys...")
		if err := sc.generatePseudoKeys(publicKeyPath, privateKeyPath); err != nil {
			log.Printf("Warning: Failed to generate pseudo keys: %v", err)
		}
	}

	defaultConfig := sc.buildDefaultConfig(publicKeyPath, privateKeyPath)

	data, err := yaml.Marshal(defaultConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal default config: %v", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	log.Printf("Default config file created at %s", path)
	log.Printf("Key pair generated at %s and %s", publicKeyPath, privateKeyPath)
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

	// Preserve existing key paths if present; otherwise generate a new key pair
	// and record the paths in the migrated config.
	cfgDir := filepath.Dir(path)
	publicKeyPath := filepath.Join(cfgDir, "public_key.pem")
	privateKeyPath := filepath.Join(cfgDir, "private_key.pem")

	existingPubPath, existingPrivPath := sc.getExistingKeyPaths(migrated)
	if existingPubPath != "" && existingPrivPath != "" {
		publicKeyPath = existingPubPath
		privateKeyPath = existingPrivPath
	} else {
		log.Printf("Signature key paths missing in config, generating new key pair...")
		if err := sc.generateKeyPair(publicKeyPath, privateKeyPath); err != nil {
			log.Printf("Warning: Failed to generate RSA key pair: %v", err)
			log.Printf("Falling back to pseudo-random keys...")
			if err := sc.generatePseudoKeys(publicKeyPath, privateKeyPath); err != nil {
				log.Printf("Warning: Failed to generate pseudo keys: %v", err)
			}
		}
		if ygg, ok := migrated["yggdrasil"].(map[string]interface{}); ok {
			if serverCfg, ok := ygg["server"].(map[string]interface{}); ok {
				serverCfg["signature_public_key_path"] = publicKeyPath
				serverCfg["signature_private_key_path"] = privateKeyPath
			}
		}
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

// getExistingKeyPaths extracts the signature key paths from a raw config map.
func (sc *StartupController) getExistingKeyPaths(cfg map[string]interface{}) (string, string) {
	yggdrasil, _ := cfg["yggdrasil"].(map[string]interface{})
	serverCfg, _ := yggdrasil["server"].(map[string]interface{})
	pubPath, _ := serverCfg["signature_public_key_path"].(string)
	privPath, _ := serverCfg["signature_private_key_path"].(string)
	return pubPath, privPath
}

func (sc *StartupController) generateKeyPair(publicKeyPath, privateKeyPath string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("failed to generate RSA private key: %v", err)
	}

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key file: %v", err)
	}

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %v", err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	if err := os.WriteFile(publicKeyPath, publicKeyPEM, 0644); err != nil {
		return fmt.Errorf("failed to write public key file: %v", err)
	}

	return nil
}

func (sc *StartupController) generatePseudoKeys(publicKeyPath, privateKeyPath string) error {
	publicPseudo := sc.generateRandomString(512)
	privatePseudo := sc.generateRandomString(1024)

	publicKeyContent := fmt.Sprintf("-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----\n", publicPseudo)
	privateKeyContent := fmt.Sprintf("-----BEGIN RSA PRIVATE KEY-----\n%s\n-----END RSA PRIVATE KEY-----\n", privatePseudo)

	if err := os.WriteFile(publicKeyPath, []byte(publicKeyContent), 0644); err != nil {
		return fmt.Errorf("failed to write pseudo public key file: %v", err)
	}
	if err := os.WriteFile(privateKeyPath, []byte(privateKeyContent), 0600); err != nil {
		return fmt.Errorf("failed to write pseudo private key file: %v", err)
	}

	return nil
}

// generateManageToken produces a random 32-byte (64 hex chars) Manage Token.
// It is generated once at config-file creation time and persisted to
// config.yaml under `manage.token`.
func (sc *StartupController) generateManageToken() string {
	return utils.GenerateRandomToken(32)
}

func (sc *StartupController) generateRandomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
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

	driver, err := mysqldriver.WithInstance(db, &mysqldriver.Config{
		MigrationsTable: "schema_migrations_ha",
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
		return fmt.Errorf("failed to run migrations: %v", err)
	}

	version, dirty, _ := m.Version()
	log.Printf("Database migration completed at version %d (dirty: %t)", version, dirty)
	return nil
}
