package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

func newTestStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite :memory:: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate oncall tables: %v", err)
	}
	return NewStore(db), db
}

func TestVoiceStore_DefaultAndEncryptAtRest(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()

	def, err := s.GetVoiceConfig(ctx)
	if err != nil {
		t.Fatalf("GetVoiceConfig default: %v", err)
	}
	if def.Provider != "mock" || !def.DispatchEnabled {
		t.Fatalf("unexpected default config: provider=%q dispatch_enabled=%v", def.Provider, def.DispatchEnabled)
	}

	const aliyunSecret = "my-super-secret-aliyun-key"
	const tencentSecret = "my-super-secret-tencent-key"
	in := &voice.VoiceGatewayConfig{
		Provider:               "aliyun",
		DispatchEnabled:        true,
		AliyunAccessKeyID:      "LTAI5testID",
		AliyunAccessKeySecret:  aliyunSecret,
		AliyunCalledShowNumber: "057188888888",
		AliyunTtsCode:          "TTS_999999",
		TencentSecretID:        "AKIDtest",
		TencentSecretKey:       tencentSecret,
		UpdatedBy:              7,
	}
	if err := s.UpdateVoiceConfig(ctx, in); err != nil {
		t.Fatalf("UpdateVoiceConfig: %v", err)
	}

	// Verify DB row stores encrypted secrets ("v1:...") and never plaintext.
	var rec model.VoiceConfigRecord
	if err := db.Order("id desc").First(&rec).Error; err != nil {
		t.Fatalf("query VoiceConfigRecord: %v", err)
	}
	if strings.Contains(rec.ConfigJSON, aliyunSecret) || strings.Contains(rec.ConfigJSON, tencentSecret) {
		t.Fatalf("plaintext secret leaked in DB config_json: %s", rec.ConfigJSON)
	}
	if !strings.Contains(rec.ConfigJSON, `"v1:`) {
		t.Fatalf("expected secretbox v1 ciphertext prefix in config_json: %s", rec.ConfigJSON)
	}

	loaded, err := s.GetVoiceConfig(ctx)
	if err != nil {
		t.Fatalf("GetVoiceConfig after save: %v", err)
	}
	if loaded.AliyunAccessKeySecret != aliyunSecret {
		t.Fatalf("aliyun secret mismatch: got %q want %q", loaded.AliyunAccessKeySecret, aliyunSecret)
	}
	if loaded.TencentSecretKey != tencentSecret {
		t.Fatalf("tencent secret mismatch: got %q want %q", loaded.TencentSecretKey, tencentSecret)
	}
	if !loaded.AliyunSecretConfigured || !loaded.TencentSecretConfigured {
		t.Fatalf("expected secret_configured booleans true, got aliyun=%v tencent=%v", loaded.AliyunSecretConfigured, loaded.TencentSecretConfigured)
	}
	if loaded.WebhookToken == "" {
		t.Fatalf("expected webhook token to be auto-generated on save")
	}
}

func TestVoiceStore_LegacyPlaintextReadThrough(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()

	legacyJSON := `{"provider":"aliyun","aliyun_access_key_id":"LTAIold","aliyun_access_key_secret":"legacy-plain-secret"}`
	if err := db.Exec(
		`INSERT INTO oncall_voice_configs (provider, dispatch_enabled, config_json, updated_by, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"aliyun", false, legacyJSON, 1, time.Now(),
	).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	cfg, err := s.GetVoiceConfig(ctx)
	if err != nil {
		t.Fatalf("GetVoiceConfig legacy row: %v", err)
	}
	if cfg.AliyunAccessKeySecret != "legacy-plain-secret" {
		t.Fatalf("expected legacy plaintext passthrough, got %q", cfg.AliyunAccessKeySecret)
	}
	if cfg.DispatchEnabled {
		t.Fatalf("expected DispatchEnabled=false preserved from column")
	}

	// Updating on first save with DispatchEnabled=false must also persist false despite DB default=true.
	s2, _ := newTestStore(t)
	if err := s2.UpdateVoiceConfig(ctx, &voice.VoiceGatewayConfig{
		Provider:        "mock",
		DispatchEnabled: false,
	}); err != nil {
		t.Fatalf("UpdateVoiceConfig dispatch=false on fresh store: %v", err)
	}
	loaded2, err := s2.GetVoiceConfig(ctx)
	if err != nil {
		t.Fatalf("GetVoiceConfig fresh store: %v", err)
	}
	if loaded2.DispatchEnabled {
		t.Fatalf("expected DispatchEnabled=false persisted on initial save")
	}
}
