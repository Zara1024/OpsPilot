package store

import (
	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// Migrate AutoMigrates all On-Call tables into the database.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.Schedule{},
		&model.Rotation{},
		&model.Override{},
		&model.Route{},
		&model.EscalationPolicy{},
		&model.UserNotificationRule{},
		&model.UserCalendarToken{},
		&model.HandoffLog{},
	)
}
