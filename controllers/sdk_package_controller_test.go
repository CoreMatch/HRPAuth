package controllers

import (
	"strings"
	"testing"
)

func TestParseManifestCriticalOperations(t *testing.T) {
	tests := []struct {
		name       string
		operations string
	}{
		{
			name:       "array",
			operations: `[{"label":"Review security","path":"/security"},{"label":"Delete account","path":"/delete"}]`,
		},
		{
			name:       "single object",
			operations: `{"label":"Review security","path":"/security"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"schema_version":1,"name":"security-tools","version":"1.0.0","routes":[{"path":"/security","module":"src/security"},{"path":"/delete","module":"src/delete"}],"critical_operations":` + tt.operations + `}`
			if _, err := parseManifest([]byte(raw)); err != nil {
				t.Fatalf("parseManifest rejected valid critical_operations: %v", err)
			}
		})
	}
}

func TestParseManifestRejectsInvalidCriticalOperations(t *testing.T) {
	tests := []struct {
		name       string
		operations string
		wantError  string
	}{
		{
			name:       "missing label",
			operations: `[{"path":"/security"}]`,
			wantError:  "critical_operations.label is required",
		},
		{
			name:       "unknown route",
			operations: `[{"label":"Review security","path":"/unknown"}]`,
			wantError:  "critical_operations.path",
		},
		{
			name:       "invalid entry type",
			operations: `["/security"]`,
			wantError:  "critical_operations entry must be an object",
		},
		{
			name:       "invalid container type",
			operations: `"not-an-entry"`,
			wantError:  "critical_operations must be an object or an array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"schema_version":1,"name":"security-tools","version":"1.0.0","routes":[{"path":"/security","module":"src/security"}],"critical_operations":` + tt.operations + `}`
			_, err := parseManifest([]byte(raw))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}

func TestBuildMetaIncludesCriticalOperations(t *testing.T) {
	manifest, err := parseManifest([]byte(`{
		"schema_version":1,
		"name":"security-tools",
		"version":"1.0.0",
		"routes":[{"path":"/security","module":"src/security"}],
		"critical_operations":[{"label":"Review security","path":"/security"}]
	}`))
	if err != nil {
		t.Fatalf("parse valid manifest: %v", err)
	}

	meta := buildMeta("security-tools", []byte("archive"), manifest, make([]byte, 32))
	if len(meta.CriticalOperations) != 1 {
		t.Fatalf("expected 1 critical operation, got %d", len(meta.CriticalOperations))
	}
	if got := meta.CriticalOperations[0]; got.Label != "Review security" || got.Path != "/security" {
		t.Fatalf("unexpected critical operation metadata: %+v", got)
	}
	if _, ok := publicMeta(meta)["critical_operations"].([]SdkCriticalOperationMeta); !ok {
		t.Fatalf("public metadata should expose critical_operations")
	}
}

func TestParseManifestProfileCardOnly(t *testing.T) {
	raw := `{
		"schema_version":1,
		"name":"profile-tools",
		"version":"1.0.0",
		"profile_cards":{"title":"Profile tools","module":"src/profile/Tools"}
	}`
	if _, err := parseManifest([]byte(raw)); err != nil {
		t.Fatalf("parseManifest rejected profile-card-only package: %v", err)
	}
}

func TestParseManifestRejectsEmptyProfileCardObject(t *testing.T) {
	raw := `{"schema_version":1,"name":"profile-tools","version":"1.0.0","profile_cards":{}}`
	if _, err := parseManifest([]byte(raw)); err == nil {
		t.Fatal("parseManifest accepted an empty profile_cards object")
	}
}

func TestParseManifestContributionOnly(t *testing.T) {
	raw := `{
		"schema_version":1,
		"name":"register-providers",
		"version":"1.0.0",
		"contributions":[
			{"id":"github","surface":"register.providers","module":"src/register/GitHubButton","label":"GitHub","priority":20}
		]
	}`
	manifest, err := parseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("parseManifest rejected contribution-only package: %v", err)
	}
	meta := buildMeta("register-providers", []byte("archive"), manifest, make([]byte, 32))
	if len(meta.Contributions) != 1 {
		t.Fatalf("expected 1 contribution, got %d", len(meta.Contributions))
	}
	contribution := meta.Contributions[0]
	if contribution.ID != "github" || contribution.Surface != "register.providers" || contribution.Label != "GitHub" {
		t.Fatalf("unexpected contribution metadata: %+v", contribution)
	}
	if contribution.Priority == nil || *contribution.Priority != 20 {
		t.Fatalf("expected priority 20, got %v", contribution.Priority)
	}
	if _, ok := publicMeta(meta)["contributions"].([]SdkContributionMeta); !ok {
		t.Fatalf("public metadata should expose contributions")
	}
}

func TestParseManifestRejectsInvalidContributions(t *testing.T) {
	tests := []struct {
		name          string
		contributions string
		wantError     string
	}{
		{
			name:          "unsupported surface",
			contributions: `[{"id":"github","surface":"unknown.surface","module":"src/GitHub","label":"GitHub"}]`,
			wantError:     "surface",
		},
		{
			name:          "missing label",
			contributions: `[{"id":"github","surface":"register.providers","module":"src/GitHub"}]`,
			wantError:     "label is required",
		},
		{
			name:          "invalid module",
			contributions: `[{"id":"github","surface":"register.providers","module":"../GitHub","label":"GitHub"}]`,
			wantError:     "module",
		},
		{
			name:          "duplicate id",
			contributions: `[{"id":"github","surface":"register.providers","module":"src/GitHub","label":"GitHub"},{"id":"github","surface":"register.providers","module":"src/GitHubAlt","label":"GitHub Alt"}]`,
			wantError:     "duplicated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"schema_version":1,"name":"register-providers","version":"1.0.0","contributions":` + tt.contributions + `}`
			_, err := parseManifest([]byte(raw))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}
