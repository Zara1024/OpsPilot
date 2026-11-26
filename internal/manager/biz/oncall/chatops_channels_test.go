package oncall

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	alertstore "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	oncallstore "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	oncallmodel "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type captureLabelsDeliverer struct {
	calls  int
	labels map[string]string
}

func (c *captureLabelsDeliverer) Deliver(_ context.Context, _ string, _ *oncallstore.UserSummary, _, _, _ string, labels map[string]string) (string, error) {
	c.calls++
	c.labels = make(map[string]string, len(labels))
	for k, v := range labels {
		c.labels[k] = v
	}
	return "feishu", nil
}

func TestQuickActionSignAndVerify(t *testing.T) {
	now := time.Now()
	link := SignQuickActionURL("https://opspilot.test/", "secret-token-123", 42, "ack", time.Hour)
	if !strings.HasPrefix(link, "https://opspilot.test/api/v1/oncall/chatops/quick-action?") {
		t.Fatalf("unexpected signed url: %q", link)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	if err := VerifyQuickAction("secret-token-123", q.Get("inc"), q.Get("action"), q.Get("exp"), q.Get("sig"), now); err != nil {
		t.Fatalf("VerifyQuickAction failed on valid signature: %v", err)
	}
	if err := VerifyQuickAction("secret-token-123", "99", q.Get("action"), q.Get("exp"), q.Get("sig"), now); err == nil {
		t.Fatal("expected tampered incident id to fail signature check")
	}
	if err := VerifyQuickAction("secret-token-123", q.Get("inc"), q.Get("action"), q.Get("exp"), q.Get("sig"), now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected expired link to fail signature check")
	}
}

func TestEscalationWorker_PinnedReassignAndQuickActionLabels(t *testing.T) {
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
		{ID: 1, Email: "alice@opspilot.local", DisplayName: "Alice", Phone: "13800000001", Role: iammodel.RoleAdmin, Status: iammodel.StatusActive},
		{ID: 2, Email: "bob@opspilot.local", DisplayName: "Bob", Phone: "13800000002", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
	} {
		uCopy := u
		_ = db.Create(&uCopy).Error
	}

	s := oncallstore.NewStore(db)
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-5 * time.Second).Truncate(time.Second)

	sched := &oncallmodel.Schedule{Name: "Core", Timezone: "Asia/Shanghai", HandoffTime: "09:00:00", Enabled: true, CreatedBy: 1, CreatedAt: t0, UpdatedAt: t0}
	_ = s.CreateSchedule(ctx, sched)
	_ = s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID: sched.ID, Name: "Primary", Tier: oncallmodel.TierPrimary,
		RotationType: oncallmodel.RotationTypeDaily, ShiftLengthSeconds: 86400, UsersJSON: `[1]`,
		EffectiveFrom: t0.Add(-24 * time.Hour), CreatedAt: t0, UpdatedAt: t0,
	})
	_ = s.CreateEscalationPolicy(ctx, &oncallmodel.EscalationPolicy{ScheduleID: sched.ID, Name: "Step1", StepNumber: 1, WaitSeconds: 0, TargetType: "primary_oncall", CreatedAt: t0})
	_ = s.CreateEscalationPolicy(ctx, &oncallmodel.EscalationPolicy{ScheduleID: sched.ID, Name: "Step2", StepNumber: 2, WaitSeconds: 60, TargetType: "fallback_lead", CreatedAt: t0})

	// Bob's ladder exercises email & im_dm.
	_ = s.SetUserNotificationRules(ctx, 2, "high", []*oncallmodel.UserNotificationRule{
		{UserID: 2, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
		{UserID: 2, Urgency: "high", StepNumber: 2, DelayMinutes: 1, Channel: "email", Enabled: true},
	})

	_ = db.Create(&alertmodel.Incident{
		ID: 77, Title: "Kafka consumer lag", Severity: "critical", Status: alertmodel.StatusOpen,
		DedupeKey: "kafka-lag-77", FirstFiredAt: t0, LastFiredAt: t0,
	}).Error
	_ = db.Create(&oncallmodel.EscalationTask{
		IncidentID:    77,
		Title:         "Kafka consumer lag",
		Severity:      "critical",
		Urgency:       "high",
		ScheduleID:    sched.ID,
		CurrentStep:   1,
		LadderStep:    1,
		StepStartedAt: t0,
		Status:        oncallmodel.TaskStatusPending,
		NextRunAt:     t0.Add(5 * time.Minute),
		CreatedAt:     t0,
		UpdatedAt:     t0,
	}).Error

	del := &captureLabelsDeliverer{}
	now := t0
	worker := NewEscalationWorker(EscalationWorkerOpts{
		Store:          s,
		Scheduler:      NewScheduler(s),
		VoiceGateway:   voice.NewVoiceGateway(),
		ChannelGateway: channels.NewChannelGateway(),
		Deliverer:      del,
		PublicURL:      "https://opspilot.test",
		Now:            func() time.Time { return now },
	})
	engine := NewEscalationEngine(s, NewScheduler(s))
	engine.SetWorker(worker)

	// Reassign incident 77 to Bob (#2) -> pins assignee & schedules immediate run.
	res, err := engine.HandleAction(ctx, ChatOpsActionRequest{
		Action:     "reassign",
		IncidentID: "INC-0077",
		OperatorID: 1,
		TargetUser: 2,
	})
	if err != nil || !res.OK || !strings.Contains(res.Message, "Bob") {
		t.Fatalf("HandleAction reassign failed: res=%+v err=%v", res, err)
	}

	now = time.Now().UTC().Add(time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected 1 processed task after reassign, got %d", processed)
	}
	if del.calls != 1 || del.labels["oncall_ack_url"] == "" || del.labels["oncall_silence_url"] == "" || del.labels["oncall_escalate_url"] == "" {
		t.Fatalf("expected signed quick action urls in IM labels, got %+v", del.labels)
	}

	// Verify step 2 email permanently fails cleanly without SMTP configured (no endless retry) and advances step clearing PinnedAssigneeID.
	now = now.Add(90 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected email step processed at t0+90s, got %d", processed)
	}
	logs, total, err := s.ListNotificationLogs(ctx, "email", "", 0, 10)
	if err != nil || total != 1 || logs[0].UserID != 2 || logs[0].Status != oncallmodel.NotifyStatusFailed {
		t.Fatalf("expected honest failed email log for Bob when SMTP unconfigured, got total=%d logs=%+v err=%v", total, logs, err)
	}
	tasks, _ := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusPending, 10)
	if len(tasks) != 1 || tasks[0].CurrentStep != 2 || tasks[0].PinnedAssigneeID != 0 {
		t.Fatalf("expected CurrentStep=2 and PinnedAssigneeID cleared on team escalation, got %+v", tasks)
	}
}
