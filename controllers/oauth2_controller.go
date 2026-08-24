package controllers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	appredis "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
)

type OAuth2Controller struct {
	authService   *services.AuthService
	oauth2Service *services.OAuth2Service
}

type AuthorizationDecisionRequest struct {
	Email               string `json:"email"`
	Password            string `json:"password"`
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

type TokenRequest struct {
	GrantType    string `form:"grant_type" json:"grant_type"`
	Code         string `form:"code" json:"code"`
	RedirectURI  string `form:"redirect_uri" json:"redirect_uri"`
	CodeVerifier string `form:"code_verifier" json:"code_verifier"`
	RefreshToken string `form:"refresh_token" json:"refresh_token"`
	ClientID     string `form:"client_id" json:"client_id"`
	ClientSecret string `form:"client_secret" json:"client_secret"`
	Scope        string `form:"scope" json:"scope"`
	TargetUID    string `form:"target_uid" json:"target_uid"`
	TargetEmail  string `form:"target_email" json:"target_email"`
}

type RevokeRequest struct {
	Token string `form:"token" json:"token"`
}

type LoginTicketRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginTicketPayload struct {
	UserID string `json:"user_id"`
}

func NewOAuth2Controller() *OAuth2Controller {
	return &OAuth2Controller{
		authService:   services.NewAuthService(),
		oauth2Service: services.NewOAuth2Service(),
	}
}

func (oc *OAuth2Controller) LoginTicket(c *gin.Context) {
	var req LoginTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}
	if !isValidEmail(req.Email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	user := oc.authService.VerifyCredentials(req.Email, req.Password, false)
	if user == nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidCredentials, "Email or password incorrect")
		return
	}

	var fullUser models.User
	if err := database.DB.Where("uuid = ?", user.UUID).First(&fullUser).Error; err != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidCredentials, "Email or password incorrect")
		return
	}

	if !fullUser.TwoFA || fullUser.TOTP == "" {
		accessToken, refreshToken, err := oc.oauth2Service.IssueFirstPartyUserTokens(fullUser.UUID)
		if err != nil {
			respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to issue OAuth2 token")
			return
		}
		oc.respondTokenPair(c, accessToken, refreshToken)
		return
	}

	ticket := utils.GenerateRandomToken(24)
	payload, _ := json.Marshal(loginTicketPayload{UserID: fullUser.UUID})
	ctx := context.Background()
	key := config.AppConfig.Redis.Prefix + "oauth2:login_ticket:" + ticket
	if err := appredis.Client.Set(ctx, key, string(payload), time.Duration(config.AppConfig.OAuth2.AuthorizationCodeTTL)*time.Second).Err(); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to create login ticket")
		return
	}

	respondOK(c, "Login ticket issued", gin.H{
		"totp_required": true,
		"login_ticket":  ticket,
		"expires_in":    config.AppConfig.OAuth2.AuthorizationCodeTTL,
	})
}

func (oc *OAuth2Controller) Authorize(c *gin.Context) {
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	scope := c.Query("scope")
	state := c.Query("state")
	codeChallenge := c.Query("code_challenge")
	codeChallengeMethod := c.DefaultQuery("code_challenge_method", "S256")
	responseType := c.DefaultQuery("response_type", "code")

	if responseType != "code" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "unsupported response_type")
		return
	}

	client, scopes, err := oc.oauth2Service.ValidateAuthorizationRequest(clientID, redirectURI, scope, codeChallenge, codeChallengeMethod)
	if err != nil {
		oc.respondOAuthError(c, err)
		return
	}

	respondOK(c, "OAuth2 authorization request accepted", gin.H{
		"client_id":              client.ClientID,
		"client_name":            client.Name,
		"redirect_uri":           redirectURI,
		"scope":                  strings.Join(scopes, " "),
		"state":                  state,
		"skip_consent":           true,
		"authorization_code_ttl": config.AppConfig.OAuth2.AuthorizationCodeTTL,
	})
}

func (oc *OAuth2Controller) AuthorizeDecision(c *gin.Context) {
	var req AuthorizationDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	client, scopes, err := oc.oauth2Service.ValidateAuthorizationRequest(req.ClientID, req.RedirectURI, req.Scope, req.CodeChallenge, req.CodeChallengeMethod)
	if err != nil {
		oc.respondOAuthError(c, err)
		return
	}

	user := oc.authService.VerifyCredentials(req.Email, req.Password, false)
	if user == nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidCredentials, "Email or password incorrect")
		return
	}

	authCode, err := oc.oauth2Service.CreateAuthorizationCode(client.ClientID, user.UUID, req.RedirectURI, scopes, req.CodeChallenge, req.CodeChallengeMethod)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to create authorization code")
		return
	}

	respondOK(c, "OAuth2 authorization granted", gin.H{
		"code":         authCode.Code,
		"state":        req.State,
		"redirect_uri": req.RedirectURI,
	})
}

func (oc *OAuth2Controller) Token(c *gin.Context) {
	var req TokenRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Invalid token request")
		return
	}

	clientID, clientSecret := clientCredentialsFromRequest(c, req.ClientID, req.ClientSecret)
	if clientID == "" {
		respondError(c, http.StatusUnauthorized, CodeOAuthInvalidClient, "client authentication required")
		return
	}

	client, err := oc.oauth2Service.AuthenticateClient(clientID, clientSecret)
	if err != nil {
		oc.respondOAuthError(c, err)
		return
	}

	switch req.GrantType {
	case "authorization_code":
		accessToken, refreshToken, err := oc.oauth2Service.ExchangeAuthorizationCode(client.ClientID, req.Code, req.RedirectURI, req.CodeVerifier)
		if err != nil {
			oc.respondOAuthError(c, err)
			return
		}
		oc.respondTokenPair(c, accessToken, refreshToken)
	case "refresh_token":
		accessToken, refreshToken, err := oc.oauth2Service.RefreshUserToken(client.ClientID, req.RefreshToken)
		if err != nil {
			oc.respondOAuthError(c, err)
			return
		}
		oc.respondTokenPair(c, accessToken, refreshToken)
	case "client_credentials":
		accessToken, err := oc.oauth2Service.IssueClientCredentialsToken(client, req.Scope, req.TargetUID, req.TargetEmail)
		if err != nil {
			oc.respondOAuthError(c, err)
			return
		}
		respondOK(c, "OAuth2 token issued", gin.H{
			"access_token": accessToken.AccessToken,
			"token_type":   "Bearer",
			"expires_in":   config.AppConfig.OAuth2.AccessTokenTTL,
			"scope":        req.Scope,
		})
	default:
		oc.respondOAuthError(c, services.ErrOAuthUnsupportedGrantType)
	}
}

func (oc *OAuth2Controller) Revoke(c *gin.Context) {
	var req RevokeRequest
	if err := c.ShouldBind(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "token is required")
		return
	}
	if err := oc.oauth2Service.RevokeAccessToken(req.Token); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to revoke token")
		return
	}
	respondOK(c, "OAuth2 token revoked", nil)
}

func (oc *OAuth2Controller) respondTokenPair(c *gin.Context, accessToken *models.OAuth2AccessToken, refreshToken *models.OAuth2RefreshToken) {
	scope := ""
	if accessToken != nil {
		scope = strings.Join(parseScopesOrNil(accessToken.Scopes), " ")
	}
	respondOK(c, "OAuth2 token issued", gin.H{
		"access_token":  accessToken.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    config.AppConfig.OAuth2.AccessTokenTTL,
		"refresh_token": refreshToken.RefreshToken,
		"scope":         scope,
	})
}

func (oc *OAuth2Controller) respondOAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrOAuthInvalidClient):
		respondError(c, http.StatusUnauthorized, CodeOAuthInvalidClient, "invalid client")
	case errors.Is(err, services.ErrOAuthInvalidGrant):
		respondError(c, http.StatusBadRequest, CodeOAuthInvalidGrant, "invalid grant")
	case errors.Is(err, services.ErrOAuthInvalidScope):
		respondError(c, http.StatusBadRequest, CodeOAuthInvalidScope, "invalid scope")
	case errors.Is(err, services.ErrOAuthUnauthorizedClient):
		respondError(c, http.StatusBadRequest, CodeOAuthUnauthorizedClient, "unauthorized client")
	case errors.Is(err, services.ErrOAuthInvalidRedirectURI):
		respondError(c, http.StatusBadRequest, CodeOAuthInvalidRedirectURI, "invalid redirect_uri")
	case errors.Is(err, services.ErrOAuthUnsupportedGrantType):
		respondError(c, http.StatusBadRequest, CodeOAuthUnsupportedGrantType, "unsupported grant_type")
	case errors.Is(err, services.ErrOAuthInvalidCodeChallenge):
		respondError(c, http.StatusBadRequest, CodeOAuthInvalidCodeChallenge, "invalid code challenge")
	case errors.Is(err, services.ErrOAuthInsufficientScope):
		respondError(c, http.StatusForbidden, CodeOAuthInsufficientScope, "insufficient scope")
	case errors.Is(err, services.ErrOAuthAccessDenied):
		respondError(c, http.StatusForbidden, CodeOAuthAccessDenied, "access denied")
	default:
		respondError(c, http.StatusInternalServerError, CodeInternalError, "oauth2 error")
	}
}

func clientCredentialsFromRequest(c *gin.Context, bodyClientID, bodyClientSecret string) (string, string) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "basic ") {
		encoded := strings.TrimSpace(authHeader[len("Basic "):])
		if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			parts := strings.SplitN(string(raw), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1]
			}
		}
	}
	return bodyClientID, bodyClientSecret
}

func parseScopesOrNil(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil
	}
	return scopes
}
