package models

import (
	"time"
)

type User struct {
	UID           uint       `gorm:"primaryKey;column:uid"`
	UUID          string     `gorm:"type:varchar(32);column:uuid;index:idx_uuid"`
	Email         string     `gorm:"type:varchar(255);column:email"`
	Avatar        string     `gorm:"type:varchar(255);column:avatar"`
	Password      string     `gorm:"type:varchar(255);not null;column:password"`
	IP            string     `gorm:"type:varchar(255);column:ip"`
	Permission    int        `gorm:"default:0;column:permission"`
	LastSignAt    *time.Time `gorm:"column:last_sign_at"`
	RegisterAt    *time.Time `gorm:"column:register_at"`
	Verified      bool       `gorm:"type:tinyint(1);default:0;column:verified"`
	RememberToken string     `gorm:"type:varchar(100);column:remember_token"`
	Username      string     `gorm:"type:varchar(255);column:username"`
	RegIP         string     `gorm:"type:varchar(40);column:regip"`
	TOTP          string     `gorm:"type:varchar(32);column:totp"`
	CBH           bool       `gorm:"type:tinyint(1);not null;default:1;column:cbh"`
	// MBE (Mojang Bind Enabled): when 1, an unbound WebUI user accepts a
	// M.T. /register bind that supplies a mojang_uuid (see Register §3.4 2.a).
	// When 0 (default), colliding Mojang players get 409 (HA priority).
	MBE        bool    `gorm:"type:tinyint(1);not null;default:0;column:mbe"`
	MojangUUID *string `gorm:"type:varchar(32);column:mojang_uuid;uniqueIndex:uk_users_mojang_uuid"`
}

func (User) TableName() string {
	return "users"
}

type Profile struct {
	ID        string    `gorm:"primaryKey;type:varchar(32);column:id"`
	UserID    string    `gorm:"type:varchar(32);column:user_id;index"`
	Name      string    `gorm:"type:varchar(30);column:name"`
	Model     string    `gorm:"type:enum('default','slim');default:'default';column:model"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Profile) TableName() string {
	return "profiles"
}

type ProfileProperty struct {
	ID        int    `gorm:"primaryKey;autoIncrement;column:id"`
	ProfileID string `gorm:"type:varchar(32);column:profile_id;index"`
	Name      string `gorm:"type:varchar(255);column:name"`
	Value     string `gorm:"type:text;column:value"`
	Signature string `gorm:"type:text;column:signature"`
}

func (ProfileProperty) TableName() string {
	return "profile_properties"
}

type Token struct {
	ID                int       `gorm:"primaryKey;autoIncrement;column:id"`
	AccessToken       string    `gorm:"type:varchar(255);uniqueIndex;column:access_token"`
	ClientToken       string    `gorm:"type:varchar(255);index:idx_tokens_client_token;column:client_token"`
	UserID            string    `gorm:"type:varchar(32);column:user_id;index"`
	SelectedProfileID string    `gorm:"type:varchar(32);column:selected_profile_id;index"`
	IssuedAt          int64     `gorm:"type:bigint(20);column:issued_at"`
	ExpiresInDays     int       `gorm:"default:15;column:expires_in_days"`
	State             string    `gorm:"type:enum('valid','temporarily_invalid','invalid');default:'valid';column:state"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (Token) TableName() string {
	return "tokens"
}

type OAuth2Client struct {
	ID            uint      `gorm:"primaryKey;autoIncrement;column:id"`
	ClientID      string    `gorm:"type:varchar(100);uniqueIndex;column:client_id"`
	ClientSecret  string    `gorm:"type:varchar(255);column:client_secret"`
	Name          string    `gorm:"type:varchar(255);column:name"`
	Type          string    `gorm:"type:enum('public','confidential');column:type"`
	GrantTypes    string    `gorm:"type:text;column:grant_types"`
	RedirectURIs  string    `gorm:"type:text;column:redirect_uris"`
	Scopes        string    `gorm:"type:text;column:scopes"`
	IsInternal    bool      `gorm:"type:tinyint(1);default:0;column:is_internal"`
	IsSuper       bool      `gorm:"type:tinyint(1);default:0;column:is_super"`
	IsActive      bool      `gorm:"type:tinyint(1);default:1;column:is_active"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (OAuth2Client) TableName() string {
	return "oauth2_clients"
}

type OAuth2AuthorizationCode struct {
	ID                  uint      `gorm:"primaryKey;autoIncrement;column:id"`
	Code                string    `gorm:"type:varchar(255);uniqueIndex;column:code"`
	ClientID            string    `gorm:"type:varchar(100);column:client_id;index"`
	UserID              string    `gorm:"type:varchar(32);column:user_id;index"`
	RedirectURI         string    `gorm:"type:text;column:redirect_uri"`
	Scopes              string    `gorm:"type:text;column:scopes"`
	CodeChallenge       string    `gorm:"type:varchar(255);column:code_challenge"`
	CodeChallengeMethod string    `gorm:"type:varchar(20);column:code_challenge_method"`
	ExpiresAt           time.Time `gorm:"column:expires_at;index"`
	ConsumedAt          *time.Time `gorm:"column:consumed_at"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (OAuth2AuthorizationCode) TableName() string {
	return "oauth2_authorization_codes"
}

type OAuth2AccessToken struct {
	ID            uint       `gorm:"primaryKey;autoIncrement;column:id"`
	AccessToken   string     `gorm:"type:varchar(255);uniqueIndex;column:access_token"`
	ClientID      string     `gorm:"type:varchar(100);column:client_id;index"`
	UserID        *string    `gorm:"type:varchar(32);column:user_id;index"`
	Scopes        string     `gorm:"type:text;column:scopes"`
	SubjectType   string     `gorm:"type:enum('user','service');column:subject_type"`
	TargetUID     *uint      `gorm:"column:target_uid;index"`
	TargetEmail   *string    `gorm:"type:varchar(255);column:target_email;index"`
	ExpiresAt     time.Time  `gorm:"column:expires_at;index"`
	RevokedAt     *time.Time `gorm:"column:revoked_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (OAuth2AccessToken) TableName() string {
	return "oauth2_access_tokens"
}

type OAuth2RefreshToken struct {
	ID            uint       `gorm:"primaryKey;autoIncrement;column:id"`
	RefreshToken  string     `gorm:"type:varchar(255);uniqueIndex;column:refresh_token"`
	AccessTokenID uint       `gorm:"column:access_token_id;index"`
	ClientID      string     `gorm:"type:varchar(100);column:client_id;index"`
	UserID        string     `gorm:"type:varchar(32);column:user_id;index"`
	Scopes        string     `gorm:"type:text;column:scopes"`
	ExpiresAt     time.Time  `gorm:"column:expires_at;index"`
	RevokedAt     *time.Time `gorm:"column:revoked_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (OAuth2RefreshToken) TableName() string {
	return "oauth2_refresh_tokens"
}

type Session struct {
	ID        int       `gorm:"primaryKey;autoIncrement;column:id"`
	ProfileID string    `gorm:"type:varchar(32);column:profile_id;index"`
	ServerID  string    `gorm:"type:varchar(255);column:server_id;index:idx_sessions_server_id"`
	IP        string    `gorm:"type:varchar(45);column:ip"`
	CreatedAt time.Time `gorm:"column:created_at"`
	ExpiresAt time.Time `gorm:"column:expires_at;index:idx_sessions_expires_at"`
}

func (Session) TableName() string {
	return "sessions"
}
