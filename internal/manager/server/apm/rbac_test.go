package apm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	biz "github.com/Zara1024/OpsPilot/internal/manager/biz/apm"
	"github.com/Zara1024/OpsPilot/internal/pkg/authzmw"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type denyPaymentServiceAuthorizer struct{}

func (denyPaymentServiceAuthorizer) AllowResource(_ context.Context, _ uint64, isAdmin bool, resourceType string, keys []string, _ string) (bool, error) {
	if isAdmin {
		return true, nil
	}
	if resourceType == authzmw.ResourceService && slices.Contains(keys, "payment-api") {
		return false, nil
	}
	return true, nil
}

func TestAPMResourceRBACDeniesRestrictedServiceDetail(t *testing.T) {
	h := NewHandler(biz.New(nil, nil, nil), nil)
	mw := authzmw.New(nil, nil)
	mw.SetResourceAuthorizer(denyPaymentServiceAuthorizer{})
	h.SetAuthz(mw)

	router := chi.NewRouter()
	h.Register(router)

	req := httptest.NewRequest(http.MethodGet, "/v1/apm/overview?start=2026-09-07T00:00:00Z&end=2026-09-07T01:00:00Z&service_name=payment-api&environment=prod&service_namespace=core", nil)
	req = req.WithContext(tenantctx.With(req.Context(), tenantctx.Tenant{UserID: 5, Role: "user"}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
