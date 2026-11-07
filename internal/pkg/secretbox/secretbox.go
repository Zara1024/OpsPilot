// Package secretbox is the at-rest encryption used by the credential vault
// (HLD-017). AES-256-GCM, key derived from OPSPILOT_SECRET_KEY or, when it
// is unset, the already-persisted OPSPILOT_JWT_SECRET. Either source lives
// outside the DB, so a DB dump alone never yields plaintext credentials.
//
// Ciphertext wire format: "v1:" + base64(nonce || ciphertext+tag). The
// version prefix lets us rotate the scheme later without ambiguity.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const prefix = "v1:"

var (
	keyOnce sync.Once
	keyVal  [32]byte
	keyWeak bool // true only when neither configured secret is usable
)

const legacyFallback = "opspilot-insecure-default-secret-key-set-OPSPILOT_SECRET_KEY"

func getSecret() string {
	if s := strings.TrimSpace(os.Getenv("OPSPILOT_SECRET_KEY")); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("ONGRID_SECRET_KEY"))
}

func getJWT() string {
	if s := strings.TrimSpace(os.Getenv("OPSPILOT_JWT_SECRET")); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("ONGRID_JWT_SECRET"))
}

func getKey() ([32]byte, bool) {
	keyOnce.Do(func() {
		keyVal, keyWeak = deriveKey(getSecret(), getJWT())
	})
	if keyWeak {
		if k, weak := deriveKey(getSecret(), getJWT()); !weak {
			keyVal, keyWeak = k, weak
		}
	}
	return keyVal, keyWeak
}

// loadKey derives the 32-byte AES key from the configured secret source.
func loadKey() {
	getKey()
}

// deriveKey keeps OPSPILOT_SECRET_KEY as the preferred independent key. A normal
// install already has a random, persisted JWT secret, so use a domain-separated
// derivative when no dedicated vault key was configured. The fixed fallback is
// retained only for local/dev compatibility.
func deriveKey(secret, jwt string) ([32]byte, bool) {
	if secret = strings.TrimSpace(secret); secret != "" {
		return sha256.Sum256([]byte(secret)), false
	}
	jwt = strings.TrimSpace(jwt)
	if jwt != "" && jwt != "dev-insecure-secret-change-me" && jwt != "change-me-to-a-long-random-string" {
		return sha256.Sum256([]byte("opspilot-secretbox-v1:" + jwt)), false
	}
	return sha256.Sum256([]byte(legacyFallback)), true
}

// KeyIsWeak reports whether neither a dedicated nor a real JWT secret exists.
func KeyIsWeak() bool {
	_, weak := getKey()
	return weak
}

// Encrypt seals plaintext with AES-256-GCM and returns the versioned,
// base64 wire string. Empty input returns empty (so an absent field stays
// absent rather than encrypting to noise).
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, _ := getKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Empty input returns empty. A value without the
// version prefix is treated as legacy plaintext and returned as-is (lets a
// pre-encryption row still read while we migrate forward).
func Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	if !strings.HasPrefix(enc, prefix) {
		return enc, nil // legacy plaintext — read through
	}
	key, _ := getKey()
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, prefix))
	if err != nil {
		return "", fmt.Errorf("secretbox: base64: %w", err)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("secretbox: ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// Rows written before JWT-derived fallback support used the fixed key.
		// Read them without forcing an operator migration; all new writes use the
		// stronger derived key when a real JWT secret is available.
		jwtKey, jwtWeak := deriveKey("", getJWT())
		legacy := sha256.Sum256([]byte(legacyFallback))
		for _, candidate := range [][32]byte{jwtKey, legacy} {
			if (candidate == jwtKey && jwtWeak) || candidate == key {
				continue
			}
			candidateBlock, blockErr := aes.NewCipher(candidate[:])
			if blockErr != nil {
				continue
			}
			candidateGCM, gcmErr := cipher.NewGCM(candidateBlock)
			if gcmErr != nil {
				continue
			}
			if candidateOut, openErr := candidateGCM.Open(nil, nonce, ct, nil); openErr == nil {
				return string(candidateOut), nil
			}
		}
		return "", fmt.Errorf("secretbox: open (wrong encryption key?): %w", err)
	}
	return string(out), nil
}
