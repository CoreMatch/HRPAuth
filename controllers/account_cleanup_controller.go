package controllers

import (
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type AccountCleanupController struct {
	authService *services.AuthService
}

func NewAccountCleanupController() *AccountCleanupController {
	return &AccountCleanupController{
		authService: services.NewAuthService(),
	}
}

func (acc *AccountCleanupController) Start(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * 24 * time.Hour
	}
	// 启动时运行一次
	acc.runOnce()
	go acc.loop(interval)
}

func (acc *AccountCleanupController) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		acc.runOnce()
	}
}

func (acc *AccountCleanupController) runOnce() {
	deleted := acc.authService.CleanupDeletedAccounts()
	if deleted > 0 {
		log.Printf("[AccountCleanup] hard deleted %d accounts marked for deletion", deleted)
	}
}
