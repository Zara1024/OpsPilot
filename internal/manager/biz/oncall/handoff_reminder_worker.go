package oncall

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

const defaultHandoffReminderTick = 5 * time.Minute

// HandoffReminderWorkerOpts configures dependencies and timing for HandoffReminderWorker.
type HandoffReminderWorkerOpts struct {
	Store        *store.Store
	Scheduler    *Scheduler
	Deliverer    MessageDeliverer
	Metrics      *Metrics
	Log          *slog.Logger
	TickInterval time.Duration
	Now          func() time.Time
}

// HandoffReminderWorker periodically dispatches pre-shift handoff reminders with DB-backed multi-replica dedup.
type HandoffReminderWorker struct {
	store        *store.Store
	scheduler    *Scheduler
	deliverer    MessageDeliverer
	metrics      *Metrics
	log          *slog.Logger
	tickInterval time.Duration
	now          func() time.Time
}

// NewHandoffReminderWorker creates a pre-shift handoff reminder worker with safe defaults.
func NewHandoffReminderWorker(opts HandoffReminderWorkerOpts) *HandoffReminderWorker {
	tick := opts.TickInterval
	if tick <= 0 {
		tick = defaultHandoffReminderTick
	}
	sched := opts.Scheduler
	if sched == nil && opts.Store != nil {
		sched = NewScheduler(opts.Store)
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &HandoffReminderWorker{
		store:        opts.Store,
		scheduler:    sched,
		deliverer:    opts.Deliverer,
		metrics:      opts.Metrics,
		log:          log,
		tickInterval: tick,
		now:          nowFn,
	}
}

// Loop evaluates enabled schedules once immediately and then on every tick until ctx is cancelled.
func (w *HandoffReminderWorker) Loop(ctx context.Context) error {
	if w == nil || w.store == nil {
		return nil
	}
	w.ProcessOnce(ctx)

	ticker := time.NewTicker(w.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce checks all enabled schedules and dispatches idempotent pre-shift reminders when within window.
func (w *HandoffReminderWorker) ProcessOnce(ctx context.Context) int {
	if w == nil || w.store == nil || w.scheduler == nil {
		return 0
	}
	scheds, err := w.store.ListSchedules(ctx, 0, true)
	if err != nil {
		w.log.Warn("list enabled schedules for handoff reminder failed", slog.Any("err", err))
		return 0
	}

	now := w.now().UTC()
	triggered := 0
	for _, sched := range scheds {
		if sched == nil || !sched.Enabled || sched.ReminderAdvanceHours == 0 {
			continue
		}
		incoming, outgoing, err := w.resolveNextShift(ctx, sched, now, false)
		if err != nil {
			w.log.Warn("resolve upcoming shift for handoff reminder failed",
				slog.Uint64("schedule_id", sched.ID), slog.Any("err", err))
			continue
		}
		if incoming == nil || incoming.UserID == 0 {
			continue
		}

		advanceWindow := time.Duration(sched.ReminderAdvanceHours) * time.Hour
		remaining := incoming.StartTime.Sub(now)
		if remaining <= 0 || remaining > advanceWindow {
			continue
		}

		_, attempted, err := w.SendShiftReminder(ctx, sched, incoming, outgoing, now, false)
		if err != nil {
			w.log.Warn("send pre-shift handoff reminder failed",
				slog.Uint64("schedule_id", sched.ID), slog.Uint64("user_id", incoming.UserID), slog.Any("err", err))
			continue
		}
		if attempted {
			triggered++
		}
	}
	return triggered
}

// RemindNow sends an immediate handoff reminder for the upcoming shift of scheduleID.
func (w *HandoffReminderWorker) RemindNow(ctx context.Context, scheduleID uint64) (*model.HandoffLog, error) {
	if w == nil || w.store == nil || w.scheduler == nil {
		return nil, errors.New("handoff reminder worker is not initialized")
	}
	if scheduleID == 0 {
		return nil, fmt.Errorf("%w: schedule_id is required", errs.ErrInvalid)
	}
	sched, err := w.store.GetSchedule(ctx, scheduleID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, fmt.Errorf("load schedule %d: %w", scheduleID, err)
	}

	now := w.now().UTC()
	incoming, outgoing, err := w.resolveNextShift(ctx, sched, now, true)
	if err != nil {
		return nil, err
	}
	if incoming == nil || incoming.UserID == 0 {
		return nil, fmt.Errorf("%w: 当前排班暂无即将接班的一线值班工程师", errs.ErrInvalid)
	}

	entry, _, err := w.SendShiftReminder(ctx, sched, incoming, outgoing, now, true)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// SendShiftReminder claims/upserts the handoff reminder record, delivers notification, and persists outcome.
func (w *HandoffReminderWorker) SendShiftReminder(
	ctx context.Context,
	sched *model.Schedule,
	incoming *ShiftSlot,
	outgoing *ShiftSlot,
	now time.Time,
	manual bool,
) (*model.HandoffLog, bool, error) {
	if sched == nil || incoming == nil || incoming.UserID == 0 {
		return nil, false, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	shiftStart := incoming.StartTime.UTC().Truncate(time.Second)
	shiftEnd := incoming.EndTime.UTC().Truncate(time.Second)
	if !shiftEnd.After(shiftStart) {
		shiftEnd = shiftStart.Add(24 * time.Hour)
	}

	targetUser, incomingName, outgoingName := w.hydrateUsers(ctx, incoming, outgoing)
	subject, body, summary := w.buildReminderMessage(ctx, sched, shiftStart, shiftEnd, incomingName, outgoingName, manual)

	var prevUserID *uint64
	if outgoing != nil && outgoing.UserID > 0 && outgoing.UserID != incoming.UserID {
		uid := outgoing.UserID
		prevUserID = &uid
	}

	var entry *model.HandoffLog
	isNewManual := false
	if !manual {
		entry = &model.HandoffLog{
			ScheduleID:     sched.ID,
			UserID:         incoming.UserID,
			PreviousUserID: prevUserID,
			ShiftStart:     shiftStart,
			ShiftEnd:       shiftEnd,
			ReminderType:   "pre_shift_reminder",
			Notes:          summary,
			AckStatus:      "pending",
			ChannelType:    "unconfigured",
			Status:         "pending",
			CreatedAt:      now,
		}
		claimed, err := w.store.TryClaimHandoffReminder(ctx, entry)
		if err != nil {
			return nil, false, err
		}
		if !claimed {
			return nil, false, nil
		}
	} else {
		existing, findErr := w.store.FindHandoffLogByUniqueKey(ctx, sched.ID, incoming.UserID, shiftStart, "manual_reminder")
		if findErr == nil && existing != nil {
			entry = existing
			entry.PreviousUserID = prevUserID
			entry.ShiftEnd = shiftEnd
			entry.Notes = summary
			entry.ErrorMessage = ""
			entry.Status = "pending"
		} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, false, fmt.Errorf("lookup manual reminder log: %w", findErr)
		} else {
			isNewManual = true
			entry = &model.HandoffLog{
				ScheduleID:     sched.ID,
				UserID:         incoming.UserID,
				PreviousUserID: prevUserID,
				ShiftStart:     shiftStart,
				ShiftEnd:       shiftEnd,
				ReminderType:   "manual_reminder",
				Notes:          summary,
				AckStatus:      "pending",
				ChannelType:    "unconfigured",
				Status:         "pending",
				CreatedAt:      now,
			}
		}
	}

	var provider string
	var deliverErr error
	if w.deliverer != nil {
		labels := map[string]string{
			"schedule_id":   strconv.FormatUint(sched.ID, 10),
			"reminder_type": entry.ReminderType,
		}
		provider, deliverErr = w.deliverer.Deliver(ctx, "im_dm", targetUser, subject, body, "info", labels)
	} else {
		provider = "unconfigured"
		deliverErr = errors.New("no notification deliverer configured")
	}

	if deliverErr != nil {
		if provider == "" {
			provider = "unconfigured"
		}
		entry.ChannelType = truncateReminderString(provider, 32)
		entry.Status = "failed"
		entry.ErrorMessage = truncateReminderString(deliverErr.Error(), 250)
	} else {
		if provider == "" {
			provider = "feishu"
		}
		entry.ChannelType = truncateReminderString(provider, 32)
		entry.Status = "sent"
		entry.ErrorMessage = ""
	}

	if !manual || !isNewManual {
		if err := w.store.UpdateHandoffLog(ctx, entry); err != nil {
			return nil, true, fmt.Errorf("update handoff reminder log: %w", err)
		}
	} else {
		if err := w.store.CreateHandoffLog(ctx, entry); err != nil {
			return nil, true, fmt.Errorf("create manual handoff reminder log: %w", err)
		}
	}

	w.metrics.incNotification("handoff_reminder", entry.ChannelType, entry.Status)
	return entry, true, nil
}

func (w *HandoffReminderWorker) resolveNextShift(ctx context.Context, sched *model.Schedule, now time.Time, extended bool) (*ShiftSlot, *ShiftSlot, error) {
	live, err := w.scheduler.GetLiveStatus(ctx, sched.ID, now)
	if err != nil {
		return nil, nil, fmt.Errorf("get live schedule status: %w", err)
	}
	var outgoing *ShiftSlot
	var nextShift *ShiftSlot
	if live != nil {
		outgoing = live.PrimaryUser
		nextShift = live.NextShift
	}
	if nextShift != nil {
		return nextShift, outgoing, nil
	}

	lookahead := time.Duration(sched.ReminderAdvanceHours+24) * time.Hour
	if extended || lookahead < 7*24*time.Hour {
		if extended {
			lookahead = 31 * 24 * time.Hour
		} else {
			lookahead = time.Duration(sched.ReminderAdvanceHours+24) * time.Hour
		}
	}
	if lookahead <= 48*time.Hour && !extended {
		return nil, outgoing, nil
	}

	shifts, err := w.scheduler.ComputeScheduleShifts(ctx, sched.ID, now, now.Add(lookahead))
	if err != nil {
		return nil, outgoing, fmt.Errorf("compute upcoming shifts: %w", err)
	}
	for _, slot := range shifts {
		if slot != nil && slot.Tier == model.TierPrimary && slot.StartTime.After(now) {
			nextShift = slot
			break
		}
	}
	return nextShift, outgoing, nil
}

func (w *HandoffReminderWorker) hydrateUsers(ctx context.Context, incoming, outgoing *ShiftSlot) (*store.UserSummary, string, string) {
	var ids []uint64
	if incoming != nil && incoming.UserID > 0 {
		ids = append(ids, incoming.UserID)
	}
	if outgoing != nil && outgoing.UserID > 0 && outgoing.UserID != incoming.UserID {
		ids = append(ids, outgoing.UserID)
	}
	usersMap, _ := w.store.GetUsersByIDs(ctx, ids)

	var targetUser *store.UserSummary
	incomingName := fmt.Sprintf("User#%d", incoming.UserID)
	if u, ok := usersMap[incoming.UserID]; ok && u != nil {
		targetUser = u
		if u.DisplayName != "" {
			incomingName = u.DisplayName
		} else if u.Email != "" {
			incomingName = u.Email
		}
	} else {
		if incoming.UserName != "" {
			incomingName = incoming.UserName
		}
		targetUser = &store.UserSummary{
			ID:          incoming.UserID,
			DisplayName: incomingName,
			Email:       incoming.UserEmail,
			Phone:       incoming.UserPhone,
		}
	}

	outgoingName := ""
	if outgoing != nil && outgoing.UserID > 0 {
		outgoingName = fmt.Sprintf("User#%d", outgoing.UserID)
		if u, ok := usersMap[outgoing.UserID]; ok && u != nil {
			if u.DisplayName != "" {
				outgoingName = u.DisplayName
			} else if u.Email != "" {
				outgoingName = u.Email
			}
		} else if outgoing.UserName != "" {
			outgoingName = outgoing.UserName
		}
	}
	return targetUser, incomingName, outgoingName
}

func (w *HandoffReminderWorker) buildReminderMessage(
	ctx context.Context,
	sched *model.Schedule,
	shiftStart, shiftEnd time.Time,
	incomingName, outgoingName string,
	manual bool,
) (string, string, string) {
	loc, err := time.LoadLocation(sched.Timezone)
	if err != nil || loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	startLocal := shiftStart.In(loc)
	endLocal := shiftEnd.In(loc)

	prefix := "值班接班提醒"
	if manual {
		prefix = "手动接班提醒"
	}
	subject := fmt.Sprintf("[%s] 排班「%s」将于 %s 交接给 %s", prefix, sched.Name, startLocal.Format("01-02 15:04"), incomingName)

	var lines []string
	lines = append(lines, fmt.Sprintf("排班计划: %s", sched.Name))
	lines = append(lines, fmt.Sprintf("接班工程师: %s", incomingName))
	if outgoingName != "" {
		lines = append(lines, fmt.Sprintf("当前值班工程师: %s", outgoingName))
	}
	lines = append(lines, fmt.Sprintf("值班时段: %s ~ %s (%s)", startLocal.Format("2006-01-02 15:04"), endLocal.Format("2006-01-02 15:04"), sched.Timezone))

	if latest, err := w.store.GetLatestHandoff(ctx, sched.ID); err == nil && latest != nil && strings.TrimSpace(latest.Notes) != "" {
		lines = append(lines, "交接备注: "+strings.TrimSpace(latest.Notes))
	}

	active, _ := w.store.ListAlertIncidents(ctx, "active", 5)
	if len(active) > 0 {
		lines = append(lines, fmt.Sprintf("当前未关闭告警 (%d 条):", len(active)))
		for _, inc := range active {
			lines = append(lines, fmt.Sprintf("- INC-%04d [%s] %s (%s)", inc.ID, inc.Severity, inc.Title, inc.Status))
		}
	} else {
		lines = append(lines, "当前未关闭告警: 0 条")
	}

	body := strings.Join(lines, "\n")
	summary := fmt.Sprintf("已通知接班人 %s（班次 %s ~ %s，活跃告警 %d 条）", incomingName, startLocal.Format("01-02 15:04"), endLocal.Format("01-02 15:04"), len(active))
	return subject, body, summary
}

func truncateReminderString(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
