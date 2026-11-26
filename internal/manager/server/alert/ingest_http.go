package alert

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	bizalert "github.com/Zara1024/OpsPilot/internal/manager/biz/alert"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// RegisterPublic mounts unauthenticated external webhook ingest and heartbeat ping endpoints under /api.
func (h *Handler) RegisterPublic(r chi.Router) {
	if h.ingestSvc == nil {
		return
	}
	r.Post("/v1/alerts/ingest/{provider}/{token}", h.ingestWebhook)
	r.Post("/v1/alerts/ingest/{provider}", h.ingestWebhook)
	r.Get("/v1/alerts/heartbeat/{token}", h.pingHeartbeatPublic)
	r.Post("/v1/alerts/heartbeat/{token}", h.pingHeartbeatPublic)
}

func (h *Handler) registerIngestProtected(r chi.Router) {
	if h.ingestSvc == nil {
		return
	}
	r.Get("/v1/alerts/ingest-sources", h.listIngestSources)
	r.Post("/v1/alerts/ingest-sources", h.createIngestSource)
	r.Put("/v1/alerts/ingest-sources/{id}", h.updateIngestSource)
	r.Delete("/v1/alerts/ingest-sources/{id}", h.deleteIngestSource)
	r.Post("/v1/alerts/ingest-sources/{id}/rotate-token", h.rotateIngestSourceToken)
	r.Post("/v1/alerts/ingest-sources/{id}/test", h.testIngestSource)

	r.Get("/v1/alerts/heartbeats", h.listHeartbeats)
	r.Post("/v1/alerts/heartbeats", h.createHeartbeat)
	r.Put("/v1/alerts/heartbeats/{id}", h.updateHeartbeat)
	r.Delete("/v1/alerts/heartbeats/{id}", h.deleteHeartbeat)
	r.Post("/v1/alerts/heartbeats/{id}/rotate-token", h.rotateHeartbeatToken)
	r.Post("/v1/alerts/heartbeats/{id}/ping", h.manualPingHeartbeat)
}

// @Summary Receive external monitoring webhook alert payload
// @Tags alerts-ingest
// @Router /v1/alerts/ingest/{provider}/{token} [post]
// @Success 200 {object} bizalert.IngestResult
func (h *Handler) ingestWebhook(w http.ResponseWriter, r *http.Request) {
	if h.ingestSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{
				Error: "webhook payload exceeds 1MiB limit",
				Code:  "payload-too-large",
			})
			return
		}
		writeErr(w, errs.ErrInvalid)
		return
	}

	provider := chi.URLParam(r, "provider")
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-OpsPilot-Token"))
	}
	if token == "" {
		authHdr := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authHdr), "bearer ") {
			token = strings.TrimSpace(authHdr[7:])
		}
	}

	res, err := h.ingestSvc.IngestWebhook(r.Context(), bizalert.IngestWebhookInput{
		Provider:    provider,
		Token:       token,
		ContentType: r.Header.Get("Content-Type"),
		Headers:     r.Header,
		Body:        body,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// @Summary Receive dead-man's switch heartbeat ping
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeat/{token} [get]
// @Success 200 {object} bizalert.HeartbeatPingResult
func (h *Handler) pingHeartbeatPublic(w http.ResponseWriter, r *http.Request) {
	if h.ingestSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	token := chi.URLParam(r, "token")
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))
	if msg == "" {
		msg = strings.TrimSpace(r.URL.Query().Get("message"))
	}
	if msg == "" && r.Method == http.MethodPost && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if body, err := io.ReadAll(r.Body); err == nil && len(body) > 0 {
			var payload struct {
				Message string `json:"message"`
				Msg     string `json:"msg"`
			}
			if json.Unmarshal(body, &payload) == nil {
				msg = strings.TrimSpace(payload.Message)
				if msg == "" {
					msg = strings.TrimSpace(payload.Msg)
				}
			}
		}
	}

	res, err := h.ingestSvc.PingHeartbeatByToken(r.Context(), token, extractClientIP(r), msg)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// @Summary List configured external alert ingest sources
// @Tags alerts-ingest
// @Router /v1/alerts/ingest-sources [get]
// @Success 200 {object} map[string]any
func (h *Handler) listIngestSources(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerFromRequest(r)
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	items, err := h.ingestSvc.ListIngestSources(r.Context(), caller.Role == "admin")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// @Summary Create an external alert ingest source
// @Tags alerts-ingest
// @Router /v1/alerts/ingest-sources [post]
// @Success 201 {object} bizalert.IngestSourceView
func (h *Handler) createIngestSource(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var req bizalert.IngestSourceInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	created, err := h.ingestSvc.CreateIngestSource(r.Context(), caller.UserID, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// @Summary Update an external alert ingest source
// @Tags alerts-ingest
// @Router /v1/alerts/ingest-sources/{id} [put]
// @Success 200 {object} bizalert.IngestSourceView
func (h *Handler) updateIngestSource(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req bizalert.IngestSourceInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	updated, err := h.ingestSvc.UpdateIngestSource(r.Context(), id, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// @Summary Delete an external alert ingest source
// @Tags alerts-ingest
// @Router /v1/alerts/ingest-sources/{id} [delete]
// @Success 200 {object} map[string]any
func (h *Handler) deleteIngestSource(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.ingestSvc.DeleteIngestSource(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// @Summary Rotate ingest webhook token for an external alert source
// @Tags alerts-ingest
// @Router /v1/alerts/ingest-sources/{id}/rotate-token [post]
// @Success 200 {object} bizalert.IngestSourceView
func (h *Handler) rotateIngestSourceToken(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	updated, err := h.ingestSvc.RotateIngestSourceToken(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// @Summary Simulate firing or resolving an alert against an ingest source
// @Tags alerts-ingest
// @Router /v1/alerts/ingest-sources/{id}/test [post]
// @Success 200 {object} bizalert.IngestResult
func (h *Handler) testIngestSource(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if r.Body != nil {
		// Optional body: empty body defaults to "fire".
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	res, err := h.ingestSvc.TestIngestSource(r.Context(), id, req.Action)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// @Summary List dead-man's switch heartbeat monitors
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeats [get]
// @Success 200 {object} map[string]any
func (h *Handler) listHeartbeats(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerFromRequest(r)
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	items, err := h.ingestSvc.ListHeartbeats(r.Context(), caller.Role == "admin")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// @Summary Create a dead-man's switch heartbeat monitor
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeats [post]
// @Success 201 {object} bizalert.HeartbeatView
func (h *Handler) createHeartbeat(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var req bizalert.HeartbeatInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	created, err := h.ingestSvc.CreateHeartbeat(r.Context(), caller.UserID, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// @Summary Update a dead-man's switch heartbeat monitor
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeats/{id} [put]
// @Success 200 {object} bizalert.HeartbeatView
func (h *Handler) updateHeartbeat(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req bizalert.HeartbeatInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, errs.ErrInvalid)
		return
	}
	updated, err := h.ingestSvc.UpdateHeartbeat(r.Context(), id, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// @Summary Delete a dead-man's switch heartbeat monitor
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeats/{id} [delete]
// @Success 200 {object} map[string]any
func (h *Handler) deleteHeartbeat(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.ingestSvc.DeleteHeartbeat(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// @Summary Rotate heartbeat ping token
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeats/{id}/rotate-token [post]
// @Success 200 {object} bizalert.HeartbeatView
func (h *Handler) rotateHeartbeatToken(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	updated, err := h.ingestSvc.RotateHeartbeatToken(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// @Summary Trigger a manual ping against a heartbeat monitor from console
// @Tags alerts-heartbeat
// @Router /v1/alerts/heartbeats/{id}/ping [post]
// @Success 200 {object} bizalert.HeartbeatPingResult
func (h *Handler) manualPingHeartbeat(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if r.Body != nil {
		// Optional message payload.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	res, err := h.ingestSvc.ManualPingHeartbeat(r.Context(), id, req.Message)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func extractClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if first != "" {
			return first
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
