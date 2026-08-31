package controllers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const orchestrationTimeout = 10 * time.Second

var httpClient = &http.Client{Timeout: orchestrationTimeout}

// responseCapture 捕获主服务响应的状态码与 body，供后置分发使用。
type responseCapture struct {
	gin.ResponseWriter
	status int
	body   bytes.Buffer
}

func (rc *responseCapture) WriteHeader(status int) {
	rc.status = status
	rc.ResponseWriter.WriteHeader(status)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if rc.status == 0 {
		rc.status = http.StatusOK
	}
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}

// OrchestrationMiddleware 是微服务编排层，挂在主服务路由之前：
//   - 前置路由：请求命中声明了 pre 的路由规则时，先按参与服务的 security_level
//     动态取 max 校验请求方鉴权级别；通过后将该请求扇出转发到对应微服务
//     （按规则顺序）；任一转发成功即以该微服务响应短路返回前端；
//     全部失败则 fail-open，放行到主服务正常处理。
//   - 后置路由：主服务响应成功（2xx）后，将请求上下文与响应内容异步投递
//     给声明了 post 的微服务；失败/非 2xx 不触发。
//   - /services/* 系统端点不参与编排。
func OrchestrationMiddleware(routes *RouteRegistry, presence *PresenceRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 系统端点不参与编排。
		if strings.HasPrefix(path, "/services/") {
			c.Next()
			return
		}

		// ---- 前置路由 ----
		preRules := routes.MatchPre(path)
		if len(preRules) > 0 {
			// 多服务交互按最严格级别：取所有参与前置转发服务的 security_level 最大值。
			if !requireAuthLevel(c, maxSecurityForRules(presence, preRules)) {
				c.Abort()
				return
			}
			for _, rule := range preRules {
				if forwardRequest(c, rule.PreURL) {
					c.Abort()
					return
				}
			}
			// 全部前置失败或无前置：fail-open，继续主服务。
		}

		// 捕获主服务响应。
		capture := &responseCapture{ResponseWriter: c.Writer}
		c.Writer = capture

		c.Next()

		// ---- 后置路由：仅主服务成功（2xx）触发，异步投递 ----
		if capture.status >= 200 && capture.status < 300 {
			postRules := routes.MatchPost(path)
			if len(postRules) > 0 {
				payload := gin.H{
					"path":       path,
					"method":     c.Request.Method,
					"request_id": requestIDFrom(c),
					"status":     capture.status,
					"response":   capture.body.String(),
				}
				for _, rule := range postRules {
					go dispatchPost(rule.PostURL, payload)
				}
			}
		}
	}
}

// forwardRequest 将当前请求转发到目标微服务；成功时把微服务响应写回前端并返回 true。
// 目标地址 = 规则 PreURL + 原请求路径，透传查询参数与关键请求头。
func forwardRequest(c *gin.Context, targetBase string) bool {
	target := strings.TrimRight(targetBase, "/") + c.Request.URL.Path
	return forwardTo(c, target)
}

// maxSecurityForRules 取一组路由规则中涉及服务的最严格鉴权级别。
func maxSecurityForRules(presence *PresenceRegistry, rules []RouteRule) int {
	levels := make([]int, 0, len(rules))
	for _, rule := range rules {
		levels = append(levels, serviceSecurityLevel(presence, rule.Service))
	}
	return maxSecurity(levels...)
}

// forwardTo 将当前请求转发到完整目标地址；成功时把响应写回前端并返回 true。
// 透传查询参数与关键请求头，供编排层前置路由与 relay 复用。
func forwardTo(c *gin.Context, target string) bool {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}

	req, err := http.NewRequest(c.Request.Method, target, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.URL.RawQuery = c.Request.URL.RawQuery
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("X-Request-Id", requestIDFrom(c))

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Data(resp.StatusCode, contentType, respBody)
	return true
}

// dispatchPost 异步把后置载荷投递给微服务，失败仅记录级别忽略（旁路语义）。
func dispatchPost(postURL string, payload gin.H) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, postURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
