package services

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"gorm.io/gorm"
)

var (
	ErrOAuthInvalidClient        = errors.New("invalid_client")
	ErrOAuthInvalidGrant         = errors.New("invalid_grant")
	ErrOAuthInvalidScope         = errors.New("invalid_scope")
	ErrOAuthUnauthorizedClient   = errors.New("unauthorized_client")
	ErrOAuthInvalidRedirectURI   = errors.New("invalid_redirect_uri")
	ErrOAuthUnsupportedGrantType = errors.New("unsupported_grant_type")
	ErrOAuthInvalidCodeChallenge = errors.New("invalid_code_challenge")
	ErrOAuthAccessDenied         = errors.New("access_denied")
	ErrOAuthInvalidTarget        = errors.New("invalid_target")
	ErrOAuthInsufficientScope    = errors.New("insufficient_scope")
)

type OAuth2Service struct{}

type OAuth2TokenContext struct {
	Token       *models.OAuth2AccessToken
	Client      *models.OAuth2Client
	User        *models.User
	Scopes      []string
	IsSuper     bool
	IsService   bool
	TargetUID   string
	TargetEmail string
}

func NewOAuth2Service() *OAuth2Service {
	return &OAuth2Service{}
}

func (os *OAuth2Service) EnsureBuiltInClients() error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := os.upsertBuiltInPublicClient(tx); err != nil {
			return err
		}
		return os.upsertBuiltInSuperClient(tx)
	})
}

func (os *OAuth2Service) upsertBuiltInPublicClient(tx *gorm.DB) error {
	redirectURIsJSON, err := marshalStringSlice(config.AppConfig.OAuth2.PublicRedirectURIs)
	if err != nil {
		return err
	}
	scopesJSON, err := marshalStringSlice(publicSiteScopes())
	if err != nil {
		return err
	}
	grantTypesJSON, err := marshalStringSlice([]string{"authorization_code", "refresh_token"})
	if err != nil {
		return err
	}

	values := map[string]any{
		"client_secret": "",
		"name":          "HRPAuth WebUI",
		"type":          "public",
		"grant_types":   grantTypesJSON,
		"redirect_uris": redirectURIsJSON,
		"scopes":        scopesJSON,
		"is_internal":   true,
		"is_super":      false,
		"is_active":     true,
	}

	var client models.OAuth2Client
	err = tx.Where("client_id = ?", config.AppConfig.OAuth2.PublicClientID).First(&client).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		client = models.OAuth2Client{
			ClientID:     config.AppConfig.OAuth2.PublicClientID,
			ClientSecret: "",
			Name:         "HRPAuth WebUI",
			Type:         "public",
			GrantTypes:   grantTypesJSON,
			RedirectURIs: redirectURIsJSON,
			Scopes:       scopesJSON,
			IsInternal:   true,
			IsSuper:      false,
			IsActive:     true,
		}
		return tx.Create(&client).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&client).Updates(values).Error
}

func (os *OAuth2Service) upsertBuiltInSuperClient(tx *gorm.DB) error {
	scopesJSON, err := marshalStringSlice(serviceSiteScopes())
	if err != nil {
		return err
	}
	grantTypesJSON, err := marshalStringSlice([]string{"client_credentials"})
	if err != nil {
		return err
	}
	values := map[string]any{
		"client_secret": config.AppConfig.OAuth2.SuperClientSecret,
		"name":          "HRPAuth Internal Super Client",
		"type":          "confidential",
		"grant_types":   grantTypesJSON,
		"redirect_uris": "[]",
		"scopes":        scopesJSON,
		"is_internal":   true,
		"is_super":      true,
		"is_active":     true,
	}

	var client models.OAuth2Client
	err = tx.Where("client_id = ?", config.AppConfig.OAuth2.SuperClientID).First(&client).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		client = models.OAuth2Client{
			ClientID:     config.AppConfig.OAuth2.SuperClientID,
			ClientSecret: config.AppConfig.OAuth2.SuperClientSecret,
			Name:         "HRPAuth Internal Super Client",
			Type:         "confidential",
			GrantTypes:   grantTypesJSON,
			RedirectURIs: "[]",
			Scopes:       scopesJSON,
			IsInternal:   true,
			IsSuper:      true,
			IsActive:     true,
		}
		return tx.Create(&client).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&client).Updates(values).Error
}

func (os *OAuth2Service) AuthenticateClient(clientID, clientSecret string) (*models.OAuth2Client, error) {
	var client models.OAuth2Client
	if err := database.DB.Where("client_id = ? AND is_active = 1", clientID).First(&client).Error; err != nil {
		return nil, ErrOAuthInvalidClient
	}
	if client.Type == "confidential" && client.ClientSecret != clientSecret {
		return nil, ErrOAuthInvalidClient
	}
	return &client, nil
}

func (os *OAuth2Service) GetClient(clientID string) (*models.OAuth2Client, error) {
	var client models.OAuth2Client
	if err := database.DB.Where("client_id = ? AND is_active = 1", clientID).First(&client).Error; err != nil {
		return nil, ErrOAuthInvalidClient
	}
	return &client, nil
}

func (os *OAuth2Service) ValidateAuthorizationRequest(clientID, redirectURI, scope, codeChallenge, codeChallengeMethod string) (*models.OAuth2Client, []string, error) {
	client, err := os.GetClient(clientID)
	if err != nil {
		return nil, nil, err
	}
	if !os.supportsGrantType(client, "authorization_code") {
		return nil, nil, ErrOAuthUnauthorizedClient
	}
	if client.Type != "public" {
		return nil, nil, ErrOAuthUnauthorizedClient
	}
	if codeChallenge == "" || strings.ToUpper(codeChallengeMethod) != "S256" {
		return nil, nil, ErrOAuthInvalidCodeChallenge
	}
	if !os.redirectURIAllowed(client, redirectURI) {
		return nil, nil, ErrOAuthInvalidRedirectURI
	}
	scopes, err := os.resolveRequestedScopes(client, scope)
	if err != nil {
		return nil, nil, err
	}
	return client, scopes, nil
}

func (os *OAuth2Service) CreateAuthorizationCode(clientID, userID, redirectURI string, scopes []string, codeChallenge, codeChallengeMethod string) (*models.OAuth2AuthorizationCode, error) {
	code := &models.OAuth2AuthorizationCode{
		Code:                utils.GenerateRandomToken(32),
		ClientID:            clientID,
		UserID:              userID,
		RedirectURI:         redirectURI,
		Scopes:              mustMarshalStringSlice(scopes),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: strings.ToUpper(codeChallengeMethod),
		ExpiresAt:           time.Now().Add(time.Duration(config.AppConfig.OAuth2.AuthorizationCodeTTL) * time.Second),
	}
	if err := database.DB.Create(code).Error; err != nil {
		return nil, err
	}
	return code, nil
}

func (os *OAuth2Service) ExchangeAuthorizationCode(clientID, code, redirectURI, codeVerifier string) (*models.OAuth2AccessToken, *models.OAuth2RefreshToken, error) {
	var authCode models.OAuth2AuthorizationCode
	if err := database.DB.Where("code = ?", code).First(&authCode).Error; err != nil {
		return nil, nil, ErrOAuthInvalidGrant
	}
	if authCode.ClientID != clientID || authCode.RedirectURI != redirectURI {
		return nil, nil, ErrOAuthInvalidGrant
	}
	if authCode.ConsumedAt != nil || time.Now().After(authCode.ExpiresAt) {
		return nil, nil, ErrOAuthInvalidGrant
	}
	if !verifyPKCE(codeVerifier, authCode.CodeChallenge) {
		return nil, nil, ErrOAuthInvalidGrant
	}

	var scopes []string
	if err := json.Unmarshal([]byte(authCode.Scopes), &scopes); err != nil {
		return nil, nil, err
	}

	accessToken, refreshToken, err := os.issueUserTokens(clientID, authCode.UserID, scopes)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	if err := database.DB.Model(&authCode).Update("consumed_at", &now).Error; err != nil {
		return nil, nil, err
	}
	return accessToken, refreshToken, nil
}

func (os *OAuth2Service) RefreshUserToken(clientID, refreshTokenValue string) (*models.OAuth2AccessToken, *models.OAuth2RefreshToken, error) {
	var refreshToken models.OAuth2RefreshToken
	if err := database.DB.Where("refresh_token = ?", refreshTokenValue).First(&refreshToken).Error; err != nil {
		return nil, nil, ErrOAuthInvalidGrant
	}
	if refreshToken.ClientID != clientID || refreshToken.RevokedAt != nil || time.Now().After(refreshToken.ExpiresAt) {
		return nil, nil, ErrOAuthInvalidGrant
	}
	var scopes []string
	if err := json.Unmarshal([]byte(refreshToken.Scopes), &scopes); err != nil {
		return nil, nil, err
	}
	accessToken, newRefreshToken, err := os.issueUserTokens(clientID, refreshToken.UserID, scopes)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	if err := database.DB.Model(&refreshToken).Update("revoked_at", &now).Error; err != nil {
		return nil, nil, err
	}
	return accessToken, newRefreshToken, nil
}

func (os *OAuth2Service) IssueClientCredentialsToken(client *models.OAuth2Client, scope string, targetUID string, targetEmail string) (*models.OAuth2AccessToken, error) {
	if !os.supportsGrantType(client, "client_credentials") {
		return nil, ErrOAuthUnauthorizedClient
	}
	scopes, err := os.resolveRequestedScopes(client, scope)
	if err != nil {
		return nil, err
	}
	if (targetUID != "" || targetEmail != "") && !containsAny(scopes, targetedServiceScopes()) {
		return nil, ErrOAuthInsufficientScope
	}

	token := &models.OAuth2AccessToken{
		AccessToken: utils.GenerateRandomToken(32),
		ClientID:    client.ClientID,
		Scopes:      mustMarshalStringSlice(scopes),
		SubjectType: "service",
		ExpiresAt:   time.Now().Add(time.Duration(config.AppConfig.OAuth2.AccessTokenTTL) * time.Second),
	}
	if targetUID != "" {
		if uid, ok := utils.ParseUintString(targetUID); ok {
			token.TargetUID = &uid
		} else {
			return nil, ErrOAuthInvalidTarget
		}
	}
	if targetEmail != "" {
		targetEmail = strings.TrimSpace(strings.ToLower(targetEmail))
		token.TargetEmail = &targetEmail
	}
	if err := database.DB.Create(token).Error; err != nil {
		return nil, err
	}
	return token, nil
}

func (os *OAuth2Service) ResolveAccessToken(accessToken string) (*OAuth2TokenContext, error) {
	var token models.OAuth2AccessToken
	if err := database.DB.Where("access_token = ?", accessToken).First(&token).Error; err != nil {
		return nil, ErrOAuthInvalidGrant
	}
	if token.RevokedAt != nil || time.Now().After(token.ExpiresAt) {
		return nil, ErrOAuthInvalidGrant
	}

	client, err := os.GetClient(token.ClientID)
	if err != nil {
		return nil, err
	}

	scopes := []string{}
	if err := json.Unmarshal([]byte(token.Scopes), &scopes); err != nil {
		return nil, err
	}

	ctx := &OAuth2TokenContext{
		Token:       &token,
		Client:      client,
		Scopes:      scopes,
		IsSuper:     client.IsSuper,
		IsService:   token.SubjectType == "service",
		TargetUID:   "",
		TargetEmail: "",
	}
	if token.TargetUID != nil {
		ctx.TargetUID = utils.UintToString(*token.TargetUID)
	}
	if token.TargetEmail != nil {
		ctx.TargetEmail = *token.TargetEmail
	}
	if token.UserID != nil {
		var user models.User
		if err := database.DB.Where("uuid = ?", *token.UserID).First(&user).Error; err == nil {
			ctx.User = &user
		}
	}
	return ctx, nil
}

func (os *OAuth2Service) RevokeAccessToken(accessToken string) error {
	now := time.Now()
	return database.DB.Model(&models.OAuth2AccessToken{}).
		Where("access_token = ? AND revoked_at IS NULL", accessToken).
		Update("revoked_at", &now).Error
}

func (os *OAuth2Service) IssueFirstPartyUserTokens(userID string) (*models.OAuth2AccessToken, *models.OAuth2RefreshToken, error) {
	return os.issueUserTokens(config.AppConfig.OAuth2.PublicClientID, userID, publicSiteScopes())
}

func (os *OAuth2Service) issueUserTokens(clientID, userID string, scopes []string) (*models.OAuth2AccessToken, *models.OAuth2RefreshToken, error) {
	uid := userID
	accessToken := &models.OAuth2AccessToken{
		AccessToken: utils.GenerateRandomToken(32),
		ClientID:    clientID,
		UserID:      &uid,
		Scopes:      mustMarshalStringSlice(scopes),
		SubjectType: "user",
		ExpiresAt:   time.Now().Add(time.Duration(config.AppConfig.OAuth2.AccessTokenTTL) * time.Second),
	}
	refreshToken := &models.OAuth2RefreshToken{
		RefreshToken: utils.GenerateRandomToken(32),
		ClientID:     clientID,
		UserID:       userID,
		Scopes:       mustMarshalStringSlice(scopes),
		ExpiresAt:    time.Now().Add(time.Duration(config.AppConfig.OAuth2.RefreshTokenTTL) * time.Second),
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(accessToken).Error; err != nil {
			return err
		}
		refreshToken.AccessTokenID = accessToken.ID
		return tx.Create(refreshToken).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return accessToken, refreshToken, nil
}

func (os *OAuth2Service) supportsGrantType(client *models.OAuth2Client, grantType string) bool {
	values, err := unmarshalStringSlice(client.GrantTypes)
	if err != nil {
		return false
	}
	return slices.Contains(values, grantType)
}

func (os *OAuth2Service) redirectURIAllowed(client *models.OAuth2Client, redirectURI string) bool {
	values, err := unmarshalStringSlice(client.RedirectURIs)
	if err != nil {
		return false
	}
	return slices.Contains(values, redirectURI)
}

func (os *OAuth2Service) resolveRequestedScopes(client *models.OAuth2Client, scope string) ([]string, error) {
	allowed, err := unmarshalStringSlice(client.Scopes)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(scope) == "" {
		return allowed, nil
	}
	requested := splitScopes(scope)
	for _, item := range requested {
		if !slices.Contains(allowed, item) {
			return nil, ErrOAuthInvalidScope
		}
	}
	return requested, nil
}

func splitScopes(scope string) []string {
	parts := strings.Fields(strings.TrimSpace(scope))
	result := make([]string, 0, len(parts))
	for _, item := range parts {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func publicSiteScopes() []string {
	return []string{
		"user.read",
		"user.mojang-bind-enable",
		"profile.change-username",
		"profile.change-name",
		"texture.upload",
		"texture.delete",
		"texture.get",
		"totp.setup",
		"totp.status",
	}
}

func serviceSiteScopes() []string {
	return append([]string{
		"register.manage",
		"user.declare-email",
	}, targetedServiceScopes()...)
}

func targetedServiceScopes() []string {
	return []string{
		"user.read.as-service",
		"user.mojang-bind-enable.as-service",
		"profile.change-username.as-service",
		"profile.change-name.as-service",
		"texture.upload.as-service",
		"texture.delete.as-service",
		"texture.get.as-service",
		"totp.setup.as-service",
		"totp.status.as-service",
	}
}

func verifyPKCE(codeVerifier, codeChallenge string) bool {
	sum := sha256.Sum256([]byte(codeVerifier))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	return encoded == codeChallenge
}

func marshalStringSlice(values []string) (string, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mustMarshalStringSlice(values []string) string {
	data, _ := marshalStringSlice(values)
	return data
}

func unmarshalStringSlice(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func containsAny(have []string, wants []string) bool {
	for _, want := range wants {
		if slices.Contains(have, want) {
			return true
		}
	}
	return false
}
