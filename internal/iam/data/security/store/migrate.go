// Package store 提供企业身份安全相关表的 GORM 持久化实现（登录限流、TOTP MFA、SSO、认证策略）。
package store

import (
	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
)

// Migrate 执行企业身份安全域全部数据表的自动迁移，严格兼容 SQLite 与 MySQL 8.0。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.LoginAttempt{},
		&model.UserMFA{},
		&model.SSOProvider{},
		&model.SSOIdentity{},
		&model.SSOState{},
		&model.AuthPolicy{},
	)
}
