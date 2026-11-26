package oncall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type upsertCalendarDayReq struct {
	Date    string `json:"date"`
	Region  string `json:"region"`
	DayType string `json:"day_type"`
	Name    string `json:"name"`
}

type syncCNCalendarReq struct {
	Year int `json:"year"`
}

// @Summary List statutory holidays and makeup workdays merged with custom overrides
// @Router /api/v1/oncall/calendar/days [get]
// @Success 200 {object} map[string]any
func (h *Handler) listCalendarDays(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	year, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("year")))
	if year <= 0 {
		year = time.Now().Year()
	}
	region := strings.TrimSpace(r.URL.Query().Get("region"))
	if region == "" {
		region = model.CalendarRegionCN
	}

	dbDays, err := h.store.ListCalendarDays(r.Context(), region, year)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := dbDays
	if strings.EqualFold(region, model.CalendarRegionCN) {
		items = bizoncall.MergeCNCalendarList(year, dbDays)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"year":   year,
		"region": region,
		"years":  bizoncall.BuiltinCNYears(),
		"items":  items,
		"total":  len(items),
	})
}

// @Summary Upsert a custom statutory holiday or makeup workday override
// @Router /api/v1/oncall/calendar/days [put]
// @Success 200 {object} map[string]any
func (h *Handler) upsertCalendarDay(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req upsertCalendarDayReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	dateStr := strings.TrimSpace(req.Date)
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		writeErr(w, fmt.Errorf("%w: date must use YYYY-MM-DD format", errs.ErrInvalid))
		return
	}
	dayType := strings.ToLower(strings.TrimSpace(req.DayType))
	if dayType != model.DayTypeHoliday && dayType != model.DayTypeWorkday {
		writeErr(w, fmt.Errorf("%w: day_type must be 'holiday' or 'workday'", errs.ErrInvalid))
		return
	}
	region := strings.TrimSpace(req.Region)
	if region == "" {
		region = model.CalendarRegionCN
	}

	saved, err := h.store.UpsertCalendarDay(r.Context(), &model.CalendarDay{
		Date:      dateStr,
		Region:    region,
		DayType:   dayType,
		Name:      strings.TrimSpace(req.Name),
		Source:    model.CalendarSourceCustom,
		CreatedBy: caller.UserID,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"item": saved,
	})
}

// @Summary Sync built-in Chinese State Council statutory holiday presets for a year
// @Router /api/v1/oncall/calendar/days/sync-cn [post]
// @Success 200 {object} map[string]any
func (h *Handler) syncCNCalendarDays(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAdmin(w, r)
	if !ok {
		return
	}

	var req syncCNCalendarReq
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
			return
		}
	}
	if req.Year <= 0 {
		req.Year = time.Now().Year()
	}

	presets := bizoncall.BuiltinCNCalendarDays(req.Year)
	if len(presets) == 0 {
		writeErr(w, fmt.Errorf("%w: no built-in CN calendar presets for year %d", errs.ErrInvalid, req.Year))
		return
	}

	rows := make([]*model.CalendarDay, 0, len(presets))
	for i := range presets {
		cp := presets[i]
		cp.CreatedBy = caller.UserID
		rows = append(rows, &cp)
	}
	synced, err := h.store.BulkUpsertCalendarDays(r.Context(), rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"year":         req.Year,
		"synced_count": synced,
	})
}

// @Summary Delete a custom calendar holiday or makeup workday override
// @Router /api/v1/oncall/calendar/days/{id} [delete]
// @Success 200 {object} map[string]any
func (h *Handler) deleteCalendarDay(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	if err := h.store.DeleteCalendarDay(r.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErr(w, errs.ErrNotFound)
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
