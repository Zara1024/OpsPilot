package mfa

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	securitystore "github.com/Zara1024/OpsPilot/internal/iam/data/security/store"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

func newTestMFAService(t *testing.T) (*Service, *securitystore.Store, *time.Time) {
	t.Helper()
	t.Setenv("OPSPILOT_SECRET_KEY", "test-mfa-secretbox-key-unit")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mfa.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, securitystore.Migrate(db))

	st := securitystore.New(db)
	svc := New(st)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, st, &now
}

func TestRFC6238TestVector(t *testing.T) {
	// RFC 6238 Appendix B SHA-1 test vector secret "12345678901234567890" -> Base32: GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := GenerateTOTP(secret, time.Unix(59, 0).UTC())
	require.NoError(t, err)
	// RFC 6238 8-digit at t=59 is 94287082 -> 6 digits is 287082.
	require.Equal(t, "287082", code)
}

func TestMFAEnrollVerifyReplayRecoveryAndDisable(t *testing.T) {
	svc, st, now := newTestMFAService(t)
	ctx := context.Background()

	setup, err := svc.Setup(ctx, 42, "alice@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, setup.Secret)
	require.Contains(t, setup.OtpAuthURI, "otpauth://totp/OpsPilot:alice@example.com")

	// DB secret must be encrypted at rest, never plaintext secret.
	row, err := st.GetUserMFA(ctx, 42)
	require.NoError(t, err)
	require.False(t, row.Enabled)
	require.NotEqual(t, setup.Secret, row.SecretEnc)

	// Bad code fails Confirm
	_, err = svc.Confirm(ctx, 42, "000000")
	require.ErrorIs(t, err, errs.ErrUnauthorized)

	// Valid TOTP confirms & returns 8 recovery codes
	code1, err := GenerateTOTP(setup.Secret, *now)
	require.NoError(t, err)
	recoveryCodes, err := svc.Confirm(ctx, 42, code1)
	require.NoError(t, err)
	require.Len(t, recoveryCodes, 8)

	// Replay of exact same step code must be rejected
	_, err = svc.Verify(ctx, 42, code1)
	require.ErrorIs(t, err, errs.ErrUnauthorized)

	// Next 30s window succeeds
	*now = now.Add(30 * time.Second)
	code2, err := GenerateTOTP(setup.Secret, *now)
	require.NoError(t, err)
	usedRecovery, err := svc.Verify(ctx, 42, code2)
	require.NoError(t, err)
	require.False(t, usedRecovery)

	// Recovery code succeeds once and is immediately burned
	usedRecovery, err = svc.Verify(ctx, 42, recoveryCodes[0])
	require.NoError(t, err)
	require.True(t, usedRecovery)
	_, err = svc.Verify(ctx, 42, recoveryCodes[0])
	require.ErrorIs(t, err, errs.ErrUnauthorized)

	stStatus, err := svc.Status(ctx, 42)
	require.NoError(t, err)
	require.True(t, stStatus.Enabled)
	require.Equal(t, 7, stStatus.RecoveryCodesRemaining)

	// Disable using second recovery code
	require.NoError(t, svc.Disable(ctx, 42, recoveryCodes[1]))
	stAfter, err := svc.Status(ctx, 42)
	require.NoError(t, err)
	require.False(t, stAfter.Enabled)
}
