package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/clients"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/redis"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	yggClient *clients.YggdrasilClient
}

func NewAuthService() *AuthService {
	return &AuthService{
		yggClient: clients.NewYggdrasilClient(),
	}
}

type UserInfo struct {
	UUID     string
	Email    string
	Username string
	Password string
}

func (as *AuthService) VerifyCredentials(identifier, password string, allowUsernameLogin bool) *UserInfo {
	var user models.User
	var err error

	// 核心服务仅支持通过 Email 或 Username 登录，不再关联 Minecraft Profile Name
	err = database.DB.Unscoped().Where("email = ? OR username = ?", identifier, identifier).First(&user).Error

	if err != nil {
		return nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil
	}

	// 如果账号之前标记了删除，登录成功则解除删除标记（恢复账号）
	if user.DeletedAt != nil {
		database.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&user).Update("deleted_at", nil).Error; err != nil {
				return err
			}
			return tx.Where("uid = ?", user.UID).Delete(&models.DeletedAccount{}).Error
		})
	}

	return &UserInfo{
		UUID:     user.UUID,
		Email:    user.Email,
		Username: user.Username,
		Password: user.Password,
	}
}

// deleteUserCascade removes a user and all core dependent rows.
func (as *AuthService) deleteUserCascade(u models.User) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", u.UUID).Delete(&models.OAuth2AuthorizationCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.UUID).Delete(&models.OAuth2AccessToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.UUID).Delete(&models.OAuth2RefreshToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.UUID).Delete(&models.WebAuthnCredential{}).Error; err != nil {
			return err
		}

		// 从已删除账号记录表中移除
		if err := tx.Where("uid = ?", u.UID).Delete(&models.DeletedAccount{}).Error; err != nil {
			return err
		}

		// 彻底删除用户记录
		if err := tx.Unscoped().Delete(&u).Error; err != nil {
			return err
		}

		return nil
	})
}

func formatCleanupDate(t *time.Time) string {
	if t == nil {
		return "nil"
	}
	return t.Format("2006-01-02")
}

// CleanupDeletedAccounts 彻底删除标记超过3天的账号及其关联数据。
func (as *AuthService) CleanupDeletedAccounts() int {
	cutoff := time.Now().Add(-3 * 24 * time.Hour)

	var candidates []models.User
	if err := database.DB.Unscoped().Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).Find(&candidates).Error; err != nil {
		log.Printf("[account-cleanup] failed to query candidates: %v", err)
		return 0
	}

	deleted := 0
	for _, u := range candidates {
                  if err := as.yggClient.DeleteAccount(u.UUID); err != nil {
                          log.Printf("[account-cleanup] failed to delete Yggdrasil account for uid=%d username=%s: %v", u.UID, u.Username, err)
                          continue
                  }
		if err := as.deleteUserCascade(u); err != nil {
			log.Printf("[account-cleanup] ERROR hard deleting uid=%d username=%s: %v", u.UID, u.Username, err)
			continue
		}
		deleted++
		log.Printf("[account-cleanup] - uid=%d username=%s (marked deleted at %s)",
			u.UID, u.Username, formatCleanupDate(u.DeletedAt),
		)
	}
	if len(candidates) > 0 {
		log.Printf("[account-cleanup] scanned %d candidates, hard deleted %d", len(candidates), deleted)
	}
	return deleted
}

func (as *AuthService) ChangeUsername(userUUID, newUsername string) error {
	var user models.User
	if err := database.DB.Where("uuid = ?", userUUID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	if user.Username == newUsername {
		return nil
	}

	var existingUser models.User
	if err := database.DB.Where("username = ? AND uuid != ?", newUsername, user.UUID).First(&existingUser).Error; err == nil {
		return fmt.Errorf("username already exists")
	}

          if err := database.DB.Model(&models.User{}).Where("uuid = ?", user.UUID).Update("username", newUsername).Error; err != nil {
		return err
	}

          if err := as.yggClient.SyncUsername(user.UUID, newUsername); err != nil {
                  if revertErr := database.DB.Model(&models.User{}).Where("uuid = ?", user.UUID).Update("username", user.Username).Error; revertErr != nil {
                          return fmt.Errorf("failed to sync username to yggdrasil api: %v (revert failed: %v)", err, revertErr)
                  }
                  return fmt.Errorf("failed to sync username to yggdrasil api: %v", err)
          }

	return nil
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
	}
}

// IsManageToken reports whether the request is a genuine Manage Token (M-T)
func (as *AuthService) IsManageToken(token, authType string) bool {
	return authType == "manage" && token != "" && config.AppConfig.Manage.Token != "" && token == config.AppConfig.Manage.Token
}
