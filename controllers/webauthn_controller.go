package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	appredis "github.com/lnb/HRPAuth-Backend-Go/redis"
	"github.com/lnb/HRPAuth-Backend-Go/services"
	"gorm.io/gorm"
)

type WebAuthnController struct {
	service *services.WebAuthnService
}

type WebAuthnRegistrationBeginRequest struct {
	Name       string `json:"name"`
	Attachment string `json:"attachment"`
}

type WebAuthnLoginBeginRequest struct {
	Email string `json:"email"`
}

type WebAuthnFinishRequest struct {
	FlowID     string          `json:"flow_id"`
	Credential json.RawMessage `json:"credential"`
}

type WebAuthnSecondFactorBeginRequest struct {
	LoginTicket string `json:"login_ticket"`
}

type WebAuthnToggle2FARequest struct {
	Enabled bool `json:"enabled"`
}

// #region debug-point B:report-helper
func reportWebAuthnControllerDebug(hypothesisID string, location string, msg string, data map[string]any) {
	payload, err := json.Marshal(map[string]any{
		"sessionId":    "webauthn-bind-flow",
		"runId":        "pre-fix",
		"hypothesisId": hypothesisID,
		"location":     location,
		"msg":          "[DEBUG] " + msg,
		"data":         data,
		"ts":           time.Now().UnixMilli(),
	})
	if err != nil {
		return
	}

	go func(body []byte) {
		req, reqErr := http.NewRequest(http.MethodPost, "http://127.0.0.1:7777/event", bytes.NewReader(body))
		if reqErr != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		_, _ = http.DefaultClient.Do(req)
	}(payload)
}

// #endregion

func NewWebAuthnController() *WebAuthnController {
	return &WebAuthnController{
		service: services.NewWebAuthnService(),
	}
}

func (wc *WebAuthnController) BeginRegistration(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "webauthn.register", "webauthn.register.as-service", false, "", "")
	if !ok {
		// #region debug-point B:begin-registration-auth-failed
		reportWebAuthnControllerDebug("B", "controllers/webauthn_controller.go:BeginRegistration:auth", "BeginRegistration failed during auth resolution", map[string]any{
			"hasAuthorization": c.GetHeader("Authorization") != "",
			"origin":           c.GetHeader("Origin"),
			"contentType":      c.ContentType(),
		})
		// #endregion
		return
	}

	var req WebAuthnRegistrationBeginRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			// #region debug-point B:begin-registration-bind-failed
			reportWebAuthnControllerDebug("B", "controllers/webauthn_controller.go:BeginRegistration:bind", "BeginRegistration request body could not be parsed", map[string]any{
				"contentLength": c.Request.ContentLength,
				"contentType":   c.ContentType(),
				"error":         err.Error(),
			})
			// #endregion
			respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
			return
		}
	}

	// #region debug-point B:begin-registration-request
	reportWebAuthnControllerDebug("B", "controllers/webauthn_controller.go:BeginRegistration:request", "BeginRegistration request accepted by controller", map[string]any{
		"userUUID":         authResult.User.UUID,
		"hasAuthorization": c.GetHeader("Authorization") != "",
		"origin":           c.GetHeader("Origin"),
		"contentType":      c.ContentType(),
		"nameLength":       len(req.Name),
		"attachment":       req.Attachment,
	})
	// #endregion

	options, flowID, err := wc.service.BeginRegistration(authResult.User, req.Name, req.Attachment)
	if err != nil {
		// #region debug-point B:begin-registration-service-failed
		reportWebAuthnControllerDebug("B", "controllers/webauthn_controller.go:BeginRegistration:service", "BeginRegistration returned an error from service layer", map[string]any{
			"userUUID": authResult.User.UUID,
			"error":    err.Error(),
		})
		// #endregion
		wc.respondWebAuthnError(c, err)
		return
	}

	// #region debug-point C:begin-registration-response
	reportWebAuthnControllerDebug("C", "controllers/webauthn_controller.go:BeginRegistration:response", "BeginRegistration returned success response", map[string]any{
		"userUUID":     authResult.User.UUID,
		"flowIDLength": len(flowID),
		"hasOptions":   options != nil,
	})
	// #endregion

	respondOK(c, "WebAuthn registration started", gin.H{
		"flow_id": flowID,
		"options": options,
	})
}

func (wc *WebAuthnController) FinishRegistration(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "webauthn.register", "webauthn.register.as-service", false, "", "")
	if !ok {
		log.Printf("info: WebAuthn finish registration auth failed origin=%q has_authorization=%t", c.GetHeader("Origin"), c.GetHeader("Authorization") != "")
		return
	}

	var req WebAuthnFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("info: WebAuthn finish registration invalid JSON user_uuid=%s origin=%q content_type=%q err=%v", authResult.User.UUID, c.GetHeader("Origin"), c.ContentType(), err)
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}
	if req.FlowID == "" || len(req.Credential) == 0 {
		log.Printf("info: WebAuthn finish registration missing fields user_uuid=%s flow_id_len=%d credential_len=%d", authResult.User.UUID, len(req.FlowID), len(req.Credential))
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing flow_id or credential")
		return
	}

	log.Printf("info: WebAuthn finish registration request user_uuid=%s flow_id_len=%d credential_len=%d origin=%q", authResult.User.UUID, len(req.FlowID), len(req.Credential), c.GetHeader("Origin"))

	credential, err := wc.service.FinishRegistration(req.FlowID, authResult.User.UUID, req.Credential)
	if err != nil {
		log.Printf("warning: WebAuthn finish registration failed user_uuid=%s flow_id_len=%d err=%v", authResult.User.UUID, len(req.FlowID), err)
		wc.respondWebAuthnError(c, err)
		return
	}

	log.Printf("info: WebAuthn finish registration succeeded user_uuid=%s credential_id=%d", authResult.User.UUID, credential.ID)

	respondCreated(c, "WebAuthn credential registered", gin.H{
		"credential": gin.H{
			"id":           credential.ID,
			"name":         credential.Name,
			"created_at":   credential.CreatedAt,
			"last_used_at": credential.LastUsedAt,
		},
	})
}

func (wc *WebAuthnController) ListCredentials(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "webauthn.credentials", "webauthn.credentials.as-service", false, "", "")
	if !ok {
		return
	}

	rows, err := wc.service.ListCredentials(authResult.User.UUID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to load WebAuthn credentials")
		return
	}

	available := true
	availabilityError := ""
	if err := wc.service.AvailabilityError(); err != nil {
		available = false
		availabilityError = err.Error()
	}

	credentials := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		credentials = append(credentials, gin.H{
			"id":           row.ID,
			"name":         row.Name,
			"created_at":   row.CreatedAt,
			"last_used_at": row.LastUsedAt,
		})
	}

	respondOK(c, "WebAuthn credentials loaded", gin.H{
		"credentials":        credentials,
		"count":              len(credentials),
		"available":          available,
		"availability_error": availabilityError,
	})
}

func (wc *WebAuthnController) DeleteCredential(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "webauthn.credentials", "webauthn.credentials.as-service", false, "", "")
	if !ok {
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Invalid credential id")
		return
	}

	if err := wc.service.DeleteCredential(authResult.User.UUID, uint(id)); err != nil {
		wc.respondWebAuthnError(c, err)
		return
	}

	respondOK(c, "WebAuthn credential deleted", gin.H{
		"id": id,
	})
}

func (wc *WebAuthnController) Toggle2FA(c *gin.Context) {
	authResult, ok := resolveSiteBearerAuth(c, "webauthn.2fa", "webauthn.2fa.as-service", false, "", "")
	if !ok {
		return
	}

	var req WebAuthnToggle2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	if err := wc.service.SetTwoFactorEnabled(authResult.User.UUID, req.Enabled); err != nil {
		wc.respondWebAuthnError(c, err)
		return
	}

	respondOK(c, "WebAuthn 2FA status updated", gin.H{
		"enabled": req.Enabled,
	})
}

func (wc *WebAuthnController) BeginLogin(c *gin.Context) {
	var req WebAuthnLoginBeginRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
			return
		}
	}

	var (
		options any
		flowID  string
		err     error
	)

	if req.Email != "" {
		options, flowID, err = wc.service.BeginUserLoginByEmail(req.Email)
	} else {
		options, flowID, err = wc.service.BeginDiscoverableLogin()
	}
	if err != nil {
		wc.respondWebAuthnError(c, err)
		return
	}

	respondOK(c, "WebAuthn login started", gin.H{
		"flow_id": flowID,
		"options": options,
	})
}

func (wc *WebAuthnController) FinishLogin(c *gin.Context) {
	var req WebAuthnFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}
	if req.FlowID == "" || len(req.Credential) == 0 {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing flow_id or credential")
		return
	}

	user, err := wc.service.FinishLogin(req.FlowID, req.Credential)
	if err != nil {
		wc.respondWebAuthnError(c, err)
		return
	}

	issueAndRespondFirstPartyUserTokens(c, user.UUID, "WebAuthn login successful", gin.H{
		"uid":      user.UID,
		"email":    user.Email,
		"username": user.Username,
	})
}

func (wc *WebAuthnController) BeginSecondFactor(c *gin.Context) {
	var req WebAuthnSecondFactorBeginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}
	if req.LoginTicket == "" {
		respondError(c, http.StatusBadRequest, CodeLoginTicketRequired, "Missing login_ticket")
		return
	}

	ticket, err := loadLoginTicket(req.LoginTicket)
	if err != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidLoginTicket, "Invalid or expired login ticket")
		return
	}

	var user models.User
	if err := database.DB.Where("uuid = ?", ticket.UserID).First(&user).Error; err != nil {
		respondError(c, http.StatusUnauthorized, CodeInvalidLoginTicket, "Invalid or expired login ticket")
		return
	}
	if !user.WebAuthn2FAEnabled {
		respondError(c, http.StatusBadRequest, CodeWebAuthnNotConfigured, "WebAuthn 2FA not configured")
		return
	}

	options, flowID, err := wc.service.BeginSecondFactorLogin(user.UUID, req.LoginTicket)
	if err != nil {
		wc.respondWebAuthnError(c, err)
		return
	}

	respondOK(c, "WebAuthn second factor started", gin.H{
		"flow_id": flowID,
		"options": options,
	})
}

func (wc *WebAuthnController) FinishSecondFactor(c *gin.Context) {
	var req WebAuthnFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}
	if req.FlowID == "" || len(req.Credential) == 0 {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Missing flow_id or credential")
		return
	}

	user, loginTicket, err := wc.service.FinishSecondFactor(req.FlowID, req.Credential)
	if err != nil {
		wc.respondWebAuthnError(c, err)
		return
	}

	if err := deleteLoginTicket(loginTicket); err != nil {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to consume login ticket")
		return
	}
	issueAndRespondFirstPartyUserTokens(c, user.UUID, "WebAuthn verification successful", nil)
}

func (wc *WebAuthnController) respondWebAuthnError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrWebAuthnNotConfigured):
		respondError(c, http.StatusBadRequest, CodeWebAuthnNotConfigured, "WebAuthn not configured")
	case errors.Is(err, services.ErrWebAuthnInvalidFlow):
		respondError(c, http.StatusUnauthorized, CodeInvalidWebAuthnFlow, "Invalid or expired WebAuthn flow")
	case errors.Is(err, services.ErrWebAuthnCredentialNotFound):
		respondError(c, http.StatusNotFound, CodeWebAuthnCredentialNotFound, "WebAuthn credential not found")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respondError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
	default:
		respondError(c, http.StatusUnauthorized, CodeWebAuthnVerificationFailed, err.Error())
	}
}

func loadLoginTicket(ticket string) (*loginTicketPayload, error) {
	raw, err := appredis.Client.Get(context.Background(), config.AppConfig.Redis.Prefix+"oauth2:login_ticket:"+ticket).Result()
	if err != nil {
		return nil, err
	}

	var payload loginTicketPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.UserID == "" {
		return nil, errors.New("invalid login ticket")
	}
	return &payload, nil
}

func deleteLoginTicket(ticket string) error {
	return appredis.Client.Del(context.Background(), config.AppConfig.Redis.Prefix+"oauth2:login_ticket:"+ticket).Err()
}
