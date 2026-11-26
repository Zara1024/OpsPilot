package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// CreateMaintenanceWindow persists a new planned maintenance window with UTC-normalized timestamps.
func (s *Store) CreateMaintenanceWindow(ctx context.Context, mw *model.MaintenanceWindow) error {
	if mw == nil || strings.TrimSpace(mw.Name) == "" {
		return errors.New("maintenance window name is required")
	}
	now := time.Now().UTC()
	mw.StartsAt = mw.StartsAt.UTC()
	mw.EndsAt = mw.EndsAt.UTC()
	if mw.Action == "" {
		mw.Action = model.MaintenanceActionSuppressAll
	}
	if mw.Status == "" {
		mw.Status = model.MaintenanceStatusActive
	}
	if strings.TrimSpace(mw.MatcherJSON) == "" {
		mw.MatcherJSON = "[]"
	}
	mw.CreatedAt = now
	mw.UpdatedAt = now
	if err := s.db.WithContext(ctx).Create(mw).Error; err != nil {
		return fmt.Errorf("create maintenance window: %w", err)
	}
	return nil
}

// GetMaintenanceWindow fetches a maintenance window by ID.
func (s *Store) GetMaintenanceWindow(ctx context.Context, id uint64) (*model.MaintenanceWindow, error) {
	if id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var item model.MaintenanceWindow
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ListMaintenanceWindows returns maintenance windows ordered by start time descending.
func (s *Store) ListMaintenanceWindows(ctx context.Context, statusFilter string, limit int) ([]*model.MaintenanceWindow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := s.db.WithContext(ctx).Model(&model.MaintenanceWindow{})
	switch strings.ToLower(strings.TrimSpace(statusFilter)) {
	case model.MaintenanceStatusActive, model.MaintenanceStatusCancelled:
		q = q.Where("status = ?", strings.ToLower(strings.TrimSpace(statusFilter)))
	}
	var items []*model.MaintenanceWindow
	if err := q.Order("starts_at desc, id desc").Limit(limit).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}
	return items, nil
}

// ListActiveMaintenanceWindowsAt returns all active windows whose [StartsAt, EndsAt) covers at.
func (s *Store) ListActiveMaintenanceWindowsAt(ctx context.Context, at time.Time) ([]*model.MaintenanceWindow, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	var items []*model.MaintenanceWindow
	if err := s.db.WithContext(ctx).
		Where("status = ? AND starts_at <= ? AND ends_at > ?", model.MaintenanceStatusActive, at, at).
		Order("id asc").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list active maintenance windows: %w", err)
	}
	return items, nil
}

// CancelMaintenanceWindow marks an active maintenance window as cancelled with audit metadata.
func (s *Store) CancelMaintenanceWindow(ctx context.Context, id, operatorID uint64) error {
	if id == 0 {
		return gorm.ErrRecordNotFound
	}
	var existing model.MaintenanceWindow
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":       model.MaintenanceStatusCancelled,
		"cancelled_at": &now,
		"updated_at":   now,
	}
	if operatorID > 0 {
		op := operatorID
		updates["cancelled_by"] = &op
	}
	if err := s.db.WithContext(ctx).Model(&model.MaintenanceWindow{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("cancel maintenance window %d: %w", id, err)
	}
	return nil
}

// DeleteMaintenanceWindow removes a maintenance window row by ID.
func (s *Store) DeleteMaintenanceWindow(ctx context.Context, id uint64) error {
	if id == 0 {
		return gorm.ErrRecordNotFound
	}
	res := s.db.WithContext(ctx).Delete(&model.MaintenanceWindow{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete maintenance window %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// IncrementMaintenanceSuppressed atomically increments suppressed_count for the given maintenance window.
func (s *Store) IncrementMaintenanceSuppressed(ctx context.Context, id uint64) error {
	if id == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&model.MaintenanceWindow{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"suppressed_count": gorm.Expr("suppressed_count + ?", 1),
			"updated_at":       time.Now().UTC(),
		}).Error; err != nil {
		return fmt.Errorf("increment maintenance window %d suppressed_count: %w", id, err)
	}
	return nil
}

// SumMaintenanceSuppressedInRange sums alerts suppressed by maintenance windows overlapping [start, end].
func (s *Store) SumMaintenanceSuppressedInRange(ctx context.Context, scheduleID uint64, start, end time.Time) (int64, error) {
	if start.IsZero() {
		start = time.Now().UTC().AddDate(0, 0, -7)
	} else {
		start = start.UTC()
	}
	if end.IsZero() {
		end = time.Now().UTC()
	} else {
		end = end.UTC()
	}

	q := s.db.WithContext(ctx).Model(&model.MaintenanceWindow{}).
		Where("ends_at >= ? AND starts_at <= ?", start, end)
	if scheduleID > 0 {
		q = q.Where("schedule_id = ? OR schedule_id = 0", scheduleID)
	}

	var total int64
	if err := q.Select("COALESCE(SUM(suppressed_count), 0)").Scan(&total).Error; err != nil {
		return 0, fmt.Errorf("sum maintenance suppressed count: %w", err)
	}
	return total, nil
}
