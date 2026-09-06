package controllers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type UserInfoController struct{}

func NewUserInfoController() *UserInfoController {
	return &UserInfoController{}
}

type GetUserRequest struct {
	UID   string `json:"uid"`
	Email string `json:"email"`
}

type DeclareEmailRequest struct {
	Email      string `json:"email"`
	PlayerName string `json:"playername"`
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
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		uid = req.UID
		email = req.Email
	}
	if uid == "" {
		uid = c.PostForm("uid")
	}
	if email == "" {
		email = c.PostForm("email")
	}

	if uid == "" {
		uid = c.Query("uid")
	}
	if email == "" {
		email = c.Query("email")
	}

	authResult, ok := resolveSiteBearerAuth(c, "user.read", "user.read.as-service", false, uid, email)
	if !ok {
		return
	}
	user := *authResult.User

	userData := gin.H{
		"uid":      user.UID,
		"email":    user.Email,
		"username": user.Username,
		"avatar":   user.Avatar,
		"verified": user.Verified,
		"mbe":      user.MBE,
	}

	respondOK(c, "获取用户信息成功", userData)
}

// EnableMojangBind sets users.mbe = 1 so that a M.T. /register from a Mojang
// player colliding on this username will bind (instead of returning 409).
//
// The user opts in themselves via their Bearer token, or a service token
// enables it on a target user. Idempotent: calling when mbe is already 1 is a
// no-op success.
//
// After a successful bind (users.mojang_uuid is set) this field becomes
// irrelevant and is left untouched.
func (uc *UserInfoController) DeclareEmail(c *gin.Context) {
	var req DeclareEmailRequest
	email := ""
	playerName := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		email = req.Email
		playerName = req.PlayerName
	}
	if email == "" {
		email = c.PostForm("email")
	}
	if playerName == "" {
		playerName = c.PostForm("playername")
	}
	if email == "" {
		email = c.Query("email")
	}
	if playerName == "" {
		playerName = c.Query("playername")
	}

	if email == "" || playerName == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "email and playername are required")
		return
	}

	accessToken := bearerTokenFromRequest(c)
	if accessToken == "" {
		respondError(c, http.StatusUnauthorized, CodeOAuthLoginRequired, "missing bearer token")
		return
	}
	tokenContext, err := services.NewOAuth2Service().ResolveAccessToken(accessToken)
	if err != nil {
		respondError(c, http.StatusUnauthorized, CodeOAuthInvalidGrant, "invalid access token")
		return
	}
	if !tokenContext.IsService {
		respondError(c, http.StatusForbidden, CodeOAuthAccessDenied, "declare-email requires a service token")
		return
	}
	if !hasScope(tokenContext.Scopes, "user.declare-email") {
		respondError(c, http.StatusForbidden, CodeOAuthInsufficientScope, "insufficient scope")
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
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		uid = req.UID
		email = req.Email
	}
	if uid == "" {
		uid = c.PostForm("uid")
	}
	if email == "" {
		email = c.PostForm("email")
	}
	if uid == "" {
		uid = c.Query("uid")
	}
	if email == "" {
		email = c.Query("email")
	}

	authResult, ok := resolveSiteBearerAuth(c, "user.mojang-bind-enable", "user.mojang-bind-enable.as-service", false, uid, email)
	if !ok {
		return
	}
	user := *authResult.User

	if err := database.DB.Model(&user).Update("mbe", true).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to enable mojang bind")
		return
	}

	respondOK(c, "Mojang bind enabled", gin.H{
		"uid": user.UID,
		"mbe": 1,
	})
}

// DisableMojangBind sets users.mbe = 0 so that a M.T. /register from a Mojang
// player colliding on this username will return 409 (HA priority) instead of
// binding.
//
// The user opts in themselves via their Bearer token, or a service token
// disables it on a target user. Idempotent: calling when mbe is already 0 is a
// no-op success.
func (uc *UserInfoController) DisableMojangBind(c *gin.Context) {
	var req GetUserRequest
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		uid = req.UID
		email = req.Email
	}
	if uid == "" {
		uid = c.PostForm("uid")
	}
	if email == "" {
		email = c.PostForm("email")
	}
	if uid == "" {
		uid = c.Query("uid")
	}
	if email == "" {
		email = c.Query("email")
	}

	authResult, ok := resolveSiteBearerAuth(c, "user.mojang-bind-disable", "user.mojang-bind-disable.as-service", false, uid, email)
	if !ok {
		return
	}
	user := *authResult.User

	if err := database.DB.Model(&user).Update("mbe", false).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to disable mojang bind")
		return
	}

	respondOK(c, "Mojang bind disabled", gin.H{
		"uid": user.UID,
		"mbe": 0,
	})
}
