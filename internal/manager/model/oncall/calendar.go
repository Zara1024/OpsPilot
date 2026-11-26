package oncall

import "time"

const (
	DayTypeHoliday = "holiday"
	DayTypeWorkday = "workday"

	CalendarRegionCN = "CN"

	CalendarSourceBuiltin = "builtin"
	CalendarSourceCustom  = "custom"
)

// CalendarDay stores statutory holiday or makeup workday overrides per calendar date and region.
type CalendarDay struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Date      string    `gorm:"column:date;size:10;not null;uniqueIndex:idx_oncall_cal_date_region,priority:1" json:"date"`
	Region    string    `gorm:"column:region;size:16;not null;default:'CN';uniqueIndex:idx_oncall_cal_date_region,priority:2" json:"region"`
	DayType   string    `gorm:"column:day_type;size:16;not null" json:"day_type"`
	Name      string    `gorm:"column:name;size:64;not null;default:''" json:"name"`
	Source    string    `gorm:"column:source;size:16;not null;default:'custom'" json:"source"`
	CreatedBy uint64    `gorm:"column:created_by;not null;default:0" json:"created_by"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (CalendarDay) TableName() string {
	return "oncall_calendar_days"
}
