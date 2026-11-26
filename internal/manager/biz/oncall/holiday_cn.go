package oncall

import (
	"sort"
	"strings"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

type cnPresetDay struct {
	Date    string
	DayType string
	Name    string
}

var builtinCNPresets = map[int][]cnPresetDay{
	2025: {
		{Date: "2025-01-01", DayType: model.DayTypeHoliday, Name: "元旦"},
		{Date: "2025-01-26", DayType: model.DayTypeWorkday, Name: "春节调休补班"},
		{Date: "2025-01-28", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-01-29", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-01-30", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-01-31", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-02-01", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-02-02", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-02-03", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-02-04", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2025-02-08", DayType: model.DayTypeWorkday, Name: "春节调休补班"},
		{Date: "2025-04-04", DayType: model.DayTypeHoliday, Name: "清明节"},
		{Date: "2025-04-05", DayType: model.DayTypeHoliday, Name: "清明节"},
		{Date: "2025-04-06", DayType: model.DayTypeHoliday, Name: "清明节"},
		{Date: "2025-04-27", DayType: model.DayTypeWorkday, Name: "劳动节调休补班"},
		{Date: "2025-05-01", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2025-05-02", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2025-05-03", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2025-05-04", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2025-05-05", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2025-05-31", DayType: model.DayTypeHoliday, Name: "端午节"},
		{Date: "2025-06-01", DayType: model.DayTypeHoliday, Name: "端午节"},
		{Date: "2025-06-02", DayType: model.DayTypeHoliday, Name: "端午节"},
		{Date: "2025-09-28", DayType: model.DayTypeWorkday, Name: "国庆中秋调休补班"},
		{Date: "2025-10-01", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-02", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-03", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-04", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-05", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-06", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-07", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-08", DayType: model.DayTypeHoliday, Name: "国庆节/中秋节"},
		{Date: "2025-10-11", DayType: model.DayTypeWorkday, Name: "国庆中秋调休补班"},
	},
	2026: {
		{Date: "2026-01-01", DayType: model.DayTypeHoliday, Name: "元旦"},
		{Date: "2026-01-02", DayType: model.DayTypeHoliday, Name: "元旦"},
		{Date: "2026-01-03", DayType: model.DayTypeHoliday, Name: "元旦"},
		{Date: "2026-01-04", DayType: model.DayTypeWorkday, Name: "元旦调休补班"},
		{Date: "2026-02-14", DayType: model.DayTypeWorkday, Name: "春节调休补班"},
		{Date: "2026-02-15", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-16", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-17", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-18", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-19", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-20", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-21", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-22", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-23", DayType: model.DayTypeHoliday, Name: "春节"},
		{Date: "2026-02-28", DayType: model.DayTypeWorkday, Name: "春节调休补班"},
		{Date: "2026-04-04", DayType: model.DayTypeHoliday, Name: "清明节"},
		{Date: "2026-04-05", DayType: model.DayTypeHoliday, Name: "清明节"},
		{Date: "2026-04-06", DayType: model.DayTypeHoliday, Name: "清明节"},
		{Date: "2026-05-01", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2026-05-02", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2026-05-03", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2026-05-04", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2026-05-05", DayType: model.DayTypeHoliday, Name: "劳动节"},
		{Date: "2026-05-09", DayType: model.DayTypeWorkday, Name: "劳动节调休补班"},
		{Date: "2026-06-19", DayType: model.DayTypeHoliday, Name: "端午节"},
		{Date: "2026-06-20", DayType: model.DayTypeHoliday, Name: "端午节"},
		{Date: "2026-06-21", DayType: model.DayTypeHoliday, Name: "端午节"},
		{Date: "2026-09-20", DayType: model.DayTypeWorkday, Name: "国庆调休补班"},
		{Date: "2026-09-25", DayType: model.DayTypeHoliday, Name: "中秋节"},
		{Date: "2026-09-26", DayType: model.DayTypeHoliday, Name: "中秋节"},
		{Date: "2026-09-27", DayType: model.DayTypeHoliday, Name: "中秋节"},
		{Date: "2026-10-01", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-02", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-03", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-04", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-05", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-06", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-07", DayType: model.DayTypeHoliday, Name: "国庆节"},
		{Date: "2026-10-10", DayType: model.DayTypeWorkday, Name: "国庆调休补班"},
	},
}

// BuiltinCNCalendarDays returns sorted official State Council holiday & makeup workday presets for the requested year.
func BuiltinCNCalendarDays(year int) []model.CalendarDay {
	presets := builtinCNPresets[year]
	if len(presets) == 0 {
		return nil
	}
	out := make([]model.CalendarDay, 0, len(presets))
	for _, p := range presets {
		out = append(out, model.CalendarDay{
			Date:    p.Date,
			Region:  model.CalendarRegionCN,
			DayType: p.DayType,
			Name:    p.Name,
			Source:  model.CalendarSourceBuiltin,
		})
	}
	return out
}

// BuiltinCNYears returns all years that currently have built-in State Council presets.
func BuiltinCNYears() []int {
	years := make([]int, 0, len(builtinCNPresets))
	for y := range builtinCNPresets {
		years = append(years, y)
	}
	sort.Ints(years)
	return years
}

// BuildCNCalendarMap merges built-in presets for years with DB overrides (DB entries win on conflicts).
func BuildCNCalendarMap(dbDays []*model.CalendarDay, years ...int) map[string]*model.CalendarDay {
	out := make(map[string]*model.CalendarDay)
	for _, yr := range years {
		for _, p := range BuiltinCNCalendarDays(yr) {
			cp := p
			out[cp.Date] = &cp
		}
	}
	for _, d := range dbDays {
		if d == nil || strings.TrimSpace(d.Date) == "" {
			continue
		}
		if d.Region != "" && d.Region != model.CalendarRegionCN {
			continue
		}
		cp := *d
		out[cp.Date] = &cp
	}
	return out
}

// MergeCNCalendarList builds a date-sorted merged list for a single year (DB overrides win over presets).
func MergeCNCalendarList(year int, dbDays []*model.CalendarDay) []*model.CalendarDay {
	m := BuildCNCalendarMap(dbDays, year)
	list := make([]*model.CalendarDay, 0, len(m))
	for _, item := range m {
		if item != nil {
			list = append(list, item)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Date < list[j].Date
	})
	return list
}

// ResolveDayKind resolves whether localDate (YYYY-MM-DD) is a Chinese statutory workday or holiday/weekend.
func ResolveDayKind(localDate string, weekday time.Weekday, calMap map[string]*model.CalendarDay) (isWorkday bool, dayTag string, dayLabel string) {
	if calMap != nil {
		if entry, ok := calMap[localDate]; ok && entry != nil {
			switch entry.DayType {
			case model.DayTypeHoliday:
				return false, "holiday", entry.Name
			case model.DayTypeWorkday:
				return true, "makeup_workday", entry.Name
			}
		}
	}
	if weekday == time.Saturday || weekday == time.Sunday {
		return false, "", ""
	}
	return true, "", ""
}
