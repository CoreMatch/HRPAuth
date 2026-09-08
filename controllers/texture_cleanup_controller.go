package controllers

import (
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type TextureCleanupController struct {
	textureService *services.TextureService
}

func NewTextureCleanupController() *TextureCleanupController {
	return &TextureCleanupController{
		textureService: services.NewTextureService(),
	}
}

func (tcc *TextureCleanupController) Start(interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	tcc.runOnce()
	go tcc.loop(interval)
}

func (tcc *TextureCleanupController) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		tcc.runOnce()
	}
}

func (tcc *TextureCleanupController) runOnce() {
	deleted := tcc.textureService.CleanupOrphanedTextures()
	if deleted > 0 {
		log.Printf("[TextureCleanup] removed %d expired orphan texture files", deleted)
	}
}
