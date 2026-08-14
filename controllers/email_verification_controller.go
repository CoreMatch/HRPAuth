package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
	"github.com/lnb/HRPAuth-Backend-Go/services"
)

type EmailVerificationController struct {
	emailService *services.EmailService
	codeStore    *services.VerificationCodeStore
}

func NewEmailVerificationController() *EmailVerificationController {
	return &EmailVerificationController{
		emailService: services.NewEmailService(),
		codeStore:    services.NewVerificationCodeStore(),
	}
}

type EmailVerificationRequest struct {
	Action  string `json:"action"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Message string `json:"message"`
	Email   string `json:"email"`
	Code    string `json:"code"`
}

func (evc *EmailVerificationController) Handle(c *gin.Context) {
	var req EmailVerificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidJSONBody, "Invalid request body")
		return
	}

	switch req.Action {
	case "send-test-email":
		evc.sendTestEmail(c, req)
	case "send-verification-code":
		evc.sendVerificationCode(c, req)
	case "verify-code":
		evc.verifyCode(c, req)
	default:
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Invalid action")
	}
}

func (evc *EmailVerificationController) sendTestEmail(c *gin.Context, req EmailVerificationRequest) {
	to := req.To
	subject := req.Subject
	message := req.Message

	if to == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Recipient email cannot be empty")
		return
	}

	if !isValidEmail(to) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid recipient email format")
		return
	}

	if subject == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Email subject cannot be empty")
		return
	}

	if message == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Email content cannot be empty")
		return
	}

	err := evc.emailService.SendMail(to, subject, message)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeEmailSendFailed, err.Error())
		return
	}

	respondOK(c, "Email sent successfully", gin.H{
		"to":      to,
		"subject": subject,
	})
}

func (evc *EmailVerificationController) sendVerificationCode(c *gin.Context, req EmailVerificationRequest) {
	email := req.Email

	if !isValidEmail(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	existingCode, found := evc.codeStore.Get(email)
	if found && existingCode != "" {
		respondError(c, http.StatusTooManyRequests, CodeVerificationCodeAlreadySent, "Verification code already sent, please wait")
		return
	}

	code := evc.codeStore.GenerateCode()

	if !evc.codeStore.Store(email, code) {
		respondError(c, http.StatusInternalServerError, CodeInternalError, "Failed to store verification code")
		return
	}

	subject := "HRPAuth - Email Verification Code"
	message := "Your verification code is: " + code + "\n\nThe code is valid for 10 minutes. Please complete the verification as soon as possible.\n\nIf you did not request this code, please ignore this email."

	err := evc.emailService.SendMail(email, subject, message)
	if err != nil {
		evc.codeStore.Delete(email)
		respondError(c, http.StatusInternalServerError, CodeEmailSendFailed, err.Error())
		return
	}

	respondOK(c, "Verification code sent successfully", nil)
}

func (evc *EmailVerificationController) verifyCode(c *gin.Context, req EmailVerificationRequest) {
	email := req.Email
	code := req.Code

	if !isValidEmail(email) {
		respondError(c, http.StatusBadRequest, CodeInvalidEmail, "Invalid email")
		return
	}

	if code == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidRequest, "Verification code is required")
		return
	}

	storedCode, found := evc.codeStore.Get(email)
	if !found {
		respondError(c, http.StatusBadRequest, CodeVerificationCodeExpired, "Verification code expired or not found")
		return
	}

	if code != storedCode {
		respondError(c, http.StatusBadRequest, CodeVerificationCodeInvalid, "Invalid verification code")
		return
	}

	evc.codeStore.Delete(email)

	result := database.DB.Model(&models.User{}).
		Where("email = ?", email).
		Update("verified", true)

	if result.Error != nil {
		respondError(c, http.StatusInternalServerError, CodeVerificationStatusUpdateFailed, "Failed to update verification status")
		return
	}

	if result.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, CodeUserNotFound, "User not found or already verified")
		return
	}

	respondOK(c, "Verification successful", nil)
}
