package config

import (
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Version          string
	Site             SiteConfig
	Server           ServerRuntimeConfig
	Security         SecurityConfig
	WebAuthn         WebAuthnConfig
	OAuth2           OAuth2Config
	Callback         CallbackConfig
	Frontend         FrontendConfig
	Database         DatabaseConfig
	VerificationCode VerificationCodeConfig
	Redis            RedisConfig
	SMTP             SMTPConfig
	Manage           ManageConfig
	CoreAPI          CoreAPIConfig
	YggdrasilAPI     YggdrasilAPIConfig
	SDKPackages      SDKPackagesConfig
}

// SDKPackagesConfig holds settings for the SDK package broker used by the
// WebUI SDK handler (HA-WebUI-SDKHandler).
type SDKPackagesConfig struct {
	StorageDir     string
	MaxPackageSize int
	MaxPackages    int
}

type CoreAPIConfig struct {
}

type YggdrasilAPIConfig struct {
	BaseURL string
}

func parseYggdrasilAPIConfig(config map[string]interface{}) YggdrasilAPIConfig {
	ygg, _ := config["yggdrasil_api"].(map[string]interface{})
	return YggdrasilAPIConfig{
		BaseURL: getString(ygg, "base_url"),
	}
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

type DatabaseConfig struct {
	Host              string
	DBName            string
	User              string
	Password          string
	Charset           string
	Port              int
	RetryIntervalSec  int
	MaxRetries        int
	NextRoundDelayMin int
}

// Addr 返回 MySQL DSN 的拨号地址。设置了 Port 时优先使用独立端口字段
// （并剥掉 Host 中可能内联的旧端口）；Port 为 0 时原样返回 Host，
// 兼容既有 "ip:port" 内联写法。
func (c DatabaseConfig) Addr() string {
	if c.Port <= 0 {
		return c.Host
	}
	host := c.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.JoinHostPort(host, strconv.Itoa(c.Port))
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

// SecurityConfig is the HRPAuth-specific security settings (registration/login UX).
type SecurityConfig struct {
	PasswordCost         int
	RateLimitMaxAttempts int
	RateLimitWindowSec   int
	EnableCaptcha        bool
	CaptchaTTL           int
}

type WebAuthnConfig struct {
	RPDisplayName string
	RPID          string
	RPOrigins     []string
	SessionTTL    int
}

type OAuth2Config struct {
	Issuer                 string
	AuthorizationCodeTTL   int
	AccessTokenTTL         int
	RefreshTokenTTL        int
	SuperClientID          string
	SuperClientSecret      string
	SuperClientExtraScopes []string
	PublicClientID         string
	PublicRedirectURIs     []string
	// ServiceClients 是可选的服务客户端种子列表（client_credentials，
	// ops 级服务身份）。启动时幂等确保对应行存在于 oauth2_clients；
	// 空列表 = 不种子任何客户端，行为与旧版本完全一致。
	ServiceClients []ServiceClientConfig
	// ServiceScopeAllowlist 限定各服务通过自助注册端点
	// （POST /oauth/register）申请客户端时可获得的 scope 清单：
	// 服务名 -> 允许的 scope 列表。不在清单内的服务名申请不到任何
	// scope；清单内不属于已知服务 scope 集的条目一律不生效
	// （super 与 db.* 类 scope 永远无法经此途径发行）。
	ServiceScopeAllowlist map[string][]string
}

// ServiceClientConfig 描述一个配置驱动的 OAuth2 服务客户端种子项。
type ServiceClientConfig struct {
	ClientID     string
	ClientSecret string
	Name         string // 可选，缺省回落到 ClientID
}

const ConfigFileName = "config.yaml"
const ConfigFileDir = "./"
const ConfigVersion = "14"

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
		WebAuthn:         parseWebAuthnConfig(yamlConfig),
		OAuth2:           parseOAuth2Config(yamlConfig),
		Callback:         parseCallbackConfig(yamlConfig),
		Frontend:         parseFrontendConfig(yamlConfig),
		Database:         parseDatabaseConfig(yamlConfig),
		VerificationCode: parseVerificationCodeConfig(yamlConfig),
		Redis:            parseRedisConfig(yamlConfig),
		SMTP:             parseSMTPConfig(yamlConfig),
		Manage:           parseManageConfig(yamlConfig),
		CoreAPI:          parseCoreAPIConfig(yamlConfig),
		YggdrasilAPI:     parseYggdrasilAPIConfig(yamlConfig),
		SDKPackages:      parseSDKPackagesConfig(yamlConfig),
	}
}

func parseSDKPackagesConfig(config map[string]interface{}) SDKPackagesConfig {
	sdk, _ := config["sdk_packages"].(map[string]interface{})
	return SDKPackagesConfig{
		StorageDir:     getString(sdk, "storage_dir"),
		MaxPackageSize: getInt(sdk, "max_package_size"),
		MaxPackages:    getInt(sdk, "max_packages"),
	}
}

func parseCoreAPIConfig(config map[string]interface{}) CoreAPIConfig {
	return CoreAPIConfig{}
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

func parseDatabaseConfig(config map[string]interface{}) DatabaseConfig {
	db, _ := config["database"].(map[string]interface{})

	retryInterval := getInt(db, "retry_interval_sec")
	if retryInterval == 0 {
		retryInterval = 3
	}

	nextRoundDelay := getInt(db, "next_round_delay_min")
	// If the key is present but value is 0, nextRoundDelay will be 0.
	// We need to distinguish between "not set" and "set to 0".
	// However, the user said "默认为5分钟，为0则不进行重试".
	// So if it's missing or set to 0, we need to handle it.
	// Actually, if it's missing, getInt returns 0.
	if _, ok := db["next_round_delay_min"]; !ok {
		nextRoundDelay = 5
	}

	maxRetries := getInt(db, "max_retries")
	if _, ok := db["max_retries"]; !ok {
		maxRetries = 5
	}

	return DatabaseConfig{
		Host:              getString(db, "host"),
		DBName:            getString(db, "db_name"),
		User:              getString(db, "user"),
		Password:          getString(db, "password"),
		Charset:           getString(db, "charset"),
		Port:              getInt(db, "port"),
		RetryIntervalSec:  retryInterval,
		MaxRetries:        maxRetries,
		NextRoundDelayMin: nextRoundDelay,
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

func parseWebAuthnConfig(config map[string]interface{}) WebAuthnConfig {
	section, _ := config["webauthn"].(map[string]interface{})

	rpDisplayName := getString(section, "rp_display_name")
	if rpDisplayName == "" {
		rpDisplayName = getString(getMap(config, "site"), "name")
	}

	rpOrigins := normalizeWebAuthnOrigins(getStringSlice(section, "rp_origins"))
	if len(rpOrigins) == 0 {
		rpOrigins = collectDefaultWebAuthnOrigins(config)
	}

	rpID := getString(section, "rp_id")
	if rpID == "" {
		rpID = deriveWebAuthnRPID(rpOrigins)
	}

	sessionTTL := getInt(section, "session_ttl_sec")
	if sessionTTL == 0 {
		sessionTTL = 300
	}

	return WebAuthnConfig{
		RPDisplayName: rpDisplayName,
		RPID:          rpID,
		RPOrigins:     rpOrigins,
		SessionTTL:    sessionTTL,
	}
}

func parseOAuth2Config(config map[string]interface{}) OAuth2Config {
	oauth2, _ := config["oauth2"].(map[string]interface{})

	issuer := getString(oauth2, "issuer")
	if issuer == "" {
		issuer = getString(getMap(config, "callback"), "url")
	}

	authCodeTTL := getInt(oauth2, "authorization_code_ttl_sec")
	if authCodeTTL == 0 {
		authCodeTTL = 300
	}

	accessTokenTTL := getInt(oauth2, "access_token_ttl_sec")
	if accessTokenTTL == 0 {
		accessTokenTTL = 3600
	}

	refreshTokenTTL := getInt(oauth2, "refresh_token_ttl_sec")
	if refreshTokenTTL == 0 {
		refreshTokenTTL = 2592000
	}

	publicRedirectURIs := getStringSlice(oauth2, "public_redirect_uris")
	if len(publicRedirectURIs) == 0 {
		frontendURL := strings.TrimRight(getString(getMap(config, "frontend"), "url"), "/")
		if frontendURL != "" {
			publicRedirectURIs = []string{frontendURL + "/oauth/callback"}
		}
	}

	return OAuth2Config{
		Issuer:                 issuer,
		AuthorizationCodeTTL:   authCodeTTL,
		AccessTokenTTL:         accessTokenTTL,
		RefreshTokenTTL:        refreshTokenTTL,
		SuperClientID:          getString(oauth2, "super_client_id"),
		SuperClientSecret:      getString(oauth2, "super_client_secret"),
		SuperClientExtraScopes: getStringSlice(oauth2, "super_client_extra_scopes"),
		PublicClientID:         getString(oauth2, "public_client_id"),
		PublicRedirectURIs:     publicRedirectURIs,
		ServiceClients:         parseServiceClients(oauth2),
		ServiceScopeAllowlist:  parseServiceScopeAllowlist(oauth2),
	}
}

// parseServiceScopeAllowlist 解析可选的 oauth2.service_scope_allowlist
// （服务名 -> 允许申请的 scope 列表）。缺失或非法时返回空 map，
// 即任何服务都不能通过自助注册获得 scope。
func parseServiceScopeAllowlist(oauth2 map[string]interface{}) map[string][]string {
	raw, _ := oauth2["service_scope_allowlist"].(map[string]interface{})
	out := make(map[string][]string, len(raw))
	for name, value := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		list, _ := value.([]interface{})
		scopes := make([]string, 0, len(list))
		for _, item := range list {
			scope, ok := item.(string)
			if !ok {
				continue
			}
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes = append(scopes, scope)
			}
		}
		out[name] = scopes
	}
	return out
}

// parseServiceClients 解析可选的 oauth2.service_clients 列表。
// 缺失或非法时返回空列表（= 不种子任何客户端，行为与旧版本一致）。
// 每项形如：
//
//   - client_id: "winnerproxy"
//     client_secret: "..."
//     name: "WinnerProxy"   # 可选
func parseServiceClients(oauth2 map[string]interface{}) []ServiceClientConfig {
	raw, _ := oauth2["service_clients"].([]interface{})
	out := make([]ServiceClientConfig, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, ServiceClientConfig{
			ClientID:     getString(entry, "client_id"),
			ClientSecret: getString(entry, "client_secret"),
			Name:         getString(entry, "name"),
		})
	}
	return out
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

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	value, _ := m[key].(map[string]interface{})
	return value
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if m == nil {
		return nil
	}

	raw, exists := m[key]
	if !exists {
		return nil
	}

	switch typed := raw.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func collectDefaultWebAuthnOrigins(config map[string]interface{}) []string {
	candidates := []string{
		getString(getMap(config, "frontend"), "url"),
		getString(getMap(config, "callback"), "url"),
	}
	return normalizeWebAuthnOrigins(candidates)
}

func normalizeWebAuthnOrigins(origins []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(origins))
	for _, origin := range origins {
		origin = normalizeWebAuthnOrigin(origin)
		if origin == "" {
			continue
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		normalized = append(normalized, origin)
	}
	return normalized
}

func normalizeWebAuthnOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(raw, "/")
	}

	return (&url.URL{
		Scheme: strings.ToLower(parsed.Scheme),
		Host:   strings.ToLower(parsed.Host),
	}).String()
}

func deriveWebAuthnRPID(origins []string) string {
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil {
			continue
		}
		host := parsed.Hostname()
		if host != "" {
			return host
		}
	}
	return ""
}
