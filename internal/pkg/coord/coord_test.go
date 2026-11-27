package coord

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTestSQLiteStore(t *testing.T) *GormStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "coord.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlDB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate twice 1: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate twice 2: %v", err)
	}
	return NewGormStore(db)
}

func TestGormStoreAcquireTakeoverReleaseAndFencingMonotonic(t *testing.T) {
	store := openTestSQLiteStore(t)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	store.setNow(func() time.Time { return now })

	ctx := context.Background()
	l1, ok, err := store.TryAcquireOrRenew(ctx, "alert-evaluator", "replica-a", 30*time.Second)
	if err != nil || !ok || l1.FencingToken != 1 || l1.HolderID != "replica-a" {
		t.Fatalf("first acquire = %+v, ok=%t, err=%v", l1, ok, err)
	}

	// 租约未过期时，其他副本竞争必须返回 false。
	_, ok, err = store.TryAcquireOrRenew(ctx, "alert-evaluator", "replica-b", 30*time.Second)
	if err != nil || ok {
		t.Fatalf("contender should lose while lease active: ok=%t err=%v", ok, err)
	}

	// 同副本续约：fencing_token 保持为 1 且 expires_at 后延。
	now = now.Add(10 * time.Second)
	l1Renew, ok, err := store.TryAcquireOrRenew(ctx, "alert-evaluator", "replica-a", 30*time.Second)
	if err != nil || !ok || l1Renew.FencingToken != 1 || !l1Renew.ExpiresAt.After(l1.ExpiresAt) {
		t.Fatalf("renew = %+v, ok=%t, err=%v", l1Renew, ok, err)
	}

	// 非持有者调用 Release 不得释放持有者的租约。
	if err := store.Release(ctx, "alert-evaluator", "replica-b"); err != nil {
		t.Fatalf("non-owner release err = %v", err)
	}
	got, err := store.Get(ctx, "alert-evaluator")
	if err != nil || got == nil || got.HolderID != "replica-a" || !got.ExpiresAt.After(now) {
		t.Fatalf("lease modified by non-owner: %+v err=%v", got, err)
	}

	// 活跃对等节点检查。
	hasPeer, err := store.HasOtherActiveHolder(ctx, "replica-b")
	if err != nil || !hasPeer {
		t.Fatalf("HasOtherActiveHolder(replica-b) = %t, err=%v; want true", hasPeer, err)
	}
	hasSelfPeer, err := store.HasOtherActiveHolder(ctx, "replica-a")
	if err != nil || hasSelfPeer {
		t.Fatalf("HasOtherActiveHolder(replica-a) = %t, err=%v; want false", hasSelfPeer, err)
	}

	// 租约过期后，replica-b 接管，fencing_token 严格递增至 2。
	now = now.Add(35 * time.Second)
	l2, ok, err := store.TryAcquireOrRenew(ctx, "alert-evaluator", "replica-b", 30*time.Second)
	if err != nil || !ok || l2.HolderID != "replica-b" || l2.FencingToken != 2 {
		t.Fatalf("expired takeover = %+v, ok=%t, err=%v", l2, ok, err)
	}

	// replica-b 主动释放后，HasOtherActiveHolder 变为 false 且 replica-a 立即接管（token = 3）。
	if err := store.Release(ctx, "alert-evaluator", "replica-b"); err != nil {
		t.Fatalf("owner release err = %v", err)
	}
	hasPeer, err = store.HasOtherActiveHolder(ctx, "replica-a")
	if err != nil || hasPeer {
		t.Fatalf("after release HasOtherActiveHolder = %t, err=%v; want false", hasPeer, err)
	}
	l3, ok, err := store.TryAcquireOrRenew(ctx, "alert-evaluator", "replica-a", 30*time.Second)
	if err != nil || !ok || l3.HolderID != "replica-a" || l3.FencingToken != 3 {
		t.Fatalf("re-acquire after release = %+v, ok=%t, err=%v", l3, ok, err)
	}
}

func TestGormStoreConcurrentMutualExclusion(t *testing.T) {
	store := openTestSQLiteStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		holder := NewInstanceID("")
		go func(id string) {
			defer wg.Done()
			_, ok, err := store.TryAcquireOrRenew(ctx, "flow-scheduler", id, 30*time.Second)
			if err != nil {
				t.Errorf("holder %s acquire err: %v", id, err)
				return
			}
			if ok {
				winners.Add(1)
			}
		}(holder)
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("concurrent winners = %d, want 1", winners.Load())
	}
}

func TestElectorFailoverAndFailClosedStepDown(t *testing.T) {
	store := openTestSQLiteStore(t)
	reg := prometheus.NewRegistry()
	log := slog.New(slog.DiscardHandler)

	cfg := ElectorConfig{
		TTL:           250 * time.Millisecond,
		RenewInterval: 60 * time.Millisecond,
		RetryInterval: 30 * time.Millisecond,
	}
	eA := NewElector(store, "replica-a", cfg, reg, log)
	eB := NewElector(store, "replica-b", cfg, reg, log)

	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelA()
	defer cancelB()

	aEntered := make(chan struct{})
	aExited := make(chan struct{})
	bEntered := make(chan struct{})
	bExited := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32

	track := func(delta int32) {
		cur := active.Add(delta)
		for {
			prev := maxActive.Load()
			if cur <= prev || maxActive.CompareAndSwap(prev, cur) {
				break
			}
		}
	}

	go func() {
		_ = eA.RunWithLeadership(ctxA, "imbridge-stream", func(leaderCtx context.Context) error {
			track(1)
			defer func() {
				track(-1)
				close(aExited)
			}()
			close(aEntered)
			<-leaderCtx.Done()
			return nil
		})
	}()

	select {
	case <-aEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("replica A did not acquire leadership")
	}

	go func() {
		_ = eB.RunWithLeadership(ctxB, "imbridge-stream", func(leaderCtx context.Context) error {
			track(1)
			defer func() {
				track(-1)
				close(bExited)
			}()
			select {
			case <-bEntered:
			default:
				close(bEntered)
			}
			<-leaderCtx.Done()
			return nil
		})
	}()

	// 等待几个续约周期，确保备副本未并发启动。
	time.Sleep(180 * time.Millisecond)
	select {
	case <-bEntered:
		t.Fatal("replica B acquired leadership while A was healthy")
	default:
	}

	// 取消 A，模拟优雅下线后 B 立即接管。
	cancelA()
	<-aExited
	select {
	case <-bEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("replica B did not take over after A stepped down")
	}

	cancelB()
	<-bExited
	if maxActive.Load() != 1 {
		t.Fatalf("max concurrent leaders = %d, want 1", maxActive.Load())
	}
}

func TestElectorStepsDownWhenLeaseStolen(t *testing.T) {
	store := openTestSQLiteStore(t)
	log := slog.New(slog.DiscardHandler)
	cfg := ElectorConfig{
		TTL:           300 * time.Millisecond,
		RenewInterval: 50 * time.Millisecond,
		RetryInterval: 30 * time.Millisecond,
	}
	eA := NewElector(store, "replica-a", cfg, nil, log)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	entered := make(chan struct{})
	steppedDown := make(chan struct{})
	go func() {
		_ = eA.RunWithLeadership(ctxA, "report-scheduler", func(leaderCtx context.Context) error {
			close(entered)
			<-leaderCtx.Done()
			close(steppedDown)
			return nil
		})
	}()

	<-entered
	// 强制改写数据库 holder_id 模拟发生租约脑裂接管。
	if err := store.db.Model(&ClusterLease{}).
		Where("lease_key = ?", "report-scheduler").
		Updates(map[string]any{"holder_id": "replica-b", "fencing_token": 99}).Error; err != nil {
		t.Fatalf("force takeover: %v", err)
	}

	select {
	case <-steppedDown:
	case <-time.After(2 * time.Second):
		t.Fatal("replica A did not step down fail-closed when lease holder changed")
	}
}
