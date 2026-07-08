package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct{}

func NewAuthService() *AuthService {
	return &AuthService{}
}

type UserInfo struct {
	UUID     string
	Email    string
	Username string
	Password string
	Locale   string
}

func (as *AuthService) VerifyCredentials(identifier, password string, allowUsernameLogin bool) *UserInfo {
	nonEmailLogin := allowUsernameLogin
	if !allowUsernameLogin {
		nonEmailLogin = config.AppConfig.Yggdrasil.FeatureFlags.NonEmailLogin
	}

	var user models.User
	var err error

	if nonEmailLogin {
		err = database.DB.
			Joins("JOIN profiles ON users.uuid = profiles.user_id").
			Where("users.email = ? OR profiles.name = ?", identifier, identifier).
			First(&user).Error
	} else {
		err = database.DB.Where("email = ?", identifier).First(&user).Error
	}

	if err != nil {
		return nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil
	}

	return &UserInfo{
		UUID:     user.UUID,
		Email:    user.Email,
		Username: user.Username,
		Password: user.Password,
		Locale:   user.Locale,
	}
}

type ProfileInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

func (as *AuthService) GetUserProfiles(userUUID string) []ProfileInfo {
	var profiles []models.Profile
	database.DB.Where("user_id = ?", userUUID).Find(&profiles)

	result := make([]ProfileInfo, 0, len(profiles))
	for _, p := range profiles {
		pi := ProfileInfo{
			ID:   p.ID,
			Name: p.Name,
		}
		if p.Model != "" {
			pi.Model = p.Model
		}
		result = append(result, pi)
	}
	return result
}

func (as *AuthService) CreateDefaultProfileForUserTx(tx *gorm.DB, userUUID, profileName string) (*models.Profile, error) {
	var count int64
	if err := tx.Model(&models.Profile{}).Where("user_id = ?", userUUID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}

	profile := models.Profile{
		ID:     utils.GenerateUnsignedUUID(),
		UserID: userUUID,
		Name:   profileName,
		Model:  "default",
	}
	if err := tx.Create(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func (as *AuthService) CreateDefaultProfileForUser(userUUID, profileName string) (*models.Profile, error) {
	return as.CreateDefaultProfileForUserTx(database.DB, userUUID, profileName)
}

func (as *AuthService) RenameProfile(userUUID, profileID, newName string) (*models.Profile, error) {
	var profile models.Profile
	if err := database.DB.Where("id = ? AND user_id = ?", profileID, userUUID).First(&profile).Error; err != nil {
		return nil, fmt.Errorf("profile not found")
	}
	if profile.Name == newName {
		return &profile, nil
	}

	var existing models.Profile
	if err := database.DB.Where("name = ? AND id != ?", newName, profileID).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("profile name already exists")
	}

	if err := database.DB.Model(&profile).Update("name", newName).Error; err != nil {
		return nil, err
	}

	profile.Name = newName
	return &profile, nil
}

func (as *AuthService) SyncUserAndProfileName(userUUID, profileID, newName string) (*models.User, *models.Profile, error) {
	var user models.User
	if err := database.DB.Where("uuid = ?", userUUID).First(&user).Error; err != nil {
		return nil, nil, fmt.Errorf("user not found")
	}

	var profile models.Profile
	if profileID == "" {
		if err := database.DB.Where("user_id = ?", userUUID).Order("created_at ASC").First(&profile).Error; err != nil {
			return nil, nil, fmt.Errorf("profile not found")
		}
		profileID = profile.ID
	} else {
		if err := database.DB.Where("id = ? AND user_id = ?", profileID, userUUID).First(&profile).Error; err != nil {
			return nil, nil, fmt.Errorf("profile not found")
		}
	}

	if user.Username == newName && profile.Name == newName {
		return &user, &profile, nil
	}

	var existingUser models.User
	if err := database.DB.Where("username = ? AND uuid != ?", newName, user.UUID).First(&existingUser).Error; err == nil {
		return nil, nil, fmt.Errorf("username already exists")
	}

	var existingProfile models.Profile
	if err := database.DB.Where("name = ? AND id != ?", newName, profile.ID).First(&existingProfile).Error; err == nil {
		return nil, nil, fmt.Errorf("profile name already exists")
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("uuid = ?", userUUID).Update("username", newName).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Profile{}).Where("id = ?", profileID).Update("name", newName).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	user.Username = newName
	profile.Name = newName
	return &user, &profile, nil
}

func (as *AuthService) CreateToken(accessToken, clientToken, userID, profileID string, expiresInDays int) bool {
	token := models.Token{
		AccessToken:       accessToken,
		ClientToken:       clientToken,
		UserID:            userID,
		SelectedProfileID: profileID,
		IssuedAt:          utils.CurrentTimestampMillis(),
		ExpiresInDays:     expiresInDays,
		State:             "valid",
	}
	result := database.DB.Create(&token)
	return result.Error == nil
}

func (as *AuthService) InvalidateToken(accessToken string) bool {
	result := database.DB.Model(&models.Token{}).
		Where("access_token = ?", accessToken).
		Update("state", "invalid")
	return result.Error == nil
}

func (as *AuthService) InvalidateAllUserTokens(userID string) bool {
	result := database.DB.Model(&models.Token{}).
		Where("user_id = ? AND state = ?", userID, "valid").
		Update("state", "invalid")
	return result.Error == nil
}

func (as *AuthService) GetValidTokenByClientToken(userID, clientToken string) *models.Token {
	if clientToken == "" {
		return nil
	}
	var token models.Token
	result := database.DB.Where("user_id = ? AND client_token = ? AND state = ?",
		userID, clientToken, "valid").First(&token)
	if result.Error != nil {
		return nil
	}
	nowMillis := utils.CurrentTimestampMillis()
	expiryMillis := token.IssuedAt + int64(token.ExpiresInDays)*24*60*60*1000
	if nowMillis > expiryMillis {
		as.InvalidateToken(token.AccessToken)
		return nil
	}
	return &token
}

func (as *AuthService) ValidateToken(accessToken string, clientToken string) *models.Token {
	var token models.Token
	result := database.DB.Where("access_token = ? AND state = ?", accessToken, "valid").First(&token)
	if result.Error != nil {
		return nil
	}

	if clientToken != "" && clientToken != token.ClientToken {
		return nil
	}

	nowMillis := utils.CurrentTimestampMillis()
	expiryMillis := token.IssuedAt + int64(token.ExpiresInDays)*24*60*60*1000
	if nowMillis > expiryMillis {
		as.InvalidateToken(accessToken)
		return nil
	}

	return &token
}

func (as *AuthService) ValidateTokenForRefresh(accessToken string, clientToken string) *models.Token {
	var token models.Token
	result := database.DB.Where("access_token = ? AND state IN ?", accessToken, []string{"valid", "temporarily_invalid"}).
		First(&token)
	if result.Error != nil {
		return nil
	}

	if clientToken != "" && clientToken != token.ClientToken {
		return nil
	}

	nowMillis := utils.CurrentTimestampMillis()
	expiryMillis := token.IssuedAt + int64(token.ExpiresInDays)*24*60*60*1000
	if nowMillis > expiryMillis {
		as.InvalidateToken(accessToken)
		return nil
	}

	return &token
}

func (as *AuthService) RefreshTokenExpiry(accessToken string, expiresInDays int) bool {
	nowMillis := utils.CurrentTimestampMillis()
	result := database.DB.Model(&models.Token{}).
		Where("access_token = ?", accessToken).
		Updates(map[string]interface{}{
			"issued_at":       nowMillis,
			"expires_in_days": expiresInDays,
		})
	return result.Error == nil
}

func (as *AuthService) MarkOtherClientTokensTemporarilyInvalid(userID, currentClientToken string) int64 {
	result := database.DB.Model(&models.Token{}).
		Where("user_id = ? AND client_token != ? AND state = ?", userID, currentClientToken, "valid").
		Update("state", "temporarily_invalid")
	if result.Error != nil {
		return 0
	}
	return result.RowsAffected
}

func (as *AuthService) CleanupExpiredTokens() int64 {
	nowMillis := utils.CurrentTimestampMillis()
	cutoff := nowMillis - int64(config.AppConfig.Yggdrasil.Security.TokenExpiryDays+1)*24*60*60*1000
	result := database.DB.Where("state = ? OR issued_at < ?", "invalid", cutoff).
		Delete(&models.Token{})
	if result.Error != nil {
		return 0
	}
	return result.RowsAffected
}

func (as *AuthService) GetProfileByID(profileID string) *ProfileInfo {
	var profile models.Profile
	var user models.User

	result := database.DB.Where("id = ?", profileID).First(&profile)
	if result.Error != nil {
		return nil
	}

	database.DB.Where("uuid = ?", profile.UserID).First(&user)

	return &ProfileInfo{
		ID:    profile.ID,
		Name:  profile.Name,
		Model: profile.Model,
	}
}

func (as *AuthService) IsProfileOwnedByUser(profileID, userID string) bool {
	var count int64
	database.DB.Model(&models.Profile{}).
		Where("id = ? AND user_id = ?", profileID, userID).
		Count(&count)
	return count > 0
}

func (as *AuthService) CreateSession(profileID, serverID, ip string) bool {
	session := models.Session{
		ProfileID: profileID,
		ServerID:  serverID,
		IP:        ip,
		ExpiresAt: time.Now().Add(time.Duration(config.AppConfig.Yggdrasil.Security.SessionExpirySeconds) * time.Second),
	}
	result := database.DB.Create(&session)
	return result.Error == nil
}

func (as *AuthService) GetSessionByProfileAndServer(profileName, serverID string) *models.Session {
	var profile models.Profile
	database.DB.Where("name = ?", profileName).First(&profile)
	if profile.ID == "" {
		return nil
	}

	var session models.Session
	result := database.DB.
		Where("profile_id = ? AND server_id = ? AND expires_at > ?", profile.ID, serverID, time.Now()).
		Order("created_at DESC").
		First(&session)

	if result.Error != nil {
		return nil
	}
	return &session
}

func (as *AuthService) IsLoginRateLimited(identifier string) bool {
	cfg := config.AppConfig.Security
	key := fmt.Sprintf("%slogin_attempts:%s", config.AppConfig.Redis.Prefix, identifier)

	ctx := context.Background()
	countStr, err := redis.Client.Get(ctx, key).Result()
	if err != nil {
		return false
	}

	count, err := strconv.Atoi(countStr)
	if err != nil {
		return false
	}

	return count >= cfg.RateLimitMaxAttempts
}

func (as *AuthService) RecordLoginAttempt(identifier string, success bool) {
	cfg := config.AppConfig.Security
	key := fmt.Sprintf("%slogin_attempts:%s", config.AppConfig.Redis.Prefix, identifier)
	window := time.Duration(cfg.RateLimitWindowSec) * time.Second

	ctx := context.Background()

	if success {
		redis.Client.Del(ctx, key)
		return
	}

	pipe := redis.Client.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	_, _ = pipe.Exec(ctx)
}

func (as *AuthService) GetUserByID(userUUID string) *UserInfo {
	var user models.User
	result := database.DB.Where("uuid = ?", userUUID).First(&user)
	if result.Error != nil {
		return nil
	}

	return &UserInfo{
		UUID:     user.UUID,
		Email:    user.Email,
		Username: user.Username,
		Locale:   user.Locale,
	}
}

func (as *AuthService) GetProfileByName(name string) *models.Profile {
	var profile models.Profile
	result := database.DB.Where("name = ?", name).First(&profile)
	if result.Error != nil {
		return nil
	}
	return &profile
}

// ResolveToken attempts to authenticate the given remtoken. It supports two
// kinds of tokens:
//   - The operator-level Manage Token (M-T) stored in config.AppConfig.Manage.Token.
//     When matched, the request is treated as authenticated and the caller must
//     identify the target user via uid/email where the endpoint requires one.
//     The returned *models.User is nil and isManage is true.
//   - A regular user's remember_token. When matched, the user is returned and
//     isManage is false.
//
// If neither matches, (nil, false) is returned and the request is unauthorized.
func (as *AuthService) ResolveToken(token string) (*models.User, bool) {
	if token == "" {
		return nil, false
	}

	if config.AppConfig.Manage.Token != "" && token == config.AppConfig.Manage.Token {
		return nil, true
	}

	var user models.User
	if err := database.DB.Where("remember_token = ?", token).First(&user).Error; err != nil {
		return nil, false
	}
	return &user, false
}

// IsManageToken reports whether the given token equals the operator-level
// Manage Token (M-T) from config.
func (as *AuthService) IsManageToken(token string) bool {
	return token != "" && config.AppConfig.Manage.Token != "" && token == config.AppConfig.Manage.Token
}
