package controllers

import (
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type SessionCleanupController struct {
	authService *services.AuthService
}

func NewSessionCleanupController() *SessionCleanupController {
	return &SessionCleanupController{
		authService: services.NewAuthService(),
	}
}

func (scc *SessionCleanupController) Start(interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	scc.runOnce()
	go scc.loop(interval)
}

func (scc *SessionCleanupController) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		scc.runOnce()
	}
}

func (scc *SessionCleanupController) runOnce() {
	deleted := scc.authService.CleanupExpiredSessions()
	if deleted > 0 {
		log.Printf("[SessionCleanup] removed %d sessions expired for more than 24 hours", deleted)
	}
}
