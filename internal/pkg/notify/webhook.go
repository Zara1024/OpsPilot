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

// NewFeishuSender posts an interactive card payload compatible with Feishu/Lark custom bots.
func NewFeishuSender(name, endpoint, secret string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, secret, client, func(msg Message) (any, error) {
		payload := map[string]any{
			"msg_type": "interactive",
			"card":     formatFeishuCard(msg),
		}
		if secret != "" {
			ts := fmt.Sprintf("%d", time.Now().Unix())
			payload["timestamp"] = ts
			payload["sign"] = signFeishu(ts, secret)
		}
		return payload, nil
	}, nil)
}

// formatFeishuCard renders a Message as a Feishu interactive message card (Card 2.0).
func formatFeishuCard(msg Message) map[string]any {
	colorTemplate := "grey"
	prefix := "🔔【系统通知】"

	statusLower := strings.ToLower(strings.TrimSpace(msg.Labels["status"]))
	isResolved := statusLower == "resolved"
	isActive := statusLower == "" || statusLower == "open" || statusLower == "firing"

	if isResolved {
		colorTemplate = "green"
		prefix = "✅【告警恢复】"
	} else {
		switch msg.Severity {
		case SeverityCritical:
			colorTemplate = "red"
			prefix = "🚨【严重告警】"
		case SeverityWarning:
			colorTemplate = "orange"
			prefix = "⚠️【告警提醒】"
		case SeverityInfo:
			colorTemplate = "blue"
			prefix = "ℹ️【信息提醒】"
		default:
			colorTemplate = "turquoise"
			prefix = "🔔【告警通知】"
		}
	}

	ruleVal := msg.Labels["rule_name"]
	if ruleVal == "" {
		ruleVal = msg.Labels["rule"]
	}
	if ruleVal == "" {
		ruleVal = nonEmpty(msg.Subject, "系统告警")
	}

	devVal := msg.Labels["device_name"]
	if devVal == "" {
		devVal = msg.Labels["device"]
	}

	fullTitle := prefix + ruleVal
	if devVal != "" {
		fullTitle += " - " + devVal
	}

	var fields []map[string]any
	addField := func(k, v string) {
		if v == "" {
			return
		}
		fields = append(fields, map[string]any{
			"is_short": true,
			"text": map[string]any{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**%s**\n%s", k, v),
			},
		})
	}

	// 1. 告警级别
	if isResolved {
		addField("告警级别", "<font color='green'>RESOLVED (已恢复)</font>")
	} else {
		switch msg.Severity {
		case SeverityCritical:
			addField("告警级别", "<font color='red'>CRITICAL (严重)</font>")
		case SeverityWarning:
			addField("告警级别", "<font color='orange'>WARNING (警告)</font>")
		case SeverityInfo:
			addField("告警级别", "<font color='blue'>INFO (信息)</font>")
		default:
			addField("告警级别", strings.ToUpper(string(msg.Severity)))
		}
	}

	// 2. 告警状态
	if isResolved {
		addField("告警状态", "<font color='green'>RESOLVED (已恢复)</font>")
	} else if isActive {
		addField("告警状态", "<font color='red'>OPEN (发生中)</font>")
	} else if statusLower != "" {
		switch statusLower {
		case "acknowledged", "ack":
			addField("告警状态", "<font color='orange'>ACKNOWLEDGED (已认领)</font>")
		case "silenced":
			addField("告警状态", "<font color='grey'>SILENCED (已静音)</font>")
		default:
			addField("告警状态", strings.ToUpper(statusLower))
		}
	}

	// 事件 ID
	if iid, ok := msg.Labels["incident_id"]; ok && iid != "" {
		addField("事件 ID", "#"+iid)
	}

	// 3. 告警对象
	if dn, ok := msg.Labels["device_name"]; ok && dn != "" {
		if did, ok2 := msg.Labels["device_id"]; ok2 && did != "" {
			addField("告警对象", fmt.Sprintf("%s (#%s)", dn, did))
		} else {
			addField("告警对象", dn)
		}
	} else if did, ok := msg.Labels["device_id"]; ok && did != "" {
		addField("告警对象", "#"+did)
	}

	// 4. 告警规则
	if r, ok := msg.Labels["rule_name"]; ok && r != "" {
		addField("告警规则", r)
	} else if r, ok := msg.Labels["rule"]; ok && r != "" {
		addField("告警规则", r)
	}

	// 5. 发生时间（转换为中国标准时间 CST UTC+8）
	if !msg.OccurredAt.IsZero() {
		cst := time.FixedZone("CST", 8*3600)
		addField("发生时间", msg.OccurredAt.In(cst).Format("2006-01-02 15:04:05"))
	}

	// 6. 告警来源
	if msg.Source != "" {
		addField("告警来源", msg.Source)
	}

	elements := []any{}
	if len(fields) > 0 {
		elements = append(elements, map[string]any{
			"tag":    "div",
			"fields": fields,
		})
	}

	// 详细内容
	content := msg.Body
	if content == "" {
		content = msg.Subject
	}
	if content != "" {
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**详细内容**\n%s", content),
			},
		})
	}

	// 操作按钮
	baseURL := strings.TrimRight(strings.TrimSpace(msg.Labels["console_url"]), "/")
	var actions []any

	if msg.Labels != nil {
		for _, qa := range []struct {
			key   string
			label string
			kind  string
			act   string
		}{
			{"oncall_ack_url", "✅ 认领告警", "primary", "ack"},
			{"oncall_silence_url", "🔇 静音 30m", "default", "silence"},
			{"oncall_escalate_url", "⚡ 升级二线", "danger", "escalate"},
		} {
			if u := strings.TrimSpace(msg.Labels[qa.key]); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				actions = append(actions, map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": qa.label},
					"type": qa.kind,
					"url":  u,
					"value": map[string]string{
						"opspilot_action": qa.act,
						"incident_id":     msg.Labels["incident_id"],
					},
				})
			}
		}
	}

	if baseURL != "" {
		if did, ok := msg.Labels["device_id"]; ok && did != "" {
			actions = append(actions, map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "🖥️ 查看设备"},
				"type": "default",
				"url":  fmt.Sprintf("%s/devices/%s", baseURL, did),
			})
		}
		if iid, ok := msg.Labels["incident_id"]; ok && iid != "" {
			actions = append(actions, map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "📋 告警事件"},
				"type": "default",
				"url":  fmt.Sprintf("%s/alerts/incidents/%s", baseURL, iid),
			})
		} else {
			actions = append(actions, map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "📋 告警事件"},
				"type": "default",
				"url":  fmt.Sprintf("%s/alerts", baseURL),
			})
		}
	}

	if rb, ok := msg.Labels["runbook_url"]; ok && rb != "" {
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": "📖 处理预案 / Runbook"},
			"type": "primary",
			"url":  rb,
		})
	}

	if len(actions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "action",
			"actions": actions,
		})
	}

	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": colorTemplate,
			"title": map[string]any{
				"tag":     "plain_text",
				"content": fullTitle,
			},
		},
		"elements": elements,
	}
}

// NewDingTalkSender posts an ActionCard or Markdown payload compatible with DingTalk custom bots.
func NewDingTalkSender(name, endpoint, secret string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, secret, client, func(msg Message) (any, error) {
		return formatDingTalkMessage(msg), nil
	}, signDingTalkURL)
}

// formatDingTalkMessage renders a Message as a DingTalk ActionCard or Markdown message.
func formatDingTalkMessage(msg Message) map[string]any {
	prefix := "🔔【系统通知】"

	statusLower := strings.ToLower(strings.TrimSpace(msg.Labels["status"]))
	isResolved := statusLower == "resolved"
	isActive := statusLower == "" || statusLower == "open" || statusLower == "firing"

	var sevColor, statusColor, sevText, statusText string

	if isResolved {
		prefix = "✅【告警恢复】"
		sevColor = "#00B42A"
		sevText = "RESOLVED (已恢复)"
		statusColor = "#00B42A"
		statusText = "RESOLVED (已恢复)"
	} else {
		switch msg.Severity {
		case SeverityCritical:
			prefix = "🚨【严重告警】"
			sevColor = "#EA3323"
			sevText = "CRITICAL (严重)"
		case SeverityWarning:
			prefix = "⚠️【告警提醒】"
			sevColor = "#FA7C17"
			sevText = "WARNING (警告)"
		case SeverityInfo:
			prefix = "ℹ️【信息提醒】"
			sevColor = "#1664FF"
			sevText = "INFO (信息)"
		default:
			prefix = "🔔【告警通知】"
			sevColor = "#888888"
			if msg.Severity != "" {
				sevText = strings.ToUpper(string(msg.Severity))
			} else {
				sevText = "NOTICE (通知)"
			}
		}

		if isActive {
			statusColor = "#EA3323"
			statusText = "OPEN (发生中)"
		} else if statusLower != "" {
			statusColor = "#888888"
			statusText = strings.ToUpper(statusLower)
		}
	}

	ruleVal := msg.Labels["rule_name"]
	if ruleVal == "" {
		ruleVal = msg.Labels["rule"]
	}
	if ruleVal == "" {
		ruleVal = nonEmpty(msg.Subject, "系统告警")
	}

	devVal := msg.Labels["device_name"]
	if devVal == "" {
		devVal = msg.Labels["device"]
	}

	fullTitle := prefix + ruleVal
	if devVal != "" {
		fullTitle += " - " + devVal
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### %s\n\n", fullTitle))

	if sevText != "" {
		sb.WriteString(fmt.Sprintf("- **告警级别**：<font color=\"%s\">%s</font>\n", sevColor, sevText))
	}
	if statusText != "" {
		sb.WriteString(fmt.Sprintf("- **告警状态**：<font color=\"%s\">%s</font>\n", statusColor, statusText))
	}

	if iid, ok := msg.Labels["incident_id"]; ok && iid != "" {
		sb.WriteString(fmt.Sprintf("- **事件 ID**：#%s\n", iid))
	}

	// 告警对象
	if dn, ok := msg.Labels["device_name"]; ok && dn != "" {
		if did, ok2 := msg.Labels["device_id"]; ok2 && did != "" {
			sb.WriteString(fmt.Sprintf("- **告警对象**：%s (#%s)\n", dn, did))
		} else {
			sb.WriteString(fmt.Sprintf("- **告警对象**：%s\n", dn))
		}
	} else if did, ok := msg.Labels["device_id"]; ok && did != "" {
		sb.WriteString(fmt.Sprintf("- **告警对象**：#%s\n", did))
	}

	// 告警规则
	if r, ok := msg.Labels["rule_name"]; ok && r != "" {
		sb.WriteString(fmt.Sprintf("- **告警规则**：%s\n", r))
	} else if r, ok := msg.Labels["rule"]; ok && r != "" {
		sb.WriteString(fmt.Sprintf("- **告警规则**：%s\n", r))
	}

	// 发生时间（转换为中国标准时间 CST UTC+8）
	if !msg.OccurredAt.IsZero() {
		cst := time.FixedZone("CST", 8*3600)
		sb.WriteString(fmt.Sprintf("- **发生时间**：%s\n", msg.OccurredAt.In(cst).Format("2006-01-02 15:04:05")))
	}

	// 告警来源
	if msg.Source != "" {
		sb.WriteString(fmt.Sprintf("- **告警来源**：%s\n", msg.Source))
	}

	// 详细内容
	content := msg.Body
	if content == "" {
		content = msg.Subject
	}
	if content != "" {
		sb.WriteString(fmt.Sprintf("\n> %s\n", content))
	}

	baseURL := strings.TrimRight(strings.TrimSpace(msg.Labels["console_url"]), "/")
	var btns []map[string]string

	if msg.Labels != nil {
		for _, qa := range []struct {
			key   string
			title string
		}{
			{"oncall_ack_url", "✅ 认领告警"},
			{"oncall_silence_url", "🔇 静音 30m"},
			{"oncall_escalate_url", "⚡ 升级二线"},
		} {
			if u := strings.TrimSpace(msg.Labels[qa.key]); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				btns = append(btns, map[string]string{
					"title":     qa.title,
					"actionURL": u,
				})
			}
		}
	}

	if baseURL != "" {
		if did, ok := msg.Labels["device_id"]; ok && did != "" {
			btns = append(btns, map[string]string{
				"title":     "🖥️ 查看设备",
				"actionURL": fmt.Sprintf("%s/devices/%s", baseURL, did),
			})
		}
		if iid, ok := msg.Labels["incident_id"]; ok && iid != "" {
			btns = append(btns, map[string]string{
				"title":     "📋 告警事件",
				"actionURL": fmt.Sprintf("%s/alerts/incidents/%s", baseURL, iid),
			})
		} else {
			btns = append(btns, map[string]string{
				"title":     "📋 告警事件",
				"actionURL": fmt.Sprintf("%s/alerts", baseURL),
			})
		}
	}

	if rb, ok := msg.Labels["runbook_url"]; ok && rb != "" {
		btns = append(btns, map[string]string{
			"title":     "📖 处理预案 / Runbook",
			"actionURL": rb,
		})
	}

	if len(btns) > 0 {
		return map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          fullTitle,
				"text":           sb.String(),
				"btnOrientation": "1",
				"btns":           btns,
			},
		}
	}

	return map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": fullTitle,
			"text":  sb.String(),
		},
	}
}

// NewWeComSender posts an interactive template_card (when valid action/console URLs exist)
// or rich markdown message compatible with 企业微信 (WeCom) group bots.
func NewWeComSender(name, endpoint string, client *http.Client) Sender {
	return newWebhookSender(name, endpoint, "", client, func(msg Message) (any, error) {
		return formatWeComMessage(msg), nil
	}, nil)
}

func formatWeComMessage(msg Message) map[string]any {
	statusLower := strings.ToLower(strings.TrimSpace(msg.Labels["status"]))
	isResolved := statusLower == "resolved"
	isActive := statusLower == "" || statusLower == "open" || statusLower == "firing"

	prefix := "🔔【系统通知】"
	sevText := "NOTICE"
	mdSevColor := "comment"
	if isResolved {
		prefix = "✅【告警恢复】"
		sevText = "RESOLVED (已恢复)"
		mdSevColor = "info"
	} else {
		switch msg.Severity {
		case SeverityCritical:
			prefix = "🚨【严重告警】"
			sevText = "CRITICAL (严重)"
			mdSevColor = "warning"
		case SeverityWarning:
			prefix = "⚠️【告警提醒】"
			sevText = "WARNING (警告)"
			mdSevColor = "warning"
		case SeverityInfo:
			prefix = "ℹ️【信息提醒】"
			sevText = "INFO (信息)"
			mdSevColor = "comment"
		default:
			if msg.Severity != "" {
				sevText = strings.ToUpper(string(msg.Severity))
			}
		}
	}

	ruleVal := firstNonEmptyLabel(msg.Labels, "rule_name", "rule")
	if ruleVal == "" {
		ruleVal = nonEmpty(msg.Subject, "系统告警")
	}
	devVal := firstNonEmptyLabel(msg.Labels, "device_name", "device", "target")
	fullTitle := prefix + ruleVal
	if devVal != "" {
		fullTitle += " - " + devVal
	}

	statusText := strings.ToUpper(statusLower)
	if isResolved {
		statusText = "RESOLVED (已恢复)"
	} else if isActive {
		statusText = "OPEN (发生中)"
	}

	subTitle := strings.TrimSpace(msg.Body)
	if subTitle == "" {
		subTitle = strings.TrimSpace(msg.Subject)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(msg.Labels["console_url"]), "/")
	type jumpItem struct {
		Title string
		URL   string
	}
	var jumps []jumpItem
	if msg.Labels != nil {
		for _, qa := range []struct {
			key   string
			label string
		}{
			{"oncall_ack_url", "✅ 认领告警"},
			{"oncall_silence_url", "🔇 静音 30m"},
			{"oncall_escalate_url", "⚡ 升级二线"},
		} {
			if u := strings.TrimSpace(msg.Labels[qa.key]); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				jumps = append(jumps, jumpItem{Title: qa.label, URL: u})
			}
		}
	}
	var detailURL string
	if baseURL != "" {
		if iid := strings.TrimSpace(msg.Labels["incident_id"]); iid != "" {
			detailURL = fmt.Sprintf("%s/alerts/incidents/%s", baseURL, iid)
		} else {
			detailURL = fmt.Sprintf("%s/alerts", baseURL)
		}
		if len(jumps) < 3 {
			jumps = append(jumps, jumpItem{Title: "📋 告警事件", URL: detailURL})
		}
	}
	if rb := strings.TrimSpace(msg.Labels["runbook_url"]); (strings.HasPrefix(rb, "http://") || strings.HasPrefix(rb, "https://")) && len(jumps) < 3 {
		jumps = append(jumps, jumpItem{Title: "📖 处理预案", URL: rb})
	}

	// Use WeCom template_card (text_notice) whenever at least one valid URL exists for card_action.
	if len(jumps) > 0 || detailURL != "" {
		primaryURL := detailURL
		if primaryURL == "" {
			primaryURL = jumps[0].URL
		}
		if len(jumps) > 3 {
			jumps = jumps[:3]
		}
		horiz := []map[string]any{
			{"keyname": "告警级别", "value": sevText},
		}
		if statusText != "" {
			horiz = append(horiz, map[string]any{"keyname": "状态", "value": statusText})
		}
		if iid := strings.TrimSpace(msg.Labels["incident_id"]); iid != "" {
			horiz = append(horiz, map[string]any{"keyname": "事件编号", "value": "#" + iid})
		}
		if devVal != "" {
			horiz = append(horiz, map[string]any{"keyname": "告警对象", "value": devVal})
		}
		if assignee := strings.TrimSpace(msg.Labels["assignee"]); assignee != "" {
			horiz = append(horiz, map[string]any{"keyname": "值班人", "value": assignee})
		}
		if !msg.OccurredAt.IsZero() && len(horiz) < 6 {
			cst := time.FixedZone("CST", 8*3600)
			horiz = append(horiz, map[string]any{"keyname": "时间", "value": msg.OccurredAt.In(cst).Format("01-02 15:04:05")})
		}

		jumpList := make([]map[string]any, 0, len(jumps))
		for _, j := range jumps {
			jumpList = append(jumpList, map[string]any{
				"type":  1,
				"title": j.Title,
				"url":   j.URL,
			})
		}

		return map[string]any{
			"msgtype": "template_card",
			"template_card": map[string]any{
				"card_type": "text_notice",
				"source": map[string]any{
					"desc":       "OpsPilot On-Call",
					"desc_color": 0,
				},
				"main_title": map[string]any{
					"title": fullTitle,
					"desc":  ruleVal,
				},
				"sub_title_text":          subTitle,
				"horizontal_content_list": horiz,
				"jump_list":               jumpList,
				"card_action": map[string]any{
					"type": 1,
					"url":  primaryURL,
				},
			},
		}
	}

	// Rich markdown fallback when no external URLs are present.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### %s\n", fullTitle))
	sb.WriteString(fmt.Sprintf("> 级别：<font color=\"%s\">%s</font>\n", mdSevColor, sevText))
	if statusText != "" {
		sb.WriteString(fmt.Sprintf("> 状态：%s\n", statusText))
	}
	if iid := strings.TrimSpace(msg.Labels["incident_id"]); iid != "" {
		sb.WriteString(fmt.Sprintf("> 事件编号：#%s\n", iid))
	}
	if devVal != "" {
		sb.WriteString(fmt.Sprintf("> 告警对象：%s\n", devVal))
	}
	if !msg.OccurredAt.IsZero() {
		cst := time.FixedZone("CST", 8*3600)
		sb.WriteString(fmt.Sprintf("> 发生时间：%s\n", msg.OccurredAt.In(cst).Format("2006-01-02 15:04:05")))
	}
	if subTitle != "" {
		sb.WriteString("\n" + subTitle)
	}
	return map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"content": sb.String(),
		},
	}
}

func firstNonEmptyLabel(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if labels == nil {
			continue
		}
		if v := strings.TrimSpace(labels[k]); v != "" {
			return v
		}
	}
	return ""
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
	}
	return ""
}

func cleanAlertSubject(subject, ruleName, ruleKey, device string) string {
	subject = strings.TrimSpace(subject)
	if !strings.Contains(subject, " ⇒ ") && !strings.Contains(subject, "value=") && !strings.Contains(subject, "rate(") && !strings.Contains(subject, "avg by") {
		if subject != "" {
			return subject
		}
	}
	displayName := ruleName
	if displayName == "" {
		displayName = ruleNameCN(ruleKey)
	}
	if displayName == "" {
		displayName = ruleKey
	}
	if displayName == "" {
		displayName = "监控指标异常"
	}
	if device != "" {
		return fmt.Sprintf("%s - %s", displayName, device)
	}
	return displayName
}

func cleanAlertBody(raw, ruleKey, ruleName, device string) string {
	raw = strings.TrimSpace(raw)
	valStr := ""
	if idx := strings.Index(raw, "value="); idx != -1 {
		valStr = strings.TrimRight(raw[idx+6:], " \t\r\n)")
	}
	valFloat, _ := strconv.ParseFloat(valStr, 64)

	key := strings.ToLower(ruleKey)
	switch {
	case key == "device_offline" || key == "edge_offline":
		if device != "" {
			return fmt.Sprintf("节点设备 %s 已超过 90 秒未上报心跳探针，判定为主机失联或 Agent 进程异常。请排查网络连通性与 Agent 服务状态。", device)
		}
		return "节点设备心跳上报超时，判定为主机失联。请排查设备运行状态。"

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
