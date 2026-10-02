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
