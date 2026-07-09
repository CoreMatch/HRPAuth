package controllers

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
)

type YggdrasilController struct {
	authService *services.AuthService
}

func NewYggdrasilController() *YggdrasilController {
	return &YggdrasilController{
		authService: services.NewAuthService(),
	}
}

type AgentInfo struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type AuthenticateRequest struct {
	Username    string    `json:"username"`
	Password    string    `json:"password"`
	Agent       AgentInfo `json:"agent"`
	ClientToken string    `json:"clientToken"`
	RequestUser bool      `json:"requestUser"`
}

type RefreshRequest struct {
	AccessToken     string `json:"accessToken"`
	ClientToken     string `json:"clientToken"`
	SelectedProfile *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"selectedProfile"`
	RequestUser bool `json:"requestUser"`
}

type ValidateRequest struct {
	AccessToken string `json:"accessToken"`
	ClientToken string `json:"clientToken"`
}

type InvalidateRequest struct {
	AccessToken string `json:"accessToken"`
	ClientToken string `json:"clientToken"`
}

type SignoutRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type JoinRequest struct {
	AccessToken     string `json:"accessToken"`
	SelectedProfile string `json:"selectedProfile"`
	ServerID        string `json:"serverId"`
}

func sendYggdrasilError(c *gin.Context, errType, errMessage string, statusCode int) {
	c.JSON(statusCode, gin.H{
		"error":        errType,
		"errorMessage": errMessage,
		"cause":        nil,
	})
}

func (yc *YggdrasilController) Meta(c *gin.Context) {
	cfg := config.AppConfig.Yggdrasil.Server
	frontendURL := config.AppConfig.Frontend.URL

	links := gin.H{
		"homepage": frontendURL,
		"register": strings.TrimRight(frontendURL, "/") + "/register",
	}

	skinDomains := cfg.SkinDomains
	if len(skinDomains) == 0 {
		skinDomains = []string{
			utils.ExtractDomain(config.AppConfig.Callback.URL),
			"." + utils.ExtractDomain(config.AppConfig.Callback.URL),
		}
	}

	c.Header("X-Authlib-Injector-API-Location", "/")

	c.JSON(http.StatusOK, gin.H{
		"meta": gin.H{
			"serverName":                          cfg.Name,
			"implementationName":                  cfg.Implementation,
			"implementationVersion":               cfg.Version,
			"links":                               links,
			"feature.non_email_login":             config.AppConfig.Yggdrasil.FeatureFlags.NonEmailLogin,
			"feature.legacy_skin_api":             config.AppConfig.Yggdrasil.FeatureFlags.LegacySkinAPI,
			"feature.no_mojang_namespace":         config.AppConfig.Yggdrasil.FeatureFlags.NoMojangNamespace,
			"feature.enable_mojang_anti_features": config.AppConfig.Yggdrasil.FeatureFlags.EnableMojangAntiFeatures,
			"feature.enable_profile_key":          config.AppConfig.Yggdrasil.FeatureFlags.EnableProfileKey,
			"feature.username_check":              config.AppConfig.Yggdrasil.FeatureFlags.UsernameCheck,
		},
		"skinDomains":        skinDomains,
		"signaturePublickey": cfg.SignaturePublicKey,
	})
}

func (yc *YggdrasilController) Authenticate(c *gin.Context) {
	var req AuthenticateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid credentials.", http.StatusForbidden)
		return
	}

	if req.Username == "" || req.Password == "" || req.Agent.Name == "" {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid credentials.", http.StatusForbidden)
		return
	}

	if yc.authService.IsLoginRateLimited(req.Username) {
		sendYggdrasilError(c, "ForbiddenOperationException", "Too many login attempts. Please try again later.", http.StatusForbidden)
		return
	}

	user := yc.authService.VerifyCredentials(req.Username, req.Password, false)
	if user == nil {
		yc.authService.RecordLoginAttempt(req.Username, false)
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid credentials.", http.StatusForbidden)
		return
	}

	yc.authService.RecordLoginAttempt(req.Username, true)

	profiles := yc.authService.GetUserProfiles(user.UUID)
	if len(profiles) == 0 {
		sendYggdrasilError(c, "ForbiddenOperationException", "User has no profiles.", http.StatusForbidden)
		return
	}

	clientToken := req.ClientToken
	if clientToken == "" {
		clientToken = utils.GenerateClientToken()
	}

	accessToken := utils.GenerateAccessToken()
	selectedProfile := profiles[0]
	expiresInDays := config.AppConfig.Yggdrasil.Security.TokenExpiryDays

	if config.AppConfig.Yggdrasil.FeatureFlags.NonEmailLogin {
		profileByName := yc.authService.GetProfileByName(req.Username)
		if profileByName != nil && profileByName.UserID == user.UUID {
			for _, p := range profiles {
				if p.ID == profileByName.ID {
					selectedProfile = p
					break
				}
			}
		}
	}

	if existing := yc.authService.GetValidTokenByClientToken(user.UUID, clientToken); existing != nil {
		for _, p := range profiles {
			if p.ID == existing.SelectedProfileID {
				selectedProfile = p
				break
			}
		}
		accessToken = existing.AccessToken
		yc.authService.RefreshTokenExpiry(existing.AccessToken, expiresInDays)
	} else {
		yc.authService.MarkOtherClientTokensTemporarilyInvalid(user.UUID, clientToken)
		if !yc.authService.CreateToken(accessToken, clientToken, user.UUID, selectedProfile.ID, expiresInDays) {
			sendYggdrasilError(c, "ForbiddenOperationException", "Failed to create session. Please try again.", http.StatusForbidden)
			return
		}
	}

	response := gin.H{
		"accessToken":       accessToken,
		"clientToken":       clientToken,
		"availableProfiles": profiles,
		"selectedProfile":   selectedProfile,
	}

	if req.RequestUser {
		userProperties := make([]gin.H, 0)

		response["user"] = gin.H{
			"id":         user.UUID,
			"email":      user.Email,
			"username":   user.Username,
			"properties": userProperties,
		}
	}

	c.JSON(http.StatusOK, response)
}

func (yc *YggdrasilController) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	token := yc.authService.ValidateTokenForRefresh(req.AccessToken, req.ClientToken)
	if token == nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	profiles := yc.authService.GetUserProfiles(token.UserID)
	if len(profiles) == 0 {
		sendYggdrasilError(c, "ForbiddenOperationException", "User has no profiles.", http.StatusForbidden)
		return
	}

	newAccessToken := utils.GenerateAccessToken()
	yc.authService.InvalidateToken(req.AccessToken)

	selectedProfileID := token.SelectedProfileID
	if req.SelectedProfile != nil {
		if yc.authService.IsProfileOwnedByUser(req.SelectedProfile.ID, token.UserID) {
			selectedProfileID = req.SelectedProfile.ID
		}
	}

	var selectedProfile *services.ProfileInfo
	for _, p := range profiles {
		if p.ID == selectedProfileID {
			selectedProfile = &p
			break
		}
	}
	if selectedProfile == nil {
		selectedProfile = &profiles[0]
	}

	expiresInDays := config.AppConfig.Yggdrasil.Security.TokenExpiryDays
	yc.authService.MarkOtherClientTokensTemporarilyInvalid(token.UserID, req.ClientToken)
	yc.authService.CreateToken(newAccessToken, req.ClientToken, token.UserID, selectedProfile.ID, expiresInDays)

	response := gin.H{
		"accessToken":       newAccessToken,
		"clientToken":       req.ClientToken,
		"availableProfiles": profiles,
		"selectedProfile":   selectedProfile,
	}

	if req.RequestUser {
		user := yc.authService.GetUserByID(token.UserID)
		if user != nil {
			userProperties := make([]gin.H, 0)
			response["user"] = gin.H{
				"id":         user.UUID,
				"email":      user.Email,
				"username":   user.Username,
				"properties": userProperties,
			}
		}
	}

	c.JSON(http.StatusOK, response)
}

func (yc *YggdrasilController) Validate(c *gin.Context) {
	var req ValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	token := yc.authService.ValidateToken(req.AccessToken, req.ClientToken)
	if token == nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	c.Status(http.StatusNoContent)
}

func (yc *YggdrasilController) Invalidate(c *gin.Context) {
	var req InvalidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	yc.authService.InvalidateToken(req.AccessToken)
	c.Status(http.StatusNoContent)
}

func (yc *YggdrasilController) Signout(c *gin.Context) {
	var req SignoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid credentials.", http.StatusForbidden)
		return
	}

	user := yc.authService.VerifyCredentials(req.Username, req.Password, false)
	if user == nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid credentials.", http.StatusForbidden)
		return
	}

	yc.authService.InvalidateAllUserTokens(user.UUID)

	c.Status(http.StatusNoContent)
}

func (yc *YggdrasilController) Join(c *gin.Context) {
	var req JoinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	token := yc.authService.ValidateToken(req.AccessToken, "")
	if token == nil {
		sendYggdrasilError(c, "ForbiddenOperationException", "Invalid token.", http.StatusForbidden)
		return
	}

	if req.SelectedProfile != token.SelectedProfileID {
		if !yc.authService.IsProfileOwnedByUser(req.SelectedProfile, token.UserID) {
			sendYggdrasilError(c, "ForbiddenOperationException", "Invalid profile.", http.StatusForbidden)
			return
		}
	}

	ip := c.ClientIP()
	if !yc.authService.CreateSession(req.SelectedProfile, req.ServerID, ip) {
		sendYggdrasilError(c, "ForbiddenOperationException", "Failed to create session.", http.StatusForbidden)
		return
	}

	c.Status(http.StatusNoContent)
}

func (yc *YggdrasilController) HasJoined(c *gin.Context) {
	username := c.Query("username")
	serverID := c.Query("serverId")
	ip := c.Query("ip")

	if username == "" || serverID == "" {
		sendYggdrasilError(c, "BadRequestException", "Bad request.", http.StatusBadRequest)
		return
	}

	profile := yc.authService.GetProfileByName(username)
	if profile == nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	session := yc.authService.GetSessionByProfileAndServer(username, serverID)
	if session == nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	if ip != "" && session.IP != ip {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	textureService := services.NewTextureService()
	properties, err := textureService.GetProfileProperties(profile.ID, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"id":         profile.ID,
			"name":       profile.Name,
			"properties": []gin.H{},
		})
		return
	}

	props := make([]gin.H, 0)
	for _, prop := range properties {
		p := gin.H{
			"name":  prop.Name,
			"value": prop.Value,
		}
		if prop.Signature != "" {
			p["signature"] = prop.Signature
		}
		props = append(props, p)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         profile.ID,
		"name":       profile.Name,
		"properties": props,
	})
}

func (yc *YggdrasilController) ProfileQuery(c *gin.Context) {
	uuid := c.Param("uuid")
	unsignedStr := c.DefaultQuery("unsigned", "true")
	unsigned := unsignedStr == "true"

	if uuid == "" {
		sendYggdrasilError(c, "BadRequestException", "Bad request.", http.StatusBadRequest)
		return
	}

	profile := yc.authService.GetProfileByID(uuid)
	if profile == nil {
		sendYggdrasilError(c, "ProfileNotFoundException", "No such profile.", http.StatusNotFound)
		return
	}

	textureService := services.NewTextureService()
	properties, err := textureService.GetProfileProperties(uuid, unsigned)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"id":         profile.ID,
			"name":       profile.Name,
			"properties": []gin.H{},
		})
		return
	}

	props := make([]gin.H, 0)
	for _, prop := range properties {
		p := gin.H{
			"name":  prop.Name,
			"value": prop.Value,
		}
		if prop.Signature != "" && !unsigned {
			p["signature"] = prop.Signature
		}
		props = append(props, p)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         profile.ID,
		"name":       profile.Name,
		"properties": props,
	})
}

type BatchProfileRequest struct {
	Names []string `json:"names"`
}

func (yc *YggdrasilController) BatchProfiles(c *gin.Context) {
	var req BatchProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendYggdrasilError(c, "BadRequestException", "Bad request.", http.StatusBadRequest)
		return
	}

	if len(req.Names) == 0 {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}

	result := make([]gin.H, 0)
	for _, name := range req.Names {
		profile := yc.authService.GetProfileByName(name)
		if profile != nil {
			result = append(result, gin.H{
				"id":   profile.ID,
				"name": profile.Name,
			})
		}
	}

	c.JSON(http.StatusOK, result)
}

func (yc *YggdrasilController) UploadTexture(c *gin.Context) {
	uuid := c.Param("uuid")
	textureType := c.Param("textureType")

	if uuid == "" || (textureType != "skin" && textureType != "cape") {
		sendYggdrasilError(c, "BadRequestException", "Invalid parameters.", http.StatusBadRequest)
		return
	}

	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		sendYggdrasilError(c, "UnauthorizedOperationException", "Unauthorized.", http.StatusUnauthorized)
		return
	}

	accessToken := ""
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		accessToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	if accessToken == "" {
		sendYggdrasilError(c, "UnauthorizedOperationException", "Unauthorized.", http.StatusUnauthorized)
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		sendYggdrasilError(c, "BadRequestException", "No file uploaded.", http.StatusBadRequest)
		return
	}

	fileData, err := file.Open()
	if err != nil {
		sendYggdrasilError(c, "InternalException", "Failed to read file.", http.StatusInternalServerError)
		return
	}
	defer fileData.Close()

	data, err := io.ReadAll(fileData)
	if err != nil {
		sendYggdrasilError(c, "InternalException", "Failed to read file data.", http.StatusInternalServerError)
		return
	}

	model := c.PostForm("model")

	textureService := services.NewTextureService()
	if err := textureService.UploadTexture(accessToken, uuid, textureType, model, data); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", err.Error(), http.StatusForbidden)
		return
	}

	c.Status(http.StatusNoContent)
}

func (yc *YggdrasilController) DeleteTexture(c *gin.Context) {
	uuid := c.Param("uuid")
	textureType := c.Param("textureType")

	if uuid == "" || (textureType != "skin" && textureType != "cape") {
		sendYggdrasilError(c, "BadRequestException", "Invalid parameters.", http.StatusBadRequest)
		return
	}

	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		sendYggdrasilError(c, "UnauthorizedOperationException", "Unauthorized.", http.StatusUnauthorized)
		return
	}

	accessToken := ""
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		accessToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	if accessToken == "" {
		sendYggdrasilError(c, "UnauthorizedOperationException", "Unauthorized.", http.StatusUnauthorized)
		return
	}

	textureService := services.NewTextureService()
	if err := textureService.RemoveTexture(accessToken, uuid, textureType); err != nil {
		sendYggdrasilError(c, "ForbiddenOperationException", err.Error(), http.StatusForbidden)
		return
	}

	c.Status(http.StatusNoContent)
}

func (yc *YggdrasilController) DownloadTexture(c *gin.Context) {
	hash := c.Param("hash")
	if hash == "" {
		sendYggdrasilError(c, "BadRequestException", "Bad request.", http.StatusBadRequest)
		return
	}

	textureService := services.NewTextureService()
	filePath, err := textureService.GetTexturePath(hash)
	if err != nil {
		sendYggdrasilError(c, "NotFoundException", "Texture not found.", http.StatusNotFound)
		return
	}

	c.Header("Content-Type", "image/png")
	c.File(filePath)
}
