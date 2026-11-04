package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupClaimTestEnv prepares an in-memory sqlite users table, points
// database.DB at it, and configures a manage token so the ops-level auth
// check passes with a known bearer. It returns a router serving only the
// claim endpoint.
func setupClaimTestEnv(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "claim-test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	database.DB = db

	config.AppConfig = &config.Config{}
	config.AppConfig.Manage.Token = "test-manage-token"

	r := gin.New()
	r.POST("/internal/claim-user", NewAuthController().ClaimUserInternal)
	return r
}

func doClaimRequest(t *testing.T, r *gin.Engine, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/claim-user", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func claimEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return envelope
}

func TestClaimUserInternalCreatesVerifiedUser(t *testing.T) {
	r := setupClaimTestEnv(t)

	w := doClaimRequest(t, r, "test-manage-token",
		`{"username":"steve","email":"Steve@Example.com","password":"secret1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var user models.User
	if err := database.DB.Where("username = ?", "steve").First(&user).Error; err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
	if user.Email != "steve@example.com" {
		t.Fatalf("email not normalized: %q", user.Email)
	}
	if !user.Verified {
		t.Fatal("claimed user must be verified=true")
	}
	if user.Password == "secret1" {
		t.Fatal("password must be hashed")
	}
}

func TestClaimUserInternalRejectsDuplicateEmail(t *testing.T) {
	r := setupClaimTestEnv(t)

	if err := database.DB.Create(&models.User{
		UID: 1, UUID: "aaaa", Email: "taken@example.com", Username: "other", Password: "x",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	w := doClaimRequest(t, r, "test-manage-token",
		`{"username":"steve","email":"taken@example.com","password":"secret1"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", w.Code, w.Body.String())
	}
	if code, _ := claimEnvelope(t, w)["code"].(string); code != CodeEmailAlreadyRegistered {
		t.Fatalf("code = %q, want %q", code, CodeEmailAlreadyRegistered)
	}
}

func TestClaimUserInternalRejectsTakenUsernameWithOtherEmail(t *testing.T) {
	r := setupClaimTestEnv(t)

	if err := database.DB.Create(&models.User{
		UID: 1, UUID: "aaaa", Email: "owner@example.com", Username: "steve", Password: "x",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	w := doClaimRequest(t, r, "test-manage-token",
		`{"username":"steve","email":"other@example.com","password":"secret1"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", w.Code, w.Body.String())
	}
	if code, _ := claimEnvelope(t, w)["code"].(string); code != CodeUsernameAlreadyBound {
		t.Fatalf("code = %q, want %q", code, CodeUsernameAlreadyBound)
	}
}

func TestClaimUserInternalIsIdempotentForSameUsernameAndEmail(t *testing.T) {
	r := setupClaimTestEnv(t)

	first := doClaimRequest(t, r, "test-manage-token",
		`{"username":"steve","email":"steve@example.com","password":"secret1"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first claim status = %d, body = %s", first.Code, first.Body.String())
	}

	second := doClaimRequest(t, r, "test-manage-token",
		`{"username":"steve","email":"steve@example.com","password":"secret1"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("retry status = %d, body = %s", second.Code, second.Body.String())
	}

	var count int64
	database.DB.Model(&models.User{}).Where("username = ?", "steve").Count(&count)
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}
}

func TestClaimUserInternalRequiresOpsAuth(t *testing.T) {
	r := setupClaimTestEnv(t)

	w := doClaimRequest(t, r, "", `{"username":"steve","email":"s@example.com","password":"secret1"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestClaimUserInternalValidation(t *testing.T) {
	r := setupClaimTestEnv(t)

	cases := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"short username", `{"username":"ab","email":"a@example.com","password":"secret1"}`, CodeUsernameTooShort},
		{"invalid email", `{"username":"steve","email":"not-an-email","password":"secret1"}`, CodeInvalidEmail},
		{"short password", `{"username":"steve","email":"a@example.com","password":"123"}`, CodePasswordTooShort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doClaimRequest(t, r, "test-manage-token", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
			if code, _ := claimEnvelope(t, w)["code"].(string); code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}
