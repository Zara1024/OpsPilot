package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type ShiftSlot struct {
	ScheduleID       uint64    `json:"schedule_id"`
	ScheduleName     string    `json:"schedule_name"`
	RotationID       uint64    `json:"rotation_id"`
	RotationName     string    `json:"rotation_name"`
	Tier             uint8     `json:"tier"` // 1=Primary, 2=Secondary
	UserID           uint64    `json:"user_id"`
	UserName         string    `json:"user_name"`
	UserEmail        string    `json:"user_email"`
	UserPhone        string    `json:"user_phone"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	BaseEndTime      time.Time `json:"base_end_time,omitempty"`
	IsOverride       bool      `json:"is_override"`
	OriginalUserID   *uint64   `json:"original_user_id,omitempty"`
	OriginalUserName string    `json:"original_user_name,omitempty"`
	Reason           string    `json:"reason,omitempty"`
}

type LiveOnCallStatus struct {
	ScheduleID       uint64     `json:"schedule_id"`
	ScheduleName     string     `json:"schedule_name"`
	PrimaryUser      *ShiftSlot `json:"primary_user,omitempty"`
	SecondaryUser    *ShiftSlot `json:"secondary_user,omitempty"`
	NextShift        *ShiftSlot `json:"next_shift,omitempty"`
	RemainingSeconds int64      `json:"remaining_seconds"`
	HandoffTime      time.Time  `json:"handoff_time"`
}

type Scheduler struct {
	store *store.Store
}

func NewScheduler(store *store.Store) *Scheduler {
	return &Scheduler{store: store}
}

// ComputeScheduleShifts generates all final active shifts within [windowStart, windowEnd]
// for the given schedule, applying rotation layers and override replacements.
func (s *Scheduler) ComputeScheduleShifts(ctx context.Context, scheduleID uint64, windowStart, windowEnd time.Time) ([]*ShiftSlot, error) {
	sched, err := s.store.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}

	rotations, err := s.store.ListRotationsBySchedule(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("list rotations: %w", err)
	}

	overrides, err := s.store.ListOverridesInWindow(ctx, scheduleID, windowStart, windowEnd)
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}

	var allShifts []*ShiftSlot
	for _, rot := range rotations {
		shifts := s.computeBaseRotationShifts(sched, rot, windowStart, windowEnd)
		// Apply overrides
		shifts = s.applyOverrides(rot, shifts, overrides)
		allShifts = append(allShifts, shifts...)
	}

	// Collect user IDs for batch hydration
	userIDSet := make(map[uint64]struct{})
	for _, shift := range allShifts {
		if shift.UserID > 0 {
			userIDSet[shift.UserID] = struct{}{}
		}
		if shift.OriginalUserID != nil && *shift.OriginalUserID > 0 {
			userIDSet[*shift.OriginalUserID] = struct{}{}
		}
	}

	userIDs := make([]uint64, 0, len(userIDSet))
	for uid := range userIDSet {
		userIDs = append(userIDs, uid)
	}

	usersMap, err := s.store.GetUsersByIDs(ctx, userIDs)
	if err == nil {
		for _, shift := range allShifts {
			if u, ok := usersMap[shift.UserID]; ok {
				shift.UserName = u.DisplayName
				shift.UserEmail = u.Email
				shift.UserPhone = u.Phone
			}
			if shift.OriginalUserID != nil {
				if orig, ok := usersMap[*shift.OriginalUserID]; ok {
					shift.OriginalUserName = orig.DisplayName
				}
			}
		}
	}

	// Sort by StartTime ascending, then Tier ascending
	sort.Slice(allShifts, func(i, j int) bool {
		if allShifts[i].StartTime.Equal(allShifts[j].StartTime) {
			return allShifts[i].Tier < allShifts[j].Tier
		}
		return allShifts[i].StartTime.Before(allShifts[j].StartTime)
	})

	return allShifts, nil
}

func (s *Scheduler) computeBaseRotationShifts(sched *model.Schedule, rot *model.Rotation, start, end time.Time) []*ShiftSlot {
	var userIDs []uint64
	if err := json.Unmarshal([]byte(rot.UsersJSON), &userIDs); err != nil || len(userIDs) == 0 {
		return nil
	}

	shiftLenSec := int64(rot.ShiftLengthSeconds)
	if shiftLenSec <= 0 {
		shiftLenSec = 86400
	}
	shiftDuration := time.Duration(shiftLenSec) * time.Second

	anchor := rot.EffectiveFrom
	if anchor.IsZero() {
		anchor = sched.CreatedAt
	}
	if sched.HandoffTime != "" {
		loc, err := time.LoadLocation(sched.Timezone)
		if err != nil || loc == nil {
			loc = time.FixedZone("CST", 8*3600)
		}
		anchorInLoc := anchor.In(loc)
		var hh, mm, ss int
		if _, scanErr := fmt.Sscanf(sched.HandoffTime, "%d:%d:%d", &hh, &mm, &ss); scanErr == nil {
			anchor = time.Date(anchorInLoc.Year(), anchorInLoc.Month(), anchorInLoc.Day(), hh, mm, ss, 0, loc).UTC()
		}
	}

	// Calculate index offset from anchor
	offsetSec := start.Sub(anchor).Seconds()
	startIndex := int64(math.Floor(offsetSec / float64(shiftLenSec)))

	var slots []*ShiftSlot
	numUsers := int64(len(userIDs))

	for i := startIndex; ; i++ {
		shiftStart := anchor.Add(time.Duration(i*shiftLenSec) * time.Second)
		shiftEnd := shiftStart.Add(shiftDuration)

		if shiftStart.After(end) || shiftStart.Equal(end) {
			break
		}
		if shiftEnd.Before(start) || shiftEnd.Equal(start) {
			continue
		}

		uIdx := ((i % numUsers) + numUsers) % numUsers
		uid := userIDs[uIdx]

		// Time restriction check
		if rot.TimeRestrictionType == model.TimeRestrictionWeekday {
			weekday := shiftStart.Weekday()
			if weekday == time.Saturday || weekday == time.Sunday {
				continue
			}
		} else if rot.TimeRestrictionType == model.TimeRestrictionTimeOfDay && rot.RestrictionStartTime != "" && rot.RestrictionEndTime != "" {
			loc, _ := time.LoadLocation(sched.Timezone)
			if loc == nil {
				loc = time.FixedZone("CST", 8*3600)
			}
			var sh, sm, ss, eh, em, es int
			_, err1 := fmt.Sscanf(rot.RestrictionStartTime, "%d:%d:%d", &sh, &sm, &ss)
			_, err2 := fmt.Sscanf(rot.RestrictionEndTime, "%d:%d:%d", &eh, &em, &es)
			if err1 == nil && err2 == nil {
				localStart := shiftStart.In(loc)
				dayStart := time.Date(localStart.Year(), localStart.Month(), localStart.Day(), sh, sm, ss, 0, loc)
				var dayEnd time.Time
				if eh > sh || (eh == sh && em > sm) {
					dayEnd = time.Date(localStart.Year(), localStart.Month(), localStart.Day(), eh, em, es, 0, loc)
				} else {
					// Overnight shift (e.g. 21:00 to 09:00 next day)
					dayEnd = time.Date(localStart.Year(), localStart.Month(), localStart.Day()+1, eh, em, es, 0, loc)
				}
				shiftStart = dayStart.UTC()
				shiftEnd = dayEnd.UTC()
			}
		}

		slots = append(slots, &ShiftSlot{
			ScheduleID:   sched.ID,
			ScheduleName: sched.Name,
			RotationID:   rot.ID,
			RotationName: rot.Name,
			Tier:         rot.Tier,
			UserID:       uid,
			StartTime:    shiftStart,
			EndTime:      shiftEnd,
			BaseEndTime:  shiftEnd,
			IsOverride:   false,
		})
	}

	return slots
}

func (s *Scheduler) applyOverrides(rot *model.Rotation, baseShifts []*ShiftSlot, overrides []*model.Override) []*ShiftSlot {
	if len(overrides) == 0 || len(baseShifts) == 0 {
		return baseShifts
	}

	// Filter relevant overrides: only approved overrides for this rotation (or global to schedule)
	var relevant []*model.Override
	for _, o := range overrides {
		if (o.RotationID == 0 || o.RotationID == rot.ID) && (o.Status == model.OverrideStatusApproved || o.Status == "") {
			relevant = append(relevant, o)
		}
	}
	if len(relevant) == 0 {
		return baseShifts
	}

	var result []*ShiftSlot
	for _, slot := range baseShifts {
		currentSlots := []*ShiftSlot{slot}

		for _, ov := range relevant {
			var nextSlots []*ShiftSlot
			for _, cs := range currentSlots {
				// Check overlap
				if !cs.StartTime.Before(ov.EndTime) || !cs.EndTime.After(ov.StartTime) {
					// No overlap
					nextSlots = append(nextSlots, cs)
					continue
				}

				// Overlapped: split cs into up to 3 segments
				overlapStart := cs.StartTime
				if ov.StartTime.After(overlapStart) {
					overlapStart = ov.StartTime
				}
				overlapEnd := cs.EndTime
				if ov.EndTime.Before(overlapEnd) {
					overlapEnd = ov.EndTime
				}

				// 1. Before segment
				if cs.StartTime.Before(overlapStart) {
					before := *cs
					before.EndTime = overlapStart
					nextSlots = append(nextSlots, &before)
				}

				// 2. Overlap segment
				if overlapStart.Before(overlapEnd) {
					sub := *cs
					sub.StartTime = overlapStart
					sub.EndTime = overlapEnd
					sub.UserID = ov.SubstituteUserID
					sub.IsOverride = true
					origID := cs.UserID
					sub.OriginalUserID = &origID
					sub.Reason = ov.Reason
					nextSlots = append(nextSlots, &sub)
				}

				// 3. After segment
				if overlapEnd.Before(cs.EndTime) {
					after := *cs
					after.StartTime = overlapEnd
					nextSlots = append(nextSlots, &after)
				}
			}
			currentSlots = nextSlots
		}

		result = append(result, currentSlots...)
	}

	return result
}

// GetLiveStatus computes the active on-call engineers (Primary & Secondary) for the schedule at now.
func (s *Scheduler) GetLiveStatus(ctx context.Context, scheduleID uint64, now time.Time) (*LiveOnCallStatus, error) {
	sched, err := s.store.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, err
	}

	// Query window: [now - 24h, now + 48h]
	windowStart := now.Add(-24 * time.Hour)
	windowEnd := now.Add(48 * time.Hour)

	shifts, err := s.ComputeScheduleShifts(ctx, scheduleID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}

	status := &LiveOnCallStatus{
		ScheduleID:   sched.ID,
		ScheduleName: sched.Name,
	}

	var activePrimary *ShiftSlot
	var upcomingPrimary []*ShiftSlot

	for _, shift := range shifts {
		// Active shift
		if !now.Before(shift.StartTime) && now.Before(shift.EndTime) {
			if shift.Tier == model.TierPrimary && status.PrimaryUser == nil {
				cp := *shift
				status.PrimaryUser = &cp
				activePrimary = shift
			} else if shift.Tier == model.TierSecondary && status.SecondaryUser == nil {
				cp := *shift
				status.SecondaryUser = &cp
			}
		}

		// Upcoming primary shift
		if shift.Tier == model.TierPrimary && shift.StartTime.After(now) {
			upcomingPrimary = append(upcomingPrimary, shift)
		}
	}

	if activePrimary != nil {
		handoffTime := activePrimary.BaseEndTime
		if handoffTime.IsZero() {
			handoffTime = activePrimary.EndTime
		}
		status.HandoffTime = handoffTime
		status.RemainingSeconds = int64(handoffTime.Sub(now).Seconds())

		// Find the next shift in the upcoming rotation (i.e. start >= handoffTime)
		for _, up := range upcomingPrimary {
			if !up.StartTime.Before(handoffTime) {
				status.NextShift = up
				break
			}
		}
		// If no shift starts exactly at or after handoffTime, fallback to first upcoming if different user
		if status.NextShift == nil && len(upcomingPrimary) > 0 {
			for _, up := range upcomingPrimary {
				if up.UserID != activePrimary.UserID {
					status.NextShift = up
					break
				}
			}
		}
	} else if len(upcomingPrimary) > 0 {
		status.NextShift = upcomingPrimary[0]
		status.HandoffTime = upcomingPrimary[0].StartTime
		status.RemainingSeconds = int64(upcomingPrimary[0].StartTime.Sub(now).Seconds())
	}

	return status, nil
}
