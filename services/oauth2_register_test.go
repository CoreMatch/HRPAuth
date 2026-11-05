package services

import (
	"errors"
	"testing"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
)

// withScopeAllowlist swaps config.AppConfig for one carrying the given
// self-service scope allowlist and restores the previous value after the
// test.
func withScopeAllowlist(t *testing.T, allowlist map[string][]string) {
	t.Helper()
	old := config.AppConfig
	config.AppConfig = &config.Config{
		OAuth2: config.OAuth2Config{ServiceScopeAllowlist: allowlist},
	}
	t.Cleanup(func() { config.AppConfig = old })
}

func TestServiceRegistrationScopesRequestedSubset(t *testing.T) {
	withScopeAllowlist(t, map[string][]string{
		"winnerproxy": {"user.read.as-service", "user.delete.as-service"},
	})
	svc := NewOAuth2Service()

	granted, err := svc.ServiceRegistrationScopes("winnerproxy", "user.read.as-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(granted) != 1 || granted[0] != "user.read.as-service" {
		t.Fatalf("granted = %v, want the requested subset", granted)
	}

	// Duplicates collapse; order follows the request.
	granted, err = svc.ServiceRegistrationScopes("winnerproxy",
		"user.delete.as-service user.read.as-service user.delete.as-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(granted) != 2 || granted[0] != "user.delete.as-service" || granted[1] != "user.read.as-service" {
		t.Fatalf("granted = %v, want deduplicated request order", granted)
	}
}

func TestServiceRegistrationScopesViolations(t *testing.T) {
	withScopeAllowlist(t, map[string][]string{
		"winnerproxy": {"user.read.as-service"},
	})
	svc := NewOAuth2Service()

	cases := map[string]string{
		"db scope is never grantable":           "db.line.read",
		"known service scope outside allowlist": "user.claim.as-service",
		"unknown scope":                         "nope",
	}
	for name, scope := range cases {
		if _, err := svc.ServiceRegistrationScopes("winnerproxy", scope); !errors.Is(err, ErrOAuthInvalidClientMetadata) {
			t.Errorf("%s: err = %v, want ErrOAuthInvalidClientMetadata", name, err)
		}
	}

	// A service missing from the allowlist cannot request any scope.
	if _, err := svc.ServiceRegistrationScopes("unlisted", "user.read.as-service"); !errors.Is(err, ErrOAuthInvalidClientMetadata) {
		t.Errorf("unlisted service: err = %v, want ErrOAuthInvalidClientMetadata", err)
	}
}

func TestServiceRegistrationScopesEmptyRequestFallsBackToAllowlist(t *testing.T) {
	withScopeAllowlist(t, map[string][]string{
		// db.* in the allowlist must never take effect: the fallback
		// intersects with the known service scope set.
		"winnerproxy": {"user.read.as-service", "db.line.read"},
	})
	svc := NewOAuth2Service()

	granted, err := svc.ServiceRegistrationScopes("winnerproxy", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(granted) != 1 || granted[0] != "user.read.as-service" {
		t.Fatalf("granted = %v, want the allowlist minus non-service scopes", granted)
	}

	// Unlisted service: empty grant, no error.
	granted, err = svc.ServiceRegistrationScopes("unlisted", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(granted) != 0 {
		t.Fatalf("granted = %v, want empty for an unlisted service", granted)
	}
}

func TestRegisterServiceClientCreatesFixedShape(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withScopeAllowlist(t, nil)
	svc := NewOAuth2Service()

	client, created, err := svc.RegisterServiceClient("winnerproxy", []string{"user.read.as-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("expected a new row to be created")
	}
	if client.ClientID != "winnerproxy" || client.Name != "winnerproxy" {
		t.Errorf("client_id/name = %q/%q, want the service name", client.ClientID, client.Name)
	}
	if client.ClientSecret == "" {
		t.Error("expected a generated client secret")
	}
	if client.Type != "confidential" {
		t.Errorf("type = %q, want confidential", client.Type)
	}
	if client.GrantTypes != `["client_credentials"]` {
		t.Errorf("grant_types = %q, want [\"client_credentials\"]", client.GrantTypes)
	}
	if client.Scopes != `["user.read.as-service"]` {
		t.Errorf("scopes = %q, want the granted subset", client.Scopes)
	}
	if client.IsSuper {
		t.Error("is_super must be false for self-registered clients")
	}
	if !client.IsActive {
		t.Error("is_active must be true for self-registered clients")
	}
	if !client.IsInternal {
		t.Error("is_internal must be true for self-registered clients")
	}
}

func TestRegisterServiceClientGetOrCreate(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withScopeAllowlist(t, nil)
	svc := NewOAuth2Service()

	first, created, err := svc.RegisterServiceClient("winnerproxy", []string{"user.read.as-service"})
	if err != nil || !created {
		t.Fatalf("first call: created=%v err=%v, want create", created, err)
	}

	second, created, err := svc.RegisterServiceClient("winnerproxy", []string{"user.delete.as-service"})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created {
		t.Fatal("second call must not create a new row")
	}
	if second.ClientSecret != first.ClientSecret {
		t.Error("existing secret must be returned unchanged")
	}
	if second.Scopes != first.Scopes {
		t.Errorf("existing scopes must be unchanged, got %q vs %q", second.Scopes, first.Scopes)
	}

	var n int64
	if err := database.DB.Model(&models.OAuth2Client{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 row, got %d", n)
	}
}

// TestRegisterServiceClientRefusesSuperPoisonedRow pins the root-key
// guard: a legacy row whose stored secret equals the super client secret
// must never be handed out by get-or-create; the call refuses with
// ErrOAuthRegistrationConflict and leaves the row untouched.
func TestRegisterServiceClientRefusesSuperPoisonedRow(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withScopeAllowlist(t, nil)
	config.AppConfig.OAuth2.SuperClientSecret = "root-secret-xyz"

	poisoned := models.OAuth2Client{
		ClientID:     "winnerproxy",
		ClientSecret: "root-secret-xyz",
		Name:         "winnerproxy",
		Type:         "confidential",
		GrantTypes:   `["client_credentials"]`,
		RedirectURIs: "[]",
		Scopes:       "[]",
		IsInternal:   true,
		IsSuper:      false,
		IsActive:     true,
	}
	if err := database.DB.Create(&poisoned).Error; err != nil {
		t.Fatalf("pre-seed: %v", err)
	}

	client, created, err := NewOAuth2Service().RegisterServiceClient("winnerproxy", []string{"user.read.as-service"})
	if !errors.Is(err, ErrOAuthRegistrationConflict) {
		t.Fatalf("err = %v, want ErrOAuthRegistrationConflict", err)
	}
	if created || client != nil {
		t.Fatal("a poisoned row must yield no client")
	}

	var row models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "winnerproxy").First(&row).Error; err != nil {
		t.Fatalf("row disappeared: %v", err)
	}
	if row.ClientSecret != "root-secret-xyz" {
		t.Errorf("poisoned row must stay untouched, secret = %q", row.ClientSecret)
	}
}

// TestRegisterServiceClientCoexistsWithSeed pins the config-first rule: a
// row already seeded from oauth2.service_clients is returned as-is by a
// later self-registration call.
func TestRegisterServiceClientCoexistsWithSeed(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withScopeAllowlist(t, nil)

	seeded := models.OAuth2Client{
		ClientID:     "winnerproxy",
		ClientSecret: "seeded-secret",
		Name:         "winnerproxy",
		Type:         "confidential",
		GrantTypes:   `["client_credentials"]`,
		RedirectURIs: "[]",
		Scopes:       "[]",
		IsInternal:   true,
		IsSuper:      false,
		IsActive:     true,
	}
	if err := database.DB.Create(&seeded).Error; err != nil {
		t.Fatalf("pre-seed: %v", err)
	}

	client, created, err := NewOAuth2Service().RegisterServiceClient("winnerproxy", []string{"user.read.as-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("self-registration must not create a second row next to the seed")
	}
	if client.ClientSecret != "seeded-secret" {
		t.Errorf("secret = %q, want the seeded value", client.ClientSecret)
	}
}
