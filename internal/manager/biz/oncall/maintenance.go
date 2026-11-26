package oncall

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// ParseMaintenanceMatchers decodes JSON label matchers stored on a maintenance window.
func ParseMaintenanceMatchers(mw *model.MaintenanceWindow) []LabelMatcher {
	if mw == nil || strings.TrimSpace(mw.MatcherJSON) == "" {
		return nil
	}
	var matchers []LabelMatcher
	if err := json.Unmarshal([]byte(mw.MatcherJSON), &matchers); err != nil {
		return nil
	}
	out := make([]LabelMatcher, 0, len(matchers))
	for _, m := range matchers {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// MatchMaintenanceWindow evaluates active windows covering at and matching scheduleID + alert labels.
// When multiple windows match, suppress_all takes precedence over suppress_voice.
func MatchMaintenanceWindow(
	ctx context.Context,
	s *store.Store,
	rm *RouteMatcher,
	scheduleID uint64,
	labels map[string]string,
	at time.Time,
) (*model.MaintenanceWindow, error) {
	if s == nil {
		return nil, nil
	}
	if rm == nil {
		rm = NewRouteMatcher(s)
	}
	windows, err := s.ListActiveMaintenanceWindowsAt(ctx, at)
	if err != nil {
		return nil, err
	}

	var voiceFallback *model.MaintenanceWindow
	for _, mw := range windows {
		if mw == nil {
			continue
		}
		if mw.ScheduleID != 0 && mw.ScheduleID != scheduleID {
			continue
		}
		matchers := ParseMaintenanceMatchers(mw)
		matched := true
		for _, m := range matchers {
			val := ""
			if labels != nil {
				val = labels[m.Name]
			}
			if !rm.evaluateMatcher(m, val) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if mw.Action == model.MaintenanceActionSuppressAll {
			return mw, nil
		}
		if voiceFallback == nil {
			voiceFallback = mw
		}
	}
	return voiceFallback, nil
}

// ComputeMaintenanceRuntimeStatus calculates display runtime status against current UTC time.
func ComputeMaintenanceRuntimeStatus(mw *model.MaintenanceWindow, now time.Time) string {
	if mw == nil {
		return "unknown"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if mw.Status == model.MaintenanceStatusCancelled {
		return "cancelled"
	}
	if now.Before(mw.StartsAt) {
		return "scheduled"
	}
	if !now.Before(mw.EndsAt) {
		return "expired"
	}
	return "running"
}
