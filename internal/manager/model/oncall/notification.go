package oncall

import (
	"time"
)

// UserNotificationRule represents personal multi-channel notification ladder.
type UserNotificationRule struct {
	ID           uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID       uint64    `gorm:"column:user_id;not null;index:idx_user_urgency,priority:1" json:"user_id"`
	Urgency      string    `gorm:"column:urgency;size:16;not null;default:'high';index:idx_user_urgency,priority:2" json:"urgency"`
	StepNumber   uint8     `gorm:"column:step_number;not null;default:1;index:idx_user_urgency,priority:3" json:"step_number"`
	DelayMinutes uint32    `gorm:"column:delay_minutes;not null;default:0" json:"delay_minutes"`
	Channel      string    `gorm:"column:channel;size:32;not null;default:'im_dm'" json:"channel"`
	Enabled      bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt    time.Time `gorm:"column:created_at;not null" json:"created_at"`
}

func (UserNotificationRule) TableName() string { return "oncall_user_notification_rules" }

// UserCalendarToken stores the secure token used for external RFC 5545 iCal/Webcal calendar subscriptions.
type UserCalendarToken struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"column:user_id;not null;uniqueIndex:uk_user_token" json:"user_id"`
	Token     string    `gorm:"column:token;size:64;not null;uniqueIndex:uk_token_val" json:"token"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (UserCalendarToken) TableName() string { return "oncall_user_calendar_tokens" }

// HandoffLog records pre-shift reminders, handoff notes, and acknowledgment audit trails.
type HandoffLog struct {
	ID             uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ScheduleID     uint64     `gorm:"column:schedule_id;not null;uniqueIndex:uk_sched_user_shift,priority:1" json:"schedule_id"`
	UserID         uint64     `gorm:"column:user_id;not null;uniqueIndex:uk_sched_user_shift,priority:2" json:"user_id"`
	PreviousUserID *uint64    `gorm:"column:previous_user_id" json:"previous_user_id,omitempty"`
	ShiftStart     time.Time  `gorm:"column:shift_start;not null;uniqueIndex:uk_sched_user_shift,priority:3" json:"shift_start"`
	ShiftEnd       time.Time  `gorm:"column:shift_end;not null" json:"shift_end"`
	ReminderType   string     `gorm:"column:reminder_type;size:32;not null;default:'3h_pre_shift';uniqueIndex:uk_sched_user_shift,priority:4" json:"reminder_type"`
	Notes          string     `gorm:"column:notes;type:text" json:"notes"`
	AckStatus      string     `gorm:"column:ack_status;size:16;not null;default:'pending'" json:"ack_status"`
	AckAt          *time.Time `gorm:"column:ack_at" json:"ack_at,omitempty"`
	ChannelType    string     `gorm:"column:channel_type;size:32;not null;default:'feishu'" json:"channel_type"`
	Status         string     `gorm:"column:status;size:16;not null;default:'sent'" json:"status"`
	ErrorMessage   string     `gorm:"column:error_message;size:255;not null;default:''" json:"error_message"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null" json:"created_at"`
}

func (HandoffLog) TableName() string { return "oncall_handoff_logs" }
