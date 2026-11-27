package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/iam/biz/membership"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/org"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/rbac"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/user"
	membershipstore "github.com/Zara1024/OpsPilot/internal/iam/data/membership/store"
	orgstore "github.com/Zara1024/OpsPilot/internal/iam/data/org/store"
	rbacstore "github.com/Zara1024/OpsPilot/internal/iam/data/rbac/store"
	userstore "github.com/Zara1024/OpsPilot/internal/iam/data/user/sqlite"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/iam/service"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
)

func TestHTTP_RBACGrantsCRUDAndSimulator(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "iam_rbac_http.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Org{}, &model.OrgMembership{}))
	require.NoError(t, rbacstore.Migrate(db))

	ctx := context.Background()
	signer := auth.NewSigner("rbac-http-secret", 15*time.Minute, time.Hour)
	uRepo := userstore.NewRepo(db)
	userUC := user.NewUsecase(uRepo, signer, nil)
	require.NoError(t, userUC.BootstrapAdmin(ctx, "admin@test.com", "AdminPass123!"))
	devUser, err := userUC.Create(ctx, user.CreateInput{Email: "dev@test.com", Password: "DevPass123!", Role: model.RoleUser})
	require.NoError(t, err)

	orgSvc := org.New(orgstore.NewRepo(db), membershipstore.NewRepo(db), nil)
	_, err = orgSvc.EnsureSeed(ctx, "默认组织", "default")
	require.NoError(t, err)
	msSvc := membership.New(membershipstore.NewRepo(db), nil)
	rbacSvc := rbac.New(rbacstore.New(db), msSvc, uRepo)

	svc := service.New(userUC, nil)
	svc.SetOrgs(orgSvc)
	svc.SetMemberships(msSvc)
	svc.SetRBAC(rbacSvc)

	h := NewHandler(svc, nil)
	r := chi.NewRouter()
	h.RegisterPublic(r)
	r.Group(func(protected chi.Router) {
		protected.Use(auth.Middleware(signer))
		h.RegisterProtected(protected)
	})

	wAdmin := doJSONReq(t, r, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": "admin@test.com", "password": "AdminPass123!"})
	var adminTokens loginResp
	require.NoError(t, json.Unmarshal(wAdmin.Body.Bytes(), &adminTokens))

	wDev := doJSONReq(t, r, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": "dev@test.com", "password": "DevPass123!"})
	var devTokens loginResp
	require.NoError(t, json.Unmarshal(wDev.Body.Bytes(), &devTokens))

	// Non-admin forbidden
	require.Equal(t, http.StatusForbidden, doJSONReq(t, r, http.MethodGet, "/v1/iam/rbac/grants", devTokens.AccessToken, nil).Code)

	// Create deny rule
	wCreate := doJSONReq(t, r, http.MethodPost, "/v1/iam/rbac/grants", adminTokens.AccessToken, map[string]any{
		"subject_type":     "user",
		"subject_id":       devUser.ID,
		"resource_type":    "device_env",
		"resource_pattern": "prod*",
		"actions":          "exec",
		"effect":           "deny",
		"description":      "No prod SSH for devs",
	})
	require.Equal(t, http.StatusCreated, wCreate.Code)
	var created rbacGrantDTO
	require.NoError(t, json.Unmarshal(wCreate.Body.Bytes(), &created))

	// Simulator verifies deny
	wCheck := doJSONReq(t, r, http.MethodPost, "/v1/iam/rbac/check", adminTokens.AccessToken, map[string]any{
		"user_id":       devUser.ID,
		"resource_type": "device_env",
		"resource_keys": []string{"production"},
		"action":        "exec",
	})
	require.Equal(t, http.StatusOK, wCheck.Code)
	var dec rbac.Decision
	require.NoError(t, json.Unmarshal(wCheck.Body.Bytes(), &dec))
	require.False(t, dec.Allowed)
	require.Equal(t, "explicit_deny", dec.Reason)

	// Delete grant -> back to allowed
	require.Equal(t, http.StatusNoContent, doJSONReq(t, r, http.MethodDelete, "/v1/iam/rbac/grants/1", adminTokens.AccessToken, nil).Code)
	wCheckAfter := doJSONReq(t, r, http.MethodPost, "/v1/iam/rbac/check", adminTokens.AccessToken, map[string]any{
		"user_id":       devUser.ID,
		"resource_type": "device_env",
		"resource_keys": []string{"production"},
		"action":        "exec",
	})
	require.Equal(t, http.StatusOK, wCheckAfter.Code)
	require.NoError(t, json.Unmarshal(wCheckAfter.Body.Bytes(), &dec))
	require.True(t, dec.Allowed)
	require.Equal(t, "default_org_role", dec.Reason)
}
