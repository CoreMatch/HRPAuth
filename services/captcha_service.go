package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	redisClient "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/mojocn/base64Captcha"
)

type CaptchaService struct {
	driver *base64Captcha.DriverString
}

func NewCaptchaService() *CaptchaService {
	driver := base64Captcha.NewDriverString(
		80,  // height
		240, // width
		5,   // noise count
		base64Captcha.OptionShowHollowLine|base64Captcha.OptionShowSlimeLine|base64Captcha.OptionShowSineLine,
		4, // length — keep aligned with frontend Canvas captcha (4 characters)
		"abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789",
		nil, // bg color (random)
		nil, // fonts storage (default)
		[]string{},
	)
	driver.ConvertFonts()
	return &CaptchaService{driver: driver}
}

func (cs *CaptchaService) captchaKey(token string) string {
	return fmt.Sprintf("%scaptcha:code:%s", config.AppConfig.Redis.Prefix, token)
}

func (cs *CaptchaService) ttl() time.Duration {
	return time.Duration(config.AppConfig.Security.CaptchaTTL) * time.Second
}

// Generate creates a new captcha and stores its code in Redis.
// Returns the token (caller-facing identifier) and the raw code.
func (cs *CaptchaService) Generate() (token, code string, err error) {
	id, content, answer := cs.driver.GenerateIdQuestionAnswer()
	_ = answer // for DriverString content == answer; we keep the raw content for re-render

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := redisClient.Client.Set(ctx, cs.captchaKey(id), content, cs.ttl()).Err(); err != nil {
		return "", "", fmt.Errorf("failed to store captcha: %w", err)
	}
	return id, content, nil
}

// Render re-renders the captcha image for the given token. Returns the PNG bytes.
// Returns an error if the token is missing/expired (Redis miss).
func (cs *CaptchaService) Render(token string) ([]byte, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	code, err := redisClient.Client.Get(ctx, cs.captchaKey(token)).Result()
	if err != nil {
		return nil, fmt.Errorf("captcha not found or expired: %w", err)
	}

	item, err := cs.driver.DrawCaptcha(code)
	if err != nil {
		return nil, fmt.Errorf("failed to draw captcha: %w", err)
	}

	var buf bytes.Buffer
	if _, err := item.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("failed to encode captcha: %w", err)
	}
	return buf.Bytes(), nil
}

// Verify checks the user-supplied code against the stored captcha.
// On success, the captcha is removed from Redis (single-use).
// Comparison is case-insensitive.
func (cs *CaptchaService) Verify(token, userCode string) bool {
	if token == "" || userCode == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := cs.captchaKey(token)

	stored, err := redisClient.Client.Get(ctx, key).Result()
	if err != nil {
		return false
	}

	if !strings.EqualFold(strings.TrimSpace(stored), strings.TrimSpace(userCode)) {
		return false
	}

	// Single-use: delete on successful verification
	redisClient.Client.Del(ctx, key)
	return true
}
