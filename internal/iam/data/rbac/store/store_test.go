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

func runRBACStoreSuite(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	st := New(db)

	g1 := &model.ResourceGrant{
		SubjectType:     model.SubjectTypeUser,
		SubjectID:       10,
		ResourceType:    model.RBACResourceDeviceEnv,
		ResourcePattern: "prod",
		Actions:         model.RBACActionExec,
		Effect:          model.RBACEffectDeny,
		Description:     "deny prod exec",
		CreatedBy:       1,
	}
	g2 := &model.ResourceGrant{
		SubjectType:     model.SubjectTypeOrg,
		SubjectID:       2,
		ResourceType:    model.RBACResourceDeviceEnv,
		ResourcePattern: "staging",
		Actions:         "read,write",
		Effect:          model.RBACEffectAllow,
		Description:     "staging rw for SRE org",
		CreatedBy:       1,
	}
	require.NoError(t, st.Create(ctx, g1))
	require.NoError(t, st.Create(ctx, g2))

	rows, err := st.ListForSubjects(ctx, 10, []uint64{2, 99}, model.RBACResourceDeviceEnv)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	filtered, err := st.List(ctx, ListFilter{Effect: model.RBACEffectDeny})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, g1.ID, filtered[0].ID)

	g1.Description = "updated desc"
	require.NoError(t, st.Update(ctx, g1))
	got, err := st.GetByID(ctx, g1.ID)
	require.NoError(t, err)
	require.Equal(t, "updated desc", got.Description)

	require.NoError(t, st.Delete(ctx, g1.ID))
	_, err = st.GetByID(ctx, g1.ID)
	require.ErrorIs(t, err, errs.ErrNotFound)
}

func TestRBACStoreSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rbac.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	runRBACStoreSuite(t, db)
}

func TestRBACStoreMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL RBAC store integration test")
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

	cfg.DBName = "opspilot_rbac_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	runRBACStoreSuite(t, db)
}
