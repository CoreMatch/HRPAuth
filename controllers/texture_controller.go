package controllers

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type TextureController struct {
	textureService *services.TextureService
	authService    *services.AuthService
}

func NewTextureController() *TextureController {
	return &TextureController{
		textureService: services.NewTextureService(),
		authService:    services.NewAuthService(),
	}
}

type UploadTextureRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
	ProfileID     string `json:"profile_id"`
	TextureType   string `json:"texture_type"`
	Model         string `json:"model"`
	AuthType      string `json:"auth_type"`
}

func (tc *TextureController) UploadTexture(c *gin.Context) {
	token := ""
	profileID := ""
	textureType := ""
	model := ""
	uid := ""
	email := ""
	authType := ""

	contentType := c.ContentType()
	if strings.Contains(contentType, "application/json") {
		var req UploadTextureRequest
		if err := c.ShouldBindJSON(&req); err == nil {
			token = req.RememberToken
			profileID = req.ProfileID
			textureType = req.TextureType
			model = req.Model
			uid = req.UID
			email = req.Email
			authType = req.AuthType
		}
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if profileID == "" {
		profileID = c.PostForm("profile_id")
	}
	if textureType == "" {
		textureType = c.PostForm("texture_type")
	}
	if model == "" {
		model = c.PostForm("model")
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
	if textureType == "" {
		textureType = c.Query("texture_type")
	}
	if model == "" {
		model = c.Query("model")
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

	if textureType != "skin" && textureType != "cape" {
		respondError(c, http.StatusBadRequest, CodeTextureTypeInvalid, "无效的材质类型，只能是 skin 或 cape")
		return
	}

	isManage, authOK := isManageRequest(c, token, authType)
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

	if !tc.authService.IsProfileOwnedByUser(profileID, user.UUID) {
		respondError(c, http.StatusForbidden, CodeProfileAccessDenied, "无权操作该角色")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, CodeTextureFileRequired, "未上传文件")
		return
	}

	fileData, err := file.Open()
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeTextureReadFailed, "无法读取文件")
		return
	}
	defer fileData.Close()

	data, err := io.ReadAll(fileData)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeTextureReadFailed, "文件读取失败")
		return
	}

	if err := tc.textureService.UploadTextureByUser(user.UUID, profileID, textureType, model, data); err != nil {
		status := http.StatusInternalServerError
		code := CodeTextureUploadFailed
		switch err.Error() {
		case "profile not owned by user":
			status = http.StatusForbidden
			code = CodeProfileAccessDenied
		case "texture not found":
			status = http.StatusNotFound
			code = CodeTextureUploadFailed
		default:
			if strings.HasPrefix(err.Error(), "invalid ") || strings.Contains(err.Error(), "must be") {
				status = http.StatusBadRequest
				code = CodeInvalidRequest
			}
		}
		respondError(c, status, code, err.Error())
		return
	}

	respondOK(c, "材质上传成功", gin.H{
		"profile_id":   profileID,
		"texture_type": textureType,
	})
}

type DeleteTextureRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
	ProfileID     string `json:"profile_id"`
	TextureType   string `json:"texture_type"`
	AuthType      string `json:"auth_type"`
}

func (tc *TextureController) DeleteTexture(c *gin.Context) {
	token := ""
	profileID := ""
	textureType := ""
	uid := ""
	email := ""
	authType := ""

	contentType := c.ContentType()
	if strings.Contains(contentType, "application/json") {
		var req DeleteTextureRequest
		if err := c.ShouldBindJSON(&req); err == nil {
			token = req.RememberToken
			profileID = req.ProfileID
			textureType = req.TextureType
			uid = req.UID
			email = req.Email
			authType = req.AuthType
		}
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if profileID == "" {
		profileID = c.PostForm("profile_id")
	}
	if textureType == "" {
		textureType = c.PostForm("texture_type")
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
	if textureType == "" {
		textureType = c.Query("texture_type")
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

	if textureType != "skin" && textureType != "cape" {
		respondError(c, http.StatusBadRequest, CodeTextureTypeInvalid, "无效的材质类型，只能是 skin 或 cape")
		return
	}

	isManage, authOK := isManageRequest(c, token, authType)
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

	if !tc.authService.IsProfileOwnedByUser(profileID, user.UUID) {
		respondError(c, http.StatusForbidden, CodeProfileAccessDenied, "无权操作该角色")
		return
	}

	if err := tc.textureService.RemoveTextureByUser(user.UUID, profileID, textureType); err != nil {
		status := http.StatusInternalServerError
		code := CodeTextureDeleteFailed
		if err.Error() == "profile not owned by user" {
			status = http.StatusForbidden
			code = CodeProfileAccessDenied
		}
		respondError(c, status, code, err.Error())
		return
	}

	respondOK(c, "材质删除成功", gin.H{
		"profile_id":   profileID,
		"texture_type": textureType,
	})
}

type GetTextureRequest struct {
	RememberToken string `json:"remember_token"`
	UID           string `json:"uid"`
	Email         string `json:"email"`
	ProfileID     string `json:"profile_id"`
	AuthType      string `json:"auth_type"`
}

type TextureResponse struct {
	TextureType string `json:"texture_type"`
	URL         string `json:"url"`
	Model       string `json:"model,omitempty"`
}

func (tc *TextureController) GetTexture(c *gin.Context) {
	token := ""
	profileID := ""
	uid := ""
	email := ""
	authType := ""

	contentType := c.ContentType()
	if strings.Contains(contentType, "application/json") {
		var req GetTextureRequest
		if err := c.ShouldBindJSON(&req); err == nil {
			token = req.RememberToken
			profileID = req.ProfileID
			uid = req.UID
			email = req.Email
			authType = req.AuthType
		}
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if profileID == "" {
		profileID = c.PostForm("profile_id")
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

	isManage, authOK := isManageRequest(c, token, authType)
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
			respondError(c, http.StatusNotFound, CodeProfileNotFound, "当前账号没有角色")
			return
		}
		profileID = profile.ID
	}

	if !tc.authService.IsProfileOwnedByUser(profileID, user.UUID) {
		respondError(c, http.StatusForbidden, CodeProfileAccessDenied, "无权查看该角色")
		return
	}

	skinInfo, _ := tc.textureService.GetTextureByProfile(profileID, "skin")
	capeInfo, _ := tc.textureService.GetTextureByProfile(profileID, "cape")

	textures := make([]TextureResponse, 0)
	if skinInfo != nil {
		skinResp := TextureResponse{
			TextureType: "skin",
			URL:         skinInfo.URL,
		}
		if skinInfo.Metadata != nil {
			if model, ok := skinInfo.Metadata["model"]; ok {
				skinResp.Model = model.(string)
			}
		}
		textures = append(textures, skinResp)
	}
	if capeInfo != nil {
		textures = append(textures, TextureResponse{
			TextureType: "cape",
			URL:         capeInfo.URL,
		})
	}

	respondOK(c, "获取材质信息成功", gin.H{
		"profile_id": profileID,
		"textures":   textures,
	})
}
