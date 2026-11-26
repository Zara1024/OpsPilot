package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// EnqueueEscalationTask cancels any previous single-alert pending escalation tasks for the incident
// and creates a fresh active state machine task atomically.
func (s *Store) EnqueueEscalationTask(ctx context.Context, task *model.EscalationTask) error {
	_, err := s.EnqueueOrMergeEscalationTask(ctx, task, 0, 0, time.Now())
	return err
}

// EnqueueOrMergeEscalationTask creates a new escalation task or merges incidentID into an active pending group task.
// Returns merged=true when appended to an existing pending group without resetting its dispatch deadline.
func (s *Store) EnqueueOrMergeEscalationTask(
	ctx context.Context,
	task *model.EscalationTask,
	groupWait time.Duration,
	groupInterval time.Duration,
	now time.Time,
) (bool, error) {
	if task == nil || task.IncidentID == 0 {
		return false, errors.New("invalid escalation task")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	var merged bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if task.GroupKey != "" {
			var active model.EscalationTask
			findErr := tx.Where("group_key = ? AND status = ?", task.GroupKey, model.TaskStatusPending).
				Order("id desc").
				First(&active).Error
			if findErr == nil {
				ids := appendUniqueUint64(model.ParseMergedIncidentIDs(&active), task.IncidentID)
				res := tx.Model(&model.EscalationTask{}).
					Where("id = ? AND status = ?", active.ID, model.TaskStatusPending).
					Updates(map[string]any{
						"merged_incident_ids_json": model.EncodeMergedIncidentIDs(ids),
						"alert_count":              len(ids),
						"updated_at":               now,
					})
				if res.Error != nil {
					return fmt.Errorf("merge incident into active group task %d: %w", active.ID, res.Error)
				}
				if res.RowsAffected == 1 {
					merged = true
					return nil
				}
			} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("query active grouped task: %w", findErr)
			}
		}

		// Cancel any standalone pending tasks owned solely by this incident before inserting a new one.
		if err := cancelOrShrinkTasksForIncidentTx(tx, task.IncidentID, model.TaskStatusCancelled, now); err != nil {
			return err
		}

		nextRun := now
		if groupWait > 0 {
			nextRun = now.Add(groupWait)
		}
		if task.GroupKey != "" && groupInterval > 0 {
			var recent model.EscalationTask
			rErr := tx.Where("group_key = ? AND status = ? AND last_dispatched_at IS NOT NULL", task.GroupKey, model.TaskStatusCompleted).
				Order("last_dispatched_at desc").
				First(&recent).Error
			if rErr == nil && recent.LastDispatchedAt != nil {
				minNext := recent.LastDispatchedAt.Add(groupInterval)
				if minNext.After(nextRun) {
					nextRun = minNext
				}
			}
		}

		if task.Status == "" {
			task.Status = model.TaskStatusPending
		}
		if task.CurrentStep == 0 {
			task.CurrentStep = 1
		}
		if task.LadderStep == 0 {
			task.LadderStep = 1
		}
		task.MergedIncidentIDsJSON = model.EncodeMergedIncidentIDs([]uint64{task.IncidentID})
		task.AlertCount = 1
		task.GroupIntervalSeconds = int(groupInterval.Seconds())
		if task.StepStartedAt.IsZero() || groupWait > 0 || groupInterval > 0 {
			task.StepStartedAt = nextRun
		} else {
			task.StepStartedAt = task.StepStartedAt.UTC()
		}
		if task.NextRunAt.IsZero() || groupWait > 0 || groupInterval > 0 {
			task.NextRunAt = nextRun
		} else {
			task.NextRunAt = task.NextRunAt.UTC()
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
		} else {
			task.CreatedAt = task.CreatedAt.UTC()
		}
		task.UpdatedAt = now
		if err := tx.Create(task).Error; err != nil {
			return fmt.Errorf("create escalation task: %w", err)
		}
		return nil
	})
	return merged, err
}

// GetEscalationTaskByID loads an escalation task by primary key.
func (s *Store) GetEscalationTaskByID(ctx context.Context, id uint64) (*model.EscalationTask, error) {
	if id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var task model.EscalationTask
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// FindPendingTaskByIncident returns the latest pending escalation task covering incidentID (single or grouped).
func (s *Store) FindPendingTaskByIncident(ctx context.Context, incidentID uint64) (*model.EscalationTask, error) {
	if incidentID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var candidates []*model.EscalationTask
	if err := s.db.WithContext(ctx).
		Where("status = ? AND (incident_id = ? OR alert_count > 1)", model.TaskStatusPending, incidentID).
		Order("id desc").
		Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, c := range candidates {
		if model.ContainsIncidentID(c, incidentID) {
			return c, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// CancelEscalationTasksByIncident terminates or shrinks pending escalation tasks covering incidentID.
// A grouped task only terminates when all of its open incidents have closed.
func (s *Store) CancelEscalationTasksByIncident(ctx context.Context, incidentID uint64, finalStatus string) error {
	if incidentID == 0 {
		return nil
	}
	if finalStatus != model.TaskStatusAcked && finalStatus != model.TaskStatusCancelled && finalStatus != model.TaskStatusCompleted {
		finalStatus = model.TaskStatusCancelled
	}
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return cancelOrShrinkTasksForIncidentTx(tx, incidentID, finalStatus, now)
	})
}

func cancelOrShrinkTasksForIncidentTx(tx *gorm.DB, incidentID uint64, finalStatus string, now time.Time) error {
	if incidentID == 0 {
		return nil
	}
	var candidates []*model.EscalationTask
	if err := tx.Where("status = ? AND (incident_id = ? OR alert_count > 1)", model.TaskStatusPending, incidentID).
		Find(&candidates).Error; err != nil {
		return fmt.Errorf("query pending tasks for incident %d: %w", incidentID, err)
	}
	for _, task := range candidates {
		if !model.ContainsIncidentID(task, incidentID) {
			continue
		}
		ids := model.ParseMergedIncidentIDs(task)
		if len(ids) <= 1 {
			if err := tx.Model(&model.EscalationTask{}).
				Where("id = ? AND status = ?", task.ID, model.TaskStatusPending).
				Updates(map[string]any{
					"status":       finalStatus,
					"locked_until": nil,
					"updated_at":   now,
				}).Error; err != nil {
				return fmt.Errorf("cancel escalation task %d: %w", task.ID, err)
			}
			continue
		}

		// Grouped task: keep running while sibling incidents in the batch remain open.
		var openIncidents []*alertmodel.Incident
		if err := tx.Where("id IN ? AND status = ?", ids, alertmodel.StatusOpen).
			Order("id asc").
			Find(&openIncidents).Error; err != nil {
			return fmt.Errorf("check sibling open incidents for task %d: %w", task.ID, err)
		}
		remaining := make([]uint64, 0, len(openIncidents))
		for _, inc := range openIncidents {
			if inc.ID != incidentID {
				remaining = append(remaining, inc.ID)
			}
		}
		if len(remaining) == 0 {
			if err := tx.Model(&model.EscalationTask{}).
				Where("id = ? AND status = ?", task.ID, model.TaskStatusPending).
				Updates(map[string]any{
					"status":       finalStatus,
					"locked_until": nil,
					"updated_at":   now,
				}).Error; err != nil {
				return fmt.Errorf("close grouped task %d: %w", task.ID, err)
			}
			continue
		}

		if err := tx.Model(&model.EscalationTask{}).
			Where("id = ? AND status = ?", task.ID, model.TaskStatusPending).
			Updates(map[string]any{
				"incident_id":              remaining[0],
				"merged_incident_ids_json": model.EncodeMergedIncidentIDs(remaining),
				"alert_count":              len(remaining),
				"updated_at":               now,
			}).Error; err != nil {
			return fmt.Errorf("shrink grouped task %d: %w", task.ID, err)
		}
	}
	return nil
}

// TriggerImmediateEscalationByIncident advances an active task (single or grouped) to the next team step immediately.
func (s *Store) TriggerImmediateEscalationByIncident(ctx context.Context, incidentID uint64, now time.Time) (bool, error) {
	if incidentID == 0 {
		return false, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	var updated bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []*model.EscalationTask
		if err := tx.Where("status = ? AND (incident_id = ? OR alert_count > 1)", model.TaskStatusPending, incidentID).
			Order("id desc").
			Find(&candidates).Error; err != nil {
			return fmt.Errorf("find pending escalation tasks: %w", err)
		}
		var target *model.EscalationTask
		for _, c := range candidates {
			if model.ContainsIncidentID(c, incidentID) {
				target = c
				break
			}
		}
		if target == nil {
			return nil
		}

		res := tx.Model(&model.EscalationTask{}).
			Where("id = ? AND status = ?", target.ID, model.TaskStatusPending).
			Updates(map[string]any{
				"current_step":         target.CurrentStep + 1,
				"ladder_step":          uint8(1),
				"pinned_assignee_id":   uint64(0),
				"assignee_user_id":     uint64(0),
				"voice_redial_count":   uint8(0),
				"last_voice_call_id":   "",
				"awaiting_voice_grace": false,
				"step_started_at":      now,
				"next_run_at":          now,
				"locked_until":         nil,
				"attempt_count":        0,
				"updated_at":           now,
			})
		if res.Error != nil {
			return fmt.Errorf("advance escalation task: %w", res.Error)
		}
		updated = res.RowsAffected > 0
		return nil
	})
	return updated, err
}

// ScheduleVoiceRedialByCall atomically schedules an automatic voice redial on the same step/user for an unanswered call.
func (s *Store) ScheduleVoiceRedialByCall(ctx context.Context, taskID uint64, callID string, maxRedials int, nextRunAt time.Time) (int, bool, error) {
	if taskID == 0 || callID == "" || maxRedials <= 0 {
		return 0, false, nil
	}
	now := time.Now().UTC()
	if nextRunAt.IsZero() {
		nextRunAt = now.Add(20 * time.Second)
	} else {
		nextRunAt = nextRunAt.UTC()
	}

	var attempt int
	var scheduled bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.EscalationTask
		findErr := tx.Where("id = ? AND status = ? AND last_voice_call_id = ?", taskID, model.TaskStatusPending, callID).
			First(&task).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if findErr != nil {
			return fmt.Errorf("find task %d for voice redial: %w", taskID, findErr)
		}
		if int(task.VoiceRedialCount) >= maxRedials || task.LastVoiceStep == 0 || task.LastVoiceLadderStep == 0 {
			return nil
		}

		nextCount := task.VoiceRedialCount + 1
		res := tx.Model(&model.EscalationTask{}).
			Where("id = ? AND status = ? AND last_voice_call_id = ? AND voice_redial_count = ?",
				task.ID, model.TaskStatusPending, callID, task.VoiceRedialCount).
			Updates(map[string]any{
				"current_step":         task.LastVoiceStep,
				"ladder_step":          task.LastVoiceLadderStep,
				"pinned_assignee_id":   task.LastVoiceUserID,
				"voice_redial_count":   nextCount,
				"awaiting_voice_grace": false,
				"next_run_at":          nextRunAt,
				"locked_until":         nil,
				"attempt_count":        0,
				"updated_at":           now,
			})
		if res.Error != nil {
			return fmt.Errorf("schedule voice redial for task %d: %w", task.ID, res.Error)
		}
		if res.RowsAffected == 1 {
			attempt = int(nextCount)
			scheduled = true
		}
		return nil
	})
	return attempt, scheduled, err
}

// FastEscalateVoiceCall atomically advances a task to failedStep+1 and clears last_voice_call_id so duplicate callbacks never double-escalate.
func (s *Store) FastEscalateVoiceCall(ctx context.Context, taskID uint64, callID string, failedStep uint8, now time.Time) (uint8, bool, error) {
	if taskID == 0 || callID == "" {
		return 0, false, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	var nextStep uint8
	var advanced bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.EscalationTask
		findErr := tx.Where("id = ? AND status = ? AND last_voice_call_id = ?", taskID, model.TaskStatusPending, callID).
			First(&task).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if findErr != nil {
			return fmt.Errorf("find task %d for fast escalate: %w", taskID, findErr)
		}

		baseStep := failedStep
		if baseStep == 0 {
			baseStep = task.LastVoiceStep
		}
		if baseStep == 0 {
			baseStep = task.CurrentStep
		}
		targetStep := baseStep + 1

		res := tx.Model(&model.EscalationTask{}).
			Where("id = ? AND status = ? AND last_voice_call_id = ?", task.ID, model.TaskStatusPending, callID).
			Updates(map[string]any{
				"current_step":         targetStep,
				"ladder_step":          uint8(1),
				"pinned_assignee_id":   uint64(0),
				"assignee_user_id":     uint64(0),
				"voice_redial_count":   uint8(0),
				"last_voice_call_id":   "",
				"awaiting_voice_grace": false,
				"step_started_at":      now,
				"next_run_at":          now,
				"locked_until":         nil,
				"attempt_count":        0,
				"updated_at":           now,
			})
		if res.Error != nil {
			return fmt.Errorf("fast escalate task %d: %w", task.ID, res.Error)
		}
		if res.RowsAffected == 1 {
			nextStep = targetStep
			advanced = true
		}
		return nil
	})
	return nextStep, advanced, err
}

// ClaimDueEscalationTasks optimistically leases due pending tasks for safe concurrent/multi-replica processing.
func (s *Store) ClaimDueEscalationTasks(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*model.EscalationTask, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var candidates []*model.EscalationTask
	if err := s.db.WithContext(ctx).
		Where("status = ? AND next_run_at <= ? AND (locked_until IS NULL OR locked_until < ?)", model.TaskStatusPending, now, now).
		Order("next_run_at asc, id asc").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("query due escalation tasks: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	leaseUntil := now.Add(lease)
	claimed := make([]*model.EscalationTask, 0, len(candidates))
	for _, task := range candidates {
		res := s.db.WithContext(ctx).Model(&model.EscalationTask{}).
			Where("id = ? AND status = ? AND (locked_until IS NULL OR locked_until < ?)", task.ID, model.TaskStatusPending, now).
			Updates(map[string]any{
				"locked_until": leaseUntil,
				"updated_at":   now,
			})
		if res.Error != nil {
			return nil, fmt.Errorf("claim escalation task %d: %w", task.ID, res.Error)
		}
		if res.RowsAffected == 1 {
			task.LockedUntil = &leaseUntil
			task.UpdatedAt = now
			claimed = append(claimed, task)
		}
	}
	return claimed, nil
}

// SaveEscalationTask persists updated state for an escalation task.
func (s *Store) SaveEscalationTask(ctx context.Context, task *model.EscalationTask) error {
	if task == nil {
		return errors.New("escalation task is nil")
	}
	if !task.StepStartedAt.IsZero() {
		task.StepStartedAt = task.StepStartedAt.UTC()
	}
	if !task.NextRunAt.IsZero() {
		task.NextRunAt = task.NextRunAt.UTC()
	}
	if task.LockedUntil != nil {
		lu := task.LockedUntil.UTC()
		task.LockedUntil = &lu
	}
	task.UpdatedAt = time.Now().UTC()
	if err := s.db.WithContext(ctx).Save(task).Error; err != nil {
		return fmt.Errorf("save escalation task %d: %w", task.ID, err)
	}
	return nil
}

// ListEscalationTasks returns recent escalation state machine rows ordered by ID descending.
func (s *Store) ListEscalationTasks(ctx context.Context, status string, limit int) ([]*model.EscalationTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := s.db.WithContext(ctx).Model(&model.EscalationTask{})
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	var list []*model.EscalationTask
	if err := q.Order("id desc").Limit(limit).Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list escalation tasks: %w", err)
	}
	return list, nil
}

// GetActiveEscalationStepsByIncidents maps incident_id -> current_step for pending escalation tasks (including grouped alerts).
func (s *Store) GetActiveEscalationStepsByIncidents(ctx context.Context, incidentIDs []uint64) (map[uint64]uint8, error) {
	if len(incidentIDs) == 0 {
		return map[uint64]uint8{}, nil
	}
	var pending []*model.EscalationTask
	if err := s.db.WithContext(ctx).
		Where("status = ?", model.TaskStatusPending).
		Find(&pending).Error; err != nil {
		return nil, fmt.Errorf("lookup active escalation steps: %w", err)
	}
	wanted := make(map[uint64]struct{}, len(incidentIDs))
	for _, id := range incidentIDs {
		wanted[id] = struct{}{}
	}
	out := make(map[uint64]uint8, len(incidentIDs))
	for _, task := range pending {
		for _, id := range model.ParseMergedIncidentIDs(task) {
			if _, ok := wanted[id]; ok {
				out[id] = task.CurrentStep
			}
		}
	}
	return out, nil
}

// GetActivePinnedAssigneesByIncidents maps incident_id -> pinned_assignee_id for pending reassigned escalation tasks.
func (s *Store) GetActivePinnedAssigneesByIncidents(ctx context.Context, incidentIDs []uint64) (map[uint64]uint64, error) {
	if len(incidentIDs) == 0 {
		return map[uint64]uint64{}, nil
	}
	var pending []*model.EscalationTask
	if err := s.db.WithContext(ctx).
		Where("status = ? AND pinned_assignee_id > 0", model.TaskStatusPending).
		Find(&pending).Error; err != nil {
		return nil, fmt.Errorf("lookup pinned assignees: %w", err)
	}
	wanted := make(map[uint64]struct{}, len(incidentIDs))
	for _, id := range incidentIDs {
		wanted[id] = struct{}{}
	}
	out := make(map[uint64]uint64, len(incidentIDs))
	for _, task := range pending {
		for _, id := range model.ParseMergedIncidentIDs(task) {
			if _, ok := wanted[id]; ok {
				out[id] = task.PinnedAssigneeID
			}
		}
	}
	return out, nil
}

// CreateNotificationLog inserts an audit log row for a notification or voice call attempt.
func (s *Store) CreateNotificationLog(ctx context.Context, entry *model.NotificationLog) error {
	if entry == nil {
		return errors.New("notification log is nil")
	}
	if entry.AlertCount <= 0 {
		entry.AlertCount = 1
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
		return fmt.Errorf("create notification log: %w", err)
	}
	return nil
}

// FindNotificationLogByCall finds the latest notification log matching a provider call ID or internal call ID.
func (s *Store) FindNotificationLogByCall(ctx context.Context, outCallID, callID string) (*model.NotificationLog, error) {
	var entry model.NotificationLog
	if outCallID != "" {
		err := s.db.WithContext(ctx).Where("out_call_id = ?", outCallID).Order("id desc").First(&entry).Error
		if err == nil {
			return &entry, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find notification log by out_call_id: %w", err)
		}
	}
	if callID != "" {
		err := s.db.WithContext(ctx).Where("call_id = ?", callID).Order("id desc").First(&entry).Error
		if err == nil {
			return &entry, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find notification log by call_id: %w", err)
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// UpdateNotificationLog persists updated callback status fields on an existing notification log.
func (s *Store) UpdateNotificationLog(ctx context.Context, entry *model.NotificationLog) error {
	if entry == nil {
		return errors.New("notification log is nil")
	}
	if err := s.db.WithContext(ctx).Save(entry).Error; err != nil {
		return fmt.Errorf("update notification log %d: %w", entry.ID, err)
	}
	return nil
}

// ListNotificationLogs retrieves paginated delivery logs filtered optionally by channel and status.
func (s *Store) ListNotificationLogs(ctx context.Context, channel, status string, offset, limit int) ([]*model.NotificationLog, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	q := s.db.WithContext(ctx).Model(&model.NotificationLog{})
	if channel != "" && channel != "all" {
		q = q.Where("channel = ?", channel)
	}
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count notification logs: %w", err)
	}

	var items []*model.NotificationLog
	if err := q.Order("id desc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list notification logs: %w", err)
	}
	return items, total, nil
}

// HasRecentVoiceCall reports whether the given user received an active or confirmed voice call since cutoff.
func (s *Store) HasRecentVoiceCall(ctx context.Context, userID uint64, since time.Time) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&model.NotificationLog{}).
		Where("user_id = ? AND channel = ? AND status IN ? AND created_at >= ?",
			userID, "voice_call", []string{model.NotifyStatusCalling, model.NotifyStatusAnswered, model.NotifyStatusDTMFConfirmed}, since).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check recent voice calls for user %d: %w", userID, err)
	}
	return count > 0, nil
}

// CountNightNotificationsByUser counts night-time notifications per user since cutoff and returns the total.
func (s *Store) CountNightNotificationsByUser(ctx context.Context, since time.Time) (map[uint64]int, int, error) {
	type nightRow struct {
		UserID uint64
		Cnt    int64
	}
	var rows []nightRow
	err := s.db.WithContext(ctx).Model(&model.NotificationLog{}).
		Select("user_id, count(*) as cnt").
		Where("is_night = ? AND created_at >= ?", true, since).
		Group("user_id").
		Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("count night notifications: %w", err)
	}
	byUser := make(map[uint64]int, len(rows))
	total := 0
	for _, r := range rows {
		c := int(r.Cnt)
		byUser[r.UserID] = c
		total += c
	}
	return byUser, total, nil
}

func appendUniqueUint64(slice []uint64, val uint64) []uint64 {
	if val == 0 {
		return slice
	}
	for _, item := range slice {
		if item == val {
			return slice
		}
	}
	return append(slice, val)
}
