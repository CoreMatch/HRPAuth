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
	AuthType      string `json:"auth_type"`
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
		respondError(c, http.StatusUnauthorized, CodeRememberTokenRequired, "未登录或登录已过期")
		return
	}

	isManage, authOK := isManageRequest(c, token, req.AuthType)
	if !authOK {
		respondError(c, http.StatusUnauthorized, CodeInvalidAuthTypeOrToken, "无效的鉴权类型或token")
		return
	}

	// M-T acts as a master remtoken: the target user must be identified by
	// uid or email since M-T itself is not stored on any user row.
	if isManage && uid == "" && email == "" {
		respondError(c, http.StatusBadRequest, CodeManageTargetRequired, "Manage Token 需要指定 uid 或 email")
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
		if isManage {
			respondError(c, http.StatusNotFound, CodeUserNotFound, "用户不存在")
			return
		}
		respondError(c, http.StatusUnauthorized, CodeInvalidRememberToken, "用户不存在或token无效")
		return
	}

	userData := gin.H{
		"uid":      user.UID,
		"email":    user.Email,
		"username": user.Username,
		"avatar":   user.Avatar,
		"verified": user.Verified,
	}

	respondOK(c, "获取用户信息成功", userData)
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
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "mt, email, and playername are required")
		return
	}

	if config.AppConfig.Manage.Token == "" || manageToken != config.AppConfig.Manage.Token {
		respondError(c, http.StatusUnauthorized, CodeInvalidManageToken, "invalid manage token")
		return
	}

	normalizedEmail, err := normalizeDeclaredEmail(email)
	if err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, err.Error())
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", playerName).First(&user).Error; err != nil {
		respondError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
		return
	}

	var existing models.User
	if err := database.DB.Where("email = ?", normalizedEmail).First(&existing).Error; err == nil && existing.UID != user.UID {
		respondError(c, http.StatusConflict, CodeEmailAlreadyRegistered, "Email already registered")
		return
	}

	if err := database.DB.Model(&user).Update("email", normalizedEmail).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to declare email")
		return
	}

	respondOK(c, "Email declared successfully", gin.H{
		"uid":      user.UID,
		"email":    normalizedEmail,
		"username": user.Username,
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
		respondError(c, http.StatusUnauthorized, CodeRememberTokenRequired, "未登录或登录已过期")
		return
	}

	isManage, authOK := isManageRequest(c, token, req.AuthType)
	if !authOK {
		respondError(c, http.StatusUnauthorized, CodeInvalidAuthTypeOrToken, "无效的鉴权类型或token")
		return
	}
	if isManage && uid == "" && email == "" {
		respondError(c, http.StatusBadRequest, CodeManageTargetRequired, "Manage Token 需要指定 uid 或 email")
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
		if isManage {
			respondError(c, http.StatusNotFound, CodeUserNotFound, "用户不存在")
			return
		}
		respondError(c, http.StatusUnauthorized, CodeInvalidRememberToken, "用户不存在或token无效")
		return
	}

	if err := database.DB.Model(&user).Update("mbe", true).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to enable mojang bind")
		return
	}

	respondOK(c, "Mojang bind enabled", gin.H{
		"uid": user.UID,
		"mbe": 1,
	})
}
