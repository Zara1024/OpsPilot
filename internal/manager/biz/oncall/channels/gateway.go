package channels

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// ChannelGateway dispatches real SMS, SMTP Email, and War Room integrations for On-Call escalations and ChatOps.
type ChannelGateway struct {
	httpClient      *http.Client
	aliyunSMSEndpt  string
	tencentSMSEndpt string
	feishuAPIBase   string
}

// NewChannelGateway constructs a ChannelGateway with sane HTTP client defaults.
func NewChannelGateway() *ChannelGateway {
	return &ChannelGateway{
		httpClient: &http.Client{Timeout: 12 * time.Second},
	}
}

// SendSMS dispatches an SMS via Aliyun or Tencent Cloud based on cfg.SMSProvider.
func (g *ChannelGateway) SendSMS(ctx context.Context, cfg *ChannelGatewayConfig, req *SMSRequest) (*SMSResult, error) {
	if req == nil {
		return nil, errors.New("sms request is nil")
	}
	if cfg == nil || !cfg.SMSEnabled {
		return &SMSResult{
			Provider:    "unconfigured",
			Status:      "failed",
			PhoneNumber: req.PhoneNumber,
			Message:     "短信通知网关未启用，请在通道网关设置中开启并配置短信服务商。",
		}, errors.New("sms gateway disabled")
	}

	client := g.client()
	switch strings.ToLower(strings.TrimSpace(cfg.SMSProvider)) {
	case "aliyun":
		return sendAliyunSMS(ctx, client, g.aliyunSMSEndpt, cfg, req)
	case "tencent":
		return sendTencentSMS(ctx, client, g.tencentSMSEndpt, cfg, req)
	default:
		return &SMSResult{
			Provider:    "unconfigured",
			Status:      "failed",
			PhoneNumber: req.PhoneNumber,
			Message:     "未选择短信服务商 (aliyun / tencent)",
		}, errors.New("sms provider not configured")
	}
}

// SendEmail delivers an incident notification email over SMTP with TLS/STARTTLS support.
func (g *ChannelGateway) SendEmail(ctx context.Context, cfg *ChannelGatewayConfig, req *EmailRequest) (*EmailResult, error) {
	if req == nil {
		return nil, errors.New("email request is nil")
	}
	if cfg == nil || !cfg.EmailEnabled {
		return &EmailResult{
			Provider: "unconfigured",
			Status:   "failed",
			To:       req.To,
			Subject:  req.Subject,
			Message:  "邮件通知网关未启用，请在通道网关设置中开启并配置 SMTP 参数。",
		}, errors.New("email gateway disabled")
	}
	return sendSMTPEmail(ctx, cfg, req)
}

// CreateWarRoom creates a real collaboration room via Feishu, Webhook, or OpsPilot console fallback.
func (g *ChannelGateway) CreateWarRoom(ctx context.Context, cfg *ChannelGatewayConfig, req *WarRoomCreateRequest) (*WarRoomCreateResult, error) {
	return createWarRoom(ctx, g.client(), g.feishuAPIBase, cfg, req)
}

func (g *ChannelGateway) client() *http.Client {
	if g != nil && g.httpClient != nil {
		return g.httpClient
	}
	return &http.Client{Timeout: 12 * time.Second}
}

// IsPermanentConfigErr reports whether an error message reflects missing/invalid configuration rather than transient I/O.
func IsPermanentConfigErr(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "未配置") ||
		strings.Contains(lower, "未启用") ||
		strings.Contains(lower, "未选择") ||
		strings.Contains(lower, "not configured") ||
		strings.Contains(lower, "disabled") ||
		strings.Contains(lower, "地址无效") ||
		strings.Contains(lower, "不能为空")
}

func nonEmptyStr(val, fallback string) string {
	val = strings.TrimSpace(val)
	if val != "" {
		return val
	}
	return fallback
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}
