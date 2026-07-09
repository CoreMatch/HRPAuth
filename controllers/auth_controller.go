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
// M.T.-authenticated (WinnerProxy) body described in references/HA-ROADMAP.md §3.
// Normal-path fields (CaptchaToken/CaptchaCode/MojangUUID/RememberToken) are
// ignored on the path that does not consume them.
type RegisterRequest struct {
	Email         string `json:"email"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	CaptchaToken  string `json:"captcha_token"`
	CaptchaCode   string `json:"captcha_code"`
	MojangUUID    string `json:"mojang_uuid"`
	RememberToken string `json:"remember_token"`
}

type LogoutRequest struct {
	RememberToken string `json:"remember_token"`
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

func (ac *AuthController) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	email := req.Email
	password := req.Password

	if !isValidEmail(email) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid email",
		})
		return
	}

	var user models.User
	result := database.DB.Where("email = ?", email).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Email or password incorrect",
		})
		return
	}

	if !utils.CheckPasswordHash(password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Email or password incorrect",
		})
		return
	}

	token := utils.GenerateRandomToken(32)
	now := time.Now()

	database.DB.Model(&user).Updates(map[string]interface{}{
		"remember_token": token,
		"last_sign_at":   now,
	})

	totp := 0
	if user.TOTP != "" {
		totp = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Login successful",
		"token":   token,
		"uid":     user.UID,
		"totp":    totp,
	})
}

// Register handles POST /register.
//
// Two paths are supported, dispatched on whether remember_token matches
// config.AppConfig.Manage.Token (the operator-level "Manage Token" / M-T):
//
//   - Normal WebUI registration: captcha enforced when enabled, email required
//     and validated, username/email uniqueness checked.
//   - M.T. (WinnerProxy) registration: captcha and email-format checks are
//     skipped, email auto-fills with a placeholder when absent, and the
//     mojang_uuid field drives the upsert/bind logic in references/HA-ROADMAP.md
//     §3.4 via handleManageRegister.
//
// Both paths return profile_id (Profile.ID) on success. The M.T. new-user case
// additionally returns cbh: 0 per §3.5.
func (ac *AuthController) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	isManage := req.RememberToken != "" &&
		config.AppConfig.Manage.Token != "" &&
		req.RememberToken == config.AppConfig.Manage.Token

	// Username / password length always enforced.
	if len(req.Username) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Username too short",
		})
		return
	}
	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Password too short",
		})
		return
	}

	// mojang_uuid format check (M.T. path; ignored on normal path).
	mojangUUID := strings.ToLower(strings.TrimSpace(req.MojangUUID))
	if mojangUUID != "" {
		if !isValidMojangUUID(mojangUUID) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "invalid_mojang_uuid",
			})
			return
		}
	}

	// Email handling differs per path.
	email := req.Email
	if isManage {
		if email == "" {
			email = strings.ToLower(req.Username) + "@mojang-imported.invalid"
		}
		// M.T. path skips strict email-format validation per §3.1.
	} else {
		if !isValidEmail(email) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "Invalid email",
			})
			return
		}
	}

	// Captcha: only on normal path.
	if !isManage && config.AppConfig.Security.EnableCaptcha {
		captchaService := services.NewCaptchaService()
		if req.CaptchaToken == "" || req.CaptchaCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "Invalid or expired captcha",
			})
			return
		}
		if !captchaService.Verify(req.CaptchaToken, req.CaptchaCode) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "Invalid or expired captcha",
			})
			return
		}
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Password hashing failed",
		})
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
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "Email already registered",
		})
		return
	}
	database.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "Username already taken",
		})
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
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to create user profile",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"uid":        user.UID,
		"message":    "Register successful",
		"profile_id": profile.ID,
	})
}

// handleManageRegister implements the upsert/bind decision tree from
// references/HA-ROADMAP.md §3.4 for the M.T. (WinnerProxy) path. The caller
// has already validated input, hashed the password, and computed the placeholder
// email where needed. This function is the only place that writes
// mojang_uuid / cbh for /register.
func (ac *AuthController) handleManageRegister(c *gin.Context, username, email, hash, mojangUUID, ip string, now time.Time) {
	authService := services.NewAuthService()
	// Per references/HA-ROADMAP.md §4.1: passively trigger cleanup on every
	// successful M.T. /register. Serialization is handled inside the service.
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
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "Failed to load profile",
				})
				return
			}
			cleanupTriggered = true
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"uid":        byMojang.UID,
				"message":    "Register successful",
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
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "mojang_uuid_required_for_existing_user",
			})
			return
		}
		if byUsername.MojangUUID != nil && *byUsername.MojangUUID == mojangUUID {
			// 2.b idempotent.
			profile, _ := authService.GetOrCreateProfileForUser(byUsername.UUID, byUsername.Username)
			cleanupTriggered = true
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"uid":        byUsername.UID,
				"message":    "Register successful",
				"profile_id": profile.ID,
			})
			return
		}
		if byUsername.MojangUUID != nil && *byUsername.MojangUUID != mojangUUID {
			// 2.c conflict.
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "username_already_bound",
			})
			return
		}
		// 2.a bind: user exists with no mojang_uuid; M.T. supplies one.
		// Per mbe (Mojang Bind Enabled):
		//   mbe=0 → reject: HA priority, Mojang player cannot claim this username.
		//   mbe=1 → bind: only set mojang_uuid + last_sign_at; preserve
		//             password/email/cbh (the WebUI user's credentials are kept).
		if !byUsername.MBE {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "username_already_bound",
			})
			return
		}
		if err := database.DB.Model(&byUsername).Updates(map[string]interface{}{
			"mojang_uuid":  mojangUUID,
			"last_sign_at": now,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Failed to bind mojang_uuid",
			})
			return
		}
		profile, _ := authService.GetOrCreateProfileForUser(byUsername.UUID, byUsername.Username)
		cleanupTriggered = true
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"uid":        byUsername.UID,
			"message":    "Register successful",
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
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to create user",
		})
		return
	}

	resp := gin.H{
		"success":    true,
		"uid":        user.UID,
		"message":    "Register successful",
		"profile_id": profile.ID,
	}
	if !user.CBH {
		// Per §3.5: cbh is only surfaced in the response when 0.
		resp["cbh"] = 0
	}
	cleanupTriggered = true
	c.JSON(http.StatusOK, resp)
}

func (ac *AuthController) Logout(c *gin.Context) {
	token := ""

	var req LogoutRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.RememberToken != "" {
		token = req.RememberToken
	}

	if token == "" {
		token = c.PostForm("remember_token")
	}
	if token == "" {
		token = c.Query("remember_token")
	}

	if token != "" {
		// The Manage Token (M-T) lives in config and is not stored in the DB,
		// so the update below is a no-op for it. For a real remember_token
		// the row is matched and cleared.
		database.DB.Model(&models.User{}).
			Where("remember_token = ?", token).
			Update("remember_token", nil)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Logout successful",
	})
}
