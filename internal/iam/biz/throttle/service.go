// Package throttle 提供基于数据库的跨副本共享登录限流与认证策略管理。
package throttle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// Store 定义 throttle 所需的持久化与只读依赖接口。
type Store interface {
	GetAuthPolicy(ctx context.Context) (*model.AuthPolicy, error)
	SaveAuthPolicy(ctx context.Context, p *model.AuthPolicy) (*model.AuthPolicy, error)
	IsLoginBlocked(ctx context.Context, scopeKey string, now time.Time, limit int, window time.Duration) (bool, error)
	RecordLoginFailure(ctx context.Context, scopeKey string, now time.Time, window time.Duration) error
	ClearLoginAttempt(ctx context.Context, scopeKey string) error
	ListSSOProviders(ctx context.Context, onlyEnabled bool) ([]*model.SSOProvider, error)
}

// Service 管理全局认证策略与共享登录防爆破限流。
type Service struct {
	store Store
	now   func() time.Time
}

// New 构造 throttle Service。
func New(store Store) *Service {
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// PolicyInput 用于更新全局认证与限流策略。
type PolicyInput struct {
	MFAEnforceMode          string
	AllowLocalPasswordLogin bool
	LoginIPLimit            int
	LoginIPWindowSec        int
	LoginEmailLimit         int
	LoginEmailWindowSec     int
}

func (s *Service) currentPolicy(ctx context.Context) (*model.AuthPolicy, error) {
	if s.store == nil {
		return &model.AuthPolicy{
			MFAEnforceMode:          model.MFAEnforceOff,
			AllowLocalPasswordLogin: true,
			LoginIPLimit:            20,
			LoginIPWindowSec:        300,
			LoginEmailLimit:         6,
			LoginEmailWindowSec:     900,
		}, nil
	}
	return s.store.GetAuthPolicy(ctx)
}

// GetPolicy 返回当前生效的全局认证策略。
func (s *Service) GetPolicy(ctx context.Context) (*model.AuthPolicy, error) {
	return s.currentPolicy(ctx)
}

// UpdatePolicy 校验并持久化全局认证策略；若无已启用 SSO 提供商则禁止关闭本地密码登录，防止平台锁死。
func (s *Service) UpdatePolicy(ctx context.Context, in PolicyInput) (*model.AuthPolicy, error) {
	mode := strings.TrimSpace(strings.ToLower(in.MFAEnforceMode))
	if mode == "" {
		mode = model.MFAEnforceOff
	}
	if !model.IsValidMFAEnforceMode(mode) {
		return nil, fmt.Errorf("%w: invalid mfa_enforce_mode %q", errs.ErrInvalid, in.MFAEnforceMode)
	}
	if in.LoginIPLimit < 3 || in.LoginIPLimit > 1000 {
		return nil, fmt.Errorf("%w: login_ip_limit must be between 3 and 1000", errs.ErrInvalid)
	}
	if in.LoginIPWindowSec < 30 || in.LoginIPWindowSec > 86400 {
		return nil, fmt.Errorf("%w: login_ip_window_sec must be between 30 and 86400", errs.ErrInvalid)
	}
	if in.LoginEmailLimit < 3 || in.LoginEmailLimit > 100 {
		return nil, fmt.Errorf("%w: login_email_limit must be between 3 and 100", errs.ErrInvalid)
	}
	if in.LoginEmailWindowSec < 30 || in.LoginEmailWindowSec > 86400 {
		return nil, fmt.Errorf("%w: login_email_window_sec must be between 30 and 86400", errs.ErrInvalid)
	}
	if !in.AllowLocalPasswordLogin {
		providers, err := s.store.ListSSOProviders(ctx, true)
		if err != nil {
			return nil, fmt.Errorf("check enabled sso providers: %w", err)
		}
		if len(providers) == 0 {
			return nil, fmt.Errorf("%w: cannot disable local password login while no enabled SSO provider exists", errs.ErrInvalid)
		}
	}
	return s.store.SaveAuthPolicy(ctx, &model.AuthPolicy{
		MFAEnforceMode:          mode,
		AllowLocalPasswordLogin: in.AllowLocalPasswordLogin,
		LoginIPLimit:            in.LoginIPLimit,
		LoginIPWindowSec:        in.LoginIPWindowSec,
		LoginEmailLimit:         in.LoginEmailLimit,
		LoginEmailWindowSec:     in.LoginEmailWindowSec,
	})
}

// LocalLoginAllowed 返回当前是否允许本地邮箱密码登录；当策略关闭本地密码登录且确有启用 SSO 时返回 false。
func (s *Service) LocalLoginAllowed(ctx context.Context) (bool, error) {
	p, err := s.currentPolicy(ctx)
	if err != nil {
		return true, err
	}
	if p.AllowLocalPasswordLogin {
		return true, nil
	}
	providers, err := s.store.ListSSOProviders(ctx, true)
	if err != nil {
		return true, err
	}
	return len(providers) == 0, nil
}

// RequiresMFAForRole 判断给定系统角色在策略下是否必须启用 MFA。
func (s *Service) RequiresMFAForRole(ctx context.Context, role string) (bool, error) {
	p, err := s.currentPolicy(ctx)
	if err != nil {
		return false, err
	}
	switch p.MFAEnforceMode {
	case model.MFAEnforceAll:
		return true, nil
	case model.MFAEnforceAdmins:
		return role == model.RoleAdmin, nil
	default:
		return false, nil
	}
}

func ipKey(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	if len(ip) > 180 {
		ip = ip[:180]
	}
	return "ip:" + ip
}

func emailKey(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	if len(email) > 180 {
		email = email[:180]
	}
	return "email:" + email
}

// Check 检查当前 IP 与目标标识是否已被共享限流拦截。
func (s *Service) Check(ctx context.Context, ip, targetKey string) error {
	if s.store == nil {
		return nil
	}
	p, err := s.currentPolicy(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	if k := ipKey(ip); k != "" {
		blocked, err := s.store.IsLoginBlocked(ctx, k, now, p.LoginIPLimit, time.Duration(p.LoginIPWindowSec)*time.Second)
		if err != nil {
			return err
		}
		if blocked {
			return errs.ErrTooManyAttempts
		}
	}
	if k := emailKey(targetKey); k != "" {
		blocked, err := s.store.IsLoginBlocked(ctx, k, now, p.LoginEmailLimit, time.Duration(p.LoginEmailWindowSec)*time.Second)
		if err != nil {
			return err
		}
		if blocked {
			return errs.ErrTooManyAttempts
		}
	}
	return nil
}

// RecordFailure 记录一次认证失败（IP 与目标邮箱/用户维度分别记账）。
func (s *Service) RecordFailure(ctx context.Context, ip, targetKey string) error {
	if s.store == nil {
		return nil
	}
	p, err := s.currentPolicy(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	if k := ipKey(ip); k != "" {
		if err := s.store.RecordLoginFailure(ctx, k, now, time.Duration(p.LoginIPWindowSec)*time.Second); err != nil {
			return err
		}
	}
	if k := emailKey(targetKey); k != "" {
		if err := s.store.RecordLoginFailure(ctx, k, now, time.Duration(p.LoginEmailWindowSec)*time.Second); err != nil {
			return err
		}
	}
	return nil
}

// RecordSuccess 登录或 MFA 验证成功后清零邮箱维度的失败计数。
func (s *Service) RecordSuccess(ctx context.Context, targetKey string) error {
	if s.store == nil {
		return nil
	}
	if k := emailKey(targetKey); k != "" {
		return s.store.ClearLoginAttempt(ctx, k)
	}
	return nil
}
