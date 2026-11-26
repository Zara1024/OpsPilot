package oncall

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type testSMSReq struct {
	PhoneNumber string `json:"phone_number"`
	Title       string `json:"title"`
	Urgency     string `json:"urgency"`
}

type testEmailReq struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Urgency string `json:"urgency"`
}

// @Summary Get On-Call SMS, SMTP email, and ChatOps/WarRoom channel configuration
// @Router /api/v1/oncall/channels/config [get]
// @Success 200 {object} channels.ChannelGatewayConfig
func (h *Handler) getChannelConfig(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAuth(w, r)
	if !ok {
		return
	}

	cfg, err := h.store.GetChannelConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}

	isAdmin := c.Role == "admin"
	if isAdmin && cfg.ChatOpsCallbackToken == "" {
		if tok, tokErr := h.store.EnsureChatOpsCallbackToken(r.Context()); tokErr == nil {
			cfg.ChatOpsCallbackToken = tok
		}
	}

	writeJSON(w, http.StatusOK, h.sanitizeChannelConfig(r, cfg, isAdmin))
}

// @Summary Update On-Call SMS, SMTP email, and ChatOps/WarRoom channel configuration
// @Router /api/v1/oncall/channels/config [put]
// @Success 200 {object} map[string]any
func (h *Handler) updateChannelConfig(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req channels.ChannelGatewayConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	smsProvider := strings.ToLower(strings.TrimSpace(req.SMSProvider))
	switch smsProvider {
	case "", "aliyun", "tencent":
		req.SMSProvider = smsProvider
	default:
		writeErr(w, fmt.Errorf("%w: unsupported sms_provider %q", errs.ErrInvalid, req.SMSProvider))
		return
	}

	tlsMode := strings.ToLower(strings.TrimSpace(req.SMTPTLSMode))
	switch tlsMode {
	case "", "ssl", "starttls", "none":
		if tlsMode != "" {
			req.SMTPTLSMode = tlsMode
		}
	default:
		writeErr(w, fmt.Errorf("%w: unsupported smtp_tls_mode %q", errs.ErrInvalid, req.SMTPTLSMode))
		return
	}

	if req.SMTPPort < 0 || req.SMTPPort > 65535 {
		writeErr(w, fmt.Errorf("%w: invalid smtp_port %d", errs.ErrInvalid, req.SMTPPort))
		return
	}

	wrProvider := strings.ToLower(strings.TrimSpace(req.WarRoomProvider))
	switch wrProvider {
	case "", "feishu", "webhook":
		req.WarRoomProvider = wrProvider
	default:
		writeErr(w, fmt.Errorf("%w: unsupported war_room_provider %q", errs.ErrInvalid, req.WarRoomProvider))
		return
	}

	req.SMTPTimeoutSeconds = channels.NormalizeSMTPTimeoutSeconds(req.SMTPTimeoutSeconds)
	req.SMTPTLSMode = channels.NormalizeSMTPTLSMode(req.SMTPTLSMode)
	req.SMTPPort = channels.NormalizeSMTPPort(req.SMTPPort, req.SMTPTLSMode)
	req.UpdatedBy = c.UserID
	req.ChatOpsCallbackToken = ""

	if err := h.store.UpdateChannelConfig(r.Context(), &req); err != nil {
		writeErr(w, err)
		return
	}

	saved, err := h.store.GetChannelConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": h.sanitizeChannelConfig(r, saved, true),
	})
}

// @Summary Rotate the ChatOps callback and quick-action signing token
// @Router /api/v1/oncall/channels/callback-token/rotate [post]
// @Success 200 {object} map[string]any
func (h *Handler) rotateChannelCallbackToken(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	cfg, err := h.store.RotateChatOpsCallbackToken(r.Context(), c.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	slog.Info("oncall chatops callback token rotated", slog.Uint64("admin_id", c.UserID))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": h.sanitizeChannelConfig(r, cfg, true),
	})
}

// @Summary Send a real SMS connectivity test via configured On-Call SMS gateway
// @Router /api/v1/oncall/channels/test-sms [post]
// @Success 200 {object} channels.SMSResult
func (h *Handler) testSMSChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req testSMSReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	usersMap, _ := h.store.GetUsersByIDs(r.Context(), []uint64{c.UserID})
	callerSummary := usersMap[c.UserID]
	callerName := fmt.Sprintf("user-%d", c.UserID)
	if callerSummary != nil {
		if callerSummary.DisplayName != "" {
			callerName = callerSummary.DisplayName
		} else if callerSummary.Email != "" {
			callerName = callerSummary.Email
		}
		if strings.TrimSpace(req.PhoneNumber) == "" {
			req.PhoneNumber = callerSummary.Phone
		}
	}
	if req.Urgency == "" {
		req.Urgency = "high"
	}
	if req.Title == "" {
		req.Title = "OpsPilot 短信网关连通性测试"
	}

	cfg, err := h.store.GetChannelConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	cg := h.channelGateway
	if cg == nil {
		cg = channels.NewChannelGateway()
	}

	res, sendErr := cg.SendSMS(r.Context(), cfg, &channels.SMSRequest{
		PhoneNumber: req.PhoneNumber,
		Urgency:     req.Urgency,
		Title:       req.Title,
	})
	status := model.NotifyStatusSent
	provider := cfg.SMSProvider
	bizID := ""
	msg := ""
	if res != nil {
		if res.Provider != "" {
			provider = res.Provider
		}
		if res.Status != "" {
			status = res.Status
		}
		bizID = res.BizID
		msg = res.Message
	}
	if sendErr != nil {
		status = model.NotifyStatusFailed
		if msg == "" {
			msg = sendErr.Error()
		}
	}
	if provider == "" {
		provider = "unconfigured"
	}

	if logErr := h.store.CreateNotificationLog(r.Context(), &model.NotificationLog{
		UserID:    c.UserID,
		UserName:  callerName,
		Target:    maskPhoneForLog(req.PhoneNumber),
		Channel:   "sms",
		Provider:  provider,
		OutCallID: bizID,
		Status:    status,
		Message:   truncateLogMsg(msg, 500),
		CreatedAt: time.Now(),
	}); logErr != nil {
		slog.Warn("record test sms notification log failed", slog.Uint64("user_id", c.UserID), slog.Any("err", logErr))
	}

	if res != nil {
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeJSON(w, http.StatusOK, &channels.SMSResult{
		Provider:    provider,
		Status:      status,
		PhoneNumber: req.PhoneNumber,
		Message:     msg,
	})
}

// @Summary Send a real SMTP test email via configured On-Call email gateway
// @Router /api/v1/oncall/channels/test-email [post]
// @Success 200 {object} channels.EmailResult
func (h *Handler) testEmailChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req testEmailReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	usersMap, _ := h.store.GetUsersByIDs(r.Context(), []uint64{c.UserID})
	callerSummary := usersMap[c.UserID]
	callerName := fmt.Sprintf("user-%d", c.UserID)
	if callerSummary != nil {
		if callerSummary.DisplayName != "" {
			callerName = callerSummary.DisplayName
		} else if callerSummary.Email != "" {
			callerName = callerSummary.Email
		}
		if strings.TrimSpace(req.To) == "" {
			req.To = callerSummary.Email
		}
	}
	if req.Urgency == "" {
		req.Urgency = "high"
	}
	if req.Subject == "" {
		req.Subject = "[OpsPilot On-Call] 邮件通知网关连通性测试"
	}

	cfg, err := h.store.GetChannelConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	cg := h.channelGateway
	if cg == nil {
		cg = channels.NewChannelGateway()
	}

	actionLinks := map[string]string{}
	baseURL := h.callbackBaseURL(r)
	if baseURL != "" && cfg.ChatOpsCallbackToken != "" {
		actionLinks["控制台查看"] = baseURL + "/oncall?tab=escalation"
		if ackLink := bizoncall.SignQuickActionURL(baseURL, cfg.ChatOpsCallbackToken, 1, "ack", time.Hour); ackLink != "" {
			actionLinks["示例快捷确认"] = ackLink
		}
	}

	res, sendErr := cg.SendEmail(r.Context(), cfg, &channels.EmailRequest{
		To:          req.To,
		Subject:     req.Subject,
		TextBody:    fmt.Sprintf("触发人: %s\n紧急程度: %s\n这是一封来自 OpsPilot On-Call 通道网关的连通性测试邮件。", callerName, req.Urgency),
		Urgency:     req.Urgency,
		ActionLinks: actionLinks,
	})
	status := model.NotifyStatusSent
	provider := "smtp"
	msg := ""
	if res != nil {
		if res.Provider != "" {
			provider = res.Provider
		}
		if res.Status != "" {
			status = res.Status
		}
		msg = res.Message
	}
	if sendErr != nil {
		status = model.NotifyStatusFailed
		if msg == "" {
			msg = sendErr.Error()
		}
	}

	if logErr := h.store.CreateNotificationLog(r.Context(), &model.NotificationLog{
		UserID:    c.UserID,
		UserName:  callerName,
		Target:    req.To,
		Channel:   "email",
		Provider:  provider,
		Status:    status,
		Message:   truncateLogMsg(msg, 500),
		CreatedAt: time.Now(),
	}); logErr != nil {
		slog.Warn("record test email notification log failed", slog.Uint64("user_id", c.UserID), slog.Any("err", logErr))
	}

	if res != nil {
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeJSON(w, http.StatusOK, &channels.EmailResult{
		Provider: provider,
		Status:   status,
		To:       req.To,
		Subject:  req.Subject,
		Message:  msg,
	})
}

func (h *Handler) sanitizeChannelConfig(r *http.Request, cfg *channels.ChannelGatewayConfig, includeCallbackURLs bool) channels.ChannelGatewayConfig {
	if cfg == nil {
		cfg = store.DefaultChannelGatewayConfig()
	}
	out := *cfg
	out.AliyunSecretConfigured = cfg.AliyunAccessKeySecret != ""
	out.TencentSecretConfigured = cfg.TencentSecretKey != ""
	out.SMTPPasswordConfigured = cfg.SMTPPassword != ""
	out.ChatOpsTokenConfigured = cfg.ChatOpsCallbackToken != ""
	out.FeishuVerificationConfigured = cfg.FeishuVerificationToken != ""
	out.FeishuEncryptConfigured = cfg.FeishuEncryptKey != ""
	out.DingTalkSecretConfigured = cfg.DingTalkAppSecret != ""
	out.FeishuAppSecretConfigured = cfg.FeishuAppSecret != ""
	out.WarRoomWebhookSecretConfigured = cfg.WarRoomWebhookSecret != ""

	out.AliyunAccessKeySecret = ""
	out.TencentSecretKey = ""
	out.SMTPPassword = ""
	out.ChatOpsCallbackToken = ""
	out.FeishuVerificationToken = ""
	out.FeishuEncryptKey = ""
	out.DingTalkAppSecret = ""
	out.FeishuAppSecret = ""
	out.WarRoomWebhookSecret = ""
	out.CallbackURLs = nil

	if includeCallbackURLs && cfg.ChatOpsCallbackToken != "" {
		base := h.callbackBaseURL(r)
		escaped := url.QueryEscape(cfg.ChatOpsCallbackToken)
		out.CallbackURLs = map[string]string{
			"feishu":   fmt.Sprintf("%s/api/v1/oncall/chatops/callback/feishu?token=%s", base, escaped),
			"dingtalk": fmt.Sprintf("%s/api/v1/oncall/chatops/callback/dingtalk?token=%s", base, escaped),
		}
	}
	return out
}
