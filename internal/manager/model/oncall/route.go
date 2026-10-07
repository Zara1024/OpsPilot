package oncall

import (
	"time"
)

// Route represents label-based dynamic alert routing to on-call schedules.
type Route struct {
	ID                   uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TeamID               uint64    `gorm:"column:team_id;not null;default:0" json:"team_id"`
	Name                 string    `gorm:"column:name;size:128;not null" json:"name"`
	MatcherJSON          string    `gorm:"column:matcher_json;type:json;not null" json:"matcher_json"`
	Priority             int       `gorm:"column:priority;not null;default:0;index:idx_route_priority,priority:1" json:"priority"`
	ScheduleID           uint64    `gorm:"column:schedule_id;not null" json:"schedule_id"`
	EscalationPolicyID   uint64    `gorm:"column:escalation_policy_id;not null;default:0" json:"escalation_policy_id"`
	GroupWaitSeconds     int       `gorm:"column:group_wait_seconds;not null;default:30" json:"group_wait_seconds"`
	GroupIntervalSeconds int       `gorm:"column:group_interval_seconds;not null;default:300" json:"group_interval_seconds"`
	Enabled              bool      `gorm:"column:enabled;not null;default:true;index:idx_route_priority,priority:2" json:"enabled"`
	CreatedAt            time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (Route) TableName() string { return "oncall_routes" }

// EscalationPolicy represents multi-step alert escalation rules.
type EscalationPolicy struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ScheduleID uint64    `gorm:"column:schedule_id;not null;index:idx_escalation_sched,priority:1" json:"schedule_id"`
	Name       string    `gorm:"column:name;size:64;not null;default:'标准告警升级规则'" json:"name"`
	StepNumber uint8     `gorm:"column:step_number;not null;default:1;index:idx_escalation_sched,priority:2" json:"step_number"`
	WaitSeconds uint32   `gorm:"column:wait_seconds;not null;default:300" json:"wait_seconds"`
	TargetType string    `gorm:"column:target_type;size:32;not null;default:'primary_oncall'" json:"target_type"`
	TargetID   *uint64   `gorm:"column:target_id" json:"target_id,omitempty"`
	CreatedAt  time.Time `gorm:"column:created_at;not null" json:"created_at"`
}

func (EscalationPolicy) TableName() string { return "oncall_escalation_policies" }
