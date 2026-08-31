package controllers

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// RelayRule 是微服务声明的转发规则：
// Dest 为主服务对外路径（前缀匹配），Source 为微服务地址。
// 请求打到 Dest（及其子路径）时，主服务作为 relay 转发到 Source + 剩余路径。
type RelayRule struct {
	Service string `json:"service"`
	Dest    string `json:"dest"`
	Source  string `json:"source"`
}

// RelayRegistry 维护各微服务的 relay 规则，进程内存储。
// 以 dest 为唯一键；以 service 为单位整体替换（后注册覆盖先注册）。
type RelayRegistry struct {
	mu    sync.RWMutex
	rules map[string]RelayRule
}

func NewRelayRegistry() *RelayRegistry {
	return &RelayRegistry{rules: make(map[string]RelayRule)}
}

// Upsert 以 service 为单位整体替换该服务的 relay 规则。
func (r *RelayRegistry) Upsert(service string, rules []RelayRule) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 先移除该服务已有规则。
	for dest, rule := range r.rules {
		if rule.Service == service {
			delete(r.rules, dest)
		}
	}
	for _, rule := range rules {
		if rule.Dest == "" || rule.Source == "" {
			continue
		}
		rule.Service = service
		r.rules[rule.Dest] = rule
	}
}

// Delete 删除指定服务下的指定 dest 规则，返回是否命中。
func (r *RelayRegistry) Delete(service, dest string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	rule, exists := r.rules[dest]
	if !exists || rule.Service != service {
		return false
	}
	delete(r.rules, dest)
	return true
}

// List 返回 relay 规则；service 非空时仅返回该服务的。
func (r *RelayRegistry) List(service string) []RelayRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]RelayRule, 0, len(r.rules))
	for _, rule := range r.rules {
		if service != "" && rule.Service != service {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// Match 返回 path 命中的最长前缀 relay 规则及剩余路径（含前导 /）。
// 匹配边界：path == dest 或 path 以 dest + "/" 开头。
func (r *RelayRegistry) Match(path string) (RelayRule, string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best RelayRule
	var bestRest string
	bestLen := -1
	for _, rule := range r.rules {
		dest := rule.Dest
		var rest string
		if path == dest {
			rest = ""
		} else if strings.HasPrefix(path, dest+"/") {
			rest = strings.TrimPrefix(path, dest)
		} else {
			continue
		}
		if len(dest) > bestLen {
			best = rule
			bestRest = rest
			bestLen = len(dest)
		}
	}
	if bestLen < 0 {
		return RelayRule{}, "", false
	}
	return best, bestRest, true
}

type RelayController struct {
	registry *RelayRegistry
	presence *PresenceRegistry
}

func NewRelayController(registry *RelayRegistry, presence *PresenceRegistry) *RelayController {
	return &RelayController{registry: registry, presence: presence}
}

type RelayRequest struct {
	Name   string      `json:"name"`
	Relays []RelayRule `json:"relays"`
}

type RelayDeleteRequest struct {
	Name string `json:"name"`
	Dest string `json:"dest"`
}

// Register 供微服务注册/更新自己的 relay 规则。
// 要求该服务已通过 /services/presence 注册，避免匿名冒名。
func (rc *RelayController) Register(c *gin.Context) {
	var req RelayRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	if _, ok := rc.presence.Get(name); !ok {
		respondError(c, http.StatusBadRequest, CodeServiceNotRegistered, "service not registered, send /services/presence first")
		return
	}

	for i := range req.Relays {
		req.Relays[i].Dest = normalizeDest(req.Relays[i].Dest)
	}

	rc.registry.Upsert(name, req.Relays)
	respondOK(c, "relay rules registered", gin.H{
		"service": name,
		"count":   len(req.Relays),
	})
}

// Delete 供微服务删除自己的某条 relay 规则。
func (rc *RelayController) Delete(c *gin.Context) {
	var req RelayDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Dest) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" and \"dest\" are required")
		return
	}

	if !rc.registry.Delete(strings.TrimSpace(req.Name), normalizeDest(req.Dest)) {
		respondError(c, http.StatusNotFound, CodeRelayNotFound, "relay rule not found")
		return
	}
	respondOK(c, "relay rule deleted", nil)
}

// List 查询当前 relay 规则；?name= 可按服务过滤。
func (rc *RelayController) List(c *gin.Context) {
	service := strings.TrimSpace(c.Query("name"))
	respondOK(c, "relay rules fetched", rc.registry.List(service))
}

// normalizeDest 规范化 relay 的对外路径：以 / 开头、无尾斜杠。
func normalizeDest(dest string) string {
	return "/" + strings.Trim(strings.TrimSpace(dest), "/")
}

// RelayMiddleware 是独立于编排层的 relay 转发处理器。
// 命中 dest 前缀时把请求转发到对应微服务并短路；转发失败返回 502，
// 不回退主服务（relay 路径归属微服务，主服务无对应处理）。
// 转发前校验请求方鉴权级别是否满足该服务的 security_level。
func RelayMiddleware(relays *RelayRegistry, presence *PresenceRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 主服务系统端点不做 relay。
		if strings.HasPrefix(c.Request.URL.Path, "/services/") {
			c.Next()
			return
		}

		rule, rest, ok := relays.Match(c.Request.URL.Path)
		if !ok {
			c.Next()
			return
		}

		if !requireAuthLevel(c, serviceSecurityLevel(presence, rule.Service)) {
			c.Abort()
			return
		}

		target := strings.TrimRight(rule.Source, "/") + rest
		if forwardTo(c, target) {
			c.Abort()
			return
		}
		respondError(c, http.StatusBadGateway, CodeRelayFailed, "relay forwarding failed")
		c.Abort()
	}
}

// serviceSecurityLevel 返回指定服务的鉴权级别；服务不存在或未声明时按 0（无须）处理。
func serviceSecurityLevel(presence *PresenceRegistry, service string) int {
	if record, ok := presence.Get(service); ok {
		return record.SecurityLevel
	}
	return SecurityLevelNone
}
