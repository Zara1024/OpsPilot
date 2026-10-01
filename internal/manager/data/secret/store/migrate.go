package store

import (
	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/secret"
)

// Migrate AutoMigrates the secrets table. Registered in cmd/opspilot/main.go
// alongside the other data-package migrations.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&model.Secret{})
}
