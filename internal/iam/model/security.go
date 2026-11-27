package model

import "time"

// MFA 强制策略枚举。
const (
	MFAEnforceOff    = "off"
	MFAEnforceAdmins = "admins"
	MFAEnforceAll    = "all"
)

// IsValidMFAEnforceMode 判断 MFA 策略值是否合法。
func IsValidMFAEnforceMode(mode string) bool {
	switch mode {
	case MFAEnforceOff, MFAEnforceAdmins, MFAEnforceAll:
		return true
	default:
		return false
	}
}

// SSOProvider 类型枚举。
const (
	SSOTypeOIDC   = "oidc"
	SSOTypeFeishu = "feishu"
	SSOTypeWeCom  = "wecom"
)

// IsValidSSOType 判断 SSO 提供商类型是否支持。
func IsValidSSOType(t string) bool {
	switch t {
	case SSOTypeOIDC, SSOTypeFeishu, SSOTypeWeCom:
		return true
	default:
		return false
	}
}

// LoginAttempt 持久化跨副本共享的登录失败计数，避免单机内存限流在多副本滚动重启后失效。
type LoginAttempt struct {
	ScopeKey        string    `gorm:"primaryKey;type:varchar(191)"`
	Count           int       `gorm:"not null;default:0"`
	WindowStartedAt time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (LoginAttempt) TableName() string { return "iam_login_attempts" }

// UserMFA 保存用户的 TOTP 密文密钥、启用状态、防重放步长及恢复码哈希列表。
type UserMFA struct {
	ID                 uint64     `gorm:"primaryKey;autoIncrement"`
	UserID             uint64     `gorm:"not null;uniqueIndex"`
	SecretEnc          string     `gorm:"type:text;not null"`
	Enabled            bool       `gorm:"not null;default:false;index"`
	RecoveryHashesJSON string     `gorm:"type:text"`
	LastUsedStep       int64      `gorm:"not null;default:0"`
	VerifiedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (UserMFA) TableName() string { return "iam_user_mfa" }

// SSOProvider 存储企业级 SSO（OIDC / 飞书 / 企业微信）配置，ClientSecret 使用 secretbox 加密落库。
type SSOProvider struct {
	ID              uint64  `gorm:"primaryKey;autoIncrement"`
	Slug            string  `gorm:"type:varchar(64);uniqueIndex;not null"`
	Name            string  `gorm:"type:varchar(128);not null"`
	Type            string  `gorm:"type:varchar(32);not null"`
	Enabled         bool    `gorm:"not null;index"`
	IssuerURL       string  `gorm:"type:varchar(512)"`
	AuthURL         string  `gorm:"type:varchar(512)"`
	TokenURL        string  `gorm:"type:varchar(512)"`
	UserInfoURL     string  `gorm:"type:varchar(512)"`
	JWKSURL         string  `gorm:"type:varchar(512)"`
	ClientID        string  `gorm:"type:varchar(255);not null"`
	ClientSecretEnc string  `gorm:"type:text"`
	CorpID          string  `gorm:"type:varchar(128)"`
	AgentID         string  `gorm:"type:varchar(128)"`
	Scopes          string  `gorm:"type:varchar(512)"`
	AutoCreateUser  bool    `gorm:"not null"`
	DefaultRole     string  `gorm:"type:varchar(32);not null;default:user"`
	DefaultOrgID    *uint64 `gorm:"index"`
	AllowedDomains  string  `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (SSOProvider) TableName() string { return "iam_sso_providers" }

// SSOIdentity 记录平台用户与外部 SSO 身份主体（Provider + ExternalSub）的绑定关系。
type SSOIdentity struct {
	ID            uint64     `gorm:"primaryKey;autoIncrement"`
	ProviderID    uint64     `gorm:"not null;uniqueIndex:idx_sso_ident_prov_sub,priority:1"`
	ExternalSub   string     `gorm:"type:varchar(191);not null;uniqueIndex:idx_sso_ident_prov_sub,priority:2"`
	UserID        uint64     `gorm:"not null;index"`
	ExternalEmail string     `gorm:"type:varchar(255)"`
	ExternalName  string     `gorm:"type:varchar(255)"`
	LastLoginAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (SSOIdentity) TableName() string { return "iam_sso_identities" }

// SSOIdentityWithProvider 组合用户 SSO 身份记录及其关联的提供商名称/类型，供 biz/server 层消费而不跨层依赖 data。
type SSOIdentityWithProvider struct {
	SSOIdentity
	ProviderSlug string
	ProviderName string
	ProviderType string
}

// SSOState 持久化 OAuth2/OIDC 一次性 CSRF state 与 PKCE verifier，保证多副本间任意节点均可完成回调。
type SSOState struct {
	State        string    `gorm:"primaryKey;type:varchar(128)"`
	ProviderSlug string    `gorm:"type:varchar(64);not null"`
	CodeVerifier string    `gorm:"type:varchar(128);not null"`
	Nonce        string    `gorm:"type:varchar(128);not null"`
	RedirectTo   string    `gorm:"type:varchar(512)"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time
}

func (SSOState) TableName() string { return "iam_sso_states" }

// AuthPolicy 保存全局登录安全与 MFA 强制策略（单例 row ID=1）。
type AuthPolicy struct {
	ID                      uint64    `gorm:"primaryKey"`
	MFAEnforceMode          string    `gorm:"type:varchar(32);not null;default:off"`
	AllowLocalPasswordLogin bool      `gorm:"not null"`
	LoginIPLimit            int       `gorm:"not null;default:20"`
	LoginIPWindowSec        int       `gorm:"not null;default:300"`
	LoginEmailLimit         int       `gorm:"not null;default:6"`
	LoginEmailWindowSec     int       `gorm:"not null;default:900"`
	UpdatedAt               time.Time
}

func (AuthPolicy) TableName() string { return "iam_auth_policies" }
