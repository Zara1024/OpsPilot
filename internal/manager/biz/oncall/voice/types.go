package voice

import "time"

// VoiceGatewayConfig represents the configuration for voice alerting providers.
type VoiceGatewayConfig struct {
	Provider string `json:"provider"` // "mock" | "aliyun" | "tencent"

	// Aliyun Dyvmsapi configuration
	AliyunAccessKeyID      string `json:"aliyun_access_key_id"`
	AliyunAccessKeySecret  string `json:"aliyun_access_key_secret"`
	AliyunCalledShowNumber string `json:"aliyun_called_show_number"`
	AliyunTtsCode          string `json:"aliyun_tts_code"`
	AliyunRegion           string `json:"aliyun_region"` // default: "cn-hangzhou"

	// Tencent Cloud VMS configuration
	TencentSecretID        string `json:"tencent_secret_id"`
	TencentSecretKey       string `json:"tencent_secret_key"`
	TencentSdkAppID        string `json:"tencent_sdk_app_id"`
	TencentTemplateID      string `json:"tencent_template_id"`
	TencentCalledShowNumber string `json:"tencent_called_show_number"`
	TencentRegion          string `json:"tencent_region"` // default: "ap-guangzhou"

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

// CallResult represents the execution result of a voice call.
type CallResult struct {
	CallID       string `json:"call_id"`
	Provider     string `json:"provider"` // "mock" | "aliyun" | "tencent"
	Status       string `json:"status"`   // "calling", "answered", "dtmf_confirmed", "failed"
	OutCallID    string `json:"out_call_id,omitempty"`
	PhoneNumber  string `json:"phone_number"`
	TTSContent   string `json:"tts_content"`
	DTMFCode     string `json:"dtmf_code,omitempty"`
	DurationSecs int    `json:"duration_secs"`
	Message      string `json:"message"`
	RawResponse  string `json:"raw_response,omitempty"`
}
