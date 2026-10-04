package controllers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	appredis "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type Email2FAController struct {
	emailService *services.EmailService
	codeStore    *services.VerificationCodeStore
}

func NewEmail2FAController() *Email2FAController {
	return &Email2FAController{
		emailService: services.NewEmailService(),
		codeStore:    services.NewVerificationCodeStore(),
	}
}

type Email2FASendRequest struct {
	LoginTicket string `json:"login_ticket"`
}

type Email2FAVerifyRequest struct {
	LoginTicket string `json:"login_ticket"`
	Code        string `json:"code"`
}

type Email2FAToggleRequest struct {
	Enabled bool `json:"enabled"`
}

func (ec *Email2FAController) SendCode(c *gin.Context) {
	var req Email2FASendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if req.LoginTicket == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing login_ticket")
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
	if result.Error != nil || user.Email == "" {
		respondError(c, http.StatusUnauthorized, CodeUserNotFound, "User not found or email not set")
		return
	}

	if !user.Email2FAEnabled {
		respondError(c, http.StatusBadRequest, CodeEmail2FANotConfigured, "Email 2FA not enabled for this user")
		return
	}

	// Use the same code store as email verification for simplicity, but maybe a separate one would be better?
	// The current codeStore uses email as key. Since login_ticket is active, we can use email as key.

	existingCode, found := ec.codeStore.Get(user.Email)
	if found && existingCode != "" {
		respondError(c, http.StatusTooManyRequests, CodeVerificationCodeAlreadySent, "Verification code already sent, please wait")
		return
	}

	code := ec.codeStore.GenerateCode()
	if !ec.codeStore.Store(user.Email, code) {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to store verification code")
		return
	}

	subject := "HRPAuth - 2FA Verification Code"
	message := "Your 2FA verification code is: " + code + "\n\nThis code is valid for 10 minutes. If you did not request this, please change your password immediately."

	err = ec.emailService.SendMail(user.Email, subject, message)
	if err != nil {
		ec.codeStore.Delete(user.Email)
		respondError(c, http.StatusInternalServerError, CodeEmailSendFailed, err.Error())
		return
	}

	respondOK(c, "Verification code sent to your email", nil)
}

func (ec *Email2FAController) VerifyCode(c *gin.Context) {
	var req Email2FAVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if req.LoginTicket == "" || req.Code == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing login_ticket or code")
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
	if result.Error != nil || user.Email == "" {
		respondError(c, http.StatusUnauthorized, CodeUserNotFound, "User not found or email not set")
		return
	}

	storedCode, found := ec.codeStore.Get(user.Email)
	if !found || storedCode != req.Code {
		respondError(c, http.StatusUnauthorized, CodeVerificationCodeInvalid, "Invalid or expired verification code")
		return
	}

	ec.codeStore.Delete(user.Email)

	if err := appredis.Client.Del(ctx, key).Err(); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to consume login ticket")
		return
	}

	issueAndRespondFirstPartyUserTokens(c, user.UUID, "Email 2FA verified successfully", nil)
}

func (ec *Email2FAController) Toggle(c *gin.Context) {
	var req Email2FAToggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	authResult, ok := resolveSiteBearerAuth(c, "email-2fa.toggle", "email-2fa.toggle.as-service", false, "", "")
	if !ok {
		return
	}
	user := *authResult.User

	if req.Enabled && !user.Verified {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Email must be verified before enabling Email 2FA")
		return
	}

	database.DB.Model(&user).Update("email_2fa_enabled", req.Enabled)

	respondOK(c, "Email 2FA status updated successfully", gin.H{
		"enabled": req.Enabled,
	})
}

func (ec *Email2FAController) Status(c *gin.Context) {
	uid := c.Query("uid")
	if uid == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing uid")
		return
	}

	authResult, ok := resolveSiteBearerAuth(c, "email-2fa.status", "email-2fa.status.as-service", false, uid, "")
	if !ok {
		return
	}
	user := *authResult.User

	respondOK(c, "Email 2FA status retrieved", gin.H{
		"enabled":  user.Email2FAEnabled,
		"verified": user.Verified,
	})
}
