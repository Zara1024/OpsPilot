package device

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	biz "github.com/Zara1024/OpsPilot/internal/manager/biz/device"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/device"
	"github.com/Zara1024/OpsPilot/internal/pkg/authzmw"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type rbacDeviceRepo struct {
	biz.Repo
	items []*model.Device
}

func (r *rbacDeviceRepo) List(_ context.Context, _ biz.ListFilter) ([]*model.Device, error) {
	return r.items, nil
}

func (r *rbacDeviceRepo) Get(_ context.Context, id uint64) (*model.Device, error) {
	for _, d := range r.items {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errs.ErrNotFound
}

type rbacMockAuthorizer struct{}

func (rbacMockAuthorizer) AllowResource(_ context.Context, _ uint64, isAdmin bool, resourceType string, resourceKeys []string, _ string) (bool, error) {
	if isAdmin {
		return true, nil
	}
	if resourceType == authzmw.ResourceDeviceEnv && slices.Contains(resourceKeys, "prod") {
		return false, nil
	}
	return true, nil
}

func TestDeviceResourceRBACFiltersListAndDeniesGet(t *testing.T) {
	prodEnv := "prod"
	stagingEnv := "staging"
	repo := &rbacDeviceRepo{items: []*model.Device{
		{ID: 1, Name: "db-prod", Hostname: "db-prod", Environment: &prodEnv},
		{ID: 2, Name: "web-staging", Hostname: "web-staging", Environment: &stagingEnv},
	}}
	uc := biz.NewUsecase(repo, nil, nil)
	h := NewHandler(uc)
	mw := authzmw.New(nil, nil)
	mw.SetResourceAuthorizer(rbacMockAuthorizer{})
	h.SetAuthz(mw)

	router := chi.NewRouter()
	h.Register(router)

	// Regular user only sees staging device in list and gets 403 on prod device
	userCtx := tenantctx.With(context.Background(), tenantctx.Tenant{UserID: 9, Role: "user"})

	wList := httptest.NewRecorder()
	router.ServeHTTP(wList, httptest.NewRequest(http.MethodGet, "/v1/devices", nil).WithContext(userCtx))
	require.Equal(t, http.StatusOK, wList.Code)
	var listOut listResp
	require.NoError(t, json.Unmarshal(wList.Body.Bytes(), &listOut))
	require.Len(t, listOut.Items, 1)
	require.Equal(t, uint64(2), listOut.Items[0].ID)

	wProd := httptest.NewRecorder()
	router.ServeHTTP(wProd, httptest.NewRequest(http.MethodGet, "/v1/devices/1", nil).WithContext(userCtx))
	require.Equal(t, http.StatusForbidden, wProd.Code)

	wStaging := httptest.NewRecorder()
	router.ServeHTTP(wStaging, httptest.NewRequest(http.MethodGet, "/v1/devices/2", nil).WithContext(userCtx))
	require.Equal(t, http.StatusOK, wStaging.Code)

	// Admin sees everything
	adminCtx := tenantctx.With(context.Background(), tenantctx.Tenant{UserID: 1, Role: "admin", IsSuperuser: true})
	wAdminList := httptest.NewRecorder()
	router.ServeHTTP(wAdminList, httptest.NewRequest(http.MethodGet, "/v1/devices", nil).WithContext(adminCtx))
	require.Equal(t, http.StatusOK, wAdminList.Code)
	var adminOut listResp
	require.NoError(t, json.Unmarshal(wAdminList.Body.Bytes(), &adminOut))
	require.Len(t, adminOut.Items, 2)
}
