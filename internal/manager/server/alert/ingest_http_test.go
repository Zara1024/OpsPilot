package alert

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	bizalert "github.com/Zara1024/OpsPilot/internal/manager/biz/alert"
	alertstore "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

func setupIngestHTTPTestHandler(t *testing.T) (*Handler, *bizalert.Usecase, *bizalert.IngestService) {
	t.Helper()
	t.Setenv("OPSPILOT_SECRET_KEY", "test-only-ingest-http-secret")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := alertstore.Migrate(db); err != nil {
		t.Fatalf("migrate alert tables: %v", err)
	}

	repo := alertstore.NewRepo(db)
	ingestRepo := alertstore.NewIngestRepo(db)
	uc := bizalert.NewUsecase(repo, nil)
	ingestSvc := bizalert.NewIngestService(ingestRepo, uc, "https://opspilot.example.com", nil)

	h := NewHandler(&fakeService{}, &fakeService{}, &fakeService{}).WithIngest(ingestSvc)
	return h, uc, ingestSvc
}

func withRole(req *http.Request, role string) *http.Request {
	return req.WithContext(tenantctx.With(req.Context(), tenantctx.Tenant{
		UserID: 1,
		Email:  "tester@opspilot.local",
		Role:   role,
	}))
}

func TestIngestHTTP_AdminCRUDPublicWebhookAndHeartbeatFlow(t *testing.T) {
	h, uc, _ := setupIngestHTTPTestHandler(t)
	mux := chi.NewRouter()
	h.RegisterPublic(mux)
	h.Register(mux)

	// 1. Non-admin cannot create ingest source (403).
	reqForbidden := withRole(httptest.NewRequest(http.MethodPost, "/v1/alerts/ingest-sources", bytes.NewBufferString(`{"name":"AM","provider":"alertmanager"}`)), "user")
	recForbidden := httptest.NewRecorder()
	mux.ServeHTTP(recForbidden, reqForbidden)
	if recForbidden.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin create, got %d", recForbidden.Code)
	}

	// 2. Admin creates Alertmanager ingest source.
	createReq := withRole(httptest.NewRequest(http.MethodPost, "/v1/alerts/ingest-sources", bytes.NewBufferString(`{
		"name":"Prometheus Prod",
		"provider":"alertmanager",
		"default_severity":"warning",
		"default_labels":{"cluster":"prod-gz"},
		"rate_limit_per_min":2
	}`)), "admin")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating ingest source, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created bizalert.IngestSourceView
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created source: %v", err)
	}
	if created.Token == "" {
		t.Fatalf("expected plain token returned to admin on create")
	}

	// 3. Non-admin GET lists sources with masked token.
	listUserReq := withRole(httptest.NewRequest(http.MethodGet, "/v1/alerts/ingest-sources", nil), "user")
	listUserRec := httptest.NewRecorder()
	mux.ServeHTTP(listUserRec, listUserReq)
	if listUserRec.Code != http.StatusOK || !bytes.Contains(listUserRec.Body.Bytes(), []byte("****")) {
		t.Fatalf("expected masked token for non-admin list, got %d: %s", listUserRec.Code, listUserRec.Body.String())
	}

	// 4. Public webhook ingestion (1st request via path token -> fires incident).
	amPayload := `{"status":"firing","alerts":[{"status":"firing","fingerprint":"http-fp-1","labels":{"alertname":"DiskFull","host":"app-09","severity":"critical"},"annotations":{"summary":"Disk > 95%"}}]}`
	pubReq1 := httptest.NewRequest(http.MethodPost, "/v1/alerts/ingest/alertmanager/"+created.Token, bytes.NewBufferString(amPayload))
	pubRec1 := httptest.NewRecorder()
	mux.ServeHTTP(pubRec1, pubReq1)
	if pubRec1.Code != http.StatusOK {
		t.Fatalf("expected 200 public ingest, got %d: %s", pubRec1.Code, pubRec1.Body.String())
	}
	var res1 bizalert.IngestResult
	_ = json.Unmarshal(pubRec1.Body.Bytes(), &res1)
	if res1.FiredCount != 1 || len(res1.IncidentIDs) != 1 {
		t.Fatalf("expected 1 fired incident, got %+v", res1)
	}

	// 5. Public webhook ingestion (2nd request via header token resolves same incident).
	amResolve := `{"status":"resolved","alerts":[{"status":"resolved","fingerprint":"http-fp-1","labels":{"alertname":"DiskFull","host":"app-09"}}]}`
	pubReq2 := httptest.NewRequest(http.MethodPost, "/v1/alerts/ingest/alertmanager", bytes.NewBufferString(amResolve))
	pubReq2.Header.Set("X-OpsPilot-Token", created.Token)
	pubRec2 := httptest.NewRecorder()
	mux.ServeHTTP(pubRec2, pubReq2)
	if pubRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 public resolve ingest, got %d: %s", pubRec2.Code, pubRec2.Body.String())
	}
	inc, err := uc.GetIncident(reqForbidden.Context(), res1.IncidentIDs[0])
	if err != nil || inc.Status != "resolved" {
		t.Fatalf("expected incident resolved, got %+v err=%v", inc, err)
	}

	// 6. 3rd request hits RateLimitPerMin=2 -> HTTP 429.
	pubReq3 := httptest.NewRequest(http.MethodPost, "/v1/alerts/ingest/alertmanager/"+created.Token, bytes.NewBufferString(amPayload))
	pubRec3 := httptest.NewRecorder()
	mux.ServeHTTP(pubRec3, pubReq3)
	if pubRec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 rate limited, got %d: %s", pubRec3.Code, pubRec3.Body.String())
	}

	// 7. Create Heartbeat & ping publicly via GET + manually via POST.
	hbCreateReq := withRole(httptest.NewRequest(http.MethodPost, "/v1/alerts/heartbeats", bytes.NewBufferString(`{
		"name":"Watchdog Cron",
		"interval_seconds":60,
		"grace_seconds":120,
		"severity":"critical"
	}`)), "admin")
	hbCreateRec := httptest.NewRecorder()
	mux.ServeHTTP(hbCreateRec, hbCreateReq)
	if hbCreateRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating heartbeat, got %d: %s", hbCreateRec.Code, hbCreateRec.Body.String())
	}
	var hb bizalert.HeartbeatView
	_ = json.Unmarshal(hbCreateRec.Body.Bytes(), &hb)

	pingReq := httptest.NewRequest(http.MethodGet, "/v1/alerts/heartbeat/"+hb.Token+"?msg=ok", nil)
	pingRec := httptest.NewRecorder()
	mux.ServeHTTP(pingRec, pingReq)
	if pingRec.Code != http.StatusOK {
		t.Fatalf("expected 200 public heartbeat ping, got %d: %s", pingRec.Code, pingRec.Body.String())
	}
	var pingRes bizalert.HeartbeatPingResult
	_ = json.Unmarshal(pingRec.Body.Bytes(), &pingRes)
	if pingRes.Heartbeat.State != "healthy" || pingRes.Heartbeat.PingCount != 1 {
		t.Fatalf("unexpected heartbeat ping result: %+v", pingRes)
	}
}
