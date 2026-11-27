package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/flow"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openFlowLeaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "flow_lease.db")), &gorm.Config{
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
		t.Fatalf("migrate flow: %v", err)
	}
	return db
}

func TestRunRepoRenewAndSweepExpiredRuns(t *testing.T) {
	db := openFlowLeaseTestDB(t)
	runs := NewRunRepo(db)
	ctx := context.Background()

	now := time.Date(2026, 11, 27, 11, 0, 0, 0, time.UTC)
	liveUntil := now.Add(45 * time.Second)
	expiredUntil := now.Add(-15 * time.Second)

	rows := []*model.FlowRun{
		{
			ID:             "live-replica-a",
			FlowID:         1,
			FlowVersion:    1,
			Status:         model.RunStatusRunning,
			TriggerType:    model.TriggerManual,
			TriggerJSON:    "{}",
			ExecutionOwner: "replica-a",
			LeaseUntil:     &liveUntil,
		},
		{
			ID:             "crashed-replica-b",
			FlowID:         1,
			FlowVersion:    1,
			Status:         model.RunStatusRunning,
			TriggerType:    model.TriggerManual,
			TriggerJSON:    "{}",
			ExecutionOwner: "replica-b",
			LeaseUntil:     &expiredUntil,
		},
		{
			ID:             "legacy-old",
			FlowID:         1,
			FlowVersion:    1,
			Status:         model.RunStatusRunning,
			TriggerType:    model.TriggerManual,
			TriggerJSON:    "{}",
			ExecutionOwner: "",
		},
		{
			ID:             "legacy-fresh",
			FlowID:         1,
			FlowVersion:    1,
			Status:         model.RunStatusRunning,
			TriggerType:    model.TriggerManual,
			TriggerJSON:    "{}",
			ExecutionOwner: "",
		},
	}
	for _, r := range rows {
		if err := runs.CreateRun(ctx, r); err != nil {
			t.Fatalf("create %s: %v", r.ID, err)
		}
	}
	// 显式调整 legacy-old / legacy-fresh 的 updated_at 以覆盖历史兼容分支。
	if err := db.Model(&model.FlowRun{}).Where("id = ?", "legacy-old").Update("updated_at", now.Add(-10*time.Minute)).Error; err != nil {
		t.Fatalf("age legacy-old: %v", err)
	}
	if err := db.Model(&model.FlowRun{}).Where("id = ?", "legacy-fresh").Update("updated_at", now.Add(-1*time.Minute)).Error; err != nil {
		t.Fatalf("touch legacy-fresh: %v", err)
	}

	// 非 owner 续约必须返回 ErrConflict。
	if err := runs.RenewRunLease(ctx, "live-replica-a", "replica-b", now.Add(time.Minute)); err != errs.ErrConflict {
		t.Fatalf("non-owner renew err = %v, want ErrConflict", err)
	}
	// owner 续约成功更新 lease_until。
	newUntil := now.Add(90 * time.Second)
	if err := runs.RenewRunLease(ctx, "live-replica-a", "replica-a", newUntil); err != nil {
		t.Fatalf("owner renew err = %v", err)
	}

	swept, err := runs.SweepExpiredRuns(ctx, now, now.Add(-5*time.Minute), "expired")
	if err != nil {
		t.Fatalf("SweepExpiredRuns err = %v", err)
	}
	if swept != 2 {
		t.Fatalf("swept = %d, want 2 (crashed-replica-b + legacy-old)", swept)
	}

	live, err := runs.GetRun(ctx, "live-replica-a")
	if err != nil || live.Status != model.RunStatusRunning || live.LeaseUntil == nil || !live.LeaseUntil.Equal(newUntil) {
		t.Fatalf("live run disturbed: %+v err=%v", live, err)
	}
	freshLegacy, err := runs.GetRun(ctx, "legacy-fresh")
	if err != nil || freshLegacy.Status != model.RunStatusRunning {
		t.Fatalf("fresh legacy run disturbed: %+v err=%v", freshLegacy, err)
	}
	crashed, err := runs.GetRun(ctx, "crashed-replica-b")
	if err != nil || crashed.Status != model.RunStatusFailed || crashed.Error != "expired" || crashed.FinishedAt == nil {
		t.Fatalf("crashed run not failed cleanly: %+v err=%v", crashed, err)
	}
	oldLegacy, err := runs.GetRun(ctx, "legacy-old")
	if err != nil || oldLegacy.Status != model.RunStatusFailed {
		t.Fatalf("old legacy run not swept: %+v err=%v", oldLegacy, err)
	}
}
