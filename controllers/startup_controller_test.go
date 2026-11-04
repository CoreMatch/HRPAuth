package controllers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"gopkg.in/yaml.v3"
)

func TestConfigFileNameConstant(t *testing.T) {
	if ConfigFileName != "config.yaml" {
		t.Fatalf("expected ConfigFileName to be config.yaml, got %q", ConfigFileName)
	}
}

func TestCheckAndMigrateConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	v2Config := `
version: "2"
site:
  name: "Test"
server:
  port: ":2778"
yggdrasil:
  server:
    signature_public_key_path: "public_key.pem"
    signature_private_key_path: "private_key.pem"
  security:
    enable_captcha: true
    captcha_ttl: 60
`
	if err := os.WriteFile(path, []byte(v2Config), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	sc := NewStartupController()
	if err := sc.checkAndMigrateConfig(path); err != nil {
		t.Fatalf("checkAndMigrateConfig failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read migrated config: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse migrated config: %v", err)
	}

	if cfg["version"] != config.ConfigVersion {
		t.Fatalf("expected version %s after migration, got %v", config.ConfigVersion, cfg["version"])
	}
	sec, ok := cfg["security"].(map[string]interface{})
	if !ok {
		t.Fatal("top-level security section missing after migration")
	}
	if sec["enable_captcha"] != true || sec["captcha_ttl"] != 60 {
		t.Errorf("expected captcha fields moved to top-level security, got %v", sec)
	}
	manage, ok := cfg["manage"].(map[string]interface{})
	if !ok {
		t.Fatal("manage section missing after migration")
	}
	if token, _ := manage["token"].(string); len(token) != 64 {
		t.Errorf("expected generated 64-char manage token, got %q", token)
	}
	oauth2, ok := cfg["oauth2"].(map[string]interface{})
	if !ok {
		t.Fatal("oauth2 section missing after migration")
	}
	if oauth2["super_client_id"] != "hrpauth-internal-super" {
		t.Errorf("expected default super_client_id, got %v", oauth2["super_client_id"])
	}

	// Verify Yggdrasil-API section
	yggAPI, ok := cfg["yggdrasil_api"].(map[string]interface{})
	if !ok {
		t.Fatal("yggdrasil_api section missing after migration")
	}
	if yggAPI["base_url"] != "http://localhost:2770" {
		t.Errorf("expected default yggdrasil_api base_url, got %v", yggAPI["base_url"])
	}

	// Verify cleanup
	if _, exists := cfg["yggdrasil"]; exists {
		t.Error("deprecated yggdrasil section still exists")
	}
	if _, exists := cfg["storage"]; exists {
		t.Error("deprecated storage section still exists")
	}
	if _, exists := cfg["keygen"]; exists {
		t.Error("deprecated keygen section still exists")
	}

	// Backup of the original file must exist.
	if _, err := os.Stat(path + ".bak.2"); err != nil {
		t.Errorf("expected backup config.yaml.bak.2: %v", err)
	}
}

func TestBuildDefaultConfigMatchesCurrentSchema(t *testing.T) {
	sc := NewStartupController()
	cfg := sc.buildDefaultConfig()

	if cfg["version"] != config.ConfigVersion {
		t.Fatalf("generated config version = %v, want %v", cfg["version"], config.ConfigVersion)
	}
	// Sections removed by migrations v8/v10/v11 must not be generated anymore.
	for _, removed := range []string{"keygen", "storage", "core_api"} {
		if _, ok := cfg[removed]; ok {
			t.Errorf("generated config still contains removed section %q", removed)
		}
	}
	// Sections introduced up to the current version must be present.
	for _, required := range []string{"sdk_packages", "yggdrasil_api", "database"} {
		if _, ok := cfg[required]; !ok {
			t.Errorf("generated config misses section %q", required)
		}
	}
	if ygg, ok := cfg["yggdrasil_api"].(map[string]interface{}); !ok || ygg["base_url"] == "" {
		t.Errorf("yggdrasil_api.base_url must be generated")
	} else if ygg["base_url"] != "http://localhost:2770" {
		t.Errorf("yggdrasil_api.base_url = %v, want http://localhost:2770", ygg["base_url"])
	} else if _, hasKey := ygg["internal_key"]; hasKey {
		t.Errorf("yggdrasil_api.internal_key was removed in v10 and must not be generated")
	}
	if db, ok := cfg["database"].(map[string]interface{}); !ok || db["port"] == nil {
		t.Errorf("database.port must be generated")
	}

	// Idempotence: an up-to-date generated config must not trigger migrations.
	_, changed, err := config.MigrateConfig(cfg, sc.generateManageToken)
	if err != nil {
		t.Fatalf("MigrateConfig on generated config: %v", err)
	}
	if changed {
		t.Fatal("generated config is already current; migration must be a no-op")
	}

	// The generated map must marshal to valid YAML (round-trip sanity).
	if _, err := yaml.Marshal(cfg); err != nil {
		t.Fatalf("generated config does not marshal: %v", err)
	}
}
