package store

import (
	"fmt"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/aicost"
)

// Migrate 注册 ai_usage_logs、ai_cost_quotas 与 ai_model_prices 表结构。
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("aicost migrate: nil db")
	}
	return db.AutoMigrate(
		&model.UsageLog{},
		&model.CostQuota{},
		&model.ModelPrice{},
	)
}
