package oncall

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type AckHandoffReq struct {
	ScheduleID uint64 `json:"schedule_id"`
	Notes      string `json:"notes"`
}

func (h *Handler) getCurrentHandoff(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	schedIDStr := r.URL.Query().Get("schedule_id")
	schedID, _ := strconv.ParseUint(schedIDStr, 10, 64)
	if schedID == 0 {
		scheds, err := h.store.ListSchedules(ctx, 0, false)
		if err == nil && len(scheds) > 0 {
			schedID = scheds[0].ID
		}
	}

	res, err := h.handoffService.GetCurrentHandoffStatus(ctx, schedID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) listHandoffLogs(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	schedIDStr := r.URL.Query().Get("schedule_id")
	schedID, _ := strconv.ParseUint(schedIDStr, 10, 64)
	limitStr := r.URL.Query().Get("limit")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 20
	}

	logs, err := h.handoffService.ListHandoffLogs(ctx, schedID, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": logs,
		"total": len(logs),
	})
}

func (h *Handler) ackHandoff(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var req AckHandoffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errors.New("invalid request body"))
		return
	}
	if req.ScheduleID == 0 {
		scheds, _ := h.store.ListSchedules(ctx, 0, false)
		if len(scheds) > 0 {
			req.ScheduleID = scheds[0].ID
		}
	}

	res, err := h.handoffService.AckHandoff(ctx, req.ScheduleID, caller.UserID, req.Notes)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) getBurnoutAnalytics(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	schedIDStr := r.URL.Query().Get("schedule_id")
	schedID, _ := strconv.ParseUint(schedIDStr, 10, 64)
	timeRange := r.URL.Query().Get("time_range")
	if timeRange == "" {
		timeRange = "7d"
	}

	res, err := h.burnoutAnalyzer.GetBurnoutAnalytics(ctx, schedID, timeRange)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) getNoisyAlerts(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	schedIDStr := r.URL.Query().Get("schedule_id")
	schedID, _ := strconv.ParseUint(schedIDStr, 10, 64)

	res, err := h.burnoutAnalyzer.GetNoisyAlerts(ctx, schedID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": res,
		"total": len(res),
	})
}
