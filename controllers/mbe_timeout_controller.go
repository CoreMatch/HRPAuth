package controllers

import (
	"log"
	"sync"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
)

const mbeTimeout = 15 * time.Minute

var (
	mbePending   = make(map[uint]time.Time) // uid → expiry
	mbePendingMu sync.Mutex
)

// RegisterMBETimeout records a 15-minute deadline for the given UID.
// After the deadline, the background loop will reset mbe to 0.
func RegisterMBETimeout(uid uint) {
	mbePendingMu.Lock()
	defer mbePendingMu.Unlock()
	mbePending[uid] = time.Now().Add(mbeTimeout)
}

// CancelMBETimeout removes a pending timeout (e.g. after successful bind or
// explicit disable).
func CancelMBETimeout(uid uint) {
	mbePendingMu.Lock()
	defer mbePendingMu.Unlock()
	delete(mbePending, uid)
}

// StartMBETimeoutLoop launches a background goroutine that periodically scans
// the pending map and disables mbe for expired entries.
func StartMBETimeoutLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go mbeTimeoutLoop(interval)
}

func mbeTimeoutLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		mbeTimeoutRunOnce()
	}
}

func mbeTimeoutRunOnce() {
	now := time.Now()
	mbePendingMu.Lock()
	expired := make([]uint, 0)
	for uid, expiry := range mbePending {
		if now.After(expiry) || now.Equal(expiry) {
			expired = append(expired, uid)
			delete(mbePending, uid)
		}
	}
	mbePendingMu.Unlock()

	for _, uid := range expired {
		var user models.User
		if err := database.DB.Where("uid = ?", uid).First(&user).Error; err != nil {
			continue
		}
		if user.MojangUUID != nil {
			// Bind already completed; skip.
			continue
		}
		if err := database.DB.Model(&user).Update("mbe", false).Error; err != nil {
			log.Printf("[MBETimeout] failed to disable mbe for uid %d: %v", uid, err)
			continue
		}
		log.Printf("[MBETimeout] uid %d: mbe auto-disabled after %s", uid, mbeTimeout)
	}
}
