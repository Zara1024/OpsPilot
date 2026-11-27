// Package aicost 提供 AI 成本核算、配额管理与熔断器控制的 HTTP 接口。
package aicost

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	bizaicost "github.com/Zara1024/OpsPilot/internal/manager/biz/aicost"
	"github.com/Zara1024/OpsPilot/internal/manager/data/aicost/store"
	"github.com/Zara1024/OpsPilot/internal/pkg/auditctx"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

// Handler 挂载管理员侧 AI 成本与熔断治理路由。
type Handler struct {
	svc *bizaicost.Service
}

// NewHandler 构造 aicost HTTP Handler。
func NewHandler(svc *bizaicost.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterProtected 将 AI 成本与配额接口挂载到需鉴权路由树（管理员可用）。
func (h *Handler) RegisterProtected(r chi.Router) {
	if h == nil || h.svc == nil {
		return
	}
	r.Get("/v1/ai-cost/summary", h.summary)
	r.Get("/v1/ai-cost/logs", h.listLogs)
	r.Get("/v1/ai-cost/quotas", h.listQuotas)
	r.Post("/v1/ai-cost/quotas", h.createQuota)
	r.Put("/v1/ai-cost/quotas/{id}", h.updateQuota)
	r.Delete("/v1/ai-cost/quotas/{id}", h.deleteQuota)
	r.Post("/v1/ai-cost/quotas/{id}/trip", h.tripCircuit)
	r.Post("/v1/ai-cost/quotas/{id}/reset", h.resetCircuit)
	r.Get("/v1/ai-cost/prices", h.listPrices)
	r.Put("/v1/ai-cost/prices", h.upsertPrice)
	r.Delete("/v1/ai-cost/prices/{id}", h.deletePrice)
}

// @Summary Get AI token usage, cost breakdown and quota circuit summary
// @Tags ai-cost
// @Router /api/v1/ai-cost/summary [get]
// @Success 200 {object} bizaicost.SummaryView
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	days := 7
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeErr(w, errs.ErrInvalid)
			return
		}
		days = v
	}
	out, err := h.svc.Summary(r.Context(), days)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary List paginated AI LLM request ledger entries
// @Tags ai-cost
// @Router /api/v1/ai-cost/logs [get]
// @Success 200 {object} map[string]any
func (h *Handler) listLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	q := r.URL.Query()
	var filter store.LogFilter
	filter.Source = q.Get("source")
	filter.Model = q.Get("model")
	filter.Status = q.Get("status")
	if raw := q.Get("user_id"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeErr(w, errs.ErrInvalid)
			return
		}
		filter.UserID = v
	}
	if raw := q.Get("org_id"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeErr(w, errs.ErrInvalid)
			return
		}
		filter.OrgID = v
	}
	if raw := q.Get("days"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days <= 0 {
			writeErr(w, errs.ErrInvalid)
			return
		}
		since := time.Now().UTC().AddDate(0, 0, -(days - 1))
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)
		filter.Since = &since
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(w, errs.ErrInvalid)
			return
		}
		filter.Limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(w, errs.ErrInvalid)
			return
		}
		filter.Offset = v
	}
	rows, total, err := h.svc.ListLogs(r.Context(), filter)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "total": total})
}

// @Summary List AI cost quotas and circuit states
// @Tags ai-cost
// @Router /api/v1/ai-cost/quotas [get]
// @Success 200 {object} map[string]any
func (h *Handler) listQuotas(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	items, err := h.svc.ListQuotas(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// @Summary Create AI token or USD cost quota rule
// @Tags ai-cost
// @Router /api/v1/ai-cost/quotas [post]
// @Success 201 {object} bizaicost.QuotaView
func (h *Handler) createQuota(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var in bizaicost.QuotaInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := h.svc.CreateQuota(r.Context(), actor.UserID, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(out.ID, 10),
		ResourceName: out.Name,
		Status:       auditctx.StatusSuccess,
		Payload: map[string]any{
			"entity":     "ai_cost_quota",
			"action":     "create",
			"scope_type": out.ScopeType,
			"scope_key":  out.ScopeKey,
		},
	})
	writeJSON(w, http.StatusCreated, out)
}

// @Summary Update AI token or USD cost quota rule
// @Tags ai-cost
// @Router /api/v1/ai-cost/quotas/{id} [put]
// @Success 200 {object} bizaicost.QuotaView
func (h *Handler) updateQuota(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in bizaicost.QuotaInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := h.svc.UpdateQuota(r.Context(), id, actor.UserID, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(out.ID, 10),
		ResourceName: out.Name,
		Status:       auditctx.StatusSuccess,
		Payload: map[string]any{
			"entity":  "ai_cost_quota",
			"enabled": out.Enabled,
		},
	})
	writeJSON(w, http.StatusOK, out)
}

// @Summary Delete AI cost quota rule
// @Tags ai-cost
// @Router /api/v1/ai-cost/quotas/{id} [delete]
// @Success 204
func (h *Handler) deleteQuota(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.svc.DeleteQuota(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingDelete,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(id, 10),
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "ai_cost_quota"},
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Manually trip AI quota circuit breaker
// @Tags ai-cost
// @Router /api/v1/ai-cost/quotas/{id}/trip [post]
// @Success 200 {object} bizaicost.QuotaView
func (h *Handler) tripCircuit(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &in) // 空请求体允许使用默认原因
	out, err := h.svc.TripCircuit(r.Context(), id, actor.UserID, in.Reason)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(id, 10),
		ResourceName: out.Name,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "ai_cost_quota", "action": "trip_circuit", "reason": out.CircuitReason},
	})
	writeJSON(w, http.StatusOK, out)
}

// @Summary Manually reset AI quota circuit breaker
// @Tags ai-cost
// @Router /api/v1/ai-cost/quotas/{id}/reset [post]
// @Success 200 {object} bizaicost.QuotaView
func (h *Handler) resetCircuit(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	out, err := h.svc.ResetCircuit(r.Context(), id, actor.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(id, 10),
		ResourceName: out.Name,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "ai_cost_quota", "action": "reset_circuit"},
	})
	writeJSON(w, http.StatusOK, out)
}

// @Summary List custom and built-in LLM model pricing catalog
// @Tags ai-cost
// @Router /api/v1/ai-cost/prices [get]
// @Success 200 {object} bizaicost.PricesCatalog
func (h *Handler) listPrices(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	out, err := h.svc.ListPrices(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary Upsert custom LLM model pricing override
// @Tags ai-cost
// @Router /api/v1/ai-cost/prices [put]
// @Success 200 {object} bizaicost.PriceEntry
func (h *Handler) upsertPrice(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	var in bizaicost.PriceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := h.svc.UpsertPrice(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(out.ID, 10),
		ResourceName: out.Provider + ":" + out.ModelPattern,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "ai_model_price"},
	})
	writeJSON(w, http.StatusOK, out)
}

// @Summary Delete custom LLM model pricing override
// @Tags ai-cost
// @Router /api/v1/ai-cost/prices/{id} [delete]
// @Success 204
func (h *Handler) deletePrice(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.svc.DeletePrice(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingDelete,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(id, 10),
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "ai_model_price"},
	})
	w.WriteHeader(http.StatusNoContent)
}

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func requireAdmin(w http.ResponseWriter, r *http.Request) (tenantctx.Tenant, bool) {
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return tenantctx.Tenant{}, false
	}
	if !t.IsSuperuser && strings.ToLower(strings.TrimSpace(t.Role)) != "admin" {
		writeErr(w, errs.ErrForbidden)
		return tenantctx.Tenant{}, false
	}
	return t, true
}

func parseID(r *http.Request) (uint64, error) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		return 0, errs.ErrInvalid
	}
	return id, nil
}

func decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return errs.ErrInvalid
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return errs.ErrInvalid
	}
	return nil
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
		code, slug = http.StatusNotFound, "not-found"
	case errors.Is(err, errs.ErrInvalid):
		code, slug = http.StatusBadRequest, "invalid-argument"
	case errors.Is(err, errs.ErrConflict):
		code, slug = http.StatusConflict, "conflict"
	case errors.Is(err, errs.ErrNotWiredYet):
		code, slug = http.StatusNotImplemented, "not-wired"
	}
	writeJSON(w, code, errorBody{Error: err.Error(), Code: slug})
}
