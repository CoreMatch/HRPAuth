package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDContextKey = "request_id"

const (
	CodeInvalidJSONBody                = "invalid_json_body"
	CodeInvalidRequest                 = "invalid_request"
	CodeInvalidEmail                   = "invalid_email"
	CodeInvalidCredentials             = "invalid_credentials"
	CodeInvalidAuthTypeOrToken         = "invalid_auth_type_or_token"
	CodeInvalidManageToken             = "invalid_manage_token"
	CodeInvalidMojangUUID              = "invalid_mojang_uuid"
	CodeMojangUUIDRequired             = "mojang_uuid_required_for_existing_user"
	CodeCaptchaDisabled                = "captcha_disabled"
	CodeCaptchaInvalid                 = "captcha_invalid"
	CodeRememberTokenRequired          = "remember_token_required"
	CodeInvalidRememberToken           = "invalid_remember_token"
	CodeManageTargetRequired           = "manage_target_required"
	CodeUserNotFound                   = "user_not_found"
	CodeUsernameTooShort               = "username_too_short"
	CodePasswordTooShort               = "password_too_short"
	CodeUsernameAlreadyTaken           = "username_already_taken"
	CodeEmailAlreadyRegistered         = "email_already_registered"
	CodeUsernameAlreadyBound           = "username_already_bound"
	CodeTextureTypeInvalid             = "invalid_texture_type"
	CodeTextureFileRequired            = "texture_file_required"
	CodeTextureUploadFailed            = "texture_upload_failed"
	CodeTextureDeleteFailed            = "texture_delete_failed"
	CodeTextureReadFailed              = "texture_read_failed"
	CodeProfileNotFound                = "profile_not_found"
	CodeProfileAccessDenied            = "profile_access_denied"
	CodeProfileNameInvalid             = "invalid_profile_name"
	CodeUsernameInvalid                = "invalid_username"
	CodeUsernameConflict               = "username_conflict"
	CodeProfileNameConflict            = "profile_name_conflict"
	CodeVerificationCodeAlreadySent    = "verification_code_already_sent"
	CodeVerificationCodeExpired        = "verification_code_expired_or_missing"
	CodeVerificationCodeInvalid        = "verification_code_invalid"
	CodeEmailSendFailed                = "email_send_failed"
	CodeVerificationStatusUpdateFailed = "verification_status_update_failed"
	CodeTOTPSecretRequired             = "totp_secret_required"
	CodeTOTPNotConfigured              = "totp_not_configured"
	CodePasscodeInvalid                = "invalid_passcode"
	CodeKeygenDisabled                 = "keygen_disabled"
	CodeKeygenFailed                   = "keygen_failed"
	CodeOAuthInvalidClient             = "oauth_invalid_client"
	CodeOAuthInvalidGrant              = "oauth_invalid_grant"
	CodeOAuthInvalidScope              = "oauth_invalid_scope"
	CodeOAuthUnauthorizedClient        = "oauth_unauthorized_client"
	CodeOAuthInvalidRedirectURI        = "oauth_invalid_redirect_uri"
	CodeOAuthUnsupportedGrantType      = "oauth_unsupported_grant_type"
	CodeOAuthInvalidCodeChallenge      = "oauth_invalid_code_challenge"
	CodeOAuthInsufficientScope         = "oauth_insufficient_scope"
	CodeOAuthAccessDenied              = "oauth_access_denied"
	CodeOAuthLoginRequired             = "oauth_login_required"
	CodeLoginTicketRequired            = "login_ticket_required"
	CodeInvalidLoginTicket             = "invalid_login_ticket"
	CodeEndpointDeprecated             = "endpoint_deprecated"
	CodeInternalError                  = "internal_error"
)

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-Id"))
		if requestID == "" {
			requestID = newRequestID()
		}

		c.Set(requestIDContextKey, requestID)
		c.Header("X-Request-Id", requestID)
		c.Next()
	}
}

func respondOK(c *gin.Context, message string, data any) {
	respondJSON(c, http.StatusOK, true, "", message, data)
}

func respondCreated(c *gin.Context, message string, data any) {
	respondJSON(c, http.StatusCreated, true, "", message, data)
}

func respondError(c *gin.Context, status int, code, message string) {
	respondJSON(c, status, false, code, message, nil)
}

func respondJSON(c *gin.Context, status int, success bool, code, message string, data any) {
	payload := gin.H{
		"success": success,
		"meta": gin.H{
			"request_id": requestIDFrom(c),
		},
	}

	if message != "" {
		payload["message"] = message
	}
	if code != "" {
		payload["code"] = code
		// Compatibility alias for existing callers like WinnerProxy.
		payload["error"] = code
	}
	if data != nil {
		payload["data"] = data
		if success {
			switch typed := data.(type) {
			case gin.H:
				for key, value := range typed {
					payload[key] = value
				}
			case map[string]any:
				for key, value := range typed {
					payload[key] = value
				}
			}
		}
	}

	c.JSON(status, payload)
}

func requestIDFrom(c *gin.Context) string {
	if value, exists := c.Get(requestIDContextKey); exists {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return newRequestID()
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
