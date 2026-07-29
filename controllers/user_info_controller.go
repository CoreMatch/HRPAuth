package controllers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
)

type UserInfoController struct{}

func NewUserInfoController() *UserInfoController {
	return &UserInfoController{}
}

type GetUserRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
}

type DeclareEmailRequest struct {
	ManageToken string `json:"mt"`
	Email       string `json:"email"`
	PlayerName  string `json:"playername"`
}

func normalizeDeclaredEmail(raw string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	if normalized == "" {
		return "", fmt.Errorf("email is required")
	}
	if !isValidEmail(normalized) {
		return "", fmt.Errorf("invalid email")
	}
	return normalized, nil
}

func (uc *UserInfoController) GetUser(c *gin.Context) {
	var req GetUserRequest
	token := ""
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		token = req.RememberToken
		uid = req.UID
		email = req.Email
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if uid == "" {
		uid = c.PostForm("uid")
	}
	if email == "" {
		email = c.PostForm("email")
	}

	if token == "" {
		token = c.Query("remember_token")
	}
	if uid == "" {
		uid = c.Query("uid")
	}
	if email == "" {
		email = c.Query("email")
	}

	if token == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未登录或登录已过期",
			"data":    nil,
		})
		return
	}

	isManage := config.AppConfig.Manage.Token != "" && token == config.AppConfig.Manage.Token

	// M-T acts as a master remtoken: the target user must be identified by
	// uid or email since M-T itself is not stored on any user row.
	if isManage && uid == "" && email == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Manage Token 需要指定 uid 或 email",
			"data":    nil,
		})
		return
	}

	query := database.DB.Model(&models.User{})
	if !isManage {
		query = query.Where("remember_token = ?", token)
	}

	if uid != "" {
		query = query.Where("uid = ?", uid)
	}
	if email != "" {
		query = query.Where("email = ?", email)
	}

	var user models.User
	result := query.First(&user)
	if result.Error != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在或token无效",
			"data":    nil,
		})
		return
	}

	userData := gin.H{
		"uid":      user.UID,
		"email":    user.Email,
		"username": user.Username,
		"avatar":   user.Avatar,
		"verified": user.Verified,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "获取用户信息成功",
		"data":    userData,
	})
}

// EnableMojangBind sets users.mbe = 1 so that a M.T. /register from a Mojang
// player colliding on this username will bind (instead of returning 409).
//
// The user opts in themselves (via their remember_token) or an operator
// enables it on a target user via the M-T path (which requires uid or email).
// Idempotent: calling when mbe is already 1 is a no-op success.
//
// After a successful bind (users.mojang_uuid is set) this field becomes
// irrelevant and is left untouched.
func (uc *UserInfoController) DeclareEmail(c *gin.Context) {
	var req DeclareEmailRequest
	manageToken := ""
	email := ""
	playerName := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		manageToken = req.ManageToken
		email = req.Email
		playerName = req.PlayerName
	}
	if manageToken == "" {
		manageToken = c.PostForm("mt")
	}
	if email == "" {
		email = c.PostForm("email")
	}
	if playerName == "" {
		playerName = c.PostForm("playername")
	}
	if manageToken == "" {
		manageToken = c.Query("mt")
	}
	if email == "" {
		email = c.Query("email")
	}
	if playerName == "" {
		playerName = c.Query("playername")
	}

	if manageToken == "" || email == "" || playerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "mt, email, and playername are required",
		})
		return
	}

	if config.AppConfig.Manage.Token == "" || manageToken != config.AppConfig.Manage.Token {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid manage token",
		})
		return
	}

	normalizedEmail, err := normalizeDeclaredEmail(email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", playerName).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "user not found",
		})
		return
	}

	var existing models.User
	if err := database.DB.Where("email = ?", normalizedEmail).First(&existing).Error; err == nil && existing.UID != user.UID {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "Email already registered",
		})
		return
	}

	if err := database.DB.Model(&user).Update("email", normalizedEmail).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to declare email",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Email declared successfully",
		"data": gin.H{
			"uid":      user.UID,
			"email":    normalizedEmail,
			"username": user.Username,
		},
	})
}

func (uc *UserInfoController) EnableMojangBind(c *gin.Context) {
	var req GetUserRequest
	token := ""
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		token = req.RememberToken
		uid = req.UID
		email = req.Email
	}
	if token == "" {
		token = c.PostForm("remember_token")
	}
	if uid == "" {
		uid = c.PostForm("uid")
	}
	if email == "" {
		email = c.PostForm("email")
	}
	if token == "" {
		token = c.Query("remember_token")
	}
	if uid == "" {
		uid = c.Query("uid")
	}
	if email == "" {
		email = c.Query("email")
	}

	if token == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未登录或登录已过期",
		})
		return
	}

	isManage := config.AppConfig.Manage.Token != "" && token == config.AppConfig.Manage.Token
	if isManage && uid == "" && email == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Manage Token 需要指定 uid 或 email",
		})
		return
	}

	query := database.DB.Model(&models.User{})
	if !isManage {
		query = query.Where("remember_token = ?", token)
	}
	if uid != "" {
		query = query.Where("uid = ?", uid)
	}
	if email != "" {
		query = query.Where("email = ?", email)
	}

	var user models.User
	if err := query.First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在或token无效",
		})
		return
	}

	if err := database.DB.Model(&user).Update("mbe", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to enable mojang bind",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Mojang bind enabled",
		"data": gin.H{
			"uid": user.UID,
			"mbe": 1,
		},
	})
}
