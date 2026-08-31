package controllers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// PresenceRecord 记录一个已注册微服务的存在状态。
// ExpiresAt 为零值表示该服务永不过期，一直保留到进程结束。
type PresenceRecord struct {
	Name      string    `json:"name"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PresenceRegistry 进程内维护所有已注册微服务的存在状态。
// 程序结束时 registry 随之销毁，天然满足"永不过期即保留到程序结束"。
type PresenceRegistry struct {
	mu      sync.RWMutex
	records map[string]PresenceRecord
}

func NewPresenceRegistry() *PresenceRegistry {
	return &PresenceRegistry{records: make(map[string]PresenceRecord)}
}

// Register 注册或刷新一个服务的心跳。
// ttlSeconds <= 0 表示永不过期（未指定或显式指定为不过期）。
func (r *PresenceRegistry) Register(name string, ttlSeconds int) PresenceRecord {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		record = PresenceRecord{Name: name, FirstSeen: now}
	}
	record.LastSeen = now
	if ttlSeconds > 0 {
		record.ExpiresAt = now.Add(time.Duration(ttlSeconds) * time.Second)
	} else {
		// 未指定或显式不过期：清除过期时间，永久保留。
		record.ExpiresAt = time.Time{}
	}
	r.records[name] = record
	return record
}

// Get 返回指定服务的存在记录。服务不存在或已过期时返回 false，
// 已过期的记录会被惰性清除。
func (r *PresenceRegistry) Get(name string) (PresenceRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		return PresenceRecord{}, false
	}
	if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
		delete(r.records, name)
		return PresenceRecord{}, false
	}
	return record, true
}

type PresenceController struct {
	registry *PresenceRegistry
}

func NewPresenceController() *PresenceController {
	return &PresenceController{registry: NewPresenceRegistry()}
}

type PresenceRequest struct {
	Name string `json:"name"`
	// TTLSeconds 为服务自定的存在时间（秒）。
	// 未指定（0）或指定为负数表示永不过期，保留到主服务进程结束。
	TTLSeconds int `json:"ttl_seconds"`
}

func (pc *PresenceController) Bonjour(c *gin.Context) {
	var req PresenceRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	record := pc.registry.Register(name, req.TTLSeconds)

	var expiresAt any
	if !record.ExpiresAt.IsZero() {
		expiresAt = record.ExpiresAt
	}

	respondOK(c, "ca va très bien, merci", gin.H{
		"service":    record.Name,
		"first_seen": record.FirstSeen,
		"last_seen":  record.LastSeen,
		"expires_at": expiresAt,
	})
}
