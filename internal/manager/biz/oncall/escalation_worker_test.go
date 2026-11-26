package oncall

import (
	"context"
	"encoding/json"
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

type stubCallingVoiceProvider struct {
	name   string
	titles []string
	callIDs []string
}

func (p *stubCallingVoiceProvider) Name() string {
	return p.name
}

func (p *stubCallingVoiceProvider) SendVoiceCall(_ context.Context, _ *voice.VoiceGatewayConfig, req *voice.CallRequest) (*voice.CallResult, error) {
	p.titles = append(p.titles, req.Title)
	p.callIDs = append(p.callIDs, req.CallID)
	return &voice.CallResult{
		CallID:      req.CallID,
		OutCallID:   "out_" + req.CallID,
		Provider:    "aliyun",
		Status:      "calling",
		PhoneNumber: req.PhoneNumber,
		Message:     "outbound call ringing",
	}, nil
}

func TestEscalationWorker_RepeatChainAndVoiceRedialCooldownBypass(t *testing.T) {
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
	t0 := time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC)

	// Part A: Escalation Chain Repeat (2 extra rounds after round 1).
	schedRepeat := &oncallmodel.Schedule{
		Name:                            "Repeat Chain Schedule",
		Timezone:                        "Asia/Shanghai",
		HandoffTime:                     "09:00:00",
		Enabled:                         true,
		EscalationRepeatCount:           2,
		EscalationRepeatIntervalSeconds: 60,
		CreatedBy:                       1,
		CreatedAt:                       t0.Add(-24 * time.Hour),
		UpdatedAt:                       t0.Add(-24 * time.Hour),
	}
	if err := s.CreateSchedule(ctx, schedRepeat); err != nil {
		t.Fatalf("CreateSchedule repeat: %v", err)
	}
	if err := s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID:         schedRepeat.ID,
		Name:               "Primary",
		Tier:               oncallmodel.TierPrimary,
		RotationType:       oncallmodel.RotationTypeDaily,
		ShiftLengthSeconds: 86400,
		UsersJSON:          `[1]`,
		EffectiveFrom:      t0.Add(-24 * time.Hour),
		CreatedAt:          t0,
		UpdatedAt:          t0,
	}); err != nil {
		t.Fatalf("CreateRotation: %v", err)
	}
	if err := s.CreateEscalationPolicy(ctx, &oncallmodel.EscalationPolicy{
		ScheduleID:  schedRepeat.ID,
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
		t.Fatalf("SetUserNotificationRules Alice: %v", err)
	}

	repeatMatch, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "api"}})
	if err := s.CreateRoute(ctx, &oncallmodel.Route{
		Name:        "API Route",
		MatcherJSON: string(repeatMatch),
		Priority:    1,
		ScheduleID:  schedRepeat.ID,
		Enabled:     true,
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}); err != nil {
		t.Fatalf("CreateRoute api: %v", err)
	}

	if err := db.Create(&alertmodel.Incident{
		ID: 301, Scope: "svc", ScopeType: "service", Rule: "api_5xx", RuleName: "API 5xx",
		Severity: "critical", Title: "API gateway 502 spike", Status: alertmodel.StatusOpen,
		DedupeKey: "repeat-301", FirstFiredAt: t0, LastFiredAt: t0,
	}).Error; err != nil {
		t.Fatalf("create incident 301: %v", err)
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

	dispatcher.DispatchFiredSync(ctx, 301, "api_5xx", "API gateway 502 spike", "critical", 1, map[string]string{"service": "api"}, t0)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("round 1 processed=%d, want 1", processed)
	}
	if len(deliverer.calls) != 1 || strings.Contains(deliverer.calls[0], "轮提醒") {
		t.Fatalf("expected round 1 without repeat prefix, got %v", deliverer.calls)
	}

	task301, err := s.FindPendingTaskByIncident(ctx, 301)
	if err != nil || task301 == nil {
		t.Fatalf("FindPendingTaskByIncident 301: %v", err)
	}
	if task301.RepeatCount != 1 || task301.CurrentStep != 1 || task301.Status != oncallmodel.TaskStatusPending {
		t.Fatalf("expected task scheduled for repeat round 2, got %+v", task301)
	}
	if diff := task301.NextRunAt.Sub(t0); diff != 60*time.Second {
		t.Fatalf("expected repeat cooldown 60s, got %v", diff)
	}

	repeatLogs, _, err := s.ListNotificationLogs(ctx, "escalation", "", 0, 10)
	if err != nil || len(repeatLogs) != 1 || repeatLogs[0].Provider != "repeat" {
		t.Fatalf("expected 1 escalation repeat audit log, got %+v err=%v", repeatLogs, err)
	}

	// Round 2 at t0 + 61s -> [第 2 轮提醒] prefix, schedules Round 3.
	now = now.Add(61 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("round 2 processed=%d, want 1", processed)
	}
	if len(deliverer.calls) != 2 || !strings.Contains(deliverer.calls[1], "[第 2 轮提醒]") {
		t.Fatalf("expected [第 2 轮提醒] prefix in round 2, got %v", deliverer.calls)
	}

	// Round 3 at t0 + 122s -> [第 3 轮提醒] prefix, repeat budget exhausted -> completed.
	now = now.Add(61 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("round 3 processed=%d, want 1", processed)
	}
	if len(deliverer.calls) != 3 || !strings.Contains(deliverer.calls[2], "[第 3 轮提醒]") {
		t.Fatalf("expected [第 3 轮提醒] prefix in round 3, got %v", deliverer.calls)
	}
	task301Done, _ := s.GetEscalationTaskByID(ctx, task301.ID)
	if task301Done.Status != oncallmodel.TaskStatusCompleted {
		t.Fatalf("expected task 301 completed after 2 repeat rounds, got %q", task301Done.Status)
	}

	// Part B: 90s final-step voice grace window + automatic redial bypassing 3m voice cooldown.
	if err := s.UpdateVoiceConfig(ctx, &voice.VoiceGatewayConfig{
		Provider:                  "mock",
		DispatchEnabled:           true,
		VoiceRedialMaxAttempts:    1,
		VoiceRedialDelaySeconds:   20,
		VoiceFastEscalateOnReject: true,
	}); err != nil {
		t.Fatalf("UpdateVoiceConfig: %v", err)
	}
	stubVG := voice.NewVoiceGateway()
	stubVoice := &stubCallingVoiceProvider{name: "mock"}
	stubVG.RegisterProvider(stubVoice)

	schedVoice := &oncallmodel.Schedule{
		Name:                  "Voice Redial Schedule",
		Timezone:              "Asia/Shanghai",
		HandoffTime:           "09:00:00",
		Enabled:               true,
		EscalationRepeatCount: 0,
		CreatedBy:             1,
		CreatedAt:             t0,
		UpdatedAt:             t0,
	}
	if err := s.CreateSchedule(ctx, schedVoice); err != nil {
		t.Fatalf("CreateSchedule voice: %v", err)
	}
	if err := s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID:         schedVoice.ID,
		Name:               "Primary Voice",
		Tier:               oncallmodel.TierPrimary,
		RotationType:       oncallmodel.RotationTypeDaily,
		ShiftLengthSeconds: 86400,
		UsersJSON:          `[2]`,
		EffectiveFrom:      t0.Add(-24 * time.Hour),
		CreatedAt:          t0,
		UpdatedAt:          t0,
	}); err != nil {
		t.Fatalf("CreateRotation voice: %v", err)
	}
	if err := s.CreateEscalationPolicy(ctx, &oncallmodel.EscalationPolicy{
		ScheduleID:  schedVoice.ID,
		Name:        "Only Step Voice",
		StepNumber:  1,
		WaitSeconds: 0,
		TargetType:  "primary_oncall",
		CreatedAt:   t0,
	}); err != nil {
		t.Fatalf("CreateEscalationPolicy voice: %v", err)
	}
	if err := s.SetUserNotificationRules(ctx, 2, "high", []*oncallmodel.UserNotificationRule{
		{UserID: 2, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "voice_call", Enabled: true},
	}); err != nil {
		t.Fatalf("SetUserNotificationRules Bob: %v", err)
	}
	payMatch, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "pay"}})
	if err := s.CreateRoute(ctx, &oncallmodel.Route{
		Name:        "Pay Route",
		MatcherJSON: string(payMatch),
		Priority:    1,
		ScheduleID:  schedVoice.ID,
		Enabled:     true,
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}); err != nil {
		t.Fatalf("CreateRoute pay: %v", err)
	}
	if err := db.Create(&alertmodel.Incident{
		ID: 302, Scope: "svc", ScopeType: "service", Rule: "pay_down", RuleName: "Payment Down",
		Severity: "critical", Title: "Checkout returning 500", Status: alertmodel.StatusOpen,
		DedupeKey: "voice-302", FirstFiredAt: t0, LastFiredAt: t0,
	}).Error; err != nil {
		t.Fatalf("create incident 302: %v", err)
	}

	now = t0.Add(10 * time.Minute)
	workerVoice := NewEscalationWorker(EscalationWorkerOpts{
		Store:         s,
		Scheduler:     NewScheduler(s),
		VoiceGateway:  stubVG,
		Deliverer:     deliverer,
		Metrics:       metrics,
		VoiceCooldown: 3 * time.Minute,
		Now:           func() time.Time { return now },
	})
	dispatcher.DispatchFiredSync(ctx, 302, "pay_down", "Checkout returning 500", "critical", 1, map[string]string{"service": "pay"}, now)
	if processed := workerVoice.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected initial voice call task processed, got %d", processed)
	}
	if len(stubVoice.titles) != 1 || strings.Contains(stubVoice.titles[0], "重拨") {
		t.Fatalf("expected 1 initial voice call without redial prefix, got %v", stubVoice.titles)
	}

	task302, err := s.FindPendingTaskByIncident(ctx, 302)
	if err != nil || task302 == nil {
		t.Fatalf("FindPendingTaskByIncident 302: %v", err)
	}
	if !task302.AwaitingVoiceGrace || task302.Status != oncallmodel.TaskStatusPending || task302.LastVoiceCallID == "" {
		t.Fatalf("expected final-step waiting voice call to enter 90s grace window, got %+v", task302)
	}
	if graceDiff := task302.NextRunAt.Sub(now); graceDiff != 90*time.Second {
		t.Fatalf("expected 90s grace window NextRunAt diff, got %v", graceDiff)
	}

	// Schedule automatic redial 20s later (well within 3m voice cooldown).
	attempt, scheduled, err := s.ScheduleVoiceRedialByCall(ctx, task302.ID, task302.LastVoiceCallID, 1, now.Add(20*time.Second))
	if err != nil || !scheduled || attempt != 1 {
		t.Fatalf("expected redial attempt 1 scheduled, got attempt=%d scheduled=%v err=%v", attempt, scheduled, err)
	}

	now = now.Add(21 * time.Second)
	if processed := workerVoice.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected redial task processed at +21s, got %d", processed)
	}
	if len(stubVoice.titles) != 2 || !strings.HasPrefix(stubVoice.titles[1], "[第 1 次重拨]") {
		t.Fatalf("expected redial call bypassing 3m cooldown with [第 1 次重拨] prefix, got %v", stubVoice.titles)
	}
	throttled302, throTotal, _ := s.ListNotificationLogs(ctx, "voice_call", oncallmodel.NotifyStatusThrottled, 0, 10)
	if throTotal != 0 || len(throttled302) != 0 {
		t.Fatalf("expected automatic redial NOT to be throttled by 3m cooldown, got %+v", throttled302)
	}
}
