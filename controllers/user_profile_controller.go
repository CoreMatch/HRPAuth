package controllers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type UserProfileController struct{}

func NewUserProfileController() *UserProfileController {
	return &UserProfileController{}
}

type ChangeUsernameRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
	Username      string `json:"username"`
	AuthType      string `json:"auth_type"`
}

type ChangeProfileNameRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
	ProfileID     string `json:"profile_id"`
	Name          string `json:"name"`
	AuthType      string `json:"auth_type"`
}

func (uc *UserProfileController) ChangeUsername(c *gin.Context) {
	var req ChangeUsernameRequest
	token := ""
	newUsername := ""
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		token = req.RememberToken
		newUsername = req.Username
		uid = req.UID
		email = req.Email
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if newUsername == "" {
		newUsername = c.PostForm("username")
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
	if newUsername == "" {
		newUsername = c.Query("username")
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

	if newUsername == "" {
		respondError(c, http.StatusBadRequest, CodeUsernameInvalid, "请提供新用户名")
		return
	}

	if len(newUsername) < 3 || len(newUsername) > 16 {
		respondError(c, http.StatusBadRequest, CodeUsernameInvalid, "用户名长度必须在3-16个字符之间")
		return
	}

	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_]+$`, newUsername)
	if !matched {
		respondError(c, http.StatusBadRequest, CodeUsernameInvalid, "用户名只能包含字母、数字和下划线")
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
	result := query.First(&user)
	if result.Error != nil {
		if isManage {
			respondError(c, http.StatusNotFound, CodeUserNotFound, "用户不存在")
			return
		}
		respondError(c, http.StatusUnauthorized, CodeInvalidRememberToken, "用户不存在或token无效")
		return
	}

	authService := services.NewAuthService()
	_, _, err := authService.SyncUserAndProfileName(user.UUID, "", newUsername)
	if err != nil {
		code := CodeInternalError
		status := http.StatusInternalServerError
		switch err.Error() {
		case "username already exists":
			code = CodeUsernameConflict
			status = http.StatusConflict
		case "profile name already exists":
			code = CodeProfileNameConflict
			status = http.StatusConflict
		case "user not found":
			code = CodeUserNotFound
			status = http.StatusNotFound
		}
		respondError(c, status, code, err.Error())
		return
	}

	respondOK(c, "用户名修改成功", gin.H{
		"username": newUsername,
	})
}

func (uc *UserProfileController) ChangeProfileName(c *gin.Context) {
	var req ChangeProfileNameRequest
	token := ""
	profileID := ""
	newName := ""
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		token = req.RememberToken
		profileID = req.ProfileID
		newName = req.Name
		uid = req.UID
		email = req.Email
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if profileID == "" {
		profileID = c.PostForm("profile_id")
	}
	if newName == "" {
		newName = c.PostForm("name")
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
	if profileID == "" {
		profileID = c.Query("profile_id")
	}
	if newName == "" {
		newName = c.Query("name")
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

	if strings.TrimSpace(newName) == "" {
		respondError(c, http.StatusBadRequest, CodeProfileNameInvalid, "请提供新的角色名")
		return
	}

	newName = strings.TrimSpace(newName)
	if len(newName) < 3 || len(newName) > 16 {
		respondError(c, http.StatusBadRequest, CodeProfileNameInvalid, "角色名长度必须在3-16个字符之间")
		return
	}

	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_]+$`, newName)
	if !matched {
		respondError(c, http.StatusBadRequest, CodeProfileNameInvalid, "角色名只能包含字母、数字和下划线")
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
	result := query.First(&user)
	if result.Error != nil {
		if isManage {
			respondError(c, http.StatusNotFound, CodeUserNotFound, "用户不存在")
			return
		}
		respondError(c, http.StatusUnauthorized, CodeInvalidRememberToken, "用户不存在或token无效")
		return
	}

	if profileID == "" {
		var profile models.Profile
		if err := database.DB.Where("user_id = ?", user.UUID).Order("created_at ASC").First(&profile).Error; err != nil {
			respondError(c, http.StatusNotFound, CodeProfileNotFound, "当前账号没有可修改的角色")
			return
		}
		profileID = profile.ID
	}

	authService := services.NewAuthService()
	_, profile, err := authService.SyncUserAndProfileName(user.UUID, profileID, newName)
	if err != nil {
		code := CodeInternalError
		status := http.StatusInternalServerError
		switch err.Error() {
		case "user not found":
			code = CodeUserNotFound
			status = http.StatusNotFound
		case "profile not found":
			code = CodeProfileNotFound
			status = http.StatusNotFound
		case "profile name already exists":
			code = CodeProfileNameConflict
			status = http.StatusConflict
		case "username already exists":
			code = CodeUsernameConflict
			status = http.StatusConflict
		}
		respondError(c, status, code, err.Error())
		return
	}

	respondOK(c, "角色名修改成功", gin.H{
		"profile_id": profile.ID,
		"name":       profile.Name,
	})
}
