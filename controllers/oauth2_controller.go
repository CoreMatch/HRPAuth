package controllers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	appredis "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/registry"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
)

type OAuth2Controller struct {
	authService   *services.AuthService
	oauth2Service *services.OAuth2Service
	hookExecutor  *HookExecutor
	presence      *registry.PresenceRegistry
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

func NewOAuth2Controller(hookRegistry *HookRegistry, presence *registry.PresenceRegistry) *OAuth2Controller {
	return &OAuth2Controller{
		authService:   services.NewAuthService(),
		oauth2Service: services.NewOAuth2Service(),
		hookExecutor:  NewHookExecutor(hookRegistry, presence),
		presence:      presence,
	}
}

func (oc *OAuth2Controller) LoginTicket(c *gin.Context) {
	var req LoginTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	hookPayload := newAuthHookPayload(c, req.Email, req.Password)
	if !oc.hookExecutor.Run(c, HookPhaseAuthInput, hookPayload) {
		return
	}
	req.Password = hookPayload.Auth.Password
	if !isValidEmail(req.Email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}
	if !oc.hookExecutor.Run(c, HookPhaseAuthRisk, hookPayload) {
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

	hasTOTP := fullUser.TwoFA && fullUser.TOTP != ""
	webAuthnCount, err := services.NewWebAuthnService().CountCredentials(fullUser.UUID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to inspect WebAuthn credentials")
		return
	}
	hasWebAuthn := fullUser.WebAuthn2FAEnabled && webAuthnCount > 0
	hasEmail2FA := fullUser.Email2FAEnabled
	hasRecoveryKey := fullUser.RecoveryKeyEnabled && fullUser.RecoveryKey != ""
	hookPayload.Auth.UserUUID = fullUser.UUID
	hookPayload.Auth.AvailableFactors = availableSecondFactors(hasTOTP, hasWebAuthn, hasEmail2FA, hasRecoveryKey)

	if !oc.hookExecutor.Run(c, HookPhaseAuthPostVerify, hookPayload) {
		return
	}

	selectedFactor := strings.TrimSpace(hookPayload.Auth.RequiredSecondFactor)
	if selectedFactor != "" {
		if !containsString(hookPayload.Auth.AvailableFactors, selectedFactor) {
			respondError(c, http.StatusForbidden, CodeHookDenied, "request denied by hook policy")
			return
		}
		hasTOTP = selectedFactor == "totp"
		hasWebAuthn = selectedFactor == "webauthn"
		hasEmail2FA = selectedFactor == "email"
		hasRecoveryKey = selectedFactor == "recovery_key"
	}

	if !hasTOTP && !hasWebAuthn && !hasEmail2FA && selectedFactor == "" {
		issueAndRespondFirstPartyUserTokens(c, fullUser.UUID, "OAuth2 token issued", nil)
		return
	}

	ticket := utils.GenerateRandomToken(24)
	payload, _ := json.Marshal(LoginTicketPayload{UserID: fullUser.UUID})
	ctx := context.Background()
	key := config.AppConfig.Redis.Prefix + "oauth2:login_ticket:" + ticket
	if err := appredis.Client.Set(ctx, key, string(payload), time.Duration(config.AppConfig.OAuth2.AuthorizationCodeTTL)*time.Second).Err(); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to create login ticket")
		return
	}

	respondOK(c, "Login ticket issued", gin.H{
		"totp_required":         hasTOTP,
		"webauthn_required":     hasWebAuthn,
		"email_required":        hasEmail2FA,
		"recovery_key_required": hasRecoveryKey,
		"second_factors":        availableSecondFactors(hasTOTP, hasWebAuthn, hasEmail2FA, hasRecoveryKey),
		"login_ticket":          ticket,
		"expires_in":            config.AppConfig.OAuth2.AuthorizationCodeTTL,
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
	hookPayload := newAuthHookPayload(c, req.Email, req.Password)
	if !oc.hookExecutor.Run(c, HookPhaseAuthInput, hookPayload) {
		return
	}
	req.Password = hookPayload.Auth.Password

	client, scopes, err := oc.oauth2Service.ValidateAuthorizationRequest(req.ClientID, req.RedirectURI, req.Scope, req.CodeChallenge, req.CodeChallengeMethod)
	if err != nil {
		oc.respondOAuthError(c, err)
		return
	}
	if !oc.hookExecutor.Run(c, HookPhaseAuthRisk, hookPayload) {
		return
	}

	user := oc.authService.VerifyCredentials(req.Email, req.Password, false)
	if user == nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidCredentials, "Email or password incorrect")
		return
	}
	hookPayload.Auth.UserUUID = user.UUID
	if !oc.hookExecutor.Run(c, HookPhaseAuthPostVerify, hookPayload) {
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

// RegisterClientRequest carries RFC 7591 client metadata for self-service
// registration. Only client_name and scope are honored; all other metadata
// fields are ignored because the issued client attributes are fixed by the
// server (confidential, client_credentials, is_super=false).
type RegisterClientRequest struct {
	ClientName string `json:"client_name"`
	Scope      string `json:"scope"`
}

// RegisterClient implements RFC 7591 protected client registration. The
// initial access credential is the built-in super client, authenticated via
// client_secret_basic (RFC 6749 §2.3.1); user tokens and any other caller
// are rejected with 401. The requested client_name must already be
// registered via /services/presence, and the requested scopes must stay
// inside the service's oauth2.service_scope_allowlist entry (super and db.*
// scopes can never be granted). Re-registering an existing service returns
// its existing credentials unchanged — a get-or-create extension documented
// in the contract; RFC 7591 itself issues a fresh client per registration.
// Every call is audit-logged (caller, service, requested scopes, outcome).
func (oc *OAuth2Controller) RegisterClient(c *gin.Context) {
	clientID, clientSecret, ok := basicClientCredentials(c.GetHeader("Authorization"))
	if !ok {
		oc.auditClientRegistration(c, "", "", "", "denied: client_secret_basic required")
		oc.respondOAuthError(c, services.ErrOAuthInvalidClient)
		return
	}

	caller, err := oc.oauth2Service.AuthenticateClient(clientID, clientSecret)
	if err != nil || !caller.IsSuper || !caller.IsInternal || caller.ClientID != config.AppConfig.OAuth2.SuperClientID {
		oc.auditClientRegistration(c, clientID, "", "", "denied: caller is not the built-in super client")
		oc.respondOAuthError(c, services.ErrOAuthInvalidClient)
		return
	}

	var req RegisterClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		oc.auditClientRegistration(c, caller.ClientID, "", "", "denied: invalid request body")
		oc.respondOAuthError(c, services.ErrOAuthInvalidClientMetadata)
		return
	}
	serviceName := strings.TrimSpace(req.ClientName)
	if serviceName == "" {
		oc.auditClientRegistration(c, caller.ClientID, "", req.Scope, "denied: missing client_name")
		oc.respondOAuthError(c, services.ErrOAuthInvalidClientMetadata)
		return
	}

	if _, registered := oc.presence.Get(serviceName); !registered {
		oc.auditClientRegistration(c, caller.ClientID, serviceName, req.Scope, "denied: service not in presence registry")
		oc.respondOAuthError(c, services.ErrOAuthInvalidClientMetadata)
		return
	}

	scopes, err := oc.oauth2Service.ServiceRegistrationScopes(serviceName, req.Scope)
	if err != nil {
		oc.auditClientRegistration(c, caller.ClientID, serviceName, req.Scope, "denied: scope not allowed")
		oc.respondOAuthError(c, err)
		return
	}

	client, created, err := oc.oauth2Service.RegisterServiceClient(serviceName, scopes)
	if err != nil {
		if errors.Is(err, services.ErrOAuthRegistrationConflict) {
			// An existing row carries the super client secret (legacy
			// poisoned data). It must never be returned — refuse with an
			// actionable message instead of distributing the root
			// credential.
			oc.auditClientRegistration(c, caller.ClientID, serviceName, req.Scope, "denied: existing row carries the super client secret")
			respondError(c, http.StatusBadRequest, CodeOAuthInvalidClientMetadata,
				"client_name conflicts with a legacy client row carrying the super client secret; an operator must remove or re-secret that row before self-registration can proceed")
			return
		}
		oc.auditClientRegistration(c, caller.ClientID, serviceName, req.Scope, "error: "+err.Error())
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to register client")
		return
	}

	result := "existing client returned"
	if created {
		result = "client created"
	}
	oc.auditClientRegistration(c, caller.ClientID, serviceName, req.Scope, result)

	respondCreated(c, "OAuth2 client registered", gin.H{
		"client_id":                client.ClientID,
		"client_secret":            client.ClientSecret,
		"client_id_issued_at":      client.CreatedAt.Unix(),
		"client_secret_expires_at": 0, // 0 = the client secret never expires
		"scope":                    strings.Join(parseScopesOrNil(client.Scopes), " "),
	})
}

// auditClientRegistration writes the mandatory audit line for every
// POST /oauth/register call (caller, service name, requested scopes,
// outcome; the timestamp is contributed by the std logger prefix).
func (oc *OAuth2Controller) auditClientRegistration(c *gin.Context, caller, service, scope, result string) {
	log.Printf("[CLIENT-REG] request_id=%s caller=%q service=%q scopes=%q result=%s",
		requestIDFrom(c), caller, service, scope, result)
}

// basicClientCredentials extracts client_secret_basic credentials
// (RFC 6749 §2.3.1) from the Authorization header. Only the Basic scheme
// is accepted; Bearer tokens and request-body credentials are rejected.
func basicClientCredentials(authHeader string) (string, string, bool) {
	const prefix = "basic "
	if len(authHeader) < len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authHeader[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
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
	case errors.Is(err, services.ErrOAuthInvalidClientMetadata):
		respondError(c, http.StatusBadRequest, CodeOAuthInvalidClientMetadata, "invalid client metadata")
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

func availableSecondFactors(hasTOTP bool, hasWebAuthn bool, hasEmail2FA bool, hasRecoveryKey bool) []string {
	factors := make([]string, 0, 4)
	if hasTOTP {
		factors = append(factors, "totp")
	}
	if hasWebAuthn {
		factors = append(factors, "webauthn")
	}
	if hasEmail2FA {
		factors = append(factors, "email")
	}
	if hasRecoveryKey {
		factors = append(factors, "recovery_key")
	}
	return factors
}

func newAuthHookPayload(c *gin.Context, email string, password string) *HookRequestPayload {
	return &HookRequestPayload{
		Request: HookRequestContext{
			ID:     requestIDFrom(c),
			Path:   c.Request.URL.Path,
			Method: c.Request.Method,
		},
		Auth: HookAuthContext{
			Email:            strings.TrimSpace(email),
			Password:         password,
			PasswordEncoding: "plain",
			RiskTags:         nil,
		},
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
