package store

import (
	"os"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

func openMySQLOnCallTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("OPSPILOT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OPSPILOT_TEST_MYSQL_DSN to run MySQL On-Call store integration test")
	}
	t.Setenv("OPSPILOT_SECRET_KEY", "test-only-oncall-mysql-encryption-key")

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

	cfg.DBName = "opspilot_oncall_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE DATABASE `"+cfg.DBName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error)
	t.Cleanup(func() {
		require.NoError(t, admin.Exec("DROP DATABASE `"+cfg.DBName+"`").Error)
	})

	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), quiet)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	// 首次迁移 + 二次幂等迁移均必须在 MySQL 8.0 严格模式下通过
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	return NewStore(db)
}

// TestOnCallStoreMySQLRoundTrip 在真实 MySQL 8.0 严格模式下验证 On-Call 排班、升级任务、语音与通道配置、维护窗口与通知审计 CRUD。
func TestOnCallStoreMySQLRoundTrip(t *testing.T) {
	s := openMySQLOnCallTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)

	// 1. 排班 + 轮转 + 临时替班
	sched := &model.Schedule{
		Name:                            "Core SRE MySQL Schedule",
		Description:                     "primary roster",
		Timezone:                        "Asia/Shanghai",
		HandoffTime:                     "09:00:00",
		ReminderAdvanceHours:            0,
		EscalationRepeatCount:           0,
		EscalationRepeatIntervalSeconds: 300,
		Enabled:                         true,
		CreatedBy:                       1,
	}
	require.NoError(t, s.CreateSchedule(ctx, sched))
	require.NotZero(t, sched.ID)

	rot := &model.Rotation{
		ScheduleID:           sched.ID,
		Name:                 "Primary Tier",
		Tier:                 model.TierPrimary,
		RotationType:         model.RotationTypeDaily,
		ShiftLengthSeconds:   86400,
		UsersJSON:            "[1,2]",
		EffectiveFrom:        now,
		TimeRestrictionType:  model.TimeRestrictionNone,
		RestrictionStartTime: "",
		RestrictionEndTime:   "",
	}
	require.NoError(t, s.CreateRotation(ctx, rot))
	rots, err := s.ListRotationsBySchedule(ctx, sched.ID)
	require.NoError(t, err)
	require.Len(t, rots, 1)

	gotSched, err := s.GetSchedule(ctx, sched.ID)
	require.NoError(t, err)
	require.Equal(t, sched.Name, gotSched.Name)
	require.Len(t, gotSched.Rotations, 1)

	schedules, err := s.ListSchedules(ctx, 0, true)
	require.NoError(t, err)
	require.Len(t, schedules, 1)

	ov := &model.Override{
		ScheduleID:       sched.ID,
		RotationID:       rot.ID,
		Type:             model.OverrideTypeOverride,
		OriginalUserID:   1,
		SubstituteUserID: 2,
		StartTime:        now,
		EndTime:          now.Add(2 * time.Hour),
		Reason:           "mysql integration check",
		Status:           model.OverrideStatusApproved,
		CreatedBy:        1,
	}
	require.NoError(t, s.CreateOverride(ctx, ov))
	overrides, err := s.ListOverridesInWindow(ctx, sched.ID, now.Add(-time.Minute), now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	require.NoError(t, s.DeleteOverride(ctx, ov.ID))

	// 2. 升级策略 + 升级状态机任务认领/保存
	ep := &model.EscalationPolicy{
		ScheduleID:  sched.ID,
		Name:        "L1 Immediate",
		StepNumber:  1,
		WaitSeconds: 300,
		TargetType:  "primary_oncall",
	}
	require.NoError(t, s.CreateEscalationPolicy(ctx, ep))
	policies, err := s.ListEscalationPolicies(ctx, sched.ID)
	require.NoError(t, err)
	require.Len(t, policies, 1)

	task := &model.EscalationTask{
		IncidentID:    5001,
		GroupKey:      "mysql-oncall-group",
		RuleKey:       "high_latency",
		Title:         "API latency spike",
		Severity:      "critical",
		Urgency:       "high",
		ScheduleID:    sched.ID,
		Status:        model.TaskStatusPending,
		StepStartedAt: now,
		NextRunAt:     now,
	}
	require.NoError(t, s.EnqueueEscalationTask(ctx, task))
	claimed, err := s.ClaimDueEscalationTasks(ctx, now.Add(time.Second), time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, task.ID, claimed[0].ID)

	claimed[0].RepeatCount = 1
	claimed[0].Status = model.TaskStatusAcked
	require.NoError(t, s.SaveEscalationTask(ctx, claimed[0]))
	reloadedTask, err := s.GetEscalationTaskByID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusAcked, reloadedTask.Status)
	require.EqualValues(t, 1, reloadedTask.RepeatCount)

	// 3. 语音网关与多通道网关加密配置读写
	require.NoError(t, s.UpdateVoiceConfig(ctx, &voice.VoiceGatewayConfig{
		Provider:              "aliyun",
		DispatchEnabled:       true,
		AliyunAccessKeyID:     "ak-mysql-test",
		AliyunAccessKeySecret: "sk-mysql-secret",
	}))
	loadedVoice, err := s.GetVoiceConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "sk-mysql-secret", loadedVoice.AliyunAccessKeySecret)

	chanCfg := DefaultChannelGatewayConfig()
	chanCfg.SMSEnabled = true
	chanCfg.SMSProvider = "aliyun"
	chanCfg.AliyunAccessKeyID = "ak-sms-test"
	chanCfg.AliyunAccessKeySecret = "sk-sms-secret"
	require.NoError(t, s.UpdateChannelConfig(ctx, chanCfg))
	loadedChan, err := s.GetChannelConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "sk-sms-secret", loadedChan.AliyunAccessKeySecret)

	// 4. 通知审计日志 + 维护窗口
	nlog := &model.NotificationLog{
		TaskID:         task.ID,
		IncidentID:     5001,
		ScheduleID:     sched.ID,
		EscalationStep: 1,
		UserID:         1,
		UserName:       "alice",
		Target:         "13800000001",
		Channel:        "voice_call",
		Provider:       "aliyun",
		CallID:         "call-mysql-001",
		Status:         model.NotifyStatusCalling,
		Message:        "critical latency",
	}
	require.NoError(t, s.CreateNotificationLog(ctx, nlog))
	foundLog, err := s.FindNotificationLogByCall(ctx, "", "call-mysql-001")
	require.NoError(t, err)
	require.Equal(t, nlog.ID, foundLog.ID)

	logs, total, err := s.ListNotificationLogs(ctx, "voice_call", model.NotifyStatusCalling, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)

	mw := &model.MaintenanceWindow{
		Name:        "MySQL Schema Window",
		Description: "planned maintenance",
		ScheduleID:  sched.ID,
		Action:      model.MaintenanceActionSuppressAll,
		StartsAt:    now.Add(-time.Minute),
		EndsAt:      now.Add(time.Hour),
		CreatedBy:   1,
	}
	require.NoError(t, s.CreateMaintenanceWindow(ctx, mw))
	gotMW, err := s.GetMaintenanceWindow(ctx, mw.ID)
	require.NoError(t, err)
	require.Equal(t, mw.Name, gotMW.Name)
	activeMWs, err := s.ListActiveMaintenanceWindowsAt(ctx, now)
	require.NoError(t, err)
	require.Len(t, activeMWs, 1)
	require.NoError(t, s.CancelMaintenanceWindow(ctx, mw.ID, 1))
	require.NoError(t, s.DeleteMaintenanceWindow(ctx, mw.ID))

	require.NoError(t, s.DeleteEscalationPolicy(ctx, ep.ID))
	require.NoError(t, s.DeleteSchedule(ctx, sched.ID))
}
