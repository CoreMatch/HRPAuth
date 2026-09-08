package controllers

import (
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/services"
)

// OAuth2CleanupController 定期清理已过期/已撤销的 OAuth2 access token，
// 以及已过期/已消费的 authorization code。
type OAuth2CleanupController struct {
	oauth2Service *services.OAuth2Service
}

func NewOAuth2CleanupController() *OAuth2CleanupController {
	return &OAuth2CleanupController{
		oauth2Service: services.NewOAuth2Service(),
	}
}

func (occ *OAuth2CleanupController) Start(interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	occ.runOnce()
	go occ.loop(interval)
}

func (occ *OAuth2CleanupController) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		occ.runOnce()
	}
}

func (occ *OAuth2CleanupController) runOnce() {
	if deleted, err := occ.oauth2Service.CleanupExpiredAccessTokens(); err != nil {
		log.Printf("[OAuth2Cleanup] failed to clean access tokens: %v", err)
	} else if deleted > 0 {
		log.Printf("[OAuth2Cleanup] removed %d expired/revoked access tokens", deleted)
	}

	if deleted, err := occ.oauth2Service.CleanupExpiredAuthorizationCodes(); err != nil {
		log.Printf("[OAuth2Cleanup] failed to clean authorization codes: %v", err)
	} else if deleted > 0 {
		log.Printf("[OAuth2Cleanup] removed %d expired authorization codes", deleted)
	}
}
