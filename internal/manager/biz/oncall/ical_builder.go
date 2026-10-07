package oncall

import (
	"fmt"
	"time"

	ics "github.com/arran4/golang-ical"
)

type ICalBuilder struct{}

func NewICalBuilder() *ICalBuilder {
	return &ICalBuilder{}
}

// BuildCalendar serializes a list of ShiftSlots into an RFC 5545 compliant iCalendar string.
func (b *ICalBuilder) BuildCalendar(scheduleName string, shifts []*ShiftSlot) string {
	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	cal.SetXWRCalName(fmt.Sprintf("OpsPilot On-Call: %s", scheduleName))
	cal.SetXWRCalDesc("OpsPilot On-Call Schedule - Auto-synced via Webcal")
	cal.SetXWRTimezone("Asia/Shanghai")
	cal.SetProductId("-//OpsPilot//On-Call Calendar//CN")

	for _, s := range shifts {
		eventID := fmt.Sprintf("opspilot-shift-%d-%d-%d", s.ScheduleID, s.UserID, s.StartTime.Unix())
		event := cal.AddEvent(eventID)
		event.SetCreatedTime(time.Now())
		event.SetDtStampTime(time.Now())
		event.SetStartAt(s.StartTime)
		event.SetEndAt(s.EndTime)

		tierStr := "一线"
		if s.Tier == 2 {
			tierStr = "二线"
		}

		summary := fmt.Sprintf("[值班] %s - %s (%s)", s.ScheduleName, s.UserName, tierStr)
		if s.IsOverride {
			summary = fmt.Sprintf("[代班] %s - %s (%s)", s.ScheduleName, s.UserName, tierStr)
		}
		event.SetSummary(summary)

		desc := fmt.Sprintf("排班计划: %s\n轮转: %s (%s)\n值班人员: %s\n联系电话: %s\n邮箱: %s",
			s.ScheduleName, s.RotationName, tierStr, s.UserName, s.UserPhone, s.UserEmail)
		if s.IsOverride && s.OriginalUserName != "" {
			desc += fmt.Sprintf("\n(代班替岗，原定值班人: %s，事由: %s)", s.OriginalUserName, s.Reason)
		}
		event.SetDescription(desc)
	}

	return cal.Serialize()
}
