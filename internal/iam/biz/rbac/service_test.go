package rbac

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/iam/biz/membership"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/org"
	membershipstore "github.com/Zara1024/OpsPilot/internal/iam/data/membership/store"
	orgstore "github.com/Zara1024/OpsPilot/internal/iam/data/org/store"
	rbacstore "github.com/Zara1024/OpsPilot/internal/iam/data/rbac/store"
	userstore "github.com/Zara1024/OpsPilot/internal/iam/data/user/sqlite"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
)

func TestRBACDecisionSemantics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rbac_biz.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Org{}, &model.OrgMembership{}))
	require.NoError(t, rbacstore.Migrate(db))

	ctx := context.Background()
	uRepo := userstore.NewRepo(db)
	devUser := &model.User{Email: "dev@test.com", Role: model.RoleUser, Status: model.StatusActive}
	adminUser := &model.User{Email: "admin@test.com", Role: model.RoleAdmin, IsSuperuser: true, Status: model.StatusActive}
	require.NoError(t, uRepo.Create(ctx, devUser))
	require.NoError(t, uRepo.Create(ctx, adminUser))

	orgSvc := org.New(orgstore.NewRepo(db), membershipstore.NewRepo(db), nil)
	sreOrg, err := orgSvc.EnsureSeed(ctx, "SRE Team", "ops")
	require.NoError(t, err)
	msSvc := membership.New(membershipstore.NewRepo(db), nil)
	_, err = msSvc.AddOrUpdate(ctx, devUser.ID, sreOrg.ID, model.MembershipRoleMember)
	require.NoError(t, err)

	st := rbacstore.New(db)
	svc := New(st, msSvc, uRepo)

	// 1. Zero rules -> backward compatible default_org_role
	dec, err := svc.Check(ctx, devUser.ID, false, model.RBACResourceService, []string{"payment-api"}, model.RBACActionRead)
	require.NoError(t, err)
	require.True(t, dec.Allowed)
	require.Equal(t, "default_org_role", dec.Reason)

	// 2. Org-inherited allowlist scopes devUser strictly to matching services
	_, err = svc.CreateGrant(ctx, adminUser.ID, GrantInput{
		SubjectType:     model.SubjectTypeOrg,
		SubjectID:       sreOrg.ID,
		ResourceType:    model.RBACResourceService,
		ResourcePattern: "order-*",
		Actions:         "read,write",
		Effect:          model.RBACEffectAllow,
	})
	require.NoError(t, err)

	decOrder, err := svc.Check(ctx, devUser.ID, false, model.RBACResourceService, []string{"order-checkout"}, model.RBACActionRead)
	require.NoError(t, err)
	require.True(t, decOrder.Allowed)
	require.Equal(t, "explicit_allow", decOrder.Reason)

	decPayment, err := svc.Check(ctx, devUser.ID, false, model.RBACResourceService, []string{"payment-api"}, model.RBACActionRead)
	require.NoError(t, err)
	require.False(t, decPayment.Allowed)
	require.Equal(t, "not_in_allow_scope", decPayment.Reason)

	// 3. Explicit deny ALWAYS overrides allow (e.g. allow all devices read/exec, but deny exec on prod env)
	_, err = svc.CreateGrant(ctx, adminUser.ID, GrantInput{
		SubjectType:     model.SubjectTypeUser,
		SubjectID:       devUser.ID,
		ResourceType:    model.RBACResourceDeviceEnv,
		ResourcePattern: "*",
		Actions:         "read,exec",
		Effect:          model.RBACEffectAllow,
	})
	require.NoError(t, err)
	_, err = svc.CreateGrant(ctx, adminUser.ID, GrantInput{
		SubjectType:     model.SubjectTypeUser,
		SubjectID:       devUser.ID,
		ResourceType:    model.RBACResourceDeviceEnv,
		ResourcePattern: "prod*",
		Actions:         "exec",
		Effect:          model.RBACEffectDeny,
	})
	require.NoError(t, err)

	decProdRead, err := svc.Check(ctx, devUser.ID, false, model.RBACResourceDeviceEnv, []string{"production"}, model.RBACActionRead)
	require.NoError(t, err)
	require.True(t, decProdRead.Allowed)

	decProdExec, err := svc.Check(ctx, devUser.ID, false, model.RBACResourceDeviceEnv, []string{"production"}, model.RBACActionExec)
	require.NoError(t, err)
	require.False(t, decProdExec.Allowed)
	require.Equal(t, "explicit_deny", decProdExec.Reason)

	// 4. Admin bypasses restrictions
	decAdmin, err := svc.Check(ctx, adminUser.ID, true, model.RBACResourceDeviceEnv, []string{"production"}, model.RBACActionExec)
	require.NoError(t, err)
	require.True(t, decAdmin.Allowed)
	require.Equal(t, "admin_bypass", decAdmin.Reason)
}
