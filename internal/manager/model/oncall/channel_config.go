package oncall

import "time"

// ChannelConfigRecord stores encrypted multi-channel notification gateway credentials (SMS, SMTP Email, ChatOps & War Room).
type ChannelConfigRecord struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ConfigJSON string    `gorm:"column:config_json;type:text" json:"config_json"`
	UpdatedBy  uint64    `gorm:"column:updated_by;not null;default:0" json:"updated_by"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (ChannelConfigRecord) TableName() string {
	return "oncall_channel_configs"
}

// WarRoom tracks a persistent incident coordination room created in Feishu, external webhook, or console fallback.
type WarRoom struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	IncidentID     uint64    `gorm:"column:incident_id;not null;uniqueIndex:uk_warroom_incident" json:"incident_id"`
	Provider       string    `gorm:"column:provider;size:32;not null;default:'console'" json:"provider"`
	ExternalChatID string    `gorm:"column:external_chat_id;size:128;not null;default:''" json:"external_chat_id"`
	RoomName       string    `gorm:"column:room_name;size:255;not null;default:''" json:"room_name"`
	RoomURL        string    `gorm:"column:room_url;size:512;not null;default:''" json:"room_url"`
	CreatedBy      uint64    `gorm:"column:created_by;not null;default:0" json:"created_by"`
	Status         string    `gorm:"column:status;size:16;not null;default:'active'" json:"status"`
	CreatedAt      time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (WarRoom) TableName() string {
	return "oncall_war_rooms"
}
