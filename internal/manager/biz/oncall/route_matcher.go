package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type MatcherOperator string

const (
	OpEqual         MatcherOperator = "="
	OpNotEqual      MatcherOperator = "!="
	OpRegexMatch    MatcherOperator = "=~"
	OpNotRegexMatch MatcherOperator = "!~"
)

// LabelMatcher represents a single key-operator-value expression.
type LabelMatcher struct {
	Name     string          `json:"name"`
	Operator MatcherOperator `json:"operator"` // =, !=, =~, !~
	Value    string          `json:"value"`
}

// MatchResult represents the outcome of route matching.
type MatchResult struct {
	MatchedRoute *model.Route `json:"matched_route,omitempty"`
	ScheduleID   uint64       `json:"schedule_id"`
	ScheduleName string       `json:"schedule_name"`
	IsDefault    bool         `json:"is_default"`
	MatchedRules []string     `json:"matched_rules,omitempty"`
}

// RouteMatcher evaluates incoming alert labels against configured routing rules.
type RouteMatcher struct {
	store *store.Store
}

func NewRouteMatcher(store *store.Store) *RouteMatcher {
	return &RouteMatcher{store: store}
}

// MatchLabels finds the first matching route rule for the given labels map.
func (rm *RouteMatcher) MatchLabels(ctx context.Context, teamID uint64, labels map[string]string) (*MatchResult, error) {
	routes, err := rm.store.ListRoutes(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}

	for _, r := range routes {
		if !r.Enabled {
			continue
		}

		var matchers []LabelMatcher
		if err := json.Unmarshal([]byte(r.MatcherJSON), &matchers); err != nil {
			continue
		}

		matched := true
		var ruleHits []string
		for _, m := range matchers {
			val := labels[m.Name]
			if !rm.evaluateMatcher(m, val) {
				matched = false
				break
			}
			ruleHits = append(ruleHits, fmt.Sprintf("%s %s %s", m.Name, m.Operator, m.Value))
		}

		if matched && len(matchers) > 0 {
			sched, _ := rm.store.GetSchedule(ctx, r.ScheduleID)
			schedName := ""
			if sched != nil {
				schedName = sched.Name
			}
			return &MatchResult{
				MatchedRoute: r,
				ScheduleID:   r.ScheduleID,
				ScheduleName: schedName,
				IsDefault:    false,
				MatchedRules: ruleHits,
			}, nil
		}
	}

	// Fallback to first active schedule as default fallback route
	schedules, err := rm.store.ListSchedules(ctx, teamID, true)
	if err == nil && len(schedules) > 0 {
		return &MatchResult{
			ScheduleID:   schedules[0].ID,
			ScheduleName: schedules[0].Name,
			IsDefault:    true,
		}, nil
	}

	return &MatchResult{
		IsDefault: true,
	}, nil
}

func (rm *RouteMatcher) evaluateMatcher(m LabelMatcher, val string) bool {
	switch m.Operator {
	case OpEqual:
		return val == m.Value
	case OpNotEqual:
		return val != m.Value
	case OpRegexMatch:
		re, err := regexp.Compile(m.Value)
		if err != nil {
			return false
		}
		return re.MatchString(val)
	case OpNotRegexMatch:
		re, err := regexp.Compile(m.Value)
		if err != nil {
			return false
		}
		return !re.MatchString(val)
	default:
		return val == m.Value
	}
}
