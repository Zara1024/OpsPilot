package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/secretbox"
)

// DefaultVoiceGatewayConfig returns the safe baseline voice gateway configuration.
func DefaultVoiceGatewayConfig() *voice.VoiceGatewayConfig {
	return &voice.VoiceGatewayConfig{
		Provider:                  "mock",
		FallbackProvider:          "",
		FailoverTimeoutSeconds:    8,
		DispatchEnabled:           true,
		VoiceRedialMaxAttempts:    1,
		VoiceRedialDelaySeconds:   20,
		VoiceFastEscalateOnReject: true,
		VoiceRedialOnNoDTMF:       false,
		AliyunRegion:              "cn-hangzhou",
		TencentRegion:             "ap-guangzhou",
	}
}

// GetVoiceConfig loads the persisted voice gateway configuration and decrypts stored provider secrets.
func (s *Store) GetVoiceConfig(ctx context.Context) (*voice.VoiceGatewayConfig, error) {
	var rec model.VoiceConfigRecord
	err := s.db.WithContext(ctx).Order("id desc").First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultVoiceGatewayConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("load voice config: %w", err)
	}

	cfg := DefaultVoiceGatewayConfig()
	if rec.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(rec.ConfigJSON), cfg); err != nil {
			return nil, fmt.Errorf("decode voice config: %w", err)
		}
	}

	if cfg.AliyunAccessKeySecret != "" {
		dec, decErr := secretbox.Decrypt(cfg.AliyunAccessKeySecret)
		if decErr != nil {
			return nil, fmt.Errorf("decrypt aliyun secret: %w", decErr)
		}
		cfg.AliyunAccessKeySecret = dec
	}
	if cfg.TencentSecretKey != "" {
		dec, decErr := secretbox.Decrypt(cfg.TencentSecretKey)
		if decErr != nil {
			return nil, fmt.Errorf("decrypt tencent secret: %w", decErr)
		}
		cfg.TencentSecretKey = dec
	}
	if cfg.WebhookToken != "" {
		dec, decErr := secretbox.Decrypt(cfg.WebhookToken)
		if decErr != nil {
			return nil, fmt.Errorf("decrypt webhook token: %w", decErr)
		}
		cfg.WebhookToken = dec
	}

	if cfg.Provider == "" {
		cfg.Provider = rec.Provider
	}
	if cfg.Provider == "" {
		cfg.Provider = "mock"
	}
	if cfg.AliyunRegion == "" {
		cfg.AliyunRegion = "cn-hangzhou"
	}
	if cfg.TencentRegion == "" {
		cfg.TencentRegion = "ap-guangzhou"
	}

	cfg.DispatchEnabled = rec.DispatchEnabled
	cfg.FailoverTimeoutSeconds = voice.NormalizeFailoverTimeoutSeconds(cfg.FailoverTimeoutSeconds)
	cfg.VoiceRedialMaxAttempts = voice.NormalizeVoiceRedialMaxAttempts(cfg.VoiceRedialMaxAttempts)
	cfg.VoiceRedialDelaySeconds = voice.NormalizeVoiceRedialDelaySeconds(cfg.VoiceRedialDelaySeconds)
	cfg.UpdatedBy = rec.UpdatedBy
	cfg.UpdatedAt = rec.UpdatedAt
	cfg.AliyunSecretConfigured = cfg.AliyunAccessKeySecret != ""
	cfg.TencentSecretConfigured = cfg.TencentSecretKey != ""
	cfg.CallbackURLs = nil
	return cfg, nil
}

// UpdateVoiceConfig encrypts sensitive credentials at rest and persists the voice gateway configuration.
func (s *Store) UpdateVoiceConfig(ctx context.Context, cfg *voice.VoiceGatewayConfig) error {
	if cfg == nil {
		return errors.New("voice config is nil")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.VoiceConfigRecord
		err := tx.Order("id desc").First(&existing).Error
		hasExisting := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query voice config: %w", err)
		}

		// Preserve existing webhook token when the caller omits it; otherwise generate a new cryptographic token.
		if cfg.WebhookToken == "" && hasExisting && existing.ConfigJSON != "" {
			var prev voice.VoiceGatewayConfig
			if jsonErr := json.Unmarshal([]byte(existing.ConfigJSON), &prev); jsonErr == nil && prev.WebhookToken != "" {
				if dec, decErr := secretbox.Decrypt(prev.WebhookToken); decErr == nil {
					cfg.WebhookToken = dec
				}
			}
		}
		if cfg.WebhookToken == "" {
			cfg.WebhookToken = generateVoiceWebhookToken()
		}

		if cfg.Provider == "" {
			cfg.Provider = "mock"
		}
		if cfg.AliyunRegion == "" {
			cfg.AliyunRegion = "cn-hangzhou"
		}
		if cfg.TencentRegion == "" {
			cfg.TencentRegion = "ap-guangzhou"
		}
		cfg.FailoverTimeoutSeconds = voice.NormalizeFailoverTimeoutSeconds(cfg.FailoverTimeoutSeconds)
		cfg.VoiceRedialMaxAttempts = voice.NormalizeVoiceRedialMaxAttempts(cfg.VoiceRedialMaxAttempts)
		cfg.VoiceRedialDelaySeconds = voice.NormalizeVoiceRedialDelaySeconds(cfg.VoiceRedialDelaySeconds)
		now := time.Now()
		cfg.UpdatedAt = now
		cfg.AliyunSecretConfigured = cfg.AliyunAccessKeySecret != ""
		cfg.TencentSecretConfigured = cfg.TencentSecretKey != ""

		toStore := *cfg
		toStore.AliyunSecretConfigured = false
		toStore.TencentSecretConfigured = false
		toStore.CallbackURLs = nil

		if toStore.AliyunAccessKeySecret != "" {
			enc, encErr := secretbox.Encrypt(toStore.AliyunAccessKeySecret)
			if encErr != nil {
				return fmt.Errorf("encrypt aliyun secret: %w", encErr)
			}
			toStore.AliyunAccessKeySecret = enc
		}
		if toStore.TencentSecretKey != "" {
			enc, encErr := secretbox.Encrypt(toStore.TencentSecretKey)
			if encErr != nil {
				return fmt.Errorf("encrypt tencent secret: %w", encErr)
			}
			toStore.TencentSecretKey = enc
		}
		if toStore.WebhookToken != "" {
			enc, encErr := secretbox.Encrypt(toStore.WebhookToken)
			if encErr != nil {
				return fmt.Errorf("encrypt webhook token: %w", encErr)
			}
			toStore.WebhookToken = enc
		}

		jsonBytes, err := json.Marshal(&toStore)
		if err != nil {
			return fmt.Errorf("encode voice config: %w", err)
		}

		if hasExisting {
			existing.Provider = cfg.Provider
			existing.DispatchEnabled = cfg.DispatchEnabled
			existing.ConfigJSON = string(jsonBytes)
			existing.UpdatedBy = cfg.UpdatedBy
			existing.UpdatedAt = now
			if err := tx.Save(&existing).Error; err != nil {
				return fmt.Errorf("save voice config: %w", err)
			}
			return nil
		}

		rec := model.VoiceConfigRecord{
			Provider:        cfg.Provider,
			DispatchEnabled: cfg.DispatchEnabled,
			ConfigJSON:      string(jsonBytes),
			UpdatedBy:       cfg.UpdatedBy,
			UpdatedAt:       now,
		}
		if err := tx.Create(&rec).Error; err != nil {
			return fmt.Errorf("create voice config: %w", err)
		}
		if !cfg.DispatchEnabled {
			if err := tx.Model(&rec).Update("dispatch_enabled", false).Error; err != nil {
				return fmt.Errorf("persist dispatch_enabled=false: %w", err)
			}
		}
		return nil
	})
}

// EnsureVoiceWebhookToken ensures a persisted webhook callback token exists and returns the decrypted token.
func (s *Store) EnsureVoiceWebhookToken(ctx context.Context) (string, error) {
	cfg, err := s.GetVoiceConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.WebhookToken != "" {
		return cfg.WebhookToken, nil
	}
	cfg.WebhookToken = generateVoiceWebhookToken()
	if err := s.UpdateVoiceConfig(ctx, cfg); err != nil {
		return "", err
	}
	return cfg.WebhookToken, nil
}

// RotateVoiceWebhookToken generates a brand-new webhook callback token and persists it encrypted while preserving other secrets.
func (s *Store) RotateVoiceWebhookToken(ctx context.Context, updatedBy uint64) (*voice.VoiceGatewayConfig, error) {
	cfg, err := s.GetVoiceConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = DefaultVoiceGatewayConfig()
	}
	cfg.WebhookToken = generateVoiceWebhookToken()
	if updatedBy > 0 {
		cfg.UpdatedBy = updatedBy
	}
	if err := s.UpdateVoiceConfig(ctx, cfg); err != nil {
		return nil, fmt.Errorf("rotate voice webhook token: %w", err)
	}
	return cfg, nil
}

func generateVoiceWebhookToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
