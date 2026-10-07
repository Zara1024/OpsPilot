package store

import (
	"context"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// ListUserNotificationRules retrieves all notification steps for the user and urgency.
func (s *Store) ListUserNotificationRules(ctx context.Context, userID uint64, urgency string) ([]*model.UserNotificationRule, error) {
	var list []*model.UserNotificationRule
	q := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if urgency != "" {
		q = q.Where("urgency = ?", urgency)
	}
	err := q.Order("step_number asc, id asc").Find(&list).Error
	return list, err
}

// SetUserNotificationRules replaces the notification rules for the user and urgency.
func (s *Store) SetUserNotificationRules(ctx context.Context, userID uint64, urgency string, rules []*model.UserNotificationRule) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND urgency = ?", userID, urgency).Delete(&model.UserNotificationRule{}).Error; err != nil {
			return err
		}
		if len(rules) == 0 {
			return nil
		}
		for i, r := range rules {
			r.ID = 0
			r.UserID = userID
			r.Urgency = urgency
			r.StepNumber = uint8(i + 1)
		}
		return tx.Create(&rules).Error
	})
}

// Escalation Policies

func (s *Store) ListEscalationPolicies(ctx context.Context, scheduleID uint64) ([]*model.EscalationPolicy, error) {
	var list []*model.EscalationPolicy
	q := s.db.WithContext(ctx)
	if scheduleID > 0 {
		q = q.Where("schedule_id = ?", scheduleID)
	}
	err := q.Order("schedule_id asc, step_number asc").Find(&list).Error
	return list, err
}

func (s *Store) GetEscalationPolicy(ctx context.Context, id uint64) (*model.EscalationPolicy, error) {
	var p model.EscalationPolicy
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateEscalationPolicy(ctx context.Context, p *model.EscalationPolicy) error {
	return s.db.WithContext(ctx).Create(p).Error
}

func (s *Store) UpdateEscalationPolicy(ctx context.Context, p *model.EscalationPolicy) error {
	return s.db.WithContext(ctx).Save(p).Error
}

func (s *Store) DeleteEscalationPolicy(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.EscalationPolicy{}, id).Error
}
