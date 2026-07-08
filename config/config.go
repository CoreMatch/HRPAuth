package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Version          string
	Site             SiteConfig
	Server           ServerRuntimeConfig
	Security         SecurityConfig
	Callback         CallbackConfig
	Frontend         FrontendConfig
	KeyGen           KeyGenConfig
	Database         DatabaseConfig
	VerificationCode VerificationCodeConfig
	Redis            RedisConfig
	SMTP             SMTPConfig
	Manage           ManageConfig
	Yggdrasil        YggdrasilConfig
}

type ServerRuntimeConfig struct {
	Port       string
	CORSOrigin string
}

type SiteConfig struct {
	Name           string
	Implementation string
	Version        string
}

type CallbackConfig struct {
	URL string
}

type FrontendConfig struct {
	URL string
}

type KeyGenConfig struct {
	Enable int
}

type DatabaseConfig struct {
	Host     string
	DBName   string
	User     string
	Password string
	Charset  string
}

type VerificationCodeConfig struct {
	CodeTTL    int
	StorageDir string
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	Prefix   string
}

type SMTPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string
	FromEmail  string
	FromName   string
}

// ManageConfig holds the operator-level "Manage Token" (M-T) which is treated
// as a remtoken (remember_token) by every site endpoint that accepts one.
// Generated randomly on first startup and persisted in config.yaml.
type ManageConfig struct {
	Token string
}

type YggdrasilConfig struct {
	Server       ServerConfig
	Security     YggdrasilSecurityConfig
	FeatureFlags FeatureFlagsConfig
}

type ServerConfig struct {
	Name                    string
	Implementation          string
	Version                 string
	Links                   LinksConfig
	SkinDomains             []string
	SignaturePublicKeyPath  string
	SignaturePrivateKeyPath string
	SignaturePublicKey      string
	SignaturePrivateKey     string
	TexturesStorage         string
}

type LinksConfig struct {
	Homepage string
	Register string
}

// SecurityConfig is the HRPAuth-specific security settings (registration/login UX).
// All Yggdrasil-protocol-related security settings live in YggdrasilSecurityConfig.
type SecurityConfig struct {
	PasswordCost         int
	RateLimitMaxAttempts int
	RateLimitWindowSec   int
	EnableCaptcha        bool
	CaptchaTTL           int
}

// YggdrasilSecurityConfig is the Yggdrasil-protocol-related security settings
// (auth flow durations, texture limits). No HRPAuth-specific fields allowed here.
type YggdrasilSecurityConfig struct {
	TokenExpiryDays      int
	SessionExpirySeconds int
	MaxTextureWidth      int
	MaxTextureHeight     int
}

type FeatureFlagsConfig struct {
	NonEmailLogin            bool
	LegacySkinAPI            bool
	NoMojangNamespace        bool
	EnableMojangAntiFeatures bool
	EnableProfileKey         bool
	UsernameCheck            bool
}

const ConfigFileName = "config.yaml"
const ConfigFileDir = "./"
const ConfigVersion = "3"

var AppConfig *Config

func Load() {
	configPath := filepath.Join(ConfigFileDir, ConfigFileName)

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read config file %s: %v", configPath, err)
	}

	// Parse YAML
	var yamlConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	// Validate config version
	configVersion := getString(yamlConfig, "version")
	if configVersion == "" {
		log.Fatalf("Config file is missing version field")
	}
	if configVersion != ConfigVersion {
		log.Printf("Warning: Config file version %s does not match expected version %s", configVersion, ConfigVersion)
	}

	// Map YAML to Config struct
	AppConfig = &Config{
		Version:          getString(yamlConfig, "version"),
		Site:             parseSiteConfig(yamlConfig),
		Server:           parseServerRuntimeConfig(yamlConfig),
		Security:         parseSecurityConfig(yamlConfig),
		Callback:         parseCallbackConfig(yamlConfig),
		Frontend:         parseFrontendConfig(yamlConfig),
		KeyGen:           parseKeyGenConfig(yamlConfig),
		Database:         parseDatabaseConfig(yamlConfig),
		VerificationCode: parseVerificationCodeConfig(yamlConfig),
		Redis:            parseRedisConfig(yamlConfig),
		SMTP:             parseSMTPConfig(yamlConfig),
		Manage:           parseManageConfig(yamlConfig),
		Yggdrasil:        parseYggdrasilConfig(yamlConfig),
	}

	log.Println("Configuration loaded successfully")
}

func parseSiteConfig(config map[string]interface{}) SiteConfig {
	site, _ := config["site"].(map[string]interface{})
	return SiteConfig{
		Name:           getString(site, "name"),
		Implementation: getString(site, "implementation"),
		Version:        getString(site, "version"),
	}
}

func parseServerRuntimeConfig(config map[string]interface{}) ServerRuntimeConfig {
	server, _ := config["server"].(map[string]interface{})
	return ServerRuntimeConfig{
		Port:       getString(server, "port"),
		CORSOrigin: getString(server, "cors_origin"),
	}
}

func parseCallbackConfig(config map[string]interface{}) CallbackConfig {
	callback, _ := config["callback"].(map[string]interface{})
	return CallbackConfig{
		URL: getString(callback, "url"),
	}
}

func parseFrontendConfig(config map[string]interface{}) FrontendConfig {
	frontend, _ := config["frontend"].(map[string]interface{})
	return FrontendConfig{
		URL: getString(frontend, "url"),
	}
}

func parseKeyGenConfig(config map[string]interface{}) KeyGenConfig {
	keygen, _ := config["keygen"].(map[string]interface{})
	return KeyGenConfig{
		Enable: getInt(keygen, "enable"),
	}
}

func parseDatabaseConfig(config map[string]interface{}) DatabaseConfig {
	db, _ := config["database"].(map[string]interface{})
	return DatabaseConfig{
		Host:     getString(db, "host"),
		DBName:   getString(db, "db_name"),
		User:     getString(db, "user"),
		Password: getString(db, "password"),
		Charset:  getString(db, "charset"),
	}
}

func parseVerificationCodeConfig(config map[string]interface{}) VerificationCodeConfig {
	vc, _ := config["verification_code"].(map[string]interface{})
	return VerificationCodeConfig{
		CodeTTL:    getInt(vc, "code_ttl"),
		StorageDir: getString(vc, "storage_dir"),
	}
}

func parseRedisConfig(config map[string]interface{}) RedisConfig {
	redis, _ := config["redis"].(map[string]interface{})
	return RedisConfig{
		Host:     getString(redis, "host"),
		Port:     getInt(redis, "port"),
		Password: getString(redis, "password"),
		DB:       getInt(redis, "db"),
		Prefix:   getString(redis, "prefix"),
	}
}

func parseSMTPConfig(config map[string]interface{}) SMTPConfig {
	smtp, _ := config["smtp"].(map[string]interface{})
	return SMTPConfig{
		Host:       getString(smtp, "host"),
		Port:       getInt(smtp, "port"),
		Username:   getString(smtp, "username"),
		Password:   getString(smtp, "password"),
		Encryption: getString(smtp, "encryption"),
		FromEmail:  getString(smtp, "from_email"),
		FromName:   getString(smtp, "from_name"),
	}
}

func parseManageConfig(config map[string]interface{}) ManageConfig {
	manage, _ := config["manage"].(map[string]interface{})
	return ManageConfig{
		Token: getString(manage, "token"),
	}
}

func parseYggdrasilConfig(config map[string]interface{}) YggdrasilConfig {
	yggdrasil, _ := config["yggdrasil"].(map[string]interface{})
	return YggdrasilConfig{
		Server:       parseServerConfig(yggdrasil),
		Security:     parseYggdrasilSecurityConfig(yggdrasil),
		FeatureFlags: parseFeatureFlagsConfig(yggdrasil),
	}
}

func parseServerConfig(config map[string]interface{}) ServerConfig {
	server, _ := config["server"].(map[string]interface{})
	links, _ := server["links"].(map[string]interface{})
	skinDomains, _ := server["skin_domains"].([]interface{})

	var skinDomainsStr []string
	for _, domain := range skinDomains {
		if str, ok := domain.(string); ok {
			skinDomainsStr = append(skinDomainsStr, str)
		}
	}

	texturesStorage := getString(server, "textures_storage")
	if texturesStorage == "" {
		texturesStorage = "./"
	}

	publicKeyPath := getString(server, "signature_public_key_path")
	privateKeyPath := getString(server, "signature_private_key_path")

	var publicKey, privateKey string
	if publicKeyPath != "" {
		if data, err := os.ReadFile(publicKeyPath); err == nil {
			publicKey = string(data)
		} else {
			log.Printf("Warning: Failed to read public key file %s: %v", publicKeyPath, err)
		}
	}
	if privateKeyPath != "" {
		if data, err := os.ReadFile(privateKeyPath); err == nil {
			privateKey = string(data)
		} else {
			log.Printf("Warning: Failed to read private key file %s: %v", privateKeyPath, err)
		}
	}

	return ServerConfig{
		Name:                    getString(server, "name"),
		Implementation:          getString(server, "implementation"),
		Version:                 getString(server, "version"),
		SignaturePublicKeyPath:  publicKeyPath,
		SignaturePrivateKeyPath: privateKeyPath,
		SignaturePublicKey:      publicKey,
		SignaturePrivateKey:     privateKey,
		Links: LinksConfig{
			Homepage: getString(links, "homepage"),
			Register: getString(links, "register"),
		},
		SkinDomains:     skinDomainsStr,
		TexturesStorage: texturesStorage,
	}
}

// parseSecurityConfig parses the top-level `security` section (HRPAuth-specific).
func parseSecurityConfig(config map[string]interface{}) SecurityConfig {
	security, _ := config["security"].(map[string]interface{})
	maxAttempts := getInt(security, "rate_limit_max_attempts")
	if maxAttempts == 0 {
		maxAttempts = 10
	}
	windowSec := getInt(security, "rate_limit_window_sec")
	if windowSec == 0 {
		windowSec = 600
	}
	captchaTTL := getInt(security, "captcha_ttl")
	if captchaTTL == 0 {
		captchaTTL = 300
	}
	return SecurityConfig{
		PasswordCost:         getInt(security, "password_cost"),
		RateLimitMaxAttempts: maxAttempts,
		RateLimitWindowSec:   windowSec,
		EnableCaptcha:        getBool(security, "enable_captcha"),
		CaptchaTTL:           captchaTTL,
	}
}

// parseYggdrasilSecurityConfig parses the `yggdrasil.security` section (Yggdrasil protocol only).
func parseYggdrasilSecurityConfig(yggdrasilConfig map[string]interface{}) YggdrasilSecurityConfig {
	security, _ := yggdrasilConfig["security"].(map[string]interface{})
	maxTextureWidth := getInt(security, "max_texture_width")
	if maxTextureWidth == 0 {
		maxTextureWidth = 1024
	}
	maxTextureHeight := getInt(security, "max_texture_height")
	if maxTextureHeight == 0 {
		maxTextureHeight = 1024
	}
	return YggdrasilSecurityConfig{
		TokenExpiryDays:      getInt(security, "token_expiry_days"),
		SessionExpirySeconds: getInt(security, "session_expiry_seconds"),
		MaxTextureWidth:      maxTextureWidth,
		MaxTextureHeight:     maxTextureHeight,
	}
}

func parseFeatureFlagsConfig(config map[string]interface{}) FeatureFlagsConfig {
	featureFlags, _ := config["feature_flags"].(map[string]interface{})
	return FeatureFlagsConfig{
		NonEmailLogin:            getBool(featureFlags, "non_email_login"),
		LegacySkinAPI:            getBool(featureFlags, "legacy_skin_api"),
		NoMojangNamespace:        getBool(featureFlags, "no_mojang_namespace"),
		EnableMojangAntiFeatures: getBool(featureFlags, "enable_mojang_anti_features"),
		EnableProfileKey:         getBool(featureFlags, "enable_profile_key"),
		UsernameCheck:            getBool(featureFlags, "username_check"),
	}
}

func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func getBool(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	if value, ok := m[key].(bool); ok {
		return value
	}
	return false
}
