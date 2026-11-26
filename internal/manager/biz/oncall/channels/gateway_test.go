package channels

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestChannelGateway_SendAliyunAndTencentSMS(t *testing.T) {
	t.Run("aliyun ok and signature check", func(t *testing.T) {
		var gotBody string
		gw := &ChannelGateway{
			aliyunSMSEndpt: "https://dysmsapi.aliyuncs.com",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					raw, _ := io.ReadAll(r.Body)
					gotBody = string(raw)
					return jsonResp(http.StatusOK, `{"Code":"OK","Message":"OK","BizId":"biz_123","RequestId":"req_1"}`), nil
				}),
			},
		}
		cfg := &ChannelGatewayConfig{
			SMSEnabled:            true,
			SMSProvider:           "aliyun",
			AliyunAccessKeyID:     "LTAI5tTest",
			AliyunAccessKeySecret: "SecretVal",
			AliyunSignName:        "OpsPilot",
			AliyunTemplateCode:    "SMS_001",
			AliyunRegion:          "cn-hangzhou",
		}
		res, err := gw.SendSMS(context.Background(), cfg, &SMSRequest{
			PhoneNumber: "13800000001",
			Urgency:     "high",
			Title:       "Disk Full > 95%",
			IncidentID:  17,
		})
		if err != nil {
			t.Fatalf("SendSMS aliyun failed: %v", err)
		}
		if res.Status != "sent" || res.BizID != "biz_123" || res.Provider != "aliyun" {
			t.Fatalf("unexpected aliyun sms result: %+v", res)
		}
		if !strings.Contains(gotBody, "Action=SendSms") || !strings.Contains(gotBody, "Signature=") {
			t.Fatalf("expected signed POP params, got %q", gotBody)
		}
	})

	t.Run("tencent +86 prefix and business error handling", func(t *testing.T) {
		var gotAuth, gotPayload string
		gw := &ChannelGateway{
			tencentSMSEndpt: "https://sms.tencentcloudapi.com",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					gotAuth = r.Header.Get("Authorization")
					raw, _ := io.ReadAll(r.Body)
					gotPayload = string(raw)
					return jsonResp(http.StatusOK, `{"Response":{"SendStatusSet":[{"SerialNo":"ser_99","Code":"Ok","Message":"send success"}],"RequestId":"tx_req_1"}}`), nil
				}),
			},
		}
		cfg := &ChannelGatewayConfig{
			SMSEnabled:        true,
			SMSProvider:       "tencent",
			TencentSecretID:   "AKIDTest",
			TencentSecretKey:  "TxSecret",
			TencentSdkAppID:   "1400001",
			TencentSignName:   "OpsPilot",
			TencentTemplateID: "9988",
		}
		res, err := gw.SendSMS(context.Background(), cfg, &SMSRequest{
			PhoneNumber: "13800000002",
			Urgency:     "high",
			Title:       "Redis latency spike",
			IncidentID:  18,
		})
		if err != nil || res.Status != "sent" || res.BizID != "ser_99" {
			t.Fatalf("unexpected tencent sms result=%+v err=%v", res, err)
		}
		if !strings.HasPrefix(gotAuth, "TC3-HMAC-SHA256 ") || !strings.Contains(gotPayload, "+8613800000002") {
			t.Fatalf("unexpected tencent headers/payload auth=%q payload=%q", gotAuth, gotPayload)
		}
	})
}

func startMockSMTPServer(t *testing.T) (host string, port int, captured func() string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock smtp: %v", err)
	}
	var mu sync.Mutex
	var rawMsg strings.Builder
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = c.Write([]byte("220 mock-smtp.local ESMTP\r\n"))
				reader := bufio.NewReader(c)
				inData := false
				for {
					line, rErr := reader.ReadString('\n')
					if rErr != nil {
						return
					}
					if inData {
						if line == ".\r\n" {
							inData = false
							_, _ = c.Write([]byte("250 OK\r\n"))
							continue
						}
						mu.Lock()
						rawMsg.WriteString(line)
						mu.Unlock()
						continue
					}
					upper := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
						_, _ = c.Write([]byte("250-mock-smtp.local\r\n250 AUTH PLAIN\r\n"))
					case strings.HasPrefix(upper, "AUTH "):
						_, _ = c.Write([]byte("235 Authentication succeeded\r\n"))
					case strings.HasPrefix(upper, "MAIL FROM:"), strings.HasPrefix(upper, "RCPT TO:"):
						_, _ = c.Write([]byte("250 OK\r\n"))
					case upper == "DATA":
						inData = true
						_, _ = c.Write([]byte("354 Start mail input\r\n"))
					case upper == "QUIT":
						_, _ = c.Write([]byte("221 Bye\r\n"))
						return
					default:
						_, _ = c.Write([]byte("250 OK\r\n"))
					}
				}
			}(conn)
		}
	}()

	tcpAddr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", tcpAddr.Port, func() string {
		mu.Lock()
		defer mu.Unlock()
		return rawMsg.String()
	}, func() {
		_ = ln.Close()
		<-done
	}
}

func TestChannelGateway_SendSMTPEmailAndSanitizeHeaders(t *testing.T) {
	host, port, captured, stop := startMockSMTPServer(t)
	defer stop()

	gw := NewChannelGateway()
	cfg := &ChannelGatewayConfig{
		EmailEnabled:       true,
		SMTPHost:           host,
		SMTPPort:           port,
		SMTPTLSMode:        "none",
		SMTPTimeoutSeconds: 5,
		SMTPFrom:           "oncall@opspilot.local",
		SMTPUsername:       "oncall@opspilot.local",
		SMTPPassword:       "app-secret",
	}

	// Valid delivery with Chinese subject & action buttons.
	res, err := gw.SendEmail(context.Background(), cfg, &EmailRequest{
		To:       "alice@opspilot.local",
		Subject:  "核心网关严重告警\r\nBcc: attacker@evil.local",
		TextBody: "CPU > 95% on node-01",
		ActionLinks: map[string]string{
			"认领告警": "https://opspilot.test/api/v1/oncall/chatops/quick-action?inc=17&action=ack",
		},
	})
	if err != nil || res.Status != "sent" {
		t.Fatalf("expected email sent cleanly, got res=%+v err=%v", res, err)
	}
	msg := captured()
	if strings.Contains(msg, "\nBcc:") || !strings.Contains(msg, "multipart/alternative") {
		t.Fatalf("expected sanitized subject & multipart MIME payload, got:\n%s", msg)
	}
	if !strings.Contains(msg, "opspilot.test/api/v1/oncall/chatops/quick-action") {
		t.Fatalf("expected action link inside HTML body, got:\n%s", msg)
	}

	// CRLF in recipient must be rejected before dialing.
	_, injErr := gw.SendEmail(context.Background(), cfg, &EmailRequest{
		To:      "alice@opspilot.local\r\nBcc: evil@evil.local",
		Subject: "test",
	})
	if injErr == nil || !IsPermanentConfigErr(injErr.Error()) {
		t.Fatalf("expected permanent header validation error for CRLF recipient, got %v", injErr)
	}
	_ = strconv.Itoa(port)
}

func TestChannelGateway_CreateWarRoomModes(t *testing.T) {
	t.Run("feishu open api creates group and applink", func(t *testing.T) {
		var tokenCalls, chatCalls, msgCalls int
		gw := &ChannelGateway{
			feishuAPIBase: "https://open.feishu.cn",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
						tokenCalls++
						return jsonResp(http.StatusOK, `{"code":0,"tenant_access_token":"t-123"}`), nil
					case strings.HasSuffix(r.URL.Path, "/open-apis/im/v1/chats"):
						chatCalls++
						return jsonResp(http.StatusOK, `{"code":0,"data":{"chat_id":"oc_warroom_88"}}`), nil
					case strings.Contains(r.URL.Path, "/open-apis/im/v1/messages"):
						msgCalls++
						return jsonResp(http.StatusOK, `{"code":0,"data":{"message_id":"om_1"}}`), nil
					default:
						return jsonResp(http.StatusNotFound, `{}`), nil
					}
				}),
			},
		}
		cfg := &ChannelGatewayConfig{
			WarRoomProvider: "feishu",
			FeishuAppID:     "cli_app_1",
			FeishuAppSecret: "cli_secret_1",
		}
		res, err := gw.CreateWarRoom(context.Background(), cfg, &WarRoomCreateRequest{
			IncidentID: 42,
			Title:      "Payment DB down",
			Severity:   "critical",
			Operator:   "Alice",
			ConsoleURL: "https://opspilot.test",
		})
		if err != nil {
			t.Fatalf("CreateWarRoom feishu failed: %v", err)
		}
		if res.Provider != "feishu" || res.ExternalChatID != "oc_warroom_88" ||
			!strings.Contains(res.RoomURL, "applink.feishu.cn/client/chat/open?openChatId=oc_warroom_88") {
			t.Fatalf("unexpected feishu warroom result: %+v", res)
		}
		if tokenCalls != 1 || chatCalls != 1 || msgCalls != 1 {
			t.Fatalf("expected 1 token/chat/msg call, got %d/%d/%d", tokenCalls, chatCalls, msgCalls)
		}
	})

	t.Run("webhook signs payload and extracts url", func(t *testing.T) {
		var gotSig string
		gw := &ChannelGateway{
			httpClient: &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					body, _ := io.ReadAll(r.Body)
					gotSig = r.Header.Get("X-OpsPilot-Signature")
					mac := hmac.New(sha256.New, []byte("hook-sec"))
					mac.Write(body)
					wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
					if gotSig != wantSig {
						t.Fatalf("signature mismatch: got %q want %q", gotSig, wantSig)
					}
					return jsonResp(http.StatusOK, `{"war_room_url":"https://chat.example.com/room/42","chat_id":"rm_42"}`), nil
				}),
			},
		}
		cfg := &ChannelGatewayConfig{
			WarRoomProvider:      "webhook",
			WarRoomWebhookURL:    "https://ops-bot.example.com/warroom",
			WarRoomWebhookSecret: "hook-sec",
		}
		res, err := gw.CreateWarRoom(context.Background(), cfg, &WarRoomCreateRequest{
			IncidentID: 42,
			Title:      "Payment DB down",
		})
		if err != nil || res.Provider != "webhook" || res.RoomURL != "https://chat.example.com/room/42" {
			t.Fatalf("unexpected webhook warroom result=%+v err=%v", res, err)
		}
	})

	t.Run("console fallback never fabricates fake feishu link", func(t *testing.T) {
		gw := NewChannelGateway()
		res, err := gw.CreateWarRoom(context.Background(), &ChannelGatewayConfig{}, &WarRoomCreateRequest{
			IncidentID: 99,
			Title:      "Disk Alert",
			ConsoleURL: "https://opspilot.test",
		})
		if err != nil {
			t.Fatalf("console fallback error: %v", err)
		}
		if res.Provider != "console" || res.RoomURL != "https://opspilot.test/alerts/incidents/99" || strings.Contains(res.RoomURL, "feishu.cn/warroom") {
			t.Fatalf("unexpected console fallback result: %+v", res)
		}
	})
}
