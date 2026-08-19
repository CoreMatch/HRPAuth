package controllers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type SiteAuthResult struct {
	TokenContext *services.OAuth2TokenContext
	User         *models.User
}

func resolveSiteBearerAuth(c *gin.Context, userScope string, adminScope string, requireExplicitTarget bool, providedUID string, providedEmail string) (*SiteAuthResult, bool) {
	accessToken := bearerTokenFromRequest(c)
	if accessToken == "" {
		respondError(c, http.StatusUnauthorized, CodeOAuthLoginRequired, "missing bearer token")
		return nil, false
	}

	tokenContext, err := services.NewOAuth2Service().ResolveAccessToken(accessToken)
	if err != nil {
		respondError(c, http.StatusUnauthorized, CodeOAuthInvalidGrant, "invalid access token")
		return nil, false
	}

	if tokenContext.IsService {
		if !hasScope(tokenContext.Scopes, adminScope) {
			respondError(c, http.StatusForbidden, CodeOAuthInsufficientScope, "insufficient scope")
			return nil, false
		}

		uid := firstNonEmpty(
			providedUID,
			c.PostForm("uid"),
			c.Query("uid"),
			tokenContext.TargetUID,
		)
		email := firstNonEmpty(
			providedEmail,
			c.PostForm("email"),
			c.Query("email"),
			tokenContext.TargetEmail,
		)

		if requireExplicitTarget && uid == "" && email == "" {
			respondError(c, http.StatusBadRequest, CodeManageTargetRequired, "target uid or email is required")
			return nil, false
		}

		user, ok := loadTargetUser(uid, email)
		if !ok {
			respondError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return nil, false
		}

		return &SiteAuthResult{
			TokenContext: tokenContext,
			User:         user,
		}, true
	}

	if !hasScope(tokenContext.Scopes, userScope) {
		respondError(c, http.StatusForbidden, CodeOAuthInsufficientScope, "insufficient scope")
		return nil, false
	}
	if tokenContext.User == nil {
		respondError(c, http.StatusUnauthorized, CodeOAuthInvalidGrant, "invalid user token")
		return nil, false
	}

	return &SiteAuthResult{
		TokenContext: tokenContext,
		User:         tokenContext.User,
	}, true
}

func bearerTokenFromRequest(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(authHeader) < 7 || !strings.EqualFold(authHeader[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(authHeader[7:])
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func loadTargetUser(uid string, email string) (*models.User, bool) {
	query := database.DB.Model(&models.User{})
	if uid != "" {
		query = query.Where("uid = ?", uid)
	}
	if email != "" {
		query = query.Where("email = ?", email)
	}

	var user models.User
	if err := query.First(&user).Error; err != nil {
		return nil, false
	}
	return &user, true
}
