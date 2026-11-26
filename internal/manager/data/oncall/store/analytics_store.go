package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// SeverityMetric captures incident counts and latency samples grouped by severity.
type SeverityMetric struct {
	Severity    string
	Count       int
	MTTASamples []float64
	MTTRSamples []float64
}

// RuleNoiseStat aggregates real firing volume and duration for a single alert rule.
type RuleNoiseStat struct {
	RuleKey        string
	RuleName       string
	Severity       string
	IncidentCount  int
	TriggerCount   int
	AvgDurationSec int
}

// DayMetric holds daily firing and night-wake counters keyed by local MM-DD date.
type DayMetric struct {
	Date          string
	IncidentCount int
	NightCount    int
}

// DetailedAlertAnalytics holds raw and aggregated metrics queried from persistent tables for [Start, End].
type DetailedAlertAnalytics struct {
	TotalIncidents        int
	AcknowledgedCount     int
	ResolvedCount         int
	OpenCount             int
	MTTASamples           []float64
	MTTRSamples           []float64
	SeverityMetrics       map[string]*SeverityMetric
	UserHandleCount       map[uint64]int64
	UserResolveCount      map[uint64]int64
	UserMTTASamples       map[uint64][]float64
	NightCallByUser       map[uint64]int
	TotalNightCalls       int
	MergedAlertsCount     int
	SuppressedAlertsCount int
	DayTrends             []DayMetric
	TopRules              []RuleNoiseStat
}

// GetDetailedAlertAnalytics computes real MTTA/MTTR samples, night-call counts, noise reduction, and trends for [start, end].
func (s *Store) GetDetailedAlertAnalytics(ctx context.Context, scheduleID uint64, start, end time.Time) (*DetailedAlertAnalytics, error) {
	if end.IsZero() {
		end = time.Now().UTC()
	} else {
		end = end.UTC()
	}
	if start.IsZero() || !start.Before(end) {
		start = end.AddDate(0, 0, -7)
	} else {
		start = start.UTC()
	}

	// Pad SQLite string-time filters by ±14h so mixed local/UTC rows are accurately filtered in Go via time.Time.
	queryStart := start.Add(-14 * time.Hour)
	queryEnd := end.Add(14 * time.Hour)

	out := &DetailedAlertAnalytics{
		SeverityMetrics:  make(map[string]*SeverityMetric),
		UserHandleCount:  make(map[uint64]int64),
		UserResolveCount: make(map[uint64]int64),
		UserMTTASamples:  make(map[uint64][]float64),
		NightCallByUser:  make(map[uint64]int),
	}

	var tasks []*model.EscalationTask
	taskQ := s.db.WithContext(ctx).Where("created_at >= ? AND created_at <= ?", queryStart, queryEnd)
	if scheduleID > 0 {
		taskQ = taskQ.Where("schedule_id = ?", scheduleID)
	}
	if err := taskQ.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("query analytics escalation tasks: %w", err)
	}

	var logs []*model.NotificationLog
	logQ := s.db.WithContext(ctx).Where("created_at >= ? AND created_at <= ?", queryStart, queryEnd)
	if scheduleID > 0 {
		logQ = logQ.Where("schedule_id = ?", scheduleID)
	}
	if err := logQ.Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("query analytics notification logs: %w", err)
	}

	schedIncidentSet := make(map[uint64]struct{})
	for _, t := range tasks {
		if t == nil || t.CreatedAt.Before(start) || t.CreatedAt.After(end) {
			continue
		}
		if t.AlertCount > 1 {
			out.MergedAlertsCount += t.AlertCount - 1
		}
		for _, id := range model.ParseMergedIncidentIDs(t) {
			schedIncidentSet[id] = struct{}{}
		}
	}

	nightByDay := make(map[string]int)
	throttledLogs := 0
	for _, l := range logs {
		if l == nil || l.CreatedAt.Before(start) || l.CreatedAt.After(end) {
			continue
		}
		if l.IncidentID > 0 {
			schedIncidentSet[l.IncidentID] = struct{}{}
		}
		if l.Status == model.NotifyStatusThrottled {
			throttledLogs++
		}
		if l.IsNight && l.Status != model.NotifyStatusSuppressed && l.Status != model.NotifyStatusThrottled {
			out.TotalNightCalls++
			if l.UserID > 0 {
				out.NightCallByUser[l.UserID]++
			}
			dayKey := l.CreatedAt.Local().Format("01-02")
			nightByDay[dayKey]++
		}
	}

	maintSuppressed, err := s.SumMaintenanceSuppressedInRange(ctx, scheduleID, start, end)
	if err != nil {
		return nil, err
	}
	out.SuppressedAlertsCount = int(maintSuppressed) + throttledLogs

	var incidents []*alertmodel.Incident
	shouldQueryIncidents := scheduleID == 0 || len(schedIncidentSet) > 0
	if shouldQueryIncidents {
		incQ := s.db.WithContext(ctx).Model(&alertmodel.Incident{}).
			Where("first_fired_at >= ? AND first_fired_at <= ?", queryStart, queryEnd)
		if scheduleID > 0 {
			ids := make([]uint64, 0, len(schedIncidentSet))
			for id := range schedIncidentSet {
				ids = append(ids, id)
			}
			incQ = incQ.Where("id IN ?", ids)
		}
		if err := incQ.Order("first_fired_at asc, id asc").Find(&incidents).Error; err != nil {
			return nil, fmt.Errorf("query analytics incidents: %w", err)
		}
	}

	dayIncidents := make(map[string]int)
	type ruleAccum struct {
		RuleKey       string
		RuleName      string
		Severity      string
		IncidentCount int
		TriggerCount  int
		TotalDurSec   float64
	}
	ruleMap := make(map[string]*ruleAccum)

	for _, inc := range incidents {
		if inc == nil || inc.FirstFiredAt.Before(start) || inc.FirstFiredAt.After(end) {
			continue
		}
		out.TotalIncidents++

		if inc.AcknowledgedAt != nil || inc.Status == alertmodel.StatusAcknowledged {
			out.AcknowledgedCount++
		}
		if inc.ResolvedAt != nil || inc.Status == alertmodel.StatusResolved {
			out.ResolvedCount++
		}
		if inc.Status == alertmodel.StatusOpen || inc.Status == alertmodel.StatusSilenced {
			out.OpenCount++
		}

		sev := strings.ToLower(strings.TrimSpace(inc.Severity))
		if sev == "" {
			sev = "unknown"
		}
		sevMetric, exists := out.SeverityMetrics[sev]
		if !exists {
			sevMetric = &SeverityMetric{Severity: sev}
			out.SeverityMetrics[sev] = sevMetric
		}
		sevMetric.Count++

		if inc.AcknowledgedAt != nil && !inc.AcknowledgedAt.Before(inc.FirstFiredAt) {
			diff := inc.AcknowledgedAt.Sub(inc.FirstFiredAt).Seconds()
			if diff >= 0 && diff < 86400*30 {
				out.MTTASamples = append(out.MTTASamples, diff)
				sevMetric.MTTASamples = append(sevMetric.MTTASamples, diff)
				if inc.AcknowledgedBy != nil && *inc.AcknowledgedBy > 0 {
					out.UserMTTASamples[*inc.AcknowledgedBy] = append(out.UserMTTASamples[*inc.AcknowledgedBy], diff)
				}
			}
		}
		if inc.AcknowledgedBy != nil && *inc.AcknowledgedBy > 0 {
			out.UserHandleCount[*inc.AcknowledgedBy]++
		}

		if inc.ResolvedAt != nil && !inc.ResolvedAt.Before(inc.FirstFiredAt) {
			diff := inc.ResolvedAt.Sub(inc.FirstFiredAt).Seconds()
			if diff >= 0 && diff < 86400*30 {
				out.MTTRSamples = append(out.MTTRSamples, diff)
				sevMetric.MTTRSamples = append(sevMetric.MTTRSamples, diff)
			}
		}
		if inc.ResolvedBy != nil && *inc.ResolvedBy > 0 {
			out.UserResolveCount[*inc.ResolvedBy]++
		}

		dayKey := inc.FirstFiredAt.Local().Format("01-02")
		dayIncidents[dayKey]++

		rName := strings.TrimSpace(inc.RuleName)
		if rName == "" || rName == "????" {
			rName = strings.TrimSpace(inc.Rule)
		}
		if rName == "" {
			rName = "unspecified_rule"
		}
		rKey := strings.TrimSpace(inc.Rule)
		if rKey == "" {
			rKey = rName
		}
		groupKey := rKey + "|" + sev
		acc, ok := ruleMap[groupKey]
		if !ok {
			acc = &ruleAccum{
				RuleKey:  rKey,
				RuleName: rName,
				Severity: sev,
			}
			ruleMap[groupKey] = acc
		}
		acc.IncidentCount++
		triggers := int(inc.EventCount)
		if triggers <= 0 {
			triggers = 1
		}
		acc.TriggerCount += triggers
		endRef := inc.LastFiredAt
		if inc.ResolvedAt != nil && !inc.ResolvedAt.Before(inc.FirstFiredAt) {
			endRef = *inc.ResolvedAt
		}
		if endRef.After(inc.FirstFiredAt) {
			dur := endRef.Sub(inc.FirstFiredAt).Seconds()
			if dur < 86400*30 {
				acc.TotalDurSec += dur
			}
		}
	}

	// Build contiguous daily trend buckets from start.Local() through end.Local().
	startDay := time.Date(start.Local().Year(), start.Local().Month(), start.Local().Day(), 0, 0, 0, 0, time.Local)
	endDay := time.Date(end.Local().Year(), end.Local().Month(), end.Local().Day(), 0, 0, 0, 0, time.Local)
	for cur := startDay; !cur.After(endDay); cur = cur.AddDate(0, 0, 1) {
		key := cur.Format("01-02")
		out.DayTrends = append(out.DayTrends, DayMetric{
			Date:          key,
			IncidentCount: dayIncidents[key],
			NightCount:    nightByDay[key],
		})
	}

	out.TopRules = make([]RuleNoiseStat, 0, len(ruleMap))
	for _, acc := range ruleMap {
		avgDur := 0
		if acc.IncidentCount > 0 {
			avgDur = int(acc.TotalDurSec / float64(acc.IncidentCount))
		}
		out.TopRules = append(out.TopRules, RuleNoiseStat{
			RuleKey:        acc.RuleKey,
			RuleName:       acc.RuleName,
			Severity:       acc.Severity,
			IncidentCount:  acc.IncidentCount,
			TriggerCount:   acc.TriggerCount,
			AvgDurationSec: avgDur,
		})
	}
	sort.Slice(out.TopRules, func(i, j int) bool {
		if out.TopRules[i].TriggerCount == out.TopRules[j].TriggerCount {
			return out.TopRules[i].RuleName < out.TopRules[j].RuleName
		}
		return out.TopRules[i].TriggerCount > out.TopRules[j].TriggerCount
	})
	if len(out.TopRules) > 10 {
		out.TopRules = out.TopRules[:10]
	}

	return out, nil
}
