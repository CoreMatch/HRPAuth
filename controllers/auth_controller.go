package controllers

import (
        "fmt"
        "net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/clients"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"gorm.io/gorm"
)

var (
	emailRegex     = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	captchaRegex   = regexp.MustCompile(`^[a-zA-Z0-9]{4}$`)
	emailCodeRegex = regexp.MustCompile(`^[0-9]{4}$`)
)

type AuthController struct{}

func NewAuthController() *AuthController {
	return &AuthController{}
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Email        string `json:"email"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captcha_token"`
	CaptchaCode  string `json:"captcha_code"`
	MojangUUID   string `json:"mojang_uuid"`
}

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

func (ac *AuthController) VerifyCredentials(c *gin.Context) {
	internalKey := c.GetHeader("X-Internal-Key")
	if config.AppConfig.CoreAPI.InternalKey == "" || internalKey != config.AppConfig.CoreAPI.InternalKey {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal key"})
		return
	}

	var req struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	authService := services.NewAuthService()
	userInfo := authService.VerifyCredentials(req.Identifier, req.Password, true)
	if userInfo == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	c.JSON(http.StatusOK, userInfo)
}

// Register handles POST /register.
func (ac *AuthController) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	// Username / password length always enforced.
	if len(req.Username) < 3 {
		respondError(c, http.StatusBadRequest, CodeUsernameTooShort, "Username too short")
		return
	}
	if len(req.Password) < 6 {
		respondError(c, http.StatusBadRequest, CodePasswordTooShort, "Password too short")
		return
	}

	// Email required and validated.
	email := req.Email
	if !isValidEmail(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	// Captcha check when enabled.
	if config.AppConfig.Security.EnableCaptcha {
		captchaService := services.NewCaptchaService()
		if req.CaptchaToken == "" || req.CaptchaCode == "" {
			respondError(c, http.StatusBadRequest, CodeCaptchaInvalid, "Invalid or expired captcha")
			return
		}
		if !captchaService.Verify(req.CaptchaToken, req.CaptchaCode) {
			respondError(c, http.StatusBadRequest, CodeCaptchaInvalid, "Invalid or expired captcha")
			return
		}
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Password hashing failed")
		return
	}

	ip := c.ClientIP()
	now := time.Now()

	// Normal WebUI registration: enforce uniqueness, then create.
	var count int64
	database.DB.Model(&models.User{}).Where("email = ?", email).Count(&count)
	if count > 0 {
		respondError(c, http.StatusConflict, CodeEmailAlreadyRegistered, "Email already registered")
		return
	}
	database.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		respondError(c, http.StatusConflict, CodeUsernameAlreadyTaken, "Username already taken")
		return
	}

	var maxUID uint
	database.DB.Model(&models.User{}).Select("COALESCE(MAX(uid), 0)").Scan(&maxUID)
	newUID := maxUID + 1
	uuid := utils.GenerateUnsignedUUID()

	user := models.User{
		UID:        newUID,
		UUID:       uuid,
		Email:      email,
		Username:   req.Username,
		Password:   hash,
		IP:         ip,
		RegIP:      ip,
		LastSignAt: &now,
		RegisterAt: &now,
		Verified:   false,
	}

        if err := database.DB.Create(&user).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to create user")
		return
	}

	if req.MojangUUID != "" {
		yggClient := clients.NewYggdrasilClient()
		if err := yggClient.ClaimAccount(req.MojangUUID, user.UUID); err != nil {
                        if cleanupErr := database.DB.Unscoped().Delete(&user).Error; cleanupErr != nil {
                                respondError(c, http.StatusInternalServerError, CodeInternalError, fmt.Sprintf("Failed to bind Yggdrasil account and rollback local user: %v", cleanupErr))
                                return
                        }
                        respondError(c, http.StatusBadGateway, CodeInternalError, "Failed to bind Yggdrasil account")
                        return
		}
	}

	respondOK(c, "Register successful", gin.H{
		"uid":  user.UID,
		"uuid": user.UUID,
	})
}

type ForgotPasswordRequest struct {
	Email        string `json:"email"`
	CaptchaToken string `json:"captcha_token"`
	CaptchaCode  string `json:"captcha_code"`
}

type ResetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

func (ac *AuthController) Logout(c *gin.Context) {
	accessToken := bearerTokenFromRequest(c)
	if accessToken == "" {
		respondError(c, http.StatusUnauthorized, CodeOAuthLoginRequired, "missing bearer token")
		return
	}
	if err := services.NewOAuth2Service().RevokeAccessToken(accessToken); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to revoke token")
		return
	}
	respondOK(c, "Logout successful", nil)
}

// ForgotPassword handles POST /forgot-password.
func (ac *AuthController) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	email := strings.TrimSpace(req.Email)
	captchaCode := strings.TrimSpace(req.CaptchaCode)

	// 1. Strict Regex Validation
	if !emailRegex.MatchString(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email format")
		return
	}
	if !captchaRegex.MatchString(captchaCode) {
		respondError(c, http.StatusBadRequest, CodeCaptchaInvalid, "Invalid captcha format")
		return
	}

	// 2. Captcha Verification
	captchaService := services.NewCaptchaService()
	if req.CaptchaToken == "" || !captchaService.Verify(req.CaptchaToken, captchaCode) {
		respondError(c, http.StatusBadRequest, CodeCaptchaInvalid, "Invalid or expired captcha")
		return
	}

	// 3. Check if email exists
	var user models.User
	err := database.DB.Where("email = ?", email).First(&user).Error

	// Always respond success to prevent email enumeration
	successMsg := "If the email is registered, a verification code has been sent."

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			respondOK(c, successMsg, nil)
			return
		}
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Database error")
		return
	}

	// 4. Generate and send 4-digit code
	codeStore := services.NewVerificationCodeStore()
	emailService := services.NewEmailService()

	code := codeStore.Generate4DigitCode()
	if !codeStore.Store(email, code) {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to store verification code")
		return
	}

	subject := "HRPAuth - Password Recovery"
	message := "Your password recovery code is: " + code + "\n\nThe code is valid for 10 minutes. If you did not request this, please ignore this email."

	if err := emailService.SendMail(email, subject, message); err != nil {
		codeStore.Delete(email)
		respondError(c, http.StatusInternalServerError, CodeEmailSendFailed, "Failed to send recovery email")
		return
	}

	respondOK(c, successMsg, nil)
}

// ResetPassword handles POST /reset-password.
func (ac *AuthController) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	email := strings.TrimSpace(req.Email)
	code := strings.TrimSpace(req.Code)

	// 1. Strict Regex Validation
	if !emailRegex.MatchString(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email format")
		return
	}
	if !emailCodeRegex.MatchString(code) {
		respondError(c, http.StatusBadRequest, CodeVerificationCodeInvalid, "Invalid verification code format")
		return
	}

	if len(req.NewPassword) < 6 {
		respondError(c, http.StatusBadRequest, CodePasswordTooShort, "Password too short")
		return
	}

	// 2. Verify Email Code
	codeStore := services.NewVerificationCodeStore()
	storedCode, found := codeStore.Get(email)
	if !found || storedCode != code {
		respondError(c, http.StatusBadRequest, CodeVerificationCodeInvalid, "Invalid or expired verification code")
		return
	}

	// 3. Update Password
	hash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Password hashing failed")
		return
	}

	result := database.DB.Model(&models.User{}).Where("email = ?", email).Update("password", hash)
	if result.Error != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to update password")
		return
	}

	if result.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, CodeUserNotFound, "User not found")
		return
	}

	// 4. Cleanup
	codeStore.Delete(email)

	respondOK(c, "Password reset successful", nil)
}
