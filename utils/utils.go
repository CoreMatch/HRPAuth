package utils

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func GenerateRandomToken(length int) string {
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

func GenerateUnsignedUUID() string {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(uuid)
}

func GenerateTOTP(secret string, digits int, period int) string {
	counter := time.Now().Unix() / int64(period)
	return computeTOTP(secret, counter, digits)
}

func GenerateTOTPAtCounter(secret string, counter int64, digits int) string {
	return computeTOTP(secret, counter, digits)
}

func computeTOTP(secret string, counter int64, digits int) string {
	secretBytes, err := base32.StdEncoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		secretBytes = []byte(secret)
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := sha1.New()
	mac.Write(secretBytes)
	hash := mac.Sum(buf)

	offset := hash[len(hash)-1] & 0x0F
	truncatedHash := hash[offset : offset+4]

	value := binary.BigEndian.Uint32(truncatedHash)
	value = value & 0x7FFFFFFF

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	otp := value % mod

	return fmt.Sprintf("%0*d", digits, otp)
}

func GenerateTOTPSecret(length int) string {
	chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}

func GenerateClientToken() string {
	return GenerateRandomToken(16)
}

func GenerateAccessToken() string {
	return GenerateRandomToken(32)
}

func ExtractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func CurrentTimestampMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
