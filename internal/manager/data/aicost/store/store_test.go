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

	model "github.com/Zara1024/OpsPilot/internal/manager/model/aicost"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

func openTestSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "aicost.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	return db
}

func runAICostStoreSuite(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	st := New(db)
	now := time.Now().UTC().Truncate(time.Second)

	// 1. Insert & list usage logs + aggregations
	require.NoError(t, st.InsertUsage(ctx, &model.UsageLog{
		UserID:           1,
		OrgID:            10,
		Provider:         "openai",
		Model:            "gpt-4o",
		Source:           "chat",
		SessionID:        "sess-1",
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		CostUSD:          0.0075,
		LatencyMS:        420,
		Status:           model.UsageStatusOK,
		CreatedAt:        now,
	}))
	require.NoError(t, st.InsertUsage(ctx, &model.UsageLog{
		UserID:       2,
		OrgID:        10,
		Provider:     "deepseek",
		Model:        "deepseek-chat",
		Source:       "alert_rca",
		Status:       model.UsageStatusBlocked,
		ErrorMessage: "quota exceeded",
		CreatedAt:    now,
	}))

	logs, total, err := st.ListUsageLogs(ctx, LogFilter{OrgID: 10, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, logs, 2)

	win, err := st.SumUsageWindow(ctx, model.ScopeGlobal, "*", now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1500), win.TotalTokens)
	require.Equal(t, int64(1), win.BlockedCount)

	slices, err := st.ListUsageByModelSince(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, slices, 1)
	require.Equal(t, "gpt-4o", slices[0].Model)

	overview, err := st.AggregateOverview(ctx, now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(2), overview.Totals.Requests)
	require.NotEmpty(t, overview.Daily)
	require.NotEmpty(t, overview.ByModel)
	require.NotEmpty(t, overview.BySource)

	// 2. Quotas & boolean MySQL safety + circuit breaker state
	q := &model.CostQuota{
		Name:              "Global Daily Cap",
		ScopeType:         model.ScopeGlobal,
		ScopeKey:          "*",
		Enabled:           true,
		DailyTokenLimit:   10000,
		DailyCostLimitUSD: 5.0,
		WarnThresholdPct:  80,
		ActionOnExceed:    model.ActionBlock,
	}
	require.NoError(t, st.CreateQuota(ctx, q))
	err = st.CreateQuota(ctx, &model.CostQuota{
		Name:           "Duplicate Global",
		ScopeType:      model.ScopeGlobal,
		ScopeKey:       "*",
		Enabled:        true,
		ActionOnExceed: model.ActionBlock,
	})
	require.ErrorIs(t, err, errs.ErrConflict)

	// Flip enabled=false & circuit_open=true then back
	q.Enabled = false
	require.NoError(t, st.UpdateQuota(ctx, q))
	openedAt := now
	require.NoError(t, st.SetCircuitState(ctx, q.ID, true, &openedAt, "manual:test"))
	gotQ, err := st.GetQuotaByID(ctx, q.ID)
	require.NoError(t, err)
	require.False(t, gotQ.Enabled)
	require.True(t, gotQ.CircuitOpen)
	require.Equal(t, "manual:test", gotQ.CircuitReason)

	require.NoError(t, st.SetCircuitState(ctx, q.ID, false, nil, ""))
	gotQ2, err := st.GetQuotaByID(ctx, q.ID)
	require.NoError(t, err)
	require.False(t, gotQ2.CircuitOpen)

	// 3. ModelPrice Upsert & Delete
	p1, err := st.UpsertPrice(ctx, &model.ModelPrice{
		Provider:       "openai",
		ModelPattern:   "gpt-4o*",
		InputUSDPer1M:  2.5,
		OutputUSDPer1M: 10.0,
		Currency:       "USD",
	})
	require.NoError(t, err)
	p2, err := st.UpsertPrice(ctx, &model.ModelPrice{
		Provider:       "openai",
		ModelPattern:   "gpt-4o*",
		InputUSDPer1M:  3.0,
		OutputUSDPer1M: 12.0,
		Currency:       "USD",
	})
	require.NoError(t, err)
	require.Equal(t, p1.ID, p2.ID)
	require.Equal(t, 3.0, p2.InputUSDPer1M)
	require.NoError(t, st.DeletePrice(ctx, p1.ID))
	require.NoError(t, st.DeleteQuota(ctx, q.ID))
}

func TestAICostStoreSQLite(t *testing.T) {
	db := openTestSQLiteDB(t)
	runAICostStoreSuite(t, db)
}

func TestAICostStoreMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL aicost store integration test")
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

	cfg.DBName = "opspilot_aicost_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	runAICostStoreSuite(t, db)
}
