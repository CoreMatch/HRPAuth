package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
)

// setupVerifyFactorTest prepares a UserSecurityController with a file-backed
// verification code store rooted in a temporary directory, so tests never
// touch the real configured storage.
func setupVerifyFactorTest(t *testing.T) (*UserSecurityController, *services.VerificationCodeStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	if config.AppConfig == nil {
		config.AppConfig = &config.Config{}
	}
	config.AppConfig.VerificationCode = config.VerificationCodeConfig{
		CodeTTL:    600,
		StorageDir: t.TempDir(),
	}

	store := services.NewVerificationCodeStore()
	usc := &UserSecurityController{codeStore: store}
	return usc, store
}

func newVerifyFactorContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

func responseErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse error response %q: %v", w.Body.String(), err)
	}
	return body.Code
}

// Regression test: with Email 2FA disabled, an email code must NOT satisfy the
// second-factor gate, even when a valid code exists in the (shared) code store.
// Before the fix, verifyAnyFactor accepted any stored code regardless of the
// Email2FAEnabled flag.
func TestVerifyAnyFactorRejectsEmailCodeWhenEmail2FADisabled(t *testing.T) {
	usc, store := setupVerifyFactorTest(t)

	user := &models.User{
		Email:           "user@example.com",
		Email2FAEnabled: false,
	}
	if !store.Store(user.Email, "123456") {
		t.Fatal("failed to store verification code")
	}

	c, w := newVerifyFactorContext()
	ok := usc.verifyAnyFactor(c, user, RecoveryKeyRequest{EmailCode: "123456"})

	if ok {
		t.Fatal("email code must not verify when Email 2FA is disabled")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if code := responseErrorCode(t, w); code != CodeEmail2FANotConfigured {
		t.Fatalf("expected error code %q, got %q", CodeEmail2FANotConfigured, code)
	}
	// The rejected attempt must not consume a code that may belong to another flow.
	if stored, found := store.Get(user.Email); !found || stored != "123456" {
		t.Fatal("stored code must remain untouched when Email 2FA is disabled")
	}
}

// With Email 2FA enabled, a correct email code must satisfy the gate and be
// consumed.
func TestVerifyAnyFactorAcceptsEmailCodeWhenEmail2FAEnabled(t *testing.T) {
	usc, store := setupVerifyFactorTest(t)

	user := &models.User{
		Email:           "user@example.com",
		Email2FAEnabled: true,
	}
	if !store.Store(user.Email, "123456") {
		t.Fatal("failed to store verification code")
	}

	c, _ := newVerifyFactorContext()
	ok := usc.verifyAnyFactor(c, user, RecoveryKeyRequest{EmailCode: "123456"})

	if !ok {
		t.Fatal("email code must verify when Email 2FA is enabled")
	}
	if _, found := store.Get(user.Email); found {
		t.Fatal("stored code must be consumed after successful verification")
	}
}

// With Email 2FA enabled, a wrong code must still be rejected.
func TestVerifyAnyFactorRejectsWrongEmailCodeWhenEmail2FAEnabled(t *testing.T) {
	usc, store := setupVerifyFactorTest(t)

	user := &models.User{
		Email:           "user@example.com",
		Email2FAEnabled: true,
	}
	if !store.Store(user.Email, "123456") {
		t.Fatal("failed to store verification code")
	}

	c, w := newVerifyFactorContext()
	ok := usc.verifyAnyFactor(c, user, RecoveryKeyRequest{EmailCode: "000000"})

	if ok {
		t.Fatal("wrong email code must not verify")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
	if code := responseErrorCode(t, w); code != CodeVerificationCodeInvalid {
		t.Fatalf("expected error code %q, got %q", CodeVerificationCodeInvalid, code)
	}
}

// Guard against collateral damage: the recovery key branch must keep working
// independently of the email branch fix.
func TestVerifyAnyFactorRecoveryKeyBranchUnaffected(t *testing.T) {
	usc, _ := setupVerifyFactorTest(t)

	plainKey := "ABCD-EFGH-IJKL-MNOP"
	hashedKey, err := utils.HashPassword(plainKey)
	if err != nil {
		t.Fatalf("failed to hash recovery key: %v", err)
	}

	t.Run("valid recovery key passes", func(t *testing.T) {
		user := &models.User{
			Email:              "user@example.com",
			Email2FAEnabled:    false,
			RecoveryKey:        hashedKey,
			RecoveryKeyEnabled: true,
		}
		c, _ := newVerifyFactorContext()
		if ok := usc.verifyAnyFactor(c, user, RecoveryKeyRequest{RecoveryKey: plainKey}); !ok {
			t.Fatal("valid recovery key must verify")
		}
	})

	t.Run("recovery key not configured is rejected", func(t *testing.T) {
		user := &models.User{
			Email:              "user@example.com",
			Email2FAEnabled:    false,
			RecoveryKeyEnabled: false,
		}
		c, w := newVerifyFactorContext()
		if ok := usc.verifyAnyFactor(c, user, RecoveryKeyRequest{RecoveryKey: plainKey}); ok {
			t.Fatal("recovery key must not verify when not configured")
		}
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
		if code := responseErrorCode(t, w); code != CodeRecoveryKeyNotConfigured {
			t.Fatalf("expected error code %q, got %q", CodeRecoveryKeyNotConfigured, code)
		}
	})
}
