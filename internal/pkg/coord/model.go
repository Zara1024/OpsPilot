// Package coord 提供基于数据库的分布式租约与选主能力，用于在多副本 OpsPilot
// 部署下协调全局唯一调度器、后台对账循环与滚动升级状态判断。
package coord

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ClusterLease 为 `cluster_leases` 持久化表模型。
// 通过 `HolderID` 与单调递增的 `FencingToken` 保证任一时刻至多只有一个副本持有有效租约。
type ClusterLease struct {
	LeaseKey     string    `gorm:"column:lease_key;primaryKey;size:128"`
	HolderID     string    `gorm:"column:holder_id;size:128;not null;default:''"`
	FencingToken uint64    `gorm:"column:fencing_token;not null;default:0"`
	AcquiredAt   time.Time `gorm:"column:acquired_at;not null"`
	RenewedAt    time.Time `gorm:"column:renewed_at;not null"`
	ExpiresAt    time.Time `gorm:"column:expires_at;not null;index"`
}

// TableName 指定底层数据库表名。
func (ClusterLease) TableName() string { return "cluster_leases" }

// Lease 表示对外暴露的租约快照与栅栏令牌。
type Lease struct {
	Key          string
	HolderID     string
	FencingToken uint64
	AcquiredAt   time.Time
	RenewedAt    time.Time
	ExpiresAt    time.Time
}

func leaseFromModel(m ClusterLease) Lease {
	return Lease{
		Key:          m.LeaseKey,
		HolderID:     m.HolderID,
		FencingToken: m.FencingToken,
		AcquiredAt:   m.AcquiredAt.UTC(),
		RenewedAt:    m.RenewedAt.UTC(),
		ExpiresAt:    m.ExpiresAt.UTC(),
	}
}

// Migrate 创建或迁移 `cluster_leases` 表，可幂等重复执行。
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("coord migrate: nil db")
	}
	if err := db.AutoMigrate(&ClusterLease{}); err != nil {
		return fmt.Errorf("coord migrate cluster_leases: %w", err)
	}
	return nil
}

// NewInstanceID 生成当前 Manager 实例的唯一标识。
// 若显式传入 `explicit` 则优先使用；否则按 `<sanitized-hostname>-<8-char-uuid>` 生成并截断至 128 字符。
func NewInstanceID(explicit string) string {
	trimmed := strings.TrimSpace(explicit)
	if trimmed != "" {
		if len(trimmed) > 128 {
			return trimmed[:128]
		}
		return trimmed
	}

	host, err := os.Hostname()
	if err != nil {
		host = "opspilot"
	}
	host = sanitizeInstanceSegment(host)
	if host == "" {
		host = "opspilot"
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	maxHost := 128 - 1 - len(suffix)
	if len(host) > maxHost {
		host = host[:maxHost]
	}
	return host + "-" + suffix
}

func sanitizeInstanceSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteByte(c)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-_.")
}
