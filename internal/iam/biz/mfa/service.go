// Package mfa 实现基于 RFC 6238 TOTP 与一次性恢复码的企业多因素认证服务（纯 Go 标准库实现）。
package mfa

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/secretbox"
)

const (
	totpPeriodSeconds = 30
	totpDigits        = 6
	totpSkewSteps     = 1
	recoveryCount     = 8
	recoveryCharset   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	totpIssuer        = "OpsPilot"
)

var b32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// Store 定义 MFA 持久化接口。
type Store interface {
	GetUserMFA(ctx context.Context, userID uint64) (*model.UserMFA, error)
	ListUserMFAEnabled(ctx context.Context) (map[uint64]bool, error)
	UpsertUserMFA(ctx context.Context, mfa *model.UserMFA) error
	DeleteUserMFA(ctx context.Context, userID uint64) error
}

// Service 提供 TOTP MFA 绑定、验证、恢复码与管理员重置功能。
type Service struct {
	store Store
	now   func() time.Time
}

// New 构造 MFA Service。
func New(store Store) *Service {
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// SetupResult 包含前端展示二维码与手动输入所需的绑定数据。
type SetupResult struct {
	Secret     string `json:"secret"`
	OtpAuthURI string `json:"otpauth_uri"`
	Issuer     string `json:"issuer"`
}

// Status 返回用户的 MFA 启用状态与剩余恢复码数量。
type Status struct {
	Enabled                bool       `json:"enabled"`
	RecoveryCodesRemaining int        `json:"recovery_codes_remaining"`
	VerifiedAt             *time.Time `json:"verified_at,omitempty"`
}

type recoveryHashEntry struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

// GenerateTOTP 按 RFC 6238 计算给定时间点对应的 6 位数字验证码（HMAC-SHA1, 30s 步长）。
func GenerateTOTP(secretBase32 string, at time.Time) (string, error) {
	step := at.UTC().Unix() / totpPeriodSeconds
	return GenerateHOTP(secretBase32, uint64(step))
}

// GenerateHOTP 按 RFC 4226 计算指定计数器的 6 位动态验证码。
func GenerateHOTP(secretBase32 string, counter uint64) (string, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secretBase32), " ", ""))
	key, err := b32NoPad.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base32 secret", errs.ErrInvalid)
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	// hash.Hash.Write never fails per Go stdlib contract.
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	codeInt := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	otp := codeInt % 1000000
	return fmt.Sprintf("%06d", otp), nil
}

// BuildOtpAuthURI 生成标准 otpauth://totp/... URI。
func BuildOtpAuthURI(issuer, account, secret string) string {
	if issuer == "" {
		issuer = totpIssuer
	}
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", totpPeriodSeconds))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Status 查询用户当前的 MFA 状态。
func (s *Service) Status(ctx context.Context, userID uint64) (*Status, error) {
	if userID == 0 {
		return nil, errs.ErrInvalid
	}
	row, err := s.store.GetUserMFA(ctx, userID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return &Status{Enabled: false, RecoveryCodesRemaining: 0}, nil
		}
		return nil, err
	}
	var entries []recoveryHashEntry
	if row.RecoveryHashesJSON != "" {
		if err := json.Unmarshal([]byte(row.RecoveryHashesJSON), &entries); err != nil {
			return nil, fmt.Errorf("parse recovery hashes: %w", err)
		}
	}
	return &Status{
		Enabled:                row.Enabled,
		RecoveryCodesRemaining: len(entries),
		VerifiedAt:             row.VerifiedAt,
	}, nil
}

// EnabledMap 返回所有已开启 MFA 的用户 ID 集合。
func (s *Service) EnabledMap(ctx context.Context) (map[uint64]bool, error) {
	if s.store == nil {
		return map[uint64]bool{}, nil
	}
	return s.store.ListUserMFAEnabled(ctx)
}

// IsEnabled 快速判断指定用户是否已激活 MFA。
func (s *Service) IsEnabled(ctx context.Context, userID uint64) (bool, error) {
	st, err := s.Status(ctx, userID)
	if err != nil {
		return false, err
	}
	return st.Enabled, nil
}

// Setup 为尚未启用 MFA 的用户生成加密存储的临时 TOTP 密钥（未 Confirm 前保持 Enabled=false）。
func (s *Service) Setup(ctx context.Context, userID uint64, email string) (*SetupResult, error) {
	if userID == 0 || strings.TrimSpace(email) == "" {
		return nil, errs.ErrInvalid
	}
	existing, err := s.store.GetUserMFA(ctx, userID)
	if err == nil && existing != nil && existing.Enabled {
		return nil, fmt.Errorf("%w: MFA is already enabled", errs.ErrConflict)
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	}

	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate totp secret: %w", err)
	}
	secret := b32NoPad.EncodeToString(raw)
	enc, err := secretbox.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt totp secret: %w", err)
	}
	row := &model.UserMFA{
		UserID:             userID,
		SecretEnc:          enc,
		Enabled:            false,
		RecoveryHashesJSON: "[]",
		LastUsedStep:       0,
		VerifiedAt:         nil,
	}
	if err := s.store.UpsertUserMFA(ctx, row); err != nil {
		return nil, err
	}
	return &SetupResult{
		Secret:     secret,
		OtpAuthURI: BuildOtpAuthURI(totpIssuer, email, secret),
		Issuer:     totpIssuer,
	}, nil
}

// Confirm 验证首次输入的 6 位动态验证码后正式开启 MFA，并生成 8 个一次性恢复码。
func (s *Service) Confirm(ctx context.Context, userID uint64, code string) ([]string, error) {
	row, err := s.store.GetUserMFA(ctx, userID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, fmt.Errorf("%w: MFA setup not started", errs.ErrInvalid)
		}
		return nil, err
	}
	if row.Enabled {
		return nil, fmt.Errorf("%w: MFA is already enabled", errs.ErrConflict)
	}
	secret, err := secretbox.Decrypt(row.SecretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt totp secret: %w", err)
	}
	matchedStep, ok := verifyTOTPWindow(secret, code, s.now(), row.LastUsedStep)
	if !ok {
		return nil, fmt.Errorf("%w: invalid verification code", errs.ErrUnauthorized)
	}

	plainCodes, hashes, err := generateRecoveryCodes(recoveryCount)
	if err != nil {
		return nil, err
	}
	hashesBytes, err := json.Marshal(hashes)
	if err != nil {
		return nil, err
	}
	now := s.now()
	row.Enabled = true
	row.LastUsedStep = matchedStep
	row.RecoveryHashesJSON = string(hashesBytes)
	row.VerifiedAt = &now
	if err := s.store.UpsertUserMFA(ctx, row); err != nil {
		return nil, err
	}
	return plainCodes, nil
}

// Verify 校验用户的 6 位 TOTP 或一次性恢复码；恢复码校验通过后立即消费销毁。
func (s *Service) Verify(ctx context.Context, userID uint64, code string) (usedRecovery bool, err error) {
	row, err := s.store.GetUserMFA(ctx, userID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return false, errs.ErrUnauthorized
		}
		return false, err
	}
	if !row.Enabled {
		return false, errs.ErrUnauthorized
	}
	secret, err := secretbox.Decrypt(row.SecretEnc)
	if err != nil {
		return false, fmt.Errorf("decrypt totp secret: %w", err)
	}
	if matchedStep, ok := verifyTOTPWindow(secret, code, s.now(), row.LastUsedStep); ok {
		row.LastUsedStep = matchedStep
		if err := s.store.UpsertUserMFA(ctx, row); err != nil {
			return false, err
		}
		return false, nil
	}

	// 尝试匹配一次性恢复码（格式 XXXX-XXXX 或连写）。
	normalized := normalizeRecoveryCode(code)
	if len(normalized) == 8 {
		var entries []recoveryHashEntry
		if row.RecoveryHashesJSON != "" {
			if err := json.Unmarshal([]byte(row.RecoveryHashesJSON), &entries); err != nil {
				return false, fmt.Errorf("parse recovery hashes: %w", err)
			}
		}
		for idx, entry := range entries {
			if matchRecoveryHash(entry, normalized) {
				remaining := append(entries[:idx:idx], entries[idx+1:]...)
				raw, err := json.Marshal(remaining)
				if err != nil {
					return false, err
				}
				row.RecoveryHashesJSON = string(raw)
				if err := s.store.UpsertUserMFA(ctx, row); err != nil {
					return false, err
				}
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("%w: invalid MFA code", errs.ErrUnauthorized)
}

// Disable 在验证当前 TOTP 或恢复码后关闭用户的 MFA。
func (s *Service) Disable(ctx context.Context, userID uint64, code string) error {
	if _, err := s.Verify(ctx, userID, code); err != nil {
		return err
	}
	err := s.store.DeleteUserMFA(ctx, userID)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	return err
}

// RegenerateRecoveryCodes 在校验当前 TOTP（非恢复码）后重新生成 8 个恢复码并作废旧恢复码。
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID uint64, totpCode string) ([]string, error) {
	row, err := s.store.GetUserMFA(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !row.Enabled {
		return nil, fmt.Errorf("%w: MFA is not enabled", errs.ErrInvalid)
	}
	secret, err := secretbox.Decrypt(row.SecretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt totp secret: %w", err)
	}
	matchedStep, ok := verifyTOTPWindow(secret, totpCode, s.now(), row.LastUsedStep)
	if !ok {
		return nil, fmt.Errorf("%w: valid TOTP code required to regenerate recovery codes", errs.ErrUnauthorized)
	}
	plainCodes, hashes, err := generateRecoveryCodes(recoveryCount)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(hashes)
	if err != nil {
		return nil, err
	}
	row.LastUsedStep = matchedStep
	row.RecoveryHashesJSON = string(raw)
	if err := s.store.UpsertUserMFA(ctx, row); err != nil {
		return nil, err
	}
	return plainCodes, nil
}

// AdminReset 允许管理员强制重置指定用户的 MFA（幂等）。
func (s *Service) AdminReset(ctx context.Context, targetUserID uint64) error {
	if targetUserID == 0 {
		return errs.ErrInvalid
	}
	err := s.store.DeleteUserMFA(ctx, targetUserID)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	return err
}

func verifyTOTPWindow(secret, code string, now time.Time, lastUsedStep int64) (int64, bool) {
	cleaned := strings.TrimSpace(code)
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	if len(cleaned) != totpDigits {
		return 0, false
	}
	curStep := now.UTC().Unix() / totpPeriodSeconds
	for delta := int64(-totpSkewSteps); delta <= totpSkewSteps; delta++ {
		step := curStep + delta
		if step < 0 || step <= lastUsedStep {
			continue
		}
		expected, err := GenerateHOTP(secret, uint64(step))
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(cleaned)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func generateRecoveryCodes(n int) ([]string, []recoveryHashEntry, error) {
	plain := make([]string, 0, n)
	entries := make([]recoveryHashEntry, 0, n)
	for i := 0; i < n; i++ {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		var chars [8]byte
		for j := 0; j < 8; j++ {
			chars[j] = recoveryCharset[int(raw[j])%len(recoveryCharset)]
		}
		norm := string(chars[:])
		display := norm[:4] + "-" + norm[4:]
		var salt [16]byte
		if _, err := rand.Read(salt[:]); err != nil {
			return nil, nil, fmt.Errorf("generate recovery salt: %w", err)
		}
		saltHex := hex.EncodeToString(salt[:])
		sum := sha256.Sum256([]byte(saltHex + ":" + norm))
		plain = append(plain, display)
		entries = append(entries, recoveryHashEntry{
			Salt: saltHex,
			Hash: hex.EncodeToString(sum[:]),
		})
	}
	return plain, entries, nil
}

func matchRecoveryHash(entry recoveryHashEntry, normalized string) bool {
	sum := sha256.Sum256([]byte(entry.Salt + ":" + normalized))
	candidate := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(entry.Hash)) == 1
}
