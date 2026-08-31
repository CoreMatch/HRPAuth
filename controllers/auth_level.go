package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

// 鉴权级别：0 无须 / 1 用户级 / 2 运维级。
const (
	SecurityLevelNone = 0
	SecurityLevelUser = 1
	SecurityLevelOps  = 2
)

// maxSecurity 返回一组级别中的最大值。
func maxSecurity(levels ...int) int {
	max := SecurityLevelNone
	for _, l := range levels {
		if l > max {
			max = l
		}
	}
	return max
}

// resolveAuthLevel 解析请求方凭据级别：
//   - 无凭据 → 0
//   - OAuth2 用户 token → 1
//   - 管理 token M-T 或 OAuth2 服务 token（client_credentials / .as-service）→ 2
func resolveAuthLevel(c *gin.Context) int {
	accessToken := bearerTokenFromRequest(c)
	if accessToken == "" {
		return SecurityLevelNone
	}
	if config.AppConfig.Manage.Token != "" && accessToken == config.AppConfig.Manage.Token {
		return SecurityLevelOps
	}
	tokenContext, err := services.NewOAuth2Service().ResolveAccessToken(accessToken)
	if err != nil {
		return SecurityLevelNone
	}
	if tokenContext.IsService {
		return SecurityLevelOps
	}
	return SecurityLevelUser
}

// requireAuthLevel 校验请求方级别是否满足 required；不满足时写出错误响应并返回 false。
func requireAuthLevel(c *gin.Context, required int) bool {
	if required <= SecurityLevelNone {
		return true
	}
	level := resolveAuthLevel(c)
	if level >= required {
		return true
	}
	if level == SecurityLevelNone {
		respondError(c, http.StatusUnauthorized, CodeOAuthLoginRequired, "authentication required")
	} else {
		respondError(c, http.StatusForbidden, CodeInsufficientAuthLevel, "insufficient auth level")
	}
	return false
}
