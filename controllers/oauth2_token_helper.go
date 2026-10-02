package controllers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

func issueAndRespondFirstPartyUserTokens(c *gin.Context, userUUID string, message string, extras gin.H) bool {
	accessToken, refreshToken, err := services.NewOAuth2Service().IssueFirstPartyUserTokens(userUUID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to issue OAuth2 token")
		return false
	}

	scope := ""
	if accessToken != nil {
		scope = strings.Join(parseScopesOrNil(accessToken.Scopes), " ")
	}

	payload := gin.H{
		"access_token":  accessToken.AccessToken,
		"refresh_token": refreshToken.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    config.AppConfig.OAuth2.AccessTokenTTL,
		"scope":         scope,
	}
	for key, value := range extras {
		payload[key] = value
	}

	respondOK(c, message, payload)
	return true
}
