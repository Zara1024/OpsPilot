package oncall

import "time"

const (
	MaintenanceActionSuppressAll   = "suppress_all"
	MaintenanceActionSuppressVoice = "suppress_voice"

	MaintenanceStatusActive    = "active"
	MaintenanceStatusCancelled = "cancelled"
)

// MaintenanceWindow represents a scheduled maintenance window that suppresses alerts or voice calls.
type MaintenanceWindow struct {
	ID              uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"column:name;size:128;not null" json:"name"`
	Description     string     `gorm:"column:description;size:255;not null;default:''" json:"description"`
	ScheduleID      uint64     `gorm:"column:schedule_id;not null;default:0;index:idx_oncall_mw_sched" json:"schedule_id"`
	MatcherJSON     string     `gorm:"column:matcher_json;type:text;not null;default:'[]'" json:"matcher_json"`
	Action          string     `gorm:"column:action;size:24;not null;default:'suppress_all'" json:"action"`
	StartsAt        time.Time  `gorm:"column:starts_at;not null;index:idx_oncall_mw_time,priority:1" json:"starts_at"`
	EndsAt          time.Time  `gorm:"column:ends_at;not null;index:idx_oncall_mw_time,priority:2" json:"ends_at"`
	Status          string     `gorm:"column:status;size:16;not null;default:'active';index:idx_oncall_mw_status" json:"status"`
	SuppressedCount uint64     `gorm:"column:suppressed_count;not null;default:0" json:"suppressed_count"`
	CreatedBy       uint64     `gorm:"column:created_by;not null;default:0" json:"created_by"`
	CancelledBy     *uint64    `gorm:"column:cancelled_by" json:"cancelled_by,omitempty"`
	CancelledAt     *time.Time `gorm:"column:cancelled_at" json:"cancelled_at,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (MaintenanceWindow) TableName() string {
	return "oncall_maintenance_windows"
}
