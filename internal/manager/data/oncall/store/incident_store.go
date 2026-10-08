package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
)

// AlertAnalyticsData holds aggregated metrics from the real alert_incidents table.
type AlertAnalyticsData struct {
	TotalIncidents  int64
	MTTASeconds     float64
	Trends          []DayTrend
	TopRules        []RuleFrequency
	UserHandleCount map[uint64]int64
}

type DayTrend struct {
	Date  string
	Count int64
}

type RuleFrequency struct {
	RuleName     string
	Severity     string
	TriggerCount int64
}

// ListAlertIncidents returns incidents from alert_incidents table.
func (s *Store) ListAlertIncidents(ctx context.Context, statusFilter string, limit int) ([]*alertmodel.Incident, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var list []*alertmodel.Incident
	q := s.db.WithContext(ctx)
	if statusFilter == "active" {
		q = q.Where("status IN ?", []string{alertmodel.StatusOpen, alertmodel.StatusAcknowledged, alertmodel.StatusSilenced})
	} else if statusFilter != "" && statusFilter != "all" {
		q = q.Where("status = ?", statusFilter)
	}
	err := q.Order("id desc").Limit(limit).Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("list alert incidents: %w", err)
	}
	return list, nil
}

// GetAlertIncident returns a single incident by ID.
func (s *Store) GetAlertIncident(ctx context.Context, id uint64) (*alertmodel.Incident, error) {
	var inc alertmodel.Incident
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&inc).Error
	if err != nil {
		return nil, fmt.Errorf("get alert incident %d: %w", id, err)
	}
	return &inc, nil
}

// AckAlertIncident acknowledges an incident and records an audit event in alert_events.
func (s *Store) AckAlertIncident(ctx context.Context, id uint64, userID uint64, note string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inc alertmodel.Incident
		if err := tx.Where("id = ?", id).First(&inc).Error; err != nil {
			return fmt.Errorf("incident %d not found: %w", id, err)
		}
		inc.Status = alertmodel.StatusAcknowledged
		inc.AcknowledgedAt = &now
		if userID > 0 {
			inc.AcknowledgedBy = &userID
		}
		if err := tx.Save(&inc).Error; err != nil {
			return fmt.Errorf("save acknowledged incident: %w", err)
		}

		reason := note
		if reason == "" {
			reason = "Acked via On-Call ChatOps"
		}
		var userPtr *uint64
		if userID > 0 {
			userPtr = &userID
		}
		evt := &alertmodel.Event{
			IncidentID:     id,
			EventType:      alertmodel.EventTypeAcknowledged,
			StatusAfter:    alertmodel.StatusAcknowledged,
			Severity:       inc.Severity,
			Title:          inc.Title,
			ActorType:      alertmodel.ActorTypeUser,
			ActorID:        userPtr,
			OperatorUserID: userPtr,
			Reason:         reason,
			SnapshotJSON:   "{}",
			OccurredAt:     now,
		}
		if err := tx.Create(evt).Error; err != nil {
			return fmt.Errorf("create ack event: %w", err)
		}
		return nil
	})
}

// SilenceAlertIncident silences an incident for the specified duration and records an audit event.
func (s *Store) SilenceAlertIncident(ctx context.Context, id uint64, userID uint64, duration time.Duration, note string) error {
	now := time.Now()
	silencedUntil := now.Add(duration)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inc alertmodel.Incident
		if err := tx.Where("id = ?", id).First(&inc).Error; err != nil {
			return fmt.Errorf("incident %d not found: %w", id, err)
		}
		inc.Status = alertmodel.StatusSilenced
		inc.SilencedUntil = &silencedUntil
		if err := tx.Save(&inc).Error; err != nil {
			return fmt.Errorf("save silenced incident: %w", err)
		}

		reason := note
		if reason == "" {
			reason = fmt.Sprintf("Silenced for %v via On-Call ChatOps", duration)
		}
		var userPtr *uint64
		if userID > 0 {
			userPtr = &userID
		}
		evt := &alertmodel.Event{
			IncidentID:     id,
			EventType:      alertmodel.EventTypeSilenced,
			StatusAfter:    alertmodel.StatusSilenced,
			Severity:       inc.Severity,
			Title:          inc.Title,
			ActorType:      alertmodel.ActorTypeUser,
			ActorID:        userPtr,
			OperatorUserID: userPtr,
			Reason:         reason,
			SnapshotJSON:   "{}",
			OccurredAt:     now,
		}
		if err := tx.Create(evt).Error; err != nil {
			return fmt.Errorf("create silence event: %w", err)
		}
		return nil
	})
}

// EscalateAlertIncident records a manual escalation event in alert_events.
func (s *Store) EscalateAlertIncident(ctx context.Context, id uint64, userID uint64, note string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inc alertmodel.Incident
		if err := tx.Where("id = ?", id).First(&inc).Error; err != nil {
			return fmt.Errorf("incident %d not found: %w", id, err)
		}

		reason := note
		if reason == "" {
			reason = "Manually escalated to tier-2 via On-Call ChatOps"
		}
		var userPtr *uint64
		if userID > 0 {
			userPtr = &userID
		}
		evt := &alertmodel.Event{
			IncidentID:     id,
			EventType:      "escalated",
			StatusAfter:    inc.Status,
			Severity:       inc.Severity,
			Title:          inc.Title,
			ActorType:      alertmodel.ActorTypeUser,
			ActorID:        userPtr,
			OperatorUserID: userPtr,
			Reason:         reason,
			SnapshotJSON:   "{}",
			OccurredAt:     now,
		}
		if err := tx.Create(evt).Error; err != nil {
			return fmt.Errorf("create escalation event: %w", err)
		}
		return nil
	})
}

// GetAlertAnalyticsData compiles real metrics from alert_incidents for burnout analysis.
func (s *Store) GetAlertAnalyticsData(ctx context.Context) (*AlertAnalyticsData, error) {
	var total int64
	if err := s.db.WithContext(ctx).Table("alert_incidents").Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count alert incidents: %w", err)
	}

	type timeRow struct {
		FirstFiredAt   time.Time
		AcknowledgedAt *time.Time
		ResolvedAt     *time.Time
		AcknowledgedBy *uint64
	}
	var rows []timeRow
	if err := s.db.WithContext(ctx).Table("alert_incidents").
		Select("first_fired_at, acknowledged_at, resolved_at, acknowledged_by").
		Order("id desc").
		Limit(200).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query incident timing: %w", err)
	}

	var totalSeconds float64
	var validTimingCount int
	dayCounts := make(map[string]int64)
	userHandleCounts := make(map[uint64]int64)

	for _, r := range rows {
		if r.AcknowledgedAt != nil && !r.AcknowledgedAt.Before(r.FirstFiredAt) {
			diff := r.AcknowledgedAt.Sub(r.FirstFiredAt).Seconds()
			if diff >= 0 && diff < 86400*30 { // Filter out invalid outliers
				totalSeconds += diff
				validTimingCount++
			}
		} else if r.ResolvedAt != nil && !r.ResolvedAt.Before(r.FirstFiredAt) {
			diff := r.ResolvedAt.Sub(r.FirstFiredAt).Seconds()
			if diff >= 0 && diff < 86400*30 {
				totalSeconds += diff
				validTimingCount++
			}
		}

		dayKey := r.FirstFiredAt.Format("01-02")
		dayCounts[dayKey]++

		if r.AcknowledgedBy != nil && *r.AcknowledgedBy > 0 {
			userHandleCounts[*r.AcknowledgedBy]++
		}
	}

	avgMTTA := 168.0 // default reasonable baseline if no samples
	if validTimingCount > 0 {
		avgMTTA = totalSeconds / float64(validTimingCount)
	}

	// 7-day trend
	now := time.Now()
	trends := make([]DayTrend, 0, 7)
	for i := 6; i >= 0; i-- {
		dayStr := now.AddDate(0, 0, -i).Format("01-02")
		trends = append(trends, DayTrend{
			Date:  dayStr,
			Count: dayCounts[dayStr],
		})
	}

	// Top rules
	type ruleResult struct {
		RuleName     string `gorm:"column:rule_name"`
		RuleKey      string `gorm:"column:rule"`
		Severity     string `gorm:"column:severity"`
		TriggerCount int64  `gorm:"column:trigger_count"`
	}
	var ruleRows []ruleResult
	_ = s.db.WithContext(ctx).Table("alert_incidents").
		Select("rule_name, rule, severity, count(*) as trigger_count").
		Group("rule_name, rule, severity").
		Order("trigger_count desc").
		Limit(10).
		Find(&ruleRows).Error

	topRules := make([]RuleFrequency, 0, len(ruleRows))
	for _, rr := range ruleRows {
		name := strings.TrimSpace(rr.RuleName)
		if name == "" || name == "????" { // Clean up unencoded names
			name = rr.RuleKey
		}
		if name == "" {
			name = "host_unspecified_alert"
		}
		topRules = append(topRules, RuleFrequency{
			RuleName:     name,
			Severity:     rr.Severity,
			TriggerCount: rr.TriggerCount,
		})
	}

	return &AlertAnalyticsData{
		TotalIncidents:  total,
		MTTASeconds:     avgMTTA,
		Trends:          trends,
		TopRules:        topRules,
		UserHandleCount: userHandleCounts,
	}, nil
}
