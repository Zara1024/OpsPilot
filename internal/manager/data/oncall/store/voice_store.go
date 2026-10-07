package store

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
)

type VoiceConfigRecord struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Provider   string    `gorm:"size:32;default:'mock'" json:"provider"`
	ConfigJSON string    `gorm:"type:text" json:"config_json"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (VoiceConfigRecord) TableName() string {
	return "oncall_voice_configs"
}

var (
	voiceMu      sync.RWMutex
	cachedConfig = &voice.VoiceGatewayConfig{
		Provider:      "mock",
		AliyunRegion:  "cn-hangzhou",
		TencentRegion: "ap-guangzhou",
		UpdatedAt:     time.Now(),
	}
)

// GetVoiceConfig retrieves the current voice gateway configuration.
func (s *Store) GetVoiceConfig(ctx context.Context) (*voice.VoiceGatewayConfig, error) {
	voiceMu.RLock()
	defer voiceMu.RUnlock()

	// Try reading from database if table exists
	var rec VoiceConfigRecord
	if err := s.db.WithContext(ctx).Order("id desc").First(&rec).Error; err == nil && rec.ConfigJSON != "" {
		var cfg voice.VoiceGatewayConfig
		if jsonErr := json.Unmarshal([]byte(rec.ConfigJSON), &cfg); jsonErr == nil {
			return &cfg, nil
		}
	}

	// Fallback to memory cached config
	cp := *cachedConfig
	return &cp, nil
}

// UpdateVoiceConfig persists the updated voice gateway configuration.
func (s *Store) UpdateVoiceConfig(ctx context.Context, cfg *voice.VoiceGatewayConfig) error {
	voiceMu.Lock()
	defer voiceMu.Unlock()

	cfg.UpdatedAt = time.Now()
	cachedConfig = cfg

	// Ensure table exists
	_ = s.db.WithContext(ctx).AutoMigrate(&VoiceConfigRecord{})

	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	rec := VoiceConfigRecord{
		Provider:   cfg.Provider,
		ConfigJSON: string(jsonBytes),
		UpdatedAt:  cfg.UpdatedAt,
	}

	// Upsert or save latest record
	var existing VoiceConfigRecord
	if err := s.db.WithContext(ctx).Order("id desc").First(&existing).Error; err == nil {
		existing.Provider = rec.Provider
		existing.ConfigJSON = rec.ConfigJSON
		existing.UpdatedAt = rec.UpdatedAt
		return s.db.WithContext(ctx).Save(&existing).Error
	}

	return s.db.WithContext(ctx).Create(&rec).Error
}
