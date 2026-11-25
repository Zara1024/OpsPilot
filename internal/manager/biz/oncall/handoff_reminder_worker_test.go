package oncall

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	alertstore "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	oncallstore "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	oncallmodel "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

func setupHandoffWorkerTestEnv(t *testing.T, advanceHours uint32, t0 time.Time) (*gorm.DB, *oncallstore.Store, uint64) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&iammodel.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := alertstore.Migrate(db); err != nil {
		t.Fatalf("migrate alerts: %v", err)
	}
	if err := oncallstore.Migrate(db); err != nil {
		t.Fatalf("migrate oncall: %v", err)
	}

	users := []iammodel.User{
		{ID: 1, Email: "alice@opspilot.local", DisplayName: "Alice", Phone: "13800000001", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
		{ID: 2, Email: "bob@opspilot.local", DisplayName: "Bob", Phone: "13800000002", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
	}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("seed user %d: %v", users[i].ID, err)
		}
	}

	s := oncallstore.NewStore(db)
	ctx := context.Background()
	sched := &oncallmodel.Schedule{
		Name:                 "SRE Primary Shift",
		Timezone:             "Asia/Shanghai",
		HandoffTime:          "09:00:00",
		ReminderAdvanceHours: advanceHours,
		Enabled:              true,
		CreatedBy:            1,
		CreatedAt:            t0,
		UpdatedAt:            t0,
	}
	if err := s.CreateSchedule(ctx, sched); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if err := s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID:         sched.ID,
		Name:               "Daily Tier 1",
		Tier:               oncallmodel.TierPrimary,
		RotationType:       oncallmodel.RotationTypeDaily,
		ShiftLengthSeconds: 86400,
		UsersJSON:          `[1, 2]`,
		EffectiveFrom:      t0,
		CreatedAt:          t0,
		UpdatedAt:          t0,
	}); err != nil {
		t.Fatalf("CreateRotation: %v", err)
	}
	return db, s, sched.ID
}

func TestHandoffReminderWorker_WindowAndIdempotency(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC) // 09:00 Asia/Shanghai
	_, s, schedID := setupHandoffWorkerTestEnv(t, 3, t0)
	ctx := context.Background()

	// 1. Outside 3h advance window (14h remaining before t0+24h shift change) -> 0 triggered.
	now := t0.Add(10 * time.Hour)
	deliverer := &spyDeliverer{}
	worker := NewHandoffReminderWorker(HandoffReminderWorkerOpts{
		Store:     s,
		Scheduler: NewScheduler(s),
		Deliverer: deliverer,
		Metrics:   NewMetrics(nil),
		Now:       func() time.Time { return now },
	})
	if count := worker.ProcessOnce(ctx); count != 0 {
		t.Fatalf("expected 0 reminders outside 3h window, got %d", count)
	}

	// 2. Inside 3h window (2h before t0+24h handoff) -> triggers once for Bob (incoming user #2).
	now = t0.Add(22 * time.Hour)
	if count := worker.ProcessOnce(ctx); count != 1 {
		t.Fatalf("expected 1 reminder inside 3h window, got %d", count)
	}
	if len(deliverer.calls) != 1 || !strings.Contains(deliverer.calls[0], "Bob") {
		t.Fatalf("expected delivery to Bob, got %v", deliverer.calls)
	}

	logs, err := s.ListHandoffLogs(ctx, schedID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected 1 handoff reminder log, got len=%d err=%v", len(logs), err)
	}
	if logs[0].ReminderType != "pre_shift_reminder" || logs[0].Status != "sent" || logs[0].PreviousUserID == nil || *logs[0].PreviousUserID != 1 {
		t.Fatalf("unexpected reminder log metadata: %+v", logs[0])
	}

	// 3. Re-running ProcessOnce on same or simulated second replica is strictly idempotent.
	replicaWorker := NewHandoffReminderWorker(HandoffReminderWorkerOpts{
		Store:     s,
		Scheduler: NewScheduler(s),
		Deliverer: deliverer,
		Now:       func() time.Time { return now.Add(5 * time.Minute) },
	})
	if count := replicaWorker.ProcessOnce(ctx); count != 0 {
		t.Fatalf("expected 0 duplicate reminders from second replica, got %d", count)
	}
	if len(deliverer.calls) != 1 {
		t.Fatalf("expected deliverer still called only once, got %v", deliverer.calls)
	}
}

func TestHandoffReminderWorker_ZeroAdvanceHoursDisablesAutoReminder(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)
	_, s, schedID := setupHandoffWorkerTestEnv(t, 0, t0)
	ctx := context.Background()

	sched, err := s.GetSchedule(ctx, schedID)
	if err != nil || sched.ReminderAdvanceHours != 0 {
		t.Fatalf("expected ReminderAdvanceHours=0 preserved on create, got %+v err=%v", sched, err)
	}

	now := t0.Add(23 * time.Hour)
	deliverer := &spyDeliverer{}
	worker := NewHandoffReminderWorker(HandoffReminderWorkerOpts{
		Store:     s,
		Scheduler: NewScheduler(s),
		Deliverer: deliverer,
		Now:       func() time.Time { return now },
	})
	if count := worker.ProcessOnce(ctx); count != 0 {
		t.Fatalf("expected 0 automatic reminders when reminder_advance_hours=0, got %d", count)
	}

	// Manual RemindNow still works even when automatic pre-shift reminder is disabled.
	logEntry, err := worker.RemindNow(ctx, schedID)
	if err != nil || logEntry == nil || logEntry.ReminderType != "manual_reminder" || logEntry.Status != "sent" {
		t.Fatalf("expected manual RemindNow to succeed, got entry=%+v err=%v", logEntry, err)
	}
}

func TestHandoffService_LatestHandoffIgnoresRemindersAndRepeatedAckUpserts(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)
	_, s, schedID := setupHandoffWorkerTestEnv(t, 3, t0)
	ctx := context.Background()

	// Seed a pre_shift_reminder record -> must NOT make GetLatestHandoff or GetCurrentHandoffStatus think shift is signed off.
	claimed, err := s.TryClaimHandoffReminder(ctx, &oncallmodel.HandoffLog{
		ScheduleID:   schedID,
		UserID:       2,
		ShiftStart:   t0.Add(24 * time.Hour),
		ShiftEnd:     t0.Add(48 * time.Hour),
		ReminderType: "pre_shift_reminder",
		Notes:        "auto reminder notes",
		AckStatus:    "pending",
		ChannelType:  "feishu",
		Status:       "sent",
		CreatedAt:    t0.Add(22 * time.Hour),
	})
	if err != nil || !claimed {
		t.Fatalf("TryClaimHandoffReminder: claimed=%v err=%v", claimed, err)
	}

	if _, err := s.GetLatestHandoff(ctx, schedID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected GetLatestHandoff to ignore reminder rows and return ErrRecordNotFound, got %v", err)
	}

	svc := NewHandoffService(s, NewScheduler(s))
	statusBefore, err := svc.GetCurrentHandoffStatus(ctx, schedID)
	if err != nil {
		t.Fatalf("GetCurrentHandoffStatus: %v", err)
	}
	if statusBefore.Status != "pending" {
		t.Fatalf("expected handoff status 'pending' when only reminder exists, got %q", statusBefore.Status)
	}

	// First sign-off + repeat sign-off upsert cleanly without unique index collision.
	if _, err := svc.AckHandoff(ctx, schedID, 1, "first shift notes"); err != nil {
		t.Fatalf("first AckHandoff: %v", err)
	}
	updated, err := svc.AckHandoff(ctx, schedID, 1, "revised handoff notes")
	if err != nil {
		t.Fatalf("second AckHandoff upsert failed: %v", err)
	}
	if updated.Status != "signed_off" || updated.Notes != "revised handoff notes" {
		t.Fatalf("expected signed_off with revised notes, got %+v", updated)
	}

	latest, err := s.GetLatestHandoff(ctx, schedID)
	if err != nil || latest.ReminderType != "shift_handoff" || latest.Notes != "revised handoff notes" {
		t.Fatalf("expected GetLatestHandoff to return revised shift_handoff row, got %+v err=%v", latest, err)
	}
}
