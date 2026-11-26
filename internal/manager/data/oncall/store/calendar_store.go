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

// ListCalendarDays returns persisted calendar overrides filtered by region and optional year.
func (s *Store) ListCalendarDays(ctx context.Context, region string, year int) ([]*model.CalendarDay, error) {
	if strings.TrimSpace(region) == "" {
		region = model.CalendarRegionCN
	}
	q := s.db.WithContext(ctx).Where("region = ?", region)
	if year > 0 {
		q = q.Where("date LIKE ?", fmt.Sprintf("%04d-%%", year))
	}
	var items []*model.CalendarDay
	if err := q.Order("date asc, id asc").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list calendar days (%s/%d): %w", region, year, err)
	}
	return items, nil
}

// ListCalendarDaysInRange returns persisted calendar entries between startDate and endDate (inclusive, YYYY-MM-DD).
func (s *Store) ListCalendarDaysInRange(ctx context.Context, region, startDate, endDate string) ([]*model.CalendarDay, error) {
	if strings.TrimSpace(region) == "" {
		region = model.CalendarRegionCN
	}
	if startDate == "" || endDate == "" {
		return nil, nil
	}
	var items []*model.CalendarDay
	if err := s.db.WithContext(ctx).
		Where("region = ? AND date >= ? AND date <= ?", region, startDate, endDate).
		Order("date asc, id asc").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list calendar days in range [%s, %s]: %w", startDate, endDate, err)
	}
	return items, nil
}

// UpsertCalendarDay creates or updates a single calendar date override keyed by (date, region).
func (s *Store) UpsertCalendarDay(ctx context.Context, day *model.CalendarDay) (*model.CalendarDay, error) {
	if day == nil || strings.TrimSpace(day.Date) == "" {
		return nil, errors.New("calendar day and date are required")
	}
	var out *model.CalendarDay
	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		saved, txErr := upsertCalendarDayTx(tx, day, now)
		if txErr != nil {
			return txErr
		}
		out = saved
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BulkUpsertCalendarDays idempotently stores a batch of calendar day definitions inside one transaction.
func (s *Store) BulkUpsertCalendarDays(ctx context.Context, days []*model.CalendarDay) (int, error) {
	if len(days) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	count := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, day := range days {
			if day == nil || strings.TrimSpace(day.Date) == "" {
				continue
			}
			if _, txErr := upsertCalendarDayTx(tx, day, now); txErr != nil {
				return txErr
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func upsertCalendarDayTx(tx *gorm.DB, day *model.CalendarDay, now time.Time) (*model.CalendarDay, error) {
	region := strings.TrimSpace(day.Region)
	if region == "" {
		region = model.CalendarRegionCN
	}
	source := strings.TrimSpace(day.Source)
	if source == "" {
		source = model.CalendarSourceCustom
	}

	var existing model.CalendarDay
	err := tx.Where("date = ? AND region = ?", day.Date, region).First(&existing).Error
	if err == nil {
		existing.DayType = day.DayType
		existing.Name = day.Name
		existing.Source = source
		if day.CreatedBy > 0 {
			existing.CreatedBy = day.CreatedBy
		}
		existing.UpdatedAt = now
		if saveErr := tx.Save(&existing).Error; saveErr != nil {
			return nil, fmt.Errorf("update calendar day %s (%s): %w", day.Date, region, saveErr)
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("query calendar day %s (%s): %w", day.Date, region, err)
	}

	rec := model.CalendarDay{
		Date:      day.Date,
		Region:    region,
		DayType:   day.DayType,
		Name:      day.Name,
		Source:    source,
		CreatedBy: day.CreatedBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if createErr := tx.Create(&rec).Error; createErr != nil {
		return nil, fmt.Errorf("create calendar day %s (%s): %w", day.Date, region, createErr)
	}
	return &rec, nil
}

// DeleteCalendarDay removes a custom calendar day override by ID.
func (s *Store) DeleteCalendarDay(ctx context.Context, id uint64) error {
	if id == 0 {
		return gorm.ErrRecordNotFound
	}
	res := s.db.WithContext(ctx).Delete(&model.CalendarDay{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete calendar day %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
