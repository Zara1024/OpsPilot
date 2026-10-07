package oncall

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	errs "github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// getVoiceConfig returns the current voice gateway configuration with masked secrets.
func (h *Handler) getVoiceConfig(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	cfg, err := h.store.GetVoiceConfig(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}

	// Create a safe copy with masked credentials
	masked := *cfg
	if masked.AliyunAccessKeySecret != "" {
		masked.AliyunAccessKeySecret = maskSecret(masked.AliyunAccessKeySecret)
	}
	if masked.TencentSecretKey != "" {
		masked.TencentSecretKey = maskSecret(masked.TencentSecretKey)
	}

	writeJSON(w, http.StatusOK, masked)
}

// updateVoiceConfig updates the voice gateway configuration.
func (h *Handler) updateVoiceConfig(w http.ResponseWriter, r *http.Request) {
	_, ok := requireAuth(w, r)
	if !ok {
		return
	}

	var req voice.VoiceGatewayConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	// Read existing config to preserve secrets if masked value was submitted
	existing, _ := h.store.GetVoiceConfig(r.Context())
	if existing != nil {
		if req.AliyunAccessKeySecret == "" || strings.HasPrefix(req.AliyunAccessKeySecret, "*") {
			req.AliyunAccessKeySecret = existing.AliyunAccessKeySecret
		}
		if req.TencentSecretKey == "" || strings.HasPrefix(req.TencentSecretKey, "*") {
			req.TencentSecretKey = existing.TencentSecretKey
		}
	}

	if req.Provider == "" {
		req.Provider = "mock"
	}
	if req.AliyunRegion == "" {
		req.AliyunRegion = "cn-hangzhou"
	}
	if req.TencentRegion == "" {
		req.TencentRegion = "ap-guangzhou"
	}

	if err := h.store.UpdateVoiceConfig(r.Context(), &req); err != nil {
		writeErr(w, err)
		return
	}

	// Return masked result
	masked := req
	if masked.AliyunAccessKeySecret != "" {
		masked.AliyunAccessKeySecret = maskSecret(masked.AliyunAccessKeySecret)
	}
	if masked.TencentSecretKey != "" {
		masked.TencentSecretKey = maskSecret(masked.TencentSecretKey)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": masked,
	})
}

// testVoiceCall initiates a test voice call (either real via configured provider, or sandbox mock).
func (h *Handler) testVoiceCall(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireAuth(w, r)
	if !ok {
		return
	}

	var req voice.CallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
		return
	}

	// If phone is empty, lookup caller's phone
	if req.PhoneNumber == "" {
		usersMap, _ := h.store.GetUsersByIDs(r.Context(), []uint64{caller.UserID})
		if u, found := usersMap[caller.UserID]; found && u.Phone != "" {
			req.PhoneNumber = u.Phone
		} else {
			req.PhoneNumber = "13800000000"
		}
	}

	if req.Title == "" {
		req.Title = "核心生产集群负载突增告警"
	}
	if req.Urgency == "" {
		req.Urgency = "high"
	}
	if req.Severity == "" {
		req.Severity = "P1"
	}

	cfg, _ := h.store.GetVoiceConfig(r.Context())

	result, err := h.voiceGateway.SendVoiceCall(r.Context(), cfg, &req)
	if err != nil {
		// Even if provider fails, return details so frontend can show failure reasons
		if result != nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// aliyunVoiceCallback handles Aliyun voice status webhooks.
func (h *Handler) aliyunVoiceCallback(w http.ResponseWriter, r *http.Request) {
	// Acknowledge receipt
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code": 0, "msg": "success"}`))
}

// tencentVoiceCallback handles Tencent VMS voice status webhooks.
func (h *Handler) tencentVoiceCallback(w http.ResponseWriter, r *http.Request) {
	// Acknowledge receipt
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"result": 0, "errmsg": "OK"}`))
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "******"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}
