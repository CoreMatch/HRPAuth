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
)

type TOTPController struct{}

func NewTOTPController() *TOTPController {
	return &TOTPController{}
}

type SetupTOTPRequest struct {
	Email string `json:"email"`
}

type VerifyTOTPRequest struct {
	LoginTicket string `json:"login_ticket"`
	Passcode    string `json:"passcode"`
}

type HasBeenEnabledRequest struct {
	UID string `json:"uid"`
}

func (tc *TOTPController) Generate(c *gin.Context) {
	secret := c.Query("secret")
	if secret == "" {
		respondError(c, http.StatusBadRequest, CodeTOTPSecretRequired, "Missing secret")
		return
	}

	otp := utils.GenerateTOTP(secret, 6, 30)
	respondOK(c, "TOTP generated successfully", gin.H{
		"otp": otp,
	})
}

func (tc *TOTPController) SetupTOTP(c *gin.Context) {
	var req SetupTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	email := req.Email
	if email == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing email")
		return
	}

	if !isValidEmail(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	authResult, ok := resolveSiteBearerAuth(c, "totp.setup", "totp.setup.as-service", false, "", email)
	if !ok {
		return
	}
	user := *authResult.User

	secret := utils.GenerateTOTPSecret(32)
	database.DB.Model(&user).Updates(map[string]interface{}{
		"totp": secret,
		"2FA":  true,
	})

	respondOK(c, "TOTP configured successfully", gin.H{
		"totpkey": secret,
	})
}

func (tc *TOTPController) VerifyTOTP(c *gin.Context) {
	var req VerifyTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if req.LoginTicket == "" || req.Passcode == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing login_ticket or passcode")
		return
	}

	ctx := context.Background()
	key := config.AppConfig.Redis.Prefix + "oauth2:login_ticket:" + req.LoginTicket
	raw, err := appredis.Client.Get(ctx, key).Result()
	if err != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidLoginTicket, "Invalid or expired login ticket")
		return
	}

	var ticket loginTicketPayload
	if err := json.Unmarshal([]byte(raw), &ticket); err != nil || ticket.UserID == "" {
		respondError(c, http.StatusUnauthorized, CodeInvalidLoginTicket, "Invalid or expired login ticket")
		return
	}

	var user models.User
	result := database.DB.Where("uuid = ?", ticket.UserID).First(&user)
	if result.Error != nil || user.TOTP == "" {
		respondError(c, http.StatusUnauthorized, CodeTOTPNotConfigured, "User not found or TOTP not configured")
		return
	}

	secret := user.TOTP
	period := int64(30)
	counter := time.Now().Unix() / period

	expected := utils.GenerateTOTPAtCounter(secret, counter, 6)

	if req.Passcode != expected {
		counterPrev := counter - 1
		otpPrev := utils.GenerateTOTPAtCounter(secret, counterPrev, 6)

		if req.Passcode != otpPrev {
			respondError(c, http.StatusUnauthorized, CodePasscodeInvalid, "Invalid passcode")
			return
		}
	}

	if err := appredis.Client.Del(ctx, key).Err(); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to consume login ticket")
		return
	}

	accessToken, refreshToken, err := services.NewOAuth2Service().IssueFirstPartyUserTokens(user.UUID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to issue OAuth2 token")
		return
	}

	scope := ""
	if accessToken != nil {
		var scopes []string
		_ = json.Unmarshal([]byte(accessToken.Scopes), &scopes)
		scope = strings.Join(scopes, " ")
	}

	respondOK(c, "TOTP verified successfully", gin.H{
		"access_token":  accessToken.AccessToken,
		"refresh_token": refreshToken.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    config.AppConfig.OAuth2.AccessTokenTTL,
		"scope":         scope,
	})
}

type Toggle2FARequest struct {
	Enabled bool `json:"enabled"`
}

func (tc *TOTPController) Toggle2FA(c *gin.Context) {
	var req Toggle2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	authResult, ok := resolveSiteBearerAuth(c, "totp.toggle", "totp.toggle.as-service", false, "", "")
	if !ok {
		return
	}
	user := *authResult.User

	if user.TOTP == "" && req.Enabled {
		respondError(c, http.StatusBadRequest, CodeTOTPNotConfigured, "TOTP not configured, cannot enable 2FA")
		return
	}

	database.DB.Model(&user).Update("2FA", req.Enabled)

	respondOK(c, "2FA status updated successfully", gin.H{
		"enabled": req.Enabled,
	})
}

func (tc *TOTPController) HasBeenEnabled(c *gin.Context) {
	var req HasBeenEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	uid := req.UID
	if uid == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing uid")
		return
	}
	authResult, ok := resolveSiteBearerAuth(c, "totp.status", "totp.status.as-service", false, uid, "")
	if !ok {
		return
	}
	user := *authResult.User

	enabled := 0
	if user.TwoFA && user.TOTP != "" {
		enabled = 1
	}

	respondOK(c, "TOTP status retrieved", gin.H{
		"enabled": enabled,
	})
}
