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
	DispatchEnabled           *bool `json:"dispatch_enabled"`
	VoiceRedialMaxAttempts    *int  `json:"voice_redial_max_attempts"`
	VoiceRedialDelaySeconds   *int  `json:"voice_redial_delay_seconds"`
	VoiceFastEscalateOnReject *bool `json:"voice_fast_escalate_on_reject"`
	VoiceRedialOnNoDTMF       *bool `json:"voice_redial_on_no_dtmf"`
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
		if req.VoiceRedialMaxAttempts == nil {
			cfg.VoiceRedialMaxAttempts = existing.VoiceRedialMaxAttempts
		} else {
			cfg.VoiceRedialMaxAttempts = *req.VoiceRedialMaxAttempts
		}
		if req.VoiceRedialDelaySeconds == nil {
			cfg.VoiceRedialDelaySeconds = existing.VoiceRedialDelaySeconds
		} else {
			cfg.VoiceRedialDelaySeconds = *req.VoiceRedialDelaySeconds
		}
		if req.VoiceFastEscalateOnReject == nil {
			cfg.VoiceFastEscalateOnReject = existing.VoiceFastEscalateOnReject
		} else {
			cfg.VoiceFastEscalateOnReject = *req.VoiceFastEscalateOnReject
		}
		if req.VoiceRedialOnNoDTMF == nil {
			cfg.VoiceRedialOnNoDTMF = existing.VoiceRedialOnNoDTMF
		} else {
			cfg.VoiceRedialOnNoDTMF = *req.VoiceRedialOnNoDTMF
		}
		if cfg.AliyunAccessKeySecret == "" || strings.Contains(cfg.AliyunAccessKeySecret, "*") {
			cfg.AliyunAccessKeySecret = existing.AliyunAccessKeySecret
		}
		if cfg.TencentSecretKey == "" || strings.Contains(cfg.TencentSecretKey, "*") {
			cfg.TencentSecretKey = existing.TencentSecretKey
		}
		cfg.WebhookToken = existing.WebhookToken
	} else {
		if req.DispatchEnabled != nil {
			cfg.DispatchEnabled = *req.DispatchEnabled
		} else {
			cfg.DispatchEnabled = true
		}
		if req.VoiceRedialMaxAttempts != nil {
			cfg.VoiceRedialMaxAttempts = *req.VoiceRedialMaxAttempts
		} else {
			cfg.VoiceRedialMaxAttempts = 1
		}
		if req.VoiceRedialDelaySeconds != nil {
			cfg.VoiceRedialDelaySeconds = *req.VoiceRedialDelaySeconds
		} else {
			cfg.VoiceRedialDelaySeconds = 20
		}
		if req.VoiceFastEscalateOnReject != nil {
			cfg.VoiceFastEscalateOnReject = *req.VoiceFastEscalateOnReject
		} else {
			cfg.VoiceFastEscalateOnReject = true
		}
		if req.VoiceRedialOnNoDTMF != nil {
			cfg.VoiceRedialOnNoDTMF = *req.VoiceRedialOnNoDTMF
		}
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
	cfg.VoiceRedialMaxAttempts = voice.NormalizeVoiceRedialMaxAttempts(cfg.VoiceRedialMaxAttempts)
	cfg.VoiceRedialDelaySeconds = voice.NormalizeVoiceRedialDelaySeconds(cfg.VoiceRedialDelaySeconds)
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

// @Summary Rotate the signed webhook callback token for On-Call voice callbacks
// @Router /api/v1/oncall/voice/callback-token/rotate [post]
// @Success 200 {object} map[string]any
func (h *Handler) rotateVoiceCallbackToken(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	cfg, err := h.store.RotateVoiceWebhookToken(r.Context(), c.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	slog.Info("oncall voice callback token rotated", slog.Uint64("admin_id", c.UserID))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": h.sanitizeVoiceConfig(r, cfg, true),
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
	OutCallID     string
	CallID        string
	DTMF          string
	Duration      int
	Failed        bool
	FailureStatus string
	StatusCode    string
	StatusMsg     string
}

func (h *Handler) applyVoiceCallbackEvent(r *http.Request, ev parsedVoiceCallback) {
	if ev.OutCallID == "" && ev.CallID == "" {
		return
	}
	ctx := r.Context()
	entry, err := h.store.FindNotificationLogByCall(ctx, ev.OutCallID, ev.CallID)
	if err != nil || entry == nil {
		return
	}

	wasDTMFConfirmed := entry.Status == model.NotifyStatusDTMFConfirmed
	wasTerminalFailure := entry.Status == model.NotifyStatusRejected ||
		entry.Status == model.NotifyStatusNoAnswer ||
		entry.Status == model.NotifyStatusUnreachable ||
		entry.Status == model.NotifyStatusFailed
	wasAnswered := entry.Status == model.NotifyStatusAnswered

	// 挂断回调晚于按键认领回调到达时，仅补充通话时长，不降级状态也不触发重拨/快进升级。
	if ev.DTMF == "" && wasDTMFConfirmed {
		if ev.Duration > entry.DurationSecs {
			entry.DurationSecs = ev.Duration
			if err := h.store.UpdateNotificationLog(ctx, entry); err != nil {
				slog.Warn("update duration on confirmed voice log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
			}
		}
		return
	}

	if ev.DTMF != "" {
		entry.DTMFCode = ev.DTMF
	}
	if ev.Duration > 0 {
		entry.DurationSecs = ev.Duration
	}

	callID := entry.CallID
	if callID == "" {
		callID = ev.CallID
	}
	now := time.Now().UTC()

	switch ev.DTMF {
	case "1":
		entry.Status = model.NotifyStatusDTMFConfirmed
		entry.Message = "语音电话按键 '1' 已确认认领接单"
		if err := h.store.UpdateNotificationLog(ctx, entry); err != nil {
			slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
		}
		if entry.TaskID > 0 {
			if task, taskErr := h.store.GetEscalationTaskByID(ctx, entry.TaskID); taskErr == nil && task != nil {
				if batchIDs := model.ParseMergedIncidentIDs(task); len(batchIDs) > 0 {
					if err := h.store.AckAlertIncidentsBatch(ctx, batchIDs, entry.UserID, "语音电话按键 1 确认接单"); err != nil {
						slog.Warn("batch ack grouped incidents from voice callback failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", err))
					}
					return
				}
			}
		}
		if entry.IncidentID > 0 {
			if err := h.store.AckAlertIncident(ctx, entry.IncidentID, entry.UserID, "语音电话按键 1 确认接单"); err != nil {
				slog.Warn("ack incident from voice callback failed", slog.Uint64("incident_id", entry.IncidentID), slog.Any("err", err))
			}
		}
	case "2":
		entry.Status = model.NotifyStatusAnswered
		entry.Message = "语音电话按键 '2' 请求立即升级至下一梯队"
		if err := h.store.UpdateNotificationLog(ctx, entry); err != nil {
			slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
		}
		if entry.IncidentID > 0 {
			if err := h.store.EscalateAlertIncident(ctx, entry.IncidentID, entry.UserID, "语音电话按键 2 请求升级"); err != nil {
				slog.Warn("escalate incident from voice callback failed", slog.Uint64("incident_id", entry.IncidentID), slog.Any("err", err))
			}
			if advanced, err := h.store.TriggerImmediateEscalationByIncident(ctx, entry.IncidentID, now); err == nil && advanced && h.worker != nil {
				h.worker.Wake()
			}
		}
	default:
		if ev.Failed {
			failureStatus := ev.FailureStatus
			if failureStatus == "" {
				failureStatus = model.NotifyStatusFailed
			}
			entry.Status = failureStatus
			if ev.StatusMsg != "" {
				entry.Message = truncateLogMsg(ev.StatusMsg, 500)
			} else if entry.Message == "" {
				entry.Message = defaultVoiceFailureMessage(failureStatus)
			}

			// 幂等保护：同一通电话若已处理过失败/未接终态，重复回调不再重复扣减重拨次数。
			shouldTriggerPolicy := !wasTerminalFailure && entry.TaskID > 0 && callID != ""
			var policySuffix string
			var wakeWorker bool

			if shouldTriggerPolicy {
				cfg := h.loadVoicePolicyConfig(r)
				switch failureStatus {
				case model.NotifyStatusRejected, model.NotifyStatusUnreachable:
					if cfg.VoiceFastEscalateOnReject {
						if nextStep, advanced, escErr := h.store.FastEscalateVoiceCall(ctx, entry.TaskID, callID, entry.EscalationStep, now); escErr != nil {
							slog.Warn("fast escalate voice call failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", escErr))
						} else if advanced {
							policySuffix = fmt.Sprintf("（已触发快进升级至第 %d 级）", nextStep)
							wakeWorker = true
						}
					}
				case model.NotifyStatusNoAnswer:
					maxRedials := voice.NormalizeVoiceRedialMaxAttempts(cfg.VoiceRedialMaxAttempts)
					delaySecs := voice.NormalizeVoiceRedialDelaySeconds(cfg.VoiceRedialDelaySeconds)
					if maxRedials > 0 {
						attempt, scheduled, redialErr := h.store.ScheduleVoiceRedialByCall(ctx, entry.TaskID, callID, maxRedials, now.Add(time.Duration(delaySecs)*time.Second))
						if redialErr != nil {
							slog.Warn("schedule voice redial failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", redialErr))
						} else if scheduled {
							policySuffix = fmt.Sprintf("（%d 秒后自动第 %d 次重拨）", delaySecs, attempt)
							wakeWorker = true
						} else if cfg.VoiceFastEscalateOnReject {
							if nextStep, advanced, escErr := h.store.FastEscalateVoiceCall(ctx, entry.TaskID, callID, entry.EscalationStep, now); escErr != nil {
								slog.Warn("fast escalate after exhausted redials failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", escErr))
							} else if advanced {
								policySuffix = fmt.Sprintf("（重拨次数已耗尽，已升级至第 %d 级）", nextStep)
								wakeWorker = true
							}
						}
					} else if cfg.VoiceFastEscalateOnReject {
						if nextStep, advanced, escErr := h.store.FastEscalateVoiceCall(ctx, entry.TaskID, callID, entry.EscalationStep, now); escErr != nil {
							slog.Warn("fast escalate voice no_answer without redial failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", escErr))
						} else if advanced {
							policySuffix = fmt.Sprintf("（未开启重拨，已升级至第 %d 级）", nextStep)
							wakeWorker = true
						}
					}
				}
			}

			if policySuffix != "" {
				entry.Message = truncateLogMsg(entry.Message+policySuffix, 500)
			}
			if err := h.store.UpdateNotificationLog(ctx, entry); err != nil {
				slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
			}
			if wakeWorker && h.worker != nil {
				h.worker.Wake()
			}
			return
		}

		if ev.Duration > 0 {
			entry.Status = model.NotifyStatusAnswered
		}
		if ev.StatusMsg != "" {
			entry.Message = truncateLogMsg(ev.StatusMsg, 500)
		}

		var wakeWorker bool
		if ev.Duration > 0 && !wasAnswered && entry.TaskID > 0 && callID != "" {
			cfg := h.loadVoicePolicyConfig(r)
			if cfg.VoiceRedialOnNoDTMF {
				maxRedials := voice.NormalizeVoiceRedialMaxAttempts(cfg.VoiceRedialMaxAttempts)
				delaySecs := voice.NormalizeVoiceRedialDelaySeconds(cfg.VoiceRedialDelaySeconds)
				if maxRedials > 0 {
					attempt, scheduled, redialErr := h.store.ScheduleVoiceRedialByCall(ctx, entry.TaskID, callID, maxRedials, now.Add(time.Duration(delaySecs)*time.Second))
					if redialErr != nil {
						slog.Warn("schedule voice no-dtmf redial failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", redialErr))
					} else if scheduled {
						base := entry.Message
						if base == "" {
							base = "已接听未按键"
						}
						entry.Message = truncateLogMsg(fmt.Sprintf("%s（未按键，%d 秒后自动第 %d 次重拨）", base, delaySecs, attempt), 500)
						wakeWorker = true
					} else if cfg.VoiceFastEscalateOnReject {
						if nextStep, advanced, escErr := h.store.FastEscalateVoiceCall(ctx, entry.TaskID, callID, entry.EscalationStep, now); escErr != nil {
							slog.Warn("fast escalate after no-dtmf redials failed", slog.Uint64("task_id", entry.TaskID), slog.Any("err", escErr))
						} else if advanced {
							base := entry.Message
							if base == "" {
								base = "已接听未按键"
							}
							entry.Message = truncateLogMsg(fmt.Sprintf("%s（未按键重拨耗尽，已升级至第 %d 级）", base, nextStep), 500)
							wakeWorker = true
						}
					}
				}
			}
		}

		if err := h.store.UpdateNotificationLog(ctx, entry); err != nil {
			slog.Warn("update voice callback log failed", slog.Uint64("log_id", entry.ID), slog.Any("err", err))
		}
		if wakeWorker && h.worker != nil {
			h.worker.Wake()
		}
	}
}

func (h *Handler) loadVoicePolicyConfig(r *http.Request) voice.VoiceGatewayConfig {
	cfg, err := h.store.GetVoiceConfig(r.Context())
	if err != nil || cfg == nil {
		return voice.VoiceGatewayConfig{
			VoiceRedialMaxAttempts:    1,
			VoiceRedialDelaySeconds:   20,
			VoiceFastEscalateOnReject: true,
			VoiceRedialOnNoDTMF:       false,
		}
	}
	return *cfg
}

func defaultVoiceFailureMessage(status string) string {
	switch status {
	case model.NotifyStatusRejected:
		return "被叫拒接或通话忙"
	case model.NotifyStatusNoAnswer:
		return "振铃超时无人接听"
	case model.NotifyStatusUnreachable:
		return "被叫关机、停机或不在服务区"
	default:
		return "语音外呼失败"
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
	statusCode := firstMapString(m, "status_code", "result", "Result", "call_result", "CallResult", "hangup_cause", "HangupCause")
	ev := parsedVoiceCallback{
		OutCallID:  firstMapString(m, "call_id", "CallId", "callid"),
		CallID:     firstMapString(m, "out_id", "OutId", "session_context", "SessionContext"),
		DTMF:       firstMapString(m, "dtmf", "digits", "key_press", "press", "Digits"),
		Duration:   firstMapInt(m, "duration", "duration_secs", "Duration"),
		StatusCode: statusCode,
		StatusMsg:  firstMapString(m, "status_msg", "errmsg", "ErrMsg", "Result", "status_code"),
	}
	successFlag := firstMapString(m, "success")
	if ev.DTMF == "" && ev.Duration == 0 {
		if status := classifyVoiceCallback(statusCode, ev.StatusMsg, successFlag); status != "" {
			ev.Failed = true
			ev.FailureStatus = status
		}
	}
	return ev
}

// classifyVoiceCallback classifies an unsuccessful voice callback (with no DTMF and 0s duration)
// into rejected, no_answer, unreachable, or generic failed. Empty string means success / indeterminate.
func classifyVoiceCallback(statusCode, statusMsg, successFlag string) string {
	code := strings.ToLower(strings.TrimSpace(statusCode))
	msg := strings.ToLower(strings.TrimSpace(statusMsg))
	combined := strings.TrimSpace(code + " " + msg)

	if containsAnyVoiceToken(combined,
		"200002", "486", "603",
		"reject", "rejected", "decline", "declined", "busy",
		"拒接", "挂断", "用户忙", "通话中", "忙碌",
	) {
		return model.NotifyStatusRejected
	}

	if containsAnyVoiceToken(combined,
		"200001", "408", "480",
		"no_answer", "no answer", "noanswer", "not_answered", "unanswered", "ring_timeout", "ringing_timeout", "timeout",
		"无人接听", "无应答", "超时", "未接",
	) {
		return model.NotifyStatusNoAnswer
	}

	if containsAnyVoiceToken(combined,
		"200003", "200004", "200005", "404", "484", "503",
		"unreachable", "not_reachable", "power_off", "poweroff", "powered_off", "shutdown", "out_of_service", "invalid_number", "not_exist", "absent",
		"关机", "停机", "空号", "不在服务区", "无法接通", "不可达",
	) {
		return model.NotifyStatusUnreachable
	}

	isSuccessCode := code == "" || code == "0" || code == "200000" || code == "ok" || code == "success"
	if strings.EqualFold(successFlag, "false") || !isSuccessCode {
		return model.NotifyStatusFailed
	}
	return ""
}

func containsAnyVoiceToken(text string, tokens ...string) bool {
	if text == "" {
		return false
	}
	for _, tok := range tokens {
		if strings.Contains(text, tok) {
			return true
		}
	}
	return false
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
