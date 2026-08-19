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
	UID      string `json:"uid"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type ChangeProfileNameRequest struct {
	UID       string `json:"uid"`
	Email     string `json:"email"`
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
}

func (uc *UserProfileController) ChangeUsername(c *gin.Context) {
	var req ChangeUsernameRequest
	newUsername := ""
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		newUsername = req.Username
		uid = req.UID
		email = req.Email
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

	if newUsername == "" {
		newUsername = c.Query("username")
	}
	if uid == "" {
		uid = c.Query("uid")
	}
	if email == "" {
		email = c.Query("email")
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

	authResult, ok := resolveSiteBearerAuth(c, "profile.change-username", "profile.change-username.as-service", false, uid, email)
	if !ok {
		return
	}
	user := *authResult.User

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
	profileID := ""
	newName := ""
	uid := ""
	email := ""

	if err := c.ShouldBindJSON(&req); err == nil {
		profileID = req.ProfileID
		newName = req.Name
		uid = req.UID
		email = req.Email
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

	authResult, ok := resolveSiteBearerAuth(c, "profile.change-name", "profile.change-name.as-service", false, uid, email)
	if !ok {
		return
	}
	user := *authResult.User

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
