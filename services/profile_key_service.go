package services

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
)

// Profile Key TTL: Mojang uses ~48h validity; refreshedAfter is 8h before
// expiresAt. We mirror those defaults so clients behave identically to a
// Mojang-issued key pair.
const (
	profileKeyValidity = 48 * time.Hour
	profileKeyRefresh  = 40 * time.Hour // refreshedAfter = expiresAt - 8h
)

// ProfileKeyResponse is the response payload for
// POST /minecraftservices/player/certificates. Field names match the Mojang
// API and the authlib-injector compatible re-implementations.
type ProfileKeyResponse struct {
	KeyPair struct {
		PrivateKey string `json:"privateKey"`
		PublicKey  string `json:"publicKey"`
	} `json:"keyPair"`
	PublicKeySignature string `json:"publicKeySignature"`
	PublicKeySignatureV2 string `json:"publicKeySignatureV2"`
	ExpiresAt          string `json:"expiresAt"`
	RefreshedAfter     string `json:"refreshedAfter"`
}

// PublicKeysResponse mirrors Mojang's GET /minecraftservices/publickeys
// payload, which lets clients verify Mojang-issued signatures locally. Only
// profilePropertyKeys is populated here (the Yggdrasil signature key already
// exposed via /); the other arrays are empty so the schema is honest.
type PublicKeysResponse struct {
	PlayerCertificateKeys []PublicKeyEntry `json:"playerCertificateKeys"`
	ProfilePropertyKeys   []PublicKeyEntry `json:"profilePropertyKeys"`
	AuthenticationKeys    []PublicKeyEntry `json:"authenticationKeys"`
}

type PublicKeyEntry struct {
	PublicKey string `json:"publicKey"`
}

// IssuedProfileKey is the in-memory representation of a freshly-issued (or
// re-issued) profile key, ready for both persistence and HTTP response.
type IssuedProfileKey struct {
	PublicKey          string
	PrivateKey         string
	PublicKeySignature string
	ExpiresAt          time.Time
	RefreshedAfter     time.Time
}

type ProfileKeyService struct{}

func NewProfileKeyService() *ProfileKeyService {
	return &ProfileKeyService{}
}

// IssueOrRotate returns the user's existing profile key when it is still
// usable (not expired and past the refreshedAfter window), otherwise
// generates a new 2048-bit RSA key pair, signs the public key with the
// server's Yggdrasil signature private key, persists it and returns the new
// value. Implements authlib-injector recommendation of avoiding key churn
// across sessions.
//
// When forceRotate is true, a fresh key pair is always issued.
func (ps *ProfileKeyService) IssueOrRotate(userID string, forceRotate bool) (*IssuedProfileKey, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id required")
	}

	now := time.Now()

	if !forceRotate {
		var existing models.ProfileKey
		err := database.DB.Where("user_id = ?", userID).First(&existing).Error
		if err == nil {
			// Reuse if still in the refreshed-after window; otherwise rotate
			// proactively (before expiry) so the client gets a fresh window.
			if existing.ExpiresAt.After(now.Add(profileKeyRefresh)) {
				return &IssuedProfileKey{
					PublicKey:          existing.PublicKey,
					PrivateKey:         existing.PrivateKey,
					PublicKeySignature: existing.PublicKeySignature,
					ExpiresAt:          existing.ExpiresAt,
					RefreshedAfter:     existing.RefreshedAfter,
				}, nil
			}
		}
	}

	issued, err := ps.generateAndSign(now)
	if err != nil {
		return nil, err
	}

	if err := ps.persist(userID, issued); err != nil {
		return nil, err
	}
	return issued, nil
}

func (ps *ProfileKeyService) generateAndSign(now time.Time) (*IssuedProfileKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key pair: %v", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&priv.PublicKey),
	})

	expiresAt := now.Add(profileKeyValidity)
	refreshedAfter := now.Add(profileKeyRefresh)

	signature, err := ps.signPublicKey(expiresAt, pubPEM)
	if err != nil {
		return nil, err
	}

	return &IssuedProfileKey{
		PublicKey:          string(pubPEM),
		PrivateKey:         string(privPEM),
		PublicKeySignature: signature,
		ExpiresAt:          expiresAt,
		RefreshedAfter:     refreshedAfter,
	}, nil
}

// signPublicKey produces the authlib-injector compatible signature of
// "<expiresAtMillis><publicKeyPEM>" using the server's existing Yggdrasil
// signature private key (RSA + SHA1, as per the existing texture-signing
// pipeline). Keeping the same key pair and algorithm means the client
// uses the well-known / signaturePublickey already advertised in /.
func (ps *ProfileKeyService) signPublicKey(expiresAt time.Time, publicKeyPEM []byte) (string, error) {
	privPEM := config.AppConfig.Yggdrasil.Server.SignaturePrivateKey
	if privPEM == "" {
		return "", fmt.Errorf("signature private key not configured")
	}

	block, _ := pem.Decode([]byte(privPEM))
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return "", fmt.Errorf("invalid RSA private key format")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	payload := fmt.Sprintf("%d%s", expiresAt.UnixMilli(), string(publicKeyPEM))
	hashed := sha1.Sum([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA1, hashed[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign public key: %v", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

func (ps *ProfileKeyService) persist(userID string, issued *IssuedProfileKey) error {
	row := models.ProfileKey{
		UserID:             userID,
		PublicKey:          issued.PublicKey,
		PrivateKey:         issued.PrivateKey,
		PublicKeySignature: issued.PublicKeySignature,
		ExpiresAt:          issued.ExpiresAt,
		RefreshedAfter:     issued.RefreshedAfter,
	}

	err := database.DB.Where("user_id = ?", userID).
		Assign(row).
		FirstOrCreate(&models.ProfileKey{UserID: userID}).Error
	if err != nil {
		return fmt.Errorf("failed to persist profile key: %v", err)
	}

	return database.DB.Model(&models.ProfileKey{}).
		Where("user_id = ?", userID).
		Updates(map[string]interface{}{
			"public_key":           issued.PublicKey,
			"private_key":          issued.PrivateKey,
			"public_key_signature": issued.PublicKeySignature,
			"expires_at":           issued.ExpiresAt,
			"refreshed_after":      issued.RefreshedAfter,
		}).Error
}

// GetByUserID returns the persisted profile key for a user without
// triggering rotation. Used to answer repeat certificates requests from
// already-issued clients.
func (ps *ProfileKeyService) GetByUserID(userID string) (*models.ProfileKey, error) {
	var row models.ProfileKey
	if err := database.DB.Where("user_id = ?", userID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// BuildResponse converts an IssuedProfileKey (or persisted row) into the
// JSON shape Mojang clients expect.
func (ps *ProfileKeyService) BuildResponse(issued *IssuedProfileKey) ProfileKeyResponse {
	var resp ProfileKeyResponse
	resp.KeyPair.PrivateKey = issued.PrivateKey
	resp.KeyPair.PublicKey = issued.PublicKey
	resp.PublicKeySignature = issued.PublicKeySignature
	resp.PublicKeySignatureV2 = issued.PublicKeySignature
	resp.ExpiresAt = issued.ExpiresAt.UTC().Format(time.RFC3339)
	resp.RefreshedAfter = issued.RefreshedAfter.UTC().Format(time.RFC3339)
	return resp
}

// CleanupExpired deletes profile keys whose expires_at is in the past. Runs
// on a periodic background sweep (controller hook); safe to call repeatedly.
func (ps *ProfileKeyService) CleanupExpired() int64 {
	now := time.Now()
	res := database.DB.Where("expires_at < ?", now).Delete(&models.ProfileKey{})
	if res.Error != nil {
		log.Printf("[profile-key] cleanup error: %v", res.Error)
		return 0
	}
	return res.RowsAffected
}

// BuildPublicKeysResponse serializes the current signature public key into
// the same envelope Mojang publishes via /minecraftservices/publickeys. The
// public key is DER (SubjectPublicKeyInfo) and base64-encoded, per Mojang.
func (ps *ProfileKeyService) BuildPublicKeysResponse() PublicKeysResponse {
	resp := PublicKeysResponse{
		PlayerCertificateKeys: []PublicKeyEntry{},
		ProfilePropertyKeys:   []PublicKeyEntry{},
		AuthenticationKeys:    []PublicKeyEntry{},
	}

	pubPEM := config.AppConfig.Yggdrasil.Server.SignaturePublicKey
	if pubPEM == "" {
		return resp
	}

	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		return resp
	}

	pub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		log.Printf("[profile-key] publickeys parse error: %v", err)
		return resp
	}

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		log.Printf("[profile-key] publickeys marshal error: %v", err)
		return resp
	}

	entry := PublicKeyEntry{PublicKey: base64.StdEncoding.EncodeToString(der)}
	// The signaturePublickey advertised in / covers profile property
	// signatures; Mojang puts the same key in all three buckets.
	resp.ProfilePropertyKeys = append(resp.ProfilePropertyKeys, entry)
	resp.PlayerCertificateKeys = append(resp.PlayerCertificateKeys, entry)
	resp.AuthenticationKeys = append(resp.AuthenticationKeys, entry)
	return resp
}