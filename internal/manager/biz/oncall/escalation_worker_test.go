package oncall

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	alertstore "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	oncallstore "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	oncallmodel "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type spyDeliverer struct {
	calls []string
}

func (s *spyDeliverer) Deliver(_ context.Context, channel string, targetUser *oncallstore.UserSummary, subject, _ string, _ string, _ map[string]string) (string, error) {
	who := "broadcast"
	if targetUser != nil {
		who = targetUser.DisplayName
	}
	s.calls = append(s.calls, channel+":"+who+":"+subject)
	return "feishu", nil
}

func TestEscalationWorker_EndToEndLadderEscalationThrottleAndAck(t *testing.T) {
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

	for _, u := range []iammodel.User{
		{ID: 1, Email: "alice@opspilot.local", DisplayName: "Alice", Phone: "13800000001", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
		{ID: 2, Email: "bob@opspilot.local", DisplayName: "Bob", Phone: "13800000002", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
	} {
		uCopy := u
		if err := db.Create(&uCopy).Error; err != nil {
			t.Fatalf("create user %d: %v", uCopy.ID, err)
		}
	}

	s := oncallstore.NewStore(db)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)

	sched := &oncallmodel.Schedule{
		Name:        "Core SRE",
		Timezone:    "Asia/Shanghai",
		HandoffTime: "09:00:00",
		Enabled:     true,
		CreatedBy:   1,
		CreatedAt:   t0.Add(-48 * time.Hour),
		UpdatedAt:   t0.Add(-48 * time.Hour),
	}
	if err := s.CreateSchedule(ctx, sched); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	for _, rot := range []*oncallmodel.Rotation{
		{
			ScheduleID:         sched.ID,
			Name:               "Primary",
			Tier:               oncallmodel.TierPrimary,
			RotationType:       oncallmodel.RotationTypeDaily,
			ShiftLengthSeconds: 86400,
			UsersJSON:          `[1]`,
			EffectiveFrom:      t0.Add(-48 * time.Hour),
			CreatedAt:          t0,
			UpdatedAt:          t0,
		},
		{
			ScheduleID:         sched.ID,
			Name:               "Secondary",
			Tier:               oncallmodel.TierSecondary,
			RotationType:       oncallmodel.RotationTypeDaily,
			ShiftLengthSeconds: 86400,
			UsersJSON:          `[2]`,
			EffectiveFrom:      t0.Add(-48 * time.Hour),
			CreatedAt:          t0,
			UpdatedAt:          t0,
		},
	} {
		if err := s.CreateRotation(ctx, rot); err != nil {
			t.Fatalf("CreateRotation tier %d: %v", rot.Tier, err)
		}
	}

	matchersBytes, _ := json.Marshal([]LabelMatcher{{Name: "severity", Operator: OpEqual, Value: "critical"}})
	if err := s.CreateRoute(ctx, &oncallmodel.Route{
		Name:        "Critical Route",
		MatcherJSON: string(matchersBytes),
		Priority:    1,
		ScheduleID:  sched.ID,
		Enabled:     true,
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	for _, pol := range []*oncallmodel.EscalationPolicy{
		{ScheduleID: sched.ID, Name: "Step 1 Primary", StepNumber: 1, WaitSeconds: 0, TargetType: "primary_oncall", CreatedAt: t0},
		{ScheduleID: sched.ID, Name: "Step 2 Secondary", StepNumber: 2, WaitSeconds: 180, TargetType: "secondary_oncall", CreatedAt: t0},
	} {
		if err := s.CreateEscalationPolicy(ctx, pol); err != nil {
			t.Fatalf("CreateEscalationPolicy: %v", err)
		}
	}

	if err := s.SetUserNotificationRules(ctx, 1, "high", []*oncallmodel.UserNotificationRule{
		{UserID: 1, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
		{UserID: 1, Urgency: "high", StepNumber: 2, DelayMinutes: 1, Channel: "voice_call", Enabled: true},
	}); err != nil {
		t.Fatalf("SetUserNotificationRules user 1: %v", err)
	}

	for _, incID := range []uint64{201, 202} {
		inc := &alertmodel.Incident{
			ID:           incID,
			Scope:        "host",
			ScopeType:    "host",
			Rule:         "disk_full",
			RuleName:     "Disk Full",
			Severity:     "critical",
			Title:        "Root disk > 95%",
			Status:       alertmodel.StatusOpen,
			DedupeKey:    string(rune('a' + int(incID%20))),
			FirstFiredAt: t0,
			LastFiredAt:  t0,
		}
		if err := db.Create(inc).Error; err != nil {
			t.Fatalf("seed incident %d: %v", incID, err)
		}
	}

	now := t0
	deliverer := &spyDeliverer{}
	metrics := NewMetrics(nil)
	worker := NewEscalationWorker(EscalationWorkerOpts{
		Store:         s,
		Scheduler:     NewScheduler(s),
		VoiceGateway:  voice.NewVoiceGateway(),
		Deliverer:     deliverer,
		Metrics:       metrics,
		VoiceCooldown: 3 * time.Minute,
		Now:           func() time.Time { return now },
	})
	dispatcher := NewDispatcher(s, NewRouteMatcher(s), nil, metrics, nil)
	dispatcher.SetClock(func() time.Time { return now })

	// 1. Fire incident 201 and run Step 1 (Alice IM DM).
	dispatcher.DispatchFiredSync(ctx, 201, "disk_full", "Root disk > 95%", "critical", 1, nil, t0)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected 1 processed task at step 1, got %d", processed)
	}
	if len(deliverer.calls) != 1 {
		t.Fatalf("expected 1 IM delivery to Alice, got %v", deliverer.calls)
	}

	// 2. Advance 70s -> Alice voice call ladder triggers via mock provider, open alert stays open.
	now = now.Add(70 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected 1 processed task for Alice voice call, got %d", processed)
	}
	logs, total, err := s.ListNotificationLogs(ctx, "voice_call", "", 0, 20)
	if err != nil || total != 1 || logs[0].UserID != 1 {
		t.Fatalf("expected 1 voice call log for Alice, got total=%d logs=%+v err=%v", total, logs, err)
	}
	var incAfterVoice alertmodel.Incident
	if err := db.First(&incAfterVoice, 201).Error; err != nil || incAfterVoice.Status != alertmodel.StatusOpen {
		t.Fatalf("expected automated mock call NOT to auto-ack incident, status=%q", incAfterVoice.Status)
	}

	// 3. Configure Alice immediate voice_call and fire incident 202 within 3m cooldown -> throttled!
	if err := s.SetUserNotificationRules(ctx, 1, "high", []*oncallmodel.UserNotificationRule{
		{UserID: 1, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "voice_call", Enabled: true},
	}); err != nil {
		t.Fatalf("SetUserNotificationRules immediate voice: %v", err)
	}
	dispatcher.DispatchFiredSync(ctx, 202, "disk_full", "Second disk alert", "critical", 1, nil, now)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected 1 processed task for incident 202, got %d", processed)
	}
	throttledLogs, throttledTotal, err := s.ListNotificationLogs(ctx, "voice_call", oncallmodel.NotifyStatusThrottled, 0, 10)
	if err != nil || throttledTotal != 1 || throttledLogs[0].IncidentID != 202 {
		t.Fatalf("expected throttled voice call log for incident 202, got total=%d err=%v", throttledTotal, err)
	}

	// 4. Advance clock past team Step 2 wait (180s from start) -> incident 201 escalates to Secondary Bob.
	now = t0.Add(200 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed < 1 {
		t.Fatalf("expected incident 201 to escalate to Secondary Bob, got %d", processed)
	}
	if len(deliverer.calls) < 2 || deliverer.calls[len(deliverer.calls)-1] == "" {
		t.Fatalf("expected Bob secondary notification, calls=%v", deliverer.calls)
	}
	bobLogs, _, _ := s.ListNotificationLogs(ctx, "", "", 0, 20)
	foundBob := false
	for _, l := range bobLogs {
		if l.IncidentID == 201 && l.UserID == 2 {
			foundBob = true
		}
	}
	if !foundBob {
		t.Fatalf("expected notification log targeting secondary user Bob (ID=2), logs=%+v", bobLogs)
	}

	// 5. Ack incident 201 -> escalation task must be cancelled as acked immediately.
	dispatcher.DispatchClosedSync(ctx, 201, "acknowledged")
	tasks, err := s.ListEscalationTasks(ctx, "", 20)
	if err != nil {
		t.Fatalf("ListEscalationTasks: %v", err)
	}
	for _, task := range tasks {
		if task.IncidentID == 201 && task.Status != oncallmodel.TaskStatusAcked {
			t.Fatalf("expected incident 201 task acked after OnAlertClosed, got %q", task.Status)
		}
	}
}
