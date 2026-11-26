package channels

import (
	"strings"
	"time"
)

const (
	defaultSMTPTimeoutSecs = 10
	minSMTPTimeoutSecs     = 3
	maxSMTPTimeoutSecs     = 30
)

// ChannelGatewayConfig stores SMS, SMTP Email, and ChatOps/WarRoom gateway configuration.
// Sensitive credentials are stored encrypted via secretbox and never returned in plaintext over HTTP.
type ChannelGatewayConfig struct {
	// SMS Gateway
	SMSEnabled             bool   `json:"sms_enabled"`
	SMSProvider            string `json:"sms_provider"` // "" | "aliyun" | "tencent"
	AliyunAccessKeyID      string `json:"aliyun_access_key_id,omitempty"`
	AliyunAccessKeySecret  string `json:"aliyun_access_key_secret,omitempty"`
	AliyunSecretConfigured bool   `json:"aliyun_secret_configured"`
	AliyunSignName         string `json:"aliyun_sign_name,omitempty"`
	AliyunTemplateCode     string `json:"aliyun_template_code,omitempty"`
	AliyunRegion           string `json:"aliyun_region,omitempty"`

	TencentSecretID         string `json:"tencent_secret_id,omitempty"`
	TencentSecretKey        string `json:"tencent_secret_key,omitempty"`
	TencentSecretConfigured bool   `json:"tencent_secret_configured"`
	TencentSdkAppID         string `json:"tencent_sdk_app_id,omitempty"`
	TencentSignName         string `json:"tencent_sign_name,omitempty"`
	TencentTemplateID       string `json:"tencent_template_id,omitempty"`
	TencentRegion           string `json:"tencent_region,omitempty"`

	// SMTP Email Gateway
	EmailEnabled           bool   `json:"email_enabled"`
	SMTPHost               string `json:"smtp_host,omitempty"`
	SMTPPort               int    `json:"smtp_port"`
	SMTPUsername           string `json:"smtp_username,omitempty"`
	SMTPPassword           string `json:"smtp_password,omitempty"`
	SMTPPasswordConfigured bool   `json:"smtp_password_configured"`
	SMTPFrom               string `json:"smtp_from,omitempty"`
	SMTPTLSMode            string `json:"smtp_tls_mode"` // "ssl" | "starttls" | "none"
	SMTPTimeoutSeconds     int    `json:"smtp_timeout_seconds"`

	// ChatOps Callback & War Room Integration
	ChatOpsCallbackToken           string `json:"chatops_callback_token,omitempty"`
	ChatOpsTokenConfigured         bool   `json:"chatops_token_configured"`
	FeishuVerificationToken        string `json:"feishu_verification_token,omitempty"`
	FeishuVerificationConfigured   bool   `json:"feishu_verification_configured"`
	FeishuEncryptKey               string `json:"feishu_encrypt_key,omitempty"`
	FeishuEncryptConfigured        bool   `json:"feishu_encrypt_configured"`
	DingTalkAppSecret              string `json:"dingtalk_app_secret,omitempty"`
	DingTalkSecretConfigured       bool   `json:"dingtalk_secret_configured"`
	WarRoomProvider                string `json:"war_room_provider"` // "" | "feishu" | "webhook"
	FeishuAppID                    string `json:"feishu_app_id,omitempty"`
	FeishuAppSecret                string `json:"feishu_app_secret,omitempty"`
	FeishuAppSecretConfigured      bool   `json:"feishu_app_secret_configured"`
	WarRoomWebhookURL              string `json:"war_room_webhook_url,omitempty"`
	WarRoomWebhookSecret           string `json:"war_room_webhook_secret,omitempty"`
	WarRoomWebhookSecretConfigured bool   `json:"war_room_webhook_secret_configured"`

	CallbackURLs map[string]string `json:"callback_urls,omitempty"`
	UpdatedBy    uint64            `json:"updated_by,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at,omitempty"`
}

// SMSRequest represents an outbound SMS notification request.
type SMSRequest struct {
	PhoneNumber string
	Urgency     string
	Title       string
	IncidentID  uint64
}

// SMSResult describes the outcome of an SMS gateway call.
type SMSResult struct {
	Provider    string `json:"provider"`
	Status      string `json:"status"` // "sent" | "failed"
	RequestID   string `json:"request_id,omitempty"`
	BizID       string `json:"biz_id,omitempty"`
	PhoneNumber string `json:"phone_number"`
	Message     string `json:"message"`
}

// EmailRequest represents an outbound SMTP notification request.
type EmailRequest struct {
	To          string
	Subject     string
	TextBody    string
	HTMLBody    string
	Urgency     string
	IncidentID  uint64
	ActionLinks map[string]string
}

// EmailResult describes the outcome of an SMTP email delivery attempt.
type EmailResult struct {
	Provider string `json:"provider"`
	Status   string `json:"status"` // "sent" | "failed"
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

// WarRoomCreateRequest holds incident metadata for creating an incident collaboration room.
type WarRoomCreateRequest struct {
	IncidentID uint64
	Title      string
	Severity   string
	RuleName   string
	Operator   string
	ConsoleURL string
}

// WarRoomCreateResult captures the created war room location and provider details.
type WarRoomCreateResult struct {
	Provider       string `json:"provider"` // "feishu" | "webhook" | "console"
	ExternalChatID string `json:"external_chat_id,omitempty"`
	RoomName       string `json:"room_name"`
	RoomURL        string `json:"room_url"`
	Message        string `json:"message"`
}

// NormalizeSMTPTimeoutSeconds clamps SMTP dial/send timeout to [3, 30] seconds (default 10s).
func NormalizeSMTPTimeoutSeconds(secs int) int {
	if secs <= 0 {
		return defaultSMTPTimeoutSecs
	}
	if secs < minSMTPTimeoutSecs {
		return minSMTPTimeoutSecs
	}
	if secs > maxSMTPTimeoutSecs {
		return maxSMTPTimeoutSecs
	}
	return secs
}

// NormalizeSMTPTLSMode normalizes TLS mode to "ssl", "starttls", or "none" (default "ssl").
func NormalizeSMTPTLSMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ssl", "smtps", "tls":
		return "ssl"
	case "starttls":
		return "starttls"
	case "none", "plain":
		return "none"
	default:
		return "ssl"
	}
}

// NormalizeSMTPPort returns port if valid [1..65535], otherwise a sensible default matching tlsMode.
func NormalizeSMTPPort(port int, tlsMode string) int {
	if port > 0 && port <= 65535 {
		return port
	}
	switch NormalizeSMTPTLSMode(tlsMode) {
	case "starttls":
		return 587
	case "none":
		return 25
	default:
		return 465
	}
}
