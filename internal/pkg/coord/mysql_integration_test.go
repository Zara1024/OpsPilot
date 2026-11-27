package coord

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestClusterLeaseMySQLIntegration 在真实 MySQL 8.0 严格模式下验证建表、并发抢占、续约、过期接管与释放。
func TestClusterLeaseMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL coord integration test")
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

	cfg.DBName = "opspilot_coord_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	ctx := context.Background()
	store := NewGormStore(db)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	store.setNow(func() time.Time { return now })

	// 8 并发抢占同一个 key：仅允许 1 个胜出者，且 token = 1。
	var wg sync.WaitGroup
	var winners atomic.Int32
	var winnerHolder string
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		holder := "mysql-replica-" + uuid.NewString()[:8]
		go func(h string) {
			defer wg.Done()
			l, ok, aErr := store.TryAcquireOrRenew(ctx, "cluster-reconcilers", h, 30*time.Second)
			require.NoError(t, aErr)
			if ok {
				require.EqualValues(t, 1, l.FencingToken)
				winners.Add(1)
				mu.Lock()
				winnerHolder = h
				mu.Unlock()
			}
		}(holder)
	}
	wg.Wait()
	require.EqualValues(t, 1, winners.Load())

	// 同持有者续约保持 token=1。
	now = now.Add(10 * time.Second)
	renewed, ok, err := store.TryAcquireOrRenew(ctx, "cluster-reconcilers", winnerHolder, 30*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, 1, renewed.FencingToken)

	// 非持有者释放无效，活跃检查仍为 true。
	require.NoError(t, store.Release(ctx, "cluster-reconcilers", "other-replica"))
	hasPeer, err := store.HasOtherActiveHolder(ctx, "other-replica")
	require.NoError(t, err)
	require.True(t, hasPeer)

	// 过期后由另一副本接管，fencing_token = 2。
	now = now.Add(35 * time.Second)
	taken, ok, err := store.TryAcquireOrRenew(ctx, "cluster-reconcilers", "other-replica", 30*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "other-replica", taken.HolderID)
	require.EqualValues(t, 2, taken.FencingToken)

	// 优雅释放后原副本再次抢占，fencing_token = 3（严格单调）。
	require.NoError(t, store.Release(ctx, "cluster-reconcilers", "other-replica"))
	reacquired, ok, err := store.TryAcquireOrRenew(ctx, "cluster-reconcilers", winnerHolder, 30*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, 3, reacquired.FencingToken)
}
