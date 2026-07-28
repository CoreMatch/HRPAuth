package services

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/database"
	"github.com/lnb/HRPAuth-Backend-Go/models"
)

type TextureService struct{}

func NewTextureService() *TextureService {
	return &TextureService{}
}

type TextureData struct {
	Hash      string
	TextureID string
	URL       string
}

type TextureInfo struct {
	URL      string                 `json:"url"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type TexturesPayload struct {
	Timestamp   int64                    `json:"timestamp"`
	ProfileID   string                   `json:"profileId"`
	ProfileName string                   `json:"profileName"`
	Textures    map[string]TextureInfo   `json:"textures"`
}

func (ts *TextureService) ValidateTexture(file io.Reader, textureType string, model string) ([]byte, error) {
	cfg := config.AppConfig.Yggdrasil.Security
	maxWidth := cfg.MaxTextureWidth
	maxHeight := cfg.MaxTextureHeight

	buf := new(bytes.Buffer)
	teeReader := io.TeeReader(file, buf)

	config, format, err := image.DecodeConfig(teeReader)
	if err != nil {
		return nil, fmt.Errorf("invalid image format: %v", err)
	}

	if format != "png" {
		return nil, fmt.Errorf("texture must be PNG format")
	}

	width := config.Width
	height := config.Height

	if width > maxWidth || height > maxHeight {
		return nil, fmt.Errorf("texture size %dx%d exceeds maximum allowed size %dx%d", width, height, maxWidth, maxHeight)
	}

	img, _, err := image.Decode(io.MultiReader(buf, teeReader))
	if err != nil {
		return nil, fmt.Errorf("failed to decode texture: %v", err)
	}

	bounds := img.Bounds()
	actualWidth := bounds.Dx()
	actualHeight := bounds.Dy()

	switch textureType {
	case "skin":
		if !isValidSkinSize(actualWidth, actualHeight) {
			return nil, fmt.Errorf("invalid skin size: %dx%d, must be 64x32 or 64x64", actualWidth, actualHeight)
		}
	case "cape":
		if !isValidCapeSize(actualWidth, actualHeight) {
			return nil, fmt.Errorf("invalid cape size: %dx%d, must be 64x32 or 22x17", actualWidth, actualHeight)
		}
		if actualWidth == 22 && actualHeight == 17 {
			img = resizeCapeToStandard(img)
		}
	default:
		return nil, fmt.Errorf("invalid texture type: %s", textureType)
	}

	resultBuf := new(bytes.Buffer)
	if err := png.Encode(resultBuf, img); err != nil {
		return nil, fmt.Errorf("failed to re-encode texture: %v", err)
	}

	return resultBuf.Bytes(), nil
}

func isValidSkinSize(width, height int) bool {
	return (width == 64 && height == 32) || (width == 64 && height == 64)
}

func isValidCapeSize(width, height int) bool {
	return (width == 64 && height == 32) || (width == 22 && height == 17)
}

func resizeCapeToStandard(img image.Image) image.Image {
	newImg := image.NewRGBA(image.Rect(0, 0, 64, 32))
	draw.Draw(newImg, newImg.Bounds(), image.Transparent, image.Point{}, draw.Src)
	draw.Draw(newImg, img.Bounds(), img, image.Point{}, draw.Src)
	return newImg
}

func (ts *TextureService) CalculateHash(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (ts *TextureService) SaveTexture(data []byte, hash string) error {
	storageDir := config.AppConfig.Yggdrasil.Server.TexturesStorage
	if storageDir == "" {
		storageDir = "./"
	}

	texturesDir := filepath.Join(storageDir, "textures")
	if err := os.MkdirAll(texturesDir, 0755); err != nil {
		return fmt.Errorf("failed to create textures directory: %v", err)
	}

	filePath := filepath.Join(texturesDir, hash)
	return os.WriteFile(filePath, data, 0644)
}

func (ts *TextureService) DeleteTexture(hash string) error {
	storageDir := config.AppConfig.Yggdrasil.Server.TexturesStorage
	if storageDir == "" {
		storageDir = "./"
	}

	filePath := filepath.Join(storageDir, "textures", hash)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete texture file: %v", err)
	}
	return nil
}

func (ts *TextureService) GetTexturePath(hash string) (string, error) {
	storageDir := config.AppConfig.Yggdrasil.Server.TexturesStorage
	if storageDir == "" {
		storageDir = "./"
	}

	filePath := filepath.Join(storageDir, "textures", hash)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("texture not found")
	}
	return filePath, nil
}

func (ts *TextureService) UploadTexture(accessToken, profileID, textureType, model string, fileData []byte) error {
	token := NewAuthService().ValidateToken(accessToken, "")
	if token == nil {
		return fmt.Errorf("invalid access token")
	}

	if !NewAuthService().IsProfileOwnedByUser(profileID, token.UserID) {
		return fmt.Errorf("profile not owned by user")
	}

	validatedData, err := ts.ValidateTexture(strings.NewReader(string(fileData)), textureType, model)
	if err != nil {
		return err
	}

	hash := ts.CalculateHash(validatedData)

	if err := ts.SaveTexture(validatedData, hash); err != nil {
		return err
	}

	callbackURL := config.AppConfig.Callback.URL
	textureURL := strings.TrimRight(callbackURL, "/") + "/textures/" + hash

	if err := ts.UpdateProfileTexture(profileID, textureType, textureURL, model); err != nil {
		return err
	}

	return nil
}

func (ts *TextureService) UpdateProfileTexture(profileID, textureType, textureURL, model string) error {
	var existingProp models.ProfileProperty
	result := database.DB.
		Where("profile_id = ? AND name = ?", profileID, "textures").
		First(&existingProp)

	payload := ts.GenerateTexturesPayload(profileID, textureType, textureURL, model)
	value := base64.StdEncoding.EncodeToString([]byte(payload))

	signature, err := ts.SignTextureValue(value)
	if err != nil {
		return err
	}

	if result.Error != nil {
		prop := models.ProfileProperty{
			ProfileID: profileID,
			Name:      "textures",
			Value:     value,
			Signature: signature,
		}
		if err := database.DB.Create(&prop).Error; err != nil {
			return fmt.Errorf("failed to create profile property: %v", err)
		}
	} else {
		existingProp.Value = value
		existingProp.Signature = signature
		if err := database.DB.Save(&existingProp).Error; err != nil {
			return fmt.Errorf("failed to update profile property: %v", err)
		}
	}

	return nil
}

func (ts *TextureService) GenerateTexturesPayload(profileID, textureType, textureURL, model string) string {
	var props map[string]TextureInfo

	var existingProp models.ProfileProperty
	database.DB.
		Where("profile_id = ? AND name = ?", profileID, "textures").
		First(&existingProp)

	if existingProp.ID != 0 {
		decoded, _ := base64.StdEncoding.DecodeString(existingProp.Value)
		var existingPayload TexturesPayload
		if err := json.Unmarshal(decoded, &existingPayload); err == nil {
			props = existingPayload.Textures
		}
	}

	if props == nil {
		props = make(map[string]TextureInfo)
	}

	metadata := make(map[string]interface{})
	if textureType == "skin" && model != "" {
		metadata["model"] = model
	}

	props[strings.ToUpper(textureType)] = TextureInfo{
		URL:      textureURL,
		Metadata: metadata,
	}

	profile := NewAuthService().GetProfileByID(profileID)
	profileName := ""
	if profile != nil {
		profileName = profile.Name
	}

	payload := TexturesPayload{
		Timestamp:   time.Now().UnixMilli(),
		ProfileID:   profileID,
		ProfileName: profileName,
		Textures:    props,
	}

	data, _ := json.Marshal(payload)
	return string(data)
}

func (ts *TextureService) SignTextureValue(value string) (string, error) {
	privateKeyPEM := config.AppConfig.Yggdrasil.Server.SignaturePrivateKey
	if privateKeyPEM == "" {
		return "", fmt.Errorf("signature private key not configured")
	}

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return "", fmt.Errorf("invalid RSA private key format")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	hashed := sha1.Sum([]byte(value))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA1, hashed[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign texture value: %v", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

func (ts *TextureService) UploadTextureByUser(userID, profileID, textureType, model string, fileData []byte) error {
	if !NewAuthService().IsProfileOwnedByUser(profileID, userID) {
		return fmt.Errorf("profile not owned by user")
	}

	validatedData, err := ts.ValidateTexture(strings.NewReader(string(fileData)), textureType, model)
	if err != nil {
		return err
	}

	hash := ts.CalculateHash(validatedData)

	if err := ts.SaveTexture(validatedData, hash); err != nil {
		return err
	}

	callbackURL := config.AppConfig.Callback.URL
	textureURL := strings.TrimRight(callbackURL, "/") + "/textures/" + hash

	if err := ts.UpdateProfileTexture(profileID, textureType, textureURL, model); err != nil {
		return err
	}

	return nil
}

func (ts *TextureService) RemoveTextureByUser(userID, profileID, textureType string) error {
	if !NewAuthService().IsProfileOwnedByUser(profileID, userID) {
		return fmt.Errorf("profile not owned by user")
	}

	var prop models.ProfileProperty
	result := database.DB.
		Where("profile_id = ? AND name = ?", profileID, "textures").
		First(&prop)

	if result.Error != nil {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(prop.Value)
	if err != nil {
		return fmt.Errorf("failed to decode texture property: %v", err)
	}

	var payload TexturesPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal texture payload: %v", err)
	}

	delete(payload.Textures, strings.ToUpper(textureType))

	if len(payload.Textures) == 0 {
		if err := database.DB.Delete(&prop).Error; err != nil {
			return fmt.Errorf("failed to delete profile property: %v", err)
		}
	} else {
		payload.Timestamp = time.Now().UnixMilli()
		newData, _ := json.Marshal(payload)
		newValue := base64.StdEncoding.EncodeToString(newData)

		signature, err := ts.SignTextureValue(newValue)
		if err != nil {
			return err
		}

		prop.Value = newValue
		prop.Signature = signature
		if err := database.DB.Save(&prop).Error; err != nil {
			return fmt.Errorf("failed to update profile property: %v", err)
		}
	}

	return nil
}

func (ts *TextureService) RemoveTexture(accessToken, profileID, textureType string) error {
	token := NewAuthService().ValidateToken(accessToken, "")
	if token == nil {
		return fmt.Errorf("invalid access token")
	}

	if !NewAuthService().IsProfileOwnedByUser(profileID, token.UserID) {
		return fmt.Errorf("profile not owned by user")
	}

	var prop models.ProfileProperty
	result := database.DB.
		Where("profile_id = ? AND name = ?", profileID, "textures").
		First(&prop)

	if result.Error != nil {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(prop.Value)
	if err != nil {
		return fmt.Errorf("failed to decode texture property: %v", err)
	}

	var payload TexturesPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal texture payload: %v", err)
	}

	delete(payload.Textures, strings.ToUpper(textureType))

	if len(payload.Textures) == 0 {
		if err := database.DB.Delete(&prop).Error; err != nil {
			return fmt.Errorf("failed to delete profile property: %v", err)
		}
	} else {
		payload.Timestamp = time.Now().UnixMilli()
		newData, _ := json.Marshal(payload)
		newValue := base64.StdEncoding.EncodeToString(newData)

		signature, err := ts.SignTextureValue(newValue)
		if err != nil {
			return err
		}

		prop.Value = newValue
		prop.Signature = signature
		if err := database.DB.Save(&prop).Error; err != nil {
			return fmt.Errorf("failed to update profile property: %v", err)
		}
	}

	return nil
}

func (ts *TextureService) GetProfileProperties(profileID string, unsigned bool) ([]models.ProfileProperty, error) {
	var props []models.ProfileProperty
	result := database.DB.
		Where("profile_id = ?", profileID).
		Find(&props)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to get profile properties: %v", result.Error)
	}

	hasUploadable := false
	for _, p := range props {
		if p.Name == "uploadableTextures" {
			hasUploadable = true
			break
		}
	}

	if !hasUploadable {
		uploadable := models.ProfileProperty{
			ProfileID: profileID,
			Name:      "uploadableTextures",
			Value:     "skin,cape",
			Signature: "",
		}
		props = append(props, uploadable)
	}

	if unsigned {
		for i := range props {
			props[i].Signature = ""
		}
	}

	return props, nil
}

func (ts *TextureService) GetTextureByProfile(profileID, textureType string) (*TextureInfo, error) {
	var prop models.ProfileProperty
	result := database.DB.
		Where("profile_id = ? AND name = ?", profileID, "textures").
		First(&prop)

	if result.Error != nil {
		return nil, fmt.Errorf("texture not found")
	}

	decoded, err := base64.StdEncoding.DecodeString(prop.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode texture property: %v", err)
	}

	var payload TexturesPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal texture payload: %v", err)
	}

	textureInfo, ok := payload.Textures[strings.ToUpper(textureType)]
	if !ok {
		return nil, fmt.Errorf("texture type %s not found", textureType)
	}

	return &textureInfo, nil
}

func (ts *TextureService) CheckDownloadPermission(accessToken, profileID string) bool {
	if accessToken == "" {
		return false
	}

	token := NewAuthService().ValidateToken(accessToken, "")
	if token == nil {
		return false
	}

	return token.SelectedProfileID == profileID || NewAuthService().IsProfileOwnedByUser(profileID, token.UserID)
}

func parseBearerToken(authHeader string) string {
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return ""
}

func (ts *TextureService) GetProfileIDFromTextureURL(textureURL string) (string, error) {
	parts := strings.Split(textureURL, "/textures/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid texture URL")
	}

	hash := parts[len(parts)-1]
	if hash == "" {
		return "", fmt.Errorf("texture hash not found")
	}

	var prop models.ProfileProperty
	result := database.DB.
		Where("value LIKE ?", "%"+hash+"%").
		First(&prop)

	if result.Error != nil {
		return "", fmt.Errorf("profile not found for texture")
	}

	return prop.ProfileID, nil
}