package oncall

import (
	"context"
	"fmt"
	"sort"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type GapSlot struct {
	ScheduleID      uint64    `json:"schedule_id"`
	Tier            uint8     `json:"tier"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds int64     `json:"duration_seconds"`
	Description     string    `json:"description"`
}

type GapDetector struct {
	scheduler *Scheduler
}

func NewGapDetector(scheduler *Scheduler) *GapDetector {
	return &GapDetector{scheduler: scheduler}
}

// DetectGaps scans for uncovered time intervals in [windowStart, windowEnd] for Tier 1 (Primary) shifts.
func (d *GapDetector) DetectGaps(ctx context.Context, scheduleID uint64, windowStart, windowEnd time.Time) ([]*GapSlot, error) {
	if !windowStart.Before(windowEnd) {
		return nil, fmt.Errorf("invalid window: start must be before end")
	}

	shifts, err := d.scheduler.ComputeScheduleShifts(ctx, scheduleID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}

	// Filter only Tier 1 (Primary) shifts
	var primaryShifts []*ShiftSlot
	for _, s := range shifts {
		if s.Tier == model.TierPrimary {
			primaryShifts = append(primaryShifts, s)
		}
	}

	// Merge contiguous or overlapping covered intervals
	type interval struct {
		start time.Time
		end   time.Time
	}

	var covered []interval
	for _, s := range primaryShifts {
		start := s.StartTime
		if start.Before(windowStart) {
			start = windowStart
		}
		end := s.EndTime
		if end.After(windowEnd) {
			end = windowEnd
		}
		if start.Before(end) {
			covered = append(covered, interval{start: start, end: end})
		}
	}

	sort.Slice(covered, func(i, j int) bool {
		return covered[i].start.Before(covered[j].start)
	})

	var merged []interval
	for _, it := range covered {
		if len(merged) == 0 {
			merged = append(merged, it)
			continue
		}
		last := &merged[len(merged)-1]
		if !it.start.After(last.end) {
			// Overlapping or adjacent
			if it.end.After(last.end) {
				last.end = it.end
			}
		} else {
			merged = append(merged, it)
		}
	}

	// Find uncovered gaps
	var gaps []*GapSlot
	cursor := windowStart

	for _, it := range merged {
		if it.start.After(cursor) {
			gapDuration := int64(it.start.Sub(cursor).Seconds())
			gaps = append(gaps, &GapSlot{
				ScheduleID:      scheduleID,
				Tier:            model.TierPrimary,
				StartTime:       cursor,
				EndTime:         it.start,
				DurationSeconds: gapDuration,
				Description:     fmt.Sprintf("排班空缺：从 %s 至 %s 无一线值班人", cursor.Format("2006-01-02 15:04"), it.start.Format("2006-01-02 15:04")),
			})
		}
		if it.end.After(cursor) {
			cursor = it.end
		}
	}

	if cursor.Before(windowEnd) {
		gapDuration := int64(windowEnd.Sub(cursor).Seconds())
		gaps = append(gaps, &GapSlot{
			ScheduleID:      scheduleID,
			Tier:            model.TierPrimary,
			StartTime:       cursor,
			EndTime:         windowEnd,
			DurationSeconds: gapDuration,
			Description:     fmt.Sprintf("排班空缺：从 %s 至 %s 无一线值班人", cursor.Format("2006-01-02 15:04"), windowEnd.Format("2006-01-02 15:04")),
		})
	}

	return gaps, nil
}
