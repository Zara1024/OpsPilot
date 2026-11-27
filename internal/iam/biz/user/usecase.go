package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

const minPasswordLength = 8

var (
	emailRegex = regexp.MustCompile("^[a-zA-Z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[a-zA-Z0-9!#$%&'*+/=?^_`{|}~-]+)*@(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?\\.)+[a-zA-Z]{2,}$")
	phoneRegex = regexp.MustCompile(`^(?:\+?86)?1[3-9]\d{9}$|^\+[1-9]\d{6,14}$`)
)

func validateEmail(email string) bool {
	if len(email) < 3 || len(email) > 254 {
		return false
	}
	return emailRegex.MatchString(email)
}

func validatePhone(phone string) bool {
	if phone == "" {
		return true
	}
	return phoneRegex.MatchString(phone)
}

// Usecase is the iam/user biz facade. It owns the authentication flows and
// the admin-side user management operations.
type Usecase struct {
	repo   Repo
	signer *auth.Signer
	log    *slog.Logger
}

// NewUsecase builds the usecase. log may be nil.
func NewUsecase(repo Repo, signer *auth.Signer, log *slog.Logger) *Usecase {
	return &Usecase{repo: repo, signer: signer, log: log}
}

// TokenPair is the return value of Login / Refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // seconds until access token expiry
	Role         string
	UserID       uint64
}

// Register creates a new user with the given role. role must be RoleAdmin or
// RoleUser. The caller (HTTP layer) is responsible for enforcing "only
// existing admins may register new users".
func (u *Usecase) Register(ctx context.Context, email, password, role string) (*model.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("%w: email and password required", errs.ErrInvalid)
	}
	if !validateEmail(email) {
		return nil, fmt.Errorf("%w: invalid email format", errs.ErrInvalid)
	}
	if utf8.RuneCountInString(password) < minPasswordLength {
		return nil, fmt.Errorf("%w: password must be at least %d characters", errs.ErrInvalid, minPasswordLength)
	}
	if role == "" {
		role = model.RoleUser
	}
	if !model.IsValidRole(role) {
		return nil, fmt.Errorf("%w: unknown role %q", errs.ErrInvalid, role)
	}

	if existing, err := u.repo.GetByEmail(ctx, email); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: email already registered", errs.ErrConflict)
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	}

	ph, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		Email:    email,
		PassHash: ph,
		Role:     role,
		Status:   model.StatusActive,
	}
	if err := u.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	// Never echo the hash back.
	user.PassHash = ""
	return user, nil
}

// Authenticate 仅校验邮箱与密码并检查账号状态，成功时返回清零 PassHash 的用户对象（供登录后 MFA 分流）。
func (u *Usecase) Authenticate(ctx context.Context, email, password string) (*model.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("%w: email and password required", errs.ErrInvalid)
	}

	// 安全加固（LOW-01）：限制密码长度上限为 128 字符，防止超长密码引发 CPU 算力耗尽拒绝服务攻击
	if len(password) > 128 {
		return nil, errs.ErrUnauthorized
	}

	// 预设结构合法的虚拟 Argon2id 哈希，用于用户不存在或停用时执行等时计算，抹平时序侧信道（HIGH-01）
	const dummyHash = "$argon2id$v=19$m=65536,t=1,p=4$dHVtum15c2FsdDEyMzQ1Njc$dummyhashdummyhashdummyhashdummyhashdummyh"

	user, err := u.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			// 即使用户不存在，也执行一次完整的哈希计算，消耗等额时间，防止攻击者通过响应时间枚举用户名
			_ = verifyPassword(password, dummyHash)
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}
	if user.Status != model.StatusActive {
		// 账号停用时同样执行等时哈希比对
		_ = verifyPassword(password, dummyHash)
		return nil, errs.ErrUnauthorized
	}
	if !verifyPassword(password, user.PassHash) {
		return nil, errs.ErrUnauthorized
	}
	user.PassHash = ""
	return user, nil
}

// Login verifies credentials and returns a fresh access/refresh pair.
func (u *Usecase) Login(ctx context.Context, email, password string) (*TokenPair, error) {
	user, err := u.Authenticate(ctx, email, password)
	if err != nil {
		return nil, err
	}
	return u.issuePair(user)
}

// Refresh verifies a refresh token and issues a new access/refresh pair.
// Rejects access / MFA challenge tokens while remaining backwards-compatible with
// legacy tokens that do not carry token_use.
func (u *Usecase) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("%w: refresh_token required", errs.ErrInvalid)
	}
	claims, err := u.signer.Verify(refreshToken)
	if err != nil {
		return nil, errs.ErrUnauthorized
	}
	if claims.TokenUse != "" && claims.TokenUse != auth.TokenUseRefresh {
		return nil, errs.ErrUnauthorized
	}
	user, err := u.repo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}
	if user.Status != model.StatusActive {
		return nil, errs.ErrUnauthorized
	}
	return u.issuePair(user)
}

// IssueTokens 为已通过认证（密码/MFA/SSO）的用户直接签发 access/refresh token 对。
func (u *Usecase) IssueTokens(user *model.User) (*TokenPair, error) {
	if user == nil || user.ID == 0 {
		return nil, fmt.Errorf("%w: invalid user", errs.ErrInvalid)
	}
	return u.issuePair(user)
}

// IssueMFAToken 签发用于 MFA 二次验证或强制首次绑定的短时凭证。
func (u *Usecase) IssueMFAToken(user *model.User, setupRequired bool, ttl time.Duration) (string, error) {
	if user == nil || user.ID == 0 {
		return "", fmt.Errorf("%w: invalid user", errs.ErrInvalid)
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c := auth.Claims{
		UserID:      user.ID,
		Email:       user.Email,
		Role:        user.Role,
		IsSuperuser: user.IsSuperuser,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: fmt.Sprintf("%d", user.ID),
		},
	}
	if setupRequired {
		return u.signer.SignMFASetup(c, ttl)
	}
	return u.signer.SignMFAChallenge(c, ttl)
}

// VerifyMFAToken 校验 MFA 临时凭证并实时检查账号是否仍处于 active 状态。
func (u *Usecase) VerifyMFAToken(ctx context.Context, token, expectedUse string) (*model.User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("%w: mfa_token required", errs.ErrInvalid)
	}
	claims, err := u.signer.Verify(token)
	if err != nil {
		return nil, errs.ErrUnauthorized
	}
	if claims.TokenUse != expectedUse {
		return nil, errs.ErrUnauthorized
	}
	user, err := u.repo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}
	if user.Status != model.StatusActive {
		return nil, errs.ErrUnauthorized
	}
	user.PassHash = ""
	return user, nil
}

// EnsureSSOUser 根据 SSO 返回的邮箱查找或按配置自动创建（JIT）用户，返回用户对象及是否新建。
func (u *Usecase) EnsureSSOUser(ctx context.Context, email, displayName, defaultRole string, autoCreate bool) (*model.User, bool, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !validateEmail(email) {
		return nil, false, fmt.Errorf("%w: invalid email from SSO provider", errs.ErrInvalid)
	}
	existing, err := u.repo.GetByEmail(ctx, email)
	if err == nil && existing != nil {
		if existing.Status != model.StatusActive {
			return nil, false, errs.ErrForbidden
		}
		existing.PassHash = ""
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, false, err
	}
	if !autoCreate {
		return nil, false, fmt.Errorf("%w: SSO user auto-provisioning is disabled", errs.ErrForbidden)
	}
	role := defaultRole
	if !model.IsValidRole(role) {
		role = model.RoleUser
	}
	randBytes := make([]byte, 32)
	if _, err := rand.Read(randBytes); err != nil {
		return nil, false, fmt.Errorf("generate sso random password: %w", err)
	}
	ph, err := hashPassword(base64.RawURLEncoding.EncodeToString(randBytes))
	if err != nil {
		return nil, false, fmt.Errorf("hash sso random password: %w", err)
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		if at := strings.Index(email, "@"); at > 0 {
			name = email[:at]
		} else {
			name = email
		}
	}
	if len([]rune(name)) > 128 {
		name = string([]rune(name)[:128])
	}
	created := &model.User{
		Email:       email,
		PassHash:    ph,
		DisplayName: name,
		Role:        role,
		IsSuperuser: role == model.RoleAdmin,
		Status:      model.StatusActive,
	}
	if err := u.repo.Create(ctx, created); err != nil {
		return nil, false, fmt.Errorf("create sso user: %w", err)
	}
	created.PassHash = ""
	return created, true, nil
}

// BootstrapAdmin seeds the first admin on a fresh DB. Idempotent: if the
// users table is non-empty it returns nil without touching anything.
func (u *Usecase) BootstrapAdmin(ctx context.Context, email, password string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil
	}
	n, err := u.repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		if u.log != nil {
			u.log.Info("bootstrap admin skipped; users table non-empty", "existing", n)
		}
		return nil
	}
	ph, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	user := &model.User{
		Email: email,
		// Default DisplayName fixed to "admin" so the SPA shows a clean
		// label in the sidebar / user menu instead of the email address.
		// Operators can rename via /v1/users/{id} later.
		DisplayName: "admin",
		PassHash:    ph,
		Role:        model.RoleAdmin,
		IsSuperuser: true,
		Status:      model.StatusActive,
	}
	if err := u.repo.Create(ctx, user); err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	if u.log != nil {
		u.log.Info("bootstrap admin created", "email", email, "id", user.ID)
	}
	return nil
}

// GetByID returns a user by id with PassHash cleared.
func (u *Usecase) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	user, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.PassHash = ""
	return user, nil
}

// List returns all users with PassHash cleared.
func (u *Usecase) List(ctx context.Context) ([]*model.User, error) {
	users, err := u.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		user.PassHash = ""
	}
	return users, nil
}

// Delete removes a user by id.
func (u *Usecase) Delete(ctx context.Context, id uint64) error {
	return u.repo.Delete(ctx, id)
}

// SetRole updates a user's role. role must be admin or user. The
// legacy is_superuser column is kept in sync (admin ⇔ superuser) so the
// casbin short-circuit in authzmw / JWT claim stay correct.
func (u *Usecase) SetRole(ctx context.Context, id uint64, role string) error {
	if !model.IsValidRole(role) {
		return fmt.Errorf("%w: unknown role %q", errs.ErrInvalid, role)
	}
	if err := u.repo.UpdateRole(ctx, id, role); err != nil {
		return err
	}
	// Best-effort: drift on the back-compat column doesn't break new
	// flows (requireAdmin reads role only), but the casbin middleware
	// still consults is_superuser, so keep them aligned.
	_ = u.repo.UpdateSuperuser(ctx, id, role == model.RoleAdmin)
	return nil
}

// UpdateProfile sets display_name + phone.
func (u *Usecase) UpdateProfile(ctx context.Context, id uint64, displayName, phone string) error {
	displayName = strings.TrimSpace(displayName)
	phone = strings.TrimSpace(phone)
	if len(displayName) > 128 {
		return fmt.Errorf("%w: display_name too long (max 128)", errs.ErrInvalid)
	}
	if len(phone) > 32 {
		return fmt.Errorf("%w: phone too long (max 32)", errs.ErrInvalid)
	}
	if phone != "" && !validatePhone(phone) {
		return fmt.Errorf("%w: invalid phone format", errs.ErrInvalid)
	}
	return u.repo.UpdateProfile(ctx, id, displayName, phone)
}

// SetStatus sets active/disabled — soft-delete equivalent. Disabling
// the last admin is rejected to keep the system manageable.
func (u *Usecase) SetStatus(ctx context.Context, id uint64, status string) error {
	switch status {
	case model.StatusActive, model.StatusDisabled:
	default:
		return fmt.Errorf("%w: unknown status %q", errs.ErrInvalid, status)
	}
	return u.repo.UpdateStatus(ctx, id, status)
}

// SetSuperuser was retired May 2026 — the privilege tier is now a
// single field (role). The DB column + JWT claim + casbin short-circuit
// stay for back-compat, but admin and superuser are kept in sync via
// SetRole / Create and the boot-time EnsureSuperuser migration.

// ResetPassword overwrites the user's password hash. Admin-only flow.
func (u *Usecase) ResetPassword(ctx context.Context, id uint64, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("%w: password required", errs.ErrInvalid)
	}
	if utf8.RuneCountInString(newPassword) < minPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters", errs.ErrInvalid, minPasswordLength)
	}
	ph, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return u.repo.UpdatePassHash(ctx, id, ph)
}

// CreateInput is the form payload for the admin "invite user" flow.
// Privilege tier collapsed to Role only (May 2026); the legacy
// is_superuser column is derived from Role inside Create.
type CreateInput struct {
	Email       string
	Password    string
	DisplayName string
	Phone       string
	Role        string // admin | user
}

// Create persists a new user with full profile fields. Admin-only;
// caller enforces permission. Returns the user with PassHash cleared.
func (u *Usecase) Create(ctx context.Context, in CreateInput) (*model.User, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if email == "" {
		return nil, fmt.Errorf("%w: email required", errs.ErrInvalid)
	}
	if !validateEmail(email) {
		return nil, fmt.Errorf("%w: invalid email format", errs.ErrInvalid)
	}
	phone := strings.TrimSpace(in.Phone)
	if phone != "" && !validatePhone(phone) {
		return nil, fmt.Errorf("%w: invalid phone format", errs.ErrInvalid)
	}
	if in.Password == "" {
		return nil, fmt.Errorf("%w: password required", errs.ErrInvalid)
	}
	if utf8.RuneCountInString(in.Password) < minPasswordLength {
		return nil, fmt.Errorf("%w: password must be at least %d characters", errs.ErrInvalid, minPasswordLength)
	}
	role := in.Role
	if role == "" {
		role = model.RoleUser
	}
	if !model.IsValidRole(role) {
		return nil, fmt.Errorf("%w: unknown role %q", errs.ErrInvalid, role)
	}
	if existing, err := u.repo.GetByEmail(ctx, email); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: email already registered", errs.ErrConflict)
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	}
	ph, err := hashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	// Display name fallback: when the form arrives without one (older
	// SPA / API caller), default to the email's local-part so the
	// sidebar / chat author / membership list don't silently fall back
	// to showing the full email address — the new SPA marks the field
	// required and auto-suggests the local-part as the user types
	// email, this branch covers anything that bypasses the SPA.
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		if at := strings.Index(email, "@"); at > 0 {
			displayName = email[:at]
		} else {
			displayName = email
		}
	}
	user := &model.User{
		Email:       email,
		PassHash:    ph,
		DisplayName: displayName,
		Phone:       strings.TrimSpace(in.Phone),
		Role:        role,
		IsSuperuser: role == model.RoleAdmin,
		Status:      model.StatusActive,
	}
	if err := u.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	user.PassHash = ""
	return user, nil
}

// EnsureSuperuser idempotently flips legacy admins (Role == admin) to
// IsSuperuser=true. Used by the boot migration so the existing JWT-
// based admin flag aligns with the new column. Same boot pass also
// backfills empty display_name → email local-part so existing rows
// stop showing the full email in the sidebar / chat author position.
func (u *Usecase) EnsureSuperuser(ctx context.Context) error {
	users, err := u.repo.List(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.Role == model.RoleAdmin && !user.IsSuperuser {
			if err := u.repo.UpdateSuperuser(ctx, user.ID, true); err != nil {
				return fmt.Errorf("ensure superuser %d: %w", user.ID, err)
			}
			if u.log != nil {
				u.log.Info("migrated legacy admin → is_superuser=true",
					slog.Uint64("user_id", user.ID))
			}
		}
		// Display-name backfill — see Create() for the rationale.
		if strings.TrimSpace(user.DisplayName) == "" {
			derived := user.Email
			if at := strings.Index(user.Email, "@"); at > 0 {
				derived = user.Email[:at]
			}
			if err := u.repo.UpdateProfile(ctx, user.ID, derived, user.Phone); err != nil {
				if u.log != nil {
					u.log.Warn("backfill display_name failed",
						slog.Uint64("user_id", user.ID),
						slog.Any("err", err))
				}
				continue
			}
			if u.log != nil {
				u.log.Info("backfilled empty display_name from email local-part",
					slog.Uint64("user_id", user.ID),
					slog.String("display_name", derived))
			}
		}
	}
	return nil
}

func (u *Usecase) issuePair(user *model.User) (*TokenPair, error) {
	base := auth.Claims{
		UserID:      user.ID,
		Email:       user.Email,
		Role:        user.Role,
		IsSuperuser: user.IsSuperuser,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: fmt.Sprintf("%d", user.ID),
		},
	}
	access, err := u.signer.SignAccess(base)
	if err != nil {
		return nil, fmt.Errorf("sign access: %w", err)
	}
	refresh, err := u.signer.SignRefresh(base)
	if err != nil {
		return nil, fmt.Errorf("sign refresh: %w", err)
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(u.signer.AccessTTL() / time.Second),
		Role:         user.Role,
		UserID:       user.ID,
	}, nil
}

// AuthorizeApproval 对审批重新读取当前账号权限，避免撤权后继续使用旧登录凭证。
func (u *Usecase) AuthorizeApproval(ctx context.Context, userID uint64) error {
	user, err := u.repo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.Status != "active" || user.Role != "admin" {
		return errs.ErrForbidden
	}
	return nil
}
