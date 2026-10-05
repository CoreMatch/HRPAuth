package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	appredis "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"gorm.io/gorm"
)

type UserSecurityController struct {
	authService     *services.AuthService
	emailService    *services.EmailService
	webauthnService *services.WebAuthnService
	codeStore       *services.VerificationCodeStore
}

func NewUserSecurityController() *UserSecurityController {
	return &UserSecurityController{
		authService:     services.NewAuthService(),
		emailService:    services.NewEmailService(),
		webauthnService: services.NewWebAuthnService(),
		codeStore:       services.NewVerificationCodeStore(),
	}
}

type ChangeEmailRequest struct {
	NewEmail    string `json:"new_email"`
	TOTPCode    string `json:"totp_code"`
	EmailCode   string `json:"email_code"`
	RecoveryKey string `json:"recovery_key"`
	WebAuthn    struct {
		FlowID     string          `json:"flow_id"`
		Credential json.RawMessage `json:"credential"`
	} `json:"webauthn"`
}

type RecoveryKeyRequest struct {
	TOTPCode    string `json:"totp_code"`
	EmailCode   string `json:"email_code"`
	RecoveryKey string `json:"recovery_key"`
	WebAuthn    struct {
		FlowID     string          `json:"flow_id"`
		Credential json.RawMessage `json:"credential"`
	} `json:"webauthn"`
}

func (usc *UserSecurityController) SendChangeEmailCode(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	if user.Email == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "User has no email set")
		return
	}

	existingCode, found := usc.codeStore.Get(user.Email)
	if found && existingCode != "" {
		respondError(c, http.StatusTooManyRequests, CodeVerificationCodeAlreadySent, "Verification code already sent, please wait")
		return
	}

	code := usc.codeStore.GenerateCode()
	if !usc.codeStore.Store(user.Email, code) {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to store verification code")
		return
	}

	subject := "HRPAuth - Email Change Verification Code"
	message := "Your verification code for changing email is: " + code + "\n\nIf you did not request this, please secure your account immediately."

	err := usc.emailService.SendMail(user.Email, subject, message)
	if err != nil {
		usc.codeStore.Delete(user.Email)
		respondError(c, http.StatusInternalServerError, CodeEmailSendFailed, err.Error())
		return
	}

	respondOK(c, "Verification code sent to your current email", nil)
}

func (usc *UserSecurityController) BeginWebAuthnSudo(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	options, flowID, err := usc.webauthnService.BeginSudoLogin(user.UUID)
	if err != nil {
		if strings.Contains(err.Error(), "webauthn_not_configured") {
			respondError(c, http.StatusBadRequest, CodeWebAuthnNotConfigured, "WebAuthn not configured for this user")
		} else {
			respondError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		}
		return
	}

	respondOK(c, "WebAuthn sudo auth started", gin.H{
		"flow_id": flowID,
		"options": options,
	})
}

func (usc *UserSecurityController) ChangeEmail(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	var req ChangeEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if !isValidEmail(req.NewEmail) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid new email format")
		return
	}

	// Check if email already taken
	var count int64
	database.DB.Model(&models.User{}).Where("email = ? AND uuid != ?", strings.TrimSpace(strings.ToLower(req.NewEmail)), user.UUID).Count(&count)
	if count > 0 {
		respondError(c, http.StatusBadRequest, CodeEmailAlreadyRegistered, "Email already registered")
		return
	}

	// Verify factors
	anyFactorVerified := usc.verifyAnyFactor(c, user, RecoveryKeyRequest{
		TOTPCode:    req.TOTPCode,
		EmailCode:   req.EmailCode,
		RecoveryKey: req.RecoveryKey,
		WebAuthn:    req.WebAuthn,
	})

	// Final Check
	if !anyFactorVerified {
		if !c.IsAborted() {
			respondError(c, http.StatusForbidden, CodeInsufficientAuthLevel, "Insufficient verification factors. Provide at least one 2FA factor (TOTP, Email Code, WebAuthn, or Recovery Key).")
		}
		return
	}

	// Start update
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		// Update Email
		updates := map[string]interface{}{
			"email":    strings.TrimSpace(strings.ToLower(req.NewEmail)),
			"verified": false,
		}

		if err := tx.Model(user).Updates(updates).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to update user information")
		return
	}

	respondOK(c, "Email updated successfully", gin.H{
		"email": req.NewEmail,
	})
}

func (usc *UserSecurityController) CreateRecoveryKey(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	if user.RecoveryKeyEnabled {
		respondError(c, http.StatusBadRequest, CodeRecoveryKeyAlreadyConfigured, "Recovery key already configured. Use regenerate instead.")
		return
	}

	key := usc.generateRecoveryKey()
	hashedKey, _ := utils.HashPassword(key)

	if err := database.DB.Model(user).Updates(map[string]interface{}{
		"recovery_key":         hashedKey,
		"recovery_key_enabled": true,
	}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to store recovery key")
		return
	}

	respondOK(c, "Recovery key created successfully. Please store it securely.", gin.H{
		"recovery_key": key,
	})
}

func (usc *UserSecurityController) RegenerateRecoveryKey(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	var req RecoveryKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if !usc.verifyAnyFactor(c, user, req) {
		if !c.IsAborted() {
			respondError(c, http.StatusForbidden, CodeInsufficientAuthLevel, "Insufficient verification factors.")
		}
		return
	}

	key := usc.generateRecoveryKey()
	hashedKey, _ := utils.HashPassword(key)

	if err := database.DB.Model(user).Updates(map[string]interface{}{
		"recovery_key":         hashedKey,
		"recovery_key_enabled": true,
	}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to update recovery key")
		return
	}

	respondOK(c, "Recovery key regenerated successfully", gin.H{
		"recovery_key": key,
	})
}

func (usc *UserSecurityController) RevokeRecoveryKey(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	var req RecoveryKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if !usc.verifyAnyFactor(c, user, req) {
		if !c.IsAborted() {
			respondError(c, http.StatusForbidden, CodeInsufficientAuthLevel, "Insufficient verification factors.")
		}
		return
	}

	if err := database.DB.Model(user).Updates(map[string]interface{}{
		"recovery_key":         "",
		"recovery_key_enabled": false,
	}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to revoke recovery key")
		return
	}

	respondOK(c, "Recovery key revoked successfully", nil)
}

func (usc *UserSecurityController) VerifyRecoveryKey(c *gin.Context) {
	var req struct {
		LoginTicket string `json:"login_ticket"`
		RecoveryKey string `json:"recovery_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if req.LoginTicket == "" || req.RecoveryKey == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing login_ticket or recovery_key")
		return
	}

	ctx := context.Background()
	key := config.AppConfig.Redis.Prefix + "oauth2:login_ticket:" + req.LoginTicket
	raw, err := appredis.Client.Get(ctx, key).Result()
	if err != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidLoginTicket, "Invalid or expired login ticket")
		return
	}

	var ticket LoginTicketPayload
	if err := json.Unmarshal([]byte(raw), &ticket); err != nil || ticket.UserID == "" {
		respondError(c, http.StatusUnauthorized, CodeInvalidLoginTicket, "Invalid or expired login ticket")
		return
	}

	var user models.User
	result := database.DB.Where("uuid = ?", ticket.UserID).First(&user)
	if result.Error != nil || !user.RecoveryKeyEnabled || user.RecoveryKey == "" {
		respondError(c, http.StatusUnauthorized, CodeRecoveryKeyNotConfigured, "User not found or recovery key not configured")
		return
	}

	if !utils.CheckPasswordHash(req.RecoveryKey, user.RecoveryKey) {
		respondError(c, http.StatusUnauthorized, CodePasscodeInvalid, "Invalid recovery key")
		return
	}

	if err := appredis.Client.Del(ctx, key).Err(); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to consume login ticket")
		return
	}

	issueAndRespondFirstPartyUserTokens(c, user.UUID, "Recovery key verified successfully", nil)
}

func (usc *UserSecurityController) RecoveryKeyStatus(c *gin.Context) {
	var req struct {
		UID string `json:"uid"`
	}
	uid := c.Query("uid")
	if uid == "" {
		if err := c.ShouldBindJSON(&req); err == nil {
			uid = req.UID
		}
	}

	authResult, ok := resolveSiteBearerAuth(c, "user.security.manage", "user.security.manage.as-service", false, uid, "")
	if !ok {
		return
	}
	user := authResult.User

	respondOK(c, "Recovery key status retrieved", gin.H{
		"enabled": user.RecoveryKeyEnabled,
	})
}

func (usc *UserSecurityController) generateRecoveryKey() string {
	// Generate a key like ABCD-EFGH-IJKL-MNOP
	k1 := strings.ToUpper(utils.GenerateRandomToken(2))
	k2 := strings.ToUpper(utils.GenerateRandomToken(2))
	k3 := strings.ToUpper(utils.GenerateRandomToken(2))
	k4 := strings.ToUpper(utils.GenerateRandomToken(2))
	return k1 + "-" + k2 + "-" + k3 + "-" + k4
}

func (usc *UserSecurityController) verifyAnyFactor(c *gin.Context, user *models.User, req RecoveryKeyRequest) bool {
	anyFactorVerified := false

	// 1. TOTP
	if req.TOTPCode != "" {
		if user.TwoFA && user.TOTP != "" {
			if usc.verifyTOTP(user.TOTP, req.TOTPCode) {
				anyFactorVerified = true
			} else {
				respondError(c, http.StatusUnauthorized, CodePasscodeInvalid, "Invalid TOTP code")
				c.Abort()
				return false
			}
		} else {
			respondError(c, http.StatusBadRequest, CodeTOTPNotConfigured, "TOTP not configured")
			c.Abort()
			return false
		}
	}

	// 2. Email Code
	if req.EmailCode != "" {
		storedCode, found := usc.codeStore.Get(user.Email)
		if found && storedCode == req.EmailCode {
			anyFactorVerified = true
			usc.codeStore.Delete(user.Email)
		} else {
			respondError(c, http.StatusUnauthorized, CodeVerificationCodeInvalid, "Invalid or expired email verification code")
			c.Abort()
			return false
		}
	}

	// 3. WebAuthn
	if req.WebAuthn.FlowID != "" && len(req.WebAuthn.Credential) > 0 {
		_, err := usc.webauthnService.FinishLogin(req.WebAuthn.FlowID, req.WebAuthn.Credential)
		if err == nil {
			anyFactorVerified = true
		} else {
			respondError(c, http.StatusUnauthorized, CodeWebAuthnVerificationFailed, "WebAuthn verification failed")
			c.Abort()
			return false
		}
	}

	// 4. Recovery Key
	if req.RecoveryKey != "" {
		if user.RecoveryKeyEnabled && user.RecoveryKey != "" {
			if utils.CheckPasswordHash(req.RecoveryKey, user.RecoveryKey) {
				anyFactorVerified = true
			} else {
				respondError(c, http.StatusUnauthorized, CodePasscodeInvalid, "Invalid recovery key")
				c.Abort()
				return false
			}
		} else {
			respondError(c, http.StatusBadRequest, CodeRecoveryKeyNotConfigured, "Recovery key not configured")
			c.Abort()
			return false
		}
	}

	return anyFactorVerified
}

func (usc *UserSecurityController) verifyTOTP(secret string, code string) bool {
	period := int64(30)
	counter := time.Now().Unix() / period

	if utils.GenerateTOTPAtCounter(secret, counter, 6) == code {
		return true
	}
	// Allow 1 step drift
	if utils.GenerateTOTPAtCounter(secret, counter-1, 6) == code {
		return true
	}
	return false
}
