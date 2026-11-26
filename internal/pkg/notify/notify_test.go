package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouterSendUsesDefaultChannels(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		var payload Message
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Subject != "CPU high" {
			t.Errorf("subject = %q", payload.Subject)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	router := NewRouter(
		true,
		time.Second,
		[]string{"webhook"},
		NewGenericWebhookSender("webhook", srv.URL+"/notify", "secret", srv.Client()),
	)
	err := router.Send(context.Background(), Message{
		Subject:  "CPU high",
		Severity: SeverityWarning,
		Source:   "alert",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/notify" {
		t.Errorf("path = %q, want /notify", gotPath)
	}
}

func TestRouterSendUnknownChannel(t *testing.T) {
	router := NewRouter(true, time.Second, []string{"missing"})
	err := router.Send(context.Background(), Message{Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), `channel "missing" not configured`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRouterDisabledDropsMessage(t *testing.T) {
	router := NewRouter(false, time.Second, []string{"missing"})
	err := router.Send(context.Background(), Message{Subject: "x"})
	if err != nil {
		t.Fatalf("Send disabled: %v", err)
	}
}

func TestFeishuSenderSignsPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload["timestamp"] == "" {
			t.Errorf("timestamp missing")
		}
		if payload["sign"] == "" {
			t.Errorf("sign missing")
		}
		if payload["msg_type"] != "interactive" {
			t.Errorf("msg_type = %v, want interactive", payload["msg_type"])
		}
		if payload["card"] == nil {
			t.Errorf("card missing in payload")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewFeishuSender("feishu", srv.URL, "secret", srv.Client())
	err := sender.Send(context.Background(), Message{Subject: "edge offline"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestFeishuSenderEmitsInteractiveCard(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewFeishuSender("feishu-ops", srv.URL, "", srv.Client())
	occurred := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	err := sender.Send(context.Background(), Message{
		Subject:    "设备离线: 阿里云ECS01 心跳超时",
		Severity:   SeverityCritical,
		Source:     "global",
		DedupeKey:  "pipeline:device_offline:1",
		OccurredAt: occurred,
		Labels: map[string]string{
			"rule":        "device_offline",
			"rule_name":   "设备离线",
			"device_id":   "1",
			"device_name": "阿里云ECS01",
			"incident_id": "88",
			"runbook_url": "https://wiki.ops/runbook/device_offline",
			"console_url": "https://opspilot.example.com",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %v, want interactive", got["msg_type"])
	}

	card, ok := got["card"].(map[string]any)
	if !ok {
		t.Fatalf("card not a map: %v", got["card"])
	}

	header, ok := card["header"].(map[string]any)
	if !ok || header["template"] != "red" {
		t.Errorf("header template = %v, want red", header["template"])
	}

	elements, ok := card["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("elements missing or empty")
	}

	hasActions := false
	for _, el := range elements {
		if elm, ok := el.(map[string]any); ok && elm["tag"] == "action" {
			hasActions = true
			break
		}
	}
	if !hasActions {
		t.Errorf("expected action element in card elements, got %v", elements)
	}

	fieldsElem := elements[0].(map[string]any)["fields"].([]any)
	foundTime := false
	for _, f := range fieldsElem {
		fMap := f.(map[string]any)
		textMap := fMap["text"].(map[string]any)
		content := textMap["content"].(string)
		if strings.Contains(content, "发生时间") {
			foundTime = true
			if !strings.Contains(content, "2026-09-29 18:00:00") {
				t.Errorf("expected CST time 2026-09-29 18:00:00, got %s", content)
			}
		}
	}
	if !foundTime {
		t.Errorf("expected 发生时间 in card fields")
	}
}

func TestDingTalkSenderSignsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("timestamp") == "" {
			t.Errorf("timestamp query missing")
		}
		if r.URL.Query().Get("sign") == "" {
			t.Errorf("sign query missing")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewDingTalkSender("dingtalk", srv.URL, "secret", srv.Client())
	err := sender.Send(context.Background(), Message{Subject: "edge offline"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestDingTalkSenderEmitsActionCard(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewDingTalkSender("dingtalk-ops", srv.URL, "", srv.Client())
	occurred := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	err := sender.Send(context.Background(), Message{
		Subject:    "edge-node-01 离线",
		Severity:   SeverityCritical,
		OccurredAt: occurred,
		Body:       "Device edge-node-01 has been disconnected for more than 5 minutes",
		Source:     "alert-pipeline",
		Labels: map[string]string{
			"status":      "open",
			"rule_name":   "主机离线告警",
			"device_name": "edge-node-01",
			"device_id":   "42",
			"incident_id": "101",
			"console_url": "https://opspilot.example.com",
			"runbook_url": "https://opspilot.example.com/wiki/runbook",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got["msgtype"] != "actionCard" {
		t.Fatalf("expected msgtype=actionCard, got %v", got["msgtype"])
	}

	card, ok := got["actionCard"].(map[string]any)
	if !ok {
		t.Fatalf("actionCard is not a map: %v", got["actionCard"])
	}

	title, _ := card["title"].(string)
	if !strings.Contains(title, "🚨【严重告警】主机离线告警 - edge-node-01") {
		t.Errorf("expected title to contain 🚨【严重告警】主机离线告警 - edge-node-01, got %q", title)
	}

	text, _ := card["text"].(string)
	if !strings.Contains(text, "2026-05-30 18:00:00") {
		t.Errorf("expected text to contain CST time 2026-05-30 18:00:00, got %q", text)
	}
	if !strings.Contains(text, "CRITICAL (严重)") {
		t.Errorf("expected text to contain CRITICAL (严重), got %q", text)
	}
	if !strings.Contains(text, "OPEN (发生中)") {
		t.Errorf("expected text to contain OPEN (发生中), got %q", text)
	}
	if !strings.Contains(text, "#101") {
		t.Errorf("expected visible incident id #101 in text, got %q", text)
	}

	btns, ok := card["btns"].([]any)
	if !ok || len(btns) != 3 {
		t.Fatalf("expected 3 buttons, got %v", card["btns"])
	}
	foundIncidentDeepLink := false
	for _, raw := range btns {
		btn, _ := raw.(map[string]any)
		if btn["actionURL"] == "https://opspilot.example.com/alerts/incidents/101" {
			foundIncidentDeepLink = true
		}
	}
	if !foundIncidentDeepLink {
		t.Fatalf("expected incident deep link button, got %#v", btns)
	}
}

func TestDingTalkCardIgnoresResolvedSubstringInSubject(t *testing.T) {
	payload := formatDingTalkMessage(Message{
		Subject:  "unresolved errors above threshold on systemd-resolved",
		Severity: SeverityCritical,
		Labels: map[string]string{
			"status":      "open",
			"incident_id": "42",
			"console_url": "https://console.example",
			"device_id":   "7",
		},
	})
	if payload["msgtype"] != "actionCard" {
		t.Fatalf("msgtype = %v, want actionCard", payload["msgtype"])
	}
	card := payload["actionCard"].(map[string]any)
	title, _ := card["title"].(string)
	if strings.Contains(title, "告警恢复") {
		t.Fatalf("title must not be recovery for open critical: %q", title)
	}
	if !strings.Contains(title, "严重告警") {
		t.Fatalf("title = %q, want critical alert prefix", title)
	}
	text, _ := card["text"].(string)
	if !strings.Contains(text, "OPEN (发生中)") {
		t.Errorf("expected OPEN status localization, got %q", text)
	}
	if strings.Contains(text, "RESOLVED") {
		t.Errorf("must not mark resolved when Labels[status]=open: %q", text)
	}
	if !strings.Contains(text, "#42") {
		t.Errorf("expected visible incident id, got %q", text)
	}
}

func TestDingTalkSenderMarkdownFallback(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewDingTalkSender("dingtalk", srv.URL, "secret", srv.Client())
	err := sender.Send(context.Background(), Message{
		Subject:  "设备恢复正常",
		Severity: SeverityInfo,
		Labels: map[string]string{
			"status":    "resolved",
			"rule_name": "主机离线告警",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got["msgtype"] != "markdown" {
		t.Fatalf("expected msgtype=markdown when no action buttons, got %v", got["msgtype"])
	}

	md, ok := got["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("markdown is not a map: %v", got["markdown"])
	}

	title, _ := md["title"].(string)
	if !strings.Contains(title, "✅【告警恢复】主机离线告警") {
		t.Errorf("expected resolved title, got %q", title)
	}

	text, _ := md["text"].(string)
	if !strings.Contains(text, "RESOLVED (已恢复)") {
		t.Errorf("expected text to contain RESOLVED (已恢复), got %q", text)
	}
}

func TestFormatTextChineseCard(t *testing.T) {
	occurred := time.Date(2026, 10, 8, 22, 50, 0, 0, time.Local)
	msg := Message{
		Subject:    "cpu_high: 100 * (1 - avg by(device_id) (rate(node_cpu_seconds_total...))) ⇒ value=85.5",
		Severity:   SeverityCritical,
		Source:     "monitoring_pipeline",
		DedupeKey:  "pipeline:cpu_high:device_id=1,device_name=edge-01",
		OccurredAt: occurred,
		Labels: map[string]string{
			"rule":        "cpu_high",
			"rule_name":   "CPU 高负载",
			"device_id":   "1",
			"device_name": "edge-01",
			"incident_id": "123",
		},
	}
	text := formatText(msg)
	if !strings.Contains(text, "🚨 【严重告警】") {
		t.Errorf("expected severity badge, got:\n%s", text)
	}
	if !strings.Contains(text, "• 告警规则: CPU 高负载") {
		t.Errorf("expected rule name, got:\n%s", text)
	}
	if !strings.Contains(text, "• 关联设备: edge-01 (ID: 1)") {
		t.Errorf("expected device display, got:\n%s", text)
	}
	if !strings.Contains(text, "• 告警编号: #123") {
		t.Errorf("expected incident ID, got:\n%s", text)
	}
	if !strings.Contains(text, "• 告警来源: 监控管线") {
		t.Errorf("expected source, got:\n%s", text)
	}
	if !strings.Contains(text, "• 去重标识: pipeline:cpu_high:device_id=1") {
		t.Errorf("expected simplified dedupe key, got:\n%s", text)
	}
}

func TestSlackSenderEmitsAttachments(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewSlackSender("slack-ops", srv.URL, srv.Client())
	occurred := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	err := sender.Send(context.Background(), Message{
		Subject:    "Swap usage 84% on VM-4-8",
		Severity:   SeverityCritical,
		Source:     "alert-evaluator",
		DedupeKey:  "pipeline:swap_high:device_id=1",
		OccurredAt: occurred,
		Labels: map[string]string{
			"rule":        "swap_high",
			"incident_id": "42",
			"device_id":   "1",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if want := "[CRITICAL] Swap usage 84% on VM-4-8"; got["text"] != want {
		t.Errorf("text = %q, want %q", got["text"], want)
	}

	atts, ok := got["attachments"].([]any)
	if !ok || len(atts) != 1 {
		t.Fatalf("attachments = %v, want exactly 1", got["attachments"])
	}
	att := atts[0].(map[string]any)

	if att["color"] != "#d92f2f" {
		t.Errorf("color = %v, want #d92f2f", att["color"])
	}
	if att["title"] != "Swap usage 84% on VM-4-8" {
		t.Errorf("title = %v", att["title"])
	}
	if att["footer"] != "opspilot" {
		t.Errorf("footer = %v, want opspilot", att["footer"])
	}
	if got, want := att["ts"], float64(occurred.Unix()); got != want {
		t.Errorf("ts = %v, want %v", got, want)
	}
}

func TestSlackSenderColorByUnknownSeverity(t *testing.T) {
	if got := slackColor(Severity("bogus")); got == "" {
		t.Errorf("color empty for unknown severity")
	}
	cases := map[Severity]string{
		SeverityCritical: "#d92f2f",
		SeverityWarning:  "#f2c037",
		SeverityInfo:     "#36a64f",
	}
	for sev, want := range cases {
		if got := slackColor(sev); got != want {
			t.Errorf("slackColor(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestFeishuAndDingTalkCardsIncludeOnCallQuickActionButtons(t *testing.T) {
	ackURL := "https://opspilot.test/api/v1/oncall/chatops/quick-action?inc=17&action=ack&exp=9999999999&sig=abc"
	silenceURL := "https://opspilot.test/api/v1/oncall/chatops/quick-action?inc=17&action=silence&exp=9999999999&sig=def"
	escalateURL := "https://opspilot.test/api/v1/oncall/chatops/quick-action?inc=17&action=escalate&exp=9999999999&sig=ghi"
	consoleURL := "https://opspilot.test/oncall"

	msg := Message{
		Subject:    "CPU overload on prod-node-01",
		Body:       "usage 98%",
		Severity:   SeverityCritical,
		Source:     "monitoring_pipeline",
		OccurredAt: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		Labels: map[string]string{
			"incident_id":         "17",
			"rule_name":           "CPU 高负载",
			"console_url":         consoleURL,
			"oncall_ack_url":      ackURL,
			"oncall_silence_url":  silenceURL,
			"oncall_escalate_url": escalateURL,
		},
	}

	feishuRaw, err := json.Marshal(formatFeishuCard(msg))
	if err != nil {
		t.Fatalf("marshal feishu card: %v", err)
	}
	wantIncidentURL := consoleURL + "/alerts/incidents/17"
	feishuStr := strings.ReplaceAll(string(feishuRaw), `\u0026`, "&")
	for _, wantURL := range []string{ackURL, silenceURL, escalateURL, wantIncidentURL} {
		if !strings.Contains(feishuStr, wantURL) {
			t.Errorf("feishu card missing expected URL %q: %s", wantURL, feishuStr)
		}
	}

	dingRaw, err := json.Marshal(formatDingTalkMessage(msg))
	if err != nil {
		t.Fatalf("marshal dingtalk card: %v", err)
	}
	var dingMap map[string]any
	if err := json.Unmarshal(dingRaw, &dingMap); err != nil {
		t.Fatalf("unmarshal dingtalk card: %v", err)
	}
	if dingMap["msgtype"] != "actionCard" {
		t.Fatalf("dingtalk msgtype = %v, want actionCard", dingMap["msgtype"])
	}
	dingStr := strings.ReplaceAll(string(dingRaw), `\u0026`, "&")
	for _, wantURL := range []string{ackURL, silenceURL, escalateURL, wantIncidentURL} {
		if !strings.Contains(dingStr, wantURL) {
			t.Errorf("dingtalk card missing expected URL %q: %s", wantURL, dingStr)
		}
	}
}
