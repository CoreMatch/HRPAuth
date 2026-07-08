package controllers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
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
}

type ChangeProfileNameRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
	ProfileID     string `json:"profile_id"`
	Name          string `json:"name"`
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
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未登录或登录已过期",
		})
		return
	}

	if newUsername == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请提供新用户名",
		})
		return
	}

	if len(newUsername) < 3 || len(newUsername) > 16 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户名长度必须在3-16个字符之间",
		})
		return
	}

	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_]+$`, newUsername)
	if !matched {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户名只能包含字母、数字和下划线",
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
	result := query.First(&user)
	if result.Error != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在或token无效",
		})
		return
	}

	authService := services.NewAuthService()
	_, _, err := authService.SyncUserAndProfileName(user.UUID, "", newUsername)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户名修改成功",
		"data": gin.H{
			"username": newUsername,
		},
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
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未登录或登录已过期",
		})
		return
	}

	if strings.TrimSpace(newName) == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请提供新的角色名",
		})
		return
	}

	newName = strings.TrimSpace(newName)
	if len(newName) < 3 || len(newName) > 16 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "角色名长度必须在3-16个字符之间",
		})
		return
	}

	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_]+$`, newName)
	if !matched {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "角色名只能包含字母、数字和下划线",
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
	result := query.First(&user)
	if result.Error != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在或token无效",
		})
		return
	}

	if profileID == "" {
		var profile models.Profile
		if err := database.DB.Where("user_id = ?", user.UUID).Order("created_at ASC").First(&profile).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "当前账号没有可修改的角色",
			})
			return
		}
		profileID = profile.ID
	}

	authService := services.NewAuthService()
	_, profile, err := authService.SyncUserAndProfileName(user.UUID, profileID, newName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "角色名修改成功",
		"data": gin.H{
			"profile_id": profile.ID,
			"name":       profile.Name,
		},
	})
}