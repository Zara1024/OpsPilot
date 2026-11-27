package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Zara1024/OpsPilot/internal/iam/biz/rbac"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/auditctx"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type rbacGrantDTO struct {
	ID              uint64    `json:"id"`
	SubjectType     string    `json:"subject_type"`
	SubjectID       uint64    `json:"subject_id"`
	ResourceType    string    `json:"resource_type"`
	ResourcePattern string    `json:"resource_pattern"`
	Actions         string    `json:"actions"`
	Effect          string    `json:"effect"`
	Description     string    `json:"description,omitempty"`
	CreatedBy       uint64    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toRBACGrantDTO(g *model.ResourceGrant) rbacGrantDTO {
	return rbacGrantDTO{
		ID:              g.ID,
		SubjectType:     g.SubjectType,
		SubjectID:       g.SubjectID,
		ResourceType:    g.ResourceType,
		ResourcePattern: g.ResourcePattern,
		Actions:         g.Actions,
		Effect:          g.Effect,
		Description:     g.Description,
		CreatedBy:       g.CreatedBy,
		CreatedAt:       g.CreatedAt,
		UpdatedAt:       g.UpdatedAt,
	}
}

// @Summary List resource-level RBAC grants
// @Router /api/v1/iam/rbac/grants [get]
// @Success 200 {object} map[string]any
func (h *Handler) listRBACGrants(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	rbacSvc := h.svc.RBAC()
	if rbacSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	q := r.URL.Query()
	var subjID uint64
	if raw := q.Get("subject_id"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeErr(w, errs.ErrInvalid)
			return
		}
		subjID = v
	}
	rows, err := rbacSvc.ListGrants(r.Context(), rbac.ListFilter{
		SubjectType:  q.Get("subject_type"),
		SubjectID:    subjID,
		ResourceType: q.Get("resource_type"),
		Effect:       q.Get("effect"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]rbacGrantDTO, 0, len(rows))
	for _, g := range rows {
		items = append(items, toRBACGrantDTO(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// @Summary Create a resource-level RBAC grant
// @Router /api/v1/iam/rbac/grants [post]
// @Success 201 {object} rbacGrantDTO
func (h *Handler) createRBACGrant(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	rbacSvc := h.svc.RBAC()
	if rbacSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, _ := tenantctx.From(r.Context())
	var in rbac.GrantInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	created, err := rbacSvc.CreateGrant(r.Context(), t.UserID, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(created.ID, 10),
		ResourceName: created.ResourcePattern,
		Status:       auditctx.StatusSuccess,
		Payload: map[string]any{
			"entity":        "rbac_grant",
			"subject_type":  created.SubjectType,
			"subject_id":    created.SubjectID,
			"resource_type": created.ResourceType,
			"effect":        created.Effect,
			"actions":       created.Actions,
		},
	})
	writeJSON(w, http.StatusCreated, toRBACGrantDTO(created))
}

// @Summary Update a resource-level RBAC grant
// @Router /api/v1/iam/rbac/grants/{id} [put]
// @Success 200 {object} rbacGrantDTO
func (h *Handler) updateRBACGrant(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	rbacSvc := h.svc.RBAC()
	if rbacSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in rbac.GrantInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	updated, err := rbacSvc.UpdateGrant(r.Context(), id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(updated.ID, 10),
		ResourceName: updated.ResourcePattern,
		Status:       auditctx.StatusSuccess,
		Payload: map[string]any{
			"entity":        "rbac_grant",
			"subject_type":  updated.SubjectType,
			"subject_id":    updated.SubjectID,
			"resource_type": updated.ResourceType,
			"effect":        updated.Effect,
			"actions":       updated.Actions,
		},
	})
	writeJSON(w, http.StatusOK, toRBACGrantDTO(updated))
}

// @Summary Delete a resource-level RBAC grant
// @Router /api/v1/iam/rbac/grants/{id} [delete]
// @Success 204
func (h *Handler) deleteRBACGrant(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	rbacSvc := h.svc.RBAC()
	if rbacSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := rbacSvc.DeleteGrant(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingDelete,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(id, 10),
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "rbac_grant"},
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Simulate resource-level RBAC decision for a user
// @Router /api/v1/iam/rbac/check [post]
// @Success 200 {object} rbac.Decision
func (h *Handler) checkRBACAccess(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	rbacSvc := h.svc.RBAC()
	if rbacSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	var in struct {
		UserID       uint64   `json:"user_id"`
		ResourceType string   `json:"resource_type"`
		ResourceKeys []string `json:"resource_keys"`
		Action       string   `json:"action"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	dec, err := rbacSvc.Check(r.Context(), in.UserID, false, in.ResourceType, in.ResourceKeys, in.Action)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dec)
}
