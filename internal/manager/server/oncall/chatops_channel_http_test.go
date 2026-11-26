package oncall

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	iammodel "github.com/Zara1024/OpsPilot/internal/iam/model"
	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

func encryptFeishuTestPayload(t *testing.T, encryptKey string, plaintext []byte) string {
	t.Helper()
	keyHash := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		t.Fatalf("aes cipher: %v", err)
	}
	pad := aes.BlockSize - (len(plaintext) % aes.BlockSize)
	padded := append([]byte{}, plaintext...)
	for i := 0; i < pad; i++ {
		padded = append(padded, byte(pad))
	}
	iv := make([]byte, aes.BlockSize)
	for i := range iv {
		iv[i] = byte(i + 1)
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	return base64.StdEncoding.EncodeToString(append(iv, ct...))
}

func TestChannelConfig_RBACAndNoPlaintextLeak(t *testing.T) {
	_, _, _, r := newTestHTTPHandler(t)
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}
	userTenant := tenantctx.Tenant{UserID: 2, Email: "alice@opspilot.local", Role: iammodel.RoleUser}

	// Non-admin write & live tests must return 403.
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, reqWithTenant(http.MethodPut, "/v1/oncall/channels/config", `{"sms_enabled":true,"sms_provider":"aliyun"}`, userTenant))
	if w1.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin PUT channel config, got %d", w1.Code)
	}
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, reqWithTenant(http.MethodPost, "/v1/oncall/channels/test-sms", `{"phone_number":"13800000001"}`, userTenant))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin POST test-sms, got %d", w2.Code)
	}

	// Admin updates secrets -> response masks plaintext and sets *_configured=true + callback_urls.
	body := `{
		"sms_enabled":true,
		"sms_provider":"aliyun",
		"aliyun_access_key_id":"LTAI5abc",
		"aliyun_access_key_secret":"sms-secret-999",
		"email_enabled":true,
		"smtp_host":"smtp.example.com",
		"smtp_port":465,
		"smtp_password":"smtp-secret-888",
		"smtp_from":"oncall@opspilot.local",
		"feishu_verification_token":"vtok-111",
		"feishu_encrypt_key":"ekey-222",
		"dingtalk_app_secret":"dsec-333"
	}`
	wSave := httptest.NewRecorder()
	r.ServeHTTP(wSave, reqWithTenant(http.MethodPut, "/v1/oncall/channels/config", body, adminTenant))
	if wSave.Code != http.StatusOK {
		t.Fatalf("expected 200 saving channel config, got %d (%s)", wSave.Code, wSave.Body.String())
	}
	respStr := wSave.Body.String()
	for _, secret := range []string{"sms-secret-999", "smtp-secret-888", "vtok-111", "ekey-222", "dsec-333"} {
		if strings.Contains(respStr, secret) {
			t.Fatalf("plaintext secret %q leaked in PUT response", secret)
		}
	}

	var parsed struct {
		OK     bool                          `json:"ok"`
		Config channels.ChannelGatewayConfig `json:"config"`
	}
	if err := json.Unmarshal(wSave.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode save resp: %v", err)
	}
	if !parsed.Config.AliyunSecretConfigured || !parsed.Config.SMTPPasswordConfigured ||
		!parsed.Config.FeishuEncryptConfigured || !parsed.Config.DingTalkSecretConfigured ||
		parsed.Config.CallbackURLs["feishu"] == "" || parsed.Config.CallbackURLs["dingtalk"] == "" {
		t.Fatalf("unexpected sanitized channel config response: %+v", parsed.Config)
	}

	// Non-admin GET must NOT see callback URLs.
	wGetUser := httptest.NewRecorder()
	r.ServeHTTP(wGetUser, reqWithTenant(http.MethodGet, "/v1/oncall/channels/config", "", userTenant))
	if wGetUser.Code != http.StatusOK {
		t.Fatalf("GET channel config user failed: %d", wGetUser.Code)
	}
	var userCfg channels.ChannelGatewayConfig
	_ = json.Unmarshal(wGetUser.Body.Bytes(), &userCfg)
	if len(userCfg.CallbackURLs) != 0 {
		t.Fatalf("expected no callback_urls exposed to non-admin, got %+v", userCfg.CallbackURLs)
	}
}

func TestChatOpsCallbacks_QuickActionFeishuDingTalkAndWarRoom(t *testing.T) {
	_, s, db, r := newTestHTTPHandler(t)
	ctx := context.Background()
	now := time.Now().UTC()
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}

	if err := s.UpdateChannelConfig(ctx, &channels.ChannelGatewayConfig{
		FeishuVerificationToken: "feishu-vtok",
		FeishuEncryptKey:        "feishu-enc-key-32bytes",
		DingTalkAppSecret:       "dingtalk-sec-1",
	}); err != nil {
		t.Fatalf("UpdateChannelConfig: %v", err)
	}
	tok, err := s.EnsureChatOpsCallbackToken(ctx)
	if err != nil || tok == "" {
		t.Fatalf("EnsureChatOpsCallbackToken: %v", err)
	}

	for _, id := range []uint64{401, 402, 403} {
		_ = db.Create(&alertmodel.Incident{
			ID: id, Title: "Node CPU spike", Severity: "critical", Status: alertmodel.StatusOpen,
			DedupeKey: "chatops-" + strconv.FormatUint(id, 10), FirstFiredAt: now, LastFiredAt: now,
		}).Error
	}

	// 1. Signed quick-action: GET must be read-only confirmation; POST executes transition.
	signed := bizoncall.SignQuickActionURL("https://opspilot.test", tok, 401, "ack", time.Hour)
	parsedURL, _ := url.Parse(signed)
	pathAndQuery := "/v1/oncall/chatops/quick-action?" + parsedURL.RawQuery

	wGetQA := httptest.NewRecorder()
	r.ServeHTTP(wGetQA, httptest.NewRequest(http.MethodGet, pathAndQuery, nil))
	if wGetQA.Code != http.StatusOK || !strings.Contains(wGetQA.Body.String(), "<form method=\"POST\"") {
		t.Fatalf("expected read-only HTML confirm form, got %d %s", wGetQA.Code, wGetQA.Body.String())
	}
	var inc401 alertmodel.Incident
	_ = db.First(&inc401, 401).Error
	if inc401.Status != alertmodel.StatusOpen {
		t.Fatalf("GET quick-action must NOT mutate incident status, got %q", inc401.Status)
	}

	form := parsedURL.Query().Encode()
	postQA := httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/quick-action", strings.NewReader(form))
	postQA.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postQA.Header.Set("Accept", "application/json")
	wPostQA := httptest.NewRecorder()
	r.ServeHTTP(wPostQA, postQA)
	if wPostQA.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST quick-action, got %d %s", wPostQA.Code, wPostQA.Body.String())
	}
	_ = db.First(&inc401, 401).Error
	if inc401.Status != alertmodel.StatusAcknowledged {
		t.Fatalf("expected incident 401 acknowledged after POST quick-action, got %q", inc401.Status)
	}

	// Invalid signature must fail with 401.
	wBadQA := httptest.NewRecorder()
	r.ServeHTTP(wBadQA, httptest.NewRequest(http.MethodGet, pathAndQuery+"tamper", nil))
	if wBadQA.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered quick-action, got %d", wBadQA.Code)
	}

	// 2. Feishu URL verification + Encrypted AES-256-CBC card callback.
	urlVerifyBody := `{"type":"url_verification","challenge":"challenge_ok_123","token":"feishu-vtok"}`
	wUV := httptest.NewRecorder()
	r.ServeHTTP(wUV, httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/callback/feishu", strings.NewReader(urlVerifyBody)))
	if wUV.Code != http.StatusOK || !strings.Contains(wUV.Body.String(), "challenge_ok_123") {
		t.Fatalf("expected feishu url_verification challenge echo, got %d %s", wUV.Code, wUV.Body.String())
	}

	encCipher := encryptFeishuTestPayload(t, "feishu-enc-key-32bytes",
		[]byte(`{"event":{"operator":{"open_id":"ou_123"},"action":{"value":{"opspilot_action":"ack","incident_id":"402"}}}}`))
	envBytes, _ := json.Marshal(map[string]string{"encrypt": encCipher})
	ts := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce1"
	sum := sha256.Sum256([]byte(ts + nonce + "feishu-enc-key-32bytes" + string(envBytes)))
	feishuReq := httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/callback/feishu", bytes.NewReader(envBytes))
	feishuReq.Header.Set("X-Lark-Request-Timestamp", ts)
	feishuReq.Header.Set("X-Lark-Request-Nonce", nonce)
	feishuReq.Header.Set("X-Lark-Signature", hex.EncodeToString(sum[:]))
	wFeishu := httptest.NewRecorder()
	r.ServeHTTP(wFeishu, feishuReq)
	if wFeishu.Code != http.StatusOK || !strings.Contains(wFeishu.Body.String(), "toast") {
		t.Fatalf("expected 200 toast on encrypted feishu card callback, got %d %s", wFeishu.Code, wFeishu.Body.String())
	}
	var inc402 alertmodel.Incident
	_ = db.First(&inc402, 402).Error
	if inc402.Status != alertmodel.StatusAcknowledged {
		t.Fatalf("expected incident 402 acknowledged by encrypted feishu card, got %q", inc402.Status)
	}

	// 3. DingTalk HMAC-SHA256 callback + replay rejection.
	tsMs := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte("dingtalk-sec-1"))
	mac.Write([]byte(tsMs + "\ndingtalk-sec-1"))
	dingSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	dingReq := httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/callback/dingtalk",
		strings.NewReader(`{"action":"silence","incident_id":"403","userId":"ding_u1"}`))
	dingReq.Header.Set("timestamp", tsMs)
	dingReq.Header.Set("sign", dingSig)
	wDing := httptest.NewRecorder()
	r.ServeHTTP(wDing, dingReq)
	if wDing.Code != http.StatusOK {
		t.Fatalf("expected 200 on signed dingtalk callback, got %d %s", wDing.Code, wDing.Body.String())
	}
	var inc403 alertmodel.Incident
	_ = db.First(&inc403, 403).Error
	if inc403.Status != alertmodel.StatusSilenced {
		t.Fatalf("expected incident 403 silenced by dingtalk card, got %q", inc403.Status)
	}

	// Stale timestamp outside 1h window -> 401.
	oldMs := strconv.FormatInt(now.Add(-2*time.Hour).UnixMilli(), 10)
	oldMac := hmac.New(sha256.New, []byte("dingtalk-sec-1"))
	oldMac.Write([]byte(oldMs + "\ndingtalk-sec-1"))
	staleReq := httptest.NewRequest(http.MethodPost, "/v1/oncall/chatops/callback/dingtalk", strings.NewReader(`{"action":"ack","incident_id":"403"}`))
	staleReq.Header.Set("timestamp", oldMs)
	staleReq.Header.Set("sign", base64.StdEncoding.EncodeToString(oldMac.Sum(nil)))
	wStale := httptest.NewRecorder()
	r.ServeHTTP(wStale, staleReq)
	if wStale.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for stale dingtalk timestamp, got %d", wStale.Code)
	}

	// 4. WarRoom creation persisted & hydrated on active incidents list.
	wWR := httptest.NewRecorder()
	r.ServeHTTP(wWR, reqWithTenant(http.MethodPost, "/v1/oncall/chatops/callback", `{"action":"warroom","incident_id":"INC-0402"}`, adminTenant))
	if wWR.Code != http.StatusOK {
		t.Fatalf("expected 200 creating warroom, got %d (%s)", wWR.Code, wWR.Body.String())
	}
	var wrResp bizoncall.ChatOpsActionResult
	_ = json.Unmarshal(wWR.Body.Bytes(), &wrResp)
	if wrResp.WarRoomURL != "https://opspilot.test/alerts/incidents/402" {
		t.Fatalf("expected console war_room_url fallback, got %q", wrResp.WarRoomURL)
	}

	wInc := httptest.NewRecorder()
	r.ServeHTTP(wInc, reqWithTenant(http.MethodGet, "/v1/oncall/incidents/active", "", adminTenant))
	if wInc.Code != http.StatusOK || !strings.Contains(wInc.Body.String(), "https://opspilot.test/alerts/incidents/402") {
		t.Fatalf("expected war_room_url hydrated in /incidents/active, got %d %s", wInc.Code, wInc.Body.String())
	}
}

func encryptWeComTestPayload(t *testing.T, encodingAESKey, corpID string, plaintext []byte) string {
	t.Helper()
	keyStr := encodingAESKey
	if !strings.HasSuffix(keyStr, "=") {
		keyStr += "="
	}
	aesKey, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil || len(aesKey) != 32 {
		t.Fatalf("invalid test encodingAESKey: %v len=%d", err, len(aesKey))
	}
	buf := make([]byte, 0, 20+len(plaintext)+len(corpID)+32)
	buf = append(buf, bytes.Repeat([]byte{'r'}, 16)...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(plaintext)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, plaintext...)
	buf = append(buf, []byte(corpID)...)
	padLen := 32 - (len(buf) % 32)
	buf = append(buf, bytes.Repeat([]byte{byte(padLen)}, padLen)...)

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	ct := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, aesKey[:aes.BlockSize]).CryptBlocks(ct, buf)
	return base64.StdEncoding.EncodeToString(ct)
}

func weComTestSignature(token, ts, nonce, encrypt string) string {
	parts := []string{token, ts, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func TestWeComChatOps_VerifyAndEncryptedCallback(t *testing.T) {
	_, _, db, r := newTestHTTPHandler(t)
	now := time.Now().UTC()
	adminTenant := tenantctx.Tenant{UserID: 1, Email: "admin@opspilot.local", Role: iammodel.RoleAdmin, IsSuperuser: true}

	const (
		corpID         = "ww_test_corp_01"
		wecomToken     = "wecom_cb_tok_secret"
		encodingAESKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	)

	// 1. Save WeCom CorpApp config via PUT /v1/oncall/channels/config -> no plaintext leak, exposes callback_urls["wecom"].
	body, _ := json.Marshal(map[string]any{
		"wecom_corp_id":          corpID,
		"wecom_token":            wecomToken,
		"wecom_encoding_aes_key": encodingAESKey,
	})
	wSave := httptest.NewRecorder()
	r.ServeHTTP(wSave, reqWithTenant(http.MethodPut, "/v1/oncall/channels/config", string(body), adminTenant))
	if wSave.Code != http.StatusOK {
		t.Fatalf("PUT channel config with wecom failed: %d (%s)", wSave.Code, wSave.Body.String())
	}
	if strings.Contains(wSave.Body.String(), wecomToken) || strings.Contains(wSave.Body.String(), encodingAESKey) {
		t.Fatalf("expected WeCom secrets masked in PUT response, got %s", wSave.Body.String())
	}
	var saveResp struct {
		OK     bool                          `json:"ok"`
		Config channels.ChannelGatewayConfig `json:"config"`
	}
	_ = json.Unmarshal(wSave.Body.Bytes(), &saveResp)
	if !saveResp.Config.WeComTokenConfigured || !saveResp.Config.WeComAESKeyConfigured ||
		saveResp.Config.WeComCorpID != corpID || saveResp.Config.CallbackURLs["wecom"] == "" {
		t.Fatalf("unexpected WeCom config response: %+v", saveResp.Config)
	}

	// 2. GET /v1/oncall/chatops/callback/wecom decrypts echostr on valid SHA-1 signature, rejects invalid signature.
	ts := strconv.FormatInt(now.Unix(), 10)
	nonce := "wc_nonce_1"
	encEcho := encryptWeComTestPayload(t, encodingAESKey, corpID, []byte("wecom_echo_verified_987"))
	sigEcho := weComTestSignature(wecomToken, ts, nonce, encEcho)

	verifyURL := "/v1/oncall/chatops/callback/wecom?msg_signature=" + url.QueryEscape(sigEcho) +
		"&timestamp=" + url.QueryEscape(ts) +
		"&nonce=" + url.QueryEscape(nonce) +
		"&echostr=" + url.QueryEscape(encEcho)
	wVerify := httptest.NewRecorder()
	r.ServeHTTP(wVerify, httptest.NewRequest(http.MethodGet, verifyURL, nil))
	if wVerify.Code != http.StatusOK || wVerify.Body.String() != "wecom_echo_verified_987" {
		t.Fatalf("expected decrypted wecom echostr, got %d (%q)", wVerify.Code, wVerify.Body.String())
	}

	wBadSig := httptest.NewRecorder()
	r.ServeHTTP(wBadSig, httptest.NewRequest(http.MethodGet, strings.Replace(verifyURL, sigEcho, "deadbeef", 1), nil))
	if wBadSig.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad WeCom signature, got %d", wBadSig.Code)
	}

	// 3. POST /v1/oncall/chatops/callback/wecom with encrypted template_card_event XML acknowledges incident.
	_ = db.Create(&alertmodel.Incident{
		ID: 409, Title: "WeCom Card Alert", Severity: "critical", Status: alertmodel.StatusOpen,
		DedupeKey: "wecom-409", FirstFiredAt: now, LastFiredAt: now,
	}).Error
	xmlPlain := `<xml><ToUserName><![CDATA[ww_test_corp_01]]></ToUserName><FromUserName><![CDATA[zhangsan]]></FromUserName><Event><![CDATA[template_card_event]]></Event><EventKey><![CDATA[ack:409]]></EventKey></xml>`
	encEvent := encryptWeComTestPayload(t, encodingAESKey, corpID, []byte(xmlPlain))
	sigEvent := weComTestSignature(wecomToken, ts, nonce, encEvent)
	envelope := `<xml><ToUserName><![CDATA[` + corpID + `]]></ToUserName><Encrypt><![CDATA[` + encEvent + `]]></Encrypt></xml>`

	postURL := "/v1/oncall/chatops/callback/wecom?msg_signature=" + url.QueryEscape(sigEvent) +
		"&timestamp=" + url.QueryEscape(ts) + "&nonce=" + url.QueryEscape(nonce)
	wPost := httptest.NewRecorder()
	r.ServeHTTP(wPost, httptest.NewRequest(http.MethodPost, postURL, strings.NewReader(envelope)))
	if wPost.Code != http.StatusOK || !strings.Contains(wPost.Body.String(), `"errcode":0`) {
		t.Fatalf("expected 200 errcode=0 on encrypted WeCom XML callback, got %d (%s)", wPost.Code, wPost.Body.String())
	}
	var inc409 alertmodel.Incident
	_ = db.First(&inc409, 409).Error
	if inc409.Status != alertmodel.StatusAcknowledged {
		t.Fatalf("expected incident 409 acknowledged via WeCom callback, got %q", inc409.Status)
	}
}
