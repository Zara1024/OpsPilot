package k8s

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	biz "github.com/Zara1024/OpsPilot/internal/manager/biz/k8s"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/k8s"
	"github.com/Zara1024/OpsPilot/internal/pkg/authzmw"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type rbacK8sService struct {
	Service
	clusters []*model.Cluster
}

func (s *rbacK8sService) ListClusters(_ context.Context, _ biz.ListClustersFilter) ([]*model.Cluster, error) {
	return s.clusters, nil
}

func (s *rbacK8sService) CountClusters(_ context.Context, _ biz.ListClustersFilter) (int64, error) {
	return int64(len(s.clusters)), nil
}

func (s *rbacK8sService) GetCluster(_ context.Context, id uint64) (*model.Cluster, error) {
	for _, c := range s.clusters {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, errs.ErrNotFound
}

func (s *rbacK8sService) GetNodeCoverage(_ context.Context, _ uint64) (biz.NodeCoverage, error) {
	return biz.NodeCoverage{}, nil
}

func (s *rbacK8sService) GetNodeCoverageByClusterIDs(_ context.Context, _ []uint64) (map[uint64]biz.NodeCoverage, error) {
	return map[uint64]biz.NodeCoverage{}, nil
}

func (s *rbacK8sService) UpgradeCommand(_ *model.Cluster) string { return "" }

type stagingClusterAuthorizer struct{}

func (stagingClusterAuthorizer) AllowResource(_ context.Context, _ uint64, isAdmin bool, resourceType string, keys []string, _ string) (bool, error) {
	if isAdmin {
		return true, nil
	}
	if resourceType == authzmw.ResourceK8sCluster {
		return slices.Contains(keys, "staging-eks"), nil
	}
	return true, nil
}

func TestK8sResourceRBACFiltersAndDenies(t *testing.T) {
	svc := &rbacK8sService{clusters: []*model.Cluster{
		{ID: 1, Name: "prod-eks"},
		{ID: 2, Name: "staging-eks"},
	}}
	h := NewHandler(svc)
	mw := authzmw.New(nil, nil)
	mw.SetResourceAuthorizer(stagingClusterAuthorizer{})
	h.SetAuthz(mw)

	router := chi.NewRouter()
	h.RegisterProtected(router)
	userCtx := tenantctx.With(context.Background(), tenantctx.Tenant{UserID: 8, Role: "user"})

	wList := httptest.NewRecorder()
	router.ServeHTTP(wList, httptest.NewRequest(http.MethodGet, "/v1/k8s/clusters", nil).WithContext(userCtx))
	require.Equal(t, http.StatusOK, wList.Code)
	var listResp struct {
		Items []clusterDTO `json:"items"`
		Total int64        `json:"total"`
	}
	require.NoError(t, json.Unmarshal(wList.Body.Bytes(), &listResp))
	require.Len(t, listResp.Items, 1)
	require.Equal(t, "staging-eks", listResp.Items[0].Name)

	wProd := httptest.NewRecorder()
	router.ServeHTTP(wProd, httptest.NewRequest(http.MethodGet, "/v1/k8s/clusters/1/pods", nil).WithContext(userCtx))
	require.Equal(t, http.StatusForbidden, wProd.Code)

	wStaging := httptest.NewRecorder()
	router.ServeHTTP(wStaging, httptest.NewRequest(http.MethodGet, "/v1/k8s/clusters/2", nil).WithContext(userCtx))
	require.Equal(t, http.StatusOK, wStaging.Code)
}
