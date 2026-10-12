package controllers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/registry"
)

type PresenceController struct {
	registry *registry.PresenceRegistry
}

func NewPresenceController(r *registry.PresenceRegistry) *PresenceController {
	return &PresenceController{registry: r}
}

type PresenceRequest struct {
	Name string `json:"name"`
	// TTLSeconds 为服务自定的存在时间（秒）。
	// 未指定（0）或指定为负数表示永不过期，保留到主服务进程结束。
	TTLSeconds int `json:"ttl_seconds"`
	// Scope 为服务声明的作用区域，可选。
	Scope *registry.PresenceScope `json:"scope"`
	// SecurityLevel 为该服务的鉴权级别：0 无须 / 1 用户级 / 2 运维级。默认 0。
	SecurityLevel int `json:"security_level"`
	// SDKURL 为该服务 SDK 的 manifest.json 地址，可选。
	SDKURL string `json:"sdk_url"`
	// InteractsWith 声明与其他微服务的交互关系；隐式默认仅与主服务交互。
	InteractsWith []string `json:"interacts_with"`
	// RootMetadata 为微服务想要在根路径 (/) 返回的顶级元数据。
	RootMetadata map[string]interface{} `json:"root_metadata"`
}

func (pc *PresenceController) Bonjour(c *gin.Context) {
	var req PresenceRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	record := pc.registry.Register(name, req.TTLSeconds, req.Scope, req.SecurityLevel, req.InteractsWith, req.SDKURL, req.RootMetadata)

	var expiresAt any
	if !record.ExpiresAt.IsZero() {
		expiresAt = record.ExpiresAt
	}

	respondOK(c, "ça va très bien, merci", gin.H{
		"service":    record.Name,
		"first_seen": record.FirstSeen,
		"last_seen":  record.LastSeen,
		"expires_at": expiresAt,
	})
}

func (pc *PresenceController) AuthKey(c *gin.Context) {
	if !RequireAuthLevel(c, SecurityLevelOps) {
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	key, err := pc.registry.RegisterAuthKey(name)
	if err != nil {
		respondError(c, http.StatusNotFound, CodeInvalidRequest, err.Error())
		return
	}

	respondOK(c, "Internal key generated/updated", gin.H{
		"internal_key": key,
	})
}
