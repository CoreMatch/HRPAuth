package controllers

import (
	"testing"
)

func TestIsValidEmailAcceptsRFCStyleAddress(t *testing.T) {
	if !isValidEmail("User.Name+tag@example.com") {
		t.Fatal("expected valid address to pass validation")
	}
}

func TestIsValidEmailRejectsInvalidAddress(t *testing.T) {
	if isValidEmail("not-an-email") {
		t.Fatal("expected invalid email to be rejected")
	}
}
