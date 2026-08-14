package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
)

type TOTPController struct{}

func NewTOTPController() *TOTPController {
	return &TOTPController{}
}

type SetupTOTPRequest struct {
	Email    string `json:"email"`
	RemToken string `json:"remtoken"`
	AuthType string `json:"auth_type"`
}

type VerifyTOTPRequest struct {
	Email    string `json:"email"`
	Passcode string `json:"passcode"`
}

type HasBeenEnabledRequest struct {
	UID      string `json:"uid"`
	RT       string `json:"rt"`
	AuthType string `json:"auth_type"`
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
	remToken := req.RemToken

	if email == "" || remToken == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing email or remtoken")
		return
	}

	if !isValidEmail(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	// M-T is treated as a remtoken: when the caller declares auth_type="manage"
	// with the configured M-T, the remember_token check is skipped so the
	// operator can configure TOTP for any user by email.
	isManage, authOK := isManageRequest(c, remToken, req.AuthType)
	if !authOK {
		respondError(c, http.StatusUnauthorized, CodeInvalidAuthTypeOrToken, "Invalid auth type or token")
		return
	}

	query := database.DB.Where("email = ?", email)
	if !isManage {
		query = query.Where("remember_token = ?", remToken)
	}

	var user models.User
	result := query.First(&user)
	if result.Error != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidCredentials, "Invalid email or remtoken")
		return
	}

	secret := utils.GenerateTOTPSecret(32)
	database.DB.Model(&user).Update("totp", secret)

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

	email := req.Email
	passcode := req.Passcode

	if email == "" || passcode == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing email or passcode")
		return
	}

	if !isValidEmail(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	var user models.User
	result := database.DB.Where("email = ?", email).First(&user)
	if result.Error != nil || user.TOTP == "" {
		respondError(c, http.StatusUnauthorized, CodeTOTPNotConfigured, "User not found or TOTP not configured")
		return
	}

	secret := user.TOTP
	period := int64(30)
	counter := time.Now().Unix() / period

	expected := utils.GenerateTOTPAtCounter(secret, counter, 6)

	if passcode != expected {
		counterPrev := counter - 1
		otpPrev := utils.GenerateTOTPAtCounter(secret, counterPrev, 6)

		if passcode != otpPrev {
			respondError(c, http.StatusUnauthorized, CodePasscodeInvalid, "Invalid passcode")
			return
		}
	}

	rt := user.RememberToken
	if rt == "" {
		rt = utils.GenerateRandomToken(32)
		database.DB.Model(&user).Update("remember_token", rt)
	}

	respondOK(c, "TOTP verified successfully", gin.H{
		"email": email,
		"rt":    rt,
	})
}

func (tc *TOTPController) HasBeenEnabled(c *gin.Context) {
	var req HasBeenEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	uid := req.UID
	rt := req.RT

	if uid == "" || rt == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing uid or rt")
		return
	}

	// M-T is treated as a remtoken: when the caller declares auth_type="manage"
	// with the configured M-T, the remember_token check is skipped so the
	// operator can query any user by uid.
	isManage, authOK := isManageRequest(c, rt, req.AuthType)
	if !authOK {
		respondError(c, http.StatusUnauthorized, CodeInvalidAuthTypeOrToken, "Invalid auth type or token")
		return
	}

	query := database.DB.Where("uid = ?", uid)
	if !isManage {
		query = query.Where("remember_token = ?", rt)
	}

	var user models.User
	result := query.First(&user)
	if result.Error != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidRememberToken, "Invalid uid or rt")
		return
	}

	enabled := 0
	if user.TOTP != "" {
		enabled = 1
	}

	respondOK(c, "TOTP status retrieved", gin.H{
		"enabled": enabled,
	})
}
