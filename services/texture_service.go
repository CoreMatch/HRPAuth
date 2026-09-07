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
	Timestamp   int64                  `json:"timestamp"`
	ProfileID   string                 `json:"profileId"`
	ProfileName string                 `json:"profileName"`
	Textures    map[string]TextureInfo `json:"textures"`
}

// TextureValidationResult contains the validated texture data together with
// any notices (informational) or warnings (potential issues) generated during
// validation. Callers should still upload the texture even when notices or
// warnings are present.
type TextureValidationResult struct {
	Data     []byte
	Notices  []string // informational: resolution exceeds limit but ratio is valid
	Warnings []string // potential issue: aspect ratio does not match any standard size
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func (ts *TextureService) ValidateTexture(file io.Reader, textureType string, model string) (*TextureValidationResult, error) {
	cfg := config.AppConfig.Yggdrasil.Security
	maxWidth := cfg.MaxTextureWidth
	maxHeight := cfg.MaxTextureHeight

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read texture data: %v", err)
	}

	if cfg.MaxTextureFileSize > 0 && int64(len(data)) > cfg.MaxTextureFileSize {
		return nil, fmt.Errorf("texture file size %d bytes exceeds maximum allowed size %d bytes", len(data), cfg.MaxTextureFileSize)
	}

	reader := bytes.NewReader(data)

	config, format, err := image.DecodeConfig(reader)
	if err != nil {
		return nil, fmt.Errorf("invalid image format: %v", err)
	}

	if format != "png" {
		return nil, fmt.Errorf("texture must be PNG format")
	}

	width := config.Width
	height := config.Height

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode texture: %v", err)
	}

	bounds := img.Bounds()
	actualWidth := bounds.Dx()
	actualHeight := bounds.Dy()

	result := &TextureValidationResult{}

	switch textureType {
	case "skin":
		if !isValidSkinSize(actualWidth, actualHeight) {
			if isProportional(actualWidth, actualHeight) {
				result.Notices = append(result.Notices,
					fmt.Sprintf("skin size %dx%d exceeds standard size, but has a valid aspect ratio", actualWidth, actualHeight))
			} else {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skin size %dx%d does not match standard proportions", actualWidth, actualHeight))
			}
		}
	case "cape":
		if !isValidCapeSize(actualWidth, actualHeight) {
			if isProportional(actualWidth, actualHeight) {
				result.Notices = append(result.Notices,
					fmt.Sprintf("cape size %dx%d exceeds standard size, but has a valid aspect ratio", actualWidth, actualHeight))
			} else {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("cape size %dx%d does not match standard proportions", actualWidth, actualHeight))
			}
		}
		if actualWidth == 22 && actualHeight == 17 {
			img = resizeCapeToStandard(img)
		}
	default:
		return nil, fmt.Errorf("invalid texture type: %s", textureType)
	}

	if width > maxWidth || height > maxHeight {
		if isProportional(width, height) {
			result.Notices = append(result.Notices,
				fmt.Sprintf("texture resolution %dx%d exceeds limit %dx%d, but has a valid aspect ratio", width, height, maxWidth, maxHeight))
		} else {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("texture resolution %dx%d exceeds limit %dx%d and has non-standard proportions", width, height, maxWidth, maxHeight))
		}
	}

	resultBuf := new(bytes.Buffer)
	if err := png.Encode(resultBuf, img); err != nil {
		return nil, fmt.Errorf("failed to re-encode texture: %v", err)
	}

	result.Data = resultBuf.Bytes()
	return result, nil
}

func isValidSkinSize(width, height int) bool {
	return (width == 64 && height == 32) || (width == 64 && height == 64)
}

func isValidCapeSize(width, height int) bool {
	return (width == 64 && height == 32) || (width == 22 && height == 17)
}

func isProportional(width, height int) bool {
	g := gcd(width, height)
	return (width/g == 2 && height/g == 1) || (width/g == 1 && height/g == 1)
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

func (ts *TextureService) UploadTexture(accessToken, profileID, textureType, model string, fileData []byte) ([]string, error) {
	token := NewAuthService().ValidateToken(accessToken, "")
	if token == nil {
		return nil, fmt.Errorf("invalid access token")
	}

	if !NewAuthService().IsProfileOwnedByUser(profileID, token.UserID) {
		return nil, fmt.Errorf("profile not owned by user")
	}

	validated, err := ts.ValidateTexture(strings.NewReader(string(fileData)), textureType, model)
	if err != nil {
		return nil, err
	}

	hash := ts.CalculateHash(validated.Data)

	if err := ts.SaveTexture(validated.Data, hash); err != nil {
		return nil, err
	}

	callbackURL := config.AppConfig.Callback.URL
	textureURL := strings.TrimRight(callbackURL, "/") + "/textures/" + hash

	if err := ts.UpdateProfileTexture(profileID, textureType, textureURL, model); err != nil {
		return nil, err
	}

	warnings := append(validated.Notices, validated.Warnings...)
	return warnings, nil
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

func (ts *TextureService) UploadTextureByUser(userID, profileID, textureType, model string, fileData []byte) ([]string, error) {
	if !NewAuthService().IsProfileOwnedByUser(profileID, userID) {
		return nil, fmt.Errorf("profile not owned by user")
	}

	validated, err := ts.ValidateTexture(strings.NewReader(string(fileData)), textureType, model)
	if err != nil {
		return nil, err
	}

	hash := ts.CalculateHash(validated.Data)

	if err := ts.SaveTexture(validated.Data, hash); err != nil {
		return nil, err
	}

	callbackURL := config.AppConfig.Callback.URL
	textureURL := strings.TrimRight(callbackURL, "/") + "/textures/" + hash

	if err := ts.UpdateProfileTexture(profileID, textureType, textureURL, model); err != nil {
		return nil, err
	}

	warnings := append(validated.Notices, validated.Warnings...)
	return warnings, nil
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

// GetSkinTextureURLByProfileName resolves the SKIN texture URL stored in a
// profile's textures property and returns the on-disk file path, ready for
// HTTP serving. Used by the legacy skin API
// (GET /skins/MinecraftSkins/{username}.png) which is enabled via the
// feature.legacy_skin_api flag.
func (ts *TextureService) GetSkinTexturePathByProfileName(name string) (string, error) {
	var profile models.Profile
	if err := database.DB.Where("name = ?", name).First(&profile).Error; err != nil {
		return "", fmt.Errorf("profile not found")
	}

	var prop models.ProfileProperty
	if err := database.DB.
		Where("profile_id = ? AND name = ?", profile.ID, "textures").
		First(&prop).Error; err != nil {
		return "", fmt.Errorf("texture not found")
	}

	decoded, err := base64.StdEncoding.DecodeString(prop.Value)
	if err != nil {
		return "", fmt.Errorf("failed to decode texture property: %v", err)
	}

	var payload TexturesPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return "", fmt.Errorf("failed to unmarshal texture payload: %v", err)
	}

	info, ok := payload.Textures["SKIN"]
	if !ok {
		return "", fmt.Errorf("skin not found")
	}

	parts := strings.Split(info.URL, "/textures/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid texture url")
	}
	hash := parts[len(parts)-1]
	if hash == "" {
		return "", fmt.Errorf("invalid texture hash")
	}

	storageDir := config.AppConfig.Yggdrasil.Server.TexturesStorage
	if storageDir == "" {
		storageDir = "./"
	}
	filePath := filepath.Join(storageDir, "textures", hash)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("texture file missing")
	}
	return filePath, nil
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

// TextureCallbackRewriteResult reports the outcome of a callback rewrite run.
type TextureCallbackRewriteResult struct {
	Total       int
	Rewritten   int
	Unchanged   int
	Failed      int
	CallbackURL string
	Errors      []string
}

// RewriteTextureCallbacks rewrites every stored texture URL to the current
// config callback base (config.AppConfig.Callback.URL), preserving the
// /textures/{hash} suffix. Values are re-encoded and re-signed because the
// signature covers the whole base64 value.
//
// When dryRun is true the payloads are only inspected and counted, nothing is
// written to the database.
func (ts *TextureService) RewriteTextureCallbacks(dryRun bool) *TextureCallbackRewriteResult {
	callbackURL := strings.TrimRight(config.AppConfig.Callback.URL, "/")
	result := &TextureCallbackRewriteResult{
		CallbackURL: callbackURL,
		Errors:      []string{},
	}

	var props []models.ProfileProperty
	if err := database.DB.Where("name = ?", "textures").Find(&props).Error; err != nil {
		result.Failed = 1
		result.Errors = append(result.Errors, fmt.Sprintf("failed to query texture properties: %v", err))
		return result
	}
	result.Total = len(props)

	for _, prop := range props {
		changed, err := ts.rewriteTextureProperty(&prop, callbackURL, dryRun)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("profile %s: %v", prop.ProfileID, err))
			continue
		}
		if changed {
			result.Rewritten++
		} else {
			result.Unchanged++
		}
	}

	return result
}

// rewriteTextureProperty rewrites the texture URLs of a single property row.
// It returns whether any URL actually changed and, unless dryRun, persists the
// re-signed value.
func (ts *TextureService) rewriteTextureProperty(prop *models.ProfileProperty, callbackURL string, dryRun bool) (bool, error) {
	decoded, err := base64.StdEncoding.DecodeString(prop.Value)
	if err != nil {
		return false, fmt.Errorf("failed to decode texture property: %v", err)
	}

	var payload TexturesPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return false, fmt.Errorf("failed to unmarshal texture payload: %v", err)
	}

	changed := false
	for textureType, info := range payload.Textures {
		if info.URL == "" {
			continue
		}
		// Keep only the /textures/{hash} suffix and swap the base.
		suffixIdx := strings.LastIndex(info.URL, "/textures/")
		if suffixIdx < 0 {
			// Not a callback-style URL; leave it untouched.
			continue
		}
		newURL := callbackURL + info.URL[suffixIdx:]
		if newURL == info.URL {
			continue
		}
		info.URL = newURL
		payload.Textures[textureType] = info
		changed = true
	}

	if !changed || dryRun {
		return changed, nil
	}

	newData, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal texture payload: %v", err)
	}
	newValue := base64.StdEncoding.EncodeToString(newData)

	signature, err := ts.SignTextureValue(newValue)
	if err != nil {
		return false, fmt.Errorf("failed to sign texture value: %v", err)
	}

	prop.Value = newValue
	prop.Signature = signature
	if err := database.DB.Save(prop).Error; err != nil {
		return false, fmt.Errorf("failed to update profile property: %v", err)
	}

	return true, nil
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
