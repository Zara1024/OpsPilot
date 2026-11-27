package coord

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Store 定义分布式租约存储抽象，便于后续按需扩展 Redis/Etcd 等后端实现。
type Store interface {
	TryAcquireOrRenew(ctx context.Context, key, holderID string, ttl time.Duration) (Lease, bool, error)
	Release(ctx context.Context, key, holderID string) error
	Get(ctx context.Context, key string) (*Lease, error)
	HasOtherActiveHolder(ctx context.Context, holderID string) (bool, error)
}

// GormStore 是基于 MySQL/SQLite 的分布式租约存储实现。
type GormStore struct {
	db  *gorm.DB
	now func() time.Time
}

// NewGormStore 构造基于 GORM 的租约存储。
func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *GormStore) setNow(nowFn func() time.Time) {
	if nowFn != nil {
		s.now = nowFn
	}
}

// TryAcquireOrRenew 原子尝试续约或抢占租约。
// 采用三段条件更新（续约自有 → 接管过期 → 首次插入），避免 MySQL 对单条 UPDATE SET 从左到右求值带来的竞态隐患。
func (s *GormStore) TryAcquireOrRenew(ctx context.Context, key, holderID string, ttl time.Duration) (Lease, bool, error) {
	if s == nil || s.db == nil {
		return Lease{}, false, fmt.Errorf("coord store not initialized")
	}
	key = strings.TrimSpace(key)
	holderID = strings.TrimSpace(holderID)
	if key == "" || holderID == "" || ttl <= 0 {
		return Lease{}, false, fmt.Errorf("coord acquire %q: key, holderID, and positive ttl are required", key)
	}

	now := s.now().UTC()
	expiresAt := now.Add(ttl)
	db := s.db.WithContext(ctx)

	// Path A: 续约自身仍未过期的租约（保持 fencing_token 与 acquired_at 不变）。
	res := db.Model(&ClusterLease{}).
		Where("lease_key = ? AND holder_id = ? AND expires_at > ?", key, holderID, now).
		Updates(map[string]any{
			"renewed_at": now,
			"expires_at": expiresAt,
		})
	if res.Error != nil {
		return Lease{}, false, fmt.Errorf("renew lease %q: %w", key, res.Error)
	}
	if res.RowsAffected == 1 {
		return s.loadAcquiredLease(ctx, key)
	}

	// Path B: 接管已过期的租约，并将 fencing_token 单调递增 1。
	res = db.Model(&ClusterLease{}).
		Where("lease_key = ? AND expires_at <= ?", key, now).
		Updates(map[string]any{
			"holder_id":     holderID,
			"fencing_token": gorm.Expr("fencing_token + ?", 1),
			"acquired_at":   now,
			"renewed_at":    now,
			"expires_at":    expiresAt,
		})
	if res.Error != nil {
		return Lease{}, false, fmt.Errorf("takeover expired lease %q: %w", key, res.Error)
	}
	if res.RowsAffected == 1 {
		return s.loadAcquiredLease(ctx, key)
	}

	// 先检查行是否已被活跃持有者占据，避免稳态轮询每次插入都产生主键冲突日志。
	var count int64
	if err := db.Model(&ClusterLease{}).Where("lease_key = ?", key).Count(&count).Error; err != nil {
		return Lease{}, false, fmt.Errorf("probe lease %q: %w", key, err)
	}
	if count > 0 {
		return Lease{}, false, nil
	}

	// Path C: 首次冷启动插入新租约（fencing_token = 1）。若并发抢注触发主键冲突则由胜出者持有。
	row := ClusterLease{
		LeaseKey:     key,
		HolderID:     holderID,
		FencingToken: 1,
		AcquiredAt:   now,
		RenewedAt:    now,
		ExpiresAt:    expiresAt,
	}
	if err := db.Create(&row).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return Lease{}, false, nil
		}
		return Lease{}, false, fmt.Errorf("insert lease %q: %w", key, err)
	}
	return leaseFromModel(row), true, nil
}

func (s *GormStore) loadAcquiredLease(ctx context.Context, key string) (Lease, bool, error) {
	var row ClusterLease
	if err := s.db.WithContext(ctx).Where("lease_key = ?", key).First(&row).Error; err != nil {
		return Lease{}, false, fmt.Errorf("reload lease %q: %w", key, err)
	}
	return leaseFromModel(row), true, nil
}

// Release 仅当当前实例仍是 holder 时主动使租约立即过期，让备副本可即时接管。
// 不删除记录行，以确保 fencing_token 在跨副本切换后仍保持全局严格单调递增。
func (s *GormStore) Release(ctx context.Context, key, holderID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("coord store not initialized")
	}
	key = strings.TrimSpace(key)
	holderID = strings.TrimSpace(holderID)
	if key == "" || holderID == "" {
		return nil
	}
	expiredAt := s.now().UTC().Add(-time.Second)
	if err := s.db.WithContext(ctx).Model(&ClusterLease{}).
		Where("lease_key = ? AND holder_id = ?", key, holderID).
		Update("expires_at", expiredAt).Error; err != nil {
		return fmt.Errorf("release lease %q: %w", key, err)
	}
	return nil
}

// Get 查询指定租约键的当前状态；若记录不存在则返回 (nil, nil)。
func (s *GormStore) Get(ctx context.Context, key string) (*Lease, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("coord store not initialized")
	}
	var row ClusterLease
	if err := s.db.WithContext(ctx).Where("lease_key = ?", strings.TrimSpace(key)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get lease %q: %w", key, err)
	}
	l := leaseFromModel(row)
	return &l, nil
}

// HasOtherActiveHolder 判断除当前 holderID 外是否仍有其他未过期租约的活跃副本，用于滚动更新安全判断。
func (s *GormStore) HasOtherActiveHolder(ctx context.Context, holderID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("coord store not initialized")
	}
	now := s.now().UTC()
	var count int64
	if err := s.db.WithContext(ctx).Model(&ClusterLease{}).
		Where("holder_id <> ? AND expires_at > ?", strings.TrimSpace(holderID), now).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("check active peer leases: %w", err)
	}
	return count > 0, nil
}

func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "Error 1062") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}
