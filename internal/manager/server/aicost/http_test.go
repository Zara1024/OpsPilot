package aicost

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	bizaicost "github.com/Zara1024/OpsPilot/internal/manager/biz/aicost"
	"github.com/Zara1024/OpsPilot/internal/manager/data/aicost/store"
	"github.com/Zara1024/OpsPilot/internal/pkg/llm"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

func newTestRouter(t *testing.T) (chi.Router, *bizaicost.Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "http.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, store.Migrate(db))

	svc := bizaicost.New(store.New(db), nil, nil)
	r := chi.NewRouter()
	NewHandler(svc).RegisterProtected(r)
	return r, svc
}

func doReq(r http.Handler, method, path string, body any, admin bool) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	role := "user"
	if admin {
		role = "admin"
	}
	req = req.WithContext(tenantctx.With(req.Context(), tenantctx.Tenant{
		UserID:      1,
		Email:       "tester@opspilot.local",
		Role:        role,
		IsSuperuser: admin,
	}))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestAICostHTTP_AuthzAndQuotaCircuitLifecycle(t *testing.T) {
	r, svc := newTestRouter(t)

	// Non-admin receives 403
	rrForbidden := doReq(r, http.MethodGet, "/v1/ai-cost/summary", nil, false)
	require.Equal(t, http.StatusForbidden, rrForbidden.Code)

	// Record one sample log
	require.NoError(t, svc.Record(httptest.NewRequest(http.MethodGet, "/", nil).Context(), llm.GuardRecordInput{
		Provider: "openai", Model: "gpt-4o", Source: llm.SourceChat,
		UserID: 1, PromptTokens: 500, CompletionTokens: 100, TotalTokens: 600, Status: "ok",
	}))

	// Admin creates quota
	rrCreate := doReq(r, http.MethodPost, "/v1/ai-cost/quotas", map[string]any{
		"name":              "Global Guard",
		"scope_type":        "global",
		"scope_key":         "*",
		"enabled":           true,
		"daily_token_limit": 50000,
		"action_on_exceed":  "block",
	}, true)
	require.Equal(t, http.StatusCreated, rrCreate.Code)
	var qResp bizaicost.QuotaView
	require.NoError(t, json.Unmarshal(rrCreate.Body.Bytes(), &qResp))
	require.Equal(t, int64(600), qResp.DailyTokensUsed)

	// Manual trip & reset
	rrTrip := doReq(r, http.MethodPost, "/v1/ai-cost/quotas/1/trip", map[string]string{"reason": "ops pause"}, true)
	require.Equal(t, http.StatusOK, rrTrip.Code)
	require.Contains(t, rrTrip.Body.String(), `"circuit_open":true`)

	rrReset := doReq(r, http.MethodPost, "/v1/ai-cost/quotas/1/reset", nil, true)
	require.Equal(t, http.StatusOK, rrReset.Code)
	require.Contains(t, rrReset.Body.String(), `"circuit_open":false`)

	// Upsert custom model price
	rrPrice := doReq(r, http.MethodPut, "/v1/ai-cost/prices", map[string]any{
		"provider":          "custom",
		"model_pattern":     "qwen-72b*",
		"input_usd_per_1m":  0.8,
		"output_usd_per_1m": 1.6,
	}, true)
	require.Equal(t, http.StatusOK, rrPrice.Code)

	// Summary & logs
	rrSum := doReq(r, http.MethodGet, "/v1/ai-cost/summary?days=7", nil, true)
	require.Equal(t, http.StatusOK, rrSum.Code)
	require.Contains(t, rrSum.Body.String(), `"total_tokens":600`)

	rrLogs := doReq(r, http.MethodGet, "/v1/ai-cost/logs?limit=10", nil, true)
	require.Equal(t, http.StatusOK, rrLogs.Code)
	require.Contains(t, rrLogs.Body.String(), `"total":1`)
}
