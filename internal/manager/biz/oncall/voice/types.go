package voice

import "time"

// VoiceGatewayConfig represents the configuration for voice alerting providers and on-call dispatch.
type VoiceGatewayConfig struct {
	Provider                  string `json:"provider"` // "mock" | "aliyun" | "tencent"
	FallbackProvider          string `json:"fallback_provider"`
	FailoverTimeoutSeconds    int    `json:"failover_timeout_seconds"`
	DispatchEnabled           bool   `json:"dispatch_enabled"`
	VoiceRedialMaxAttempts    int    `json:"voice_redial_max_attempts"`
	VoiceRedialDelaySeconds   int    `json:"voice_redial_delay_seconds"`
	VoiceFastEscalateOnReject bool   `json:"voice_fast_escalate_on_reject"`
	VoiceRedialOnNoDTMF       bool   `json:"voice_redial_on_no_dtmf"`

	// Aliyun Dyvmsapi configuration
	AliyunAccessKeyID      string `json:"aliyun_access_key_id"`
	AliyunAccessKeySecret  string `json:"aliyun_access_key_secret,omitempty"`
	AliyunSecretConfigured bool   `json:"aliyun_secret_configured"`
	AliyunCalledShowNumber string `json:"aliyun_called_show_number"`
	AliyunTtsCode          string `json:"aliyun_tts_code"`
	AliyunRegion           string `json:"aliyun_region"` // default: "cn-hangzhou"

	// Tencent Cloud VMS configuration
	TencentSecretID         string `json:"tencent_secret_id"`
	TencentSecretKey        string `json:"tencent_secret_key,omitempty"`
	TencentSecretConfigured bool   `json:"tencent_secret_configured"`
	TencentSdkAppID         string `json:"tencent_sdk_app_id"`
	TencentTemplateID       string `json:"tencent_template_id"`
	TencentCalledShowNumber string `json:"tencent_called_show_number"`
	TencentRegion           string `json:"tencent_region"` // default: "ap-guangzhou"

	// WebhookToken authenticates provider callback requests. Never returned in plain GET config outside signed callback_urls.
	WebhookToken string            `json:"webhook_token,omitempty"`
	CallbackURLs map[string]string `json:"callback_urls,omitempty"`

	UpdatedBy uint64    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CallRequest represents an outgoing voice notification request.
type CallRequest struct {
	CallID      string            `json:"call_id"`
	PhoneNumber string            `json:"phone_number"`
	Urgency     string            `json:"urgency"` // "high" | "low"
	Title       string            `json:"title"`
	Severity    string            `json:"severity"` // "P0", "P1", "P2"
	Params      map[string]string `json:"params,omitempty"`
	ForceMock   bool              `json:"force_mock,omitempty"`
}

// CallAttempt records an individual provider attempt during primary/fallback execution.
type CallAttempt struct {
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	OutCallID string `json:"out_call_id,omitempty"`
	Message   string `json:"message"`
}

// CallResult represents the execution result of a voice call.
type CallResult struct {
	CallID       string        `json:"call_id"`
	Provider     string        `json:"provider"` // "mock" | "aliyun" | "tencent"
	Status       string        `json:"status"`   // "calling", "answered", "dtmf_confirmed", "throttled", "failed"
	OutCallID    string        `json:"out_call_id,omitempty"`
	PhoneNumber  string        `json:"phone_number"`
	TTSContent   string        `json:"tts_content"`
	DTMFCode     string        `json:"dtmf_code,omitempty"`
	DurationSecs int           `json:"duration_secs"`
	Message      string        `json:"message"`
	RawResponse  string        `json:"raw_response,omitempty"`
	FallbackFrom string        `json:"fallback_from,omitempty"`
	PrimaryError string        `json:"primary_error,omitempty"`
	Attempts     []CallAttempt `json:"attempts,omitempty"`
}
