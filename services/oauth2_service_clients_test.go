package services

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupServiceClientSeedEnv prepares a sqlite oauth2_clients table and
// points database.DB at it, mirroring the DDL approach of
// controllers/verify_token_test.go (sqlite cannot express the MySQL enum
// columns via AutoMigrate).
func setupServiceClientSeedEnv(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "svc-seed-test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// sqlite 不支持 MySQL 那种多连接并发写；单连接串行化事务，让并发
	// 种子测试考察"重复调用幂等"而非文件锁。真实的跨进程竞争由
	// client_id 唯一索引 + Create 失败复查兜底（见
	// ensureConfiguredServiceClients）。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec(`CREATE TABLE oauth2_clients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id varchar(100) UNIQUE,
		client_secret varchar(255),
		name varchar(255),
		type varchar(20),
		grant_types text,
		redirect_uris text,
		scopes text,
		is_internal BOOLEAN DEFAULT 0,
		is_super BOOLEAN DEFAULT 0,
		is_active BOOLEAN DEFAULT 1,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create oauth2_clients: %v", err)
	}
	database.DB = db
}

// withServiceClientsConfig swaps config.AppConfig for one carrying the
// given service-client seed list and restores the previous value after
// the test.
func withServiceClientsConfig(t *testing.T, clients []config.ServiceClientConfig) {
	t.Helper()
	old := config.AppConfig
	config.AppConfig = &config.Config{
		OAuth2: config.OAuth2Config{ServiceClients: clients},
	}
	t.Cleanup(func() { config.AppConfig = old })
}

// runSeed runs ensureConfiguredServiceClients inside a transaction, the
// same way EnsureBuiltInClients invokes it.
func runSeed(t *testing.T) error {
	t.Helper()
	svc := NewOAuth2Service()
	return database.DB.Transaction(func(tx *gorm.DB) error {
		return svc.ensureConfiguredServiceClients(tx)
	})
}

func countClients(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := database.DB.Model(&models.OAuth2Client{}).Count(&n).Error; err != nil {
		t.Fatalf("count clients: %v", err)
	}
	return n
}

// TestEnsureConfiguredServiceClients_EmptyListNoSideEffects pins the
// backward-compatibility guarantee: no service_clients section (empty
// list) seeds nothing and changes nothing.
func TestEnsureConfiguredServiceClients_EmptyListNoSideEffects(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withServiceClientsConfig(t, nil)

	if err := runSeed(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countClients(t); n != 0 {
		t.Fatalf("expected zero clients, got %d", n)
	}
}

// TestEnsureConfiguredServiceClients_CreatesMissing verifies the seeded
// row shape: confidential, client_credentials only, not super, active,
// secret from config, name falling back to client_id.
func TestEnsureConfiguredServiceClients_CreatesMissing(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withServiceClientsConfig(t, []config.ServiceClientConfig{
		{ClientID: "winnerproxy", ClientSecret: "secret-w"},
		{ClientID: "hrpauth-yggdrasil-api", ClientSecret: "secret-y", Name: "Yggdrasil API"},
	})

	if err := runSeed(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countClients(t); n != 2 {
		t.Fatalf("expected 2 clients, got %d", n)
	}

	var wp models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "winnerproxy").First(&wp).Error; err != nil {
		t.Fatalf("winnerproxy row missing: %v", err)
	}
	if wp.Type != "confidential" {
		t.Errorf("type = %q, want confidential", wp.Type)
	}
	if wp.GrantTypes != `["client_credentials"]` {
		t.Errorf("grant_types = %q, want [\"client_credentials\"]", wp.GrantTypes)
	}
	if wp.IsSuper {
		t.Error("is_super must be false for seeded service clients")
	}
	if !wp.IsActive {
		t.Error("is_active must be true for seeded service clients")
	}
	if wp.ClientSecret != "secret-w" {
		t.Errorf("secret = %q, want the configured value", wp.ClientSecret)
	}
	if wp.Name != "winnerproxy" {
		t.Errorf("name = %q, want fallback to client_id", wp.Name)
	}
	if wp.Scopes != "[]" {
		t.Errorf("scopes = %q, want []", wp.Scopes)
	}

	var ygg models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "hrpauth-yggdrasil-api").First(&ygg).Error; err != nil {
		t.Fatalf("yggdrasil row missing: %v", err)
	}
	if ygg.Name != "Yggdrasil API" {
		t.Errorf("name = %q, want the configured display name", ygg.Name)
	}
}

// TestEnsureConfiguredServiceClients_ExistingSecretUntouched pins the
// no-clobber rule: a row already present (e.g. manually rotated secret)
// is left exactly as-is.
func TestEnsureConfiguredServiceClients_ExistingSecretUntouched(t *testing.T) {
	setupServiceClientSeedEnv(t)
	pre := models.OAuth2Client{
		ClientID:     "winnerproxy",
		ClientSecret: "rotated-manually",
		Name:         "WinnerProxy (manual)",
		Type:         "confidential",
		GrantTypes:   `["client_credentials"]`,
		RedirectURIs: "[]",
		Scopes:       "[]",
		IsInternal:   true,
		IsSuper:      false,
		IsActive:     true,
	}
	if err := database.DB.Create(&pre).Error; err != nil {
		t.Fatalf("pre-seed: %v", err)
	}
	withServiceClientsConfig(t, []config.ServiceClientConfig{
		{ClientID: "winnerproxy", ClientSecret: "config-secret"},
	})

	if err := runSeed(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countClients(t); n != 1 {
		t.Fatalf("expected exactly 1 client, got %d", n)
	}
	var row models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "winnerproxy").First(&row).Error; err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if row.ClientSecret != "rotated-manually" {
		t.Errorf("secret was overwritten: %q", row.ClientSecret)
	}
	if row.Name != "WinnerProxy (manual)" {
		t.Errorf("name was overwritten: %q", row.Name)
	}
}

// TestEnsureConfiguredServiceClients_DriftWarnsNoChange pins the drift
// rule: an existing row that differs from config (here: is_super) is not
// modified; the seeding still succeeds (warning is logged).
func TestEnsureConfiguredServiceClients_DriftWarnsNoChange(t *testing.T) {
	setupServiceClientSeedEnv(t)
	pre := models.OAuth2Client{
		ClientID:     "winnerproxy",
		ClientSecret: "other-secret",
		Name:         "WinnerProxy",
		Type:         "confidential",
		GrantTypes:   `["client_credentials"]`,
		RedirectURIs: "[]",
		Scopes:       "[]",
		IsInternal:   true,
		IsSuper:      true, // drift vs the seeded is_super=false
		IsActive:     true,
	}
	if err := database.DB.Create(&pre).Error; err != nil {
		t.Fatalf("pre-seed: %v", err)
	}
	withServiceClientsConfig(t, []config.ServiceClientConfig{
		{ClientID: "winnerproxy", ClientSecret: "config-secret"},
	})

	if err := runSeed(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countClients(t); n != 1 {
		t.Fatalf("expected exactly 1 client, got %d", n)
	}
	var row models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "winnerproxy").First(&row).Error; err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if !row.IsSuper {
		t.Error("drifted row must stay untouched (is_super)")
	}
	if row.ClientSecret != "other-secret" {
		t.Errorf("drifted row secret changed: %q", row.ClientSecret)
	}
}

// TestEnsureConfiguredServiceClients_ConcurrentNoDuplicates races the
// seeding path: even with concurrent runs only one row per client_id may
// exist (unique index + re-check fallback).
func TestEnsureConfiguredServiceClients_ConcurrentNoDuplicates(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withServiceClientsConfig(t, []config.ServiceClientConfig{
		{ClientID: "winnerproxy", ClientSecret: "secret-w"},
	})

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = runSeed(t)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: unexpected error: %v", i, err)
		}
	}
	if n := countClients(t); n != 1 {
		t.Fatalf("expected exactly 1 client after concurrent seeding, got %d", n)
	}
}

// TestEnsureConfiguredServiceClients_SkipsIncompleteEntries verifies that
// entries missing client_id or client_secret are skipped, not seeded.
func TestEnsureConfiguredServiceClients_SkipsIncompleteEntries(t *testing.T) {
	setupServiceClientSeedEnv(t)
	withServiceClientsConfig(t, []config.ServiceClientConfig{
		{ClientID: "", ClientSecret: "x"},
		{ClientID: "no-secret", ClientSecret: "  "},
	})

	if err := runSeed(t); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countClients(t); n != 0 {
		t.Fatalf("expected zero clients for incomplete entries, got %d", n)
	}
}
