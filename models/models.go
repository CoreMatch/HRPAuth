package models

import (
	"time"
)

type User struct {
	UID                uint       `gorm:"primaryKey;column:uid"`
	UUID               string     `gorm:"type:varchar(32);column:uuid;index:idx_uuid"`
	Email              string     `gorm:"type:varchar(255);column:email"`
	Avatar             string     `gorm:"type:varchar(255);column:avatar"`
	Password           string     `gorm:"type:varchar(255);not null;column:password"`
	IP                 string     `gorm:"type:varchar(255);column:ip"`
	Permission         int        `gorm:"default:0;column:permission"`
	LastSignAt         *time.Time `gorm:"column:last_sign_at"`
	RegisterAt         *time.Time `gorm:"column:register_at"`
	Verified           bool       `gorm:"type:tinyint(1);default:0;column:verified"`
	Username           string     `gorm:"type:varchar(255);column:username"`
	RegIP              string     `gorm:"type:varchar(40);column:regip"`
	TOTP               string     `gorm:"type:varchar(32);column:totp"`
	TwoFA              bool       `gorm:"type:tinyint(1);not null;default:0;column:2FA"`
	Email2FAEnabled    bool       `gorm:"type:tinyint(1);not null;default:0;column:email_2fa_enabled"`
	RecoveryKey        string     `gorm:"type:varchar(255);column:recovery_key"`
	RecoveryKeyEnabled bool       `gorm:"type:tinyint(1);not null;default:0;column:recovery_key_enabled"`
	DeletedAt          *time.Time `gorm:"index;column:deleted_at"`
}

func (User) TableName() string {
	return "users"
}

type OAuth2Client struct {
	ID           uint      `gorm:"primaryKey;autoIncrement;column:id"`
	ClientID     string    `gorm:"type:varchar(100);uniqueIndex;column:client_id"`
	ClientSecret string    `gorm:"type:varchar(255);column:client_secret"`
	Name         string    `gorm:"type:varchar(255);column:name"`
	Type         string    `gorm:"type:enum('public','confidential');column:type"`
	GrantTypes   string    `gorm:"type:text;column:grant_types"`
	RedirectURIs string    `gorm:"type:text;column:redirect_uris"`
	Scopes       string    `gorm:"type:text;column:scopes"`
	IsInternal   bool      `gorm:"type:tinyint(1);default:0;column:is_internal"`
	IsSuper      bool      `gorm:"type:tinyint(1);default:0;column:is_super"`
	IsActive     bool      `gorm:"type:tinyint(1);default:1;column:is_active"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (OAuth2Client) TableName() string {
	return "oauth2_clients"
}

type OAuth2AuthorizationCode struct {
	ID                  uint       `gorm:"primaryKey;autoIncrement;column:id"`
	Code                string     `gorm:"type:varchar(255);uniqueIndex;column:code"`
	ClientID            string     `gorm:"type:varchar(100);column:client_id;index"`
	UserID              string     `gorm:"type:varchar(32);column:user_id;index"`
	RedirectURI         string     `gorm:"type:text;column:redirect_uri"`
	Scopes              string     `gorm:"type:text;column:scopes"`
	CodeChallenge       string     `gorm:"type:varchar(255);column:code_challenge"`
	CodeChallengeMethod string     `gorm:"type:varchar(20);column:code_challenge_method"`
	ExpiresAt           time.Time  `gorm:"column:expires_at;index"`
	ConsumedAt          *time.Time `gorm:"column:consumed_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
}

func (OAuth2AuthorizationCode) TableName() string {
	return "oauth2_authorization_codes"
}

type OAuth2AccessToken struct {
	ID          uint       `gorm:"primaryKey;autoIncrement;column:id"`
	AccessToken string     `gorm:"type:varchar(255);uniqueIndex;column:access_token"`
	ClientID    string     `gorm:"type:varchar(100);column:client_id;index"`
	UserID      *string    `gorm:"type:varchar(32);column:user_id;index"`
	Scopes      string     `gorm:"type:text;column:scopes"`
	SubjectType string     `gorm:"type:enum('user','service');column:subject_type"`
	TargetUID   *uint      `gorm:"column:target_uid;index"`
	TargetEmail *string    `gorm:"type:varchar(255);column:target_email;index"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;index"`
	RevokedAt   *time.Time `gorm:"column:revoked_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
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

type WebAuthnCredential struct {
	ID               uint       `gorm:"primaryKey;autoIncrement;column:id"`
	UserID           string     `gorm:"type:varchar(32);column:user_id;index:idx_webauthn_credentials_user_id"`
	Name             string     `gorm:"type:varchar(255);column:name"`
	CredentialID     string     `gorm:"type:varchar(1024);column:credential_id"`
	CredentialIDHash string     `gorm:"type:char(64);column:credential_id_hash;uniqueIndex:uk_webauthn_credentials_credential_id_hash"`
	CredentialJSON   string     `gorm:"type:mediumtext;column:credential_json"`
	LastUsedAt       *time.Time `gorm:"column:last_used_at"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func (WebAuthnCredential) TableName() string {
	return "webauthn_credentials"
}

type DeletedAccount struct {
	UID       uint      `gorm:"primaryKey;column:uid"`
	DeletedAt time.Time `gorm:"column:deleted_at"`
}

func (DeletedAccount) TableName() string {
	return "deleted_accounts"
}
