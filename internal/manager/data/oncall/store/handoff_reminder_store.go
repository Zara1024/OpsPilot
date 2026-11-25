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

// FindHandoffLogByUniqueKey looks up a handoff or reminder log entry by its unique composite key.
func (s *Store) FindHandoffLogByUniqueKey(
	ctx context.Context,
	scheduleID, userID uint64,
	shiftStart time.Time,
	reminderType string,
) (*model.HandoffLog, error) {
	var entry model.HandoffLog
	err := s.db.WithContext(ctx).
		Where("schedule_id = ? AND user_id = ? AND shift_start = ? AND reminder_type = ?",
			scheduleID, userID, shiftStart.UTC().Truncate(time.Second), reminderType).
		First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// TryClaimHandoffReminder atomically inserts a reminder log claim keyed by uk_sched_user_shift.
// Returns (true, nil) when this caller wins the claim, or (false, nil) if already claimed by another replica.
func (s *Store) TryClaimHandoffReminder(ctx context.Context, entry *model.HandoffLog) (bool, error) {
	if entry == nil {
		return false, errors.New("handoff reminder entry is nil")
	}
	entry.ShiftStart = entry.ShiftStart.UTC().Truncate(time.Second)
	entry.ShiftEnd = entry.ShiftEnd.UTC().Truncate(time.Second)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	err := s.db.WithContext(ctx).Create(entry).Error
	if err != nil {
		if isUniqueConstraintErr(err) {
			return false, nil
		}
		return false, fmt.Errorf("claim handoff reminder: %w", err)
	}
	return true, nil
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "sqlstate 23505")
}
