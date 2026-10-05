package controllers

import (
	"fmt"
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

type LookupUserRequest struct {
	UID      *uint   `json:"uid"`
	Username *string `json:"username"`
}

type DeleteAccountRequest struct {
	Password string `json:"password" binding:"required"`
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
	if user.MojangUUID != nil {
		userData["mojang_uuid"] = *user.MojangUUID
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

	RegisterMBETimeout(user.UID)

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

	CancelMBETimeout(user.UID)

	respondOK(c, "Mojang bind disabled", gin.H{
		"uid": user.UID,
		"mbe": 0,
	})
}

// LookupUser resolves a username from a UID, or a UID from a username.
//
// POST /user/lookup
// Body (JSON): { "uid": 42 }  → returns { "uid": 42, "username": "..." }
//
//	or  { "username": "Steve" } → returns { "uid": 42, "username": "Steve" }
//
// Public endpoint — no authentication required.
func (uc *UserInfoController) LookupUser(c *gin.Context) {
	var req LookupUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}

	hasUID := req.UID != nil
	hasUsername := req.Username != nil && strings.TrimSpace(*req.Username) != ""

	if !hasUID && !hasUsername {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "please provide uid or username")
		return
	}
	if hasUID && hasUsername {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "please provide only uid or username, not both")
		return
	}

	var user models.User
	var err error

	if hasUID {
		err = database.DB.Where("uid = ?", *req.UID).First(&user).Error
	} else {
		err = database.DB.Where("username = ?", strings.TrimSpace(*req.Username)).First(&user).Error
	}

	if err != nil {
		respondError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
		return
	}

	respondOK(c, "lookup successful", gin.H{
		"uid":      user.UID,
		"username": user.Username,
	})
}

func (uc *UserInfoController) DeleteAccount(c *gin.Context) {
	var req DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "password is required")
		return
	}

	authResult, ok := resolveSiteBearerAuth(c, "user.delete", "user.delete.as-service", false, "", "")
	if !ok {
		return
	}
	user := authResult.User

	// 验证密码
	if !utils.CheckPasswordHash(req.Password, user.Password) {
		respondError(c, http.StatusForbidden, CodeInvalidCredentials, "密码错误")
		return
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		// 清理相关令牌
		if err := tx.Where("user_id = ?", user.UUID).Delete(&models.Token{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.UUID).Delete(&models.OAuth2AuthorizationCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.UUID).Delete(&models.OAuth2AccessToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.UUID).Delete(&models.OAuth2RefreshToken{}).Error; err != nil {
			return err
		}

		// 软删除用户
		if err := tx.Delete(user).Error; err != nil {
			return err
		}

		// 记录到已删除账号表
		deletedAcc := models.DeletedAccount{
			UID:       user.UID,
			DeletedAt: time.Now(),
		}
		if err := tx.Create(&deletedAcc).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "注销失败")
		return
	}

	respondOK(c, "账号注销成功", nil)
}

func (uc *UserInfoController) ListDeletedAccounts(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "user.delete.list", "user.delete.list.as-service", false, "", "")
	if !ok {
		return
	}
	_ = authResult // 目前不需要从 authResult 中获取额外信息，仅用于鉴权

	var deletedAccounts []models.DeletedAccount
	if err := database.DB.Order("deleted_at DESC").Find(&deletedAccounts).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "获取删除列表失败")
		return
	}

	respondOK(c, "获取删除列表成功", gin.H{
		"deleted_accounts": deletedAccounts,
	})
}
