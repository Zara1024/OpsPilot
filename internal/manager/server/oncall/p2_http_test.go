package oncall

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	oncallmodel "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

func TestCalendarDays_HTTP_SyncOverrideAndRBAC(t *testing.T) {
	_, _, _, r := newTestHTTPHandler(t)
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}
	userTenant := tenantctx.Tenant{UserID: 2, Email: "alice@opspilot.local", Role: iammodel.RoleUser}

	// Authenticated user can read merged CN calendar presets.
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqWithTenant(http.MethodGet, "/v1/oncall/calendar/days?year=2026&region=CN", "", userTenant))
	if wList.Code != http.StatusOK {
		t.Fatalf("GET /calendar/days failed: %d (%s)", wList.Code, wList.Body.String())
	}
	if !strings.Contains(wList.Body.String(), "2026-01-01") || !strings.Contains(wList.Body.String(), "2026-01-04") {
		t.Fatalf("expected built-in 2026 CN holiday & makeup workday presets, got: %s", wList.Body.String())
	}

	// Non-admin writes must return 403.
	wForbidSync := httptest.NewRecorder()
	r.ServeHTTP(wForbidSync, reqWithTenant(http.MethodPost, "/v1/oncall/calendar/days/sync-cn", `{"year":2026}`, userTenant))
	if wForbidSync.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin sync-cn, got %d", wForbidSync.Code)
	}
	wForbidPut := httptest.NewRecorder()
	r.ServeHTTP(wForbidPut, reqWithTenant(http.MethodPut, "/v1/oncall/calendar/days", `{"date":"2026-07-01","day_type":"holiday","name":"Demo"}`, userTenant))
	if wForbidPut.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin PUT calendar day, got %d", wForbidPut.Code)
	}

	// Admin sync-cn populates DB.
	wSync := httptest.NewRecorder()
	r.ServeHTTP(wSync, reqWithTenant(http.MethodPost, "/v1/oncall/calendar/days/sync-cn", `{"year":2026}`, adminTenant))
	if wSync.Code != http.StatusOK || !strings.Contains(wSync.Body.String(), `"synced_count"`) {
		t.Fatalf("expected 200 on sync-cn, got %d (%s)", wSync.Code, wSync.Body.String())
	}

	// Admin custom upsert then delete.
	wPut := httptest.NewRecorder()
	r.ServeHTTP(wPut, reqWithTenant(http.MethodPut, "/v1/oncall/calendar/days", `{"date":"2026-07-01","day_type":"holiday","name":"公司周年纪念日"}`, adminTenant))
	if wPut.Code != http.StatusOK {
		t.Fatalf("PUT calendar day failed: %d (%s)", wPut.Code, wPut.Body.String())
	}
	var putResp struct {
		OK   bool                    `json:"ok"`
		Item oncallmodel.CalendarDay `json:"item"`
	}
	if err := json.Unmarshal(wPut.Body.Bytes(), &putResp); err != nil || putResp.Item.ID == 0 {
		t.Fatalf("decode putResp: %v body=%s", err, wPut.Body.String())
	}

	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqWithTenant(http.MethodDelete, "/v1/oncall/calendar/days/"+url.PathEscape(strconvUint(putResp.Item.ID)), "", adminTenant))
	if wDel.Code != http.StatusOK {
		t.Fatalf("DELETE calendar day failed: %d (%s)", wDel.Code, wDel.Body.String())
	}
}

func TestMaintenanceWindows_HTTP_ValidationLifecycle(t *testing.T) {
	_, _, _, r := newTestHTTPHandler(t)
	userTenant := tenantctx.Tenant{UserID: 2, Email: "alice@opspilot.local", Role: iammodel.RoleUser}

	now := time.Now().UTC().Truncate(time.Second)
	// Invalid end <= start must fail 400.
	badBody, _ := json.Marshal(map[string]any{
		"name":      "Bad Window",
		"starts_at": now.Add(time.Hour),
		"ends_at":   now,
	})
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqWithTenant(http.MethodPost, "/v1/oncall/maintenance-windows", string(badBody), userTenant))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid time window, got %d (%s)", wBad.Code, wBad.Body.String())
	}

	// Create running window.
	okBody, _ := json.Marshal(map[string]any{
		"name":        "Core Network Cutover",
		"description": "BGP peer maintenance",
		"schedule_id": 0,
		"action":      "suppress_voice",
		"matchers":    []map[string]string{{"name": "service", "operator": "=", "value": "edge"}},
		"starts_at":   now.Add(-5 * time.Minute),
		"ends_at":     now.Add(45 * time.Minute),
	})
	wCreate := httptest.NewRecorder()
	r.ServeHTTP(wCreate, reqWithTenant(http.MethodPost, "/v1/oncall/maintenance-windows", string(okBody), userTenant))
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("POST maintenance-windows failed: %d (%s)", wCreate.Code, wCreate.Body.String())
	}
	var created MaintenanceWindowSummary
	if err := json.Unmarshal(wCreate.Body.Bytes(), &created); err != nil || created.RuntimeStatus != "running" {
		t.Fatalf("unexpected created summary: %+v err=%v", created, err)
	}

	// Filter running.
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqWithTenant(http.MethodGet, "/v1/oncall/maintenance-windows?status=running", "", userTenant))
	if wList.Code != http.StatusOK || !strings.Contains(wList.Body.String(), "Core Network Cutover") {
		t.Fatalf("expected running maintenance window in list, got %d (%s)", wList.Code, wList.Body.String())
	}

	// Cancel & verify status.
	idStr := strconvUint(created.ID)
	wCancel := httptest.NewRecorder()
	r.ServeHTTP(wCancel, reqWithTenant(http.MethodPost, "/v1/oncall/maintenance-windows/"+idStr+"/cancel", "{}", userTenant))
	if wCancel.Code != http.StatusOK || !strings.Contains(wCancel.Body.String(), `"cancelled"`) {
		t.Fatalf("cancel maintenance window failed: %d (%s)", wCancel.Code, wCancel.Body.String())
	}

	// Delete then confirm 404.
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqWithTenant(http.MethodDelete, "/v1/oncall/maintenance-windows/"+idStr, "", userTenant))
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete maintenance window failed: %d (%s)", wDel.Code, wDel.Body.String())
	}
	wDelAgain := httptest.NewRecorder()
	r.ServeHTTP(wDelAgain, reqWithTenant(http.MethodDelete, "/v1/oncall/maintenance-windows/"+idStr, "", userTenant))
	if wDelAgain.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting already deleted window, got %d", wDelAgain.Code)
	}
}

func TestAnalyticsExport_HTTP_CSVAndJSON(t *testing.T) {
	_, _, db, r := newTestHTTPHandler(t)
	userTenant := tenantctx.Tenant{UserID: 2, Email: "alice@opspilot.local", Role: iammodel.RoleUser}

	now := time.Now().UTC()
	ackAt := now.Add(-15 * time.Minute)
	resAt := now.Add(-10 * time.Minute)
	_ = db.Create(&alertmodel.Incident{
		ID: 801, Rule: "cpu_high", RuleName: "CPU 负载过高", Severity: "critical", Title: "CPU > 95%",
		Status: alertmodel.StatusResolved, DedupeKey: "exp-801", EventCount: 3,
		FirstFiredAt: now.Add(-20 * time.Minute), LastFiredAt: now.Add(-10 * time.Minute),
		AcknowledgedAt: &ackAt, ResolvedAt: &resAt,
	}).Error

	wCSV := httptest.NewRecorder()
	r.ServeHTTP(wCSV, reqWithTenant(http.MethodGet, "/v1/oncall/analytics/export?time_range=30d&format=csv", "", userTenant))
	if wCSV.Code != http.StatusOK {
		t.Fatalf("export CSV failed: %d (%s)", wCSV.Code, wCSV.Body.String())
	}
	if !strings.Contains(wCSV.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(wCSV.Header().Get("Content-Disposition"), "opspilot-oncall-report-30d.csv") ||
		!bytes.HasPrefix(wCSV.Body.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("unexpected CSV headers/body: ct=%q cd=%q", wCSV.Header().Get("Content-Type"), wCSV.Header().Get("Content-Disposition"))
	}

	wJSON := httptest.NewRecorder()
	r.ServeHTTP(wJSON, reqWithTenant(http.MethodGet, "/v1/oncall/analytics/export?time_range=7d&format=json", "", userTenant))
	if wJSON.Code != http.StatusOK || !strings.Contains(wJSON.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("export JSON failed: %d ct=%q", wJSON.Code, wJSON.Header().Get("Content-Type"))
	}
	var parsed map[string]any
	if err := json.Unmarshal(wJSON.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid exported JSON: %v", err)
	}
	if parsed["analytics"] == nil || parsed["exported_at"] == nil {
		t.Fatalf("expected analytics and exported_at keys in JSON export, got %+v", parsed)
	}

	wBadFmt := httptest.NewRecorder()
	r.ServeHTTP(wBadFmt, reqWithTenant(http.MethodGet, "/v1/oncall/analytics/export?format=xml", "", userTenant))
	if wBadFmt.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unsupported export format, got %d", wBadFmt.Code)
	}
}

func TestCallbackTokenRotation_HTTP_InvalidatesOldTokens(t *testing.T) {
	_, s, _, r := newTestHTTPHandler(t)
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}
	userTenant := tenantctx.Tenant{UserID: 2, Email: "alice@opspilot.local", Role: iammodel.RoleUser}

	// Seed voice & channel configs with secrets to ensure secrets are preserved across rotation.
	_ = s.UpdateVoiceConfig(adminTenantContext(), &voice.VoiceGatewayConfig{
		Provider:              "aliyun",
		DispatchEnabled:       true,
		AliyunAccessKeyID:     "LTAI-voice",
		AliyunAccessKeySecret: "keep-voice-secret",
	})
	_ = s.UpdateChannelConfig(adminTenantContext(), &channels.ChannelGatewayConfig{
		FeishuVerificationToken: "keep-feishu-verify",
		SMTPPassword:            "keep-smtp-secret",
	})

	oldVoiceTok, err := s.EnsureVoiceWebhookToken(adminTenantContext())
	if err != nil || oldVoiceTok == "" {
		t.Fatalf("EnsureVoiceWebhookToken: %v", err)
	}
	oldChanTok, err := s.EnsureChatOpsCallbackToken(adminTenantContext())
	if err != nil || oldChanTok == "" {
		t.Fatalf("EnsureChatOpsCallbackToken: %v", err)
	}

	// Non-admin must get 403 on both rotation endpoints.
	wForbidV := httptest.NewRecorder()
	r.ServeHTTP(wForbidV, reqWithTenant(http.MethodPost, "/v1/oncall/voice/callback-token/rotate", "{}", userTenant))
	if wForbidV.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-admin voice rotate, got %d", wForbidV.Code)
	}
	wForbidC := httptest.NewRecorder()
	r.ServeHTTP(wForbidC, reqWithTenant(http.MethodPost, "/v1/oncall/channels/callback-token/rotate", "{}", userTenant))
	if wForbidC.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-admin channel rotate, got %d", wForbidC.Code)
	}

	// Rotate voice token -> returns new callback URLs, preserves Aliyun secret, rejects old token.
	wRotVoice := httptest.NewRecorder()
	r.ServeHTTP(wRotVoice, reqWithTenant(http.MethodPost, "/v1/oncall/voice/callback-token/rotate", "{}", adminTenant))
	if wRotVoice.Code != http.StatusOK {
		t.Fatalf("rotate voice token failed: %d (%s)", wRotVoice.Code, wRotVoice.Body.String())
	}
	var vResp struct {
		OK     bool                   `json:"ok"`
		Config voice.VoiceGatewayConfig `json:"config"`
	}
	_ = json.Unmarshal(wRotVoice.Body.Bytes(), &vResp)
	if !vResp.Config.AliyunSecretConfigured || vResp.Config.CallbackURLs["aliyun"] == "" {
		t.Fatalf("expected AliyunSecretConfigured=true and new callback_urls, got %+v", vResp.Config)
	}
	newVoiceURL, _ := url.Parse(vResp.Config.CallbackURLs["aliyun"])
	newVoiceTok := newVoiceURL.Query().Get("token")
	if newVoiceTok == "" || newVoiceTok == oldVoiceTok {
		t.Fatalf("expected rotated voice token != old (%q), got %q", oldVoiceTok, newVoiceTok)
	}

	// Verify old voice token fails 401 and new token reaches callback (200 on valid JSON).
	wOldVoiceCB := httptest.NewRecorder()
	r.ServeHTTP(wOldVoiceCB, httptest.NewRequest(http.MethodPost, "/v1/oncall/voice/callback/aliyun?token="+url.QueryEscape(oldVoiceTok), strings.NewReader(`{"call_id":"none"}`)))
	if wOldVoiceCB.Code != http.StatusUnauthorized {
		t.Fatalf("expected old voice token 401 after rotation, got %d", wOldVoiceCB.Code)
	}
	wNewVoiceCB := httptest.NewRecorder()
	r.ServeHTTP(wNewVoiceCB, httptest.NewRequest(http.MethodPost, "/v1/oncall/voice/callback/aliyun?token="+url.QueryEscape(newVoiceTok), strings.NewReader(`{"call_id":"none"}`)))
	if wNewVoiceCB.Code != http.StatusOK {
		t.Fatalf("expected new voice token callback 200, got %d (%s)", wNewVoiceCB.Code, wNewVoiceCB.Body.String())
	}

	// Rotate channel token -> rejects old token, keeps FeishuVerificationConfigured, accepts new token.
	wRotChan := httptest.NewRecorder()
	r.ServeHTTP(wRotChan, reqWithTenant(http.MethodPost, "/v1/oncall/channels/callback-token/rotate", "{}", adminTenant))
	if wRotChan.Code != http.StatusOK {
		t.Fatalf("rotate channel token failed: %d (%s)", wRotChan.Code, wRotChan.Body.String())
	}
	var cResp struct {
		OK     bool                          `json:"ok"`
		Config channels.ChannelGatewayConfig `json:"config"`
	}
	_ = json.Unmarshal(wRotChan.Body.Bytes(), &cResp)
	if !cResp.Config.FeishuVerificationConfigured || !cResp.Config.SMTPPasswordConfigured || cResp.Config.CallbackURLs["feishu"] == "" {
		t.Fatalf("expected existing channel secrets preserved & callback_urls updated, got %+v", cResp.Config)
	}
	newChanURL, _ := url.Parse(cResp.Config.CallbackURLs["feishu"])
	newChanTok := newChanURL.Query().Get("token")
	if newChanTok == "" || newChanTok == oldChanTok {
		t.Fatalf("expected rotated channel token != old (%q), got %q", oldChanTok, newChanTok)
	}

	feishuChallenge := `{"type":"url_verification","challenge":"ok_ping"}`
	wOldFeishuCB := httptest.NewRecorder()
	r.ServeHTTP(wOldFeishuCB, httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/callback/feishu?token="+url.QueryEscape(oldChanTok), strings.NewReader(feishuChallenge)))
	if wOldFeishuCB.Code != http.StatusUnauthorized {
		t.Fatalf("expected old channel token 401 after rotation, got %d", wOldFeishuCB.Code)
	}
	wNewFeishuCB := httptest.NewRecorder()
	r.ServeHTTP(wNewFeishuCB, httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/callback/feishu?token="+url.QueryEscape(newChanTok), strings.NewReader(feishuChallenge)))
	if wNewFeishuCB.Code != http.StatusOK || !strings.Contains(wNewFeishuCB.Body.String(), "ok_ping") {
		t.Fatalf("expected new channel token 200 with challenge echo, got %d (%s)", wNewFeishuCB.Code, wNewFeishuCB.Body.String())
	}
}

func adminTenantContext() context.Context {
	return tenantctx.With(context.Background(), tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true})
}

func strconvUint(id uint64) string {
	return strconv.FormatUint(id, 10)
}
