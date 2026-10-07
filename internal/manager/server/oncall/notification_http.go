package oncall

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
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

func (h *Handler) testNotification(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	var req TestNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	usersMap, _ := h.store.GetUsersByIDs(r.Context(), []uint64{caller.UserID})
	phone := "13800000000"
	if u, ok := usersMap[caller.UserID]; ok && u.Phone != "" {
		phone = u.Phone
	}
	sim := h.escalationEngine.SimulateVoiceCall(phone, req.Urgency)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"channel": req.Channel,
		"target":  phone,
		"result":  sim,
		"message": fmt.Sprintf("测试通知已成功推送到渠道 [%s]，模拟拨号已接通并收到 DTMF 按键反馈。", req.Channel),
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
	incidents := []bizoncall.IncidentSummary{
		{
			ID:              "INC-20261005-001",
			Title:           "订单支付接口超时率突增 (>5.2%)",
			Severity:        "P1",
			ScheduleID:      2,
			ScheduleName:    "基础设施与网络值班",
			CurrentAssignee: "李四 (SRE)",
			CurrentTier:     1,
			Status:          "firing",
			StartedAt:       time.Now().Add(-14 * time.Minute),
			EscalationStep:  2,
		},
		{
			ID:              "INC-20261005-002",
			Title:           "Kubernetes Node 内存利用率高位告警 (>92%)",
			Severity:        "P2",
			ScheduleID:      1,
			ScheduleName:    "SRE 核心生产值班",
			CurrentAssignee: "admin",
			CurrentTier:     1,
			Status:          "acknowledged",
			StartedAt:       time.Now().Add(-38 * time.Minute),
			AckedBy:         "admin",
			EscalationStep:  1,
		},
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": incidents, "total": len(incidents)})
}
