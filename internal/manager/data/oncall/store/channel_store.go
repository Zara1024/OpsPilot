package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/secretbox"
)

// DefaultChannelGatewayConfig returns safe baseline configuration for SMS, SMTP Email, and ChatOps/WarRoom.
func DefaultChannelGatewayConfig() *channels.ChannelGatewayConfig {
	return &channels.ChannelGatewayConfig{
		SMSProvider:        "",
		AliyunRegion:       "cn-hangzhou",
		TencentRegion:      "ap-guangzhou",
		SMTPPort:           465,
		SMTPTLSMode:        "ssl",
		SMTPTimeoutSeconds: 10,
		WarRoomProvider:    "",
	}
}

func channelSecretSlots(cfg *channels.ChannelGatewayConfig) []struct {
	name string
	val  *string
} {
	return []struct {
		name string
		val  *string
	}{
		{"aliyun_sms_secret", &cfg.AliyunAccessKeySecret},
		{"tencent_sms_secret", &cfg.TencentSecretKey},
		{"smtp_password", &cfg.SMTPPassword},
		{"chatops_callback_token", &cfg.ChatOpsCallbackToken},
		{"feishu_verification_token", &cfg.FeishuVerificationToken},
		{"feishu_encrypt_key", &cfg.FeishuEncryptKey},
		{"dingtalk_app_secret", &cfg.DingTalkAppSecret},
		{"feishu_app_secret", &cfg.FeishuAppSecret},
		{"war_room_webhook_secret", &cfg.WarRoomWebhookSecret},
	}
}

func decryptChannelSecrets(cfg *channels.ChannelGatewayConfig) error {
	for _, slot := range channelSecretSlots(cfg) {
		if *slot.val == "" {
			continue
		}
		dec, err := secretbox.Decrypt(*slot.val)
		if err != nil {
			return fmt.Errorf("decrypt %s: %w", slot.name, err)
		}
		*slot.val = dec
	}
	return nil
}

func applyChannelConfigDefaults(cfg *channels.ChannelGatewayConfig) {
	if cfg.AliyunRegion == "" {
		cfg.AliyunRegion = "cn-hangzhou"
	}
	if cfg.TencentRegion == "" {
		cfg.TencentRegion = "ap-guangzhou"
	}
	cfg.SMTPTLSMode = channels.NormalizeSMTPTLSMode(cfg.SMTPTLSMode)
	cfg.SMTPPort = channels.NormalizeSMTPPort(cfg.SMTPPort, cfg.SMTPTLSMode)
	cfg.SMTPTimeoutSeconds = channels.NormalizeSMTPTimeoutSeconds(cfg.SMTPTimeoutSeconds)

	cfg.AliyunSecretConfigured = cfg.AliyunAccessKeySecret != ""
	cfg.TencentSecretConfigured = cfg.TencentSecretKey != ""
	cfg.SMTPPasswordConfigured = cfg.SMTPPassword != ""
	cfg.ChatOpsTokenConfigured = cfg.ChatOpsCallbackToken != ""
	cfg.FeishuVerificationConfigured = cfg.FeishuVerificationToken != ""
	cfg.FeishuEncryptConfigured = cfg.FeishuEncryptKey != ""
	cfg.DingTalkSecretConfigured = cfg.DingTalkAppSecret != ""
	cfg.FeishuAppSecretConfigured = cfg.FeishuAppSecret != ""
	cfg.WarRoomWebhookSecretConfigured = cfg.WarRoomWebhookSecret != ""
	cfg.CallbackURLs = nil
}

// GetChannelConfig loads the persisted multi-channel gateway configuration and decrypts stored secrets.
func (s *Store) GetChannelConfig(ctx context.Context) (*channels.ChannelGatewayConfig, error) {
	var rec model.ChannelConfigRecord
	err := s.db.WithContext(ctx).Order("id desc").First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultChannelGatewayConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("load channel config: %w", err)
	}

	cfg := DefaultChannelGatewayConfig()
	if rec.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(rec.ConfigJSON), cfg); err != nil {
			return nil, fmt.Errorf("decode channel config: %w", err)
		}
	}
	if err := decryptChannelSecrets(cfg); err != nil {
		return nil, err
	}
	applyChannelConfigDefaults(cfg)
	cfg.UpdatedBy = rec.UpdatedBy
	cfg.UpdatedAt = rec.UpdatedAt
	return cfg, nil
}

// UpdateChannelConfig encrypts sensitive secrets at rest (preserving existing secrets when omitted/blank) and saves config.
func (s *Store) UpdateChannelConfig(ctx context.Context, cfg *channels.ChannelGatewayConfig) error {
	if cfg == nil {
		return errors.New("channel config is nil")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.ChannelConfigRecord
		err := tx.Order("id desc").First(&existing).Error
		hasExisting := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query channel config: %w", err)
		}

		if hasExisting && existing.ConfigJSON != "" {
			prev := DefaultChannelGatewayConfig()
			if json.Unmarshal([]byte(existing.ConfigJSON), prev) == nil && decryptChannelSecrets(prev) == nil {
				curSlots := channelSecretSlots(cfg)
				prevSlots := channelSecretSlots(prev)
				for i := range curSlots {
					if *curSlots[i].val == "" {
						*curSlots[i].val = *prevSlots[i].val
					}
				}
			}
		}

		if cfg.ChatOpsCallbackToken == "" {
			cfg.ChatOpsCallbackToken = generateVoiceWebhookToken()
		}

		now := time.Now()
		cfg.UpdatedAt = now
		applyChannelConfigDefaults(cfg)

		toStore := *cfg
		toStore.AliyunSecretConfigured = false
		toStore.TencentSecretConfigured = false
		toStore.SMTPPasswordConfigured = false
		toStore.ChatOpsTokenConfigured = false
		toStore.FeishuVerificationConfigured = false
		toStore.FeishuEncryptConfigured = false
		toStore.DingTalkSecretConfigured = false
		toStore.FeishuAppSecretConfigured = false
		toStore.WarRoomWebhookSecretConfigured = false
		toStore.CallbackURLs = nil

		for _, slot := range channelSecretSlots(&toStore) {
			if *slot.val == "" {
				continue
			}
			enc, encErr := secretbox.Encrypt(*slot.val)
			if encErr != nil {
				return fmt.Errorf("encrypt %s: %w", slot.name, encErr)
			}
			*slot.val = enc
		}

		jsonBytes, err := json.Marshal(&toStore)
		if err != nil {
			return fmt.Errorf("encode channel config: %w", err)
		}

		if hasExisting {
			existing.ConfigJSON = string(jsonBytes)
			existing.UpdatedBy = cfg.UpdatedBy
			existing.UpdatedAt = now
			if err := tx.Save(&existing).Error; err != nil {
				return fmt.Errorf("save channel config: %w", err)
			}
			return nil
		}

		rec := model.ChannelConfigRecord{
			ConfigJSON: string(jsonBytes),
			UpdatedBy:  cfg.UpdatedBy,
			UpdatedAt:  now,
		}
		if err := tx.Create(&rec).Error; err != nil {
			return fmt.Errorf("create channel config: %w", err)
		}
		return nil
	})
}

// EnsureChatOpsCallbackToken guarantees a persisted cryptographic callback/signing token exists and returns it decrypted.
func (s *Store) EnsureChatOpsCallbackToken(ctx context.Context) (string, error) {
	cfg, err := s.GetChannelConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.ChatOpsCallbackToken != "" {
		return cfg.ChatOpsCallbackToken, nil
	}
	cfg.ChatOpsCallbackToken = generateVoiceWebhookToken()
	if err := s.UpdateChannelConfig(ctx, cfg); err != nil {
		return "", err
	}
	return cfg.ChatOpsCallbackToken, nil
}

// GetWarRoomByIncident fetches the persisted incident war room, if one exists.
func (s *Store) GetWarRoomByIncident(ctx context.Context, incidentID uint64) (*model.WarRoom, error) {
	if incidentID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var room model.WarRoom
	if err := s.db.WithContext(ctx).Where("incident_id = ?", incidentID).First(&room).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

// ListWarRoomsByIncidents batch-loads war rooms keyed by incident ID.
func (s *Store) ListWarRoomsByIncidents(ctx context.Context, incidentIDs []uint64) (map[uint64]*model.WarRoom, error) {
	if len(incidentIDs) == 0 {
		return map[uint64]*model.WarRoom{}, nil
	}
	var rows []*model.WarRoom
	if err := s.db.WithContext(ctx).Where("incident_id IN ?", incidentIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list war rooms: %w", err)
	}
	res := make(map[uint64]*model.WarRoom, len(rows))
	for _, r := range rows {
		if r != nil {
			res[r.IncidentID] = r
		}
	}
	return res, nil
}

// UpsertWarRoom atomically creates or returns/updates the persisted war room for an incident.
func (s *Store) UpsertWarRoom(ctx context.Context, room *model.WarRoom) (*model.WarRoom, error) {
	if room == nil || room.IncidentID == 0 {
		return nil, errors.New("invalid war room")
	}
	now := time.Now()
	var out model.WarRoom
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.WarRoom
		findErr := tx.Where("incident_id = ?", room.IncidentID).First(&existing).Error
		if findErr == nil {
			existing.Provider = room.Provider
			if room.ExternalChatID != "" {
				existing.ExternalChatID = room.ExternalChatID
			}
			if room.RoomName != "" {
				existing.RoomName = room.RoomName
			}
			if room.RoomURL != "" {
				existing.RoomURL = room.RoomURL
			}
			if room.Status != "" {
				existing.Status = room.Status
			}
			existing.UpdatedAt = now
			if err := tx.Save(&existing).Error; err != nil {
				return fmt.Errorf("update war room: %w", err)
			}
			out = existing
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query war room: %w", findErr)
		}
		if room.Status == "" {
			room.Status = "active"
		}
		if room.CreatedAt.IsZero() {
			room.CreatedAt = now
		}
		room.UpdatedAt = now
		if err := tx.Create(room).Error; err != nil {
			if isUniqueConstraintErr(err) {
				if reloadErr := tx.Where("incident_id = ?", room.IncidentID).First(&out).Error; reloadErr == nil {
					return nil
				}
			}
			return fmt.Errorf("create war room: %w", err)
		}
		out = *room
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
