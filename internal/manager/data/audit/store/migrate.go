// Package store is the GORM-backed persistence layer for HLD-010
// audit_logs. Migration is composed from cmd/opspilot via dbx.RunMigrations.
package store

import (
	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/audit"
)

// Migrate registers the audit_logs table.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&model.Log{})
}
