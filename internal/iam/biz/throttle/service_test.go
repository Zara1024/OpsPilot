package throttle

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	securitystore "github.com/Zara1024/OpsPilot/internal/iam/data/security/store"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

func newTestThrottle(t *testing.T) (*Service, *securitystore.Store, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "throttle.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, securitystore.Migrate(db))

	st := securitystore.New(db)
	svc := New(st)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, st, &now
}

func TestThrottleSharedAcrossServiceInstances(t *testing.T) {
	svcA, st, now := newTestThrottle(t)
	// 模拟第二个副本实例化同一个 DB store。
	svcB := New(st)
	svcB.now = func() time.Time { return *now }

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		require.NoError(t, svcA.Check(ctx, "1.2.3.4", "alice@example.com"))
		require.NoError(t, svcA.RecordFailure(ctx, "1.2.3.4", "alice@example.com"))
	}
	// 副本 B 必须立即可见限流状态并返回 ErrTooManyAttempts。
	err := svcB.Check(ctx, "9.9.9.9", "alice@example.com")
	require.ErrorIs(t, err, errs.ErrTooManyAttempts)

	// 其他邮箱在同一 IP 未超 IP 限额（20）时不受 alice 邮箱锁死影响。
	require.NoError(t, svcB.Check(ctx, "1.2.3.4", "bob@example.com"))

	// 成功后清除邮箱计数。
	require.NoError(t, svcB.RecordSuccess(ctx, "alice@example.com"))
	require.NoError(t, svcA.Check(ctx, "9.9.9.9", "alice@example.com"))
}

func TestPolicyGuardsLocalPasswordLockout(t *testing.T) {
	svc, st, _ := newTestThrottle(t)
	ctx := context.Background()

	// 未启用任何 SSO 提供商时禁止关闭本地密码登录。
	_, err := svc.UpdatePolicy(ctx, PolicyInput{
		MFAEnforceMode:          model.MFAEnforceAdmins,
		AllowLocalPasswordLogin: false,
		LoginIPLimit:            20,
		LoginIPWindowSec:        300,
		LoginEmailLimit:         6,
		LoginEmailWindowSec:     900,
	})
	require.ErrorIs(t, err, errs.ErrInvalid)

	require.NoError(t, st.CreateSSOProvider(ctx, &model.SSOProvider{
		Slug:        "okta",
		Name:        "Okta",
		Type:        model.SSOTypeOIDC,
		Enabled:     true,
		ClientID:    "cid",
		DefaultRole: model.RoleUser,
	}))

	updated, err := svc.UpdatePolicy(ctx, PolicyInput{
		MFAEnforceMode:          model.MFAEnforceAdmins,
		AllowLocalPasswordLogin: false,
		LoginIPLimit:            15,
		LoginIPWindowSec:        300,
		LoginEmailLimit:         5,
		LoginEmailWindowSec:     600,
	})
	require.NoError(t, err)
	require.False(t, updated.AllowLocalPasswordLogin)

	allowed, err := svc.LocalLoginAllowed(ctx)
	require.NoError(t, err)
	require.False(t, allowed)

	reqAdmin, err := svc.RequiresMFAForRole(ctx, model.RoleAdmin)
	require.NoError(t, err)
	require.True(t, reqAdmin)

	reqUser, err := svc.RequiresMFAForRole(ctx, model.RoleUser)
	require.NoError(t, err)
	require.False(t, reqUser)
}
