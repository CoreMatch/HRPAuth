package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	wa "github.com/go-webauthn/webauthn/webauthn"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	appredis "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/utils"
	"gorm.io/gorm"
)

var (
	ErrWebAuthnNotConfigured      = errors.New("webauthn_not_configured")
	ErrWebAuthnInvalidFlow        = errors.New("invalid_webauthn_flow")
	ErrWebAuthnCredentialNotFound = errors.New("webauthn_credential_not_found")
)

const (
	webAuthnFlowTypeRegister     = "register"
	webAuthnFlowTypeLogin        = "login"
	webAuthnFlowTypeDiscoverable = "discoverable_login"
	webAuthnFlowTypeSecondFactor = "second_factor"
)

var (
	webAuthnOnce     sync.Once
	webAuthnInstance *wa.WebAuthn
	webAuthnInitErr  error
)

type WebAuthnService struct{}

type webAuthnIdentity struct {
	user        models.User
	credentials []wa.Credential
}

type webAuthnFlow struct {
	Type           string         `json:"type"`
	UserID         string         `json:"user_id,omitempty"`
	LoginTicket    string         `json:"login_ticket,omitempty"`
	CredentialName string         `json:"credential_name,omitempty"`
	SessionData    wa.SessionData `json:"session_data"`
}

func NewWebAuthnService() *WebAuthnService {
	return &WebAuthnService{}
}

func (u *webAuthnIdentity) WebAuthnID() []byte {
	return []byte(u.user.UUID)
}

func (u *webAuthnIdentity) WebAuthnName() string {
	if strings.TrimSpace(u.user.Email) != "" {
		return u.user.Email
	}
	return u.user.Username
}

func (u *webAuthnIdentity) WebAuthnDisplayName() string {
	if strings.TrimSpace(u.user.Username) != "" {
		return u.user.Username
	}
	return u.WebAuthnName()
}

func (u *webAuthnIdentity) WebAuthnCredentials() []wa.Credential {
	return u.credentials
}

func (ws *WebAuthnService) BeginRegistration(user *models.User, credentialName string, attachment string) (*protocol.CredentialCreation, string, error) {
        identity, err := ws.loadIdentityByUUIDAllowEmpty(user.UUID)
	if err != nil {
		return nil, "", err
	}

	instance, err := ws.instance()
	if err != nil {
		return nil, "", err
	}

	selection := protocol.AuthenticatorSelection{
		UserVerification:   protocol.VerificationRequired,
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		RequireResidentKey: protocol.ResidentKeyRequired(),
	}

	if attachment != "" {
		parsedAttachment, err := parseAuthenticatorAttachment(attachment)
		if err != nil {
			return nil, "", err
		}
		selection.AuthenticatorAttachment = parsedAttachment
	}

	creation, session, err := instance.BeginRegistration(identity,
		wa.WithAuthenticatorSelection(selection),
		wa.WithExclusions(wa.Credentials(identity.WebAuthnCredentials()).CredentialDescriptors()),
		wa.WithExtensions(protocol.AuthenticationExtensions{"credProps": true}),
	)
	if err != nil {
		return nil, "", err
	}

	flowID, err := ws.saveFlow(&webAuthnFlow{
		Type:           webAuthnFlowTypeRegister,
		UserID:         user.UUID,
		CredentialName: defaultCredentialName(credentialName, attachment),
		SessionData:    *session,
	})
	if err != nil {
		return nil, "", err
	}

	return creation, flowID, nil
}

func (ws *WebAuthnService) FinishRegistration(flowID string, expectedUserID string, payload []byte) (*models.WebAuthnCredential, error) {
	flow, err := ws.getFlow(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Type != webAuthnFlowTypeRegister || flow.UserID == "" {
		return nil, ErrWebAuthnInvalidFlow
	}
	if strings.TrimSpace(expectedUserID) == "" || flow.UserID != expectedUserID {
		return nil, ErrWebAuthnInvalidFlow
	}

	identity, err := ws.loadIdentityByUUID(flow.UserID)
	if err != nil {
		return nil, err
	}

	instance, err := ws.instance()
	if err != nil {
		return nil, err
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload)
	if err != nil {
		return nil, err
	}

	credential, err := instance.CreateCredential(identity, flow.SessionData, parsed)
	if err != nil {
		return nil, err
	}

	row, err := ws.insertCredential(flow.UserID, flow.CredentialName, credential)
	if err != nil {
		return nil, err
	}

	_ = ws.deleteFlow(flowID)

	return row, nil
}

func (ws *WebAuthnService) BeginUserLoginByEmail(email string) (*protocol.CredentialAssertion, string, error) {
	normalized := strings.TrimSpace(strings.ToLower(email))
	if normalized == "" {
		return nil, "", ErrWebAuthnNotConfigured
	}

	identity, err := ws.loadIdentityByEmail(normalized)
	if err != nil {
		return nil, "", err
	}
	if len(identity.credentials) == 0 {
		return nil, "", ErrWebAuthnNotConfigured
	}

	instance, err := ws.instance()
	if err != nil {
		return nil, "", err
	}

	assertion, session, err := instance.BeginLogin(identity, wa.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}

	flowID, err := ws.saveFlow(&webAuthnFlow{
		Type:        webAuthnFlowTypeLogin,
		UserID:      identity.user.UUID,
		SessionData: *session,
	})
	if err != nil {
		return nil, "", err
	}

	return assertion, flowID, nil
}

func (ws *WebAuthnService) BeginDiscoverableLogin() (*protocol.CredentialAssertion, string, error) {
	instance, err := ws.instance()
	if err != nil {
		return nil, "", err
	}

	assertion, session, err := instance.BeginDiscoverableLogin(wa.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}

	flowID, err := ws.saveFlow(&webAuthnFlow{
		Type:        webAuthnFlowTypeDiscoverable,
		SessionData: *session,
	})
	if err != nil {
		return nil, "", err
	}

	return assertion, flowID, nil
}

func (ws *WebAuthnService) BeginSecondFactorLogin(userUUID string, loginTicket string) (*protocol.CredentialAssertion, string, error) {
	identity, err := ws.loadIdentityByUUID(userUUID)
	if err != nil {
		return nil, "", err
	}
	if len(identity.credentials) == 0 {
		return nil, "", ErrWebAuthnNotConfigured
	}

	instance, err := ws.instance()
	if err != nil {
		return nil, "", err
	}

	assertion, session, err := instance.BeginLogin(identity, wa.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return nil, "", err
	}

	flowID, err := ws.saveFlow(&webAuthnFlow{
		Type:        webAuthnFlowTypeSecondFactor,
		UserID:      userUUID,
		LoginTicket: loginTicket,
		SessionData: *session,
	})
	if err != nil {
		return nil, "", err
	}

	return assertion, flowID, nil
}

func (ws *WebAuthnService) FinishLogin(flowID string, payload []byte) (*models.User, error) {
	flow, err := ws.getFlow(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Type == webAuthnFlowTypeSecondFactor {
		if strings.TrimSpace(flow.LoginTicket) == "" {
			return nil, ErrWebAuthnInvalidFlow
		}
		key := config.AppConfig.Redis.Prefix + "oauth2:login_ticket:" + flow.LoginTicket
		if _, err := appredis.Client.Get(context.Background(), key).Result(); err != nil {
			return nil, ErrWebAuthnInvalidFlow
		}
	}

	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		return nil, err
	}

	instance, err := ws.instance()
	if err != nil {
		return nil, err
	}

	switch flow.Type {
	case webAuthnFlowTypeLogin, webAuthnFlowTypeSecondFactor:
		identity, err := ws.loadIdentityByUUID(flow.UserID)
		if err != nil {
			return nil, err
		}
		credential, err := instance.ValidateLogin(identity, flow.SessionData, parsed)
		if err != nil {
			return nil, err
		}
		if err := ws.updateCredential(identity.user.UUID, credential); err != nil {
			return nil, err
		}
		_ = ws.deleteFlow(flowID)
		return &identity.user, nil
	case webAuthnFlowTypeDiscoverable:
		user, credential, err := instance.ValidatePasskeyLogin(ws.lookupIdentityByCredential, flow.SessionData, parsed)
		if err != nil {
			return nil, err
		}
		identity, ok := user.(*webAuthnIdentity)
		if !ok {
			return nil, fmt.Errorf("unexpected user type")
		}
		if err := ws.updateCredential(identity.user.UUID, credential); err != nil {
			return nil, err
		}
		_ = ws.deleteFlow(flowID)
		return &identity.user, nil
	default:
		return nil, ErrWebAuthnInvalidFlow
	}
}

func (ws *WebAuthnService) FinishSecondFactor(flowID string, payload []byte) (*models.User, string, error) {
	flow, err := ws.getFlow(flowID)
	if err != nil {
		return nil, "", err
	}
	if flow.Type != webAuthnFlowTypeSecondFactor || strings.TrimSpace(flow.LoginTicket) == "" {
		return nil, "", ErrWebAuthnInvalidFlow
	}

	user, err := ws.FinishLogin(flowID, payload)
	if err != nil {
		return nil, "", err
	}

	return user, flow.LoginTicket, nil
}

func (ws *WebAuthnService) CountCredentials(userUUID string) (int64, error) {
	var count int64
	if err := database.DB.Model(&models.WebAuthnCredential{}).Where("user_id = ?", userUUID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (ws *WebAuthnService) ListCredentials(userUUID string) ([]models.WebAuthnCredential, error) {
	var rows []models.WebAuthnCredential
	if err := database.DB.Where("user_id = ?", userUUID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (ws *WebAuthnService) DeleteCredential(userUUID string, id uint) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND user_id = ?", id, userUUID).Delete(&models.WebAuthnCredential{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrWebAuthnCredentialNotFound
		}

		var count int64
		if err := tx.Model(&models.WebAuthnCredential{}).Where("user_id = ?", userUUID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := tx.Model(&models.User{}).Where("uuid = ?", userUUID).Update("webauthn_2fa_enabled", false).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (ws *WebAuthnService) SetTwoFactorEnabled(userUUID string, enabled bool) error {
	if enabled {
		count, err := ws.CountCredentials(userUUID)
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrWebAuthnNotConfigured
		}
	}

	return database.DB.Model(&models.User{}).Where("uuid = ?", userUUID).Update("webauthn_2fa_enabled", enabled).Error
}

func (ws *WebAuthnService) loadIdentityByEmail(email string) (*webAuthnIdentity, error) {
	var user models.User
	if err := database.DB.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return ws.loadIdentityByUUID(user.UUID)
}

func (ws *WebAuthnService) loadIdentityByUUID(userUUID string) (*webAuthnIdentity, error) {
        return ws.loadIdentityByUUIDWithCredentialRequirement(userUUID, true)
}

func (ws *WebAuthnService) loadIdentityByUUIDAllowEmpty(userUUID string) (*webAuthnIdentity, error) {
        return ws.loadIdentityByUUIDWithCredentialRequirement(userUUID, false)
}

func (ws *WebAuthnService) loadIdentityByUUIDWithCredentialRequirement(userUUID string, requireCredentials bool) (*webAuthnIdentity, error) {
	var user models.User
	if err := database.DB.Where("uuid = ?", userUUID).First(&user).Error; err != nil {
		return nil, err
	}

	rows, err := ws.ListCredentials(userUUID)
	if err != nil {
		return nil, err
	}
        if requireCredentials && len(rows) == 0 {
		return nil, ErrWebAuthnNotConfigured
	}

	credentials := make([]wa.Credential, 0, len(rows))
	for _, row := range rows {
		var credential wa.Credential
		if err := json.Unmarshal([]byte(row.CredentialJSON), &credential); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}

	return &webAuthnIdentity{
		user:        user,
		credentials: credentials,
	}, nil
}

func (ws *WebAuthnService) lookupIdentityByCredential(rawID []byte, userHandle []byte) (wa.User, error) {
	var row models.WebAuthnCredential
	if err := database.DB.Where("credential_id_hash = ?", credentialIDHash(rawID)).First(&row).Error; err != nil {
		return nil, err
	}

	identity, err := ws.loadIdentityByUUID(row.UserID)
	if err != nil {
		return nil, err
	}

	if len(userHandle) > 0 && string(userHandle) != identity.user.UUID {
		return nil, fmt.Errorf("user handle mismatch")
	}

	return identity, nil
}

func (ws *WebAuthnService) insertCredential(userUUID string, name string, credential *wa.Credential) (*models.WebAuthnCredential, error) {
	payload, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}

	row := &models.WebAuthnCredential{
		UserID:           userUUID,
		Name:             name,
		CredentialID:     encodeCredentialID(credential.ID),
		CredentialIDHash: credentialIDHash(credential.ID),
		CredentialJSON:   string(payload),
	}

	if err := database.DB.Create(row).Error; err != nil {
		return nil, err
	}

	return row, nil
}

func (ws *WebAuthnService) updateCredential(userUUID string, credential *wa.Credential) error {
	payload, err := json.Marshal(credential)
	if err != nil {
		return err
	}

	now := time.Now()
	result := database.DB.Model(&models.WebAuthnCredential{}).
		Where("user_id = ? AND credential_id_hash = ?", userUUID, credentialIDHash(credential.ID)).
		Updates(map[string]any{
			"credential_json": string(payload),
			"last_used_at":    &now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWebAuthnCredentialNotFound
	}
	return nil
}

func (ws *WebAuthnService) saveFlow(flow *webAuthnFlow) (string, error) {
	payload, err := json.Marshal(flow)
	if err != nil {
		return "", err
	}

	flowID := utils.GenerateRandomToken(24)
	ttl := time.Duration(config.AppConfig.WebAuthn.SessionTTL) * time.Second
	if !flow.SessionData.Expires.IsZero() {
		if until := time.Until(flow.SessionData.Expires); until > 0 {
			ttl = until
		}
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	key := config.AppConfig.Redis.Prefix + "webauthn:flow:" + flowID
	if err := appredis.Client.Set(context.Background(), key, payload, ttl).Err(); err != nil {
		return "", err
	}
	return flowID, nil
}

func (ws *WebAuthnService) getFlow(flowID string) (*webAuthnFlow, error) {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return nil, ErrWebAuthnInvalidFlow
	}

	key := config.AppConfig.Redis.Prefix + "webauthn:flow:" + flowID
	raw, err := appredis.Client.Get(context.Background(), key).Result()
	if err != nil {
		return nil, ErrWebAuthnInvalidFlow
	}

	var flow webAuthnFlow
	if err := json.Unmarshal([]byte(raw), &flow); err != nil {
		return nil, ErrWebAuthnInvalidFlow
	}

	return &flow, nil
}

func (ws *WebAuthnService) deleteFlow(flowID string) error {
	key := config.AppConfig.Redis.Prefix + "webauthn:flow:" + strings.TrimSpace(flowID)
	return appredis.Client.Del(context.Background(), key).Err()
}

func (ws *WebAuthnService) instance() (*wa.WebAuthn, error) {
	webAuthnOnce.Do(func() {
		cfg := config.AppConfig.WebAuthn
		webAuthnInstance, webAuthnInitErr = wa.New(&wa.Config{
			RPDisplayName: cfg.RPDisplayName,
			RPID:          cfg.RPID,
			RPOrigins:     cfg.RPOrigins,
			Timeouts: wa.TimeoutsConfig{
				Login: wa.TimeoutConfig{
					Enforce: true,
					Timeout: time.Duration(cfg.SessionTTL) * time.Second,
				},
				Registration: wa.TimeoutConfig{
					Enforce: true,
					Timeout: time.Duration(cfg.SessionTTL) * time.Second,
				},
			},
		})
	})
	return webAuthnInstance, webAuthnInitErr
}

func parseAuthenticatorAttachment(value string) (protocol.AuthenticatorAttachment, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "platform":
		return protocol.Platform, nil
	case "cross-platform", "cross_platform", "security-key", "security_key":
		return protocol.CrossPlatform, nil
	default:
		return "", fmt.Errorf("invalid authenticator attachment")
	}
}

func defaultCredentialName(name string, attachment string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	switch strings.TrimSpace(strings.ToLower(attachment)) {
	case "platform":
		return "Platform Passkey"
	case "cross-platform", "cross_platform", "security-key", "security_key":
		return "Security Key"
	default:
		return "Passkey"
	}
}

func encodeCredentialID(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func credentialIDHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
