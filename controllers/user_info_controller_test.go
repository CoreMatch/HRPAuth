package controllers

import (
	"testing"
)

func TestNormalizeDeclaredEmail(t *testing.T) {
	normalized, err := normalizeDeclaredEmail("  User@Example.COM  ")
	if err != nil {
		t.Fatalf("normalizeDeclaredEmail returned error: %v", err)
	}
	if normalized != "user@example.com" {
		t.Fatalf("expected normalized email to be lower-cased, got %q", normalized)
	}
}

func TestNormalizeDeclaredEmailRejectsInvalid(t *testing.T) {
	_, err := normalizeDeclaredEmail("not-an-email")
	if err == nil {
		t.Fatal("expected invalid email to be rejected")
	}
}
