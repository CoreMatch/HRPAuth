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
