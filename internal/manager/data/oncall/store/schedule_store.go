package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type UserSummary struct {
	ID          uint64 `gorm:"column:id" json:"id"`
	DisplayName string `gorm:"column:display_name" json:"display_name"`
	Email       string `gorm:"column:email" json:"email"`
	Phone       string `gorm:"column:phone" json:"phone"`
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Schedules

func (s *Store) CreateSchedule(ctx context.Context, sched *model.Schedule) error {
	return s.db.WithContext(ctx).Create(sched).Error
}

func (s *Store) GetSchedule(ctx context.Context, id uint64) (*model.Schedule, error) {
	var sched model.Schedule
	err := s.db.WithContext(ctx).
		Preload("Rotations").
		Where("id = ?", id).
		First(&sched).Error
	if err != nil {
		return nil, err
	}
	return &sched, nil
}

func (s *Store) ListSchedules(ctx context.Context, teamID uint64, enabledOnly bool) ([]*model.Schedule, error) {
	var list []*model.Schedule
	q := s.db.WithContext(ctx).Preload("Rotations")
	if teamID > 0 {
		q = q.Where("team_id = ?", teamID)
	}
	if enabledOnly {
		q = q.Where("enabled = ?", true)
	}
	err := q.Order("id desc").Find(&list).Error
	return list, err
}

func (s *Store) UpdateSchedule(ctx context.Context, sched *model.Schedule) error {
	return s.db.WithContext(ctx).Save(sched).Error
}

func (s *Store) DeleteSchedule(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("schedule_id = ?", id).Delete(&model.Rotation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("schedule_id = ?", id).Delete(&model.Override{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Schedule{}, id).Error
	})
}

// Rotations

func (s *Store) CreateRotation(ctx context.Context, r *model.Rotation) error {
	return s.db.WithContext(ctx).Create(r).Error
}

func (s *Store) UpdateRotation(ctx context.Context, r *model.Rotation) error {
	return s.db.WithContext(ctx).Save(r).Error
}

func (s *Store) DeleteRotation(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.Rotation{}, id).Error
}

func (s *Store) ListRotationsBySchedule(ctx context.Context, scheduleID uint64) ([]*model.Rotation, error) {
	var list []*model.Rotation
	err := s.db.WithContext(ctx).
		Where("schedule_id = ?", scheduleID).
		Order("tier asc, id asc").
		Find(&list).Error
	return list, err
}

// Overrides

func (s *Store) CreateOverride(ctx context.Context, o *model.Override) error {
	return s.db.WithContext(ctx).Create(o).Error
}

func (s *Store) UpdateOverride(ctx context.Context, o *model.Override) error {
	return s.db.WithContext(ctx).Save(o).Error
}

func (s *Store) DeleteOverride(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.Override{}, id).Error
}

func (s *Store) ListOverridesInWindow(ctx context.Context, scheduleID uint64, start, end time.Time) ([]*model.Override, error) {
	var list []*model.Override
	// Overlaps if o.StartTime < end && o.EndTime > start
	err := s.db.WithContext(ctx).
		Where("schedule_id = ? AND status = ? AND start_time < ? AND end_time > ?",
			scheduleID, model.OverrideStatusApproved, end, start).
		Order("start_time asc").
		Find(&list).Error
	return list, err
}

func (s *Store) ListOverridesByStatus(ctx context.Context, scheduleID uint64, status string) ([]*model.Override, error) {
	var list []*model.Override
	q := s.db.WithContext(ctx)
	if scheduleID > 0 {
		q = q.Where("schedule_id = ?", scheduleID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at desc").Find(&list).Error
	return list, err
}

func (s *Store) UpdateOverrideStatus(ctx context.Context, id uint64, status string, approvedBy *uint64) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if approvedBy != nil {
		updates["approved_by"] = *approvedBy
	}
	return s.db.WithContext(ctx).Model(&model.Override{}).Where("id = ?", id).Updates(updates).Error
}

func (s *Store) UpsertSecondaryRotation(ctx context.Context, scheduleID uint64, r *model.Rotation) error {
	var existing model.Rotation
	err := s.db.WithContext(ctx).
		Where("schedule_id = ? AND tier = ?", scheduleID, model.TierSecondary).
		First(&existing).Error
	if err == nil {
		existing.Name = r.Name
		existing.RotationType = r.RotationType
		existing.ShiftLengthSeconds = r.ShiftLengthSeconds
		existing.UsersJSON = r.UsersJSON
		existing.TimeRestrictionType = r.TimeRestrictionType
		existing.RestrictionStartTime = r.RestrictionStartTime
		existing.RestrictionEndTime = r.RestrictionEndTime
		existing.UpdatedAt = time.Now()
		return s.db.WithContext(ctx).Save(&existing).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		r.ScheduleID = scheduleID
		r.Tier = model.TierSecondary
		r.CreatedAt = time.Now()
		r.UpdatedAt = time.Now()
		return s.db.WithContext(ctx).Create(r).Error
	}
	return err
}

// Calendar Tokens

func generateSecureToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) GetOrCreateCalendarToken(ctx context.Context, userID uint64) (*model.UserCalendarToken, error) {
	var tok model.UserCalendarToken
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&tok).Error
	if err == nil {
		return &tok, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	tok = model.UserCalendarToken{
		UserID: userID,
		Token:  generateSecureToken(),
	}
	if err := s.db.WithContext(ctx).Create(&tok).Error; err != nil {
		return nil, err
	}
	return &tok, nil
}

func (s *Store) ResetCalendarToken(ctx context.Context, userID uint64) (*model.UserCalendarToken, error) {
	newToken := generateSecureToken()
	err := s.db.WithContext(ctx).
		Model(&model.UserCalendarToken{}).
		Where("user_id = ?", userID).
		Updates(map[string]interface{}{
			"token":      newToken,
			"updated_at": time.Now(),
		}).Error
	if err != nil {
		return nil, err
	}
	return s.GetOrCreateCalendarToken(ctx, userID)
}

func (s *Store) GetUserByCalendarToken(ctx context.Context, token string) (*model.UserCalendarToken, error) {
	var tok model.UserCalendarToken
	err := s.db.WithContext(ctx).Where("token = ?", token).First(&tok).Error
	if err != nil {
		return nil, err
	}
	return &tok, nil
}

// Users Lookup (without importing iam)

func (s *Store) GetUsersByIDs(ctx context.Context, userIDs []uint64) (map[uint64]*UserSummary, error) {
	if len(userIDs) == 0 {
		return map[uint64]*UserSummary{}, nil
	}
	var users []UserSummary
	err := s.db.WithContext(ctx).
		Table("users").
		Select("id, display_name, email, phone").
		Where("id IN ?", userIDs).
		Find(&users).Error
	if err != nil {
		return nil, fmt.Errorf("lookup users: %w", err)
	}

	res := make(map[uint64]*UserSummary, len(users))
	for i := range users {
		u := users[i]
		res[u.ID] = &u
	}
	return res, nil
}

// ListAllUsers returns basic user summaries for on-call roster lookups.
func (s *Store) ListAllUsers(ctx context.Context) ([]*UserSummary, error) {
	var users []*UserSummary
	err := s.db.WithContext(ctx).
		Table("users").
		Select("id, display_name, email, phone").
		Order("id asc").
		Find(&users).Error
	return users, err
}
