package oncall

import (
	"time"
)

type RotationInput struct {
	ID                   uint64    `json:"id,omitempty"`
	Name                 string    `json:"name"`
	Tier                 uint8     `json:"tier"`
	RotationType         string    `json:"rotation_type"`
	ShiftLengthSeconds   uint32    `json:"shift_length_seconds"`
	Users                []uint64  `json:"users"`
	EffectiveFrom        time.Time `json:"effective_from"`
	TimeRestrictionType  string    `json:"time_restriction_type"`
	RestrictionStartTime string    `json:"restriction_start_time"`
	RestrictionEndTime   string    `json:"restriction_end_time"`
}

type CreateScheduleRequest struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	Timezone             string          `json:"timezone"`
	HandoffTime          string          `json:"handoff_time"`
	ReminderAdvanceHours uint32          `json:"reminder_advance_hours"`
	RequireSwapApproval  bool            `json:"require_swap_approval"`
	Enabled              bool            `json:"enabled"`
	Rotations            []RotationInput `json:"rotations"`
}

type UpdateScheduleRequest struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	Timezone             string          `json:"timezone"`
	HandoffTime          string          `json:"handoff_time"`
	ReminderAdvanceHours uint32          `json:"reminder_advance_hours"`
	RequireSwapApproval  bool            `json:"require_swap_approval"`
	Enabled              bool            `json:"enabled"`
	Rotations            []RotationInput `json:"rotations"`
}

type CreateOverrideRequest struct {
	RotationID       uint64    `json:"rotation_id"`
	Type             string    `json:"type"` // "override" | "swap"
	OriginalUserID   uint64    `json:"original_user_id"`
	SubstituteUserID uint64    `json:"substitute_user_id"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	Reason           string    `json:"reason"`
	Status           string    `json:"status"` // "approved" | "pending"
}

type CalendarTokenResponse struct {
	Token     string `json:"token"`
	WebcalURL string `json:"webcal_url"`
	HttpURL   string `json:"http_url"`
}
