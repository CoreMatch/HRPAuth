package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	redisClient "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/redis/go-redis/v9"
)

// presenceBaseTTL 是 Redis 中 presence 记录的基础存活时间。
// 每次心跳注册都会刷新 TTL；超过该时间未心跳的服务视为已下线，
// 由惰性清理机制 + Redis TTL 兜底共同保证僵尸数据被回收。
const presenceBaseTTL = 24 * time.Hour

// presencePersistentTTL 是声明"永不过期"服务的兜底 TTL，
// 防止其注册数据在 Redis 中永久驻留成为僵尸。
const presencePersistentTTL = 7 * 24 * time.Hour

func presenceKey(prefix, name string) string {
	return prefix + "presence:" + name
}

func presenceIndexKey(prefix string) string {
	return prefix + "presence:_index"
}

// PresenceScope 是微服务声明的作用区域。
// Name 为作用区域名；FrontendAreas 列出该服务覆盖的前端挂载区域，
// 非空即表示该微服务对前端可见。这里的前端区域是单个 WebUI 宿主内部的挂载点，
// 不是多个独立 WebUI 或多个独立 SDK 宿主。
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
	Name          string   `json:"name"`
	ScopeName     string   `json:"scope_name"`
	FrontendAreas []string `json:"frontend_areas,omitempty"`
	SDKURL        string   `json:"sdk_url,omitempty"`
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
// 同步将记录持久化到 Redis，使主服务重启后能恢复注册状态。
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

	r.persist(name, record)
	return record
}

// persist 将单条 presence 记录写入 Redis。
// 永不过期的记录使用较长的兜底 TTL，防止成为永久僵尸数据。
// 调用方需持有 r.mu 写锁。
func (r *PresenceRegistry) persist(name string, record PresenceRecord) {
	if redisClient.Client == nil {
		return
	}
	prefix := config.AppConfig.Redis.Prefix
	ctx := context.Background()

	payload, err := json.Marshal(record)
	if err != nil {
		return
	}

	ttl := presenceBaseTTL
	if record.ExpiresAt.IsZero() {
		ttl = presencePersistentTTL
	} else if d := time.Until(record.ExpiresAt); d > 0 && d < ttl {
		ttl = d
	}

	key := presenceKey(prefix, name)
	pipe := redisClient.Client.TxPipeline()
	pipe.Set(ctx, key, payload, ttl)
	pipe.SAdd(ctx, presenceIndexKey(prefix), name)
	// 索引本身也设置 TTL，每次心跳刷新。
	pipe.Expire(ctx, presenceIndexKey(prefix), 30*24*time.Hour)
	_, _ = pipe.Exec(ctx)
}

// Load 从 Redis 恢复所有 presence 注册到内存。
// 由主服务启动时调用，确保重启后微服务状态延续。
// 加载过程中跳过已过期的记录，避免恢复僵尸数据。
func (r *PresenceRegistry) Load(ctx context.Context) error {
	if redisClient.Client == nil {
		return nil
	}
	prefix := config.AppConfig.Redis.Prefix

	names, err := redisClient.Client.SMembers(ctx, presenceIndexKey(prefix)).Result()
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, name := range names {
		raw, err := redisClient.Client.Get(ctx, presenceKey(prefix, name)).Result()
		if err == redis.Nil {
			// 主键已过期：从索引中清理。
			_ = redisClient.Client.SRem(ctx, presenceIndexKey(prefix), name).Err()
			continue
		}
		if err != nil {
			continue
		}
		var record PresenceRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			continue
		}
		// 跳过已过期记录（按业务 TTL）。
		if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
			_ = redisClient.Client.Del(ctx, presenceKey(prefix, name)).Err()
			_ = redisClient.Client.SRem(ctx, presenceIndexKey(prefix), name).Err()
			continue
		}
		r.records[name] = record
	}
	return nil
}

// Get 返回指定服务的存在记录。服务不存在或已过期时返回 false，
// 已过期的记录会被惰性清除（同时清理 Redis 持久化数据）。
func (r *PresenceRegistry) Get(name string) (PresenceRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.records[name]
	if !exists {
		return PresenceRecord{}, false
	}
	if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
		delete(r.records, name)
		r.evict(name)
		return PresenceRecord{}, false
	}
	return record, true
}

// evict 从 Redis 移除某条 presence 持久化数据。
// 调用方需持有 r.mu 写锁。
func (r *PresenceRegistry) evict(name string) {
	if redisClient.Client == nil {
		return
	}
	prefix := config.AppConfig.Redis.Prefix
	ctx := context.Background()
	_ = redisClient.Client.Del(ctx, presenceKey(prefix, name)).Err()
	_ = redisClient.Client.SRem(ctx, presenceIndexKey(prefix), name).Err()
}

// FrontendSDKs 返回当前所有对前端可见且声明了 sdk_url 的微服务概要列表。
// 前端实例本身不需要再注册 presence；它只需通过该列表了解当前有哪些前端 SDK
// 可供加载，并结合 frontend_areas 决定挂载位置。
func (r *PresenceRegistry) FrontendSDKs() []ServiceSummary {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 已过期记录惰性清除。
	expired := make([]string, 0)
	for name, record := range r.records {
		if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
			expired = append(expired, name)
		}
	}
	for _, name := range expired {
		delete(r.records, name)
		r.evict(name)
	}

	services := make([]ServiceSummary, 0, len(r.records))
	for _, record := range r.records {
		if record.Scope == nil || len(record.Scope.FrontendAreas) == 0 || strings.TrimSpace(record.SDKURL) == "" {
			continue
		}
		services = append(services, ServiceSummary{
			Name:          record.Name,
			ScopeName:     record.Scope.Name,
			FrontendAreas: append([]string(nil), record.Scope.FrontendAreas...),
			SDKURL:        record.SDKURL,
		})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services
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

// ListFrontendServices 供前端实例拉取当前存在的前端 SDK 列表。
// 公开接口，无需鉴权，也不要求前端先通过 /services/presence 注册自己。
// 后端把前端视为一个特殊的微服务 SDK 消费端：只返回声明了 frontend_areas
// 且带有 sdk_url 的微服务，并把 frontend_areas 一并返回给前端自行决定挂载。
func (pc *PresenceController) ListFrontendServices(c *gin.Context) {
	services := pc.registry.FrontendSDKs()

	// 将微服务的内网 sdk_url 转换为主服务公网的 relay 路径，避免向浏览器泄露内部地址。
	base := strings.TrimRight(config.AppConfig.Callback.URL, "/")
	if base != "" {
		for i, svc := range services {
			if svc.SDKURL != "" {
				services[i].SDKURL = base + "/services/sdk/" + svc.Name
			}
		}
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
