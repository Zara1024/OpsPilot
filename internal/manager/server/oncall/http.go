package oncall

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type Handler struct {
	store            *store.Store
	scheduler        *bizoncall.Scheduler
	gapDetector      *bizoncall.GapDetector
	icalBuilder      *bizoncall.ICalBuilder
	routeMatcher     *bizoncall.RouteMatcher
	escalationEngine *bizoncall.EscalationEngine
	handoffService   *bizoncall.HandoffService
	burnoutAnalyzer  *bizoncall.BurnoutAnalyzer
	voiceGateway     *voice.VoiceGateway
}

func NewHandler(s *store.Store) *Handler {
	sched := bizoncall.NewScheduler(s)
	return &Handler{
		store:            s,
		scheduler:        sched,
		gapDetector:      bizoncall.NewGapDetector(sched),
		icalBuilder:      bizoncall.NewICalBuilder(),
		routeMatcher:     bizoncall.NewRouteMatcher(s),
		escalationEngine: bizoncall.NewEscalationEngine(s, sched),
		handoffService:   bizoncall.NewHandoffService(s, sched),
		burnoutAnalyzer:  bizoncall.NewBurnoutAnalyzer(s),
		voiceGateway:     voice.NewVoiceGateway(),
	}
}

// Register mounts authenticated On-Call endpoints.
func (h *Handler) Register(r chi.Router) {
	r.Get("/v1/oncall/schedules", h.listSchedules)
	r.Post("/v1/oncall/schedules", h.createSchedule)
	r.Get("/v1/oncall/schedules/{id}", h.getSchedule)
	r.Put("/v1/oncall/schedules/{id}", h.updateSchedule)
	r.Delete("/v1/oncall/schedules/{id}", h.deleteSchedule)

	r.Get("/v1/oncall/schedules/{id}/calendar", h.getCalendarShifts)
	r.Get("/v1/oncall/schedules/{id}/current", h.getCurrentOnCall)
	r.Get("/v1/oncall/schedules/{id}/gaps", h.getScheduleGaps)

	r.Get("/v1/oncall/schedules/{id}/overrides", h.listOverrides)
	r.Post("/v1/oncall/schedules/{id}/overrides", h.createOverride)
	r.Delete("/v1/oncall/overrides/{overrideId}", h.deleteOverride)

	// Phase 2: Pending Overrides & Approvals
	r.Get("/v1/oncall/pending-overrides", h.listPendingOverrides)
	r.Post("/v1/oncall/overrides/{overrideId}/approve", h.approveOverride)
	r.Post("/v1/oncall/overrides/{overrideId}/reject", h.rejectOverride)
	r.Post("/v1/oncall/schedules/{id}/secondary-rotation", h.setSecondaryRotation)

	// Phase 2: Alert Label Route Tree
	r.Get("/v1/oncall/routes", h.listRoutes)
	r.Post("/v1/oncall/routes", h.createRoute)
	r.Get("/v1/oncall/routes/{id}", h.getRoute)
	r.Put("/v1/oncall/routes/{id}", h.updateRoute)
	r.Delete("/v1/oncall/routes/{id}", h.deleteRoute)
	r.Post("/v1/oncall/routes/test-match", h.testMatchRoute)

	r.Get("/v1/oncall/users/me/calendar/token", h.getMyCalendarToken)
	r.Post("/v1/oncall/users/me/calendar/token/reset", h.resetMyCalendarToken)

	// Phase 3: Personal Notification Ladder & Escalation Policies
	r.Get("/v1/oncall/users/me/notification-rules", h.getMyNotificationRules)
	r.Put("/v1/oncall/users/me/notification-rules", h.setMyNotificationRules)
	r.Post("/v1/oncall/users/me/notification-rules/test", h.testNotification)

	r.Get("/v1/oncall/escalation-policies", h.listEscalationPolicies)
	r.Post("/v1/oncall/escalation-policies", h.createEscalationPolicy)
	r.Put("/v1/oncall/escalation-policies/{id}", h.updateEscalationPolicy)
	r.Delete("/v1/oncall/escalation-policies/{id}", h.deleteEscalationPolicy)

	// Phase 3: ChatOps & Incident Actions
	r.Post("/v1/oncall/chatops/callback", h.chatopsCallback)
	r.Get("/v1/oncall/incidents/active", h.listActiveIncidents)

	// Phase 4: Shift Handoff Notes & SRE Burnout Analytics
	r.Get("/v1/oncall/handoff/current", h.getCurrentHandoff)
	r.Get("/v1/oncall/handoff/logs", h.listHandoffLogs)
	r.Post("/v1/oncall/handoff/ack", h.ackHandoff)
	r.Get("/v1/oncall/analytics/burnout", h.getBurnoutAnalytics)
	r.Get("/v1/oncall/analytics/noisy-alerts", h.getNoisyAlerts)

	// Voice Gateway
	r.Get("/v1/oncall/voice/config", h.getVoiceConfig)
	r.Put("/v1/oncall/voice/config", h.updateVoiceConfig)
	r.Post("/v1/oncall/voice/test-call", h.testVoiceCall)
}

// RegisterPublic mounts public unauthenticated endpoints (e.g. token-secured Webcal feed).
func (h *Handler) RegisterPublic(r chi.Router) {
	r.Get("/v1/oncall/calendar/subscribe/{token}.ics", h.subscribeCalendarICS)
	r.Post("/v1/oncall/voice/callback/aliyun", h.aliyunVoiceCallback)
	r.Post("/v1/oncall/voice/callback/tencent", h.tencentVoiceCallback)
}

// --- Schedule CRUD ---

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListSchedules(r.Context(), 0, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	item, err := h.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeErr(w, errs.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}

	var req CreateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.Name == "" {
		writeErr(w, fmt.Errorf("%w: name is required", errs.ErrInvalid))
		return
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Shanghai"
	}
	if req.HandoffTime == "" {
		req.HandoffTime = "09:00:00"
	}
	if req.ReminderAdvanceHours == 0 {
		req.ReminderAdvanceHours = 3
	}

	sched := &model.Schedule{
		Name:                 req.Name,
		Description:          req.Description,
		Timezone:             req.Timezone,
		HandoffTime:          req.HandoffTime,
		ReminderAdvanceHours: req.ReminderAdvanceHours,
		RequireSwapApproval:  req.RequireSwapApproval,
		Enabled:              req.Enabled,
		CreatedBy:            caller.UserID,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	if err := h.store.CreateSchedule(r.Context(), sched); err != nil {
		writeErr(w, err)
		return
	}

	for _, rot := range req.Rotations {
		uJSON, _ := json.Marshal(rot.Users)
		eff := rot.EffectiveFrom
		if eff.IsZero() {
			eff = time.Now()
		}
		shiftLen := rot.ShiftLengthSeconds
		if shiftLen == 0 {
			shiftLen = 86400
		}
		name := rot.Name
		if name == "" {
			name = "一线值班轮转"
		}
		tier := rot.Tier
		if tier == 0 {
			tier = model.TierPrimary
		}
		rModel := &model.Rotation{
			ScheduleID:           sched.ID,
			Name:                 name,
			Tier:                 tier,
			RotationType:         rot.RotationType,
			ShiftLengthSeconds:   shiftLen,
			UsersJSON:            string(uJSON),
			EffectiveFrom:        eff,
			TimeRestrictionType:  rot.TimeRestrictionType,
			RestrictionStartTime: rot.RestrictionStartTime,
			RestrictionEndTime:   rot.RestrictionEndTime,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		}
		_ = h.store.CreateRotation(r.Context(), rModel)
	}

	created, _ := h.store.GetSchedule(r.Context(), sched.ID)
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	sched, err := h.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeErr(w, errs.ErrNotFound)
		return
	}

	var req UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	if req.Name != "" {
		sched.Name = req.Name
	}
	sched.Description = req.Description
	if req.Timezone != "" {
		sched.Timezone = req.Timezone
	}
	if req.HandoffTime != "" {
		sched.HandoffTime = req.HandoffTime
	}
	sched.ReminderAdvanceHours = req.ReminderAdvanceHours
	sched.RequireSwapApproval = req.RequireSwapApproval
	sched.Enabled = req.Enabled
	sched.UpdatedAt = time.Now()

	if err := h.store.UpdateSchedule(r.Context(), sched); err != nil {
		writeErr(w, err)
		return
	}

	if len(req.Rotations) > 0 {
		// Update rotations: replace with submitted
		existing, _ := h.store.ListRotationsBySchedule(r.Context(), sched.ID)
		for _, ex := range existing {
			_ = h.store.DeleteRotation(r.Context(), ex.ID)
		}
		for _, rot := range req.Rotations {
			uJSON, _ := json.Marshal(rot.Users)
			eff := rot.EffectiveFrom
			if eff.IsZero() {
				eff = time.Now()
			}
			shiftLen := rot.ShiftLengthSeconds
			if shiftLen == 0 {
				shiftLen = 86400
			}
			name := rot.Name
			if name == "" {
				name = "一线值班轮转"
			}
			tier := rot.Tier
			if tier == 0 {
				tier = model.TierPrimary
			}
			rModel := &model.Rotation{
				ScheduleID:           sched.ID,
				Name:                 name,
				Tier:                 tier,
				RotationType:         rot.RotationType,
				ShiftLengthSeconds:   shiftLen,
				UsersJSON:            string(uJSON),
				EffectiveFrom:        eff,
				TimeRestrictionType:  rot.TimeRestrictionType,
				RestrictionStartTime: rot.RestrictionStartTime,
				RestrictionEndTime:   rot.RestrictionEndTime,
				CreatedAt:            time.Now(),
				UpdatedAt:            time.Now(),
			}
			_ = h.store.CreateRotation(r.Context(), rModel)
		}
	}

	updated, _ := h.store.GetSchedule(r.Context(), sched.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.DeleteSchedule(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Calendar & Live Status ---

func (h *Handler) getCalendarShifts(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	now := time.Now()
	start := now.AddDate(0, 0, -7)
	end := now.AddDate(0, 1, 7)

	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	shifts, err := h.scheduler.ComputeScheduleShifts(r.Context(), id, start, end)
	if err != nil {
		writeErr(w, err)
		return
	}

	gaps, _ := h.gapDetector.DetectGaps(r.Context(), id, start, end)

	writeJSON(w, http.StatusOK, map[string]any{
		"shifts": shifts,
		"gaps":   gaps,
		"start":  start,
		"end":    end,
	})
}

func (h *Handler) getCurrentOnCall(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	status, err := h.scheduler.GetLiveStatus(r.Context(), id, time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) getScheduleGaps(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	start := time.Now()
	end := start.AddDate(0, 0, 14) // default 14 days

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	gaps, err := h.gapDetector.DetectGaps(r.Context(), id, start, end)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"gaps": gaps, "total": len(gaps)})
}

// --- Overrides ---

func (h *Handler) listOverrides(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	now := time.Now()
	start := now.AddDate(0, -1, 0)
	end := now.AddDate(0, 2, 0)

	items, err := h.store.ListOverridesInWindow(r.Context(), id, start, end)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createOverride(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	var req CreateOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if req.SubstituteUserID == 0 || req.StartTime.IsZero() || req.EndTime.IsZero() {
		writeErr(w, fmt.Errorf("%w: substitute_user_id, start_time, end_time are required", errs.ErrInvalid))
		return
	}
	if !req.EndTime.After(req.StartTime) {
		writeErr(w, fmt.Errorf("%w: end_time must be after start_time", errs.ErrInvalid))
		return
	}

	ovType := req.Type
	if ovType == "" {
		ovType = model.OverrideTypeOverride
	}

	status := model.OverrideStatusApproved
	if req.Status != "" {
		status = req.Status
	}

	ov := &model.Override{
		ScheduleID:       id,
		RotationID:       req.RotationID,
		Type:             ovType,
		OriginalUserID:   req.OriginalUserID,
		SubstituteUserID: req.SubstituteUserID,
		StartTime:        req.StartTime,
		EndTime:          req.EndTime,
		Reason:           req.Reason,
		Status:           status,
		CreatedBy:        caller.UserID,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := h.store.CreateOverride(r.Context(), ov); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ov)
}

func (h *Handler) deleteOverride(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "overrideId")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.DeleteOverride(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Calendar Token & Webcal Subscription ---

func (h *Handler) getMyCalendarToken(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}

	tok, err := h.store.GetOrCreateCalendarToken(r.Context(), caller.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}

	host := r.Host
	proto := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		proto = "http"
	}

	httpURL := fmt.Sprintf("%s://%s/api/v1/oncall/calendar/subscribe/%s.ics", proto, host, tok.Token)
	webcalURL := fmt.Sprintf("webcal://%s/api/v1/oncall/calendar/subscribe/%s.ics", host, tok.Token)

	writeJSON(w, http.StatusOK, CalendarTokenResponse{
		Token:     tok.Token,
		WebcalURL: webcalURL,
		HttpURL:   httpURL,
	})
}

func (h *Handler) resetMyCalendarToken(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}

	tok, err := h.store.ResetCalendarToken(r.Context(), caller.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}

	host := r.Host
	webcalURL := fmt.Sprintf("webcal://%s/api/v1/oncall/calendar/subscribe/%s.ics", host, tok.Token)
	httpURL := fmt.Sprintf("https://%s/api/v1/oncall/calendar/subscribe/%s.ics", host, tok.Token)

	writeJSON(w, http.StatusOK, CalendarTokenResponse{
		Token:     tok.Token,
		WebcalURL: webcalURL,
		HttpURL:   httpURL,
	})
}

func (h *Handler) subscribeCalendarICS(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	tok, err := h.store.GetUserByCalendarToken(r.Context(), token)
	if err != nil {
		http.Error(w, "invalid or expired calendar subscription token", http.StatusUnauthorized)
		return
	}

	// Fetch all enabled schedules
	schedules, err := h.store.ListSchedules(r.Context(), 0, true)
	if err != nil {
		http.Error(w, "error fetching schedules", http.StatusInternalServerError)
		return
	}

	// Window: [now - 30 days, now + 60 days]
	now := time.Now()
	start := now.AddDate(0, 0, -30)
	end := now.AddDate(0, 0, 60)

	var userShifts []*bizoncall.ShiftSlot
	for _, sched := range schedules {
		shifts, err := h.scheduler.ComputeScheduleShifts(r.Context(), sched.ID, start, end)
		if err != nil {
			continue
		}
		// Filter shifts that belong to this user
		for _, s := range shifts {
			if s.UserID == tok.UserID {
				userShifts = append(userShifts, s)
			}
		}
	}

	icsContent := h.icalBuilder.BuildCalendar("My On-Call Shifts", userShifts)

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"oncall.ics\"")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(icsContent))
}

// --- Helpers ---

type caller struct {
	UserID uint64
	Role   string
}

func requireAuth(w http.ResponseWriter, r *http.Request) (caller, bool) {
	t, ok := tenantctx.From(r.Context())
	if !ok || t.UserID == 0 {
		writeErr(w, errs.ErrUnauthorized)
		return caller{}, false
	}
	return caller{UserID: t.UserID, Role: t.Role}, true
}

func parseUintParam(r *http.Request, key string) (uint64, error) {
	raw := chi.URLParam(r, key)
	return strconv.ParseUint(raw, 10, 64)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	slug := "internal"
	switch {
	case errors.Is(err, errs.ErrUnauthorized):
		code, slug = http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, errs.ErrForbidden):
		code, slug = http.StatusForbidden, "forbidden"
	case errors.Is(err, errs.ErrNotFound):
		code, slug = http.StatusNotFound, "not_found"
	case errors.Is(err, errs.ErrInvalid):
		code, slug = http.StatusBadRequest, "invalid"
	}
	writeJSON(w, code, map[string]any{"error": err.Error(), "code": slug})
}
