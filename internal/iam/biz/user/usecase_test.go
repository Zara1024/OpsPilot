package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// fakeRepo is an in-memory Repo for usecase-level tests.
type fakeRepo struct {
	byID    map[uint64]*model.User
	byEmail map[string]*model.User
	nextID  uint64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uint64]*model.User{}, byEmail: map[string]*model.User{}}
}

func (r *fakeRepo) Create(_ context.Context, u *model.User) error {
	r.nextID++
	u.ID = r.nextID
	cp := *u
	r.byID[u.ID] = &cp
	r.byEmail[u.Email] = &cp
	return nil
}

func (r *fakeRepo) GetByEmail(_ context.Context, email string) (*model.User, error) {
	u, ok := r.byEmail[email]
	if !ok {
		return nil, errs.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uint64) (*model.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeRepo) List(_ context.Context) ([]*model.User, error) {
	out := make([]*model.User, 0, len(r.byID))
	for _, u := range r.byID {
		cp := *u
		out = append(out, &cp)
	}
	return out, nil
}

func (r *fakeRepo) Count(_ context.Context) (int64, error) {
	return int64(len(r.byID)), nil
}

func (r *fakeRepo) Delete(_ context.Context, id uint64) error {
	u, ok := r.byID[id]
	if !ok {
		return errs.ErrNotFound
	}
	delete(r.byID, id)
	delete(r.byEmail, u.Email)
	return nil
}

func (r *fakeRepo) UpdateRole(_ context.Context, id uint64, role string) error {
	u, ok := r.byID[id]
	if !ok {
		return errs.ErrNotFound
	}
	u.Role = role
	return nil
}

func (r *fakeRepo) UpdateProfile(_ context.Context, id uint64, displayName, phone string) error {
	u, ok := r.byID[id]
	if !ok {
		return errs.ErrNotFound
	}
	u.DisplayName = displayName
	u.Phone = phone
	return nil
}

func (r *fakeRepo) UpdateStatus(_ context.Context, id uint64, status string) error {
	u, ok := r.byID[id]
	if !ok {
		return errs.ErrNotFound
	}
	u.Status = status
	return nil
}

func (r *fakeRepo) UpdateSuperuser(_ context.Context, id uint64, isSuperuser bool) error {
	u, ok := r.byID[id]
	if !ok {
		return errs.ErrNotFound
	}
	u.IsSuperuser = isSuperuser
	return nil
}

func (r *fakeRepo) UpdatePassHash(_ context.Context, id uint64, passHash string) error {
	u, ok := r.byID[id]
	if !ok {
		return errs.ErrNotFound
	}
	u.PassHash = passHash
	return nil
}

func newTestUsecase(t *testing.T) *Usecase {
	t.Helper()
	signer := auth.NewSigner("test-secret", 15*time.Minute, 24*time.Hour)
	return NewUsecase(newFakeRepo(), signer, nil)
}

func TestBootstrapAdmin_SeedsThenNoops(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	if err := uc.BootstrapAdmin(ctx, "root@example.com", "secret-password"); err != nil {
		t.Fatalf("bootstrap first: %v", err)
	}
	if err := uc.BootstrapAdmin(ctx, "other@example.com", "another-password"); err != nil {
		t.Fatalf("bootstrap second: %v", err)
	}
	users, err := uc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("want 1 user after double-bootstrap, got %d", len(users))
	}
	if users[0].Email != "root@example.com" || users[0].Role != model.RoleAdmin {
		t.Errorf("unexpected admin: %+v", users[0])
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	if err := uc.BootstrapAdmin(ctx, "root@example.com", "goodpass"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	_, err := uc.Login(ctx, "root@example.com", "badpass")
	if !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
	pair, err := uc.Login(ctx, "root@example.com", "goodpass")
	if err != nil {
		t.Fatalf("good login: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("empty tokens in pair %+v", pair)
	}
	if pair.Role != model.RoleAdmin {
		t.Errorf("role = %q, want admin", pair.Role)
	}
}

func TestLogin_RejectsLongPassword(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	longPass := strings.Repeat("a", 129)
	_, err := uc.Login(ctx, "test@example.com", longPass)
	if !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized for long password, got %v", err)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	if _, err := uc.Register(ctx, "a@example.com", "password-aaaa", model.RoleUser); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, err := uc.Register(ctx, "a@example.com", "password-bbbb", model.RoleUser)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestRegister_EmailFormat(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	// Valid emails including apostrophe per RFC 5322 §3.2.3
	validEmails := []string{
		"o'connor@example.com",
		"user.name@example.com",
		"user+tag@example.com",
	}
	for _, email := range validEmails {
		u, err := uc.Register(ctx, email, "validpass123", model.RoleUser)
		if err != nil {
			t.Errorf("Register(%q): unexpected error: %v", email, err)
		}
		if u == nil || u.Email != email {
			t.Errorf("Register(%q): expected user email %q", email, email)
		}
	}

	// Invalid emails: consecutive dots, leading/trailing dots, malformed domain
	invalidEmails := []string{
		"not-an-email",
		"@example.com",
		"user@",
		"user@.com",
		"user@com",
		"first..last@example.com",
		"user@example..com",
		"user@-example.com",
		"user@example-.com",
		".user@example.com",
		"user.@example.com",
	}
	for _, email := range invalidEmails {
		_, err := uc.Register(ctx, email, "validpass123", model.RoleUser)
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("Register(%q): want ErrInvalid, got %v", email, err)
		}
	}
}

func TestPasswordLengthPolicy(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	shortPasswords := []string{
		"1", "1234567", "a", "short",
		"密码短",        // 3 Unicode characters, but 9 UTF-8 bytes
		"密码六个字",      // 5 Unicode characters
		"七个字符的密码",    // 7 Unicode characters
		"🔑🔑🔑",         // 3 emojis, 12 UTF-8 bytes
		"🔑🔑🔑🔑🔑🔑🔑",     // 7 emojis, 28 UTF-8 bytes
	}
	for _, pw := range shortPasswords {
		// Register rejects short passwords
		_, err := uc.Register(ctx, "reg@example.com", pw, model.RoleUser)
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("Register with password %q: want ErrInvalid, got %v", pw, err)
		}

		// Create rejects short passwords
		_, err = uc.Create(ctx, CreateInput{
			Email:    "create@example.com",
			Password: pw,
		})
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("Create with password %q: want ErrInvalid, got %v", pw, err)
		}
	}

	// Create user with valid password for testing ResetPassword
	u, err := uc.Create(ctx, CreateInput{
		Email:    "reset@example.com",
		Password: "validpassword123",
	})
	if err != nil {
		t.Fatalf("Create user for reset: %v", err)
	}

	for _, pw := range shortPasswords {
		err := uc.ResetPassword(ctx, u.ID, pw)
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("ResetPassword with password %q: want ErrInvalid, got %v", pw, err)
		}
	}

	// Valid reset password succeeds
	if err := uc.ResetPassword(ctx, u.ID, "newvalidpw123"); err != nil {
		t.Errorf("ResetPassword with valid password: %v", err)
	}

	// Valid non-ASCII passwords with 8+ runes succeed
	validNonAscii := []string{
		"密码至少八个字符",     // 8 Chinese characters
		"这是一段足够长的安全密码", // 11 Chinese characters
		"🔑🔑🔑🔑🔑🔑🔑🔑",   // 8 emojis, 32 UTF-8 bytes
	}
	for i, pw := range validNonAscii {
		email := fmt.Sprintf("nonascii-%d@example.com", i)
		if _, err := uc.Register(ctx, email, pw, model.RoleUser); err != nil {
			t.Errorf("Register with valid non-ASCII password %q: unexpected error %v", pw, err)
		}
		if err := uc.ResetPassword(ctx, u.ID, pw); err != nil {
			t.Errorf("ResetPassword with valid non-ASCII password %q: unexpected error %v", pw, err)
		}
	}
}

func TestCreate_EmailAndPhoneValidation(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	// Invalid email
	_, err := uc.Create(ctx, CreateInput{
		Email:    "bad-email",
		Password: "password123",
	})
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("Create with invalid email: want ErrInvalid, got %v", err)
	}

	// Invalid phone
	_, err = uc.Create(ctx, CreateInput{
		Email:    "valid@example.com",
		Password: "password123",
		Phone:    "not-a-phone-number",
	})
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("Create with invalid phone: want ErrInvalid, got %v", err)
	}

	// Valid email and phone
	u, err := uc.Create(ctx, CreateInput{
		Email:    "valid@example.com",
		Password: "password123",
		Phone:    "+8613800138000",
	})
	if err != nil {
		t.Fatalf("Create with valid email and phone: %v", err)
	}
	if u.Email != "valid@example.com" {
		t.Errorf("email = %q, want valid@example.com", u.Email)
	}

	// Invalid phone in UpdateProfile
	if err := uc.UpdateProfile(ctx, u.ID, "New Name", "invalid-phone"); !errors.Is(err, errs.ErrInvalid) {
		t.Errorf("UpdateProfile with invalid phone: want ErrInvalid, got %v", err)
	}

	// Valid phone in UpdateProfile
	if err := uc.UpdateProfile(ctx, u.ID, "New Name", "+12025550123"); err != nil {
		t.Errorf("UpdateProfile with valid phone: %v", err)
	}

	// Clear phone in UpdateProfile
	if err := uc.UpdateProfile(ctx, u.ID, "New Name", ""); err != nil {
		t.Errorf("UpdateProfile with empty phone: %v", err)
	}
	updated, err := uc.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetByID after clearing phone: %v", err)
	}
	if updated.Phone != "" {
		t.Errorf("phone = %q, want empty string", updated.Phone)
	}
}

func TestRefreshRejectsAccessAndMFATokens(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	u, err := uc.Create(ctx, CreateInput{
		Email:    "tokenuser@example.com",
		Password: "password123",
		Role:     model.RoleUser,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pair, err := uc.IssueTokens(u)
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}
	if _, err := uc.Refresh(ctx, pair.AccessToken); !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("refresh with access token: want ErrUnauthorized, got %v", err)
	}
	mfaTok, err := uc.IssueMFAToken(u, false, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue mfa token: %v", err)
	}
	if _, err := uc.Refresh(ctx, mfaTok); !errors.Is(err, errs.ErrUnauthorized) {
		t.Fatalf("refresh with mfa token: want ErrUnauthorized, got %v", err)
	}
	refreshed, err := uc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh with refresh token: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatalf("expected refreshed tokens")
	}
}

func TestEnsureSSOUser_JITAndExisting(t *testing.T) {
	uc := newTestUsecase(t)
	ctx := context.Background()

	if _, _, err := uc.EnsureSSOUser(ctx, "new.sso@example.com", "New SSO", model.RoleUser, false); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("autoCreate=false: want ErrForbidden, got %v", err)
	}
	created, isNew, err := uc.EnsureSSOUser(ctx, "new.sso@example.com", "New SSO", model.RoleUser, true)
	if err != nil {
		t.Fatalf("autoCreate=true: %v", err)
	}
	if !isNew || created.Email != "new.sso@example.com" || created.Role != model.RoleUser {
		t.Fatalf("unexpected created sso user: isNew=%v user=%+v", isNew, created)
	}
	again, isNew2, err := uc.EnsureSSOUser(ctx, "new.sso@example.com", "Ignored", model.RoleAdmin, true)
	if err != nil || isNew2 || again.ID != created.ID {
		t.Fatalf("second lookup mismatch: isNew=%v user=%+v err=%v", isNew2, again, err)
	}
}
