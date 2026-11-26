package oncall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type createMaintenanceWindowReq struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	ScheduleID  uint64                   `json:"schedule_id"`
	Matchers    []bizoncall.LabelMatcher `json:"matchers"`
	Action      string                   `json:"action"`
	StartsAt    time.Time                `json:"starts_at"`
	EndsAt      time.Time                `json:"ends_at"`
}

// MaintenanceWindowSummary enriches persisted maintenance windows with runtime status and schedule names.
type MaintenanceWindowSummary struct {
	*model.MaintenanceWindow
	ScheduleName  string                   `json:"schedule_name"`
	RuntimeStatus string                   `json:"runtime_status"`
	Matchers      []bizoncall.LabelMatcher `json:"matchers"`
}

// @Summary List On-Call scheduled maintenance windows
// @Router /api/v1/oncall/maintenance-windows [get]
// @Success 200 {object} map[string]any
func (h *Handler) listMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	rows, err := h.store.ListMaintenanceWindows(ctx, "", 200)
	if err != nil {
		writeErr(w, err)
		return
	}

	schedNames := h.loadScheduleNameMap(ctx)
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	now := time.Now().UTC()

	items := make([]MaintenanceWindowSummary, 0, len(rows))
	for _, mw := range rows {
		if mw == nil {
			continue
		}
		summary := buildMaintenanceSummary(mw, schedNames, now)
		if statusFilter != "" && statusFilter != "all" && summary.RuntimeStatus != statusFilter {
			continue
		}
		items = append(items, summary)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// @Summary Create a scheduled On-Call maintenance window
// @Router /api/v1/oncall/maintenance-windows [post]
// @Success 201 {object} MaintenanceWindowSummary
func (h *Handler) createMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}

	var req createMaintenanceWindowReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, fmt.Errorf("%w: name is required", errs.ErrInvalid))
		return
	}
	if req.StartsAt.IsZero() || req.EndsAt.IsZero() || !req.EndsAt.After(req.StartsAt) {
		writeErr(w, fmt.Errorf("%w: ends_at must be strictly after starts_at", errs.ErrInvalid))
		return
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = model.MaintenanceActionSuppressAll
	}
	if action != model.MaintenanceActionSuppressAll && action != model.MaintenanceActionSuppressVoice {
		writeErr(w, fmt.Errorf("%w: action must be suppress_all or suppress_voice", errs.ErrInvalid))
		return
	}

	cleanMatchers := make([]bizoncall.LabelMatcher, 0, len(req.Matchers))
	for _, m := range req.Matchers {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		if m.Operator == "" {
			m.Operator = bizoncall.OpEqual
		}
		cleanMatchers = append(cleanMatchers, m)
	}
	matcherBytes, err := json.Marshal(cleanMatchers)
	if err != nil {
		writeErr(w, fmt.Errorf("%w: invalid matchers", errs.ErrInvalid))
		return
	}

	mw := &model.MaintenanceWindow{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		ScheduleID:  req.ScheduleID,
		MatcherJSON: string(matcherBytes),
		Action:      action,
		StartsAt:    req.StartsAt.UTC(),
		EndsAt:      req.EndsAt.UTC(),
		Status:      model.MaintenanceStatusActive,
		CreatedBy:   caller.UserID,
	}
	if err := h.store.CreateMaintenanceWindow(r.Context(), mw); err != nil {
		writeErr(w, err)
		return
	}
	schedNames := h.loadScheduleNameMap(r.Context())
	writeJSON(w, http.StatusCreated, buildMaintenanceSummary(mw, schedNames, time.Now().UTC()))
}

// @Summary Cancel an active or scheduled maintenance window
// @Router /api/v1/oncall/maintenance-windows/{id}/cancel [post]
// @Success 200 {object} map[string]any
func (h *Handler) cancelMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.CancelMaintenanceWindow(r.Context(), id, caller.UserID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErr(w, errs.ErrNotFound)
			return
		}
		writeErr(w, err)
		return
	}
	updated, err := h.store.GetMaintenanceWindow(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	schedNames := h.loadScheduleNameMap(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"item": buildMaintenanceSummary(updated, schedNames, time.Now().UTC()),
	})
}

// @Summary Delete a maintenance window record
// @Router /api/v1/oncall/maintenance-windows/{id} [delete]
// @Success 200 {object} map[string]any
func (h *Handler) deleteMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAuth(w, r); !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.DeleteMaintenanceWindow(r.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErr(w, errs.ErrNotFound)
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) loadScheduleNameMap(ctx context.Context) map[uint64]string {
	out := map[uint64]string{}
	if h == nil || h.store == nil {
		return out
	}
	scheds, err := h.store.ListSchedules(ctx, 0, false)
	if err != nil {
		return out
	}
	for _, s := range scheds {
		if s != nil {
			out[s.ID] = s.Name
		}
	}
	return out
}

func buildMaintenanceSummary(mw *model.MaintenanceWindow, schedNames map[uint64]string, now time.Time) MaintenanceWindowSummary {
	schedName := "全局所有排班"
	if mw != nil && mw.ScheduleID > 0 {
		if name, ok := schedNames[mw.ScheduleID]; ok && name != "" {
			schedName = name
		} else {
			schedName = fmt.Sprintf("Schedule #%d", mw.ScheduleID)
		}
	}
	return MaintenanceWindowSummary{
		MaintenanceWindow: mw,
		ScheduleName:      schedName,
		RuntimeStatus:     bizoncall.ComputeMaintenanceRuntimeStatus(mw, now),
		Matchers:          bizoncall.ParseMaintenanceMatchers(mw),
	}
}
