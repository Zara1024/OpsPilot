package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// EnqueueEscalationTask cancels any previous pending escalation tasks for the incident
// and creates a fresh active state machine task atomically.
func (s *Store) EnqueueEscalationTask(ctx context.Context, task *model.EscalationTask) error {
	if task == nil || task.IncidentID == 0 {
		return errors.New("invalid escalation task")
	}
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.EscalationTask{}).
			Where("incident_id = ? AND status = ?", task.IncidentID, model.TaskStatusPending).
			Updates(map[string]any{
				"status":       model.TaskStatusCancelled,
				"locked_until": nil,
				"updated_at":   now,
			}).Error; err != nil {
			return fmt.Errorf("cancel prior escalation tasks: %w", err)
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
		if task.StepStartedAt.IsZero() {
			task.StepStartedAt = now
		}
		if task.NextRunAt.IsZero() {
			task.NextRunAt = now
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
		}
		task.UpdatedAt = now
		if err := tx.Create(task).Error; err != nil {
			return fmt.Errorf("create escalation task: %w", err)
		}
		return nil
	})
}

// CancelEscalationTasksByIncident terminates all pending escalation tasks for an incident.
func (s *Store) CancelEscalationTasksByIncident(ctx context.Context, incidentID uint64, finalStatus string) error {
	if incidentID == 0 {
		return nil
	}
	if finalStatus != model.TaskStatusAcked && finalStatus != model.TaskStatusCancelled && finalStatus != model.TaskStatusCompleted {
		finalStatus = model.TaskStatusCancelled
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&model.EscalationTask{}).
		Where("incident_id = ? AND status = ?", incidentID, model.TaskStatusPending).
		Updates(map[string]any{
			"status":       finalStatus,
			"locked_until": nil,
			"updated_at":   now,
		}).Error; err != nil {
		return fmt.Errorf("cancel escalation tasks for incident %d: %w", incidentID, err)
	}
	return nil
}

// TriggerImmediateEscalationByIncident advances an active task to the next team escalation step immediately.
func (s *Store) TriggerImmediateEscalationByIncident(ctx context.Context, incidentID uint64, now time.Time) (bool, error) {
	if incidentID == 0 {
		return false, nil
	}
	var updated bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.EscalationTask
		qErr := tx.Where("incident_id = ? AND status = ?", incidentID, model.TaskStatusPending).
			Order("id desc").
			First(&task).Error
		if errors.Is(qErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if qErr != nil {
			return fmt.Errorf("find pending escalation task: %w", qErr)
		}

		res := tx.Model(&model.EscalationTask{}).
			Where("id = ? AND status = ?", task.ID, model.TaskStatusPending).
			Updates(map[string]any{
				"current_step":    task.CurrentStep + 1,
				"ladder_step":     uint8(1),
				"step_started_at": now,
				"next_run_at":     now,
				"locked_until":    nil,
				"attempt_count":   0,
				"updated_at":      now,
			})
		if res.Error != nil {
			return fmt.Errorf("advance escalation task: %w", res.Error)
		}
		updated = res.RowsAffected > 0
		return nil
	})
	return updated, err
}

// ClaimDueEscalationTasks optimistically leases due pending tasks for safe concurrent/multi-replica processing.
func (s *Store) ClaimDueEscalationTasks(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*model.EscalationTask, error) {
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
	task.UpdatedAt = time.Now()
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

// GetActiveEscalationStepsByIncidents maps incident_id -> current_step for pending escalation tasks.
func (s *Store) GetActiveEscalationStepsByIncidents(ctx context.Context, incidentIDs []uint64) (map[uint64]uint8, error) {
	if len(incidentIDs) == 0 {
		return map[uint64]uint8{}, nil
	}
	type stepRow struct {
		IncidentID  uint64
		CurrentStep uint8
	}
	var rows []stepRow
	if err := s.db.WithContext(ctx).Model(&model.EscalationTask{}).
		Select("incident_id, current_step").
		Where("incident_id IN ? AND status = ?", incidentIDs, model.TaskStatusPending).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("lookup active escalation steps: %w", err)
	}
	out := make(map[uint64]uint8, len(rows))
	for _, r := range rows {
		out[r.IncidentID] = r.CurrentStep
	}
	return out, nil
}

// CreateNotificationLog inserts an audit log row for a notification or voice call attempt.
func (s *Store) CreateNotificationLog(ctx context.Context, entry *model.NotificationLog) error {
	if entry == nil {
		return errors.New("notification log is nil")
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
