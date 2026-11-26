package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	alertstore "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	oncallmodel "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

func setupChannelStoreTestDB(t *testing.T) (*gorm.DB, *Store) {
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
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate oncall: %v", err)
	}
	return db, NewStore(db)
}

func TestChannelStore_EncryptAtRestBlankPreserveAndToken(t *testing.T) {
	db, s := setupChannelStoreTestDB(t)
	ctx := context.Background()

	cfg := DefaultChannelGatewayConfig()
	cfg.SMSEnabled = true
	cfg.SMSProvider = "aliyun"
	cfg.AliyunAccessKeyID = "LTAI5tSms"
	cfg.AliyunAccessKeySecret = "plain-sms-secret-001"
	cfg.EmailEnabled = true
	cfg.SMTPHost = "smtp.example.com"
	cfg.SMTPPassword = "plain-smtp-password-002"
	cfg.FeishuEncryptKey = "plain-feishu-encrypt-003"
	cfg.DingTalkAppSecret = "plain-ding-secret-004"
	cfg.WarRoomProvider = "webhook"
	cfg.WarRoomWebhookSecret = "plain-webhook-secret-005"

	if err := s.UpdateChannelConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateChannelConfig: %v", err)
	}

	var rec oncallmodel.ChannelConfigRecord
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("read raw record: %v", err)
	}
	for _, plaintext := range []string{
		"plain-sms-secret-001",
		"plain-smtp-password-002",
		"plain-feishu-encrypt-003",
		"plain-ding-secret-004",
		"plain-webhook-secret-005",
	} {
		if strings.Contains(rec.ConfigJSON, plaintext) {
			t.Fatalf("plaintext secret %q leaked into DB column ConfigJSON", plaintext)
		}
	}

	loaded, err := s.GetChannelConfig(ctx)
	if err != nil {
		t.Fatalf("GetChannelConfig: %v", err)
	}
	if loaded.AliyunAccessKeySecret != "plain-sms-secret-001" ||
		loaded.SMTPPassword != "plain-smtp-password-002" ||
		loaded.FeishuEncryptKey != "plain-feishu-encrypt-003" ||
		loaded.DingTalkAppSecret != "plain-ding-secret-004" ||
		loaded.WarRoomWebhookSecret != "plain-webhook-secret-005" ||
		loaded.ChatOpsCallbackToken == "" {
		t.Fatalf("decrypted fields mismatch: %+v", loaded)
	}

	// Updating with empty secret strings preserves existing encrypted values & callback token.
	updateWithBlanks := &channels.ChannelGatewayConfig{
		SMSEnabled:            true,
		SMSProvider:           "aliyun",
		AliyunAccessKeyID:     "LTAI5tSmsUpdated",
		AliyunAccessKeySecret: "",
		EmailEnabled:          true,
		SMTPHost:              "smtp2.example.com",
		SMTPPassword:          "",
	}
	if err := s.UpdateChannelConfig(ctx, updateWithBlanks); err != nil {
		t.Fatalf("UpdateChannelConfig blanks: %v", err)
	}
	reloaded, err := s.GetChannelConfig(ctx)
	if err != nil {
		t.Fatalf("reload after blank update: %v", err)
	}
	if reloaded.AliyunAccessKeySecret != "plain-sms-secret-001" ||
		reloaded.SMTPPassword != "plain-smtp-password-002" ||
		reloaded.ChatOpsCallbackToken != loaded.ChatOpsCallbackToken {
		t.Fatalf("expected preserved secrets on blank update, got %+v", reloaded)
	}
}

func TestChannelStore_WarRoomUpsertAndReassignIncident(t *testing.T) {
	db, s := setupChannelStoreTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, u := range []iammodel.User{
		{ID: 1, Email: "alice@opspilot.local", DisplayName: "Alice", Phone: "13800000001", Role: iammodel.RoleAdmin, Status: iammodel.StatusActive},
		{ID: 2, Email: "bob@opspilot.local", DisplayName: "Bob", Phone: "13800000002", Role: iammodel.RoleUser, Status: iammodel.StatusActive},
	} {
		uCopy := u
		if err := db.Create(&uCopy).Error; err != nil {
			t.Fatalf("seed user %d: %v", uCopy.ID, err)
		}
	}
	if err := db.Create(&alertmodel.Incident{
		ID:           301,
		Title:        "API 5xx surge",
		Severity:     "critical",
		Status:       alertmodel.StatusOpen,
		DedupeKey:    "inc-301",
		FirstFiredAt: now,
		LastFiredAt:  now,
	}).Error; err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	task := &oncallmodel.EscalationTask{
		IncidentID:    301,
		Title:         "API 5xx surge",
		Severity:      "critical",
		Status:        oncallmodel.TaskStatusPending,
		StepStartedAt: now,
		NextRunAt:     now.Add(5 * time.Minute),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}

	targetName, err := s.ReassignAlertIncident(ctx, 301, 1, 2, "handover to DB owner")
	if err != nil || targetName != "Bob" {
		t.Fatalf("ReassignAlertIncident: targetName=%q err=%v", targetName, err)
	}
	reloadedTask, err := s.GetEscalationTaskByID(ctx, task.ID)
	if err != nil || reloadedTask.PinnedAssigneeID != 2 || reloadedTask.AssigneeUserID != 2 {
		t.Fatalf("expected PinnedAssigneeID=2 on task, got %+v err=%v", reloadedTask, err)
	}

	saved1, err := s.UpsertWarRoom(ctx, &oncallmodel.WarRoom{
		IncidentID: 301,
		Provider:   "console",
		RoomName:   "[INC-0301] API 5xx surge",
		RoomURL:    "https://opspilot.test/alerts/incidents/301",
		CreatedBy:  1,
	})
	if err != nil || saved1.ID == 0 {
		t.Fatalf("UpsertWarRoom create: %+v err=%v", saved1, err)
	}
	rooms, err := s.ListWarRoomsByIncidents(ctx, []uint64{301})
	if err != nil || rooms[301] == nil || rooms[301].RoomURL != "https://opspilot.test/alerts/incidents/301" {
		t.Fatalf("ListWarRoomsByIncidents mismatch: %+v err=%v", rooms, err)
	}
}
