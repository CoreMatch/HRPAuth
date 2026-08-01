package controllers

import "testing"

func TestSchemaMigrationServiceConstant(t *testing.T) {
	if schemaMigrationService != "HA" {
		t.Fatalf("expected schemaMigrationService to be HA, got %q", schemaMigrationService)
	}
}
