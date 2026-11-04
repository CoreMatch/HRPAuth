package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupVerifyTokenEnv prepares sqlite tables for the verify-token chain
// (users, oauth2_clients, oauth2_access_tokens) and a manage token for the
// ops-level gate, mirroring setupClaimTestEnv.
func setupVerifyTokenEnv(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "verify-token-test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// sqlite cannot express the MySQL enum columns via AutoMigrate, so the
	// OAuth2 tables are created with plain DDL mirroring the models.
	for _, stmt := range []string{
		`CREATE TABLE oauth2_clients (
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
		)`,
		`CREATE TABLE oauth2_access_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			access_token varchar(255) UNIQUE,
			client_id varchar(100),
			user_id varchar(32),
			scopes text,
			subject_type varchar(10),
			target_uid INTEGER,
			target_email varchar(255),
			expires_at datetime,
			revoked_at datetime,
			created_at datetime
		)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create tables: %v", err)
		}
	}
	database.DB = db

	config.AppConfig = &config.Config{}
	config.AppConfig.Manage.Token = "test-manage-token"

	r := gin.New()
	r.POST("/internal/verify-token", NewAuthController().VerifyToken)
	return r
}

func doVerifyTokenRequest(t *testing.T, r *gin.Engine, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/verify-token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func verifyTokenData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return envelope.Data
}

func seedServiceToken(t *testing.T, value string, expiresAt time.Time) {
	t.Helper()
	client := models.OAuth2Client{
		ClientID:   "test-super",
		Name:       "test super client",
		Type:       "confidential",
		GrantTypes: `["client_credentials"]`,
		Scopes:     `["user.claim.as-service","user.force-bind.as-service"]`,
		IsInternal: true,
		IsSuper:    true,
		IsActive:   true,
	}
	if err := database.DB.Create(&client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	token := models.OAuth2AccessToken{
		AccessToken: value,
		ClientID:    client.ClientID,
		Scopes:      `["user.claim.as-service","user.force-bind.as-service"]`,
		SubjectType: "service",
		ExpiresAt:   expiresAt,
	}
	if err := database.DB.Create(&token).Error; err != nil {
		t.Fatalf("seed service token: %v", err)
	}
}

func TestVerifyTokenRequiresOpsAuth(t *testing.T) {
	r := setupVerifyTokenEnv(t)

	w := doVerifyTokenRequest(t, r, "", `{"token":"anything"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestVerifyTokenRejectsUnknownAndMalformed(t *testing.T) {
	r := setupVerifyTokenEnv(t)

	w := doVerifyTokenRequest(t, r, "test-manage-token", `{not-json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	w = doVerifyTokenRequest(t, r, "test-manage-token", `{"token":"no-such-token"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestVerifyTokenResolvesServiceToken(t *testing.T) {
	r := setupVerifyTokenEnv(t)
	seedServiceToken(t, "svc-token-1", time.Now().Add(time.Hour))

	w := doVerifyTokenRequest(t, r, "test-manage-token", `{"token":"svc-token-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	data := verifyTokenData(t, w)
	if data["is_service"] != true {
		t.Fatalf("is_service = %v, want true", data["is_service"])
	}
	scopes, _ := data["scopes"].([]any)
	if len(scopes) != 2 {
		t.Fatalf("scopes = %v, want the two seeded scopes", scopes)
	}
}

func TestVerifyTokenRejectsExpiredServiceToken(t *testing.T) {
	r := setupVerifyTokenEnv(t)
	seedServiceToken(t, "svc-token-expired", time.Now().Add(-time.Hour))

	w := doVerifyTokenRequest(t, r, "test-manage-token", `{"token":"svc-token-expired"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestVerifyTokenResolvesUserToken(t *testing.T) {
	r := setupVerifyTokenEnv(t)

	user := models.User{UID: 7, UUID: "user-uuid-1", Email: "alice@example.com", Username: "alice"}
	if err := database.DB.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	client := models.OAuth2Client{
		ClientID:   "test-public",
		Name:       "test public client",
		Type:       "public",
		GrantTypes: `["authorization_code"]`,
		Scopes:     `["user.read"]`,
		IsActive:   true,
	}
	if err := database.DB.Create(&client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	token := models.OAuth2AccessToken{
		AccessToken: "user-token-1",
		ClientID:    client.ClientID,
		UserID:      &user.UUID,
		Scopes:      `["user.read"]`,
		SubjectType: "user",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	if err := database.DB.Create(&token).Error; err != nil {
		t.Fatalf("seed user token: %v", err)
	}

	w := doVerifyTokenRequest(t, r, "test-manage-token", `{"token":"user-token-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	data := verifyTokenData(t, w)
	if data["is_service"] != false {
		t.Fatalf("is_service = %v, want false", data["is_service"])
	}
	if data["email"] != "alice@example.com" || data["username"] != "alice" {
		t.Fatalf("user payload mismatch: %v", data)
	}
}

// TestVerifyTokenUserDeleted covers the branch where the token row outlives
// its user (core account deleted between issuance and verification).
func TestVerifyTokenUserDeleted(t *testing.T) {
	r := setupVerifyTokenEnv(t)

	client := models.OAuth2Client{
		ClientID:   "test-public",
		Name:       "test public client",
		Type:       "public",
		GrantTypes: `["authorization_code"]`,
		Scopes:     `["user.read"]`,
		IsActive:   true,
	}
	if err := database.DB.Create(&client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	gone := "deleted-user-uuid"
	token := models.OAuth2AccessToken{
		AccessToken: "orphan-user-token",
		ClientID:    client.ClientID,
		UserID:      &gone,
		Scopes:      `["user.read"]`,
		SubjectType: "user",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	if err := database.DB.Create(&token).Error; err != nil {
		t.Fatalf("seed token: %v", err)
	}

	w := doVerifyTokenRequest(t, r, "test-manage-token", `{"token":"orphan-user-token"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a token whose user is gone", w.Code)
	}
}
