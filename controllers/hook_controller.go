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
	"github.com/lnb/HRPAuth-Backend-Go/registry"
)

const (
	hookTTL = 30 * 24 * time.Hour

	HookPhaseAuthInput      = "auth.input"
	HookPhaseAuthRisk       = "auth.risk"
	HookPhaseAuthPostVerify = "auth.post_verify"

	HookCapabilityObserve = "observe"
	HookCapabilityPatch   = "patch"
	HookCapabilityVeto    = "veto"

	HookFailPolicyIgnore     = "ignore"
	HookFailPolicyFailOpen   = "fail_open"
	HookFailPolicyFailClosed = "fail_closed"
)

func hookKey(prefix, service string) string {
	return prefix + "hook:" + service
}

type HookRule struct {
	Service        string   `json:"service"`
	Paths          []string `json:"paths"`
	Phase          string   `json:"phase"`
	Capability     string   `json:"capability"`
	Priority       int      `json:"priority"`
	FailPolicy     string   `json:"fail_policy"`
	PatchAllowlist []string `json:"patch_allowlist"`
	URL            string   `json:"url"`
}

func (r *HookRule) MatchPath(path string) bool {
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
			continue
		}
		if path == p {
			return true
		}
	}
	return false
}

type HookRegistry struct {
	mu    sync.RWMutex
	rules map[string][]HookRule
}

func NewHookRegistry() *HookRegistry {
	return &HookRegistry{rules: make(map[string][]HookRule)}
}

func (r *HookRegistry) Upsert(service string, rules []HookRule) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rules[service] = rules
	r.persist(service, rules)
}

func (r *HookRegistry) persist(service string, rules []HookRule) {
	if redisClient.Client == nil {
		return
	}

	payload, err := json.Marshal(rules)
	if err != nil {
		return
	}

	ctx := context.Background()
	prefix := config.AppConfig.Redis.Prefix
	_ = redisClient.Client.Set(ctx, hookKey(prefix, service), payload, hookTTL).Err()
}

func (r *HookRegistry) Load(ctx context.Context) error {
	if redisClient.Client == nil {
		return nil
	}

	prefix := config.AppConfig.Redis.Prefix
	iter := redisClient.Client.Scan(ctx, 0, prefix+"hook:*", 0).Iterator()

	r.mu.Lock()
	defer r.mu.Unlock()

	for iter.Next(ctx) {
		key := iter.Val()
		service := strings.TrimPrefix(key, prefix+"hook:")
		if service == "" || strings.Contains(service, ":") {
			continue
		}

		raw, err := redisClient.Client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var rules []HookRule
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			continue
		}
		r.rules[service] = rules
	}

	return iter.Err()
}

func (r *HookRegistry) Match(path string, phase string) []HookRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matched := make([]HookRule, 0)
	for _, rules := range r.rules {
		for _, rule := range rules {
			if rule.Phase != phase || !rule.MatchPath(path) || strings.TrimSpace(rule.URL) == "" {
				continue
			}
			matched = append(matched, rule)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Priority == matched[j].Priority {
			return matched[i].Service < matched[j].Service
		}
		return matched[i].Priority < matched[j].Priority
	})
	return matched
}

type HookRequest struct {
	Name  string     `json:"name"`
	Hooks []HookRule `json:"hooks"`
}

type HookController struct {
	registry *HookRegistry
	presence *registry.PresenceRegistry
}

func NewHookController(registry *HookRegistry, presence *registry.PresenceRegistry) *HookController {
	return &HookController{registry: registry, presence: presence}
}

func (hc *HookController) Register(c *gin.Context) {
	var req HookRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "\"name\" is required")
		return
	}

	name := strings.TrimSpace(req.Name)
	if _, ok := hc.presence.Get(name); !ok {
		respondError(c, http.StatusBadRequest, CodeServiceNotRegistered, "service not registered, send /services/presence first")
		return
	}

	rules := make([]HookRule, 0, len(req.Hooks))
	for _, rule := range req.Hooks {
		normalized, ok := normalizeHookRule(rule, name)
		if !ok {
			respondError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid hook rule")
			return
		}
		rules = append(rules, normalized)
	}

	hc.registry.Upsert(name, rules)
	respondOK(c, "hook rules registered", gin.H{
		"service": name,
		"count":   len(rules),
	})
}

func normalizeHookRule(rule HookRule, service string) (HookRule, bool) {
	rule.Service = service
	rule.Phase = strings.TrimSpace(rule.Phase)
	rule.Capability = strings.TrimSpace(rule.Capability)
	rule.FailPolicy = strings.TrimSpace(rule.FailPolicy)
	rule.URL = strings.TrimSpace(rule.URL)

	if rule.URL == "" || !validHookPhase(rule.Phase) || !validHookCapability(rule.Capability) {
		return HookRule{}, false
	}
	if rule.FailPolicy == "" {
		rule.FailPolicy = HookFailPolicyIgnore
	}
	if !validHookFailPolicy(rule.FailPolicy) {
		return HookRule{}, false
	}

	cleanPaths := make([]string, 0, len(rule.Paths))
	for _, path := range rule.Paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		cleanPaths = append(cleanPaths, path)
	}
	if len(cleanPaths) == 0 {
		return HookRule{}, false
	}
	rule.Paths = cleanPaths

	allowlist := make([]string, 0, len(rule.PatchAllowlist))
	seen := make(map[string]struct{})
	for _, field := range rule.PatchAllowlist {
		field = strings.TrimSpace(field)
		if field == "" || !validHookPatchField(field) {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		allowlist = append(allowlist, field)
	}
	rule.PatchAllowlist = allowlist

	if rule.Capability == HookCapabilityPatch && len(rule.PatchAllowlist) == 0 {
		return HookRule{}, false
	}

	return rule, true
}

func validHookPhase(phase string) bool {
	switch phase {
	case HookPhaseAuthInput, HookPhaseAuthRisk, HookPhaseAuthPostVerify:
		return true
	default:
		return false
	}
}

func validHookCapability(capability string) bool {
	switch capability {
	case HookCapabilityObserve, HookCapabilityPatch, HookCapabilityVeto:
		return true
	default:
		return false
	}
}

func validHookFailPolicy(policy string) bool {
	switch policy {
	case HookFailPolicyIgnore, HookFailPolicyFailOpen, HookFailPolicyFailClosed:
		return true
	default:
		return false
	}
}

func validHookPatchField(field string) bool {
	switch field {
	case "auth.password", "auth.password_encoding", "auth.risk_tags", "auth.required_second_factor":
		return true
	default:
		return false
	}
}
