package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func setupGroupingTestEnv(t *testing.T, t0 time.Time) (*gorm.DB, *oncallstore.Store, uint64) {
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
		Name:        "Payments Core",
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
	if err := s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID:         sched.ID,
		Name:               "Primary",
		Tier:               oncallmodel.TierPrimary,
		RotationType:       oncallmodel.RotationTypeDaily,
		ShiftLengthSeconds: 86400,
		UsersJSON:          `[1]`,
		EffectiveFrom:      t0.Add(-48 * time.Hour),
		CreatedAt:          t0,
		UpdatedAt:          t0,
	}); err != nil {
		t.Fatalf("CreateRotation: %v", err)
	}
	if err := s.CreateEscalationPolicy(ctx, &oncallmodel.EscalationPolicy{
		ScheduleID:  sched.ID,
		Name:        "Step 1 Primary",
		StepNumber:  1,
		WaitSeconds: 0,
		TargetType:  "primary_oncall",
		CreatedAt:   t0,
	}); err != nil {
		t.Fatalf("CreateEscalationPolicy: %v", err)
	}
	if err := s.SetUserNotificationRules(ctx, 1, "high", []*oncallmodel.UserNotificationRule{
		{UserID: 1, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
	}); err != nil {
		t.Fatalf("SetUserNotificationRules: %v", err)
	}
	return db, s, sched.ID
}

func seedOpenIncident(t *testing.T, db *gorm.DB, id uint64, rule, title, severity string, t0 time.Time) {
	t.Helper()
	inc := &alertmodel.Incident{
		ID:           id,
		Scope:        fmt.Sprintf("pod-%d", id),
		ScopeType:    "k8s",
		Rule:         rule,
		RuleName:     rule,
		Severity:     severity,
		Title:        title,
		Status:       alertmodel.StatusOpen,
		DedupeKey:    fmt.Sprintf("dedupe-%s-%d", rule, id),
		FirstFiredAt: t0,
		LastFiredAt:  t0,
	}
	if err := db.Create(inc).Error; err != nil {
		t.Fatalf("seed incident %d: %v", id, err)
	}
}

func TestAlertGrouping_BurstMergeWithinGroupWait(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC)
	db, s, schedID := setupGroupingTestEnv(t, t0)
	ctx := context.Background()

	matchersBytes, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "payments"}})
	groupByBytes, _ := json.Marshal([]string{"service", "cluster"})
	if err := s.CreateRoute(ctx, &oncallmodel.Route{
		Name:                 "Payments Grouping Route",
		MatcherJSON:          string(matchersBytes),
		GroupByJSON:          string(groupByBytes),
		Priority:             10,
		ScheduleID:           schedID,
		GroupWaitSeconds:     30,
		GroupIntervalSeconds: 300,
		Enabled:              true,
		CreatedAt:            t0,
		UpdatedAt:            t0,
	}); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	for _, id := range []uint64{301, 302, 303} {
		seedOpenIncident(t, db, id, "http_5xx_spike", fmt.Sprintf("Payments 5xx burst #%d", id), "critical", t0)
	}

	now := t0
	deliverer := &spyDeliverer{}
	metrics := NewMetrics(nil)
	worker := NewEscalationWorker(EscalationWorkerOpts{
		Store:        s,
		Scheduler:    NewScheduler(s),
		VoiceGateway: voice.NewVoiceGateway(),
		Deliverer:    deliverer,
		Metrics:      metrics,
		Now:          func() time.Time { return now },
	})
	dispatcher := NewDispatcher(s, NewRouteMatcher(s), nil, metrics, nil)
	dispatcher.SetClock(func() time.Time { return now })

	labels := map[string]string{"service": "payments", "cluster": "prod-sh"}
	dispatcher.DispatchFiredSync(ctx, 301, "http_5xx_spike", "Payments 5xx burst #301", "critical", 1, labels, t0)

	now = t0.Add(5 * time.Second)
	dispatcher.DispatchFiredSync(ctx, 302, "http_5xx_spike", "Payments 5xx burst #302", "critical", 2, labels, now)

	now = t0.Add(10 * time.Second)
	dispatcher.DispatchFiredSync(ctx, 303, "http_5xx_spike", "Payments 5xx burst #303", "critical", 3, labels, now)

	// Still inside 30s group_wait window -> worker must not dispatch prematurely.
	now = t0.Add(20 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 0 {
		t.Fatalf("expected 0 processed tasks inside group_wait window, got %d", processed)
	}

	tasks, err := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusPending, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected 1 merged pending task, got len=%d err=%v", len(tasks), err)
	}
	if tasks[0].AlertCount != 3 {
		t.Fatalf("expected alert_count=3, got %d", tasks[0].AlertCount)
	}
	if gotMerged := testutil.ToFloat64(metrics.AlertsMergedTotal); gotMerged != 2 {
		t.Fatalf("expected opspilot_oncall_alerts_merged_total=2, got %v", gotMerged)
	}

	// Cross 30s group_wait boundary -> single grouped notification summarizes 3 alerts.
	now = t0.Add(31 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected 1 processed grouped task after group_wait, got %d", processed)
	}
	if len(deliverer.calls) != 1 || !strings.Contains(deliverer.calls[0], "[合并 3 条告警]") {
		t.Fatalf("expected single grouped notification with '[合并 3 条告警]', got %v", deliverer.calls)
	}

	logs, total, err := s.ListNotificationLogs(ctx, "", "", 0, 10)
	if err != nil || total != 1 || logs[0].AlertCount != 3 {
		t.Fatalf("expected notification log with alert_count=3, got total=%d logs=%+v err=%v", total, logs, err)
	}
}

func TestAlertGrouping_ZeroGroupWaitPreservedAndImmediate(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	db, s, schedID := setupGroupingTestEnv(t, t0)
	ctx := context.Background()

	matchersBytes, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "auth"}})
	route := &oncallmodel.Route{
		Name:                 "Zero Wait Route",
		MatcherJSON:          string(matchersBytes),
		Priority:             10,
		ScheduleID:           schedID,
		GroupWaitSeconds:     0,
		GroupIntervalSeconds: 0,
		Enabled:              true,
		CreatedAt:            t0,
		UpdatedAt:            t0,
	}
	if err := s.CreateRoute(ctx, route); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	reloaded, err := s.GetRoute(ctx, route.ID)
	if err != nil || reloaded.GroupWaitSeconds != 0 || reloaded.GroupIntervalSeconds != 0 {
		t.Fatalf("expected explicit 0 wait & interval preserved in DB, got %+v err=%v", reloaded, err)
	}

	seedOpenIncident(t, db, 401, "login_fail", "Auth latency #1", "critical", t0)
	seedOpenIncident(t, db, 402, "login_fail", "Auth latency #2", "critical", t0)

	now := t0
	deliverer := &spyDeliverer{}
	metrics := NewMetrics(nil)
	worker := NewEscalationWorker(EscalationWorkerOpts{
		Store:        s,
		Scheduler:    NewScheduler(s),
		VoiceGateway: voice.NewVoiceGateway(),
		Deliverer:    deliverer,
		Metrics:      metrics,
		Now:          func() time.Time { return now },
	})
	dispatcher := NewDispatcher(s, NewRouteMatcher(s), nil, metrics, nil)
	dispatcher.SetClock(func() time.Time { return now })

	labels := map[string]string{"service": "auth"}
	dispatcher.DispatchFiredSync(ctx, 401, "login_fail", "Auth latency #1", "critical", 1, labels, t0)
	dispatcher.DispatchFiredSync(ctx, 402, "login_fail", "Auth latency #2", "critical", 2, labels, t0)

	if processed := worker.ProcessBatch(ctx); processed != 2 {
		t.Fatalf("expected 2 immediate unmerged tasks when group_wait=0, got %d", processed)
	}
	if len(deliverer.calls) != 2 {
		t.Fatalf("expected 2 separate notifications, got %v", deliverer.calls)
	}
}

func TestAlertGrouping_GroupIntervalThrottleAndPartialAckShrink(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 4, 0, 0, 0, time.UTC)
	db, s, schedID := setupGroupingTestEnv(t, t0)
	ctx := context.Background()

	matchersBytes, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "billing"}})
	route := &oncallmodel.Route{
		Name:                 "Billing Route",
		MatcherJSON:          string(matchersBytes),
		Priority:             10,
		ScheduleID:           schedID,
		GroupWaitSeconds:     10,
		GroupIntervalSeconds: 300,
		Enabled:              true,
		CreatedAt:            t0,
		UpdatedAt:            t0,
	}
	if err := s.CreateRoute(ctx, route); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	for _, id := range []uint64{501, 502, 503} {
		seedOpenIncident(t, db, id, "db_pool_exhausted", fmt.Sprintf("Billing DB #%d", id), "critical", t0)
	}

	now := t0
	deliverer := &spyDeliverer{}
	metrics := NewMetrics(nil)
	worker := NewEscalationWorker(EscalationWorkerOpts{
		Store:        s,
		Scheduler:    NewScheduler(s),
		VoiceGateway: voice.NewVoiceGateway(),
		Deliverer:    deliverer,
		Metrics:      metrics,
		Now:          func() time.Time { return now },
	})
	dispatcher := NewDispatcher(s, NewRouteMatcher(s), nil, metrics, nil)
	dispatcher.SetClock(func() time.Time { return now })

	labels := map[string]string{"service": "billing"}
	dispatcher.DispatchFiredSync(ctx, 501, "db_pool_exhausted", "Billing DB #501", "critical", 1, labels, t0)
	dispatcher.DispatchFiredSync(ctx, 502, "db_pool_exhausted", "Billing DB #502", "critical", 2, labels, t0)

	// Ack 501 -> grouped task shrinks to [502], remains pending because 502 is still open.
	if err := s.AckAlertIncident(ctx, 501, 1, "handling 501"); err != nil {
		t.Fatalf("AckAlertIncident 501: %v", err)
	}
	pending, _ := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusPending, 10)
	if len(pending) != 1 || pending[0].AlertCount != 1 || pending[0].IncidentID != 502 {
		t.Fatalf("expected shrunken pending task for 502, got %+v", pending)
	}

	// Dispatch past group_wait -> notifies 502, completes single-step policy with LastDispatchedAt set.
	now = t0.Add(15 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected 1 processed task at t0+15s, got %d", processed)
	}
	completed, _ := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusCompleted, 10)
	if len(completed) != 1 || completed[0].LastDispatchedAt == nil {
		t.Fatalf("expected completed task with LastDispatchedAt recorded, got %+v", completed)
	}

	// Fire 503 60s later -> throttled by group_interval (300s from t0+15s = t0+315s).
	now = t0.Add(60 * time.Second)
	dispatcher.DispatchFiredSync(ctx, 503, "db_pool_exhausted", "Billing DB #503", "critical", 3, labels, now)
	pendingAfter, _ := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusPending, 10)
	if len(pendingAfter) != 1 {
		t.Fatalf("expected 1 throttled pending task, got %d", len(pendingAfter))
	}
	expectedMinNext := t0.Add(315 * time.Second)
	if pendingAfter[0].NextRunAt.Before(expectedMinNext) {
		t.Fatalf("expected NextRunAt >= %v due to group_interval suppression, got %v", expectedMinNext, pendingAfter[0].NextRunAt)
	}
	if processed := worker.ProcessBatch(ctx); processed != 0 {
		t.Fatalf("expected 0 processed tasks while suppressed by group_interval, got %d", processed)
	}

	// Closing last open incident cancels task cleanly.
	if err := s.AckAlertIncident(ctx, 503, 1, "resolved 503"); err != nil {
		t.Fatalf("AckAlertIncident 503: %v", err)
	}
	remainingPending, _ := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusPending, 10)
	if len(remainingPending) != 0 {
		t.Fatalf("expected 0 pending tasks after closing final incident, got %+v", remainingPending)
	}
}
