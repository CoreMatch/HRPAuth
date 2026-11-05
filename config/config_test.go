package config

import "testing"

func TestParseWebAuthnConfigNormalizesFallbackOrigins(t *testing.T) {
	cfg := map[string]interface{}{
		"site": map[string]interface{}{
			"name": "HRPAuth",
		},
		"frontend": map[string]interface{}{
			"url": "https://auth.example.com/app/",
		},
		"callback": map[string]interface{}{
			"url": "https://api.example.com/v1/hrpauth?foo=bar",
		},
	}

	parsed := parseWebAuthnConfig(cfg)

	if parsed.RPID != "auth.example.com" {
		t.Fatalf("expected derived rp_id auth.example.com, got %q", parsed.RPID)
	}

	wantOrigins := []string{
		"https://auth.example.com",
		"https://api.example.com",
	}
	if len(parsed.RPOrigins) != len(wantOrigins) {
		t.Fatalf("expected %d origins, got %d (%v)", len(wantOrigins), len(parsed.RPOrigins), parsed.RPOrigins)
	}
	for i, want := range wantOrigins {
		if parsed.RPOrigins[i] != want {
			t.Fatalf("expected origin %d to be %q, got %q", i, want, parsed.RPOrigins[i])
		}
	}
}

func TestParseWebAuthnConfigNormalizesExplicitOrigins(t *testing.T) {
	cfg := map[string]interface{}{
		"webauthn": map[string]interface{}{
			"rp_origins": []interface{}{
				"https://auth.example.com/app",
				"https://auth.example.com/",
				"https://admin.example.com/settings?tab=security",
			},
		},
	}

	parsed := parseWebAuthnConfig(cfg)

	wantOrigins := []string{
		"https://auth.example.com",
		"https://admin.example.com",
	}
	if len(parsed.RPOrigins) != len(wantOrigins) {
		t.Fatalf("expected %d origins, got %d (%v)", len(wantOrigins), len(parsed.RPOrigins), parsed.RPOrigins)
	}
	for i, want := range wantOrigins {
		if parsed.RPOrigins[i] != want {
			t.Fatalf("expected origin %d to be %q, got %q", i, want, parsed.RPOrigins[i])
		}
	}
}

func TestDatabaseConfigAddr(t *testing.T) {
	cases := []struct {
		name string
		cfg  DatabaseConfig
		want string
	}{
		{"standalone port", DatabaseConfig{Host: "db.example", Port: 3307}, "db.example:3307"},
		{"legacy inline port without field", DatabaseConfig{Host: "db.example:3306"}, "db.example:3306"},
		{"bare host without port", DatabaseConfig{Host: "db.example"}, "db.example"},
		{"standalone port wins over inline", DatabaseConfig{Host: "db.example:3306", Port: 3307}, "db.example:3307"},
	}
	for _, tc := range cases {
		if got := tc.cfg.Addr(); got != tc.want {
			t.Errorf("%s: Addr() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseDatabaseConfigPort(t *testing.T) {
	withPort := parseDatabaseConfig(map[string]interface{}{
		"database": map[string]interface{}{"host": "db.example", "port": 3307},
	})
	if withPort.Port != 3307 {
		t.Errorf("expected port 3307, got %d", withPort.Port)
	}
	withoutPort := parseDatabaseConfig(map[string]interface{}{
		"database": map[string]interface{}{"host": "db.example:3306"},
	})
	if withoutPort.Port != 0 {
		t.Errorf("expected port 0 when absent, got %d", withoutPort.Port)
	}
	if withoutPort.Addr() != "db.example:3306" {
		t.Errorf("legacy inline host must keep working, got %q", withoutPort.Addr())
	}
}

func TestParseOAuth2ConfigServiceClients(t *testing.T) {
	// 缺失 service_clients 段 → 空列表（旧配置零行为变化）。
	missing := parseOAuth2Config(map[string]interface{}{
		"oauth2": map[string]interface{}{"super_client_id": "s"},
	})
	if len(missing.ServiceClients) != 0 {
		t.Fatalf("expected no service clients when the section is missing, got %v", missing.ServiceClients)
	}

	// 显式列表按字段解析，name 可缺省。
	parsed := parseOAuth2Config(map[string]interface{}{
		"oauth2": map[string]interface{}{
			"service_clients": []interface{}{
				map[string]interface{}{
					"client_id":     "winnerproxy",
					"client_secret": "secret-w",
					"name":          "WinnerProxy",
				},
				map[string]interface{}{
					"client_id":     "hrpauth-yggdrasil-api",
					"client_secret": "secret-y",
				},
			},
		},
	})
	if len(parsed.ServiceClients) != 2 {
		t.Fatalf("expected 2 service clients, got %v", parsed.ServiceClients)
	}
	first := parsed.ServiceClients[0]
	if first.ClientID != "winnerproxy" || first.ClientSecret != "secret-w" || first.Name != "WinnerProxy" {
		t.Errorf("unexpected first entry: %+v", first)
	}
	second := parsed.ServiceClients[1]
	if second.ClientID != "hrpauth-yggdrasil-api" || second.ClientSecret != "secret-y" || second.Name != "" {
		t.Errorf("unexpected second entry: %+v", second)
	}
}

func TestParseOAuth2ConfigServiceScopeAllowlist(t *testing.T) {
	// 缺失 service_scope_allowlist 段 → 空 map（旧配置零行为变化）。
	missing := parseOAuth2Config(map[string]interface{}{
		"oauth2": map[string]interface{}{"super_client_id": "s"},
	})
	if len(missing.ServiceScopeAllowlist) != 0 {
		t.Fatalf("expected an empty allowlist when the section is missing, got %v", missing.ServiceScopeAllowlist)
	}

	// 显式 map 按服务名解析；空白条目被剔除。
	parsed := parseOAuth2Config(map[string]interface{}{
		"oauth2": map[string]interface{}{
			"service_scope_allowlist": map[string]interface{}{
				"winnerproxy": []interface{}{"user.read.as-service", "  ", "user.delete.as-service"},
				"  ":          []interface{}{"user.read.as-service"},
			},
		},
	})
	if len(parsed.ServiceScopeAllowlist) != 1 {
		t.Fatalf("expected exactly 1 allowlist entry, got %v", parsed.ServiceScopeAllowlist)
	}
	got := parsed.ServiceScopeAllowlist["winnerproxy"]
	if len(got) != 2 || got[0] != "user.read.as-service" || got[1] != "user.delete.as-service" {
		t.Errorf("unexpected winnerproxy allowlist: %v", got)
	}
}
