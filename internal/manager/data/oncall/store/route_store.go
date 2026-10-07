package store

import (
	"context"

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

// CreateRoute inserts a new alert route.
func (s *Store) CreateRoute(ctx context.Context, route *model.Route) error {
	return s.db.WithContext(ctx).Create(route).Error
}

// UpdateRoute updates an existing route rule.
func (s *Store) UpdateRoute(ctx context.Context, route *model.Route) error {
	return s.db.WithContext(ctx).Save(route).Error
}

// DeleteRoute removes a route rule by ID.
func (s *Store) DeleteRoute(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.Route{}, id).Error
}
