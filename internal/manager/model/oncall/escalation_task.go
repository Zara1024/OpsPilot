package oncall

import (
	"encoding/json"
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

// EscalationTask tracks a persistent, lease-locked escalation state machine for one or a grouped batch of firing alert incidents.
type EscalationTask struct {
	ID                    uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	IncidentID            uint64     `gorm:"column:incident_id;not null;index:idx_oncall_task_incident" json:"incident_id"`
	GroupKey              string     `gorm:"column:group_key;size:128;not null;default:'';index:idx_oncall_task_group" json:"group_key"`
	MergedIncidentIDsJSON string     `gorm:"column:merged_incident_ids_json;type:text" json:"merged_incident_ids_json"`
	AlertCount            int        `gorm:"column:alert_count;not null;default:1" json:"alert_count"`
	GroupIntervalSeconds  int        `gorm:"column:group_interval_seconds;not null;default:0" json:"group_interval_seconds"`
	LastDispatchedAt      *time.Time `gorm:"column:last_dispatched_at" json:"last_dispatched_at,omitempty"`
	RuleKey               string     `gorm:"column:rule_key;size:128;not null;default:''" json:"rule_key"`
	Title                 string     `gorm:"column:title;size:512;not null;default:''" json:"title"`
	Severity              string     `gorm:"column:severity;size:32;not null;default:''" json:"severity"`
	Urgency               string     `gorm:"column:urgency;size:16;not null;default:'high'" json:"urgency"`
	ScheduleID            uint64     `gorm:"column:schedule_id;not null;default:0;index:idx_oncall_task_schedule" json:"schedule_id"`
	RouteID               uint64     `gorm:"column:route_id;not null;default:0" json:"route_id"`
	CurrentStep           uint8      `gorm:"column:current_step;not null;default:1" json:"current_step"`
	LadderStep            uint8      `gorm:"column:ladder_step;not null;default:1" json:"ladder_step"`
	StepStartedAt         time.Time  `gorm:"column:step_started_at;not null" json:"step_started_at"`
	AssigneeUserID        uint64     `gorm:"column:assignee_user_id;not null;default:0" json:"assignee_user_id"`
	PinnedAssigneeID      uint64     `gorm:"column:pinned_assignee_id;not null;default:0" json:"pinned_assignee_id"`
	Status                string     `gorm:"column:status;size:24;not null;default:'pending';index:idx_oncall_task_due,priority:1" json:"status"`
	NextRunAt             time.Time  `gorm:"column:next_run_at;not null;index:idx_oncall_task_due,priority:2" json:"next_run_at"`
	LockedUntil           *time.Time `gorm:"column:locked_until" json:"locked_until,omitempty"`
	AttemptCount          int        `gorm:"column:attempt_count;not null;default:0" json:"attempt_count"`
	LastError             string     `gorm:"column:last_error;size:512;not null;default:''" json:"last_error"`
	CreatedAt             time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (EscalationTask) TableName() string {
	return "oncall_escalation_tasks"
}

// ParseMergedIncidentIDs returns deduplicated incident IDs covered by the task, preserving insertion order.
func ParseMergedIncidentIDs(task *EscalationTask) []uint64 {
	if task == nil {
		return nil
	}
	seen := make(map[uint64]struct{})
	var out []uint64
	if task.MergedIncidentIDsJSON != "" {
		var parsed []uint64
		if err := json.Unmarshal([]byte(task.MergedIncidentIDsJSON), &parsed); err == nil {
			for _, id := range parsed {
				if id == 0 {
					continue
				}
				if _, exists := seen[id]; !exists {
					seen[id] = struct{}{}
					out = append(out, id)
				}
			}
		}
	}
	if task.IncidentID > 0 {
		if _, exists := seen[task.IncidentID]; !exists {
			out = append([]uint64{task.IncidentID}, out...)
		}
	}
	return out
}

// EncodeMergedIncidentIDs deduplicates and marshals incident IDs to JSON.
func EncodeMergedIncidentIDs(ids []uint64) string {
	seen := make(map[uint64]struct{}, len(ids))
	clean := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return ""
	}
	return string(b)
}

// ContainsIncidentID reports whether the task covers incidentID either directly or in MergedIncidentIDsJSON.
func ContainsIncidentID(task *EscalationTask, incidentID uint64) bool {
	if task == nil || incidentID == 0 {
		return false
	}
	for _, id := range ParseMergedIncidentIDs(task) {
		if id == incidentID {
			return true
		}
	}
	return false
}

// NotificationLog audits every On-Call notification and voice call attempt plus provider callback state.
type NotificationLog struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TaskID         uint64    `gorm:"column:task_id;not null;default:0;index:idx_oncall_nlog_task" json:"task_id"`
	IncidentID     uint64    `gorm:"column:incident_id;not null;default:0;index:idx_oncall_nlog_incident" json:"incident_id"`
	AlertCount     int       `gorm:"column:alert_count;not null;default:1" json:"alert_count"`
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
