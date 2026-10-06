package controllers

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
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
	err := authService.ChangeUsername(user.UUID, newUsername)
	if err != nil {
		code := CodeInternalError
		status := http.StatusInternalServerError
		switch err.Error() {
		case "username already exists":
			code = CodeUsernameConflict
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
