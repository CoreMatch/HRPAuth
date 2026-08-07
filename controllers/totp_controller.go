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
		c.String(http.StatusBadRequest, "Missing secret")
		return
	}

	otp := utils.GenerateTOTP(secret, 6, 30)
	c.String(http.StatusOK, otp)
}

func (tc *TOTPController) SetupTOTP(c *gin.Context) {
	var req SetupTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	email := req.Email
	remToken := req.RemToken

	if email == "" || remToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Missing email or remtoken",
		})
		return
	}

	if !isValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid email",
		})
		return
	}

	// M-T is treated as a remtoken: when the caller declares auth_type="manage"
	// with the configured M-T, the remember_token check is skipped so the
	// operator can configure TOTP for any user by email.
	isManage, authOK := isManageRequest(c, remToken, req.AuthType)
	if !authOK {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid auth type or token",
		})
		return
	}

	query := database.DB.Where("email = ?", email)
	if !isManage {
		query = query.Where("remember_token = ?", remToken)
	}

	var user models.User
	result := query.First(&user)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid email or remtoken",
		})
		return
	}

	secret := utils.GenerateTOTPSecret(32)
	database.DB.Model(&user).Update("totp", secret)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"totpkey": secret,
	})
}

func (tc *TOTPController) VerifyTOTP(c *gin.Context) {
	var req VerifyTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	email := req.Email
	passcode := req.Passcode

	if email == "" || passcode == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Missing email or passcode",
		})
		return
	}

	if !isValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid email",
		})
		return
	}

	var user models.User
	result := database.DB.Where("email = ?", email).First(&user)
	if result.Error != nil || user.TOTP == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "User not found or TOTP not configured",
		})
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
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "Invalid passcode",
			})
			return
		}
	}

	rt := user.RememberToken
	if rt == "" {
		rt = utils.GenerateRandomToken(32)
		database.DB.Model(&user).Update("remember_token", rt)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"email":   email,
		"rt":      rt,
	})
}

func (tc *TOTPController) HasBeenEnabled(c *gin.Context) {
	var req HasBeenEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	uid := req.UID
	rt := req.RT

	if uid == "" || rt == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Missing uid or rt",
		})
		return
	}

	// M-T is treated as a remtoken: when the caller declares auth_type="manage"
	// with the configured M-T, the remember_token check is skipped so the
	// operator can query any user by uid.
	isManage, authOK := isManageRequest(c, rt, req.AuthType)
	if !authOK {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid auth type or token",
		})
		return
	}

	query := database.DB.Where("uid = ?", uid)
	if !isManage {
		query = query.Where("remember_token = ?", rt)
	}

	var user models.User
	result := query.First(&user)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid uid or rt",
		})
		return
	}

	enabled := 0
	if user.TOTP != "" {
		enabled = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"enabled": enabled,
	})
}
