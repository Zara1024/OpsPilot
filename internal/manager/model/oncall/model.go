// Package oncall holds persistence entities and types for the manager/oncall sub-domain.
package oncall

import (
	"time"

	"gorm.io/gorm"
)

const (
	TierPrimary   uint8 = 1
	TierSecondary uint8 = 2
)

const (
	RotationTypeDaily  = "daily"
	RotationTypeWeekly = "weekly"
	RotationTypeCustom = "custom"
)

const (
	TimeRestrictionNone      = "none"
	TimeRestrictionTimeOfDay = "time_of_day"
	TimeRestrictionWeekday   = "weekday"
)

const (
	OverrideTypeOverride = "override"
	OverrideTypeSwap     = "swap"
)

const (
	OverrideStatusPending   = "pending"
	OverrideStatusApproved  = "approved"
	OverrideStatusRejected  = "rejected"
	OverrideStatusCancelled = "cancelled"
)

// Schedule represents an On-Call schedule plan.
type Schedule struct {
	ID                   uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TeamID               uint64         `gorm:"column:team_id;not null;default:0;index:idx_oncall_sched_team" json:"team_id"`
	Name                 string         `gorm:"column:name;size:128;not null" json:"name"`
	Description          string         `gorm:"column:description;size:255;not null;default:''" json:"description"`
	Timezone             string         `gorm:"column:timezone;size:64;not null;default:'Asia/Shanghai'" json:"timezone"`
	HandoffTime          string         `gorm:"column:handoff_time;size:16;not null;default:'09:00:00'" json:"handoff_time"`
	ReminderAdvanceHours uint32         `gorm:"column:reminder_advance_hours;not null;default:3" json:"reminder_advance_hours"`
	RequireSwapApproval  bool           `gorm:"column:require_swap_approval;not null;default:false" json:"require_swap_approval"`
	Enabled              bool           `gorm:"column:enabled;not null;default:true;index:idx_oncall_sched_enabled" json:"enabled"`
	CreatedBy            uint64         `gorm:"column:created_by;not null;default:0" json:"created_by"`
	CreatedAt            time.Time      `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at;not null" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`

	Rotations []Rotation `gorm:"foreignKey:ScheduleID" json:"rotations,omitempty"`
}

func (Schedule) TableName() string { return "oncall_schedules" }

// Rotation represents a rotation tier layer (e.g. Primary or Secondary) within a Schedule.
type Rotation struct {
	ID                   uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ScheduleID           uint64    `gorm:"column:schedule_id;not null;index:idx_rotation_sched_tier,priority:1" json:"schedule_id"`
	Name                 string    `gorm:"column:name;size:64;not null;default:'一线值班轮转'" json:"name"`
	Tier                 uint8     `gorm:"column:tier;not null;default:1;index:idx_rotation_sched_tier,priority:2" json:"tier"`
	RotationType         string    `gorm:"column:rotation_type;size:16;not null;default:'daily'" json:"rotation_type"`
	ShiftLengthSeconds   uint32    `gorm:"column:shift_length_seconds;not null;default:86400" json:"shift_length_seconds"`
	UsersJSON            string    `gorm:"column:users_json;type:json;not null" json:"users_json"`
	EffectiveFrom        time.Time `gorm:"column:effective_from;not null" json:"effective_from"`
	TimeRestrictionType  string    `gorm:"column:time_restriction_type;size:16;not null;default:'none'" json:"time_restriction_type"`
	RestrictionStartTime string    `gorm:"column:restriction_start_time;size:16;not null;default:''" json:"restriction_start_time"`
	RestrictionEndTime   string    `gorm:"column:restriction_end_time;size:16;not null;default:''" json:"restriction_end_time"`
	CreatedAt            time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (Rotation) TableName() string { return "oncall_rotations" }

// Override represents a temporary replacement (override) or point-to-point shift swap.
type Override struct {
	ID               uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ScheduleID       uint64    `gorm:"column:schedule_id;not null;index:idx_override_window,priority:1" json:"schedule_id"`
	RotationID       uint64    `gorm:"column:rotation_id;not null;default:0" json:"rotation_id"`
	Type             string    `gorm:"column:type;size:16;not null;default:'override'" json:"type"`
	OriginalUserID   uint64    `gorm:"column:original_user_id;not null" json:"original_user_id"`
	SubstituteUserID uint64    `gorm:"column:substitute_user_id;not null" json:"substitute_user_id"`
	StartTime        time.Time `gorm:"column:start_time;not null;index:idx_override_window,priority:2" json:"start_time"`
	EndTime          time.Time `gorm:"column:end_time;not null;index:idx_override_window,priority:3" json:"end_time"`
	Reason           string    `gorm:"column:reason;size:255;not null;default:''" json:"reason"`
	Status           string    `gorm:"column:status;size:16;not null;default:'approved';index:idx_override_window,priority:4" json:"status"`
	ApprovedBy       *uint64   `gorm:"column:approved_by" json:"approved_by,omitempty"`
	CreatedBy        uint64    `gorm:"column:created_by;not null;default:0" json:"created_by"`
	CreatedAt        time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (Override) TableName() string { return "oncall_overrides" }
