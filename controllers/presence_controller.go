package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/redis"
)

const presenceKeyPrefix = "services:presence:"
const presenceTTL = 60 * time.Second

type PresenceController struct{}

func NewPresenceController() *PresenceController {
	return &PresenceController{}
}

type PresenceRequest struct {
	Name string `json:"name"`
}

type PresenceRecord struct {
	Name      string    `json:"name"`
	LastSeen  time.Time `json:"last_seen"`
	FirstSeen time.Time `json:"first_seen"`
}

func (pc *PresenceController) Bonjour(c *gin.Context) {
	var req PresenceRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	key := config.AppConfig.Redis.Prefix + presenceKeyPrefix + name
	ctx := context.Background()

	existingJSON, err := redis.Client.Get(ctx, key).Result()
	var firstSeen time.Time
	now := time.Now()

	if err == nil && existingJSON != "" {
		var existing PresenceRecord
		if json.Unmarshal([]byte(existingJSON), &existing) == nil {
			firstSeen = existing.FirstSeen
		}
	}
	if firstSeen.IsZero() {
		firstSeen = now
	}

	record := PresenceRecord{
		Name:      name,
		LastSeen:  now,
		FirstSeen: firstSeen,
	}

	recordJSON, err := json.Marshal(record)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "failed to register presence")
		return
	}

	if err := redis.Client.Set(ctx, key, string(recordJSON), presenceTTL).Err(); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "failed to register presence")
		return
	}

	respondOK(c, "ca va très bien, merci", gin.H{
		"service": name,
	})
}
