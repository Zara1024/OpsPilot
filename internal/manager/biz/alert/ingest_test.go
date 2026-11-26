package alert

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type memoryIngestRepo struct {
	mu      sync.Mutex
	nextSrc uint64
	nextHB  uint64
	sources map[uint64]*model.IngestSource
	hbs     map[uint64]*model.Heartbeat
}

func newMemoryIngestRepo() *memoryIngestRepo {
	return &memoryIngestRepo{
		sources: make(map[uint64]*model.IngestSource),
		hbs:     make(map[uint64]*model.Heartbeat),
	}
}

func (r *memoryIngestRepo) ListIngestSources(_ context.Context) ([]*model.IngestSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*model.IngestSource, 0, len(r.sources))
	for _, s := range r.sources {
		cp := *s
		out = append(out, &cp)
	}
	return out, nil
}

func (r *memoryIngestRepo) GetIngestSourceByID(_ context.Context, id uint64) (*model.IngestSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sources[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *memoryIngestRepo) GetIngestSourceByTokenHash(_ context.Context, tokenHash string) (*model.IngestSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sources {
		if s.TokenHash == tokenHash {
			cp := *s
			return &cp, nil
		}
	}
	return nil, errs.ErrNotFound
}

func (r *memoryIngestRepo) CreateIngestSource(_ context.Context, src *model.IngestSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSrc++
	src.ID = r.nextSrc
	now := time.Now().UTC()
	src.CreatedAt = now
	src.UpdatedAt = now
	cp := *src
	r.sources[src.ID] = &cp
	return nil
}

func (r *memoryIngestRepo) UpdateIngestSource(_ context.Context, src *model.IngestSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.sources[src.ID]
	if !ok {
		return errs.ErrNotFound
	}
	cp := *src
	cp.CreatedAt = cur.CreatedAt
	cp.ReceivedCount = cur.ReceivedCount
	cp.ErrorCount = cur.ErrorCount
	cp.UpdatedAt = time.Now().UTC()
	r.sources[src.ID] = &cp
	return nil
}

func (r *memoryIngestRepo) DeleteIngestSource(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[id]; !ok {
		return errs.ErrNotFound
	}
	delete(r.sources, id)
	return nil
}

func (r *memoryIngestRepo) UpdateIngestSourceToken(_ context.Context, id uint64, tokenHash, tokenEnc string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sources[id]
	if !ok {
		return errs.ErrNotFound
	}
	s.TokenHash = tokenHash
	s.TokenEncrypted = tokenEnc
	s.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *memoryIngestRepo) RecordIngestOutcome(_ context.Context, id uint64, at time.Time, status, errMsg, payloadPreview string, isError bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sources[id]
	if !ok {
		return nil
	}
	s.ReceivedCount++
	if isError {
		s.ErrorCount++
	}
	ts := at.UTC()
	s.LastReceivedAt = &ts
	s.LastStatus = status
	s.LastError = errMsg
	if payloadPreview != "" {
		s.LastPayloadPreview = payloadPreview
	}
	return nil
}

func (r *memoryIngestRepo) ListHeartbeats(_ context.Context) ([]*model.Heartbeat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*model.Heartbeat, 0, len(r.hbs))
	for _, hb := range r.hbs {
		cp := *hb
		out = append(out, &cp)
	}
	return out, nil
}

func (r *memoryIngestRepo) ListEnabledHeartbeats(_ context.Context) ([]*model.Heartbeat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Heartbeat
	for _, hb := range r.hbs {
		if hb.Enabled {
			cp := *hb
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *memoryIngestRepo) GetHeartbeatByID(_ context.Context, id uint64) (*model.Heartbeat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hb, ok := r.hbs[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	cp := *hb
	return &cp, nil
}

func (r *memoryIngestRepo) GetHeartbeatByTokenHash(_ context.Context, tokenHash string) (*model.Heartbeat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, hb := range r.hbs {
		if hb.TokenHash == tokenHash {
			cp := *hb
			return &cp, nil
		}
	}
	return nil, errs.ErrNotFound
}

func (r *memoryIngestRepo) CreateHeartbeat(_ context.Context, hb *model.Heartbeat) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextHB++
	hb.ID = r.nextHB
	now := time.Now().UTC()
	hb.CreatedAt = now
	hb.UpdatedAt = now
	cp := *hb
	r.hbs[hb.ID] = &cp
	return nil
}

func (r *memoryIngestRepo) UpdateHeartbeat(_ context.Context, hb *model.Heartbeat) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.hbs[hb.ID]
	if !ok {
		return errs.ErrNotFound
	}
	cp := *hb
	cp.CreatedAt = cur.CreatedAt
	cp.PingCount = cur.PingCount
	cp.UpdatedAt = time.Now().UTC()
	r.hbs[hb.ID] = &cp
	return nil
}

func (r *memoryIngestRepo) DeleteHeartbeat(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.hbs[id]; !ok {
		return errs.ErrNotFound
	}
	delete(r.hbs, id)
	return nil
}

func (r *memoryIngestRepo) UpdateHeartbeatToken(_ context.Context, id uint64, tokenHash, tokenEnc string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	hb, ok := r.hbs[id]
	if !ok {
		return errs.ErrNotFound
	}
	hb.TokenHash = tokenHash
	hb.TokenEncrypted = tokenEnc
	hb.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *memoryIngestRepo) RecordHeartbeatPing(_ context.Context, id uint64, at time.Time, clientIP, message string) (*model.Heartbeat, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hb, ok := r.hbs[id]
	if !ok {
		return nil, "", errs.ErrNotFound
	}
	prev := hb.State
	if hb.Enabled {
		hb.State = model.HeartbeatStateHealthy
	} else {
		hb.State = model.HeartbeatStatePaused
	}
	ts := at.UTC()
	hb.LastPingAt = &ts
	hb.LastPingIP = clientIP
	hb.LastPingMessage = message
	hb.AlertingSince = nil
	hb.IncidentID = nil
	hb.PingCount++
	hb.UpdatedAt = ts
	cp := *hb
	return &cp, prev, nil
}

func (r *memoryIngestRepo) MarkHeartbeatAlerting(_ context.Context, id uint64, at time.Time, incidentID uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hb, ok := r.hbs[id]
	if !ok || !hb.Enabled || hb.State != model.HeartbeatStateHealthy {
		return false, nil
	}
	ts := at.UTC()
	hb.State = model.HeartbeatStateAlerting
	hb.AlertingSince = &ts
	if incidentID > 0 {
		inc := incidentID
		hb.IncidentID = &inc
	}
	hb.UpdatedAt = ts
	return true, nil
}

func TestIngestAdapters_AllSevenProtocols(t *testing.T) {
	reg := NewDefaultAdapterRegistry()

	t.Run("alertmanager_batch_fire_and_resolve", func(t *testing.T) {
		body := []byte(`{
			"status":"firing",
			"commonLabels":{"alertname":"PodCrashLoop","namespace":"prod"},
			"alerts":[
				{"status":"firing","fingerprint":"fp-1","labels":{"severity":"critical","pod":"pay-1"},"annotations":{"summary":"pay-1 crash"}},
				{"status":"resolved","fingerprint":"fp-2","labels":{"severity":"warning","pod":"pay-2"},"annotations":{"summary":"pay-2 recovered"}}
			]
		}`)
		res, err := reg.Parse(model.IngestProviderAlertmanager, "application/json", nil, body)
		if err != nil {
			t.Fatalf("Parse alertmanager: %v", err)
		}
		if len(res.Events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(res.Events))
		}
		if res.Events[0].Status != "firing" || res.Events[0].Severity != "critical" || res.Events[0].Target != "pay-1" || res.Events[0].RawDedupeKey != "fp-1" {
			t.Fatalf("unexpected first alertmanager event: %+v", res.Events[0])
		}
		if res.Events[1].Status != "resolved" || res.Events[1].RawDedupeKey != "fp-2" {
			t.Fatalf("unexpected second alertmanager event: %+v", res.Events[1])
		}
	})

	t.Run("grafana_unified_and_legacy", func(t *testing.T) {
		legacy := []byte(`{"state":"alerting","ruleId":99,"ruleName":"DB Slow Query","message":"slow > 2s","evalMatches":[{"metric":"qps","value":18.5,"tags":{"host":"db-01"}}]}`)
		res, err := reg.Parse(model.IngestProviderGrafana, "application/json", nil, legacy)
		if err != nil || len(res.Events) != 1 {
			t.Fatalf("Parse legacy grafana: %v / %+v", err, res)
		}
		if res.Events[0].Status != "firing" || res.Events[0].Target != "db-01" || res.Events[0].RawDedupeKey != "grafana-rule-99" {
			t.Fatalf("unexpected legacy grafana event: %+v", res.Events[0])
		}
		unified := []byte(`{"status":"resolved","alerts":[{"status":"resolved","fingerprint":"gf-fp-1","labels":{"alertname":"CPUHigh","instance":"app-1","severity":"warning"}}]}`)
		res2, err := reg.Parse(model.IngestProviderGrafana, "application/json", nil, unified)
		if err != nil || len(res2.Events) != 1 || res2.Events[0].Status != "resolved" {
			t.Fatalf("Parse unified grafana failed: %v / %+v", err, res2)
		}
	})

	t.Run("aliyun_cms_form_and_json", func(t *testing.T) {
		form := "alertName=DiskUsageHigh&alertState=ALERT&curLevel=CRITICAL&ruleId=cms-1&instanceName=ecs-01&curValue=95.4&timestamp=1795650000000"
		res, err := reg.Parse(model.IngestProviderAliyunCMS, "application/x-www-form-urlencoded", nil, []byte(form))
		if err != nil || len(res.Events) != 1 {
			t.Fatalf("Parse aliyun form: %v", err)
		}
		if res.Events[0].Status != "firing" || res.Events[0].Severity != "critical" || res.Events[0].Target != "ecs-01" {
			t.Fatalf("unexpected aliyun event: %+v", res.Events[0])
		}
		okJSON := []byte(`{"alertName":"DiskUsageHigh","alertState":"OK","ruleId":"cms-1","instanceName":"ecs-01"}`)
		res2, err := reg.Parse(model.IngestProviderAliyunCMS, "application/json", nil, okJSON)
		if err != nil || len(res2.Events) != 1 || res2.Events[0].Status != "resolved" {
			t.Fatalf("Parse aliyun JSON ok failed: %v / %+v", err, res2)
		}
	})

	t.Run("tencent_cm_trigger_and_recover", func(t *testing.T) {
		body := []byte(`{"sessionId":"sess-77","alarmStatus":"0","alarmObjInfo":{"region":"ap-shanghai","namespace":"qce/cvm","dimensions":{"unInstanceId":"ins-abc","objName":"10.1.1.2"}},"alarmPolicyInfo":{"policyId":"pol-77","policyName":"MemHigh","conditions":{"metricName":"MemUsage","currentValue":"22.1","calcValue":"85","unit":"%"}}}`)
		res, err := reg.Parse(model.IngestProviderTencentCM, "application/json", nil, body)
		if err != nil || len(res.Events) != 1 {
			t.Fatalf("Parse tencent: %v", err)
		}
		if res.Events[0].Status != "resolved" || res.Events[0].RawDedupeKey != "sess-77" || res.Events[0].Target != "ins-abc" {
			t.Fatalf("unexpected tencent event: %+v", res.Events[0])
		}
	})

	t.Run("huawei_ces_smn_confirmation_and_notification", func(t *testing.T) {
		confirm := []byte(`{"type":"SubscriptionConfirmation","topic_urn":"urn:smn:demo","subscribe_url":"https://smn.cn-north-4.myhuaweicloud.com/confirm?token=abc"}`)
		resConfirm, err := reg.Parse(model.IngestProviderHuaweiCES, "application/json", nil, confirm)
		if err != nil {
			t.Fatalf("Parse huawei confirm: %v", err)
		}
		if len(resConfirm.Events) != 0 || resConfirm.SubscribeURL == "" {
			t.Fatalf("expected subscribe_url without events, got %+v", resConfirm)
		}

		confirmCamel := []byte(`{"Type":"SubscriptionConfirmation","SubscribeUrl":"https://smn.cn-south-1.myhuaweicloud.com/confirm?token=camel"}`)
		resConfirmCamel, err := reg.Parse(model.IngestProviderHuaweiCES, "application/json", nil, confirmCamel)
		if err != nil {
			t.Fatalf("Parse huawei confirm camelCase: %v", err)
		}
		if len(resConfirmCamel.Events) != 0 || resConfirmCamel.SubscribeURL != "https://smn.cn-south-1.myhuaweicloud.com/confirm?token=camel" {
			t.Fatalf("expected camelCase subscribe_url without events, got %+v", resConfirmCamel)
		}

		notif := []byte(`{"type":"Notification","subject":"CES Alert","message":"{\"alarm_id\":\"hw-9\",\"alarm_name\":\"RDS CPU\",\"alarm_status\":\"alarm\",\"alarm_level\":2,\"resource_name\":\"rds-01\",\"current_data\":93.2}"}`)
		resNotif, err := reg.Parse(model.IngestProviderHuaweiCES, "application/json", nil, notif)
		if err != nil || len(resNotif.Events) != 1 {
			t.Fatalf("Parse huawei notification: %v", err)
		}
		if resNotif.Events[0].Status != "firing" || resNotif.Events[0].Severity != "critical" || resNotif.Events[0].Target != "rds-01" {
			t.Fatalf("unexpected huawei event: %+v", resNotif.Events[0])
		}
	})

	t.Run("zabbix_uppercase_macros_and_problem_ok_correlation", func(t *testing.T) {
		prob := []byte(`{"EVENT_ID":"101","TRIGGER_ID":"550","EVENT_STATUS":"PROBLEM","EVENT_NSEVERITY":"5","EVENT_NAME":"MySQL Down","HOST_NAME":"db-prod-01","EVENT_TAGSJSON":[{"tag":"service","value":"billing"}]}`)
		resProb, err := reg.Parse(model.IngestProviderZabbix, "application/json", nil, prob)
		if err != nil || len(resProb.Events) != 1 {
			t.Fatalf("Parse zabbix problem: %v", err)
		}
		ok := []byte(`{"EVENT_ID":"102","TRIGGER_ID":"550","EVENT_STATUS":"OK","EVENT_NSEVERITY":"0","EVENT_NAME":"MySQL Down","HOST_NAME":"db-prod-01"}`)
		resOK, err := reg.Parse(model.IngestProviderZabbix, "application/json", nil, ok)
		if err != nil || len(resOK.Events) != 1 {
			t.Fatalf("Parse zabbix ok: %v", err)
		}
		if resProb.Events[0].Severity != "critical" || resProb.Events[0].Labels["service"] != "billing" {
			t.Fatalf("unexpected zabbix problem event: %+v", resProb.Events[0])
		}
		if resOK.Events[0].Status != "resolved" {
			t.Fatalf("expected zabbix OK resolved, got %s", resOK.Events[0].Status)
		}
		if resProb.Events[0].RawDedupeKey != resOK.Events[0].RawDedupeKey {
			t.Fatalf("zabbix dedupe mismatch across different event_ids: %q vs %q", resProb.Events[0].RawDedupeKey, resOK.Events[0].RawDedupeKey)
		}
	})

	t.Run("generic_and_pagerduty_v2", func(t *testing.T) {
		pd := []byte(`{"event_action":"trigger","dedup_key":"pd-key-1","payload":{"summary":"Redis OOM","severity":"error","source":"redis-01","component":"cache"}}`)
		res, err := reg.Parse(model.IngestProviderGenericWebhook, "application/json", nil, pd)
		if err != nil || len(res.Events) != 1 {
			t.Fatalf("Parse generic pagerduty: %v", err)
		}
		if res.Events[0].Status != "firing" || res.Events[0].Severity != "critical" || res.Events[0].Target != "redis-01" || res.Events[0].RawDedupeKey != "pd-key-1" {
			t.Fatalf("unexpected generic event: %+v", res.Events[0])
		}
	})
}

func TestIngestService_HMACRateLimitTokenRotationAndLifecycle(t *testing.T) {
	t.Setenv("OPSPILOT_SECRET_KEY", "test-ingest-service-secret-key")
	ctx := context.Background()
	alertRepo := newFakeRepo()
	alertUC := NewUsecase(alertRepo, nil)
	spy := &spyOnCallDispatcher{}
	alertUC.SetOnCallDispatcher(spy)

	ingestRepo := newMemoryIngestRepo()
	svc := NewIngestService(ingestRepo, alertUC, "https://opspilot.example.com", nil)

	src, err := svc.CreateIngestSource(ctx, 1, IngestSourceInput{
		Name:            "Zabbix 生产集群",
		Provider:        model.IngestProviderZabbix,
		SigningSecret:   "hmac-secret-123",
		DefaultSeverity: "warning",
		DefaultLabels:   map[string]string{"env": "prod", "team": "sre"},
		RateLimitPerMin: 3,
	})
	if err != nil {
		t.Fatalf("CreateIngestSource: %v", err)
	}
	if !src.SigningSecretConfigured || !strings.HasPrefix(src.WebhookURL, "https://opspilot.example.com/api/v1/alerts/ingest/zabbix/") {
		t.Fatalf("unexpected created source view: %+v", src)
	}

	// Missing signature fails 401.
	probBody := []byte(`{"trigger_id":"77","status":"PROBLEM","severity":"4","event_name":"Nginx 502","host":"gw-01"}`)
	_, err = svc.IngestWebhook(ctx, IngestWebhookInput{
		Provider: model.IngestProviderZabbix,
		Token:    src.Token,
		Body:     probBody,
	})
	if !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for missing HMAC, got %v", err)
	}

	sign := func(payload []byte) http.Header {
		mac := hmac.New(sha256.New, []byte("hmac-secret-123"))
		mac.Write(payload)
		h := make(http.Header)
		h.Set("X-OpsPilot-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		return h
	}

	// 1st request triggers incident + OnCall dispatcher.
	res1, err := svc.IngestWebhook(ctx, IngestWebhookInput{
		Provider: model.IngestProviderZabbix,
		Token:    src.Token,
		Headers:  sign(probBody),
		Body:     probBody,
	})
	if err != nil || res1.FiredCount != 1 || len(res1.IncidentIDs) != 1 {
		t.Fatalf("expected fired incident, got res=%+v err=%v", res1, err)
	}
	inc, err := alertUC.GetIncident(ctx, res1.IncidentIDs[0])
	if err != nil {
		t.Fatalf("GetIncident: %v", err)
	}
	if inc.SourceType != model.RuleSourceExternalWebhook || !strings.Contains(inc.LabelsJSON, `"env":"prod"`) {
		t.Fatalf("unexpected incident stored: %+v", inc)
	}

	// 2nd signed request (resolve) resolves same incident & triggers OnCall close.
	okBody := []byte(`{"trigger_id":"77","status":"OK","severity":"0","event_name":"Nginx 502","host":"gw-01"}`)
	res2, err := svc.IngestWebhook(ctx, IngestWebhookInput{
		Provider: model.IngestProviderZabbix,
		Token:    src.Token,
		Headers:  sign(okBody),
		Body:     okBody,
	})
	if err != nil || res2.ResolvedCount != 1 {
		t.Fatalf("expected auto resolve, got res=%+v err=%v", res2, err)
	}
	incAfter, _ := alertUC.GetIncident(ctx, res1.IncidentIDs[0])
	if incAfter.Status != model.IncidentStatusResolved {
		t.Fatalf("expected resolved status, got %s", incAfter.Status)
	}
	if len(spy.calls) != 2 || spy.calls[0].kind != "fired" || spy.calls[1].kind != "closed" {
		t.Fatalf("expected OnCall fire+close hooks, got %+v", spy.calls)
	}

	// Sliding window counted: 1 invalid-HMAC + 1 fire + 1 resolve = 3 requests; 4th request hits rate limit (3/min).
	_, err = svc.IngestWebhook(ctx, IngestWebhookInput{
		Provider: model.IngestProviderZabbix,
		Token:    src.Token,
		Headers:  sign(probBody),
		Body:     probBody,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on 4th request, got %v", err)
	}

	// Rotate token invalidates old token immediately.
	rotated, err := svc.RotateIngestSourceToken(ctx, src.ID)
	if err != nil || rotated.Token == "" || rotated.Token == src.Token {
		t.Fatalf("RotateIngestSourceToken failed: %+v err=%v", rotated, err)
	}
	_, err = svc.IngestWebhook(ctx, IngestWebhookInput{
		Provider: model.IngestProviderZabbix,
		Token:    src.Token,
		Headers:  sign(probBody),
		Body:     probBody,
	})
	if !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("expected old token to be rejected with ErrUnauthorized, got %v", err)
	}

	// TestIngestSource works for every provider.
	for _, provider := range []string{
		model.IngestProviderAlertmanager,
		model.IngestProviderGrafana,
		model.IngestProviderAliyunCMS,
		model.IngestProviderTencentCM,
		model.IngestProviderHuaweiCES,
		model.IngestProviderZabbix,
		model.IngestProviderGenericWebhook,
	} {
		sItem, err := svc.CreateIngestSource(ctx, 1, IngestSourceInput{Name: "S-" + provider, Provider: provider})
		if err != nil {
			t.Fatalf("create %s: %v", provider, err)
		}
		tFire, err := svc.TestIngestSource(ctx, sItem.ID, "fire")
		if err != nil || tFire.FiredCount != 1 {
			t.Fatalf("test fire %s failed: res=%+v err=%v", provider, tFire, err)
		}
		tRes, err := svc.TestIngestSource(ctx, sItem.ID, "resolve")
		if err != nil || tRes.ResolvedCount != 1 {
			t.Fatalf("test resolve %s failed: res=%+v err=%v", provider, tRes, err)
		}
	}
}

type staticClock struct{ now time.Time }

func (c *staticClock) Now() time.Time { return c.now }

func TestHeartbeatWorker_OverdueFiringAndPingAutoResolve(t *testing.T) {
	t.Setenv("OPSPILOT_SECRET_KEY", "test-heartbeat-secret-key")
	ctx := context.Background()
	alertRepo := newFakeRepo()
	alertUC := NewUsecase(alertRepo, nil)
	spy := &spyOnCallDispatcher{}
	alertUC.SetOnCallDispatcher(spy)

	ingestRepo := newMemoryIngestRepo()
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	clk := &staticClock{now: base}
	alertUC.clock = clk

	svc := NewIngestService(ingestRepo, alertUC, "https://opspilot.example.com", nil)
	svc.setClock(clk)
	worker := NewHeartbeatWorker(ingestRepo, alertUC, time.Second, nil)
	worker.setClock(clk)

	hb, err := svc.CreateHeartbeat(ctx, 1, HeartbeatInput{
		Name:            "Prometheus Watchdog",
		IntervalSeconds: 60,
		GraceSeconds:    120,
		Severity:        "critical",
		Labels:          map[string]string{"service": "monitoring"},
	})
	if err != nil || hb.State != model.HeartbeatStateWaiting {
		t.Fatalf("expected waiting heartbeat, got %+v err=%v", hb, err)
	}

	// Overdue evaluation must NOT fire while waiting for first ping.
	clk.now = base.Add(10 * time.Minute)
	if err := worker.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce waiting: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("waiting heartbeat should not fire, got %+v", spy.calls)
	}

	// First ping activates healthy monitoring.
	pingRes, err := svc.PingHeartbeatByToken(ctx, hb.Token, "10.0.0.5", "ok")
	if err != nil || pingRes.Heartbeat.State != model.HeartbeatStateHealthy {
		t.Fatalf("first ping failed: %+v err=%v", pingRes, err)
	}

	// At +179s (< 60+120=180s), still healthy.
	clk.now = base.Add(10*time.Minute + 179*time.Second)
	if err := worker.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce grace boundary: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("should still be within grace window, got %+v", spy.calls)
	}

	// At +185s, overdue -> triggers P0 incident & moves state to alerting.
	clk.now = base.Add(10*time.Minute + 185*time.Second)
	if err := worker.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce overdue: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0].kind != "fired" {
		t.Fatalf("expected heartbeat missed incident fired, got %+v", spy.calls)
	}
	hbAfter, _ := ingestRepo.GetHeartbeatByID(ctx, hb.ID)
	if hbAfter.State != model.HeartbeatStateAlerting || hbAfter.IncidentID == nil {
		t.Fatalf("expected alerting state with incident_id, got %+v", hbAfter)
	}

	// Subsequent ping auto-resolves incident back to healthy!
	clk.now = clk.now.Add(30 * time.Second)
	recRes, err := svc.PingHeartbeatByToken(ctx, hb.Token, "10.0.0.5", "recovered")
	if err != nil {
		t.Fatalf("recovery ping failed: %v", err)
	}
	if !recRes.AutoResolved || recRes.PreviousState != model.HeartbeatStateAlerting || recRes.Heartbeat.State != model.HeartbeatStateHealthy {
		t.Fatalf("expected auto-resolved recovery, got %+v", recRes)
	}
	inc, err := alertUC.GetIncident(ctx, *hbAfter.IncidentID)
	if err != nil || inc.Status != model.IncidentStatusResolved {
		t.Fatalf("expected incident %d resolved, got %+v err=%v", *hbAfter.IncidentID, inc, err)
	}
	if len(spy.calls) != 2 || spy.calls[1].kind != "closed" {
		t.Fatalf("expected OnCall closed hook, got %+v", spy.calls)
	}

	// Oversized rawKey dedupe construction stays <= 191 chars and namespaced per source.
	dk := buildExternalDedupeKey(42, strings.Repeat("a", 300), NormalizedAlertEvent{})
	if !strings.HasPrefix(dk, fmt.Sprintf("ext:%d:", 42)) || len(dk) > 191 {
		t.Fatalf("invalid normalized dedupe key len=%d val=%q", len(dk), dk)
	}
}
