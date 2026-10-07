package oncall

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type CreateRouteRequest struct {
	Name                 string                   `json:"name"`
	Matchers             []bizoncall.LabelMatcher `json:"matchers"`
	MatcherJSON          string                   `json:"matcher_json"`
	Priority             int                      `json:"priority"`
	ScheduleID           uint64                   `json:"schedule_id"`
	EscalationPolicyID   uint64                   `json:"escalation_policy_id"`
	GroupWaitSeconds     int                      `json:"group_wait_seconds"`
	GroupIntervalSeconds int                      `json:"group_interval_seconds"`
	Enabled              bool                     `json:"enabled"`
}

type TestMatchRouteRequest struct {
	Labels map[string]string `json:"labels"`
}

type SetSecondaryRotationRequest struct {
	Name                 string    `json:"name"`
	RotationType         string    `json:"rotation_type"`
	ShiftLengthSeconds   uint32    `json:"shift_length_seconds"`
	Users                []uint64  `json:"users"`
	EffectiveFrom        time.Time `json:"effective_from"`
	TimeRestrictionType  string    `json:"time_restriction_type"`
	RestrictionStartTime string    `json:"restriction_start_time"`
	RestrictionEndTime   string    `json:"restriction_end_time"`
}

type PendingOverrideSummary struct {
	model.Override
	ScheduleName       string `json:"schedule_name"`
	OriginalUserName   string `json:"original_user_name"`
	SubstituteUserName string `json:"substitute_user_name"`
}

func (h *Handler) listRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.store.ListRoutes(r.Context(), 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": routes, "total": len(routes)})
}

func (h *Handler) getRoute(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	item, err := h.store.GetRoute(r.Context(), id)
	if err != nil {
		writeErr(w, errs.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) createRoute(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	_ = caller

	var req CreateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	if req.Name == "" {
		writeErr(w, fmt.Errorf("%w: name is required", errs.ErrInvalid))
		return
	}
	if req.ScheduleID == 0 {
		writeErr(w, fmt.Errorf("%w: schedule_id is required", errs.ErrInvalid))
		return
	}

	mJSON := req.MatcherJSON
	if mJSON == "" && len(req.Matchers) > 0 {
		b, _ := json.Marshal(req.Matchers)
		mJSON = string(b)
	}
	if mJSON == "" {
		mJSON = "[]"
	}

	if req.GroupWaitSeconds <= 0 {
		req.GroupWaitSeconds = 30
	}
	if req.GroupIntervalSeconds <= 0 {
		req.GroupIntervalSeconds = 300
	}

	route := &model.Route{
		Name:                 req.Name,
		MatcherJSON:          mJSON,
		Priority:             req.Priority,
		ScheduleID:           req.ScheduleID,
		EscalationPolicyID:   req.EscalationPolicyID,
		GroupWaitSeconds:     req.GroupWaitSeconds,
		GroupIntervalSeconds: req.GroupIntervalSeconds,
		Enabled:              req.Enabled,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	if err := h.store.CreateRoute(r.Context(), route); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, route)
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	existing, err := h.store.GetRoute(r.Context(), id)
	if err != nil {
		writeErr(w, errs.ErrNotFound)
		return
	}

	var req CreateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.ScheduleID > 0 {
		existing.ScheduleID = req.ScheduleID
	}
	existing.EscalationPolicyID = req.EscalationPolicyID
	existing.Priority = req.Priority
	existing.Enabled = req.Enabled

	if len(req.Matchers) > 0 {
		b, _ := json.Marshal(req.Matchers)
		existing.MatcherJSON = string(b)
	} else if req.MatcherJSON != "" {
		existing.MatcherJSON = req.MatcherJSON
	}

	if req.GroupWaitSeconds > 0 {
		existing.GroupWaitSeconds = req.GroupWaitSeconds
	}
	if req.GroupIntervalSeconds > 0 {
		existing.GroupIntervalSeconds = req.GroupIntervalSeconds
	}
	existing.UpdatedAt = time.Now()

	if err := h.store.UpdateRoute(r.Context(), existing); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.DeleteRoute(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) testMatchRoute(w http.ResponseWriter, r *http.Request) {
	var req TestMatchRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	result, err := h.routeMatcher.MatchLabels(r.Context(), 0, req.Labels)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) listPendingOverrides(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListOverridesByStatus(r.Context(), 0, model.OverrideStatusPending)
	if err != nil {
		writeErr(w, err)
		return
	}

	// Batch hydrate user names and schedule names
	var uids []uint64
	for _, it := range items {
		uids = append(uids, it.OriginalUserID, it.SubstituteUserID)
	}
	usersMap, _ := h.store.GetUsersByIDs(r.Context(), uids)
	schedules, _ := h.store.ListSchedules(r.Context(), 0, false)
	schedMap := make(map[uint64]string)
	for _, s := range schedules {
		schedMap[s.ID] = s.Name
	}

	var summaries []*PendingOverrideSummary
	for _, it := range items {
		s := &PendingOverrideSummary{
			Override:     *it,
			ScheduleName: schedMap[it.ScheduleID],
		}
		if u, ok := usersMap[it.OriginalUserID]; ok {
			s.OriginalUserName = u.DisplayName
		}
		if u, ok := usersMap[it.SubstituteUserID]; ok {
			s.SubstituteUserName = u.DisplayName
		}
		summaries = append(summaries, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": summaries, "total": len(summaries)})
}

func (h *Handler) approveOverride(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "overrideId")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	if err := h.store.UpdateOverrideStatus(r.Context(), id, model.OverrideStatusApproved, &caller.UserID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": model.OverrideStatusApproved})
}

func (h *Handler) rejectOverride(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "overrideId")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	if err := h.store.UpdateOverrideStatus(r.Context(), id, model.OverrideStatusRejected, &caller.UserID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": model.OverrideStatusRejected})
}

func (h *Handler) setSecondaryRotation(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}

	var req SetSecondaryRotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	usersBytes, _ := json.Marshal(req.Users)
	rotName := req.Name
	if rotName == "" {
		rotName = "二线专家支持轮转"
	}
	shiftLen := req.ShiftLengthSeconds
	if shiftLen == 0 {
		shiftLen = 86400
	}
	eff := req.EffectiveFrom
	if eff.IsZero() {
		eff = time.Now()
	}

	rot := &model.Rotation{
		ScheduleID:           id,
		Name:                 rotName,
		Tier:                 model.TierSecondary,
		RotationType:         req.RotationType,
		ShiftLengthSeconds:   shiftLen,
		UsersJSON:            string(usersBytes),
		EffectiveFrom:        eff,
		TimeRestrictionType:  req.TimeRestrictionType,
		RestrictionStartTime: req.RestrictionStartTime,
		RestrictionEndTime:   req.RestrictionEndTime,
	}

	if err := h.store.UpsertSecondaryRotation(r.Context(), id, rot); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rotation": rot})
}
