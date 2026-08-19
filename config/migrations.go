package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// ConfigMigration describes one version step: it transforms a config map from
// FromVersion into ToVersion. Migrations run strictly in the order they are
// registered and must be idempotent for the version they target.
type ConfigMigration struct {
	FromVersion string
	ToVersion   string
	Migrate     func(cfg map[string]interface{}, tokenGen func() string) error
}

// configMigrations holds the version chain, ordered from oldest to newest.
//
// History:
//   - "1.0": initial YAML config (memcache-based verification codes, no redis)
//   - "2":   redis added, memcache renamed to verification_code
//   - "3":   security fields (incl. captcha) moved from yggdrasil.security to
//     top-level security, manage.token introduced
//   - "4":   oauth2 section introduced for site-side OAuth2 authorization
func configMigrations() []ConfigMigration {
	return []ConfigMigration{
		{FromVersion: "1.0", ToVersion: "2", Migrate: migrateV1ToV2},
		{FromVersion: "2", ToVersion: "3", Migrate: migrateV2ToV3},
		{FromVersion: "3", ToVersion: "4", Migrate: migrateV3ToV4},
	}
}

// MigrateConfig upgrades cfg step by step until it reaches ConfigVersion.
// It returns the migrated config and whether at least one migration ran.
//
// Behavior:
//   - version equal to ConfigVersion: no-op, changed=false
//   - version newer than ConfigVersion: warns and keeps the file untouched
//     (changed=false) — an older program must not rewrite a newer config
//   - version older than ConfigVersion: runs the chain; an error aborts with
//     the original config left untouched
func MigrateConfig(cfg map[string]interface{}, tokenGen func() string) (map[string]interface{}, bool, error) {
	if tokenGen == nil {
		tokenGen = func() string { return "" }
	}

	current, _ := cfg["version"].(string)
	if current == "" {
		return nil, false, fmt.Errorf("config file is missing the version field; add version: %q or restore a backup", ConfigVersion)
	}

	if current == ConfigVersion {
		return cfg, false, nil
	}

	if VersionMajor(current) > VersionMajor(ConfigVersion) {
		log.Printf("Warning: config file version %q is newer than supported version %q, continuing without migration",
			current, ConfigVersion)
		return cfg, false, nil
	}

	changed := false
	steps := 0
	for current != ConfigVersion {
		step, found := findMigration(current)
		if !found {
			return nil, changed, fmt.Errorf(
				"no migration path from config version %q to %q; restore a backup or upgrade the program", current, ConfigVersion)
		}
		if err := step.Migrate(cfg, tokenGen); err != nil {
			return nil, changed, fmt.Errorf("migration %s->%s failed: %w", step.FromVersion, step.ToVersion, err)
		}
		next, _ := cfg["version"].(string)
		if next == "" || next == current {
			return nil, changed, fmt.Errorf(
				"migration %s->%s did not advance the version field (got %q)", step.FromVersion, step.ToVersion, next)
		}
		current = next
		changed = true
		steps++
		if steps > len(configMigrations())+1 {
			return nil, changed, fmt.Errorf("config migration did not converge")
		}
	}

	return cfg, changed, nil
}

// findMigration returns the registered migration starting from version.
func findMigration(from string) (*ConfigMigration, bool) {
	for i := range configMigrations() {
		if configMigrations()[i].FromVersion == from {
			m := configMigrations()[i]
			return &m, true
		}
	}
	return nil, false
}

// VersionMajor extracts the leading integer of a version string, e.g. "1.0" -> 1,
// "3" -> 3. Unknown or non-numeric versions yield 0.
func VersionMajor(v string) int {
	v = strings.TrimSpace(v)
	i := 0
	for i < len(v) && v[i] >= '0' && v[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0
	}
	n, err := strconv.Atoi(v[:i])
	if err != nil {
		return 0
	}
	return n
}

// BackupConfigFile copies path to path + ".bak." + version before migration.
// An existing backup for the same version is preserved (not overwritten).
func BackupConfigFile(path, version string) error {
	backupPath := fmt.Sprintf("%s.bak.%s", path, version)
	if _, err := os.Stat(backupPath); err == nil {
		log.Printf("Config backup %s already exists, keeping it", backupPath)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file for backup: %w", err)
	}
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config backup %s: %w", backupPath, err)
	}
	log.Printf("Config backed up to %s", backupPath)
	return nil
}

// migrateV1ToV2 migrates the initial config:
//   - memcache section renamed to verification_code (code_ttl / storage_dir kept)
//   - redis section added
func migrateV1ToV2(cfg map[string]interface{}, tokenGen func() string) error {
	if mem, ok := cfg["memcache"].(map[string]interface{}); ok {
		vc := map[string]interface{}{}
		if v, exists := mem["code_ttl"]; exists {
			vc["code_ttl"] = v
		}
		if v, exists := mem["storage_dir"]; exists {
			vc["storage_dir"] = v
		}
		cfg["verification_code"] = vc
		delete(cfg, "memcache")
	}
	if _, ok := cfg["verification_code"].(map[string]interface{}); !ok {
		cfg["verification_code"] = map[string]interface{}{
			"code_ttl":    600,
			"storage_dir": "./cache/verification_codes",
		}
	}
	if _, ok := cfg["redis"].(map[string]interface{}); !ok {
		cfg["redis"] = map[string]interface{}{
			"host":     "127.0.0.1",
			"port":     6379,
			"password": "",
			"db":       0,
			"prefix":   "hrpauth_",
		}
	}
	cfg["version"] = "2"
	return nil
}

// migrateV2ToV3 migrates the v2 config:
//   - password_cost / rate_limit_* / enable_captcha / captcha_ttl move from
//     yggdrasil.security to the top-level security section (v3 schema)
//   - manage.token introduced (generated when missing)
//   - redis.prefix defaulted to "hrpauth_"
func migrateV2ToV3(cfg map[string]interface{}, tokenGen func() string) error {
	ygg, _ := cfg["yggdrasil"].(map[string]interface{})
	var ySec map[string]interface{}
	if ygg != nil {
		ySec, _ = ygg["security"].(map[string]interface{})
	}

	topSec, _ := cfg["security"].(map[string]interface{})
	if topSec == nil {
		topSec = map[string]interface{}{}
	}

	// Fields that belong to the top-level HRPAuth security section in v3.
	movedFields := []string{
		"password_cost",
		"rate_limit_max_attempts",
		"rate_limit_window_sec",
		"enable_captcha",
		"captcha_ttl",
	}
	defaults := map[string]interface{}{
		"password_cost":           10,
		"rate_limit_max_attempts": 10,
		"rate_limit_window_sec":   600,
		"enable_captcha":          true,
		"captcha_ttl":             300,
	}
	for _, key := range movedFields {
		if _, exists := topSec[key]; exists {
			continue
		}
		if ySec != nil {
			if v, exists := ySec[key]; exists {
				topSec[key] = v
				delete(ySec, key)
				continue
			}
		}
		topSec[key] = defaults[key]
	}
	cfg["security"] = topSec

	manage, _ := cfg["manage"].(map[string]interface{})
	if manage == nil {
		manage = map[string]interface{}{}
	}
	if token, _ := manage["token"].(string); token == "" {
		manage["token"] = tokenGen()
	}
	cfg["manage"] = manage

	if redisCfg, ok := cfg["redis"].(map[string]interface{}); ok {
		if prefix, _ := redisCfg["prefix"].(string); prefix == "" {
			redisCfg["prefix"] = "hrpauth_"
		}
	}

	cfg["version"] = "3"
	return nil
}

// migrateV3ToV4 introduces the site-side oauth2 section.
// It creates:
//   - a built-in confidential "super client" for low-friction service-to-service use
//   - a built-in public client for WebUI authorization_code + PKCE
func migrateV3ToV4(cfg map[string]interface{}, tokenGen func() string) error {
	oauth2, _ := cfg["oauth2"].(map[string]interface{})
	if oauth2 == nil {
		oauth2 = map[string]interface{}{}
	}

	callback, _ := cfg["callback"].(map[string]interface{})
	frontend, _ := cfg["frontend"].(map[string]interface{})

	if issuer, _ := oauth2["issuer"].(string); issuer == "" {
		if url, _ := callback["url"].(string); url != "" {
			oauth2["issuer"] = url
		}
	}
	if ttl, ok := oauth2["authorization_code_ttl_sec"]; !ok || ttl == 0 {
		oauth2["authorization_code_ttl_sec"] = 300
	}
	if ttl, ok := oauth2["access_token_ttl_sec"]; !ok || ttl == 0 {
		oauth2["access_token_ttl_sec"] = 3600
	}
	if ttl, ok := oauth2["refresh_token_ttl_sec"]; !ok || ttl == 0 {
		oauth2["refresh_token_ttl_sec"] = 2592000
	}
	if clientID, _ := oauth2["super_client_id"].(string); clientID == "" {
		oauth2["super_client_id"] = "hrpauth-internal-super"
	}
	if secret, _ := oauth2["super_client_secret"].(string); secret == "" {
		oauth2["super_client_secret"] = tokenGen()
	}
	if clientID, _ := oauth2["public_client_id"].(string); clientID == "" {
		oauth2["public_client_id"] = "hrpauth-webui"
	}
	if _, exists := oauth2["public_redirect_uris"]; !exists {
		redirects := []string{}
		if frontendURL, _ := frontend["url"].(string); frontendURL != "" {
			redirects = append(redirects, strings.TrimRight(frontendURL, "/")+"/oauth/callback")
		}
		oauth2["public_redirect_uris"] = redirects
	}

	cfg["oauth2"] = oauth2
	cfg["version"] = "4"
	return nil
}
