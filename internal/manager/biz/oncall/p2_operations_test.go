package oncall

import (
	"bytes"
	"context"
	"encoding/json"
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

func openP2TestDB(t *testing.T) (*gorm.DB, *oncallstore.Store) {
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
	for _, u := range []iammodel.User{
		{ID: 1, Email: "alice@opspilot.local", DisplayName: "Alice", Phone: "13800000001", Role: iammodel.RoleAdmin, Status: iammodel.StatusActive},
		{ID: 2, Email: "bob@opspilot.local", DisplayName: "Bob", Phone: "13800000002", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
	} {
		uc := u
		if err := db.Create(&uc).Error; err != nil {
			t.Fatalf("create user %d: %v", uc.ID, err)
		}
	}
	return db, oncallstore.NewStore(db)
}

func TestCNStatutoryHolidayAndMakeupWorkdayShifts(t *testing.T) {
	_, s := openP2TestDB(t)
	ctx := context.Background()
	loc := loadScheduleLocation("Asia/Shanghai")

	anchor := time.Date(2025, 12, 30, 9, 0, 0, 0, loc)
	sched := &oncallmodel.Schedule{
		Name:        "CN Workday Schedule",
		Timezone:    "Asia/Shanghai",
		HandoffTime: "09:00:00",
		Enabled:     true,
		CreatedBy:   1,
		CreatedAt:   anchor,
		UpdatedAt:   anchor,
	}
	if err := s.CreateSchedule(ctx, sched); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	// Custom override: 2026-01-08 (Thursday) marked as holiday ("公司年会调休").
	if _, err := s.UpsertCalendarDay(ctx, &oncallmodel.CalendarDay{
		Date:    "2026-01-08",
		Region:  oncallmodel.CalendarRegionCN,
		DayType: oncallmodel.DayTypeHoliday,
		Name:    "公司年会放假",
		Source:  oncallmodel.CalendarSourceCustom,
	}); err != nil {
		t.Fatalf("UpsertCalendarDay: %v", err)
	}

	if err := s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID:          sched.ID,
		Name:                "Workday Tier",
		Tier:                oncallmodel.TierPrimary,
		RotationType:        oncallmodel.RotationTypeDaily,
		ShiftLengthSeconds:  86400,
		UsersJSON:           `[1]`,
		EffectiveFrom:       anchor,
		TimeRestrictionType: oncallmodel.TimeRestrictionWorkdayCN,
		CreatedAt:           anchor,
		UpdatedAt:           anchor,
	}); err != nil {
		t.Fatalf("CreateRotation workday_cn: %v", err)
	}
	if err := s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID:          sched.ID,
		Name:                "Holiday Tier",
		Tier:                oncallmodel.TierSecondary,
		RotationType:        oncallmodel.RotationTypeDaily,
		ShiftLengthSeconds:  86400,
		UsersJSON:           `[2]`,
		EffectiveFrom:       anchor,
		TimeRestrictionType: oncallmodel.TimeRestrictionHolidayCN,
		CreatedAt:           anchor,
		UpdatedAt:           anchor,
	}); err != nil {
		t.Fatalf("CreateRotation holiday_cn: %v", err)
	}

	scheduler := NewScheduler(s)
	winStart := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	winEnd := time.Date(2026, 1, 12, 0, 0, 0, 0, loc)
	shifts, err := scheduler.ComputeScheduleShifts(ctx, sched.ID, winStart, winEnd)
	if err != nil {
		t.Fatalf("ComputeScheduleShifts: %v", err)
	}

	workdayDates := make(map[string]*ShiftSlot)
	holidayDates := make(map[string]*ShiftSlot)
	for _, sh := range shifts {
		d := sh.StartTime.In(loc).Format("2006-01-02")
		if sh.Tier == oncallmodel.TierPrimary {
			workdayDates[d] = sh
		} else {
			holidayDates[d] = sh
		}
	}

	// 2026-01-01 (Thu, 元旦) -> excluded from workday_cn, included in holiday_cn.
	if _, ok := workdayDates["2026-01-01"]; ok {
		t.Fatalf("expected 2026-01-01 (New Year) skipped from workday_cn shifts")
	}
	if sh, ok := holidayDates["2026-01-01"]; !ok || sh.DayTag != "holiday" || sh.DayLabel != "元旦" {
		t.Fatalf("expected 2026-01-01 in holiday_cn with 元旦 label, got %+v", sh)
	}

	// 2026-01-04 (Sun, 元旦调休补班) -> included in workday_cn, excluded from holiday_cn.
	if sh, ok := workdayDates["2026-01-04"]; !ok || sh.DayTag != "makeup_workday" {
		t.Fatalf("expected 2026-01-04 (Sunday makeup workday) included in workday_cn, got %+v", sh)
	}
	if _, ok := holidayDates["2026-01-04"]; ok {
		t.Fatalf("expected 2026-01-04 (makeup workday) excluded from holiday_cn")
	}

	// 2026-01-08 (Thu, custom holiday override) -> skipped from workday_cn, present in holiday_cn.
	if _, ok := workdayDates["2026-01-08"]; ok {
		t.Fatalf("expected custom holiday 2026-01-08 skipped from workday_cn")
	}
	if sh, ok := holidayDates["2026-01-08"]; !ok || sh.DayLabel != "公司年会放假" {
		t.Fatalf("expected custom holiday 2026-01-08 in holiday_cn, got %+v", sh)
	}

	// Normal Sunday 2026-01-11 -> holiday_cn only.
	if _, ok := workdayDates["2026-01-11"]; ok {
		t.Fatalf("expected normal Sunday 2026-01-11 skipped from workday_cn")
	}
	if _, ok := holidayDates["2026-01-11"]; !ok {
		t.Fatalf("expected normal Sunday 2026-01-11 present in holiday_cn")
	}
}

func TestMaintenanceWindow_SuppressAllAndSuppressVoice(t *testing.T) {
	db, s := openP2TestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	sched := &oncallmodel.Schedule{
		Name: "Core SRE", Timezone: "Asia/Shanghai", HandoffTime: "09:00:00",
		Enabled: true, CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	_ = s.CreateSchedule(ctx, sched)
	_ = s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID: sched.ID, Name: "Primary", Tier: oncallmodel.TierPrimary,
		RotationType: oncallmodel.RotationTypeDaily, ShiftLengthSeconds: 86400,
		UsersJSON: `[1]`, EffectiveFrom: now.Add(-24 * time.Hour), CreatedAt: now, UpdatedAt: now,
	})
	_ = s.CreateEscalationPolicy(ctx, &oncallmodel.EscalationPolicy{
		ScheduleID: sched.ID, Name: "Step1", StepNumber: 1, WaitSeconds: 0, TargetType: "primary_oncall", CreatedAt: now,
	})

	// Part A: Active suppress_all window matching service=payment stops Dispatcher.
	payMatcher, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "payment"}})
	mwAll := &oncallmodel.MaintenanceWindow{
		Name:        "Payment DB Migration",
		ScheduleID:  sched.ID,
		MatcherJSON: string(payMatcher),
		Action:      oncallmodel.MaintenanceActionSuppressAll,
		StartsAt:    now.Add(-5 * time.Minute),
		EndsAt:      now.Add(25 * time.Minute),
		Status:      oncallmodel.MaintenanceStatusActive,
		CreatedBy:   1,
	}
	if err := s.CreateMaintenanceWindow(ctx, mwAll); err != nil {
		t.Fatalf("CreateMaintenanceWindow suppress_all: %v", err)
	}

	metrics := NewMetrics(nil)
	disp := NewDispatcher(s, NewRouteMatcher(s), nil, metrics, nil)
	disp.SetClock(func() time.Time { return now })

	_ = db.Create(&alertmodel.Incident{
		ID: 501, Rule: "db_conn_high", Severity: "critical", Title: "Payment DB saturation",
		Status: alertmodel.StatusOpen, DedupeKey: "pay-501", LabelsJSON: `{"service":"payment"}`,
		FirstFiredAt: now, LastFiredAt: now,
	}).Error
	disp.DispatchFiredSync(ctx, 501, "db_conn_high", "Payment DB saturation", "critical", 1, map[string]string{"service": "payment"}, now)

	tasks, _ := s.ListEscalationTasks(ctx, oncallmodel.TaskStatusPending, 10)
	if len(tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks under suppress_all window, got %d", len(tasks))
	}
	reloadedMW, _ := s.GetMaintenanceWindow(ctx, mwAll.ID)
	if reloadedMW.SuppressedCount != 1 {
		t.Fatalf("expected suppress_all SuppressedCount=1, got %d", reloadedMW.SuppressedCount)
	}
	supLogs, supTotal, _ := s.ListNotificationLogs(ctx, "maintenance", oncallmodel.NotifyStatusSuppressed, 0, 10)
	if supTotal != 1 || supLogs[0].IncidentID != 501 {
		t.Fatalf("expected suppressed audit log for 501, got total=%d logs=%+v", supTotal, supLogs)
	}

	// Part B: Pending task created earlier gets delayed to EndsAt when suppress_all is active.
	delayedTask := &oncallmodel.EscalationTask{
		IncidentID:    501,
		Title:         "Payment DB saturation",
		Severity:      "critical",
		Urgency:       "high",
		ScheduleID:    sched.ID,
		CurrentStep:   1,
		LadderStep:    1,
		StepStartedAt: now,
		Status:        oncallmodel.TaskStatusPending,
		NextRunAt:     now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.EnqueueEscalationTask(ctx, delayedTask); err != nil {
		t.Fatalf("seed delayedTask: %v", err)
	}

	deliverer := &captureLabelsDeliverer{}
	curNow := now
	worker := NewEscalationWorker(EscalationWorkerOpts{
		Store:          s,
		Scheduler:      NewScheduler(s),
		VoiceGateway:   voice.NewVoiceGateway(),
		ChannelGateway: channels.NewChannelGateway(),
		Deliverer:      deliverer,
		Now:            func() time.Time { return curNow },
	})
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected pending task processed/delayed by maintenance check, got %d", processed)
	}
	if deliverer.calls != 0 {
		t.Fatalf("expected 0 deliveries while suppressed, got %d", deliverer.calls)
	}
	storedDelayed, _ := s.GetEscalationTaskByID(ctx, delayedTask.ID)
	if storedDelayed.NextRunAt.Before(mwAll.EndsAt.Add(-time.Second)) {
		t.Fatalf("expected NextRunAt delayed to EndsAt (%v), got %v", mwAll.EndsAt, storedDelayed.NextRunAt)
	}

	// Part C: suppress_voice allows IM notification on Step 1, suppresses voice_call on Step 2.
	_ = s.SetUserNotificationRules(ctx, 1, "high", []*oncallmodel.UserNotificationRule{
		{UserID: 1, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
		{UserID: 1, Urgency: "high", StepNumber: 2, DelayMinutes: 1, Channel: "voice_call", Enabled: true},
	})
	authMatcher, _ := json.Marshal([]LabelMatcher{{Name: "service", Operator: OpEqual, Value: "auth"}})
	mwVoice := &oncallmodel.MaintenanceWindow{
		Name:        "Auth Quiet Deploy",
		ScheduleID:  sched.ID,
		MatcherJSON: string(authMatcher),
		Action:      oncallmodel.MaintenanceActionSuppressVoice,
		StartsAt:    now.Add(-time.Minute),
		EndsAt:      now.Add(30 * time.Minute),
		Status:      oncallmodel.MaintenanceStatusActive,
		CreatedBy:   1,
	}
	_ = s.CreateMaintenanceWindow(ctx, mwVoice)
	_ = db.Create(&alertmodel.Incident{
		ID: 502, Rule: "auth_latency", Severity: "critical", Title: "Auth P99 spike",
		Status: alertmodel.StatusOpen, DedupeKey: "auth-502", LabelsJSON: `{"service":"auth"}`,
		FirstFiredAt: now, LastFiredAt: now,
	}).Error
	disp.DispatchFiredSync(ctx, 502, "auth_latency", "Auth P99 spike", "critical", 1, map[string]string{"service": "auth"}, now)

	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected im_dm step processed under suppress_voice, got %d", processed)
	}
	if deliverer.calls != 1 {
		t.Fatalf("expected IM notification sent under suppress_voice, got %d", deliverer.calls)
	}

	curNow = curNow.Add(90 * time.Second)
	if processed := worker.ProcessBatch(ctx); processed != 1 {
		t.Fatalf("expected voice_call step evaluated at +90s, got %d", processed)
	}
	voiceSupLogs, voiceSupTotal, _ := s.ListNotificationLogs(ctx, "voice_call", oncallmodel.NotifyStatusSuppressed, 0, 10)
	if voiceSupTotal != 1 || voiceSupLogs[0].IncidentID != 502 {
		t.Fatalf("expected voice_call log with status suppressed, got total=%d logs=%+v", voiceSupTotal, voiceSupLogs)
	}
	reloadedVoiceMW, _ := s.GetMaintenanceWindow(ctx, mwVoice.ID)
	if reloadedVoiceMW.SuppressedCount != 1 {
		t.Fatalf("expected suppress_voice SuppressedCount=1, got %d", reloadedVoiceMW.SuppressedCount)
	}
}

func TestBurnoutAnalytics_PercentilesRealShiftHoursAndCSVExport(t *testing.T) {
	db, s := openP2TestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	sched := &oncallmodel.Schedule{
		Name: "Core SRE", Timezone: "Asia/Shanghai", HandoffTime: "09:00:00",
		Enabled: true, CreatedBy: 1, CreatedAt: now.AddDate(0, 0, -10), UpdatedAt: now,
	}
	_ = s.CreateSchedule(ctx, sched)
	_ = s.CreateRotation(ctx, &oncallmodel.Rotation{
		ScheduleID: sched.ID, Name: "Primary", Tier: oncallmodel.TierPrimary,
		RotationType: oncallmodel.RotationTypeDaily, ShiftLengthSeconds: 86400,
		UsersJSON: `[1,2]`, EffectiveFrom: now.AddDate(0, 0, -10), CreatedAt: now, UpdatedAt: now,
	})

	aliceID := uint64(1)
	bobID := uint64(2)
	samples := []struct {
		id       uint64
		ruleName string
		sev      string
		ackDur   time.Duration
		resDur   time.Duration
		ackBy    *uint64
		resBy    *uint64
		status   string
		events   uint64
	}{
		{601, "=SUM(1,2)", "critical", 60 * time.Second, 300 * time.Second, &aliceID, &aliceID, alertmodel.StatusResolved, 5},
		{602, "api_latency", "critical", 120 * time.Second, 600 * time.Second, &bobID, &bobID, alertmodel.StatusResolved, 3},
		{603, "disk_high", "warning", 300 * time.Second, 0, &aliceID, nil, alertmodel.StatusAcknowledged, 2},
		{604, "api_latency", "warning", 600 * time.Second, 1200 * time.Second, &aliceID, &aliceID, alertmodel.StatusResolved, 4},
	}
	for _, sm := range samples {
		fired := now.Add(-2 * time.Hour)
		var ackAt, resAt *time.Time
		if sm.ackDur > 0 {
			tAck := fired.Add(sm.ackDur)
			ackAt = &tAck
		}
		if sm.resDur > 0 {
			tRes := fired.Add(sm.resDur)
			resAt = &tRes
		}
		_ = db.Create(&alertmodel.Incident{
			ID: sm.id, Rule: sm.ruleName, RuleName: sm.ruleName, Severity: sm.sev, Title: sm.ruleName,
			Status: sm.status, DedupeKey: sm.ruleName + string(rune('a'+int(sm.id%10))), EventCount: sm.events,
			FirstFiredAt: fired, LastFiredAt: fired.Add(sm.ackDur),
			AcknowledgedAt: ackAt, AcknowledgedBy: sm.ackBy, ResolvedAt: resAt, ResolvedBy: sm.resBy,
		}).Error
	}

	// Grouped escalation task (AlertCount=3 -> 2 merged alerts) & standalone task for 604 & Maintenance (SuppressedCount=2).
	_ = db.Create(&oncallmodel.EscalationTask{
		IncidentID: 601, ScheduleID: sched.ID, Title: "group", Severity: "critical",
		AlertCount: 3, MergedIncidentIDsJSON: `[601,602,603]`, Status: oncallmodel.TaskStatusAcked,
		StepStartedAt: now.Add(-time.Hour), NextRunAt: now, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}).Error
	_ = db.Create(&oncallmodel.EscalationTask{
		IncidentID: 604, ScheduleID: sched.ID, Title: "api_latency", Severity: "warning",
		AlertCount: 1, MergedIncidentIDsJSON: `[604]`, Status: oncallmodel.TaskStatusAcked,
		StepStartedAt: now.Add(-time.Hour), NextRunAt: now, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}).Error
	_ = s.CreateMaintenanceWindow(ctx, &oncallmodel.MaintenanceWindow{
		Name: "Planned Window", ScheduleID: sched.ID, Action: oncallmodel.MaintenanceActionSuppressAll,
		StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(time.Hour), Status: oncallmodel.MaintenanceStatusActive,
		SuppressedCount: 2,
	})
	_ = s.CreateNotificationLog(ctx, &oncallmodel.NotificationLog{
		IncidentID: 601, ScheduleID: sched.ID, UserID: 1, UserName: "Alice", Channel: "voice_call",
		Provider: "aliyun", Status: oncallmodel.NotifyStatusAnswered, IsNight: true, CreatedAt: now.Add(-30 * time.Minute),
	})

	analyzer := NewBurnoutAnalyzer(s, NewScheduler(s))
	res, err := analyzer.GetBurnoutAnalytics(ctx, sched.ID, "7d")
	if err != nil {
		t.Fatalf("GetBurnoutAnalytics: %v", err)
	}
	if res.TotalIncidents != 4 || res.AcknowledgedCount != 4 || res.ResolvedCount != 3 || res.AckRate != 100 || res.ResolveRate != 75 {
		t.Fatalf("unexpected counts/rates: %+v", res)
	}
	if res.MTTASeconds != 270 || res.MTTAP50Seconds < 120 || res.MTTAP90Seconds <= res.MTTAP50Seconds {
		t.Fatalf("unexpected MTTA percentiles: avg=%.1f p50=%.1f p90=%.1f p95=%.1f", res.MTTASeconds, res.MTTAP50Seconds, res.MTTAP90Seconds, res.MTTAP95Seconds)
	}
	if res.MTTRSeconds != 700 || res.MTTRP50Seconds != 600 || res.MTTRP90Seconds <= res.MTTRP50Seconds {
		t.Fatalf("unexpected MTTR metrics: avg=%.1f p50=%.1f p90=%.1f", res.MTTRSeconds, res.MTTRP50Seconds, res.MTTRP90Seconds)
	}
	if res.MergedAlertsCount != 2 || res.SuppressedAlertsCount != 2 || res.NoiseReductionRatio != 50 {
		t.Fatalf("unexpected noise metrics: merged=%d suppressed=%d ratio=%.1f", res.MergedAlertsCount, res.SuppressedAlertsCount, res.NoiseReductionRatio)
	}
	if res.NightCallCount != 1 || len(res.Trends) < 7 || len(res.SeverityBreakdown) != 2 {
		t.Fatalf("unexpected night/trends/severity breakdown: %+v", res)
	}

	var aliceLoad *EngineerLoad
	for i := range res.EngineerLoads {
		if res.EngineerLoads[i].UserID == 1 {
			aliceLoad = &res.EngineerLoads[i]
		}
	}
	if aliceLoad == nil || aliceLoad.OnCallHours == 168 || aliceLoad.OnCallHours < 48 || aliceLoad.IncidentsHandled != 3 || aliceLoad.ResolvedCount != 2 {
		t.Fatalf("expected real rotated shift hours (<168 for 2-person 7d rotation) and real handled/resolved counts for Alice, got %+v", aliceLoad)
	}

	// Unrelated schedule with 0 incidents must return empty noisy alerts slice (never fake mock items).
	emptyNoisy, err := analyzer.GetNoisyAlerts(ctx, 9999, "7d")
	if err != nil || len(emptyNoisy) != 0 {
		t.Fatalf("expected 0 noisy alerts for empty schedule, got %d err=%v", len(emptyNoisy), err)
	}

	noisy, err := analyzer.GetNoisyAlerts(ctx, sched.ID, "7d")
	if err != nil || len(noisy) == 0 {
		t.Fatalf("expected noisy rules from seeded incidents, got %d err=%v", len(noisy), err)
	}

	csvData, err := BuildAnalyticsCSV(res, noisy)
	if err != nil {
		t.Fatalf("BuildAnalyticsCSV: %v", err)
	}
	if !bytes.HasPrefix(csvData, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("expected UTF-8 BOM prefix on exported CSV")
	}
	csvStr := string(csvData)
	if !strings.Contains(csvStr, "'=SUM(1,2)") {
		t.Fatalf("expected CSV formula injection escaping on '=SUM(1,2), got:\n%s", csvStr)
	}
}
