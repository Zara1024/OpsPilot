package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// ListRoutes returns all alert label routing rules ordered by priority descending.
func (s *Store) ListRoutes(ctx context.Context, teamID uint64) ([]*model.Route, error) {
	var list []*model.Route
	q := s.db.WithContext(ctx)
	if teamID > 0 {
		q = q.Where("team_id = ?", teamID)
	}
	err := q.Order("priority desc, id desc").Find(&list).Error
	return list, err
}

// GetRoute returns a route by its ID.
func (s *Store) GetRoute(ctx context.Context, id uint64) (*model.Route, error) {
	var r model.Route
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&r).Error
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateRoute inserts a new alert route while preserving explicit zero values (such as group_wait_seconds=0 or enabled=false).
func (s *Store) CreateRoute(ctx context.Context, route *model.Route) error {
	wantZeroWait := route.GroupWaitSeconds == 0
	wantZeroInterval := route.GroupIntervalSeconds == 0
	wantDisabled := !route.Enabled

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(route).Error; err != nil {
			return fmt.Errorf("create route: %w", err)
		}
		zeroUpdates := make(map[string]any)
		if wantZeroWait {
			zeroUpdates["group_wait_seconds"] = 0
			route.GroupWaitSeconds = 0
		}
		if wantZeroInterval {
			zeroUpdates["group_interval_seconds"] = 0
			route.GroupIntervalSeconds = 0
		}
		if wantDisabled {
			zeroUpdates["enabled"] = false
			route.Enabled = false
		}
		if len(zeroUpdates) > 0 {
			if err := tx.Model(route).Updates(zeroUpdates).Error; err != nil {
				return fmt.Errorf("apply zero fields on route: %w", err)
			}
		}
		return nil
	})
}

// UpdateRoute updates an existing route rule.
func (s *Store) UpdateRoute(ctx context.Context, route *model.Route) error {
	return s.db.WithContext(ctx).Save(route).Error
}

// DeleteRoute removes a route rule by ID.
func (s *Store) DeleteRoute(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.Route{}, id).Error
}
