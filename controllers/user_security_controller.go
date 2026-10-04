package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
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
	NewEmail        string `json:"new_email"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	TOTPCode        string `json:"totp_code"`
	EmailCode       string `json:"email_code"`
	WebAuthn        struct {
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
	factorsCount := 0
	passwordVerified := false

	// 1. Password
	if req.CurrentPassword != "" {
		verifiedUser := usc.authService.VerifyCredentials(user.Email, req.CurrentPassword, false)
		if verifiedUser != nil && verifiedUser.UUID == user.UUID {
			factorsCount++
			passwordVerified = true
		} else {
			respondError(c, http.StatusUnauthorized, CodeInvalidCredentials, "Current password incorrect")
			return
		}
	}

	// 2. TOTP
	if req.TOTPCode != "" {
		if user.TwoFA && user.TOTP != "" {
			if usc.verifyTOTP(user.TOTP, req.TOTPCode) {
				factorsCount++
			} else {
				respondError(c, http.StatusUnauthorized, CodePasscodeInvalid, "Invalid TOTP code")
				return
			}
		} else {
			respondError(c, http.StatusBadRequest, CodeTOTPNotConfigured, "TOTP not configured")
			return
		}
	}

	// 3. Email Code
	if req.EmailCode != "" {
		storedCode, found := usc.codeStore.Get(user.Email)
		if found && storedCode == req.EmailCode {
			factorsCount++
			usc.codeStore.Delete(user.Email)
		} else {
			respondError(c, http.StatusUnauthorized, CodeVerificationCodeInvalid, "Invalid or expired email verification code")
			return
		}
	}

	// 4. WebAuthn
	if req.WebAuthn.FlowID != "" && len(req.WebAuthn.Credential) > 0 {
		_, err := usc.webauthnService.FinishLogin(req.WebAuthn.FlowID, req.WebAuthn.Credential)
		if err == nil {
			factorsCount++
		} else {
			respondError(c, http.StatusUnauthorized, CodeWebAuthnVerificationFailed, "WebAuthn verification failed")
			return
		}
	}

	// Final Check
	// Path A: Password (1) + 1 factor (1) = 2
	// Path B: 2 factors = 2
	if factorsCount < 2 {
		respondError(c, http.StatusForbidden, CodeInsufficientAuthLevel, "Insufficient verification factors. Provide password + 1 factor, or 2 factors.")
		return
	}

	// Start update
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		// Update Email
		updates := map[string]interface{}{
			"email":    strings.TrimSpace(strings.ToLower(req.NewEmail)),
			"verified": false,
		}

		// Update Password if Path B and new password provided
		if !passwordVerified && req.NewPassword != "" {
			if len(req.NewPassword) < 6 {
				return errors.New("new password too short")
			}
			hashed, err := utils.HashPassword(req.NewPassword)
			if err != nil {
				return err
			}
			updates["password"] = hashed
		}

		if err := tx.Model(user).Updates(updates).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		if err.Error() == "new password too short" {
			respondError(c, http.StatusBadRequest, CodePasswordTooShort, err.Error())
		} else {
			respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to update user information")
		}
		return
	}

	respondOK(c, "Email updated successfully", gin.H{
		"email":            req.NewEmail,
		"password_updated": !passwordVerified && req.NewPassword != "",
	})
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
