package store

import (
	"context"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// CreateHandoffLog creates a new shift handoff record.
func (s *Store) CreateHandoffLog(ctx context.Context, log *model.HandoffLog) error {
	return s.db.WithContext(ctx).Create(log).Error
}

// UpdateHandoffLog updates an existing shift handoff record.
func (s *Store) UpdateHandoffLog(ctx context.Context, log *model.HandoffLog) error {
	return s.db.WithContext(ctx).Save(log).Error
}

// GetHandoffLog retrieves a handoff log by its primary key ID.
func (s *Store) GetHandoffLog(ctx context.Context, id uint64) (*model.HandoffLog, error) {
	var l model.HandoffLog
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&l).Error
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ListHandoffLogs lists recent handoff logs for a schedule.
func (s *Store) ListHandoffLogs(ctx context.Context, scheduleID uint64, limit int) ([]*model.HandoffLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []*model.HandoffLog
	q := s.db.WithContext(ctx)
	if scheduleID > 0 {
		q = q.Where("schedule_id = ?", scheduleID)
	}
	err := q.Order("shift_start desc, id desc").Limit(limit).Find(&list).Error
	return list, err
}

// GetLatestHandoff gets the most recent handoff record for a schedule.
func (s *Store) GetLatestHandoff(ctx context.Context, scheduleID uint64) (*model.HandoffLog, error) {
	var l model.HandoffLog
	err := s.db.WithContext(ctx).
		Where("schedule_id = ?", scheduleID).
		Order("shift_start desc, id desc").
		First(&l).Error
	if err != nil {
		return nil, err
	}
	return &l, nil
}
