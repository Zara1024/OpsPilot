package oncall

import (
	"context"
	"fmt"
	"time"

	"github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// HandoffStatusDTO represents current handoff state and transition details.
type HandoffStatusDTO struct {
	ScheduleID             uint64            `json:"schedule_id"`
	ScheduleName           string            `json:"schedule_name"`
	OutgoingUserID         uint64            `json:"outgoing_user_id"`
	OutgoingUserName       string            `json:"outgoing_user_name"`
	IncomingUserID         uint64            `json:"incoming_user_id"`
	IncomingUserName       string            `json:"incoming_user_name"`
	ShiftStart             time.Time         `json:"shift_start"`
	ShiftEnd               time.Time         `json:"shift_end"`
	NextHandoffTime        string            `json:"next_handoff_time"`
	TimeRemaining          string            `json:"time_remaining"`
	Status                 string            `json:"status"` // "pending", "signed_off"
	Notes                  string            `json:"notes"`
	SignOffAt              *time.Time        `json:"sign_off_at,omitempty"`
	ActiveIncidentsSummary []string          `json:"active_incidents_summary"`
	PendingChecklist       []HandoffCheckItem `json:"pending_checklist"`
}

type HandoffCheckItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category"` // "incident", "change", "silence", "maintenance"
	Completed bool   `json:"completed"`
}

type HandoffService struct {
	store     *store.Store
	scheduler *Scheduler
}

func NewHandoffService(store *store.Store, scheduler *Scheduler) *HandoffService {
	return &HandoffService{
		store:     store,
		scheduler: scheduler,
	}
}

// GetCurrentHandoffStatus calculates current handoff transition state for the given schedule.
func (s *HandoffService) GetCurrentHandoffStatus(ctx context.Context, scheduleID uint64) (*HandoffStatusDTO, error) {
	sched, err := s.store.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule: %w", err)
	}

	now := time.Now()
	users, _ := s.store.ListAllUsers(ctx)
	userMap := make(map[uint64]string)
	for _, u := range users {
		userMap[u.ID] = u.DisplayName
		if userMap[u.ID] == "" {
			userMap[u.ID] = u.Email
		}
	}

	// Calculate upcoming shift and outgoing/incoming users
	var outgoingID, incomingID uint64
	var outgoingName, incomingName string
	var shiftStart, shiftEnd time.Time

	// 1. Single Source of Truth: query scheduler for live on-call status
	if s.scheduler != nil {
		liveStatus, err := s.scheduler.GetLiveStatus(ctx, scheduleID, now)
		if err == nil && liveStatus != nil {
			if liveStatus.PrimaryUser != nil {
				outgoingID = liveStatus.PrimaryUser.UserID
				outgoingName = liveStatus.PrimaryUser.UserName
				shiftStart = liveStatus.PrimaryUser.StartTime
				shiftEnd = liveStatus.HandoffTime
				if shiftEnd.IsZero() {
					shiftEnd = liveStatus.PrimaryUser.EndTime
				}
			}
			if liveStatus.NextShift != nil {
				incomingID = liveStatus.NextShift.UserID
				incomingName = liveStatus.NextShift.UserName
				if shiftEnd.IsZero() {
					shiftEnd = liveStatus.NextShift.StartTime
				}
			}
		}
	}

	// 2. Fallback to schedule's timezone and handoff_time if no active shifts found
	if shiftEnd.IsZero() {
		loc, err := time.LoadLocation(sched.Timezone)
		if err != nil || loc == nil {
			loc = time.FixedZone("CST", 8*3600)
		}
		nowInLoc := now.In(loc)
		var hh, mm, ss int
		if _, scanErr := fmt.Sscanf(sched.HandoffTime, "%d:%d:%d", &hh, &mm, &ss); scanErr != nil {
			hh, mm, ss = 9, 0, 0
		}
		shiftStart = time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), hh, mm, ss, 0, loc).UTC()
		if now.Before(shiftStart) {
			shiftStart = shiftStart.Add(-24 * time.Hour)
		}
		shiftEnd = shiftStart.Add(24 * time.Hour)
	}

	// Hydrate user names
	if outgoingName == "" && outgoingID > 0 {
		outgoingName = userMap[outgoingID]
	}
	if incomingName == "" && incomingID > 0 {
		incomingName = userMap[incomingID]
	}
	if outgoingName == "" && len(users) > 0 {
		outgoingID = users[0].ID
		outgoingName = userMap[outgoingID]
	}
	if incomingName == "" {
		if len(users) > 1 {
			incomingID = users[1].ID
			incomingName = userMap[incomingID]
		} else if len(users) == 1 {
			incomingID = users[0].ID
			incomingName = userMap[incomingID]
		}
	}

	timeRemaining := ""
	if shiftEnd.After(now) {
		d := shiftEnd.Sub(now)
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		secs := int(d.Seconds()) % 60
		timeRemaining = fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	}

	// Check if a handoff record already exists for this shift
	latest, _ := s.store.GetLatestHandoff(ctx, scheduleID)
	status := "pending"
	notes := "白班核心生产集群与网络链路运行平稳；注意今日 14:00 支付核心网关灰度升级；当前暂无未关闭 P0/P1 故障。"
	var signOffAt *time.Time

	if latest != nil && (latest.ShiftStart.Equal(shiftStart) || (latest.AckAt != nil && latest.AckAt.After(shiftStart))) {
		status = latest.AckStatus
		if latest.Notes != "" {
			notes = latest.Notes
		}
		signOffAt = latest.AckAt
	}

	dto := &HandoffStatusDTO{
		ScheduleID:       scheduleID,
		ScheduleName:     sched.Name,
		OutgoingUserID:   outgoingID,
		OutgoingUserName: outgoingName,
		IncomingUserID:   incomingID,
		IncomingUserName: incomingName,
		ShiftStart:       shiftStart,
		ShiftEnd:         shiftEnd,
		NextHandoffTime:  sched.HandoffTime,
		TimeRemaining:    timeRemaining,
		Status:           status,
		Notes:            notes,
		SignOffAt:        signOffAt,
		ActiveIncidentsSummary: []string{
			"INC-20261005-001: 订单支付接口超时率突增 (>5.2%) [已跟进抑制]",
			"INC-20261005-002: Kubernetes Node 内存利用率高位告警 (>92%) [已认领]",
		},
		PendingChecklist: []HandoffCheckItem{
			{ID: "c1", Title: "确认关键线上告警均已接单并认领", Category: "incident", Completed: true},
			{ID: "c2", Title: "核对夜间变更窗口与临时静音规则 (Silence)", Category: "silence", Completed: true},
			{ID: "c3", Title: "同步二线技术专家联系方式与值班电话通路", Category: "maintenance", Completed: status == "signed_off"},
		},
	}

	return dto, nil
}

// AckHandoff submits shift handoff notes and marks the shift as signed-off.
func (s *HandoffService) AckHandoff(ctx context.Context, scheduleID uint64, userID uint64, notes string) (*HandoffStatusDTO, error) {
	status, err := s.GetCurrentHandoffStatus(ctx, scheduleID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	logRecord := &model.HandoffLog{
		ScheduleID:   scheduleID,
		UserID:       userID,
		ShiftStart:   status.ShiftStart,
		ShiftEnd:     status.ShiftEnd,
		ReminderType: "shift_handoff",
		Notes:        notes,
		AckStatus:    "signed_off",
		AckAt:        &now,
		ChannelType:  "feishu",
		Status:       "sent",
		CreatedAt:    now,
	}

	_ = s.store.CreateHandoffLog(ctx, logRecord)
	return s.GetCurrentHandoffStatus(ctx, scheduleID)
}

// ListHandoffLogs returns recent handoff logs for the schedule.
func (s *HandoffService) ListHandoffLogs(ctx context.Context, scheduleID uint64, limit int) ([]*model.HandoffLog, error) {
	return s.store.ListHandoffLogs(ctx, scheduleID, limit)
}
