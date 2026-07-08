package controllers

import (
	"net/http"
	"net/mail"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"gorm.io/gorm"
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
}

type LogoutRequest struct {
	RememberToken string `json:"remember_token"`
}

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

func (ac *AuthController) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	email := req.Email
	password := req.Password

	if !isValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid email",
		})
		return
	}

	var user models.User
	result := database.DB.Where("email = ?", email).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Email or password incorrect",
		})
		return
	}

	if !utils.CheckPasswordHash(password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Email or password incorrect",
		})
		return
	}

	token := utils.GenerateRandomToken(32)
	now := time.Now()

	database.DB.Model(&user).Updates(map[string]interface{}{
		"remember_token": token,
		"last_sign_at":   now,
	})

	totp := 0
	if user.TOTP != "" {
		totp = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Login successful",
		"token":   token,
		"uid":     user.UID,
		"totp":    totp,
	})
}

func (ac *AuthController) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	email := req.Email
	username := req.Username
	password := req.Password

	if !isValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid email",
		})
		return
	}

	if len(username) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Username too short",
		})
		return
	}

	if len(password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Password too short",
		})
		return
	}

	// Verify captcha when enabled (fail fast before DB hits)
	if config.AppConfig.Security.EnableCaptcha {
		captchaService := services.NewCaptchaService()
		if req.CaptchaToken == "" || req.CaptchaCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "Invalid or expired captcha",
			})
			return
		}
		if !captchaService.Verify(req.CaptchaToken, req.CaptchaCode) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "Invalid or expired captcha",
			})
			return
		}
	}

	var count int64
	database.DB.Model(&models.User{}).Where("email = ?", email).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "Email already registered",
		})
		return
	}

	database.DB.Model(&models.User{}).Where("username = ?", username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "Username already taken",
		})
		return
	}

	hash, err := utils.HashPassword(password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Password hashing failed",
		})
		return
	}

	ip := c.ClientIP()
	now := time.Now()
	uuid := utils.GenerateUnsignedUUID()

	var maxUID uint
	database.DB.Model(&models.User{}).Select("COALESCE(MAX(uid), 0)").Scan(&maxUID)
	newUID := maxUID + 1

	user := models.User{
		UID:        newUID,
		UUID:       uuid,
		Email:      email,
		Username:   username,
		Password:   hash,
		IP:         ip,
		RegIP:      ip,
		LastSignAt: &now,
		RegisterAt: &now,
		Score:      1000,
		Verified:   false,
		RegDate:    now.Unix(),
	}

	authService := services.NewAuthService()
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		_, err = authService.CreateDefaultProfileForUserTx(tx, user.UUID, username)
		return err
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to create user profile",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"uid":     user.UID,
		"message": "Register successful",
	})
}

func (ac *AuthController) Logout(c *gin.Context) {
	token := ""

	var req LogoutRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.RememberToken != "" {
		token = req.RememberToken
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if token == "" {
		token = c.Query("remember_token")
	}

	if token != "" {
		// The Manage Token (M-T) lives in config and is not stored in the DB,
		// so the update below is a no-op for it. For a real remember_token
		// the row is matched and cleared.
		database.DB.Model(&models.User{}).
			Where("remember_token = ?", token).
			Update("remember_token", nil)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Logout successful",
	})
}
