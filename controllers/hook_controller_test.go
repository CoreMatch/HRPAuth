package controllers

import "testing"

func TestNormalizeHookRule(t *testing.T) {
	rule, ok := normalizeHookRule(HookRule{
		Paths:          []string{"oauth/login-ticket", "/oauth/authorize/decision"},
		Phase:          HookPhaseAuthInput,
		Capability:     HookCapabilityPatch,
		FailPolicy:     HookFailPolicyFailClosed,
		PatchAllowlist: []string{"auth.password", "auth.password", "invalid"},
		URL:            "http://hook-service/input",
	}, "auth-adapter")
	if !ok {
		t.Fatal("expected hook rule to normalize successfully")
	}

	if rule.Service != "auth-adapter" {
		t.Fatalf("expected service auth-adapter, got %q", rule.Service)
	}
	if len(rule.Paths) != 2 || rule.Paths[0] != "/oauth/login-ticket" {
		t.Fatalf("unexpected normalized paths: %#v", rule.Paths)
	}
	if len(rule.PatchAllowlist) != 1 || rule.PatchAllowlist[0] != "auth.password" {
		t.Fatalf("unexpected patch allowlist: %#v", rule.PatchAllowlist)
	}
}

func TestApplyHookPatch(t *testing.T) {
	payload := &HookRequestPayload{
		Auth: HookAuthContext{
			Password:         "encoded",
			PasswordEncoding: "base64",
		},
	}

	err := applyHookPatch(payload, []string{"auth.password", "auth.password_encoding", "auth.risk_tags"}, map[string]interface{}{
		"auth.password":          "decoded-secret",
		"auth.password_encoding": "plain",
		"auth.risk_tags":         []interface{}{"device:new", "ip:trusted"},
		"auth.email":             "ignored@example.com",
	})
	if err != nil {
		t.Fatalf("applyHookPatch returned error: %v", err)
	}

	if payload.Auth.Password != "decoded-secret" {
		t.Fatalf("expected patched password, got %q", payload.Auth.Password)
	}
	if payload.Auth.PasswordEncoding != "plain" {
		t.Fatalf("expected patched password encoding, got %q", payload.Auth.PasswordEncoding)
	}
	if len(payload.Auth.RiskTags) != 2 || payload.Auth.RiskTags[0] != "device:new" {
		t.Fatalf("unexpected risk tags: %#v", payload.Auth.RiskTags)
	}
}
