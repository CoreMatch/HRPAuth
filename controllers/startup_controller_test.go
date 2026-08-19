package controllers

import (
	"os"
	"path/filepath"
	"testing"

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

	if cfg["version"] != "4" {
		t.Fatalf("expected version 4 after migration, got %v", cfg["version"])
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

	// Backup of the original file must exist.
	if _, err := os.Stat(path + ".bak.2"); err != nil {
		t.Errorf("expected backup config.yaml.bak.2: %v", err)
	}
}
