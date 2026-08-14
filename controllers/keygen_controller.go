package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
)

type KeyGenController struct{}

func NewKeyGenController() *KeyGenController {
	return &KeyGenController{}
}

func (kgc *KeyGenController) Generate(c *gin.Context) {
	if config.AppConfig.KeyGen.Enable == 1 {
		respondError(c, http.StatusForbidden, CodeKeygenDisabled, "Key generation endpoint is disabled")
		return
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeKeygenFailed, "Failed to generate key pair")
		return
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		respondError(c, http.StatusInternalServerError, CodeKeygenFailed, "Failed to generate public key")
		return
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	keysDir := "./keys"
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		respondError(c, http.StatusInternalServerError, CodeKeygenFailed, "Failed to create keys directory")
		return
	}

	publicKeyPath := filepath.Join(keysDir, "public.pem")
	privateKeyPath := filepath.Join(keysDir, "private.pem")

	if err := os.WriteFile(publicKeyPath, publicKeyPEM, 0600); err != nil {
		respondError(c, http.StatusInternalServerError, CodeKeygenFailed, "Failed to save public key")
		return
	}

	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		respondError(c, http.StatusInternalServerError, CodeKeygenFailed, "Failed to save private key")
		return
	}

	config.AppConfig.KeyGen.Enable = 1

	respondOK(c, "Key pair generated successfully", gin.H{
		"public_key": string(publicKeyPEM),
	})
}
