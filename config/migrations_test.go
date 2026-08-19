package config

import "testing"

func tokenGen() string { return "test-manage-token" }

func TestVersionMajor(t *testing.T) {
	cases := map[string]int{
		"1.0": 1,
		"2":   2,
		"3":   3,
		"4":   4,
		"10":  10,
		"":    0,
		"abc": 0,
	}
	for in, want := range cases {
		if got := VersionMajor(in); got != want {
			t.Errorf("VersionMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestMigrateConfigUpToDate(t *testing.T) {
	cfg := map[string]interface{}{"version": ConfigVersion}
	out, changed, err := MigrateConfig(cfg, tokenGen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected no migration for up-to-date config")
	}
	if out["version"] != cfg["version"] {
		t.Fatal("expected the same map back for up-to-date config")
	}
}

func TestMigrateConfigFutureVersion(t *testing.T) {
	cfg := map[string]interface{}{"version": "5", "site": map[string]interface{}{"name": "future"}}
	out, changed, err := MigrateConfig(cfg, tokenGen)
	if err != nil {
		t.Fatalf("future version must warn and continue, got error: %v", err)
	}
	if changed {
		t.Fatal("expected no migration for a newer config")
	}
	if out["version"] != "5" {
		t.Fatalf("expected untouched version, got %v", out["version"])
	}
}

func TestMigrateConfigMissingVersion(t *testing.T) {
	cfg := map[string]interface{}{"site": map[string]interface{}{}}
	_, _, err := MigrateConfig(cfg, tokenGen)
	if err == nil {
		t.Fatal("expected error for config missing version field")
	}
}

func TestMigrateConfigUnknownOldVersion(t *testing.T) {
	cfg := map[string]interface{}{"version": "1.5"}
	_, _, err := MigrateConfig(cfg, tokenGen)
	if err == nil {
		t.Fatal("expected error when no migration path exists")
	}
}

func TestMigrateConfigV1Chain(t *testing.T) {
	cfg := map[string]interface{}{
		"version": "1.0",
		"memcache": map[string]interface{}{
			"host":        "127.0.0.1",
			"port":        11211,
			"prefix":      "hrpauth_",
			"code_ttl":    123,
			"storage_dir": "./cache/vcodes",
		},
		"yggdrasil": map[string]interface{}{
			"security": map[string]interface{}{
				"password_cost":           12,
				"rate_limit_max_attempts": 5,
				"enable_captcha":          false,
				"captcha_ttl":             99,
				"token_expiry_days":       7,
			},
		},
	}

	out, changed, err := MigrateConfig(cfg, tokenGen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected chain migration to run")
	}
	if out["version"] != ConfigVersion {
		t.Fatalf("expected final version %q, got %v", ConfigVersion, out["version"])
	}

	// memcache renamed to verification_code
	if _, ok := out["memcache"]; ok {
		t.Fatal("memcache section should have been removed")
	}
	vc, ok := out["verification_code"].(map[string]interface{})
	if !ok {
		t.Fatal("verification_code section missing after v1->v2")
	}
	if vc["code_ttl"] != 123 {
		t.Errorf("expected code_ttl 123 preserved, got %v", vc["code_ttl"])
	}

	// redis added with default prefix
	redisCfg, ok := out["redis"].(map[string]interface{})
	if !ok {
		t.Fatal("redis section missing after v1->v2")
	}
	if redisCfg["prefix"] != "hrpauth_" {
		t.Errorf("expected redis prefix default, got %v", redisCfg["prefix"])
	}

	// security moved to top level, manage token generated
	sec, ok := out["security"].(map[string]interface{})
	if !ok {
		t.Fatal("top-level security section missing after v2->v3")
	}
	if sec["password_cost"] != 12 {
		t.Errorf("expected password_cost 12 preserved, got %v", sec["password_cost"])
	}
	if sec["enable_captcha"] != false {
		t.Errorf("expected enable_captcha false preserved, got %v", sec["enable_captcha"])
	}
	manage, ok := out["manage"].(map[string]interface{})
	if !ok || manage["token"] != "test-manage-token" {
		t.Fatalf("expected generated manage token, got %v", out["manage"])
	}
		oauth2, ok := out["oauth2"].(map[string]interface{})
		if !ok {
			t.Fatal("oauth2 section missing after v3->v4")
		}
		if oauth2["super_client_id"] != "hrpauth-internal-super" {
			t.Errorf("expected default super_client_id, got %v", oauth2["super_client_id"])
		}

	// yggdrasil.security keeps only its own fields
	ygg := out["yggdrasil"].(map[string]interface{})
	ySec := ygg["security"].(map[string]interface{})
	if _, ok := ySec["enable_captcha"]; ok {
		t.Error("enable_captcha should have been removed from yggdrasil.security")
	}
	if ySec["token_expiry_days"] != 7 {
		t.Errorf("expected yggdrasil.security.token_expiry_days preserved, got %v", ySec["token_expiry_days"])
	}
}

func TestMigrateConfigV2ToV3(t *testing.T) {
	cfg := map[string]interface{}{
		"version": "2",
		"yggdrasil": map[string]interface{}{
			"security": map[string]interface{}{
				"enable_captcha":  true,
				"captcha_ttl":     60,
				"token_expiry_days": 15,
			},
		},
		"redis": map[string]interface{}{
			"host": "1.2.3.4",
			"port": 6380,
		},
	}

	out, changed, err := MigrateConfig(cfg, tokenGen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to run")
	}

	sec := out["security"].(map[string]interface{})
	if sec["enable_captcha"] != true || sec["captcha_ttl"] != 60 {
		t.Errorf("expected captcha fields moved to top-level security, got %v", sec)
	}

	ygg := out["yggdrasil"].(map[string]interface{})
	ySec := ygg["security"].(map[string]interface{})
	if _, ok := ySec["enable_captcha"]; ok {
		t.Error("enable_captcha should have been removed from yggdrasil.security")
	}

	redisCfg := out["redis"].(map[string]interface{})
	if redisCfg["prefix"] != "hrpauth_" {
		t.Errorf("expected redis prefix default, got %v", redisCfg["prefix"])
	}

	manage := out["manage"].(map[string]interface{})
	if manage["token"] != "test-manage-token" {
		t.Errorf("expected generated manage token, got %v", manage)
	}
		oauth2 := out["oauth2"].(map[string]interface{})
		if oauth2["public_client_id"] != "hrpauth-webui" {
			t.Errorf("expected public_client_id default, got %v", oauth2["public_client_id"])
		}
}

func TestMigrateV2ToV3PreservesExistingTopLevelSecurity(t *testing.T) {
	cfg := map[string]interface{}{
		"version": "2",
		"security": map[string]interface{}{
			"password_cost":   14,
			"enable_captcha":  false,
			"captcha_ttl":     42,
		},
		"yggdrasil": map[string]interface{}{
			"security": map[string]interface{}{
				"password_cost":   8, // must NOT override existing top-level value
				"enable_captcha":  true,
				"token_expiry_days": 30,
			},
		},
	}

	out, _, err := MigrateConfig(cfg, tokenGen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sec := out["security"].(map[string]interface{})
	if sec["password_cost"] != 14 {
		t.Errorf("existing top-level password_cost must be preserved, got %v", sec["password_cost"])
	}
	if sec["enable_captcha"] != false {
		t.Errorf("existing top-level enable_captcha must be preserved, got %v", sec["enable_captcha"])
	}
}
