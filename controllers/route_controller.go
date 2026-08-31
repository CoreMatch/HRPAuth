package controllers

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// RouteRule 是微服务针对某个作用区域声明的路由规则。
// Service 由后端在注册时填充；Scope 为作用区域名；
// Paths 声明该区域覆盖的请求路径（支持精确路径与尾部 * 前缀通配）；
// PreURL 非空表示该服务对该区域做前置路由；PostURL 非空表示做后置路由。
type RouteRule struct {
	Service string   `json:"service"`
	Scope   string   `json:"scope"`
	Paths   []string `json:"paths"`
	PreURL  string   `json:"pre_url"`
	PostURL string   `json:"post_url"`
}

// RouteRegistry 维护各微服务注册的路由规则，进程内存储。
// 以 service 名为单位整体替换（后注册覆盖先注册）。
type RouteRegistry struct {
	mu    sync.RWMutex
	rules map[string][]RouteRule // key: service name
}

func NewRouteRegistry() *RouteRegistry {
	return &RouteRegistry{rules: make(map[string][]RouteRule)}
}

// Upsert 以 service 为单位整体替换其路由规则。
func (r *RouteRegistry) Upsert(service string, rules []RouteRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[service] = rules
}

// MatchPre 返回命中 path 且声明了前置转发（PreURL 非空）的路由规则。
func (r *RouteRegistry) MatchPre(path string) []RouteRule {
	return r.match(path, true)
}

// MatchPost 返回命中 path 且声明了后置投递（PostURL 非空）的路由规则。
func (r *RouteRegistry) MatchPost(path string) []RouteRule {
	return r.match(path, false)
}

func (r *RouteRegistry) match(path string, pre bool) []RouteRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []RouteRule
	for _, rules := range r.rules {
		for _, rule := range rules {
			if !rule.MatchPath(path) {
				continue
			}
			if pre && rule.PreURL != "" {
				matched = append(matched, rule)
			}
			if !pre && rule.PostURL != "" {
				matched = append(matched, rule)
			}
		}
	}
	return matched
}

// MatchPath 判断 path 是否命中规则中的路径声明。
// 精确路径直接比较；以 * 结尾的声明按前缀匹配。
func (r *RouteRule) MatchPath(path string) bool {
	for _, p := range r.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		} else if path == p {
			return true
		}
	}
	return false
}

type RouteRequest struct {
	Name  string      `json:"name"`
	Rules []RouteRule `json:"rules"`
}

type RouteController struct {
	registry *RouteRegistry
	presence *PresenceRegistry
}

func NewRouteController(registry *RouteRegistry, presence *PresenceRegistry) *RouteController {
	return &RouteController{registry: registry, presence: presence}
}

// Register 供微服务注册/更新自己的路由规则。
// 要求该服务已通过 /services/presence 注册，避免匿名服务冒名注册路由。
func (rc *RouteController) Register(c *gin.Context) {
	var req RouteRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	if _, ok := rc.presence.Get(name); !ok {
		respondError(c, http.StatusBadRequest, CodeServiceNotRegistered, "service not registered, send /services/presence first")
		return
	}

	for i := range req.Rules {
		req.Rules[i].Service = name
	}

	rc.registry.Upsert(name, req.Rules)
	respondOK(c, "route rules registered", gin.H{
		"service": name,
		"count":   len(req.Rules),
	})
}
