package oncall

import (
	"time"
)

const (
	TaskStatusPending   = "pending"
	TaskStatusAcked     = "acked"
	TaskStatusCancelled = "cancelled"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
)

const (
	NotifyStatusSent          = "sent"
	NotifyStatusCalling       = "calling"
	NotifyStatusAnswered      = "answered"
	NotifyStatusDTMFConfirmed = "dtmf_confirmed"
	NotifyStatusThrottled     = "throttled"
	NotifyStatusFailed        = "failed"
)

// VoiceConfigRecord stores encrypted multi-cloud voice gateway settings and global on-call dispatch toggle.
type VoiceConfigRecord struct {
	ID              uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Provider        string    `gorm:"column:provider;size:32;not null;default:'mock'" json:"provider"`
	DispatchEnabled bool      `gorm:"column:dispatch_enabled;not null;default:true" json:"dispatch_enabled"`
	ConfigJSON      string    `gorm:"column:config_json;type:text" json:"config_json"`
	UpdatedBy       uint64    `gorm:"column:updated_by;not null;default:0" json:"updated_by"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (VoiceConfigRecord) TableName() string {
	return "oncall_voice_configs"
}

// EscalationTask tracks a persistent, lease-locked escalation state machine for a firing alert incident.
type EscalationTask struct {
	ID             uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	IncidentID     uint64     `gorm:"column:incident_id;not null;index:idx_oncall_task_incident" json:"incident_id"`
	RuleKey        string     `gorm:"column:rule_key;size:128;not null;default:''" json:"rule_key"`
	Title          string     `gorm:"column:title;size:512;not null;default:''" json:"title"`
	Severity       string     `gorm:"column:severity;size:32;not null;default:''" json:"severity"`
	Urgency        string     `gorm:"column:urgency;size:16;not null;default:'high'" json:"urgency"`
	ScheduleID     uint64     `gorm:"column:schedule_id;not null;default:0;index:idx_oncall_task_schedule" json:"schedule_id"`
	RouteID        uint64     `gorm:"column:route_id;not null;default:0" json:"route_id"`
	CurrentStep    uint8      `gorm:"column:current_step;not null;default:1" json:"current_step"`
	LadderStep     uint8      `gorm:"column:ladder_step;not null;default:1" json:"ladder_step"`
	StepStartedAt  time.Time  `gorm:"column:step_started_at;not null" json:"step_started_at"`
	AssigneeUserID uint64     `gorm:"column:assignee_user_id;not null;default:0" json:"assignee_user_id"`
	Status         string     `gorm:"column:status;size:24;not null;default:'pending';index:idx_oncall_task_due,priority:1" json:"status"`
	NextRunAt      time.Time  `gorm:"column:next_run_at;not null;index:idx_oncall_task_due,priority:2" json:"next_run_at"`
	LockedUntil    *time.Time `gorm:"column:locked_until" json:"locked_until,omitempty"`
	AttemptCount   int        `gorm:"column:attempt_count;not null;default:0" json:"attempt_count"`
	LastError      string     `gorm:"column:last_error;size:512;not null;default:''" json:"last_error"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (EscalationTask) TableName() string {
	return "oncall_escalation_tasks"
}

// NotificationLog audits every On-Call notification and voice call attempt plus provider callback state.
type NotificationLog struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TaskID         uint64    `gorm:"column:task_id;not null;default:0;index:idx_oncall_nlog_task" json:"task_id"`
	IncidentID     uint64    `gorm:"column:incident_id;not null;default:0;index:idx_oncall_nlog_incident" json:"incident_id"`
	ScheduleID     uint64    `gorm:"column:schedule_id;not null;default:0" json:"schedule_id"`
	EscalationStep uint8     `gorm:"column:escalation_step;not null;default:1" json:"escalation_step"`
	UserID         uint64    `gorm:"column:user_id;not null;default:0;index:idx_oncall_nlog_user" json:"user_id"`
	UserName       string    `gorm:"column:user_name;size:128;not null;default:''" json:"user_name"`
	Target         string    `gorm:"column:target;size:128;not null;default:''" json:"target"`
	Channel        string    `gorm:"column:channel;size:32;not null;index:idx_oncall_nlog_channel" json:"channel"`
	Provider       string    `gorm:"column:provider;size:32;not null;default:''" json:"provider"`
	CallID         string    `gorm:"column:call_id;size:64;not null;default:'';index:idx_oncall_nlog_call_id" json:"call_id"`
	OutCallID      string    `gorm:"column:out_call_id;size:128;not null;default:'';index:idx_oncall_nlog_out_call_id" json:"out_call_id"`
	Status         string    `gorm:"column:status;size:32;not null;index:idx_oncall_nlog_status" json:"status"`
	DTMFCode       string    `gorm:"column:dtmf_code;size:8;not null;default:''" json:"dtmf_code"`
	DurationSecs   int       `gorm:"column:duration_secs;not null;default:0" json:"duration_secs"`
	Message        string    `gorm:"column:message;size:512;not null;default:''" json:"message"`
	IsNight        bool      `gorm:"column:is_night;not null;default:false;index:idx_oncall_nlog_night" json:"is_night"`
	CreatedAt      time.Time `gorm:"column:created_at;not null;index:idx_oncall_nlog_created" json:"created_at"`
}

func (NotificationLog) TableName() string {
	return "oncall_notification_logs"
}
