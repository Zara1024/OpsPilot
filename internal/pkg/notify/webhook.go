package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type webhookSender struct {
	name       string
	endpoint   string
	secret     string
	client     *http.Client
	buildBody  func(Message) (any, error)
	signTarget func(endpoint, secret string, body []byte) (string, map[string]string, error)
}

// NewGenericWebhookSender posts the normalized Message JSON. When secret is
// configured it adds an HMAC signature header over the request body.
func NewGenericWebhookSender(name, endpoint, secret string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, secret, client, func(msg Message) (any, error) {
		return msg, nil
	}, signGenericWebhook)
}

// NewSlackSender posts to a Slack incoming webhook in the attachments
// format so the alert renders with severity-tinted color bar + structured
// fields (Severity / Source / Rule / Incident / Device / Dedupe) instead
// of an unstyled paragraph. Slack incoming webhooks ignore any secret —
// the credential is the URL itself — so the secret field is silently
// dropped at the channel-builder layer.
func NewSlackSender(name, endpoint string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, "", client, func(msg Message) (any, error) {
		return formatSlack(msg), nil
	}, nil)
}

// formatSlack renders one Message as a Slack incoming-webhook payload using
// the attachments format. We pick attachments over Block Kit because:
//   - it carries the colored side-bar that operators read as "how bad",
//   - it's the universally-supported format (Block Kit needs newer apps),
//   - the schema is JSON-flat and easy to test.
//
// "text" at the top stays populated with a one-line summary so Slack's own
// notification preview (push, sidebar, email digest) shows something useful
// even when the recipient client strips attachments.
func formatSlack(msg Message) map[string]any {
	sevUpper := strings.ToUpper(string(msg.Severity))
	if sevUpper == "" {
		sevUpper = "ALERT"
	}
	summary := fmt.Sprintf("[%s] %s", sevUpper, msg.Subject)

	att := map[string]any{
		"color":    slackColor(msg.Severity),
		"fallback": summary,
		"title":    nonEmpty(msg.Subject, sevUpper),
	}
	if msg.Body != "" {
		att["text"] = msg.Body
		att["mrkdwn_in"] = []string{"text"}
	}

	fields := make([]map[string]any, 0, 6)
	addField := func(title, value string, short bool) {
		if value == "" {
			return
		}
		fields = append(fields, map[string]any{
			"title": title,
			"value": value,
			"short": short,
		})
	}
	addField("Severity", sevUpper, true)
	addField("Source", msg.Source, true)
	if msg.Labels != nil {
		// Surface the alert-pipeline labels operators care about as
		// short fields; the remaining labels stay out of the message
		// to keep the card readable. Rule/incident/device are the same
		ruleVal := msg.Labels["rule_name"]
		if ruleVal == "" {
			ruleVal = msg.Labels["rule"]
		}
		addField("Rule", ruleVal, true)
		if id := msg.Labels["incident_id"]; id != "" {
			addField("Incident", "#"+id, true)
		}
		deviceVal := msg.Labels["device"]
		if deviceVal == "" && msg.Labels["device_id"] != "" {
			deviceVal = "#" + msg.Labels["device_id"]
		}
		if deviceVal != "" {
			addField("Device", deviceVal, true)
		}
	}
	// Dedupe key is the join key for ops chatter — keep full width so
	// long pipeline:rule:label-set strings stay readable.
	addField("Dedupe key", msg.DedupeKey, false)
	if len(fields) > 0 {
		att["fields"] = fields
	}

	att["footer"] = "opspilot"
	if !msg.OccurredAt.IsZero() {
		att["ts"] = msg.OccurredAt.Unix()
	}

	return map[string]any{
		"text":        summary,
		"attachments": []any{att},
	}
}

// slackColor maps a Severity onto the Slack attachment color rail. Critical
// uses the red the Slack sentinel "danger" resolves to but as a hex so we
// pin the shade across Slack client versions; same idea for warning.
// Unknown severities get a neutral slate so the rail still renders.
func slackColor(sev Severity) string {
	switch sev {
	case SeverityCritical:
		return "#d92f2f"
	case SeverityWarning:
		return "#f2c037"
	case SeverityInfo:
		return "#36a64f"
	default:
		return "#6f7a87"
	}
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// NewFeishuSender posts a text payload compatible with Feishu/Lark custom bots.
func NewFeishuSender(name, endpoint, secret string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, secret, client, func(msg Message) (any, error) {
		payload := map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": formatText(msg)},
		}
		if secret != "" {
			ts := fmt.Sprintf("%d", time.Now().Unix())
			payload["timestamp"] = ts
			payload["sign"] = signFeishu(ts, secret)
		}
		return payload, nil
	}, nil)
}

// NewDingTalkSender posts a text payload compatible with DingTalk custom bots.
func NewDingTalkSender(name, endpoint, secret string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, secret, client, func(msg Message) (any, error) {
		return map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": formatText(msg)},
		}, nil
	}, signDingTalkURL)
}

// NewWeComSender posts a text payload compatible with 企业微信 (WeCom) group
// bots. Endpoint URL carries the bot key as a query param; the v1 wiring
// has no extra signing — the secret query string IS the credential. Same
// JSON shape as DingTalk: {"msgtype":"text","text":{"content":"..."}}.
func NewWeComSender(name, endpoint string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, "", client, func(msg Message) (any, error) {
		return map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": formatText(msg)},
		}, nil
	}, nil)
}

// NewTelegramSender posts to the Telegram Bot API sendMessage endpoint.
// endpoint is the full https://api.telegram.org/bot<TOKEN>/sendMessage URL
// (bot token in the path); chatID is the target chat, sent in the JSON
// body. Telegram's auth model differs from the webhook channels — token in
// the URL, chat_id in the body — so it doesn't use the secret/signing path.
func NewTelegramSender(name, endpoint, chatID string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, "", client, func(msg Message) (any, error) {
		return map[string]any{
			"chat_id": chatID,
			"text":    formatText(msg),
		}, nil
	}, nil)
}

func newWebhookSender(
	name string,
	endpoint string,
	secret string,
	client *http.Client,
	buildBody func(Message) (any, error),
	signTarget func(endpoint, secret string, body []byte) (string, map[string]string, error),
) Sender {
	if name == "" {
		name = "webhook"
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &webhookSender{
		name:       name,
		endpoint:   endpoint,
		secret:     secret,
		client:     client,
		buildBody:  buildBody,
		signTarget: signTarget,
	}
}

func (s *webhookSender) Name() string { return s.name }

func (s *webhookSender) Send(ctx context.Context, msg Message) error {
	if s.endpoint == "" {
		return fmt.Errorf("endpoint required")
	}
	payload, err := s.buildBody(msg)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	endpoint := s.endpoint
	headers := map[string]string{}
	if s.signTarget != nil {
		endpoint, headers, err = s.signTarget(s.endpoint, s.secret, body)
		if err != nil {
			return fmt.Errorf("sign request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "opspilot-notify/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}

func formatSeverityBadge(sev Severity) string {
	switch strings.ToLower(string(sev)) {
	case string(SeverityCritical):
		return "🚨 【严重告警】"
	case string(SeverityWarning):
		return "⚠️ 【告警提醒】"
	case string(SeverityInfo):
		return "ℹ️ 【信息提醒】"
	default:
		if sev == "" {
			return "⚠️ 【告警提醒】"
		}
		return fmt.Sprintf("【%s】", strings.ToUpper(string(sev)))
	}
}

func formatSeverityName(sev Severity) string {
	switch strings.ToLower(string(sev)) {
	case string(SeverityCritical):
		return "严重"
	case string(SeverityWarning):
		return "警告"
	case string(SeverityInfo):
		return "提醒"
	default:
		return strings.ToUpper(string(sev))
	}
}

func formatSourceCN(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "host":
		return "主机监控"
	case "global":
		return "全局监控"
	case "monitoring_pipeline":
		return "监控管线"
	case "channel_test":
		return "渠道连通性测试"
	case "alert-evaluator":
		return "告警引擎"
	default:
		return source
	}
}

func ruleNameCN(ruleKey string) string {
	switch strings.ToLower(strings.TrimSpace(ruleKey)) {
	case "device_offline", "edge_offline":
		return "设备离线"
	case "cpu_high", "cpu_high_default", "cpu_high_80":
		return "CPU 高负载"
	case "mem_high":
		return "内存高占用"
	case "disk_high", "disk_full_warning":
		return "磁盘空间不足"
	case "load1_high":
		return "系统负载过高"
	case "swap_high":
		return "Swap 内存高占用"
	case "fd_exhaustion":
		return "文件句柄耗尽"
	case "scrape_down":
		return "采集探针离线"
	case "prom_ingest_fail":
		return "数据摄取失败"
	default:
		return ""
	}
}

func extractDeviceDisplayName(labels map[string]string, rawText ...string) string {
	if labels != nil {
		if d := labels["device"]; d != "" {
			return d
		}
		if dn := labels["device_name"]; dn != "" {
			// 如果是形如 k8s:k3s-edge-51:lavm-8d0eljpims，提取最后有意义的主机名部分
			parts := strings.Split(dn, ":")
			shortName := parts[len(parts)-1]
			if id := labels["device_id"]; id != "" {
				return fmt.Sprintf("%s (ID: %s)", shortName, id)
			}
			return shortName
		}
		host := labels["device_hostname"]
		ip := labels["device_ip"]
		if host != "" && ip != "" {
			return fmt.Sprintf("%s (%s)", host, ip)
		} else if host != "" {
			return host
		} else if ip != "" {
			return ip
		} else if id := labels["device_id"]; id != "" {
			return "#" + id
		}
	}

	// 从原始文本中正则匹配 device_name 或 device_id
	for _, text := range rawText {
		if text == "" {
			continue
		}
		if idx := strings.Index(text, "device_name="); idx != -1 {
			sub := text[idx+12:]
			end := strings.IndexAny(sub, ",) \n\t")
			if end != -1 {
				sub = sub[:end]
			}
			sub = strings.TrimSpace(sub)
			if sub != "" {
				parts := strings.Split(sub, ":")
				shortName := parts[len(parts)-1]
				// 查是否有 device_id
				if idIdx := strings.Index(text, "device_id="); idIdx != -1 {
					idSub := text[idIdx+10:]
					idEnd := strings.IndexAny(idSub, ",) \n\t")
					if idEnd != -1 {
						idSub = idSub[:idEnd]
					}
					idSub = strings.TrimSpace(idSub)
					if idSub != "" {
						return fmt.Sprintf("%s (ID: %s)", shortName, idSub)
					}
				}
				return shortName
			}
		}
		if idx := strings.Index(text, "device_id="); idx != -1 {
			sub := text[idx+10:]
			end := strings.IndexAny(sub, ",) \n\t")
			if end != -1 {
				sub = sub[:end]
			}
			sub = strings.TrimSpace(sub)
			if sub != "" {
				return "#" + sub
			}
		}
	}
	return ""
}

func isRawMachineExpr(s string) bool {
	return strings.Contains(s, "⇒") ||
		strings.Contains(s, "device_last_seen_seconds_ago") ||
		strings.Contains(s, "node_cpu_seconds_total") ||
		strings.Contains(s, "node_memory_") ||
		strings.Contains(s, "node_filesystem_") ||
		strings.Contains(s, "node_load1") ||
		strings.Contains(s, "up == 0") ||
		(strings.Contains(s, "value=") && strings.Contains(s, ":"))
}

func cleanAlertSubject(rawSubject, ruleName, ruleKey, device string) string {
	s := strings.TrimSpace(rawSubject)

	name := strings.TrimSpace(ruleName)
	if name == "" {
		name = ruleNameCN(ruleKey)
	}
	if name == "" {
		name = ruleKey
	}

	if s == "" || isRawMachineExpr(s) {
		if device != "" {
			// 去掉 device 里的 ID: 部分做简短标题
			devTitle := device
			if parenIdx := strings.Index(devTitle, " ("); parenIdx != -1 {
				devTitle = devTitle[:parenIdx]
			}
			return fmt.Sprintf("%s - %s", name, devTitle)
		}
		return name
	}

	// 如果前面有 [device=...] 标签，规范化展示
	if strings.HasPrefix(s, "[device=") {
		endIdx := strings.Index(s, "] ")
		if endIdx != -1 {
			devTag := s[8:endIdx]
			content := s[endIdx+2:]
			if isRawMachineExpr(content) {
				return fmt.Sprintf("%s - %s", name, devTag)
			}
		}
	}

	return s
}

func cleanAlertBody(rawBody, ruleKey, ruleName, device string) string {
	raw := strings.TrimSpace(rawBody)
	if raw == "" {
		return ""
	}

	if !isRawMachineExpr(raw) {
		return raw
	}

	// 提取 value (value=xxx)
	valStr := ""
	if idx := strings.Index(raw, "value="); idx != -1 {
		valPart := raw[idx+6:]
		if endIdx := strings.Index(valPart, ")"); endIdx != -1 {
			valStr = strings.TrimSpace(valPart[:endIdx])
		} else {
			valStr = strings.TrimSpace(valPart)
		}
	}
	var valFloat float64
	if valStr != "" {
		valFloat, _ = strconv.ParseFloat(valStr, 64)
	}

	key := strings.ToLower(ruleKey)
	switch {
	case key == "device_offline" || key == "edge_offline" || strings.Contains(raw, "device_last_seen_seconds_ago"):
		if valFloat > 0 {
			return fmt.Sprintf("设备已超过 90 秒未上报心跳数据，当前已累计离线约 %.0f 秒。建议检查设备网络连通性及 OpsPilot Agent 运行状态。", valFloat)
		}
		return "设备已超过 90 秒未上报心跳数据，处于离线失联状态。请排查设备运行状态。"

	case key == "cpu_high" || key == "cpu_high_default" || strings.Contains(raw, "node_cpu_seconds_total"):
		if valFloat > 0 {
			return fmt.Sprintf("主机 CPU 使用率达到 %.1f%%，已超过预警阈值。请检查系统高占用进程。", valFloat)
		}
		return "主机 CPU 使用率持续偏高，已超出预设阈值。请检查系统负载。"

	case key == "mem_high" || strings.Contains(raw, "node_memory_MemAvailable_bytes"):
		if valFloat > 0 {
			return fmt.Sprintf("主机内存占用率达到 %.1f%%，已超过预警阈值。请排查高内存消耗应用。", valFloat)
		}
		return "主机物理内存占用率过高，剩余可用内存不足。"

	case key == "disk_high" || strings.Contains(raw, "node_filesystem_avail_bytes"):
		if valFloat > 0 {
			return fmt.Sprintf("主机磁盘分区使用率达到 %.1f%%，存储空间严重不足。请及时清理磁盘空间。", valFloat)
		}
		return "主机磁盘存储空间不足，已超出告警阈值。"

	case key == "load1_high" || strings.Contains(raw, "node_load1"):
		if valFloat > 0 {
			return fmt.Sprintf("主机 1 分钟平均系统负载达到 %.2f，CPU 与 I/O 资源压力较大。", valFloat)
		}
		return "主机系统平均负载过高，已超出预设阈值。"

	case key == "swap_high":
		if valFloat > 0 {
			return fmt.Sprintf("主机 Swap 交换分区使用率达到 %.1f%%，物理内存可能已出现严重紧缺。", valFloat)
		}
		return "主机 Swap 分区占用过高，请注意内存瓶颈。"

	case key == "scrape_down" || strings.Contains(raw, "up == 0"):
		return "监控采集探针无响应，目标实例可能已下线或端口不可达。"

	default:
		if valStr != "" {
			return fmt.Sprintf("监控指标异常触发告警阈值规则（当前检测值: %s）。", valStr)
		}
		return "监控指标偏离正常范围，触发告警阈值条件。"
	}
}

func formatDedupeKey(key string) string {
	if key == "" {
		return ""
	}
	// 如果包含多余的指标标签，例如 pipeline:device_offline:device_id=12,device_name=...
	// 精简为关键定位标识
	if idx := strings.Index(key, ",device_name="); idx != -1 {
		key = key[:idx]
	}
	if idx := strings.Index(key, ",instance="); idx != -1 {
		key = key[:idx]
	}
	if idx := strings.Index(key, ",job="); idx != -1 {
		key = key[:idx]
	}
	return key
}

func formatText(msg Message) string {
	badge := formatSeverityBadge(msg.Severity)

	var ruleKey, ruleName string
	if msg.Labels != nil {
		ruleKey = msg.Labels["rule"]
		ruleName = msg.Labels["rule_name"]
	}
	if ruleName == "" {
		ruleName = ruleNameCN(ruleKey)
	}

	device := extractDeviceDisplayName(msg.Labels, msg.Subject, msg.Body)
	title := cleanAlertSubject(msg.Subject, ruleName, ruleKey, device)
	body := cleanAlertBody(msg.Body, ruleKey, ruleName, device)

	var lines []string
	// 标题行
	lines = append(lines, fmt.Sprintf("%s %s", badge, title))
	lines = append(lines, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 核心元数据列表
	if ruleName != "" {
		lines = append(lines, "• 告警规则: "+ruleName)
	} else if ruleKey != "" {
		lines = append(lines, "• 告警规则: "+ruleKey)
	}

	if sevName := formatSeverityName(msg.Severity); sevName != "" {
		lines = append(lines, "• 告警等级: "+sevName)
	}

	if device != "" {
		lines = append(lines, "• 关联设备: "+device)
	}

	if msg.Labels != nil {
		if incidentID := msg.Labels["incident_id"]; incidentID != "" {
			lines = append(lines, "• 告警编号: #"+incidentID)
		}
		if svc := msg.Labels["service"]; svc != "" {
			lines = append(lines, "• 关联服务: "+svc)
		}
	}

	if src := formatSourceCN(msg.Source); src != "" {
		lines = append(lines, "• 告警来源: "+src)
	}

	if !msg.OccurredAt.IsZero() {
		lines = append(lines, "• 触发时间: "+msg.OccurredAt.Local().Format("2006-01-02 15:04:05"))
	}

	lines = append(lines, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 详情内容
	if body != "" && body != title {
		lines = append(lines, "告警详情:")
		lines = append(lines, body)
		lines = append(lines, "")
	}

	// 追踪标识
	if msg.DedupeKey != "" {
		lines = append(lines, "• 去重标识: "+formatDedupeKey(msg.DedupeKey))
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n")
}

func signGenericWebhook(endpoint string, secret string, body []byte) (string, map[string]string, error) {
	headers := map[string]string{}
	if secret == "" {
		return endpoint, headers, nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(body); err != nil {
		return "", nil, err
	}
	headers["X-OpsPilot-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return endpoint, headers, nil
}

func signFeishu(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func signDingTalkURL(endpoint, secret string, _ []byte) (string, map[string]string, error) {
	if secret == "" {
		return endpoint, nil, nil
	}
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	stringToSign := ts + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", nil, err
	}
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", nil, err
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil, nil
}
