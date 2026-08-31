package controllers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// PresenceScope 是微服务声明的作用区域。
// Name 为作用区域名；FrontendAreas 列出该区域覆盖的前端区域/页面，
// 非空即表示该微服务的作用区域包含前端。
type PresenceScope struct {
	Name          string   `json:"name"`
	FrontendAreas []string `json:"frontend_areas"`
}

// PresenceRecord 记录一个已注册微服务的存在状态。
// ExpiresAt 为零值表示该服务永不过期，一直保留到进程结束。
// SDKURL 指向一份 JS 文件，告知目标区域如何使用本服务；
// 内容由微服务与前端自行协商，主服务不参与，仅负责转发该文件。
// SecurityLevel 为该服务的鉴权级别（0 无须 / 1 用户级 / 2 运维级）；
// InteractsWith 声明与其他微服务的交互关系（隐式默认仅与主服务交互）。
type PresenceRecord struct {
	Name          string         `json:"name"`
	Scope         *PresenceScope `json:"scope,omitempty"`
	SDKURL        string         `json:"sdk_url,omitempty"`
	SecurityLevel int            `json:"security_level"`
	InteractsWith []string       `json:"interacts_with,omitempty"`
	FirstSeen     time.Time      `json:"first_seen"`
	LastSeen      time.Time      `json:"last_seen"`
	ExpiresAt     time.Time      `json:"expires_at"`
}

// ServiceSummary 是前端可见的微服务概要。
type ServiceSummary struct {
	Name      string `json:"name"`
	ScopeName string `json:"scope_name"`
	SDKURL    string `json:"sdk_url,omitempty"`
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
// scope 可选；传入 nil 表示该服务不声明作用区域。sdkURL 可选。
// securityLevel 钳制在 0~2。interactsWith 声明与其他服务的交互关系，可空。
func (r *PresenceRegistry) Register(name string, ttlSeconds int, scope *PresenceScope, sdkURL string, securityLevel int, interactsWith []string) PresenceRecord {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		record = PresenceRecord{Name: name, FirstSeen: now}
	}
	record.LastSeen = now
	if scope != nil {
		record.Scope = scope
	}
	if sdkURL != "" {
		record.SDKURL = sdkURL
	}
	if securityLevel < SecurityLevelNone {
		securityLevel = SecurityLevelNone
	}
	if securityLevel > SecurityLevelOps {
		securityLevel = SecurityLevelOps
	}
	record.SecurityLevel = securityLevel
	record.InteractsWith = interactsWith
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

// FrontendServices 返回与指定前端相关的微服务概要列表。
// frontendName 为前端在 presence 中注册的微服务名；按前端区域匹配：
// 前端自身声明的 scope.frontend_areas 作为其前端区域集合，
// 返回所有 scope.frontend_areas 与该集合有交集的微服务。
func (r *PresenceRegistry) FrontendServices(frontendName string) ([]ServiceSummary, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 已过期记录惰性清除。
	for name, record := range r.records {
		if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
			delete(r.records, name)
		}
	}

	frontend, exists := r.records[frontendName]
	if !exists {
		return nil, false
	}
	if frontend.Scope == nil || len(frontend.Scope.FrontendAreas) == 0 {
		// 该服务已注册，但未声明任何前端区域，不视为前端。
		return nil, false
	}

	frontendAreas := make(map[string]struct{}, len(frontend.Scope.FrontendAreas))
	for _, area := range frontend.Scope.FrontendAreas {
		frontendAreas[area] = struct{}{}
	}

	services := make([]ServiceSummary, 0, len(r.records))
	for name, record := range r.records {
		if name == frontendName || record.Scope == nil || len(record.Scope.FrontendAreas) == 0 {
			continue
		}
		if overlaps(frontendAreas, record.Scope.FrontendAreas) {
			services = append(services, ServiceSummary{
				Name:      record.Name,
				ScopeName: record.Scope.Name,
				SDKURL:    record.SDKURL,
			})
		}
	}
	return services, true
}

// overlaps 判断两个前端区域集合是否有交集。
func overlaps(areas map[string]struct{}, other []string) bool {
	for _, area := range other {
		if _, ok := areas[area]; ok {
			return true
		}
	}
	return false
}

type PresenceController struct {
	registry *PresenceRegistry
}

func NewPresenceController(registry *PresenceRegistry) *PresenceController {
	return &PresenceController{registry: registry}
}

type PresenceRequest struct {
	Name string `json:"name"`
	// TTLSeconds 为服务自定的存在时间（秒）。
	// 未指定（0）或指定为负数表示永不过期，保留到主服务进程结束。
	TTLSeconds int `json:"ttl_seconds"`
	// Scope 为服务声明的作用区域，可选。
	Scope *PresenceScope `json:"scope"`
	// SDKURL 为指向 JS 文件的地址，用于告知目标区域如何使用本服务；
	// 内容由微服务与前端自行协商，主服务仅负责转发。可选。
	SDKURL string `json:"sdk_url"`
	// SecurityLevel 为该服务的鉴权级别：0 无须 / 1 用户级 / 2 运维级。默认 0。
	SecurityLevel int `json:"security_level"`
	// InteractsWith 声明与其他微服务的交互关系；隐式默认仅与主服务交互。
	InteractsWith []string `json:"interacts_with"`
}

func (pc *PresenceController) Bonjour(c *gin.Context) {
	var req PresenceRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	record := pc.registry.Register(name, req.TTLSeconds, req.Scope, strings.TrimSpace(req.SDKURL), req.SecurityLevel, req.InteractsWith)

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

// ListFrontendServices 供前端 SPA 拉取与自身相关的微服务列表。
// 公开接口，无需鉴权；但要求调用方（前端）已通过 /services/presence
// 注册自己，并通过 ?name= 携带自己的微服务名。后端按前端区域匹配：
// 只返回 scope.frontend_areas 与前端声明区域有交集的微服务。
func (pc *PresenceController) ListFrontendServices(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "query param \"name\" is required")
		return
	}

	services, ok := pc.registry.FrontendServices(name)
	if !ok {
		respondError(c, http.StatusBadRequest, CodeServiceNotRegistered, "frontend service not registered or not declared as frontend")
		return
	}

	respondOK(c, "services fetched", services)
}

// GetSDK 供前端拉取某服务的 JS 使用说明文件。
// 主服务不参与文件内容，仅作为 relay 转发微服务声明的 sdk_url；
// 要求该服务已注册且声明了 sdk_url，否则返回 404。
func (pc *PresenceController) GetSDK(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "service name is required")
		return
	}

	record, ok := pc.registry.Get(name)
	if !ok {
		respondError(c, http.StatusNotFound, CodeSDKNotFound, "service not registered")
		return
	}
	if strings.TrimSpace(record.SDKURL) == "" {
		respondError(c, http.StatusNotFound, CodeSDKNotFound, "service has no sdk_url declared")
		return
	}

	if !forwardTo(c, record.SDKURL) {
		respondError(c, http.StatusBadGateway, CodeRelayFailed, "failed to relay sdk file")
		return
	}
	c.Abort()
}
