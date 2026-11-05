package controllers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/registry"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupRegisterClientEnv prepares a sqlite oauth2_clients table, a config
// carrying the super-client credentials and a self-service scope allowlist,
// and a presence registry with "winnerproxy" registered — the preconditions
// of POST /oauth/register.
func setupRegisterClientEnv(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "register-client-test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
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
	config.AppConfig.OAuth2.SuperClientID = "test-super"
	config.AppConfig.OAuth2.SuperClientSecret = "super-secret"
	config.AppConfig.OAuth2.ServiceScopeAllowlist = map[string][]string{
		"winnerproxy": {"user.read.as-service", "user.delete.as-service"},
	}

	presence := registry.NewPresenceRegistry()
	presence.Register("winnerproxy", 0, nil, 2, nil, nil)

	r := gin.New()
	r.POST("/oauth/register", NewOAuth2Controller(nil, presence).RegisterClient)
	return r
}

// seedSuperClientRow inserts the DB row the built-in super client relies on
// (secret itself lives in config; the row stores the "(config)" placeholder).
func seedSuperClientRow(t *testing.T) {
	t.Helper()
	client := models.OAuth2Client{
		ClientID:     "test-super",
		ClientSecret: "(config)",
		Name:         "test super client",
		Type:         "confidential",
		GrantTypes:   `["client_credentials"]`,
		RedirectURIs: "[]",
		Scopes:       `["user.claim.as-service"]`,
		IsInternal:   true,
		IsSuper:      true,
		IsActive:     true,
	}
	if err := database.DB.Create(&client).Error; err != nil {
		t.Fatalf("seed super client: %v", err)
	}
}

func basicAuthHeader(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
}

func doRegisterClientRequest(t *testing.T, r *gin.Engine, authHeader, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return out
}

func errorCode(body map[string]any) string {
	code, _ := body["code"].(string)
	return code
}

func TestRegisterClientSuccess(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

	w := doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"winnerproxy","scope":"user.read.as-service"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s; want 201", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["client_id"] != "winnerproxy" {
		t.Errorf("client_id = %v, want winnerproxy", body["client_id"])
	}
	secret, _ := body["client_secret"].(string)
	if secret == "" {
		t.Error("client_secret must be returned")
	}
	issuedAt, _ := body["client_id_issued_at"].(float64)
	if issuedAt <= 0 {
		t.Errorf("client_id_issued_at = %v, want a unix timestamp", body["client_id_issued_at"])
	}
	if expiresAt, _ := body["client_secret_expires_at"].(float64); expiresAt != 0 {
		t.Errorf("client_secret_expires_at = %v, want 0 (never expires)", body["client_secret_expires_at"])
	}
	if body["scope"] != "user.read.as-service" {
		t.Errorf("scope = %v, want the granted subset", body["scope"])
	}

	// Issued row shape: confidential + client_credentials + is_super=false.
	var row models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "winnerproxy").First(&row).Error; err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if row.ClientSecret != secret {
		t.Error("returned secret must match the stored row")
	}
	if row.Type != "confidential" || row.GrantTypes != `["client_credentials"]` {
		t.Errorf("row shape = %q/%q, want confidential/client_credentials", row.Type, row.GrantTypes)
	}
	if row.IsSuper || !row.IsActive {
		t.Errorf("is_super/is_active = %v/%v, want false/true", row.IsSuper, row.IsActive)
	}
}

func TestRegisterClientAuthFailures(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

	// A non-super confidential client authenticates fine but must not be
	// able to register other clients.
	plain := models.OAuth2Client{
		ClientID:     "plain-client",
		ClientSecret: "plain-secret",
		Name:         "plain",
		Type:         "confidential",
		GrantTypes:   `["client_credentials"]`,
		RedirectURIs: "[]",
		Scopes:       "[]",
		IsInternal:   true,
		IsActive:     true,
	}
	if err := database.DB.Create(&plain).Error; err != nil {
		t.Fatalf("seed plain client: %v", err)
	}

	cases := map[string]string{
		"missing header":            "",
		"wrong super secret":        basicAuthHeader("test-super", "wrong"),
		"unknown client":            basicAuthHeader("ghost", "whatever"),
		"malformed base64":          "Basic !!!not-base64!!!",
		"basic without colon":       "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")),
		"authenticated non-super":   basicAuthHeader("plain-client", "plain-secret"),
		"user bearer token ignored": "Bearer user-token-1",
	}
	for name, header := range cases {
		w := doRegisterClientRequest(t, r, header, `{"client_name":"winnerproxy"}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, w.Code)
			continue
		}
		if code := errorCode(decodeBody(t, w)); code != CodeOAuthInvalidClient {
			t.Errorf("%s: code = %q, want %q", name, code, CodeOAuthInvalidClient)
		}
	}
}

// TestRegisterClientRejectsUserToken covers the "user tokens are not
// accepted" rule with a genuine user access token present in the DB.
func TestRegisterClientRejectsUserToken(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

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
		Scopes:      `["user.read"]`,
		SubjectType: "user",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	if err := database.DB.Create(&token).Error; err != nil {
		t.Fatalf("seed user token: %v", err)
	}

	w := doRegisterClientRequest(t, r, "Bearer user-token-1", `{"client_name":"winnerproxy"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a user bearer token", w.Code)
	}
}

func TestRegisterClientUnregisteredService(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

	w := doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"ghost-service","scope":"user.read.as-service"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if code := errorCode(decodeBody(t, w)); code != CodeOAuthInvalidClientMetadata {
		t.Errorf("code = %q, want %q", code, CodeOAuthInvalidClientMetadata)
	}
}

func TestRegisterClientScopeViolations(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)
	auth := basicAuthHeader("test-super", "super-secret")

	cases := map[string]string{
		"db scope rejected":             `{"client_name":"winnerproxy","scope":"db.line.read"}`,
		"known scope outside allowlist": `{"client_name":"winnerproxy","scope":"user.claim.as-service"}`,
		"unknown scope":                 `{"client_name":"winnerproxy","scope":"nope"}`,
		"missing client_name":           `{"scope":"user.read.as-service"}`,
		"invalid json body":             `{not-json`,
	}
	for name, body := range cases {
		w := doRegisterClientRequest(t, r, auth, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Code)
			continue
		}
		if code := errorCode(decodeBody(t, w)); code != CodeOAuthInvalidClientMetadata {
			t.Errorf("%s: code = %q, want %q", name, code, CodeOAuthInvalidClientMetadata)
		}
	}
}

func TestRegisterClientIdempotent(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)
	auth := basicAuthHeader("test-super", "super-secret")

	first := doRegisterClientRequest(t, r, auth, `{"client_name":"winnerproxy","scope":"user.read.as-service"}`)
	second := doRegisterClientRequest(t, r, auth, `{"client_name":"winnerproxy","scope":"user.read.as-service"}`)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d/%d, want 201/201", first.Code, second.Code)
	}
	firstBody := decodeBody(t, first)
	secondBody := decodeBody(t, second)
	if firstBody["client_id"] != secondBody["client_id"] {
		t.Error("repeated registration must return the same client_id")
	}
	if firstBody["client_secret"] != secondBody["client_secret"] {
		t.Error("repeated registration must return the same client_secret")
	}

	var n int64
	if err := database.DB.Model(&models.OAuth2Client{}).Where("client_id = ?", "winnerproxy").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 row for winnerproxy, got %d", n)
	}
}

func TestRegisterClientEmptyScopeFallsBackToAllowlist(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

	w := doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"winnerproxy"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s; want 201", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["scope"] != "user.read.as-service user.delete.as-service" {
		t.Errorf("scope = %v, want the full allowlist", body["scope"])
	}
}

func TestRegisterClientAuditLog(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	w := doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"winnerproxy","scope":"user.read.as-service"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	logged := buf.String()
	for _, want := range []string{
		"[CLIENT-REG]",
		`caller="test-super"`,
		`service="winnerproxy"`,
		`scopes="user.read.as-service"`,
		"result=client created",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("audit log missing %q; got:\n%s", want, logged)
		}
	}

	// A denied call must leave an audit trail too.
	buf.Reset()
	w = doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"ghost-service"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if logged = buf.String(); !strings.Contains(logged, "[CLIENT-REG]") || !strings.Contains(logged, "denied") {
		t.Errorf("denied call not audit-logged; got:\n%s", logged)
	}
}

// TestRegisterClientRefusesLegacySuperSecretRow pins the root-key guard
// end to end: a legacy row whose stored secret equals the super client
// secret must never be returned to the caller. The endpoint refuses with
// 400 invalid_client_metadata and an actionable message, the row stays
// untouched, and neither the response nor the audit log leaks the secret.
func TestRegisterClientRefusesLegacySuperSecretRow(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

	poisoned := models.OAuth2Client{
		ClientID:     "winnerproxy",
		ClientSecret: "super-secret", // equals config SuperClientSecret in this env
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

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	w := doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"winnerproxy","scope":"user.read.as-service"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if errorCode(body) != CodeOAuthInvalidClientMetadata {
		t.Errorf("code = %q, want %q", errorCode(body), CodeOAuthInvalidClientMetadata)
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "super client secret") {
		t.Errorf("message must explain the poisoned-row conflict, got %q", msg)
	}

	// The root credential must appear nowhere in the response or the audit
	// trail (the Basic header of the request is never echoed).
	if strings.Contains(w.Body.String(), "super-secret") {
		t.Error("response body leaks the super secret")
	}
	if logged := buf.String(); !strings.Contains(logged, "denied: existing row carries the super client secret") {
		t.Errorf("audit log missing the conflict denial; got:\n%s", logged)
	} else if strings.Contains(logged, "super-secret") {
		t.Error("audit log leaks the super secret")
	}

	// Row untouched, still the only one.
	var row models.OAuth2Client
	if err := database.DB.Where("client_id = ?", "winnerproxy").First(&row).Error; err != nil {
		t.Fatalf("row disappeared: %v", err)
	}
	if row.ClientSecret != "super-secret" {
		t.Errorf("poisoned row must stay untouched, secret changed")
	}
	var n int64
	if err := database.DB.Model(&models.OAuth2Client{}).Where("client_id = ?", "winnerproxy").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 row, got %d", n)
	}
}

// TestRegisterClientCoexistsWithConfigSeed pins the config-first rule: a
// client already seeded from oauth2.service_clients is returned unchanged
// by a later self-registration call (no second row, seeded secret kept).
func TestRegisterClientCoexistsWithConfigSeed(t *testing.T) {
	r := setupRegisterClientEnv(t)
	seedSuperClientRow(t)

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

	w := doRegisterClientRequest(t, r, basicAuthHeader("test-super", "super-secret"),
		`{"client_name":"winnerproxy","scope":"user.read.as-service"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s; want 201", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["client_secret"] != "seeded-secret" {
		t.Errorf("client_secret = %v, want the seeded value", body["client_secret"])
	}

	var n int64
	if err := database.DB.Model(&models.OAuth2Client{}).Where("client_id = ?", "winnerproxy").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the seed row to stay unique, got %d rows", n)
	}
}
