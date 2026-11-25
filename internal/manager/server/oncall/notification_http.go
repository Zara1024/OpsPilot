package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type SetNotificationRulesRequest struct {
	Urgency string                       `json:"urgency"` // high, low
	Rules   []model.UserNotificationRule `json:"rules"`
}

type TestNotificationRequest struct {
	Channel string `json:"channel"` // sms, voice_call, im_dm, im_group_at, email
	Urgency string `json:"urgency"`
}

type CreateEscalationPolicyRequest struct {
	ScheduleID  uint64  `json:"schedule_id"`
	Name        string  `json:"name"`
	StepNumber  uint8   `json:"step_number"`
	WaitSeconds uint32  `json:"wait_seconds"`
	TargetType  string  `json:"target_type"`
	TargetID    *uint64 `json:"target_id,omitempty"`
}

func (h *Handler) getMyNotificationRules(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	urgency := r.URL.Query().Get("urgency")
	rules, err := h.store.ListUserNotificationRules(r.Context(), caller.UserID, urgency)
	if err != nil {
		writeErr(w, err)
		return
	}
	// Default rules if empty
	if len(rules) == 0 {
		rules = []*model.UserNotificationRule{
			{UserID: caller.UserID, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
			{UserID: caller.UserID, Urgency: "high", StepNumber: 2, DelayMinutes: 2, Channel: "sms", Enabled: true},
			{UserID: caller.UserID, Urgency: "high", StepNumber: 3, DelayMinutes: 5, Channel: "voice_call", Enabled: true},
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules, "total": len(rules)})
}

func (h *Handler) setMyNotificationRules(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	var req SetNotificationRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.Urgency == "" {
		req.Urgency = "high"
	}
	var ptrRules []*model.UserNotificationRule
	for i := range req.Rules {
		ru := req.Rules[i]
		ru.UserID = caller.UserID
		ru.Urgency = req.Urgency
		ru.StepNumber = uint8(i + 1)
		ptrRules = append(ptrRules, &ru)
	}
	if err := h.store.SetUserNotificationRules(r.Context(), caller.UserID, req.Urgency, ptrRules); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": ptrRules})
}

// @Summary Test personal notification channel connectivity
// @Router /api/v1/oncall/users/me/notification-rules/test [post]
// @Success 200 {object} map[string]any
func (h *Handler) testNotification(w http.ResponseWriter, r *http.Request) {
	c, ok := requireAuth(w, r)
	if !ok {
		return
	}
	var req TestNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.Channel == "" {
		req.Channel = "im_dm"
	}
	if req.Urgency == "" {
		req.Urgency = "high"
	}

	usersMap, _ := h.store.GetUsersByIDs(r.Context(), []uint64{c.UserID})
	userSummary := usersMap[c.UserID]
	if userSummary == nil {
		userSummary = &store.UserSummary{ID: c.UserID, DisplayName: fmt.Sprintf("user-%d", c.UserID)}
	}
	userName := userSummary.DisplayName
	if userName == "" {
		userName = userSummary.Email
	}

	switch req.Channel {
	case "voice_call":
		throttled, tErr := h.store.HasRecentVoiceCall(r.Context(), c.UserID, time.Now().Add(-3*time.Minute))
		if tErr != nil {
			slog.Warn("check recent voice calls failed", slog.Uint64("user_id", c.UserID), slog.Any("err", tErr))
		}
		if throttled {
			msg := "3 分钟内已发起过语音外呼，触发频控保护，请稍后再试。"
			h.recordNotificationLog(r.Context(), &model.NotificationLog{
				UserID:    c.UserID,
				UserName:  userName,
				Target:    maskPhoneForLog(userSummary.Phone),
				Channel:   "voice_call",
				Provider:  "throttle",
				Status:    model.NotifyStatusThrottled,
				Message:   msg,
				CreatedAt: time.Now(),
			})
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      false,
				"channel": req.Channel,
				"target":  maskPhoneForLog(userSummary.Phone),
				"status":  model.NotifyStatusThrottled,
				"message": msg,
			})
			return
		}

		cfg, cfgErr := h.store.GetVoiceConfig(r.Context())
		if cfgErr != nil || cfg == nil {
			cfg = store.DefaultVoiceGatewayConfig()
		}
		forceMock := c.Role != "admin"
		phone := userSummary.Phone
		if phone == "" && (cfg.Provider == "mock" || forceMock) {
			phone = "13800000000"
		}
		callID := fmt.Sprintf("call_rule_%d", time.Now().UnixNano())
		res, callErr := h.voiceGateway.SendVoiceCall(r.Context(), cfg, &voice.CallRequest{
			CallID:      callID,
			PhoneNumber: phone,
			Urgency:     req.Urgency,
			Title:       "OpsPilot 个人通知阶梯语音连通性测试",
			Severity:    "P1",
			ForceMock:   forceMock,
		})
		status := model.NotifyStatusCalling
		provider := cfg.Provider
		outCallID := ""
		dtmf := ""
		dur := 0
		msg := ""
		if res != nil {
			if res.Status != "" {
				status = res.Status
			}
			if res.Provider != "" {
				provider = res.Provider
			}
			outCallID = res.OutCallID
			dtmf = res.DTMFCode
			dur = res.DurationSecs
			msg = res.Message
		}
		if callErr != nil {
			status = model.NotifyStatusFailed
			if msg == "" {
				msg = callErr.Error()
			}
		}
		h.recordNotificationLog(r.Context(), &model.NotificationLog{
			UserID:       c.UserID,
			UserName:     userName,
			Target:       maskPhoneForLog(phone),
			Channel:      "voice_call",
			Provider:     provider,
			CallID:       callID,
			OutCallID:    outCallID,
			Status:       status,
			DTMFCode:     dtmf,
			DurationSecs: dur,
			Message:      truncateLogMsg(msg, 500),
			CreatedAt:    time.Now(),
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      callErr == nil,
			"channel": req.Channel,
			"target":  maskPhoneForLog(phone),
			"status":  status,
			"result":  res,
			"message": msg,
		})

	case "im_dm", "im_group_at", "webhook":
		if h.deliverer == nil {
			msg := "尚未配置 IM / Webhook 告警通知渠道，请先在系统设置中创建并启用通知渠道。"
			h.recordNotificationLog(r.Context(), &model.NotificationLog{
				UserID:    c.UserID,
				UserName:  userName,
				Target:    userName,
				Channel:   req.Channel,
				Provider:  "unconfigured",
				Status:    model.NotifyStatusFailed,
				Message:   msg,
				CreatedAt: time.Now(),
			})
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      false,
				"channel": req.Channel,
				"target":  userName,
				"status":  model.NotifyStatusFailed,
				"message": msg,
			})
			return
		}
		provider, err := h.deliverer.Deliver(
			r.Context(),
			req.Channel,
			userSummary,
			fmt.Sprintf("[OpsPilot On-Call] 通知通道连通性测试 (%s)", req.Channel),
			fmt.Sprintf("触发人: %s | 紧急程度: %s", userName, req.Urgency),
			"warning",
			map[string]string{"source": "oncall_test", "channel": req.Channel},
		)
		status := model.NotifyStatusSent
		msg := fmt.Sprintf("测试通知已通过 [%s] 发送。", provider)
		if err != nil {
			status = model.NotifyStatusFailed
			msg = err.Error()
			if provider == "" {
				provider = "unconfigured"
			}
		}
		h.recordNotificationLog(r.Context(), &model.NotificationLog{
			UserID:    c.UserID,
			UserName:  userName,
			Target:    userName,
			Channel:   req.Channel,
			Provider:  provider,
			Status:    status,
			Message:   truncateLogMsg(msg, 500),
			CreatedAt: time.Now(),
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      err == nil,
			"channel": req.Channel,
			"target":  userName,
			"status":  status,
			"message": msg,
		})

	default:
		msg := fmt.Sprintf("通道 [%s] 尚未接入外部服务商，请优先使用语音网关或 IM/Webhook 渠道。", req.Channel)
		h.recordNotificationLog(r.Context(), &model.NotificationLog{
			UserID:    c.UserID,
			UserName:  userName,
			Target:    userName,
			Channel:   req.Channel,
			Provider:  "unconfigured",
			Status:    model.NotifyStatusFailed,
			Message:   msg,
			CreatedAt: time.Now(),
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      false,
			"channel": req.Channel,
			"target":  userName,
			"status":  model.NotifyStatusFailed,
			"message": msg,
		})
	}
}

func (h *Handler) recordNotificationLog(ctx context.Context, entry *model.NotificationLog) {
	if err := h.store.CreateNotificationLog(ctx, entry); err != nil {
		slog.Warn("record oncall notification log failed", slog.Uint64("user_id", entry.UserID), slog.Any("err", err))
	}
}

// @Summary List On-Call notification and voice delivery logs
// @Router /api/v1/oncall/notifications/logs [get]
// @Success 200 {object} map[string]any
func (h *Handler) listNotificationLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAuth(w, r); !ok {
		return
	}
	page := 0
	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p >= 0 {
			page = p
		}
	}
	pageSize := 20
	if psStr := r.URL.Query().Get("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}
	channel := r.URL.Query().Get("channel")
	status := r.URL.Query().Get("status")
	items, total, err := h.store.ListNotificationLogs(r.Context(), channel, status, page*pageSize, pageSize)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// @Summary List On-Call escalation state machine tasks
// @Router /api/v1/oncall/escalations/tasks [get]
// @Success 200 {object} map[string]any
func (h *Handler) listEscalationTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAuth(w, r); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}
	items, err := h.store.ListEscalationTasks(r.Context(), status, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (h *Handler) listEscalationPolicies(w http.ResponseWriter, r *http.Request) {
	schedIDStr := r.URL.Query().Get("schedule_id")
	var schedID uint64
	if schedIDStr != "" {
		schedID, _ = strconv.ParseUint(schedIDStr, 10, 64)
	}
	list, err := h.store.ListEscalationPolicies(r.Context(), schedID)
	if err != nil {
		writeErr(w, err)
		return
	}
	// If empty, supply standard multi-step templates
	if len(list) == 0 {
		list = []*model.EscalationPolicy{
			{ScheduleID: schedID, Name: "一线主值班即时通知", StepNumber: 1, WaitSeconds: 0, TargetType: "primary_oncall"},
			{ScheduleID: schedID, Name: "未响应升级至二线技术专家", StepNumber: 2, WaitSeconds: 300, TargetType: "secondary_oncall"},
			{ScheduleID: schedID, Name: "仍未响应升级至SRE应急负责人", StepNumber: 3, WaitSeconds: 900, TargetType: "fallback_lead"},
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": len(list)})
}

func (h *Handler) createEscalationPolicy(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	var req CreateEscalationPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.Name == "" {
		writeErr(w, fmt.Errorf("%w: name is required", errs.ErrInvalid))
		return
	}
	p := &model.EscalationPolicy{
		ScheduleID:  req.ScheduleID,
		Name:        req.Name,
		StepNumber:  req.StepNumber,
		WaitSeconds: req.WaitSeconds,
		TargetType:  req.TargetType,
		TargetID:    req.TargetID,
		CreatedAt:   time.Now(),
	}
	if err := h.store.CreateEscalationPolicy(r.Context(), p); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) updateEscalationPolicy(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	existing, err := h.store.GetEscalationPolicy(r.Context(), id)
	if err != nil {
		writeErr(w, errs.ErrNotFound)
		return
	}
	var req CreateEscalationPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.WaitSeconds > 0 {
		existing.WaitSeconds = req.WaitSeconds
	}
	if req.TargetType != "" {
		existing.TargetType = req.TargetType
	}
	if err := h.store.UpdateEscalationPolicy(r.Context(), existing); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *Handler) deleteEscalationPolicy(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.DeleteEscalationPolicy(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) chatopsCallback(w http.ResponseWriter, r *http.Request) {
	caller, _ := requireAuth(w, r)
	var req bizoncall.ChatOpsActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.OperatorID == 0 {
		req.OperatorID = caller.UserID
	}
	res, err := h.escalationEngine.HandleAction(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) listActiveIncidents(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	incidents, err := h.store.ListAlertIncidents(r.Context(), statusFilter, limit)
	if err != nil {
		writeErr(w, err)
		return
	}

	// If default query (no statusFilter specified) and there are 0 active,
	// fall back to the most recent alert records so operators have visibility over real devices.
	if statusFilter == "" && len(incidents) == 0 {
		incidents, _ = h.store.ListAlertIncidents(r.Context(), "all", 20)
	}

	// Gather acked_by user IDs and incident IDs for display name & live escalation step resolution
	var userIDs []uint64
	incidentIDs := make([]uint64, 0, len(incidents))
	for _, inc := range incidents {
		incidentIDs = append(incidentIDs, inc.ID)
		if inc.AcknowledgedBy != nil && *inc.AcknowledgedBy > 0 {
			userIDs = append(userIDs, *inc.AcknowledgedBy)
		}
	}
	userMap, _ := h.store.GetUsersByIDs(r.Context(), userIDs)
	activeSteps, _ := h.store.GetActiveEscalationStepsByIncidents(r.Context(), incidentIDs)

	items := make([]bizoncall.IncidentSummary, 0, len(incidents))
	for _, inc := range incidents {
		// Severity mapping
		sev := "P2"
		switch strings.ToLower(inc.Severity) {
		case "critical", "p0", "p1":
			sev = "P1"
		case "warning", "p2":
			sev = "P2"
		case "info", "p3":
			sev = "P3"
		}

		// Status mapping
		st := "firing"
		switch inc.Status {
		case "acknowledged":
			st = "acknowledged"
		case "silenced":
			st = "silenced"
		case "resolved":
			st = "resolved"
		case "open":
			st = "firing"
		default:
			st = inc.Status
		}

		// Parse labels and match route
		labels := make(map[string]string)
		if inc.LabelsJSON != "" {
			_ = json.Unmarshal([]byte(inc.LabelsJSON), &labels)
		}
		labels["severity"] = inc.Severity
		if inc.Rule != "" {
			labels["rule"] = inc.Rule
		}
		if inc.RuleName != "" {
			labels["rule_name"] = inc.RuleName
		}
		if inc.Scope != "" {
			labels["scope"] = inc.Scope
		}

		schedID := uint64(0)
		schedName := "基础设施核心值班"
		assignee := "admin"

		matchRes, mErr := h.routeMatcher.MatchLabels(r.Context(), 0, labels)
		if mErr == nil && matchRes != nil && matchRes.ScheduleID > 0 {
			schedID = matchRes.ScheduleID
			if matchRes.ScheduleName != "" {
				schedName = matchRes.ScheduleName
			}
			live, lerr := h.scheduler.GetLiveStatus(r.Context(), schedID, time.Now())
			if lerr == nil && live != nil && live.PrimaryUser != nil && live.PrimaryUser.UserName != "" {
				assignee = live.PrimaryUser.UserName
			}
		}

		ackedByName := ""
		if inc.AcknowledgedBy != nil {
			if u, ok := userMap[*inc.AcknowledgedBy]; ok && u != nil {
				ackedByName = u.DisplayName
				if ackedByName == "" {
					ackedByName = u.Email
				}
			}
		}

		step := uint8(1)
		if s := activeSteps[inc.ID]; s > 0 {
			step = s
		}
		items = append(items, bizoncall.IncidentSummary{
			ID:              fmt.Sprintf("INC-%04d", inc.ID),
			Title:           inc.Title,
			Severity:        sev,
			ScheduleID:      schedID,
			ScheduleName:    schedName,
			CurrentAssignee: assignee,
			CurrentTier:     1,
			Status:          st,
			StartedAt:       inc.FirstFiredAt,
			AckedAt:         inc.AcknowledgedAt,
			AckedBy:         ackedByName,
			EscalationStep:  step,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}
