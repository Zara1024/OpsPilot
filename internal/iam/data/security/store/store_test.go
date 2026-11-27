package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

func openTestSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "iam_security.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	return db
}

func runSecurityStoreSuite(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	st := New(db)

	// 1. AuthPolicy defaults + save
	def, err := st.GetAuthPolicy(ctx)
	require.NoError(t, err)
	require.Equal(t, model.MFAEnforceOff, def.MFAEnforceMode)
	require.True(t, def.AllowLocalPasswordLogin)

	def.MFAEnforceMode = model.MFAEnforceAdmins
	saved, err := st.SaveAuthPolicy(ctx, def)
	require.NoError(t, err)
	require.Equal(t, model.MFAEnforceAdmins, saved.MFAEnforceMode)

	// 2. Shared LoginThrottle window + reset
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		require.NoError(t, st.RecordLoginFailure(ctx, "email:a@test", now, 15*time.Minute))
	}
	blocked, err := st.IsLoginBlocked(ctx, "email:a@test", now, 3, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, blocked)

	// Expired window should not block
	blockedLater, err := st.IsLoginBlocked(ctx, "email:a@test", now.Add(16*time.Minute), 3, 15*time.Minute)
	require.NoError(t, err)
	require.False(t, blockedLater)

	require.NoError(t, st.ClearLoginAttempt(ctx, "email:a@test"))
	blockedAfterClear, err := st.IsLoginBlocked(ctx, "email:a@test", now, 3, 15*time.Minute)
	require.NoError(t, err)
	require.False(t, blockedAfterClear)

	// 3. UserMFA upsert + query + delete
	mfa := &model.UserMFA{
		UserID:             11,
		SecretEnc:          "v1:dummy",
		Enabled:            true,
		RecoveryHashesJSON: `["h1","h2"]`,
		LastUsedStep:       99,
	}
	require.NoError(t, st.UpsertUserMFA(ctx, mfa))
	enabledMap, err := st.ListUserMFAEnabled(ctx)
	require.NoError(t, err)
	require.True(t, enabledMap[11])
	require.NoError(t, st.DeleteUserMFA(ctx, 11))
	_, err = st.GetUserMFA(ctx, 11)
	require.ErrorIs(t, err, errs.ErrNotFound)

	// 4. SSOProvider + one-time SSOState + SSOIdentity
	prov := &model.SSOProvider{
		Slug:        "corp-oidc",
		Name:        "Corp OIDC",
		Type:        model.SSOTypeOIDC,
		Enabled:     true,
		ClientID:    "cid",
		DefaultRole: model.RoleUser,
	}
	require.NoError(t, st.CreateSSOProvider(ctx, prov))

	stateVal := "st-" + uuid.NewString()
	require.NoError(t, st.CreateSSOState(ctx, &model.SSOState{
		State:        stateVal,
		ProviderSlug: prov.Slug,
		CodeVerifier: "ver",
		Nonce:        "non",
		ExpiresAt:    now.Add(5 * time.Minute),
	}))
	consumed, err := st.ConsumeSSOState(ctx, stateVal, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "ver", consumed.CodeVerifier)
	// Second consumption must fail (replay protection)
	_, err = st.ConsumeSSOState(ctx, stateVal, now.Add(time.Minute))
	require.ErrorIs(t, err, errs.ErrNotFound)

	ident := &model.SSOIdentity{
		ProviderID:    prov.ID,
		ExternalSub:   "sub-123",
		UserID:        22,
		ExternalEmail: "sso@example.com",
		ExternalName:  "SSO User",
	}
	require.NoError(t, st.UpsertSSOIdentity(ctx, ident))
	listIdents, err := st.ListSSOIdentitiesByUser(ctx, 22)
	require.NoError(t, err)
	require.Len(t, listIdents, 1)
	require.Equal(t, "corp-oidc", listIdents[0].ProviderSlug)

	require.NoError(t, st.DeleteSSOProvider(ctx, prov.ID))
	listAfterDel, err := st.ListSSOIdentitiesByUser(ctx, 22)
	require.NoError(t, err)
	require.Empty(t, listAfterDel)
}

func TestSecurityStoreSQLite(t *testing.T) {
	db := openTestSQLiteDB(t)
	runSecurityStoreSuite(t, db)
}

func TestSecurityStoreMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL security store integration test")
	}
	cfg, err := driver.ParseDSN(dsn)
	require.NoError(t, err)
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["sql_mode"] = "'STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'"

	quiet := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(mysql.Open(cfg.FormatDSN()), quiet)
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, adminSQL.Close()) })

	cfg.DBName = "opspilot_iamsec_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE DATABASE `"+cfg.DBName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error)
	t.Cleanup(func() {
		require.NoError(t, admin.Exec("DROP DATABASE `"+cfg.DBName+"`").Error)
	})

	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), quiet)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	runSecurityStoreSuite(t, db)
}
