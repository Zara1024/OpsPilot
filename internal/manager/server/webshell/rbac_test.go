package webshell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Zara1024/OpsPilot/internal/pkg/authzmw"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type stubAuthz struct{}

func (stubAuthz) Allow(context.Context, uint64, uint64, string, string) bool   { return true }
func (stubAuthz) AllowAnyOrg(context.Context, uint64, string, string) bool { return true }

type denyProdExecAuthorizer struct{}

func (denyProdExecAuthorizer) AllowResource(_ context.Context, _ uint64, isAdmin bool, resourceType string, keys []string, action string) (bool, error) {
	if isAdmin {
		return true, nil
	}
	if resourceType == authzmw.ResourceDeviceEnv && action == authzmw.ActionExec {
		for _, k := range keys {
			if k == "prod" {
				return false, nil
			}
		}
	}
	return true, nil
}

type stubScopeResolver struct{}

func (stubScopeResolver) DeviceScopeKeys(_ context.Context, _ uint64) (string, string, string, error) {
	return "pay-core-01", "pay-core-01.internal", "prod", nil
}

func TestWebshellIssueTicketBlockedByResourceRBAC(t *testing.T) {
	mw := authzmw.New(stubAuthz{}, nil)
	mw.SetResourceAuthorizer(denyProdExecAuthorizer{})

	h := NewHandler(nil, nil, nil, nil, nil, nil, nil)
	h.SetAuthz(mw)
	h.SetDeviceScopeResolver(stubScopeResolver{})

	router := chi.NewRouter()
	h.Register(router)

	req := httptest.NewRequest(http.MethodPost, "/v1/devices/5/shell/tickets", strings.NewReader(`{"ssh_user":"ops","ssh_pass":"pass"}`))
	req = req.WithContext(tenantctx.With(req.Context(), tenantctx.Tenant{UserID: 42, Role: "user"}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
