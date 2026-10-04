package services

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
)

type EmailService struct{}

func NewEmailService() *EmailService {
	return &EmailService{}
}

func (es *EmailService) SendMail(to, subject, message string) error {
	smtpConfig := config.AppConfig.SMTP

	from := smtpConfig.FromEmail
	fromName := smtpConfig.FromName

	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("=?UTF-8?B?%s?= <%s>", b64Encode(fromName), from)
	headers["To"] = to
	headers["Subject"] = fmt.Sprintf("=?UTF-8?B?%s?=", b64Encode(subject))
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=UTF-8"
	headers["Content-Transfer-Encoding"] = "base64"

	var headerStr string
	for k, v := range headers {
		headerStr += fmt.Sprintf("%s: %s\r\n", k, v)
	}

	body := b64Encode(message)

	fullMessage := headerStr + "\r\n" + wrapBase64(body, 76)

	addr := fmt.Sprintf("%s:%d", smtpConfig.Host, smtpConfig.Port)

	var auth smtp.Auth
	if smtpConfig.Username != "" && smtpConfig.Password != "" {
		auth = smtp.PlainAuth("", smtpConfig.Username, smtpConfig.Password, smtpConfig.Host)
	}

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(fullMessage))
}

func b64Encode(s string) string {
	return base64Encode([]byte(s))
}

func base64Encode(data []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	n := len(data)
	for i := 0; i < n; i += 3 {
		if i+3 <= n {
			b := (uint(data[i]) << 16) | (uint(data[i+1]) << 8) | uint(data[i+2])
			result.WriteByte(table[(b>>18)&0x3F])
			result.WriteByte(table[(b>>12)&0x3F])
			result.WriteByte(table[(b>>6)&0x3F])
			result.WriteByte(table[b&0x3F])
		} else if i+2 <= n {
			b := (uint(data[i]) << 16) | (uint(data[i+1]) << 8)
			result.WriteByte(table[(b>>18)&0x3F])
			result.WriteByte(table[(b>>12)&0x3F])
			result.WriteByte(table[(b>>6)&0x3F])
			result.WriteByte('=')
		} else {
			b := uint(data[i]) << 16
			result.WriteByte(table[(b>>18)&0x3F])
			result.WriteByte(table[(b>>12)&0x3F])
			result.WriteByte('=')
			result.WriteByte('=')
		}
	}
	return result.String()
}

func wrapBase64(s string, lineLen int) string {
	var result strings.Builder
	for i := 0; i < len(s); i += lineLen {
		end := i + lineLen
		if end > len(s) {
			end = len(s)
		}
		result.WriteString(s[i:end])
		result.WriteString("\r\n")
	}
	return result.String()
}

type VerificationCodeStore struct{}

func NewVerificationCodeStore() *VerificationCodeStore {
	return &VerificationCodeStore{}
}

type codeData struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
}

func (vcs *VerificationCodeStore) Store(email, code string) bool {
	storageDir := config.AppConfig.VerificationCode.StorageDir
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return false
	}

	filename := filepath.Join(storageDir, md5Hash(email)+".json")
	data := codeData{
		Code:      code,
		ExpiresAt: time.Now().Unix() + int64(config.AppConfig.VerificationCode.CodeTTL),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return false
	}

	return os.WriteFile(filename, jsonData, 0644) == nil
}

func (vcs *VerificationCodeStore) Get(email string) (string, bool) {
	storageDir := config.AppConfig.VerificationCode.StorageDir
	filename := filepath.Join(storageDir, md5Hash(email)+".json")

	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}

	var cd codeData
	if err := json.Unmarshal(data, &cd); err != nil {
		return "", false
	}

	if cd.ExpiresAt < time.Now().Unix() {
		os.Remove(filename)
		return "", false
	}

	return cd.Code, true
}

func (vcs *VerificationCodeStore) Delete(email string) bool {
	storageDir := config.AppConfig.VerificationCode.StorageDir
	filename := filepath.Join(storageDir, md5Hash(email)+".json")

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return true
	}

	return os.Remove(filename) == nil
}

func (vcs *VerificationCodeStore) GenerateCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

func (vcs *VerificationCodeStore) Generate4DigitCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(10000))
	return fmt.Sprintf("%04d", n.Int64())
}

func md5Hash(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func IsPortOpen(host string, port int) bool {
	timeout := 5 * time.Second
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
