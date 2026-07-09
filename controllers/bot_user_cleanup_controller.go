package controllers

import (
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/services"
)

// BotUserCleanupController runs the periodic cleanup of inactive WinnerProxy-
// generated (cbh=0) user accounts. See references/HA-ROADMAP.md §4.
//
// On startup it runs once immediately, then loops every 24h. Each run is
// serialized via services.AuthService.CleanupInactiveBotUsers' internal mutex
// so concurrent triggers (e.g. the M.T. /register post-success hook) cannot
// race with the periodic loop.
type BotUserCleanupController struct {
	authService *services.AuthService
}

func NewBotUserCleanupController() *BotUserCleanupController {
	return &BotUserCleanupController{
		authService: services.NewAuthService(),
	}
}

// Start runs the cleanup once on startup, then loops every `interval`.
// interval <= 0 falls back to the 24h default.
func (bcc *BotUserCleanupController) Start(interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	bcc.runOnce()
	go bcc.loop(interval)
}

func (bcc *BotUserCleanupController) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		bcc.runOnce()
	}
}

func (bcc *BotUserCleanupController) runOnce() {
	deleted := bcc.authService.CleanupInactiveBotUsers()
	if deleted > 0 {
		log.Printf("[BotUserCleanup] removed %d inactive bot users", deleted)
	}
}
