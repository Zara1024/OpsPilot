package oncall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type AckHandoffReq struct {
	ScheduleID uint64 `json:"schedule_id"`
	Notes      string `json:"notes"`
}

type RemindHandoffNowReq struct {
	ScheduleID uint64 `json:"schedule_id"`
}

// HandoffLogResponse enriches a persisted HandoffLog row with display names for the UI.
type HandoffLogResponse struct {
	*model.HandoffLog
	UserName         string `json:"user_name,omitempty"`
	PreviousUserName string `json:"previous_user_name,omitempty"`
	ScheduleName     string `json:"schedule_name,omitempty"`
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

// @Summary List shift handoff sign-off and pre-shift reminder logs
// @Router /api/v1/oncall/handoff/logs [get]
// @Success 200 {object} map[string]any
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
	items := h.hydrateHandoffLogs(ctx, logs)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// @Summary Send an immediate handoff reminder for an upcoming On-Call shift
// @Router /api/v1/oncall/handoff/remind-now [post]
// @Success 200 {object} map[string]any
func (h *Handler) remindHandoffNow(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var req RemindHandoffNowReq
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, fmt.Errorf("%w: invalid request body", errs.ErrInvalid))
			return
		}
	}
	if req.ScheduleID == 0 {
		scheds, _ := h.store.ListSchedules(ctx, 0, false)
		if len(scheds) > 0 {
			req.ScheduleID = scheds[0].ID
		}
	}
	if req.ScheduleID == 0 {
		writeErr(w, fmt.Errorf("%w: no schedule available", errs.ErrInvalid))
		return
	}

	entry, err := h.handoffReminder.RemindNow(ctx, req.ScheduleID)
	if err != nil {
		writeErr(w, err)
		return
	}
	hydrated := h.hydrateHandoffLogs(ctx, []*model.HandoffLog{entry})
	var respLog any = entry
	if len(hydrated) > 0 {
		respLog = hydrated[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            entry.Status == "sent",
		"status":        entry.Status,
		"channel_type":  entry.ChannelType,
		"error_message": entry.ErrorMessage,
		"log":           respLog,
	})
}

func (h *Handler) hydrateHandoffLogs(ctx context.Context, logs []*model.HandoffLog) []HandoffLogResponse {
	items := make([]HandoffLogResponse, 0, len(logs))
	if len(logs) == 0 {
		return items
	}

	uidSet := make(map[uint64]struct{}, len(logs)*2)
	for _, item := range logs {
		if item == nil {
			continue
		}
		if item.UserID > 0 {
			uidSet[item.UserID] = struct{}{}
		}
		if item.PreviousUserID != nil && *item.PreviousUserID > 0 {
			uidSet[*item.PreviousUserID] = struct{}{}
		}
	}
	uids := make([]uint64, 0, len(uidSet))
	for uid := range uidSet {
		uids = append(uids, uid)
	}
	usersMap, _ := h.store.GetUsersByIDs(ctx, uids)

	scheds, _ := h.store.ListSchedules(ctx, 0, false)
	schedMap := make(map[uint64]string, len(scheds))
	for _, s := range scheds {
		if s != nil {
			schedMap[s.ID] = s.Name
		}
	}

	resolveName := func(uid uint64) string {
		if uid == 0 {
			return ""
		}
		if u, ok := usersMap[uid]; ok && u != nil {
			if u.DisplayName != "" {
				return u.DisplayName
			}
			if u.Email != "" {
				return u.Email
			}
		}
		return fmt.Sprintf("User#%d", uid)
	}

	for _, item := range logs {
		if item == nil {
			continue
		}
		resp := HandoffLogResponse{
			HandoffLog:   item,
			UserName:     resolveName(item.UserID),
			ScheduleName: schedMap[item.ScheduleID],
		}
		if item.PreviousUserID != nil && *item.PreviousUserID > 0 {
			resp.PreviousUserName = resolveName(*item.PreviousUserID)
		}
		items = append(items, resp)
	}
	return items
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

// @Summary Get On-Call MTTA/MTTR percentiles, fatigue scores, and trends
// @Router /api/v1/oncall/analytics/burnout [get]
// @Success 200 {object} bizoncall.BurnoutAnalytics
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

// @Summary List high-frequency noisy alert rules in the selected window
// @Router /api/v1/oncall/analytics/noisy-alerts [get]
// @Success 200 {object} map[string]any
func (h *Handler) getNoisyAlerts(w http.ResponseWriter, r *http.Request) {
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

	res, err := h.burnoutAnalyzer.GetNoisyAlerts(ctx, schedID, timeRange)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": res,
		"total": len(res),
	})
}

// @Summary Export On-Call SLA, engineer fatigue, and noisy alert governance report as CSV or JSON
// @Router /api/v1/oncall/analytics/export [get]
// @Success 200 {file} file
func (h *Handler) exportBurnoutAnalytics(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	schedID, _ := strconv.ParseUint(r.URL.Query().Get("schedule_id"), 10, 64)
	timeRange := r.URL.Query().Get("time_range")
	if timeRange == "" {
		timeRange = "30d"
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		writeErr(w, fmt.Errorf("%w: format must be csv or json", errs.ErrInvalid))
		return
	}

	analytics, err := h.burnoutAnalyzer.GetBurnoutAnalytics(ctx, schedID, timeRange)
	if err != nil {
		writeErr(w, err)
		return
	}
	noisy, err := h.burnoutAnalyzer.GetNoisyAlerts(ctx, schedID, timeRange)
	if err != nil {
		writeErr(w, err)
		return
	}

	filename := fmt.Sprintf("opspilot-oncall-report-%s.%s", analytics.TimeRange, format)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")

	if format == "json" {
		payload := map[string]any{
			"analytics":    analytics,
			"noisy_alerts": noisy,
			"exported_at":  time.Now().UTC().Format(time.RFC3339),
		}
		raw, mErr := json.MarshalIndent(payload, "", "  ")
		if mErr != nil {
			writeErr(w, mErr)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		// Streaming attachment write after headers; partial network write errors cannot be converted to JSON errors.
		_, _ = w.Write(raw)
		return
	}

	csvBytes, csvErr := bizoncall.BuildAnalyticsCSV(analytics, noisy)
	if csvErr != nil {
		writeErr(w, csvErr)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// Streaming attachment write after headers; partial network write errors cannot be converted to JSON errors.
	_, _ = w.Write(csvBytes)
}
