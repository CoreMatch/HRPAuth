package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/registry"
)

type HookRequestPayload struct {
	Request HookRequestContext `json:"request"`
	Auth    HookAuthContext    `json:"auth"`
	Policy  HookPolicyContext  `json:"policy"`
}

type HookRequestContext struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Method string `json:"method"`
}

type HookAuthContext struct {
	Email                string   `json:"email,omitempty"`
	Password             string   `json:"password,omitempty"`
	PasswordEncoding     string   `json:"password_encoding,omitempty"`
	RiskTags             []string `json:"risk_tags,omitempty"`
	UserUUID             string   `json:"user_uuid,omitempty"`
	AvailableFactors     []string `json:"available_factors,omitempty"`
	RequiredSecondFactor string   `json:"required_second_factor,omitempty"`
}

type HookPolicyContext struct {
	PatchAllowlist []string `json:"patch_allowlist,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
}

type HookResponse struct {
	Result string                 `json:"result"`
	Patch  map[string]interface{} `json:"patch,omitempty"`
}

type HookExecutor struct {
	registry *HookRegistry
	presence *registry.PresenceRegistry
}

func NewHookExecutor(registry *HookRegistry, presence *registry.PresenceRegistry) *HookExecutor {
	return &HookExecutor{registry: registry, presence: presence}
}

func (e *HookExecutor) Run(c *gin.Context, phase string, payload *HookRequestPayload) bool {
	if e == nil || e.registry == nil || payload == nil {
		return true
	}

	path := c.Request.URL.Path
	rules := e.registry.Match(path, phase)
	for _, rule := range rules {
		if !e.runRule(c, rule, payload) {
			return false
		}
	}
	return true
}

func (e *HookExecutor) runRule(c *gin.Context, rule HookRule, payload *HookRequestPayload) bool {
	resp, err := e.callHook(c, rule, payload)
	if err != nil {
		log.Printf("[HA-HOOK] request_id=%s phase=%s service=%s url=%s result=call_failed err=%v",
			requestIDFrom(c), rule.Phase, rule.Service, rule.URL, err)
		if rule.FailPolicy == HookFailPolicyFailClosed {
			respondError(c, http.StatusBadGateway, CodeHookFailed, "hook execution failed")
			return false
		}
		return true
	}

	switch strings.ToLower(strings.TrimSpace(resp.Result)) {
	case "", "ok", "observe":
		return true
	case "deny", "veto":
		respondError(c, http.StatusForbidden, CodeHookDenied, "request denied by hook policy")
		return false
	case "patch":
		if rule.Capability != HookCapabilityPatch {
			if rule.FailPolicy == HookFailPolicyFailClosed {
				respondError(c, http.StatusBadGateway, CodeHookFailed, "hook execution failed")
				return false
			}
			return true
		}
		if err := applyHookPatch(payload, rule.PatchAllowlist, resp.Patch); err != nil {
			log.Printf("[HA-HOOK] request_id=%s phase=%s service=%s url=%s result=patch_failed err=%v",
				requestIDFrom(c), rule.Phase, rule.Service, rule.URL, err)
			if rule.FailPolicy == HookFailPolicyFailClosed {
				respondError(c, http.StatusBadGateway, CodeHookFailed, "hook execution failed")
				return false
			}
		}
		return true
	default:
		if rule.FailPolicy == HookFailPolicyFailClosed {
			respondError(c, http.StatusBadGateway, CodeHookFailed, "hook execution failed")
			return false
		}
		return true
	}
}

func (e *HookExecutor) callHook(c *gin.Context, rule HookRule, payload *HookRequestPayload) (*HookResponse, error) {
	requestPayload := *payload
	requestPayload.Policy = HookPolicyContext{
		PatchAllowlist: append([]string(nil), rule.PatchAllowlist...),
		Capabilities:   []string{rule.Capability},
	}

	body, err := json.Marshal(&requestPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, rule.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestIDFrom(c))
	if record, ok := e.presence.Get(rule.Service); ok && strings.TrimSpace(record.InternalKey) != "" {
		req.Header.Set("X-Internal-Key", record.InternalKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hook returned non-2xx status")
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return &HookResponse{Result: "ok"}, nil
	}

	var hookResp HookResponse
	if err := json.Unmarshal(raw, &hookResp); err != nil {
		return nil, err
	}
	return &hookResp, nil
}

func applyHookPatch(payload *HookRequestPayload, allowlist []string, patch map[string]interface{}) error {
	if payload == nil || len(patch) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(allowlist))
	for _, field := range allowlist {
		allowed[field] = struct{}{}
	}

	for key, value := range patch {
		if _, ok := allowed[key]; !ok {
			continue
		}

		switch key {
		case "auth.password":
			raw, ok := value.(string)
			if !ok {
				return errors.New("auth.password must be string")
			}
			payload.Auth.Password = raw
		case "auth.password_encoding":
			raw, ok := value.(string)
			if !ok {
				return errors.New("auth.password_encoding must be string")
			}
			payload.Auth.PasswordEncoding = strings.TrimSpace(raw)
		case "auth.required_second_factor":
			raw, ok := value.(string)
			if !ok {
				return errors.New("auth.required_second_factor must be string")
			}
			payload.Auth.RequiredSecondFactor = strings.TrimSpace(raw)
		case "auth.risk_tags":
			tags, err := parseHookStringSlice(value)
			if err != nil {
				return err
			}
			payload.Auth.RiskTags = tags
		}
	}

	return nil
}

func parseHookStringSlice(value interface{}) ([]string, error) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, errors.New("auth.risk_tags must be array")
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		raw, ok := item.(string)
		if !ok {
			return nil, errors.New("auth.risk_tags must contain only strings")
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		out = append(out, raw)
	}
	return out, nil
}
