package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/dbx"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

const authPolicySingletonID uint64 = 1

// Store 封装 iam_login_attempts / iam_user_mfa / iam_sso_* / iam_auth_policies 持久化操作。
type Store struct {
	db *gorm.DB
}

// New 构造 Store。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// DefaultAuthPolicy 返回未显式配置时的全局默认认证与限流策略。
func DefaultAuthPolicy() *model.AuthPolicy {
	return &model.AuthPolicy{
		ID:                      authPolicySingletonID,
		MFAEnforceMode:          model.MFAEnforceOff,
		AllowLocalPasswordLogin: true,
		LoginIPLimit:            20,
		LoginIPWindowSec:        300,
		LoginEmailLimit:         6,
		LoginEmailWindowSec:     900,
	}
}

// --- Auth Policy ---

// GetAuthPolicy 读取全局认证策略；若尚无记录则补全默认值后返回。
func (s *Store) GetAuthPolicy(ctx context.Context) (*model.AuthPolicy, error) {
	var row model.AuthPolicy
	err := dbx.WithContext(ctx, s.db).First(&row, authPolicySingletonID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DefaultAuthPolicy(), nil
		}
		return nil, err
	}
	if !model.IsValidMFAEnforceMode(row.MFAEnforceMode) {
		row.MFAEnforceMode = model.MFAEnforceOff
	}
	if row.LoginIPLimit <= 0 {
		row.LoginIPLimit = 20
	}
	if row.LoginIPWindowSec <= 0 {
		row.LoginIPWindowSec = 300
	}
	if row.LoginEmailLimit <= 0 {
		row.LoginEmailLimit = 6
	}
	if row.LoginEmailWindowSec <= 0 {
		row.LoginEmailWindowSec = 900
	}
	return &row, nil
}

// SaveAuthPolicy 持久化全局认证策略单例。
func (s *Store) SaveAuthPolicy(ctx context.Context, p *model.AuthPolicy) (*model.AuthPolicy, error) {
	if p == nil {
		return nil, errs.ErrInvalid
	}
	row := *p
	row.ID = authPolicySingletonID
	row.UpdatedAt = time.Now().UTC()
	err := dbx.Transaction(ctx, s.db, func(txCtx context.Context) error {
		tx := dbx.WithContext(txCtx, s.db)
		var existing model.AuthPolicy
		findErr := tx.First(&existing, authPolicySingletonID).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return tx.Create(&row).Error
		}
		if findErr != nil {
			return findErr
		}
		return tx.Model(&model.AuthPolicy{}).Where("id = ?", authPolicySingletonID).Updates(map[string]any{
			"mfa_enforce_mode":           row.MFAEnforceMode,
			"allow_local_password_login": row.AllowLocalPasswordLogin,
			"login_ip_limit":             row.LoginIPLimit,
			"login_ip_window_sec":        row.LoginIPWindowSec,
			"login_email_limit":          row.LoginEmailLimit,
			"login_email_window_sec":     row.LoginEmailWindowSec,
			"updated_at":                 row.UpdatedAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// --- Shared DB Login Throttle ---

// IsLoginBlocked 判断给定 scopeKey 在时间窗内是否已达到失败次数上限。
func (s *Store) IsLoginBlocked(ctx context.Context, scopeKey string, now time.Time, limit int, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 || strings.TrimSpace(scopeKey) == "" {
		return false, nil
	}
	var row model.LoginAttempt
	err := dbx.WithContext(ctx, s.db).Where("scope_key = ?", scopeKey).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if now.Sub(row.WindowStartedAt) > window {
		return false, nil
	}
	return row.Count >= limit, nil
}

// RecordLoginFailure 原子累加指定 scopeKey 的登录失败次数；窗口已过期时重置计数。
func (s *Store) RecordLoginFailure(ctx context.Context, scopeKey string, now time.Time, window time.Duration) error {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		return nil
	}
	return dbx.Transaction(ctx, s.db, func(txCtx context.Context) error {
		tx := dbx.WithContext(txCtx, s.db)
		// 先尝试幂等插入初始行；已存在则按窗口状态更新，避免多副本并发首次写入主键冲突。
		initial := model.LoginAttempt{
			ScopeKey:        scopeKey,
			Count:           0,
			WindowStartedAt: now,
			UpdatedAt:       now,
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope_key"}}, DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		var row model.LoginAttempt
		if err := tx.Where("scope_key = ?", scopeKey).First(&row).Error; err != nil {
			return err
		}
		if row.Count == 0 || now.Sub(row.WindowStartedAt) > window {
			return tx.Model(&model.LoginAttempt{}).
				Where("scope_key = ?", scopeKey).
				Updates(map[string]any{
					"count":             1,
					"window_started_at": now,
					"updated_at":        now,
				}).Error
		}
		return tx.Model(&model.LoginAttempt{}).
			Where("scope_key = ?", scopeKey).
			Updates(map[string]any{
				"count":      gorm.Expr("count + 1"),
				"updated_at": now,
			}).Error
	})
}

// ClearLoginAttempt 清除指定 scopeKey 的失败记录（例如用户登录成功后清零邮箱维度计数）。
func (s *Store) ClearLoginAttempt(ctx context.Context, scopeKey string) error {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		return nil
	}
	return dbx.WithContext(ctx, s.db).Where("scope_key = ?", scopeKey).Delete(&model.LoginAttempt{}).Error
}

// PruneExpiredAttempts 清理过期的限流计数行。
func (s *Store) PruneExpiredAttempts(ctx context.Context, cutoff time.Time) error {
	return dbx.WithContext(ctx, s.db).Where("updated_at < ?", cutoff).Delete(&model.LoginAttempt{}).Error
}

// --- User MFA ---

// GetUserMFA 按 userID 读取 MFA 记录。
func (s *Store) GetUserMFA(ctx context.Context, userID uint64) (*model.UserMFA, error) {
	var row model.UserMFA
	if err := dbx.WithContext(ctx, s.db).Where("user_id = ?", userID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// ListUserMFAEnabled 批量返回开启了 MFA 的 user_id 集合，用于用户列表高效装配状态。
func (s *Store) ListUserMFAEnabled(ctx context.Context) (map[uint64]bool, error) {
	var rows []model.UserMFA
	if err := dbx.WithContext(ctx, s.db).Select("user_id, enabled").Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]bool, len(rows))
	for _, r := range rows {
		if r.Enabled {
			out[r.UserID] = true
		}
	}
	return out, nil
}

// UpsertUserMFA 创建或覆盖指定用户的 MFA 记录。
func (s *Store) UpsertUserMFA(ctx context.Context, mfa *model.UserMFA) error {
	if mfa == nil || mfa.UserID == 0 {
		return errs.ErrInvalid
	}
	return dbx.Transaction(ctx, s.db, func(txCtx context.Context) error {
		tx := dbx.WithContext(txCtx, s.db)
		var existing model.UserMFA
		err := tx.Where("user_id = ?", mfa.UserID).First(&existing).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Create(mfa).Error
			}
			return err
		}
		mfa.ID = existing.ID
		mfa.CreatedAt = existing.CreatedAt
		return tx.Save(mfa).Error
	})
}

// DeleteUserMFA 删除用户的 MFA 记录（停用或管理员重置时使用）。
func (s *Store) DeleteUserMFA(ctx context.Context, userID uint64) error {
	res := dbx.WithContext(ctx, s.db).Where("user_id = ?", userID).Delete(&model.UserMFA{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// --- SSO Providers ---

// ListSSOProviders 列出全部或仅启用的 SSO 提供商。
func (s *Store) ListSSOProviders(ctx context.Context, onlyEnabled bool) ([]*model.SSOProvider, error) {
	q := dbx.WithContext(ctx, s.db).Model(&model.SSOProvider{})
	if onlyEnabled {
		q = q.Where("enabled = ?", true)
	}
	var rows []*model.SSOProvider
	if err := q.Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetSSOProviderByID 按 ID 获取 SSO 提供商。
func (s *Store) GetSSOProviderByID(ctx context.Context, id uint64) (*model.SSOProvider, error) {
	var row model.SSOProvider
	if err := dbx.WithContext(ctx, s.db).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// GetSSOProviderBySlug 按 slug 获取 SSO 提供商。
func (s *Store) GetSSOProviderBySlug(ctx context.Context, slug string) (*model.SSOProvider, error) {
	var row model.SSOProvider
	if err := dbx.WithContext(ctx, s.db).Where("slug = ?", strings.TrimSpace(slug)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// CreateSSOProvider 插入 SSO 提供商配置。
func (s *Store) CreateSSOProvider(ctx context.Context, p *model.SSOProvider) error {
	if p == nil {
		return errs.ErrInvalid
	}
	return dbx.WithContext(ctx, s.db).Create(p).Error
}

// UpdateSSOProvider 保存 SSO 提供商更新。
func (s *Store) UpdateSSOProvider(ctx context.Context, p *model.SSOProvider) error {
	if p == nil || p.ID == 0 {
		return errs.ErrInvalid
	}
	return dbx.WithContext(ctx, s.db).Save(p).Error
}

// DeleteSSOProvider 删除 SSO 提供商及其关联的外部身份绑定与未消费状态。
func (s *Store) DeleteSSOProvider(ctx context.Context, id uint64) error {
	return dbx.Transaction(ctx, s.db, func(txCtx context.Context) error {
		tx := dbx.WithContext(txCtx, s.db)
		var row model.SSOProvider
		if err := tx.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errs.ErrNotFound
			}
			return err
		}
		if err := tx.Where("provider_id = ?", id).Delete(&model.SSOIdentity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("provider_slug = ?", row.Slug).Delete(&model.SSOState{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.SSOProvider{}, id).Error
	})
}

// --- SSO State & Identities ---

// CreateSSOState 持久化一次性 SSO 状态。
func (s *Store) CreateSSOState(ctx context.Context, st *model.SSOState) error {
	if st == nil || strings.TrimSpace(st.State) == "" {
		return errs.ErrInvalid
	}
	// 最佳努力清理过期 state，不影响本次新登录 state 签发。
	if err := dbx.WithContext(ctx, s.db).Where("expires_at < ?", time.Now().UTC()).Delete(&model.SSOState{}).Error; err != nil {
		// best-effort opportunistic prune
	}
	return dbx.WithContext(ctx, s.db).Create(st).Error
}

// ConsumeSSOState 原子读取并作废一次性 SSO state，防重放与 CSRF。
func (s *Store) ConsumeSSOState(ctx context.Context, state string, now time.Time) (*model.SSOState, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, errs.ErrInvalid
	}
	var out model.SSOState
	err := dbx.Transaction(ctx, s.db, func(txCtx context.Context) error {
		tx := dbx.WithContext(txCtx, s.db)
		if err := tx.Where("state = ?", state).First(&out).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errs.ErrNotFound
			}
			return err
		}
		res := tx.Where("state = ?", state).Delete(&model.SSOState{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errs.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out.ExpiresAt.Before(now) {
		return nil, errs.ErrUnauthorized
	}
	return &out, nil
}

// GetSSOIdentity 按 providerID + externalSub 查找绑定身份。
func (s *Store) GetSSOIdentity(ctx context.Context, providerID uint64, externalSub string) (*model.SSOIdentity, error) {
	var row model.SSOIdentity
	err := dbx.WithContext(ctx, s.db).
		Where("provider_id = ? AND external_sub = ?", providerID, strings.TrimSpace(externalSub)).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// UpsertSSOIdentity 创建或更新用户的 SSO 外部身份关联。
func (s *Store) UpsertSSOIdentity(ctx context.Context, ident *model.SSOIdentity) error {
	if ident == nil || ident.ProviderID == 0 || strings.TrimSpace(ident.ExternalSub) == "" || ident.UserID == 0 {
		return errs.ErrInvalid
	}
	return dbx.Transaction(ctx, s.db, func(txCtx context.Context) error {
		tx := dbx.WithContext(txCtx, s.db)
		var existing model.SSOIdentity
		err := tx.Where("provider_id = ? AND external_sub = ?", ident.ProviderID, ident.ExternalSub).First(&existing).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Create(ident).Error
			}
			return err
		}
		ident.ID = existing.ID
		ident.CreatedAt = existing.CreatedAt
		return tx.Save(ident).Error
	})
}

// SSOIdentityWithProvider 兼容别名，真实定义已上移至 model 层以遵守单向依赖。
type SSOIdentityWithProvider = model.SSOIdentityWithProvider

// ListSSOIdentitiesByUser 列出用户已关联的 SSO 账号。
func (s *Store) ListSSOIdentitiesByUser(ctx context.Context, userID uint64) ([]model.SSOIdentityWithProvider, error) {
	var idents []model.SSOIdentity
	if err := dbx.WithContext(ctx, s.db).Where("user_id = ?", userID).Order("id asc").Find(&idents).Error; err != nil {
		return nil, err
	}
	if len(idents) == 0 {
		return nil, nil
	}
	providerIDs := make([]uint64, 0, len(idents))
	for _, item := range idents {
		providerIDs = append(providerIDs, item.ProviderID)
	}
	var providers []model.SSOProvider
	if err := dbx.WithContext(ctx, s.db).Where("id IN ?", providerIDs).Find(&providers).Error; err != nil {
		return nil, err
	}
	provByID := make(map[uint64]model.SSOProvider, len(providers))
	for _, p := range providers {
		provByID[p.ID] = p
	}
	out := make([]model.SSOIdentityWithProvider, 0, len(idents))
	for _, item := range idents {
		p := provByID[item.ProviderID]
		out = append(out, model.SSOIdentityWithProvider{
			SSOIdentity:  item,
			ProviderSlug: p.Slug,
			ProviderName: p.Name,
			ProviderType: p.Type,
		})
	}
	return out, nil
}

// DeleteSSOIdentity 删除属于指定用户的 SSO 绑定记录。
func (s *Store) DeleteSSOIdentity(ctx context.Context, userID, identityID uint64) error {
	res := dbx.WithContext(ctx, s.db).
		Where("id = ? AND user_id = ?", identityID, userID).
		Delete(&model.SSOIdentity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}
