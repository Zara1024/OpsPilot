package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	iamdatatoken "github.com/Zara1024/OpsPilot/internal/iam/data/integrationtoken"
	iamdatamembership "github.com/Zara1024/OpsPilot/internal/iam/data/membership/store"
	iamdataorg "github.com/Zara1024/OpsPilot/internal/iam/data/org/store"
	iamdatauser "github.com/Zara1024/OpsPilot/internal/iam/data/user/sqlite"
	managerbizskill "github.com/Zara1024/OpsPilot/internal/manager/biz/skill"
	manageraiopsdata "github.com/Zara1024/OpsPilot/internal/manager/data/aiops/store"
	manageralertdata "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	managerapprovaldata "github.com/Zara1024/OpsPilot/internal/manager/data/approval/store"
	manageraudtdata "github.com/Zara1024/OpsPilot/internal/manager/data/audit/store"
	managerdevicedata "github.com/Zara1024/OpsPilot/internal/manager/data/device/store"
	manageredgedata "github.com/Zara1024/OpsPilot/internal/manager/data/edge/store"
	externalmcpdata "github.com/Zara1024/OpsPilot/internal/manager/data/externalmcp"
	managerflowdata "github.com/Zara1024/OpsPilot/internal/manager/data/flow/store"
	managerimbridgedata "github.com/Zara1024/OpsPilot/internal/manager/data/imbridge/store"
	managerk8sdata "github.com/Zara1024/OpsPilot/internal/manager/data/k8s/store"
	managerknowledgedata "github.com/Zara1024/OpsPilot/internal/manager/data/knowledge/store"
	managerlogsdata "github.com/Zara1024/OpsPilot/internal/manager/data/logs/store"
	managermarketplacedata "github.com/Zara1024/OpsPilot/internal/manager/data/marketplace/store"
	managermcpdata "github.com/Zara1024/OpsPilot/internal/manager/data/mcp/store"
	managermetricdata "github.com/Zara1024/OpsPilot/internal/manager/data/metric/store"
	managermonitordata "github.com/Zara1024/OpsPilot/internal/manager/data/monitor/store"
	manageroncalldata "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	managerpacketcapturedata "github.com/Zara1024/OpsPilot/internal/manager/data/packetcapture/store"
	managerreportdata "github.com/Zara1024/OpsPilot/internal/manager/data/report/store"
	managersecretdata "github.com/Zara1024/OpsPilot/internal/manager/data/secret/store"
	managersettingdata "github.com/Zara1024/OpsPilot/internal/manager/data/setting/store"
	resultdata "github.com/Zara1024/OpsPilot/internal/manager/data/toolresult/store"
	managertopologydata "github.com/Zara1024/OpsPilot/internal/manager/data/topology/store"
	managerwebshelldata "github.com/Zara1024/OpsPilot/internal/manager/data/webshell/store"
	"github.com/Zara1024/OpsPilot/internal/pkg/coord"
	"github.com/Zara1024/OpsPilot/internal/pkg/dbx"
)

// TestMySQLFullSchemaMigrations 在真实 MySQL 8.0 严格模式下验证全部 29 个生产域的首次建表与幂等重复迁移。
func TestMySQLFullSchemaMigrations(t *testing.T) {
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL full-schema migration test")
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

	cfg.DBName = "opspilot_schema_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE DATABASE `"+cfg.DBName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error)
	t.Cleanup(func() {
		require.NoError(t, admin.Exec("DROP DATABASE `"+cfg.DBName+"`").Error)
	})

	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), quiet)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	migrators := []dbx.Migrator{
		iamdatauser.Migrate,
		iamdatatoken.Migrate,
		iamdataorg.Migrate,
		iamdatamembership.Migrate,
		manageralertdata.Migrate,
		managerdevicedata.Migrate,
		manageredgedata.Migrate,
		managerk8sdata.Migrate,
		managerlogsdata.Migrate,
		managertopologydata.Migrate,
		managermetricdata.Migrate,
		manageraiopsdata.Migrate,
		resultdata.Migrate,
		externalmcpdata.Migrate,
		managerbizskill.Migrate,
		managersettingdata.Migrate,
		managermarketplacedata.Migrate,
		managersecretdata.Migrate,
		managermcpdata.Migrate,
		managerapprovaldata.Migrate,
		managermonitordata.Migrate,
		managerwebshelldata.Migrate,
		manageraudtdata.Migrate,
		managerreportdata.Migrate,
		managerflowdata.Migrate,
		managerpacketcapturedata.Migrate,
		manageroncalldata.Migrate,
		managerimbridgedata.Migrate,
		managerknowledgedata.Migrate,
		coord.Migrate,
	}

	// 首次冷启动迁移 + 重启二次幂等迁移
	require.NoError(t, dbx.RunMigrations(db, log, migrators...))
	require.NoError(t, dbx.RunMigrations(db, log, migrators...))

	ctx := t.Context()
	_, err = managerflowdata.MigrateLegacyIMNotificationTool(ctx, db)
	require.NoError(t, err)
	require.NoError(t, managerk8sdata.RepairNodeHostIdentities(ctx, db, log))

	for _, table := range []string{
		"users",
		"iam_integration_tokens",
		"alert_incidents",
		"alert_ingest_sources",
		"alert_heartbeats",
		"oncall_schedules",
		"oncall_escalation_tasks",
		"devices",
		"chat_sessions",
		"im_apps",
		"knowledge_repos",
		"cluster_leases",
	} {
		require.True(t, db.Migrator().HasTable(table), "expected table %q after full schema migration", table)
	}
}
