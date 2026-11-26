package oncall

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const dingTalkMaxReplaySkew = 1 * time.Hour

// SignQuickActionURL builds an HMAC-SHA256 signed ChatOps action link for Feishu/DingTalk cards and alert emails.
func SignQuickActionURL(publicURL, token string, incID uint64, action string, ttl time.Duration) string {
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" || token == "" || incID == 0 {
		return ""
	}
	if !isAllowedQuickAction(action) {
		return ""
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expStr := strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	incStr := strconv.FormatUint(incID, 10)
	sig := computeQuickActionHMAC(token, incStr, action, expStr)

	q := url.Values{}
	q.Set("inc", incStr)
	q.Set("action", action)
	q.Set("exp", expStr)
	q.Set("sig", sig)
	return base + "/api/v1/oncall/chatops/quick-action?" + q.Encode()
}

// VerifyQuickAction validates the HMAC signature and expiration timestamp of a ChatOps quick-action link.
func VerifyQuickAction(token, incIDStr, action, expStr, sig string, now time.Time) error {
	if token == "" || incIDStr == "" || action == "" || expStr == "" || sig == "" {
		return errors.New("missing signed quick-action parameters")
	}
	if !isAllowedQuickAction(action) {
		return errors.New("unsupported quick action")
	}
	incID, err := strconv.ParseUint(incIDStr, 10, 64)
	if err != nil || incID == 0 {
		return errors.New("invalid incident id")
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return errors.New("invalid expiration timestamp")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if now.Unix() > expUnix {
		return errors.New("quick action link expired")
	}

	expected := computeQuickActionHMAC(token, incIDStr, action, expStr)
	if len(sig) != len(expected) || subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return errors.New("invalid quick action signature")
	}
	return nil
}

func computeQuickActionHMAC(token, incIDStr, action, expStr string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(fmt.Sprintf("%s:%s:%s", incIDStr, action, expStr)))
	return hex.EncodeToString(mac.Sum(nil))
}

func isAllowedQuickAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "ack", "silence", "escalate":
		return true
	default:
		return false
	}
}

// VerifyFeishuEventSignature validates X-Lark-Signature = sha256(timestamp + nonce + encryptKey + body).
func VerifyFeishuEventSignature(timestamp, nonce, encryptKey string, body []byte, signatureHex string) error {
	if encryptKey == "" || signatureHex == "" {
		return errors.New("feishu signature or encrypt key is empty")
	}
	h := sha256.New()
	h.Write([]byte(timestamp))
	h.Write([]byte(nonce))
	h.Write([]byte(encryptKey))
	h.Write(body)
	expected := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(signatureHex))), []byte(expected)) != 1 {
		return errors.New("feishu signature mismatch")
	}
	return nil
}

// DecryptFeishuEvent decrypts Feishu's AES-256-CBC encrypted event/card payload using SHA256(encryptKey).
func DecryptFeishuEvent(encryptKey, encryptedBase64 string) ([]byte, error) {
	if encryptKey == "" {
		return nil, errors.New("feishu encrypt_key is empty")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encryptedBase64))
	if err != nil {
		return nil, fmt.Errorf("feishu base64 decode: %w", err)
	}
	if len(raw) < aes.BlockSize*2 {
		return nil, errors.New("feishu ciphertext too short")
	}
	keyHash := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return nil, err
	}
	iv := raw[:aes.BlockSize]
	ct := raw[aes.BlockSize:]
	if len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("feishu ciphertext is not block aligned")
	}
	pt := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, ct)
	return unpadPKCS7(pt, aes.BlockSize)
}

func unpadPKCS7(b []byte, blockSize int) ([]byte, error) {
	if len(b) == 0 || blockSize <= 0 {
		return nil, errors.New("empty plaintext")
	}
	pad := int(b[len(b)-1])
	if pad <= 0 || pad > blockSize || pad > len(b) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for i := len(b) - pad; i < len(b); i++ {
		if int(b[i]) != pad {
			return nil, errors.New("invalid pkcs7 padding byte")
		}
	}
	return b[:len(b)-pad], nil
}

// VerifyWeComSignature validates WeCom CorpApp msg_signature = sha1(sort(token, timestamp, nonce, encrypt)).
func VerifyWeComSignature(token, timestamp, nonce, encrypt, msgSignature string) error {
	if token == "" || msgSignature == "" {
		return errors.New("wecom signature or token is empty")
	}
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	expected := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(msgSignature))), []byte(expected)) != 1 {
		return errors.New("wecom msg_signature mismatch")
	}
	return nil
}

// DecryptWeComMessage decrypts a WeCom CorpApp Base64 ciphertext using EncodingAESKey (AES-256-CBC, 32-byte PKCS#7).
func DecryptWeComMessage(encodingAESKey, encryptedBase64, expectedCorpID string) ([]byte, error) {
	keyStr := strings.TrimSpace(encodingAESKey)
	if keyStr == "" {
		return nil, errors.New("wecom encoding_aes_key is empty")
	}
	if !strings.HasSuffix(keyStr, "=") {
		keyStr += "="
	}
	aesKey, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil || len(aesKey) != 32 {
		return nil, errors.New("invalid wecom encoding_aes_key")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encryptedBase64))
	if err != nil {
		return nil, fmt.Errorf("wecom ciphertext base64 decode: %w", err)
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return nil, errors.New("wecom ciphertext is not block aligned")
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	pt := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, aesKey[:aes.BlockSize]).CryptBlocks(pt, raw)
	unpadded, err := unpadPKCS7(pt, 32)
	if err != nil {
		return nil, fmt.Errorf("wecom pkcs7 unpad: %w", err)
	}
	if len(unpadded) < 20 {
		return nil, errors.New("wecom decrypted payload too short")
	}
	msgLen := int(binary.BigEndian.Uint32(unpadded[16:20]))
	if msgLen < 0 || 20+msgLen > len(unpadded) {
		return nil, errors.New("invalid wecom embedded message length")
	}
	msg := unpadded[20 : 20+msgLen]
	corpID := string(unpadded[20+msgLen:])
	if expectedCorpID != "" && corpID != expectedCorpID {
		return nil, errors.New("wecom receiveid mismatch")
	}
	return msg, nil
}

// VerifyDingTalkCallbackSignature verifies DingTalk timestamp + "\n" + appSecret HMAC-SHA256 Base64 signature with replay protection.
func VerifyDingTalkCallbackSignature(timestampMs, appSecret, signHeader string, now time.Time) error {
	if appSecret == "" || timestampMs == "" || signHeader == "" {
		return errors.New("missing dingtalk callback signature headers")
	}
	tsVal, err := strconv.ParseInt(strings.TrimSpace(timestampMs), 10, 64)
	if err != nil {
		return errors.New("invalid dingtalk timestamp")
	}
	if now.IsZero() {
		now = time.Now()
	}
	diff := now.Sub(time.UnixMilli(tsVal))
	if diff < 0 {
		diff = -diff
	}
	if diff > dingTalkMaxReplaySkew {
		return errors.New("dingtalk callback timestamp outside allowed replay window")
	}

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(strings.TrimSpace(timestampMs) + "\n" + appSecret))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(signHeader)), []byte(expected)) != 1 {
		return errors.New("dingtalk callback signature mismatch")
	}
	return nil
}
