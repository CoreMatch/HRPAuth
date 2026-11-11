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

// setupUserExistsEnv prepares an in-memory sqlite users table, points
// database.DB at it, and configures a manage token so the ops-level auth
// check passes with a known bearer. It returns a router serving only the
// user-exists endpoint.
func setupUserExistsEnv(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "user-exists-test.db")), &gorm.Config{})
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
	r.POST("/internal/user-exists", NewAuthController().UserExists)
	return r
}

func doUserExists(t *testing.T, r *gin.Engine, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/user-exists", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func userExistsFlag(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	var envelope struct {
		Data struct {
			Exists bool `json:"exists"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return envelope.Data.Exists
}

func TestUserExistsReportsRegisteredEmail(t *testing.T) {
	r := setupUserExistsEnv(t)

	if err := database.DB.Create(&models.User{
		UID: 1, UUID: "aaaa", Email: "steve@example.com", Username: "steve", Password: "x",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	w := doUserExists(t, r, "test-manage-token", `{"email":"steve@example.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !userExistsFlag(t, w) {
		t.Fatalf("exists = false, want true; body = %s", w.Body.String())
	}
}

func TestUserExistsNormalizesCaseAndWhitespace(t *testing.T) {
	r := setupUserExistsEnv(t)

	if err := database.DB.Create(&models.User{
		UID: 1, UUID: "aaaa", Email: "steve@example.com", Username: "steve", Password: "x",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	w := doUserExists(t, r, "test-manage-token", `{"email":"  Steve@Example.com  "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !userExistsFlag(t, w) {
		t.Fatalf("exists = false for mixed-case input, want true; body = %s", w.Body.String())
	}
}

func TestUserExistsReportsUnknownEmailFalse(t *testing.T) {
	r := setupUserExistsEnv(t)

	w := doUserExists(t, r, "test-manage-token", `{"email":"nobody@example.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if userExistsFlag(t, w) {
		t.Fatalf("exists = true, want false; body = %s", w.Body.String())
	}
}

func TestUserExistsRequiresOpsAuth(t *testing.T) {
	r := setupUserExistsEnv(t)

	w := doUserExists(t, r, "", `{"email":"steve@example.com"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", w.Code, w.Body.String())
	}
}

func TestUserExistsValidation(t *testing.T) {
	r := setupUserExistsEnv(t)

	cases := []struct {
		name string
		body string
	}{
		{"invalid email", `{"email":"not-an-email"}`},
		{"empty body", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doUserExists(t, r, "test-manage-token", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
		})
	}
}
