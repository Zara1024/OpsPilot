package oncall

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type updateVoiceConfigReq struct {
	voice.VoiceGatewayConfig
	DispatchEnabled *bool `json:"dispatch_enabled"`
}

// @Summary Get On-Call voice gateway configuration
// @Router /api/v1/oncall/voice/config [get]
// @Success 200 {object} voice.VoiceGatewayConfig
func (h *Handler) getVoiceConfig(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAuth(w, r)
	if !ok {
		return
	}

	cfg, err := h.store.GetVoiceConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}

	isAdmin := c.Role == "admin"
	if isAdmin && cfg.WebhookToken == "" {
		if tok, tokErr := h.store.EnsureVoiceWebhookToken(r.Context()); tokErr == nil {
			cfg.WebhookToken = tok
		}
	}

	writeJSON(w, http.StatusOK, h.sanitizeVoiceConfig(r, cfg, isAdmin))
}

// @Summary Update On-Call voice gateway configuration
// @Router /api/v1/oncall/voice/config [put]
// @Success 200 {object} map[string]any
func (h *Handler) updateVoiceConfig(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req updateVoiceConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	cfg := req.VoiceGatewayConfig
	existing, err := h.store.GetVoiceConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if existing != nil {
		if req.DispatchEnabled == nil {
			cfg.DispatchEnabled = existing.DispatchEnabled
		} else {
			cfg.DispatchEnabled = *req.DispatchEnabled
		}
		if cfg.AliyunAccessKeySecret == "" || strings.Contains(cfg.AliyunAccessKeySecret, "*") {
			cfg.AliyunAccessKeySecret = existing.AliyunAccessKeySecret
		}
		if cfg.TencentSecretKey == "" || strings.Contains(cfg.TencentSecretKey, "*") {
			cfg.TencentSecretKey = existing.TencentSecretKey
		}
		cfg.WebhookToken = existing.WebhookToken
	} else if req.DispatchEnabled != nil {
		cfg.DispatchEnabled = *req.DispatchEnabled
	} else {
		cfg.DispatchEnabled = true
	}

	if cfg.Provider == "" {
		cfg.Provider = "mock"
	}
	cfg.FallbackProvider = strings.TrimSpace(cfg.FallbackProvider)
	if cfg.FallbackProvider != "" {
		if cfg.FallbackProvider == cfg.Provider || (cfg.FallbackProvider != "aliyun" && cfg.FallbackProvider != "tencent") {
			writeErr(w, fmt.Errorf("%w: fallback_provider must be empty or a different cloud provider (aliyun/tencent)", errs.ErrInvalid))
			return
		}
	}
	cfg.FailoverTimeoutSeconds = voice.NormalizeFailoverTimeoutSeconds(cfg.FailoverTimeoutSeconds)
	if cfg.AliyunRegion == "" {
		cfg.AliyunRegion = "cn-hangzhou"
	}
	if cfg.TencentRegion == "" {
		cfg.TencentRegion = "ap-guangzhou"
	}
	cfg.UpdatedBy = c.UserID

	if err := h.store.UpdateVoiceConfig(r.Context(), &cfg); err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": h.sanitizeVoiceConfig(r, &cfg, true),
	})
}

// @Summary Trigger an On-Call test voice call
// @Router /api/v1/oncall/voice/test-call [post]
// @Success 200 {object} voice.CallResult
func (h *Handler) testVoiceCall(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req voice.CallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	cfg, err := h.store.GetVoiceConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}

	usersMap, _ := h.store.GetUsersByIDs(r.Context(), []uint64{c.UserID})
	callerName := ""
	if u, found := usersMap[c.UserID]; found && u != nil {
		callerName = u.DisplayName
		if callerName == "" {
			callerName = u.Email
		}
		if req.PhoneNumber == "" {
			req.PhoneNumber = u.Phone
		}
	}

	effectiveProvider := cfg.Provider
	if effectiveProvider == "" {
		effectiveProvider = "mock"
	}
	if req.ForceMock {
		effectiveProvider = "mock"
	}
	if req.PhoneNumber == "" && effectiveProvider == "mock" {
		req.PhoneNumber = "13800000000"
	}
	if req.CallID == "" {
		req.CallID = fmt.Sprintf("call_test_%d", time.Now().UnixNano())
	}
	if req.Title == "" {
		req.Title = "核心生产集群负载突增告警"
	}
	if req.Urgency == "" {
		req.Urgency = "high"
	}
	if req.Severity == "" {
		req.Severity = "P1"
	}

	result, callErr := h.voiceGateway.SendVoiceCall(r.Context(), cfg, &req)
	now := time.Now()
	if result != nil && result.FallbackFrom != "" {
		if err := h.store.CreateNotificationLog(r.Context(), &model.NotificationLog{
			UserID:     c.UserID,
			UserName:   callerName,
			Target:     maskPhoneForLog(req.PhoneNumber),
			Channel:    "voice_call",
			Provider:   result.FallbackFrom,
			CallID:     req.CallID,
			Status:     model.NotifyStatusFailed,
			Message:    truncateLogMsg(fmt.Sprintf("[容灾切换至 %s] %s", result.Provider, result.PrimaryError), 500),
			CreatedAt:  now,
		}); err != nil {
			slog.Warn("record primary failover voice log failed", slog.Uint64("user_id", c.UserID), slog.Any("err", err))
		}
	}

	logStatus := model.NotifyStatusCalling
	logProvider := effectiveProvider
	outCallID := ""
	dtmf := ""
	dur := 0
	msg := ""
	if result != nil {
		if result.Status != "" {
			logStatus = result.Status
		}
		if result.Provider != "" {
			logProvider = result.Provider
		}
		outCallID = result.OutCallID
		dtmf = result.DTMFCode
		dur = result.DurationSecs
		msg = result.Message
	}
	if callErr != nil {
		logStatus = model.NotifyStatusFailed
		if msg == "" {
			msg = callErr.Error()
		}
	}

	if err := h.store.CreateNotificationLog(r.Context(), &model.NotificationLog{
		UserID:       c.UserID,
		UserName:     callerName,
		Target:       maskPhoneForLog(req.PhoneNumber),
		Channel:      "voice_call",
		Provider:     logProvider,
		CallID:       req.CallID,
		OutCallID:    outCallID,
		Status:       logStatus,
		DTMFCode:     dtmf,
		DurationSecs: dur,
		Message:      truncateLogMsg(msg, 500),
		CreatedAt:    now,
	}); err != nil {
		slog.Warn("record voice test notification log failed", slog.Uint64("user_id", c.UserID), slog.Any("err", err))
	}

	if callErr != nil {
		if result != nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
		writeJSON(w, http.StatusOK, &voice.CallResult{
			CallID:      req.CallID,
			Provider:    logProvider,
			Status:      model.NotifyStatusFailed,
			PhoneNumber: req.PhoneNumber,
			Message:     callErr.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// @Summary Receive Aliyun voice call status and DTMF callback
// @Router /api/v1/oncall/voice/callback/aliyun [post]
// @Success 200 {object} map[string]any
func (h *Handler) aliyunVoiceCallback(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeVoiceCallback(w, r) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeErr(w, fmt.Errorf("%w: read callback body", errs.ErrInvalid))
		return
	}

	events := parseAliyunCallbackEvents(r, body)
	for _, ev := range events {
		h.applyVoiceCallbackEvent(r, ev)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code": 0, "msg": "success"}`))
}

// @Summary Receive Tencent VMS call status and DTMF callback
// @Router /api/v1/oncall/voice/callback/tencent [post]
// @Success 200 {object} map[string]any
func (h *Handler) tencentVoiceCallback(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeVoiceCallback(w, r) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeErr(w, fmt.Errorf("%w: read callback body", errs.ErrInvalid))
		return
	}

	events := parseTencentCallbackEvents(body)
	for _, ev := range events {
		h.applyVoiceCallbackEvent(r, ev)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"result": 0, "errmsg": "OK"}`))
}

func (h *Handler) authorizeVoiceCallback(w http.ResponseWriter, r *http.Request) bool {
	cfg, err := h.store.GetVoiceConfig(r.Context())
	if err != nil || cfg == nil || cfg.WebhookToken == "" {
		writeErr(w, errs.ErrUnauthorized)
		return false
	}
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = r.Header.Get("X-Voice-Webhook-Token")
	}
	if tok == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.WebhookToken)) != 1 {
		writeErr(w, errs.ErrUnauthorized)
		return false
	}
	return true
}

type parsedVoiceCallback struct {
	OutCallID string
	CallID    string
	DTMF      string
	Duration  int
	Failed    bool
	StatusMsg string
}

func (h *Handler) applyVoiceCallbackEvent(r *http.Request, ev parsedVoiceCallback) {
	if ev.OutCallID == "" && ev.CallID == "" {
		return
	}
	entry, err := h.store.FindNotificationLogByCall(r.Context(), ev.OutCallID, ev.CallID)
	if err != nil || entry == nil {
		return
	}

	if ev.DTMF != "" {
		entry.DTMFCode = ev.DTMF
	}
	if ev.Duration > 0 {
		entry.DurationSecs = ev.Duration
	}

	switch ev.DTMF {
	case "1":
		entry.Status = model.NotifyStatusDTMFConfirmed
		entry.Message = "语音电话按键 '1' 已确认认领接单"
		if err := h.store.UpdateNotificationLog(r.Context(), entry); err != nil {
			slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
		}
		if entry.TaskID > 0 {
			if task, taskErr := h.store.GetEscalationTaskByID(r.Context(), entry.TaskID); taskErr == nil && task != nil {
				if batchIDs := model.ParseMergedIncidentIDs(task); len(batchIDs) > 0 {
					if err := h.store.AckAlertIncidentsBatch(r.Context(), batchIDs, entry.UserID, "语音电话按键 1 确认接单"); err != nil {
						slog.Warn("batch ack grouped incidents from voice callback failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", err))
					}
					return
				}
			}
		}
		if entry.IncidentID > 0 {
			if err := h.store.AckAlertIncident(r.Context(), entry.IncidentID, entry.UserID, "语音电话按键 1 确认接单"); err != nil {
				slog.Warn("ack incident from voice callback failed", slog.Uint64("incident_id", entry.IncidentID), slog.Any("err", err))
			}
		}
	case "2":
		entry.Status = model.NotifyStatusAnswered
		entry.Message = "语音电话按键 '2' 请求立即升级至下一梯队"
		if err := h.store.UpdateNotificationLog(r.Context(), entry); err != nil {
			slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
		}
		if entry.IncidentID > 0 {
			if err := h.store.EscalateAlertIncident(r.Context(), entry.IncidentID, entry.UserID, "语音电话按键 2 请求升级"); err != nil {
				slog.Warn("escalate incident from voice callback failed", slog.Uint64("incident_id", entry.IncidentID), slog.Any("err", err))
			}
			if advanced, err := h.store.TriggerImmediateEscalationByIncident(r.Context(), entry.IncidentID, time.Now().UTC()); err == nil && advanced && h.worker != nil {
				h.worker.Wake()
			}
		}
	default:
		if ev.Failed {
			entry.Status = model.NotifyStatusFailed
		} else if ev.Duration > 0 {
			entry.Status = model.NotifyStatusAnswered
		}
		if ev.StatusMsg != "" {
			entry.Message = truncateLogMsg(ev.StatusMsg, 500)
		}
		if err := h.store.UpdateNotificationLog(r.Context(), entry); err != nil {
			slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
		}
	}
}

func parseAliyunCallbackEvents(r *http.Request, body []byte) []parsedVoiceCallback {
	trimmed := strings.TrimSpace(string(body))
	var out []parsedVoiceCallback
	if strings.HasPrefix(trimmed, "[") {
		var items []map[string]any
		if json.Unmarshal(body, &items) == nil {
			for _, m := range items {
				out = append(out, callbackFromMap(m))
			}
			return out
		}
	}
	if strings.HasPrefix(trimmed, "{") {
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			return []parsedVoiceCallback{callbackFromMap(m)}
		}
	}
	if formVals, err := url.ParseQuery(trimmed); err == nil && len(formVals) > 0 {
		m := make(map[string]any, len(formVals))
		for k, vs := range formVals {
			if len(vs) > 0 {
				m[k] = vs[0]
			}
		}
		return []parsedVoiceCallback{callbackFromMap(m)}
	}
	if err := r.ParseForm(); err == nil && len(r.Form) > 0 {
		m := make(map[string]any, len(r.Form))
		for k, vs := range r.Form {
			if len(vs) > 0 {
				m[k] = vs[0]
			}
		}
		return []parsedVoiceCallback{callbackFromMap(m)}
	}
	return nil
}

func parseTencentCallbackEvents(body []byte) []parsedVoiceCallback {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var items []map[string]any
		if json.Unmarshal(body, &items) == nil {
			out := make([]parsedVoiceCallback, 0, len(items))
			for _, m := range items {
				out = append(out, callbackFromMap(m))
			}
			return out
		}
	}
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		return []parsedVoiceCallback{callbackFromMap(m)}
	}
	return nil
}

func callbackFromMap(m map[string]any) parsedVoiceCallback {
	ev := parsedVoiceCallback{
		OutCallID: firstMapString(m, "call_id", "CallId", "callid"),
		CallID:    firstMapString(m, "out_id", "OutId", "session_context", "SessionContext"),
		DTMF:      firstMapString(m, "dtmf", "digits", "key_press", "press", "Digits"),
		Duration:  firstMapInt(m, "duration", "duration_secs", "Duration"),
		StatusMsg: firstMapString(m, "status_msg", "errmsg", "Result", "status_code"),
	}
	statusCode := firstMapString(m, "status_code", "result", "Result")
	successFlag := firstMapString(m, "success")
	if strings.EqualFold(successFlag, "false") || (statusCode != "" && statusCode != "0" && statusCode != "200000" && strings.ToLower(statusCode) != "ok" && strings.ToLower(statusCode) != "success") {
		if ev.DTMF == "" && ev.Duration == 0 {
			ev.Failed = true
		}
	}
	return ev
}

func firstMapString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstMapInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			case string:
				if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func (h *Handler) sanitizeVoiceConfig(r *http.Request, cfg *voice.VoiceGatewayConfig, includeCallbackURLs bool) voice.VoiceGatewayConfig {
	out := *cfg
	out.AliyunSecretConfigured = cfg.AliyunAccessKeySecret != ""
	out.TencentSecretConfigured = cfg.TencentSecretKey != ""
	out.AliyunAccessKeySecret = ""
	out.TencentSecretKey = ""
	out.WebhookToken = ""
	out.CallbackURLs = nil
	if includeCallbackURLs && cfg.WebhookToken != "" {
		base := h.callbackBaseURL(r)
		out.CallbackURLs = map[string]string{
			"aliyun":  fmt.Sprintf("%s/api/v1/oncall/voice/callback/aliyun?token=%s", base, url.QueryEscape(cfg.WebhookToken)),
			"tencent": fmt.Sprintf("%s/api/v1/oncall/voice/callback/tencent?token=%s", base, url.QueryEscape(cfg.WebhookToken)),
		}
	}
	return out
}

func (h *Handler) callbackBaseURL(r *http.Request) string {
	if base := strings.TrimRight(strings.TrimSpace(h.publicURL), "/"); base != "" {
		return base
	}
	proto := "https"
	if r != nil && r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		proto = "http"
	}
	host := "localhost:8080"
	if r != nil && r.Host != "" {
		host = r.Host
	}
	return fmt.Sprintf("%s://%s", proto, host)
}

func maskPhoneForLog(p string) string {
	p = strings.TrimSpace(p)
	if len(p) < 7 {
		return p
	}
	return p[:3] + "****" + p[len(p)-4:]
}

func truncateLogMsg(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
