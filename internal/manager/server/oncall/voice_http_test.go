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

	"github.com/go-chi/chi/v5"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	alertstore "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	oncallstore "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	oncallmodel "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

func newTestHTTPHandler(t *testing.T) (*Handler, *oncallstore.Store, *gorm.DB, chi.Router) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite :memory:: %v", err)
	}
	if err := db.AutoMigrate(&iammodel.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := alertstore.Migrate(db); err != nil {
		t.Fatalf("migrate alerts: %v", err)
	}
	if err := oncallstore.Migrate(db); err != nil {
		t.Fatalf("migrate oncall: %v", err)
	}

	users := []iammodel.User{
		{ID: 1, Email: "admin@opspilot.local", DisplayName: "Admin", Phone: "13800000001", Role: iammodel.RoleAdmin, IsSuperuser: true, Status: iammodel.StatusActive},
		{ID: 2, Email: "alice@opspilot.local", DisplayName: "Alice", Phone: "13800000002", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
	}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("seed user %d: %v", users[i].ID, err)
		}
	}

	s := oncallstore.NewStore(db)
	h := NewHandlerWithDeps(s, HandlerDeps{PublicURL: "https://opspilot.test"})
	r := chi.NewRouter()
	h.Register(r)
	h.RegisterPublic(r)
	return h, s, db, r
}

func reqWithTenant(method, target, body string, tnt tenantctx.Tenant) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if tnt.UserID > 0 {
		req = req.WithContext(tenantctx.With(req.Context(), tnt))
	}
	return req
}

func TestVoiceConfig_RBACAndZeroSecretLeak(t *testing.T) {
	_, s, _, r := newTestHTTPHandler(t)
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}
	userTenant := tenantctx.Tenant{UserID: 2, Email: "alice@opspilot.local", Role: iammodel.RoleUser}

	// 1. Non-admin write & live test call must fail with 403.
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, reqWithTenant(http.MethodPut, "/v1/oncall/voice/config", `{"provider":"mock"}`, userTenant))
	if w1.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin PUT voice config, got %d (%s)", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, reqWithTenant(http.MethodPost, "/v1/oncall/voice/test-call", `{"phone_number":"13800000002"}`, userTenant))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin POST voice test-call, got %d (%s)", w2.Code, w2.Body.String())
	}

	// 2. Admin saves real secret; response must clear plaintext secret and report *_secret_configured=true.
	savePayload := `{
		"provider":"aliyun",
		"dispatch_enabled":true,
		"aliyun_access_key_id":"LTAI5abc",
		"aliyun_access_key_secret":"real-secret-value-9876",
		"aliyun_tts_code":"TTS_1001"
	}`
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, reqWithTenant(http.MethodPut, "/v1/oncall/voice/config", savePayload, adminTenant))
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 on admin PUT voice config, got %d (%s)", w3.Code, w3.Body.String())
	}
	if strings.Contains(w3.Body.String(), "real-secret-value-9876") || strings.Contains(w3.Body.String(), "re****76") {
		t.Fatalf("response must never expose secret or partial mask: %s", w3.Body.String())
	}

	// 3. GET must never expose secret, only callback_urls for admin, not for regular user.
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, reqWithTenant(http.MethodGet, "/v1/oncall/voice/config", "", adminTenant))
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 on admin GET voice config, got %d", w4.Code)
	}
	var adminResp map[string]any
	if err := json.Unmarshal(w4.Body.Bytes(), &adminResp); err != nil {
		t.Fatalf("decode admin GET: %v", err)
	}
	if adminResp["aliyun_secret_configured"] != true {
		t.Fatalf("expected aliyun_secret_configured=true, got %v", adminResp["aliyun_secret_configured"])
	}
	if _, hasSecret := adminResp["aliyun_access_key_secret"]; hasSecret {
		t.Fatalf("expected aliyun_access_key_secret omitted from GET response, got %v", adminResp["aliyun_access_key_secret"])
	}
	urls, _ := adminResp["callback_urls"].(map[string]any)
	if urls == nil || !strings.Contains(urls["aliyun"].(string), "token=") {
		t.Fatalf("expected signed aliyun callback_urls for admin, got %v", adminResp["callback_urls"])
	}

	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, reqWithTenant(http.MethodGet, "/v1/oncall/voice/config", "", userTenant))
	if w5.Code != http.StatusOK {
		t.Fatalf("expected 200 on user GET voice config, got %d", w5.Code)
	}
	var userResp map[string]any
	if err := json.Unmarshal(w5.Body.Bytes(), &userResp); err != nil {
		t.Fatalf("decode user GET: %v", err)
	}
	if _, hasCallbacks := userResp["callback_urls"]; hasCallbacks {
		t.Fatalf("non-admin GET must not include callback_urls with token: %v", userResp["callback_urls"])
	}

	// 4. Subsequent PUT with blank secret must keep the existing decrypted secret intact.
	w6 := httptest.NewRecorder()
	r.ServeHTTP(w6, reqWithTenant(http.MethodPut, "/v1/oncall/voice/config", `{"provider":"aliyun","aliyun_access_key_id":"LTAI5abc","aliyun_access_key_secret":"","aliyun_tts_code":"TTS_1002"}`, adminTenant))
	if w6.Code != http.StatusOK {
		t.Fatalf("expected 200 on second PUT, got %d", w6.Code)
	}
	stored, err := s.GetVoiceConfig(context.Background())
	if err != nil {
		t.Fatalf("GetVoiceConfig: %v", err)
	}
	if stored.AliyunAccessKeySecret != "real-secret-value-9876" {
		t.Fatalf("secret overwritten by blank PUT! got %q", stored.AliyunAccessKeySecret)
	}
	if !stored.DispatchEnabled {
		t.Fatalf("partial PUT without dispatch_enabled must preserve existing true")
	}
}

func TestVoiceCallback_SignedTokenAndDTMFAck(t *testing.T) {
	_, s, db, r := newTestHTTPHandler(t)
	ctx := context.Background()

	tok, err := s.EnsureVoiceWebhookToken(ctx)
	if err != nil || tok == "" {
		t.Fatalf("EnsureVoiceWebhookToken: %v (tok=%q)", err, tok)
	}

	now := time.Now()
	inc := &alertmodel.Incident{
		ID:           101,
		Scope:        "host",
		ScopeType:    "host",
		Rule:         "cpu_high",
		RuleName:     "CPU High",
		Severity:     "critical",
		Title:        "CPU > 95%",
		Status:       alertmodel.StatusOpen,
		DedupeKey:    "host:1:cpu_high",
		FirstFiredAt: now,
		LastFiredAt:  now,
	}
	if err := db.Create(inc).Error; err != nil {
		t.Fatalf("seed incident: %v", err)
	}

	task := &oncallmodel.EscalationTask{
		IncidentID:    101,
		Title:         "CPU > 95%",
		Severity:      "critical",
		Urgency:       "high",
		ScheduleID:    1,
		CurrentStep:   1,
		LadderStep:    2,
		StepStartedAt: now,
		Status:        oncallmodel.TaskStatusPending,
		NextRunAt:     now.Add(5 * time.Minute),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed escalation task: %v", err)
	}

	logEntry := &oncallmodel.NotificationLog{
		TaskID:     task.ID,
		IncidentID: 101,
		UserID:     2,
		UserName:   "Alice",
		Target:     "138****0002",
		Channel:    "voice_call",
		Provider:   "aliyun",
		CallID:     "call_101_abc",
		OutCallID:  "aliyun_out_99",
		Status:     oncallmodel.NotifyStatusCalling,
		CreatedAt:  now,
	}
	if err := db.Create(logEntry).Error; err != nil {
		t.Fatalf("seed notification log: %v", err)
	}

	payload := `{"call_id":"aliyun_out_99","out_id":"call_101_abc","dtmf":"1","duration":18}`

	// Missing or invalid token -> 401.
	wMissing := httptest.NewRecorder()
	r.ServeHTTP(wMissing, httptest.NewRequest(http.MethodPost, "/v1/oncall/voice/callback/aliyun", bytes.NewBufferString(payload)))
	if wMissing.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", wMissing.Code)
	}

	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, httptest.NewRequest(http.MethodPost, "/v1/oncall/voice/callback/aliyun?token=wrong-token", bytes.NewBufferString(payload)))
	if wBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", wBad.Code)
	}

	// Valid token + DTMF 1 -> acknowledges incident, updates notification log, and halts escalation task.
	wOK := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/oncall/voice/callback/aliyun?token="+url.QueryEscape(tok), bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wOK, req)
	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on signed callback, got %d (%s)", wOK.Code, wOK.Body.String())
	}

	var updatedLog oncallmodel.NotificationLog
	if err := db.First(&updatedLog, logEntry.ID).Error; err != nil {
		t.Fatalf("reload notification log: %v", err)
	}
	if updatedLog.Status != oncallmodel.NotifyStatusDTMFConfirmed || updatedLog.DTMFCode != "1" || updatedLog.DurationSecs != 18 {
		t.Fatalf("unexpected notification log state after DTMF 1 callback: %+v", updatedLog)
	}

	var updatedInc alertmodel.Incident
	if err := db.First(&updatedInc, 101).Error; err != nil {
		t.Fatalf("reload incident: %v", err)
	}
	if updatedInc.Status != alertmodel.StatusAcknowledged {
		t.Fatalf("expected incident status acknowledged, got %q", updatedInc.Status)
	}

	var updatedTask oncallmodel.EscalationTask
	if err := db.First(&updatedTask, task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if updatedTask.Status != oncallmodel.TaskStatusAcked {
		t.Fatalf("expected task status acked, got %q", updatedTask.Status)
	}
}

type spyHTTPDeliverer struct {
	sent int
}

func (s *spyHTTPDeliverer) Deliver(_ context.Context, _ string, _ *oncallstore.UserSummary, _, _, _ string, _ map[string]string) (string, error) {
	s.sent++
	return "feishu", nil
}

func TestVoiceCallback_GroupedTaskBatchAckAndFailoverValidation(t *testing.T) {
	_, s, db, r := newTestHTTPHandler(t)
	ctx := context.Background()
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}

	// 1. Validate fallback_provider rejects self-fallback and accepts alternate cloud + timeout.
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqWithTenant(http.MethodPut, "/v1/oncall/voice/config",
		`{"provider":"aliyun","fallback_provider":"aliyun","failover_timeout_seconds":10}`, adminTenant))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when fallback_provider == provider, got %d (%s)", wBad.Code, wBad.Body.String())
	}

	wGood := httptest.NewRecorder()
	r.ServeHTTP(wGood, reqWithTenant(http.MethodPut, "/v1/oncall/voice/config",
		`{"provider":"aliyun","fallback_provider":"tencent","failover_timeout_seconds":12,"aliyun_access_key_id":"k1","aliyun_access_key_secret":"s1","aliyun_tts_code":"t1","tencent_secret_id":"tk1","tencent_secret_key":"ts1","tencent_sdk_app_id":"1400","tencent_template_id":"888"}`, adminTenant))
	if wGood.Code != http.StatusOK {
		t.Fatalf("expected 200 saving dual-cloud config, got %d (%s)", wGood.Code, wGood.Body.String())
	}

	// 2. DTMF 1 callback on grouped task batch-acks ALL open incidents in merged_incident_ids_json.
	tok, err := s.EnsureVoiceWebhookToken(ctx)
	if err != nil {
		t.Fatalf("EnsureVoiceWebhookToken: %v", err)
	}
	now := time.Now().UTC()
	for _, id := range []uint64{201, 202, 203} {
		if err := db.Create(&alertmodel.Incident{
			ID:           id,
			Scope:        "host",
			ScopeType:    "host",
			Rule:         "mem_high",
			RuleName:     "Memory High",
			Severity:     "critical",
			Title:        "Memory > 95%",
			Status:       alertmodel.StatusOpen,
			DedupeKey:    "grp-" + string(rune('a'+int(id%10))),
			FirstFiredAt: now,
			LastFiredAt:  now,
		}).Error; err != nil {
			t.Fatalf("seed incident %d: %v", id, err)
		}
	}

	groupedTask := &oncallmodel.EscalationTask{
		IncidentID:            201,
		GroupKey:              "r1:s1:rule=mem_high:urgency=high",
		MergedIncidentIDsJSON: "[201,202,203]",
		AlertCount:            3,
		Title:                 "[合并 3 条] Memory > 95%",
		Severity:              "critical",
		Urgency:               "high",
		ScheduleID:            1,
		CurrentStep:           1,
		LadderStep:            1,
		StepStartedAt:         now,
		Status:                oncallmodel.TaskStatusPending,
		NextRunAt:             now.Add(5 * time.Minute),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := db.Create(groupedTask).Error; err != nil {
		t.Fatalf("seed grouped task: %v", err)
	}
	if err := s.CreateNotificationLog(ctx, &oncallmodel.NotificationLog{
		TaskID:     groupedTask.ID,
		IncidentID: 201,
		AlertCount: 3,
		UserID:     2,
		UserName:   "Alice",
		Channel:    "voice_call",
		Provider:   "aliyun",
		CallID:     "call_grp_1",
		OutCallID:  "out_grp_1",
		Status:     oncallmodel.NotifyStatusCalling,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("CreateNotificationLog: %v", err)
	}

	cbReq := httptest.NewRequest(http.MethodPost, "/v1/oncall/voice/callback/aliyun?token="+url.QueryEscape(tok),
		bytes.NewBufferString(`{"call_id":"out_grp_1","out_id":"call_grp_1","dtmf":"1","duration":12}`))
	cbReq.Header.Set("Content-Type", "application/json")
	cbW := httptest.NewRecorder()
	r.ServeHTTP(cbW, cbReq)
	if cbW.Code != http.StatusOK {
		t.Fatalf("expected 200 on grouped DTMF 1 callback, got %d", cbW.Code)
	}

	for _, id := range []uint64{201, 202, 203} {
		var inc alertmodel.Incident
		if err := db.First(&inc, id).Error; err != nil {
			t.Fatalf("reload incident %d: %v", id, err)
		}
		if inc.Status != alertmodel.StatusAcknowledged {
			t.Fatalf("expected grouped incident %d acknowledged after DTMF 1, got %q", id, inc.Status)
		}
	}
}

func TestHandoffHTTP_RemindNowLogsHydrationAndZeroAdvanceHours(t *testing.T) {
	_, s, _, _ := newTestHTTPHandler(t)
	del := &spyHTTPDeliverer{}
	h := NewHandlerWithDeps(s, HandlerDeps{
		PublicURL: "https://opspilot.test",
		Deliverer: del,
	})
	r := chi.NewRouter()
	h.Register(r)

	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}

	// 1. POST /v1/oncall/schedules with reminder_advance_hours: 0 must persist 0 instead of 3.
	createSchedBody := `{
		"name":"Zero Reminder Schedule",
		"timezone":"Asia/Shanghai",
		"handoff_time":"09:00:00",
		"reminder_advance_hours":0,
		"enabled":true,
		"rotations":[{"name":"Primary","tier":1,"rotation_type":"daily","shift_length_seconds":86400,"users":[1,2]}]
	}`
	wSched := httptest.NewRecorder()
	r.ServeHTTP(wSched, reqWithTenant(http.MethodPost, "/v1/oncall/schedules", createSchedBody, adminTenant))
	if wSched.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating schedule, got %d (%s)", wSched.Code, wSched.Body.String())
	}
	var createdSched oncallmodel.Schedule
	if err := json.Unmarshal(wSched.Body.Bytes(), &createdSched); err != nil {
		t.Fatalf("decode schedule: %v", err)
	}
	if createdSched.ReminderAdvanceHours != 0 {
		t.Fatalf("expected reminder_advance_hours=0 persisted, got %d", createdSched.ReminderAdvanceHours)
	}

	// 2. POST /v1/oncall/handoff/remind-now sends manual reminder and GET /v1/oncall/handoff/logs hydrates user names.
	wRem := httptest.NewRecorder()
	r.ServeHTTP(wRem, reqWithTenant(http.MethodPost, "/v1/oncall/handoff/remind-now", `{"schedule_id":`+strconv.FormatUint(createdSched.ID, 10)+`}`, adminTenant))
	if wRem.Code != http.StatusOK {
		t.Fatalf("expected 200 on remind-now, got %d (%s)", wRem.Code, wRem.Body.String())
	}
	if del.sent != 1 {
		t.Fatalf("expected deliverer invoked once, got %d", del.sent)
	}

	wLogs := httptest.NewRecorder()
	r.ServeHTTP(wLogs, reqWithTenant(http.MethodGet, "/v1/oncall/handoff/logs", "", adminTenant))
	if wLogs.Code != http.StatusOK {
		t.Fatalf("expected 200 on list handoff logs, got %d (%s)", wLogs.Code, wLogs.Body.String())
	}
	var logsResp struct {
		Items []HandoffLogResponse `json:"items"`
		Total int                  `json:"total"`
	}
	if err := json.Unmarshal(wLogs.Body.Bytes(), &logsResp); err != nil {
		t.Fatalf("decode handoff logs: %v", err)
	}
	if logsResp.Total != 1 || logsResp.Items[0].UserName == "" || logsResp.Items[0].ScheduleName != "Zero Reminder Schedule" {
		t.Fatalf("expected hydrated handoff log with names, got %+v", logsResp)
	}
	_ = bizoncall.NewScheduler
}
