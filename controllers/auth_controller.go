package controllers

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"gorm.io/gorm"
)

type AuthController struct{}

func NewAuthController() *AuthController {
	return &AuthController{}
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRequest captures both the normal WebUI registration body and the
// Bearer-authenticated service proxy body used by WinnerProxy-like services.
// Normal-path fields (CaptchaToken/CaptchaCode/MojangUUID) are ignored on the
// path that does not consume them.
type RegisterRequest struct {
	Email        string `json:"email"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captcha_token"`
	CaptchaCode  string `json:"captcha_code"`
	MojangUUID   string `json:"mojang_uuid"`
}

type LogoutRequest struct {
	RememberToken string `json:"remember_token"`
}

type LoginByMTRequest struct {
	UID         uint   `json:"uid"`
	Email       string `json:"email"`
	ManageToken string `json:"manage_token"`
}

// ClaimUserRequest is the body for POST /admin/claim-user. Operators provide
// the username of a proxy-registered user (cbh=0) along with the credentials
// (email + password) the user wants to use for subsequent WebUI login.
type ClaimUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// isValidMojangUUID validates the 32-char lowercase-hex form (no dashes) used
// in users.mojang_uuid. Callers are expected to lower-case and trim first.
func isValidMojangUUID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// authTypeFromRequest returns the declared token auth type. It is read from
// the JSON body, then form fields, then query params — mirroring how
// remember_token is collected. When undeclared it defaults to "remember".
// Supported values: "remember" (default) and "manage".
func authTypeFromRequest(c *gin.Context, jsonAuthType string) string {
	authType := jsonAuthType
	if authType == "" {
		authType = c.PostForm("auth_type")
	}
	if authType == "" {
		authType = c.Query("auth_type")
	}
	if authType == "" {
		authType = "remember"
	}
	return authType
}

// isManageRequest classifies a request by its declared auth_type:
//   - undeclared / "remember"          -> (false, true)   remember-token path
//   - "manage" + token == Manage.Token -> (true, true)    genuine M-T request
//   - anything else (unknown value, or "manage" with a token that does not
//     match the configured M-T)        -> (false, false)  caller should reject
//
// The frontend must declare "manage" explicitly; a plain token that happens to
// equal the M-T is no longer auto-promoted to the manage path.
func isManageRequest(c *gin.Context, token, jsonAuthType string) (isManage, valid bool) {
	switch authTypeFromRequest(c, jsonAuthType) {
	case "manage":
		if config.AppConfig.Manage.Token != "" && token == config.AppConfig.Manage.Token {
			return true, true
		}
		return false, false
	default:
		return false, true
	}
}

func (ac *AuthController) Login(c *gin.Context) {
	respondError(c, http.StatusGone, CodeEndpointDeprecated, "POST /login is deprecated; use OAuth2 endpoints instead")
}

// Register handles POST /register.
//
// Two paths are supported:
//
//   - Normal WebUI registration (no Bearer token):
//     captcha enforced when enabled, email required and validated,
//     username/email uniqueness checked.
//   - Service proxy registration (Bearer service token with register.manage):
//     captcha and email-format checks are skipped, email auto-fills with a
//     placeholder when absent, and the mojang_uuid field
//     drives the upsert/bind logic in references/HA-ROADMAP.md §3.4 via
//     handleManageRegister.
//
// Both paths return profile_id (Profile.ID) on success. The service new-user case
// additionally returns cbh: 0 per §3.5.
func (ac *AuthController) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	// Service-side proxy registration now uses OAuth2 Bearer tokens instead of
	// remember_token/auth_type=manage. No Authorization header => normal user
	// registration path.
	isManage := false
	accessToken := bearerTokenFromRequest(c)
	if accessToken != "" {
		tokenContext, err := services.NewOAuth2Service().ResolveAccessToken(accessToken)
		if err != nil {
			respondError(c, http.StatusUnauthorized, CodeOAuthInvalidGrant, "invalid access token")
			return
		}
		if !tokenContext.IsService {
			respondError(c, http.StatusForbidden, CodeOAuthAccessDenied, "register proxy path requires a service token")
			return
		}
		if !hasScope(tokenContext.Scopes, "register.manage") {
			respondError(c, http.StatusForbidden, CodeOAuthInsufficientScope, "insufficient scope")
			return
		}
		isManage = true
	}

	// Username / password length always enforced.
	if len(req.Username) < 3 {
		respondError(c, http.StatusBadRequest, CodeUsernameTooShort, "Username too short")
		return
	}
	if len(req.Password) < 6 {
		respondError(c, http.StatusBadRequest, CodePasswordTooShort, "Password too short")
		return
	}

	// mojang_uuid format check (service proxy path; ignored on normal path).
	mojangUUID := strings.ToLower(strings.TrimSpace(req.MojangUUID))
	if mojangUUID != "" {
		if !isValidMojangUUID(mojangUUID) {
			respondError(c, http.StatusBadRequest, CodeInvalidMojangUUID, "Invalid mojang_uuid")
			return
		}
	}

	// Email handling differs per path.
	email := req.Email
	if isManage {
		if email == "" {
			email = strings.ToLower(req.Username) + "@mojang-imported.invalid"
		}
		// Service proxy path skips strict email-format validation.
	} else {
		if !isValidEmail(email) {
			respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
			return
		}
	}

	// Captcha: only on normal path.
	if !isManage && config.AppConfig.Security.EnableCaptcha {
		captchaService := services.NewCaptchaService()
		if req.CaptchaToken == "" || req.CaptchaCode == "" {
			respondError(c, http.StatusBadRequest, CodeCaptchaInvalid, "Invalid or expired captcha")
			return
		}
		if !captchaService.Verify(req.CaptchaToken, req.CaptchaCode) {
			respondError(c, http.StatusBadRequest, CodeCaptchaInvalid, "Invalid or expired captcha")
			return
		}
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Password hashing failed")
		return
	}

	ip := c.ClientIP()
	now := time.Now()

	if isManage {
		ac.handleManageRegister(c, req.Username, email, hash, mojangUUID, ip, now)
		return
	}

	// Normal WebUI registration: enforce uniqueness, then create.
	var count int64
	database.DB.Model(&models.User{}).Where("email = ?", email).Count(&count)
	if count > 0 {
		respondError(c, http.StatusConflict, CodeEmailAlreadyRegistered, "Email already registered")
		return
	}
	database.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		respondError(c, http.StatusConflict, CodeUsernameAlreadyTaken, "Username already taken")
		return
	}

	var maxUID uint
	database.DB.Model(&models.User{}).Select("COALESCE(MAX(uid), 0)").Scan(&maxUID)
	newUID := maxUID + 1
	uuid := utils.GenerateUnsignedUUID()

	user := models.User{
		UID:        newUID,
		UUID:       uuid,
		Email:      email,
		Username:   req.Username,
		Password:   hash,
		IP:         ip,
		RegIP:      ip,
		LastSignAt: &now,
		RegisterAt: &now,
		Verified:   false,
	}

	authService := services.NewAuthService()
	var profile *models.Profile
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		var pErr error
		profile, pErr = authService.CreateDefaultProfileForUserTx(tx, user.UUID, req.Username)
		return pErr
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to create user profile")
		return
	}

	respondOK(c, "Register successful", gin.H{
		"uid":        user.UID,
		"profile_id": profile.ID,
	})
}

// handleManageRegister implements the upsert/bind decision tree from
// references/HA-ROADMAP.md §3.4 for the service proxy registration path. The caller
// has already validated input, hashed the password, and computed the placeholder
// email where needed. This function is the only place that writes
// mojang_uuid / cbh for /register.
func (ac *AuthController) handleManageRegister(c *gin.Context, username, email, hash, mojangUUID, ip string, now time.Time) {
	authService := services.NewAuthService()
	// Per references/HA-ROADMAP.md §4.1: passively trigger cleanup on every
	// successful service /register. Serialization is handled inside the service.
	cleanupTriggered := false
	defer func() {
		if cleanupTriggered {
			go authService.CleanupInactiveBotUsers()
		}
	}()

	// Step 1: lookup by mojang_uuid (§3.4.1).
	if mojangUUID != "" {
		var byMojang models.User
		if err := database.DB.Where("mojang_uuid = ?", mojangUUID).First(&byMojang).Error; err == nil {
			// 1.1 hit: idempotent return.
			profile, pErr := authService.GetOrCreateProfileForUser(byMojang.UUID, byMojang.Username)
			if pErr != nil {
				respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to load profile")
				return
			}
			cleanupTriggered = true
			respondOK(c, "Register successful", gin.H{
				"uid":        byMojang.UID,
				"profile_id": profile.ID,
			})
			return
		}
	}

	// Step 2: lookup by username (§3.4.2).
	var byUsername models.User
	if err := database.DB.Where("username = ?", username).First(&byUsername).Error; err == nil {
		// Existing user.
		if mojangUUID == "" {
			// 2.d
			respondError(c, http.StatusBadRequest, CodeMojangUUIDRequired, "mojang_uuid is required for existing user")
			return
		}
		if byUsername.MojangUUID != nil && *byUsername.MojangUUID == mojangUUID {
			// 2.b idempotent.
			profile, _ := authService.GetOrCreateProfileForUser(byUsername.UUID, byUsername.Username)
			cleanupTriggered = true
			respondOK(c, "Register successful", gin.H{
				"uid":        byUsername.UID,
				"profile_id": profile.ID,
			})
			return
		}
		if byUsername.MojangUUID != nil && *byUsername.MojangUUID != mojangUUID {
			// 2.c conflict.
			respondError(c, http.StatusConflict, CodeUsernameAlreadyBound, "Username already bound")
			return
		}
		// 2.a bind: user exists with no mojang_uuid; M.T. supplies one.
		// Per mbe (Mojang Bind Enabled):
		//   mbe=0 → reject: HA priority, Mojang player cannot claim this username.
		//   mbe=1 → bind: set mojang_uuid + last_sign_at, reset mbe=0; preserve
		//             password/email/cbh (the WebUI user's credentials are kept).
		if !byUsername.MBE {
			respondError(c, http.StatusConflict, CodeUsernameAlreadyBound, "Username already bound")
			return
		}
		if err := database.DB.Model(&byUsername).Updates(map[string]interface{}{
			"mojang_uuid":  mojangUUID,
			"last_sign_at": now,
			"mbe":          false,
		}).Error; err != nil {
			respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to bind mojang_uuid")
			return
		}
		CancelMBETimeout(byUsername.UID)
		profile, _ := authService.GetOrCreateProfileForUser(byUsername.UUID, byUsername.Username)
		cleanupTriggered = true
		respondOK(c, "Register successful", gin.H{
			"uid":        byUsername.UID,
			"profile_id": profile.ID,
		})
		return
	}

	// Step 2 miss: create new user.
	// cbh = 0 (代注册) when mojang_uuid is provided, else cbh = 1.
	var maxUID uint
	database.DB.Model(&models.User{}).Select("COALESCE(MAX(uid), 0)").Scan(&maxUID)
	newUID := maxUID + 1
	uuid := utils.GenerateUnsignedUUID()

	cbh := true
	var muuidPtr *string
	if mojangUUID != "" {
		cbh = false
		muuidPtr = &mojangUUID
	}

	user := models.User{
		UID:        newUID,
		UUID:       uuid,
		Email:      email,
		Username:   username,
		Password:   hash,
		IP:         ip,
		RegIP:      ip,
		LastSignAt: &now,
		RegisterAt: &now,
		Verified:   false,
		CBH:        cbh,
		MojangUUID: muuidPtr,
	}

	var profile *models.Profile
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		var pErr error
		profile, pErr = authService.CreateDefaultProfileForUserTx(tx, user.UUID, username)
		return pErr
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to create user")
		return
	}

	resp := gin.H{
		"uid":        user.UID,
		"profile_id": profile.ID,
	}
	if !user.CBH {
		// Per §3.5: cbh is only surfaced in the response when 0.
		resp["cbh"] = 0
	}
	cleanupTriggered = true
	respondOK(c, "Register successful", resp)
}

func (ac *AuthController) Logout(c *gin.Context) {
	accessToken := bearerTokenFromRequest(c)
	if accessToken == "" {
		respondError(c, http.StatusUnauthorized, CodeOAuthLoginRequired, "missing bearer token")
		return
	}
	if err := services.NewOAuth2Service().RevokeAccessToken(accessToken); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to revoke token")
		return
	}
	respondOK(c, "Logout successful", nil)
}

// LoginByMT handles POST /loginbymt. It issues a remember_token for a user
// identified by UID or Email, authenticated by the operator-level M-T.
func (ac *AuthController) LoginByMT(c *gin.Context) {
	respondError(c, http.StatusGone, CodeEndpointDeprecated, "POST /loginbymt is deprecated; use OAuth2 client_credentials instead")
}

// ClaimUser handles POST /admin/claim-user. It lets an operator (authenticated
// via OAuth2 Service Token with scope user.claim.as-service) take a proxy-
// registered user (cbh=0) and assign them a real email + password so that the
// user can log in to the business system via WebUI.
//
// Pre-conditions:
//   - Caller must present a Service Token with scope user.claim.as-service.
//   - The target user must exist, must be a proxy registration (cbh=0), and
//     is matched by username.
//   - email must be a valid email address; password must satisfy the same
//     minimum-length rule as WebUI registration.
//
// On success: email is updated, password is re-hashed (bcrypt), and cbh is
// flipped from 0 to 1 (so the user is no longer eligible for bot-user cleanup).
func (ac *AuthController) ClaimUser(c *gin.Context) {
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
		respondError(c, http.StatusForbidden, CodeOAuthAccessDenied, "claim-user requires a service token")
		return
	}
	if !hasScope(tokenContext.Scopes, "user.claim.as-service") {
		respondError(c, http.StatusForbidden, CodeOAuthInsufficientScope, "insufficient scope")
		return
	}

	var req ClaimUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if len(username) < 3 {
		respondError(c, http.StatusBadRequest, CodeUsernameTooShort, "Username too short")
		return
	}
	if !isValidEmail(req.Email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}
	if len(req.Password) < 6 {
		respondError(c, http.StatusBadRequest, CodePasswordTooShort, "Password too short")
		return
	}

	// Email uniqueness: a freshly claimed user must not collide with an
	// existing account's email (proxy registrations use placeholder emails,
	// so collisions on the placeholder domain are theoretically possible).
	var emailCount int64
	database.DB.Model(&models.User{}).Where("email = ?", req.Email).Count(&emailCount)
	if emailCount > 0 {
		respondError(c, http.StatusConflict, CodeEmailAlreadyRegistered, "Email already registered")
		return
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Password hashing failed")
		return
	}

	var target models.User
	if err := database.DB.Where("username = ?", username).First(&target).Error; err != nil {
		respondError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
		return
	}
	if target.CBH {
		respondError(c, http.StatusConflict, CodeUserNotClaimable, "user is not a proxy-registered account")
		return
	}

	if err := database.DB.Model(&target).Updates(map[string]interface{}{
		"email":    req.Email,
		"password": hash,
		"cbh":      true,
		"verified": true,
	}).Error; err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to claim user")
		return
	}

	respondOK(c, "User claimed", gin.H{
		"uid":      target.UID,
		"username": target.Username,
		"email":    req.Email,
	})
}
