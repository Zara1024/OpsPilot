package alert

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
)

// NormalizedAlertEvent is the canonical alert payload produced by all external protocol adapters.
type NormalizedAlertEvent struct {
	RawDedupeKey string
	Status       string // "firing" | "resolved"
	RuleKey      string
	RuleName     string
	Severity     string // "critical" | "warning" | "info" | "" (falls back to source default)
	Title        string
	Summary      string
	Description  string
	Target       string
	Value        *float64
	Threshold    *float64
	Labels       map[string]string
	Annotations  map[string]string
	RunbookURL   string
	OccurredAt   time.Time
}

// AdapterResult wraps normalized events or control-plane messages (such as Huawei SMN subscription confirmation).
type AdapterResult struct {
	Events       []NormalizedAlertEvent
	InfoMessage  string
	SubscribeURL string
}

// IngestAdapter translates an upstream webhook payload into normalized OpsPilot alert events.
type IngestAdapter interface {
	Provider() string
	Parse(contentType string, headers http.Header, body []byte) (*AdapterResult, error)
	SamplePayload(action string) (contentType string, body []byte)
}

// AdapterRegistry resolves external protocol adapters by provider key.
type AdapterRegistry struct {
	adapters map[string]IngestAdapter
}

// NewDefaultAdapterRegistry builds the immutable default registry of all supported external monitoring protocols.
func NewDefaultAdapterRegistry() *AdapterRegistry {
	list := []IngestAdapter{
		alertmanagerAdapter{},
		grafanaAdapter{},
		aliyunCMSAdapter{},
		tencentCMAdapter{},
		huaweiCESAdapter{},
		zabbixAdapter{},
		genericWebhookAdapter{},
	}
	m := make(map[string]IngestAdapter, len(list))
	for _, a := range list {
		m[a.Provider()] = a
	}
	return &AdapterRegistry{adapters: m}
}

// Supports reports whether provider has a registered adapter.
func (r *AdapterRegistry) Supports(provider string) bool {
	if r == nil {
		return false
	}
	_, ok := r.adapters[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}

// Parse dispatches payload parsing to the provider's adapter.
func (r *AdapterRegistry) Parse(provider, contentType string, headers http.Header, body []byte) (*AdapterResult, error) {
	if r == nil {
		return nil, fmt.Errorf("adapter registry is nil")
	}
	key := strings.ToLower(strings.TrimSpace(provider))
	adapter, ok := r.adapters[key]
	if !ok {
		return nil, fmt.Errorf("unsupported ingest provider %q", provider)
	}
	return adapter.Parse(contentType, headers, body)
}

// SamplePayload returns a realistic test payload ("fire" or "resolve") for provider.
func (r *AdapterRegistry) SamplePayload(provider, action string) (string, []byte, bool) {
	if r == nil {
		return "", nil, false
	}
	adapter, ok := r.adapters[strings.ToLower(strings.TrimSpace(provider))]
	if !ok {
		return "", nil, false
	}
	ct, body := adapter.SamplePayload(action)
	return ct, body, true
}

// ---------------------------------------------------------------------------
// 1. Prometheus Alertmanager
// ---------------------------------------------------------------------------

type alertmanagerAdapter struct{}

func (alertmanagerAdapter) Provider() string { return model.IngestProviderAlertmanager }

func (alertmanagerAdapter) Parse(_ string, _ http.Header, body []byte) (*AdapterResult, error) {
	var payload struct {
		Status            string            `json:"status"`
		GroupKey          string            `json:"groupKey"`
		GroupLabels       map[string]string `json:"groupLabels"`
		CommonLabels      map[string]string `json:"commonLabels"`
		CommonAnnotations map[string]string `json:"commonAnnotations"`
		ExternalURL       string            `json:"externalURL"`
		Alerts            []struct {
			Status       string            `json:"status"`
			Labels       map[string]string `json:"labels"`
			Annotations  map[string]string `json:"annotations"`
			StartsAt     string            `json:"startsAt"`
			EndsAt       string            `json:"endsAt"`
			GeneratorURL string            `json:"generatorURL"`
			Fingerprint  string            `json:"fingerprint"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode alertmanager JSON: %w", err)
	}
	if len(payload.Alerts) == 0 {
		return nil, fmt.Errorf("alertmanager payload contains no alerts")
	}

	events := make([]NormalizedAlertEvent, 0, len(payload.Alerts))
	for _, a := range payload.Alerts {
		labels := cloneAndMergeMaps(payload.CommonLabels, payload.GroupLabels, a.Labels)
		annotations := cloneAndMergeMaps(payload.CommonAnnotations, a.Annotations)

		status := normalizeEventStatus(firstPresent(a.Status, payload.Status))
		ruleName := firstPresent(labels["alertname"], labels["rule"], "alertmanager_alert")
		target := extractTargetFromLabels(labels)
		title := firstPresent(annotations["summary"], annotations["title"], formatRuleAndTarget(ruleName, target))
		desc := firstPresent(annotations["description"], annotations["message"])
		runbook := firstPresent(annotations["runbook_url"], a.GeneratorURL, payload.ExternalURL)

		var occ time.Time
		if status == "resolved" {
			occ = firstParsedTime(a.EndsAt, a.StartsAt)
		} else {
			occ = firstParsedTime(a.StartsAt)
		}

		events = append(events, NormalizedAlertEvent{
			RawDedupeKey: firstPresent(a.Fingerprint, payload.GroupKey),
			Status:       status,
			RuleKey:      sanitizeRuleKey(ruleName, "alertmanager_alert"),
			RuleName:     ruleName,
			Severity:     normalizeExternalSeverity(firstPresent(labels["severity"], labels["level"], labels["priority"])),
			Title:        title,
			Summary:      firstPresent(annotations["summary"], desc, title),
			Description:  desc,
			Target:       target,
			Labels:       labels,
			Annotations:  annotations,
			RunbookURL:   runbook,
			OccurredAt:   occ,
		})
	}
	return &AdapterResult{Events: events}, nil
}

func (alertmanagerAdapter) SamplePayload(action string) (string, []byte) {
	status := "firing"
	endsAt := "0001-01-01T00:00:00Z"
	if strings.EqualFold(action, "resolve") {
		status = "resolved"
		endsAt = time.Now().UTC().Format(time.RFC3339)
	}
	startsAt := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"status": status,
		"commonLabels": map[string]string{
			"alertname": "HighHTTP5xxRate",
			"service":   "checkout-api",
			"instance":  "10.10.1.22:8080",
			"severity":  "critical",
		},
		"alerts": []map[string]any{
			{
				"status":      status,
				"fingerprint": "am-demo-fp-001",
				"startsAt":    startsAt,
				"endsAt":      endsAt,
				"labels": map[string]string{
					"alertname": "HighHTTP5xxRate",
					"service":   "checkout-api",
					"instance":  "10.10.1.22:8080",
					"severity":  "critical",
				},
				"annotations": map[string]string{
					"summary":     "[演练] checkout-api HTTP 5xx 错误率超过 5%",
					"description": "Prometheus Alertmanager 模拟联调告警",
					"runbook_url": "https://runbook.example.com/http-5xx",
				},
			},
		},
	})
	return "application/json", body
}

// ---------------------------------------------------------------------------
// 2. Grafana Alerting (Unified Alerting + Legacy Webhook)
// ---------------------------------------------------------------------------

type grafanaAdapter struct{}

func (grafanaAdapter) Provider() string { return model.IngestProviderGrafana }

func (grafanaAdapter) Parse(_ string, _ http.Header, body []byte) (*AdapterResult, error) {
	var payload struct {
		Status            string            `json:"status"`
		State             string            `json:"state"`
		Title             string            `json:"title"`
		Message           string            `json:"message"`
		RuleID            any               `json:"ruleId"`
		RuleName          string            `json:"ruleName"`
		RuleURL           string            `json:"ruleUrl"`
		ExternalURL       string            `json:"externalURL"`
		CommonLabels      map[string]string `json:"commonLabels"`
		CommonAnnotations map[string]string `json:"commonAnnotations"`
		Tags              map[string]string `json:"tags"`
		EvalMatches       []struct {
			Metric string   `json:"metric"`
			Value  *float64 `json:"value"`
			Tags   map[string]string `json:"tags"`
		} `json:"evalMatches"`
		Alerts []struct {
			Status       string            `json:"status"`
			Labels       map[string]string `json:"labels"`
			Annotations  map[string]string `json:"annotations"`
			StartsAt     string            `json:"startsAt"`
			EndsAt       string            `json:"endsAt"`
			GeneratorURL string            `json:"generatorURL"`
			DashboardURL string            `json:"dashboardURL"`
			PanelURL     string            `json:"panelURL"`
			Fingerprint  string            `json:"fingerprint"`
			ValueString  string            `json:"valueString"`
			Values       map[string]any    `json:"values"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode grafana JSON: %w", err)
	}

	// Unified Alerting path
	if len(payload.Alerts) > 0 {
		events := make([]NormalizedAlertEvent, 0, len(payload.Alerts))
		for _, a := range payload.Alerts {
			labels := cloneAndMergeMaps(payload.CommonLabels, a.Labels)
			annotations := cloneAndMergeMaps(payload.CommonAnnotations, a.Annotations)
			status := normalizeEventStatus(firstPresent(a.Status, payload.Status, payload.State))
			ruleName := firstPresent(labels["alertname"], labels["rulename"], payload.RuleName, "grafana_alert")
			target := extractTargetFromLabels(labels)
			title := firstPresent(annotations["summary"], payload.Title, formatRuleAndTarget(ruleName, target))
			desc := firstPresent(annotations["description"], payload.Message, a.ValueString)
			runbook := firstPresent(annotations["runbook_url"], a.PanelURL, a.DashboardURL, a.GeneratorURL, payload.ExternalURL)

			var valPtr *float64
			for _, rawVal := range a.Values {
				if parsed := parseNumericAny(rawVal); parsed != nil {
					valPtr = parsed
					break
				}
			}

			events = append(events, NormalizedAlertEvent{
				RawDedupeKey: a.Fingerprint,
				Status:       status,
				RuleKey:      sanitizeRuleKey(ruleName, "grafana_alert"),
				RuleName:     ruleName,
				Severity:     normalizeExternalSeverity(firstPresent(labels["severity"], labels["level"])),
				Title:        title,
				Summary:      firstPresent(annotations["summary"], a.ValueString, desc, title),
				Description:  desc,
				Target:       target,
				Value:        valPtr,
				Labels:       labels,
				Annotations:  annotations,
				RunbookURL:   runbook,
				OccurredAt:   firstParsedTime(a.StartsAt),
			})
		}
		return &AdapterResult{Events: events}, nil
	}

	// Legacy Grafana v8 webhook fallback
	if payload.RuleName == "" && payload.Title == "" && payload.State == "" {
		return nil, fmt.Errorf("grafana payload missing alerts and rule fields")
	}
	labels := cloneAndMergeMaps(payload.Tags, payload.CommonLabels)
	var valPtr *float64
	if len(payload.EvalMatches) > 0 {
		valPtr = payload.EvalMatches[0].Value
		for k, v := range payload.EvalMatches[0].Tags {
			labels[k] = v
		}
		if payload.EvalMatches[0].Metric != "" && labels["metric"] == "" {
			labels["metric"] = payload.EvalMatches[0].Metric
		}
	}
	ruleName := firstPresent(payload.RuleName, payload.Title, "grafana_legacy_alert")
	status := normalizeEventStatus(firstPresent(payload.State, payload.Status))
	target := extractTargetFromLabels(labels)
	ruleID := strings.TrimSpace(fmt.Sprint(payload.RuleID))
	rawKey := ""
	if ruleID != "" && ruleID != "<nil>" {
		rawKey = "grafana-rule-" + ruleID
	}

	return &AdapterResult{
		Events: []NormalizedAlertEvent{{
			RawDedupeKey: rawKey,
			Status:       status,
			RuleKey:      sanitizeRuleKey(ruleName, "grafana_alert"),
			RuleName:     ruleName,
			Severity:     normalizeExternalSeverity(labels["severity"]),
			Title:        firstPresent(payload.Title, ruleName),
			Summary:      firstPresent(payload.Message, payload.Title, ruleName),
			Description:  payload.Message,
			Target:       target,
			Value:        valPtr,
			Labels:       labels,
			RunbookURL:   payload.RuleURL,
		}},
	}, nil
}

func (grafanaAdapter) SamplePayload(action string) (string, []byte) {
	status := "firing"
	if strings.EqualFold(action, "resolve") {
		status = "resolved"
	}
	body, _ := json.Marshal(map[string]any{
		"status": status,
		"title":  "[演练] Grafana JVM Heap 使用率过高",
		"alerts": []map[string]any{
			{
				"status":      status,
				"fingerprint": "gf-demo-fp-001",
				"startsAt":    time.Now().UTC().Format(time.RFC3339),
				"labels": map[string]string{
					"alertname": "JVMHeapUsageHigh",
					"service":   "order-svc",
					"severity":  "warning",
				},
				"annotations": map[string]string{
					"summary":     "[演练] order-svc JVM 老年代内存占用达到 89%",
					"description": "Grafana Unified Alerting 模拟联调告警",
				},
				"dashboardURL": "https://grafana.example.com/d/jvm-overview",
				"values":       map[string]any{"A": 89.4},
			},
		},
	})
	return "application/json", body
}

// ---------------------------------------------------------------------------
// 3. Aliyun Cloud Monitor (CMS) - Form URL-Encoded & JSON
// ---------------------------------------------------------------------------

type aliyunCMSAdapter struct{}

func (aliyunCMSAdapter) Provider() string { return model.IngestProviderAliyunCMS }

func (aliyunCMSAdapter) Parse(contentType string, _ http.Header, body []byte) (*AdapterResult, error) {
	fields := make(map[string]string)
	trimmed := strings.TrimSpace(string(body))
	isForm := strings.Contains(strings.ToLower(contentType), "application/x-www-form-urlencoded") ||
		(!strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "="))

	if isForm {
		values, err := url.ParseQuery(trimmed)
		if err != nil {
			return nil, fmt.Errorf("decode aliyun cms form: %w", err)
		}
		for k, v := range values {
			if len(v) > 0 {
				fields[k] = v[0]
			}
		}
	} else {
		var rawMap map[string]any
		if err := json.Unmarshal(body, &rawMap); err != nil {
			return nil, fmt.Errorf("decode aliyun cms JSON: %w", err)
		}
		// Unwrap EventBridge envelope if "data" map exists
		if dataMap, ok := rawMap["data"].(map[string]any); ok {
			for k, v := range dataMap {
				fields[k] = stringifyAny(v)
			}
		}
		for k, v := range rawMap {
			if k == "data" {
				continue
			}
			if _, exists := fields[k]; !exists {
				fields[k] = stringifyAny(v)
			}
		}
	}

	alertName := firstPresent(fields["alertName"], fields["ruleName"], fields["name"], fields["metricName"])
	if alertName == "" && fields["alertState"] == "" && fields["state"] == "" {
		return nil, fmt.Errorf("aliyun cms payload missing alertName and alertState")
	}
	if alertName == "" {
		alertName = "aliyun_cms_alert"
	}

	state := firstPresent(fields["alertState"], fields["state"], fields["status"], "ALERT")
	status := normalizeEventStatus(state)
	level := firstPresent(fields["curLevel"], fields["triggerLevel"], fields["level"], fields["severity"])
	instance := firstPresent(fields["instanceName"], fields["userId"])
	dimensions := strings.TrimSpace(fields["dimensions"])
	ruleID := strings.TrimSpace(fields["ruleId"])

	labels := map[string]string{}
	if ns := strings.TrimSpace(fields["namespace"]); ns != "" {
		labels["namespace"] = ns
	}
	if metric := strings.TrimSpace(fields["metricName"]); metric != "" {
		labels["metric"] = metric
	}
	if instance != "" {
		labels["instance"] = instance
	}
	if ruleID != "" {
		labels["rule_id"] = ruleID
	}
	if dimensions != "" {
		var dimMap map[string]any
		if strings.HasPrefix(dimensions, "{") && json.Unmarshal([]byte(dimensions), &dimMap) == nil {
			for k, v := range dimMap {
				if vs := stringifyAny(v); vs != "" {
					labels[k] = vs
				}
			}
		} else {
			labels["dimensions"] = dimensions
		}
	}

	target := firstPresent(instance, labels["instanceId"], dimensions)
	title := formatRuleAndTarget(alertName, target)
	expr := strings.TrimSpace(fields["expression"])
	curVal := strings.TrimSpace(fields["curValue"])
	summary := firstPresent(fields["message"], fmt.Sprintf("%s %s %s", alertName, expr, curVal))

	return &AdapterResult{
		Events: []NormalizedAlertEvent{{
			RawDedupeKey: joinNonEmpty(":", ruleID, alertName, firstPresent(dimensions, instance)),
			Status:       status,
			RuleKey:      sanitizeRuleKey(firstPresent(ruleID, alertName), "aliyun_cms"),
			RuleName:     alertName,
			Severity:     normalizeExternalSeverity(level),
			Title:        title,
			Summary:      strings.TrimSpace(summary),
			Description:  strings.TrimSpace(firstPresent(fields["expression"], fields["dimensions"])),
			Target:       target,
			Value:        parseNumericStr(curVal),
			Labels:       labels,
			OccurredAt:   parseAnyTimestamp(fields["timestamp"]),
		}},
	}, nil
}

func (aliyunCMSAdapter) SamplePayload(action string) (string, []byte) {
	state := "ALERT"
	curLevel := "CRITICAL"
	val := "96.2"
	if strings.EqualFold(action, "resolve") {
		state = "OK"
		curLevel = "INFO"
		val = "41.0"
	}
	form := url.Values{}
	form.Set("alertName", "[演练] 阿里云 ECS CPU 使用率过高")
	form.Set("alertState", state)
	form.Set("curLevel", curLevel)
	form.Set("ruleId", "cms-rule-demo-01")
	form.Set("namespace", "acs_ecs_dashboard")
	form.Set("metricName", "CPUUtilization")
	form.Set("instanceName", "ecs-prod-pay-01")
	form.Set("dimensions", `{"instanceId":"i-bp1demo001","userId":"10001"}`)
	form.Set("expression", "Average >= 90")
	form.Set("curValue", val)
	form.Set("timestamp", strconv.FormatInt(time.Now().UTC().UnixMilli(), 10))
	return "application/x-www-form-urlencoded", []byte(form.Encode())
}

// ---------------------------------------------------------------------------
// 4. Tencent Cloud Monitor (CM)
// ---------------------------------------------------------------------------

type tencentCMAdapter struct{}

func (tencentCMAdapter) Provider() string { return model.IngestProviderTencentCM }

func (tencentCMAdapter) Parse(_ string, _ http.Header, body []byte) (*AdapterResult, error) {
	var payload struct {
		SessionID       string `json:"sessionId"`
		AlarmStatus     string `json:"alarmStatus"`
		AlarmType       string `json:"alarmType"`
		DurationTime    int64  `json:"durationTime"`
		FirstOccurTime  string `json:"firstOccurTime"`
		RecoverTime     string `json:"recoverTime"`
		AlarmObjInfo struct {
			Region     string `json:"region"`
			Namespace  string `json:"namespace"`
			AppID      string `json:"appId"`
			Uin        string `json:"uin"`
			Dimensions any    `json:"dimensions"`
		} `json:"alarmObjInfo"`
		AlarmPolicyInfo struct {
			PolicyID         string `json:"policyId"`
			PolicyType       string `json:"policyType"`
			PolicyName       string `json:"policyName"`
			PolicyTypeCname  string `json:"policyTypeCname"`
			Conditions struct {
				MetricName     string `json:"metricName"`
				MetricShowName string `json:"metricShowName"`
				CalcType       string `json:"calcType"`
				CalcValue      string `json:"calcValue"`
				CurrentValue   string `json:"currentValue"`
				Unit           string `json:"unit"`
				CalcUnit       string `json:"calcUnit"`
				Period         string `json:"period"`
			} `json:"conditions"`
		} `json:"alarmPolicyInfo"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode tencent cm JSON: %w", err)
	}
	if payload.AlarmStatus == "" && payload.AlarmPolicyInfo.PolicyName == "" && payload.SessionID == "" {
		return nil, fmt.Errorf("tencent cm payload missing alarmStatus and policy info")
	}

	status := "firing"
	switch strings.ToLower(strings.TrimSpace(payload.AlarmStatus)) {
	case "0", "ok", "resolved", "recover":
		status = "resolved"
	case "1", "firing", "alarm":
		status = "firing"
	default:
		status = normalizeEventStatus(payload.AlarmStatus)
	}

	labels := map[string]string{}
	if payload.AlarmObjInfo.Region != "" {
		labels["region"] = payload.AlarmObjInfo.Region
	}
	if payload.AlarmObjInfo.Namespace != "" {
		labels["namespace"] = payload.AlarmObjInfo.Namespace
	}
	if payload.AlarmPolicyInfo.PolicyID != "" {
		labels["policy_id"] = payload.AlarmPolicyInfo.PolicyID
	}
	if payload.AlarmPolicyInfo.Conditions.MetricName != "" {
		labels["metric"] = payload.AlarmPolicyInfo.Conditions.MetricName
	}

	target := ""
	switch dims := payload.AlarmObjInfo.Dimensions.(type) {
	case map[string]any:
		var objName, instanceID string
		for k, v := range dims {
			sv := strings.TrimSpace(stringifyAny(v))
			if sv == "" {
				continue
			}
			labels[k] = sv
			if k == "objName" {
				objName = sv
			} else if strings.Contains(strings.ToLower(k), "instance") && instanceID == "" {
				instanceID = sv
			}
		}
		target = firstPresent(instanceID, objName)
	case string:
		target = strings.TrimSpace(dims)
		if target != "" {
			labels["dimensions"] = target
		}
	}

	policyName := firstPresent(payload.AlarmPolicyInfo.PolicyName, payload.AlarmPolicyInfo.Conditions.MetricShowName, "腾讯云告警")
	cond := payload.AlarmPolicyInfo.Conditions
	summary := firstPresent(
		strings.TrimSpace(fmt.Sprintf("%s %s %s (当前值 %s%s)",
			firstPresent(cond.MetricShowName, cond.MetricName),
			cond.CalcType,
			cond.CalcValue,
			cond.CurrentValue,
			firstPresent(cond.Unit, cond.CalcUnit),
		)),
		policyName,
	)

	var occ time.Time
	if status == "resolved" && payload.RecoverTime != "" {
		occ = parseAnyTimestamp(payload.RecoverTime)
	} else {
		occ = parseAnyTimestamp(payload.FirstOccurTime)
	}

	return &AdapterResult{
		Events: []NormalizedAlertEvent{{
			RawDedupeKey: firstPresent(payload.SessionID, joinNonEmpty(":", payload.AlarmPolicyInfo.PolicyID, target)),
			Status:       status,
			RuleKey:      sanitizeRuleKey(firstPresent(payload.AlarmPolicyInfo.PolicyID, policyName), "tencent_cm"),
			RuleName:     policyName,
			Severity:     normalizeExternalSeverity(payload.AlarmType),
			Title:        formatRuleAndTarget(policyName, target),
			Summary:      summary,
			Description:  summary,
			Target:       target,
			Value:        parseNumericStr(cond.CurrentValue),
			Threshold:    parseNumericStr(cond.CalcValue),
			Labels:       labels,
			OccurredAt:   occ,
		}},
	}, nil
}

func (tencentCMAdapter) SamplePayload(action string) (string, []byte) {
	alarmStatus := "1"
	recoverTime := ""
	curVal := "94.5"
	if strings.EqualFold(action, "resolve") {
		alarmStatus = "0"
		recoverTime = time.Now().UTC().Format("2006-01-02 15:04:05")
		curVal = "38.0"
	}
	body, _ := json.Marshal(map[string]any{
		"sessionId":      "tcm-session-demo-01",
		"alarmStatus":    alarmStatus,
		"alarmType":      "metric",
		"firstOccurTime": time.Now().UTC().Add(-3 * time.Minute).Format("2006-01-02 15:04:05"),
		"recoverTime":    recoverTime,
		"alarmObjInfo": map[string]any{
			"region":    "ap-guangzhou",
			"namespace": "qce/cvm",
			"dimensions": map[string]string{
				"unInstanceId": "ins-demo9527",
				"objName":      "10.0.4.12",
			},
		},
		"alarmPolicyInfo": map[string]any{
			"policyId":   "policy-tcm-001",
			"policyName": "[演练] 腾讯云 CVM 磁盘 IO 利用率告警",
			"conditions": map[string]string{
				"metricName":     "CpuUsage",
				"metricShowName": "CPU 利用率",
				"calcType":       ">",
				"calcValue":      "85",
				"currentValue":   curVal,
				"unit":           "%",
			},
		},
	})
	return "application/json", body
}

// ---------------------------------------------------------------------------
// 5. Huawei Cloud CES / SMN
// ---------------------------------------------------------------------------

type huaweiCESAdapter struct{}

func (huaweiCESAdapter) Provider() string { return model.IngestProviderHuaweiCES }

func (huaweiCESAdapter) Parse(_ string, _ http.Header, body []byte) (*AdapterResult, error) {
	var envelope struct {
		Type              string `json:"type"`
		MessageID         string `json:"message_id"`
		MessageIDAlt      string `json:"MessageId"`
		TopicURN          string `json:"topic_urn"`
		TopicURNAlt       string `json:"TopicArn"`
		Subject           string `json:"subject"`
		Message           string `json:"message"`
		SubscribeURL      string `json:"subscribe_url"`
		SubscribeURLCamel string `json:"SubscribeURL"`
		Timestamp         string `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode huawei ces JSON: %w", err)
	}

	// SMN Subscription Confirmation: store subscribe_url safely without triggering server-side HTTP calls.
	if strings.EqualFold(strings.TrimSpace(envelope.Type), "SubscriptionConfirmation") {
		subURL := strings.TrimSpace(firstPresent(envelope.SubscribeURL, envelope.SubscribeURLCamel))
		if subURL == "" {
			return nil, fmt.Errorf("huawei SMN SubscriptionConfirmation missing subscribe_url")
		}
		return &AdapterResult{
			SubscribeURL: subURL,
			InfoMessage:  "收到华为云 SMN 订阅确认请求，请复制确认 URL 完成终端订阅。",
		}, nil
	}

	// Unwrap SMN Notification outer envelope when present.
	innerBody := body
	if strings.EqualFold(strings.TrimSpace(envelope.Type), "Notification") && strings.TrimSpace(envelope.Message) != "" {
		msgTrimmed := strings.TrimSpace(envelope.Message)
		topicURN := firstPresent(envelope.TopicURN, envelope.TopicURNAlt)
		messageID := firstPresent(envelope.MessageID, envelope.MessageIDAlt)
		if strings.HasPrefix(msgTrimmed, "{") {
			innerBody = []byte(msgTrimmed)
		} else {
			ruleName := firstPresent(envelope.Subject, "华为云 SMN 通知")
			return &AdapterResult{
				Events: []NormalizedAlertEvent{{
					RawDedupeKey: firstPresent(messageID, joinNonEmpty(":", topicURN, ruleName)),
					Status:       "firing",
					RuleKey:      sanitizeRuleKey(ruleName, "huawei_ces"),
					RuleName:     ruleName,
					Title:        ruleName,
					Summary:      msgTrimmed,
					Description:  msgTrimmed,
					Labels: map[string]string{
						"topic_urn": topicURN,
					},
					OccurredAt: parseAnyTimestamp(envelope.Timestamp),
				}},
			}, nil
		}
	}

	var ces struct {
		AlarmID      string `json:"alarm_id"`
		AlarmName    string `json:"alarm_name"`
		AlarmStatus  string `json:"alarm_status"`
		AlarmLevel   any    `json:"alarm_level"`
		AlarmType    string `json:"alarm_type"`
		Region       string `json:"region"`
		ResourceName string `json:"resource_name"`
		ResourceID   string `json:"resource_id"`
		Condition    string `json:"condition"`
		CurrentData  any    `json:"current_data"`
		Time         any    `json:"time"`
		Namespace    string `json:"namespace"`
		Metric       struct {
			Namespace  string `json:"namespace"`
			MetricName string `json:"metric_name"`
			Dimensions []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"dimensions"`
		} `json:"metric"`
		Data struct {
			AlarmID      string `json:"alarm_id"`
			AlarmName    string `json:"alarm_name"`
			AlarmStatus  string `json:"alarm_status"`
			AlarmLevel   any    `json:"alarm_level"`
			ResourceName string `json:"resource_name"`
			Namespace    string `json:"namespace"`
			MetricName   string `json:"metric_name"`
			CurrentData  any    `json:"current_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(innerBody, &ces); err != nil {
		return nil, fmt.Errorf("decode huawei ces payload: %w", err)
	}

	alarmID := firstPresent(ces.AlarmID, ces.Data.AlarmID)
	alarmName := firstPresent(ces.AlarmName, ces.Data.AlarmName, envelope.Subject, "huawei_ces_alarm")
	alarmStatus := firstPresent(ces.AlarmStatus, ces.Data.AlarmStatus)
	if alarmID == "" && alarmName == "huawei_ces_alarm" && alarmStatus == "" {
		return nil, fmt.Errorf("huawei ces payload missing alarm identifiers")
	}

	labels := map[string]string{}
	if ns := firstPresent(ces.Metric.Namespace, ces.Namespace, ces.Data.Namespace); ns != "" {
		labels["namespace"] = ns
	}
	if m := firstPresent(ces.Metric.MetricName, ces.Data.MetricName); m != "" {
		labels["metric"] = m
	}
	if ces.Region != "" {
		labels["region"] = ces.Region
	}
	if alarmID != "" {
		labels["alarm_id"] = alarmID
	}
	dimTarget := ""
	for _, d := range ces.Metric.Dimensions {
		if d.Name != "" && d.Value != "" {
			labels[d.Name] = d.Value
			if dimTarget == "" {
				dimTarget = d.Value
			}
		}
	}
	target := firstPresent(ces.ResourceName, ces.Data.ResourceName, ces.ResourceID, dimTarget)
	curData := stringifyAny(ces.CurrentData)
	if curData == "" {
		curData = stringifyAny(ces.Data.CurrentData)
	}

	levelRaw := stringifyAny(ces.AlarmLevel)
	if levelRaw == "" {
		levelRaw = stringifyAny(ces.Data.AlarmLevel)
	}

	summary := strings.TrimSpace(firstPresent(
		fmt.Sprintf("%s %s (当前值: %s)", alarmName, ces.Condition, curData),
		alarmName,
	))

	return &AdapterResult{
		Events: []NormalizedAlertEvent{{
			RawDedupeKey: joinNonEmpty(":", firstPresent(alarmID, alarmName), target),
			Status:       normalizeEventStatus(firstPresent(alarmStatus, "alarm")),
			RuleKey:      sanitizeRuleKey(firstPresent(alarmID, alarmName), "huawei_ces"),
			RuleName:     alarmName,
			Severity:     normalizeHuaweiSeverity(levelRaw),
			Title:        formatRuleAndTarget(alarmName, target),
			Summary:      summary,
			Description:  strings.TrimSpace(ces.Condition),
			Target:       target,
			Value:        parseNumericStr(curData),
			Labels:       labels,
			OccurredAt:   firstParsedTime(stringifyAny(ces.Time), envelope.Timestamp),
		}},
	}, nil
}

func (huaweiCESAdapter) SamplePayload(action string) (string, []byte) {
	alarmStatus := "alarm"
	curVal := 91.8
	if strings.EqualFold(action, "resolve") {
		alarmStatus = "ok"
		curVal = 35.2
	}
	inner, _ := json.Marshal(map[string]any{
		"alarm_id":      "al-huawei-demo-01",
		"alarm_name":    "[演练] 华为云 RDS 内存使用率超限",
		"alarm_status":  alarmStatus,
		"alarm_level":   2,
		"region":        "cn-north-4",
		"resource_name": "rds-mysql-core-01",
		"condition":     "mem_util >= 85%",
		"current_data":  curVal,
		"time":          time.Now().UTC().UnixMilli(),
		"metric": map[string]any{
			"namespace":   "SYS.RDS",
			"metric_name": "rds002_mem_util",
			"dimensions": []map[string]string{
				{"name": "rds_instance_id", "value": "rds-core-01"},
			},
		},
	})
	body, _ := json.Marshal(map[string]any{
		"type":       "Notification",
		"message_id": "smn-msg-demo-01",
		"topic_urn":  "urn:smn:cn-north-4:demo:opspilot-alerts",
		"subject":    "华为云 CES 告警通知",
		"message":    string(inner),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	})
	return "application/json", body
}

// ---------------------------------------------------------------------------
// 6. Zabbix (Webhook JSON / Uppercase Macros / 0-5 Severity)
// ---------------------------------------------------------------------------

type zabbixAdapter struct{}

func (zabbixAdapter) Provider() string { return model.IngestProviderZabbix }

func (zabbixAdapter) Parse(_ string, _ http.Header, body []byte) (*AdapterResult, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode zabbix JSON: %w", err)
	}

	getField := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				if s := strings.TrimSpace(stringifyAny(v)); s != "" {
					return s
				}
			}
		}
		return ""
	}

	eventID := getField("event_id", "EVENT_ID", "EVENT.ID")
	triggerID := getField("trigger_id", "TRIGGER_ID", "TRIGGER.ID")
	rawStatus := getField("status", "event_status", "EVENT_STATUS", "EVENT.STATUS", "event_value", "EVENT_VALUE", "EVENT.VALUE")
	rawSev := getField("severity", "event_nseverity", "EVENT_NSEVERITY", "EVENT.NSEVERITY", "EVENT_SEVERITY", "EVENT.SEVERITY")
	name := getField("event_name", "trigger_name", "title", "EVENT_NAME", "EVENT.NAME", "TRIGGER_NAME", "TRIGGER.NAME", "alert_subject", "ALERT_SUBJECT")
	host := getField("host", "host_name", "HOST_NAME", "HOST.NAME")
	hostIP := getField("host_ip", "HOST_IP", "HOST.IP", "HOST.CONN")
	message := getField("message", "alert_message", "ALERT_MESSAGE", "ALERT.MESSAGE", "opdata", "EVENT_OPDATA", "EVENT.OPDATA")
	itemValue := getField("item_value", "ITEM_LASTVALUE", "ITEM.LASTVALUE", "value")
	itemKey := getField("item_key", "ITEM_KEY", "ITEM.KEY")
	triggerURL := getField("trigger_url", "TRIGGER_URL", "TRIGGER.URL", "event_url")
	occurredRaw := getField("occurred_at", "timestamp", "event_time", "EVENT_TIMESTAMP")

	if name == "" && triggerID == "" && eventID == "" && rawStatus == "" {
		return nil, fmt.Errorf("zabbix payload missing trigger/event identifiers")
	}

	labels := map[string]string{}
	if host != "" {
		labels["host"] = host
	}
	if hostIP != "" {
		labels["host_ip"] = hostIP
	}
	if triggerID != "" {
		labels["trigger_id"] = triggerID
	}
	if eventID != "" {
		labels["event_id"] = eventID
	}
	if itemKey != "" {
		labels["item_key"] = itemKey
	}
	extractZabbixTags(raw["tags"], labels)
	extractZabbixTags(raw["EVENT_TAGSJSON"], labels)
	extractZabbixTags(raw["EVENT.TAGSJSON"], labels)

	target := firstPresent(host, hostIP)
	ruleName := firstPresent(name, "zabbix_trigger")

	status := "firing"
	switch strings.ToLower(strings.TrimSpace(rawStatus)) {
	case "0", "ok", "resolved", "recover", "normal":
		status = "resolved"
	case "1", "problem", "firing", "alert":
		status = "firing"
	default:
		status = normalizeEventStatus(rawStatus)
	}

	// Zabbix uses trigger_id + host for persistent PROBLEM/OK correlation across distinct recovery event IDs.
	dedupeSeed := ""
	if triggerID != "" {
		dedupeSeed = "trigger:" + triggerID + ":" + firstPresent(host, hostIP, "default")
	} else if eventID != "" {
		dedupeSeed = "event:" + eventID
	}

	return &AdapterResult{
		Events: []NormalizedAlertEvent{{
			RawDedupeKey: dedupeSeed,
			Status:       status,
			RuleKey:      sanitizeRuleKey(firstPresent(itemKey, triggerID, ruleName), "zabbix_trigger"),
			RuleName:     ruleName,
			Severity:     normalizeZabbixSeverity(rawSev),
			Title:        formatRuleAndTarget(ruleName, target),
			Summary:      firstPresent(message, formatRuleAndTarget(ruleName, target)),
			Description:  firstPresent(message, itemValue),
			Target:       target,
			Value:        parseNumericStr(itemValue),
			Labels:       labels,
			RunbookURL:   triggerURL,
			OccurredAt:   parseAnyTimestamp(occurredRaw),
		}},
	}, nil
}

func (zabbixAdapter) SamplePayload(action string) (string, []byte) {
	status := "PROBLEM"
	val := "98.1 %"
	msg := "[演练] Zabbix Trigger: 负载均衡节点 /var 磁盘剩余不足 5%"
	if strings.EqualFold(action, "resolve") {
		status = "OK"
		val = "42.0 %"
		msg = "[演练] Zabbix Resolved: 磁盘清理已完成，使用率恢复正常"
	}
	body, _ := json.Marshal(map[string]any{
		"event_id":   "9800412",
		"trigger_id": "44102",
		"status":     status,
		"severity":   "4",
		"event_name": "[演练] /var 分区磁盘剩余空间过低",
		"host":       "lb-edge-gz-02",
		"host_ip":    "10.20.8.15",
		"item_key":   "vfs.fs.size[/var,pfree]",
		"item_value": val,
		"message":    msg,
		"tags": map[string]string{
			"service": "gateway",
			"env":     "prod",
		},
	})
	return "application/json", body
}

// ---------------------------------------------------------------------------
// 7. Generic Webhook (Flat JSON & PagerDuty Events v2 compatible)
// ---------------------------------------------------------------------------

type genericWebhookAdapter struct{}

func (genericWebhookAdapter) Provider() string { return model.IngestProviderGenericWebhook }

func (genericWebhookAdapter) Parse(_ string, _ http.Header, body []byte) (*AdapterResult, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode generic webhook JSON: %w", err)
	}

	payloadMap := map[string]any{}
	if nested, ok := raw["payload"].(map[string]any); ok {
		payloadMap = nested
	}

	getField := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				if s := strings.TrimSpace(stringifyAny(v)); s != "" {
					return s
				}
			}
			if v, ok := payloadMap[k]; ok {
				if s := strings.TrimSpace(stringifyAny(v)); s != "" {
					return s
				}
			}
		}
		return ""
	}

	rawAction := getField("event_action", "status", "state")
	if rawAction == "" && getField("title", "summary", "rule", "rule_key", "message") == "" {
		return nil, fmt.Errorf("generic webhook payload missing status and title/summary")
	}

	labels := map[string]string{}
	if lm, ok := raw["labels"].(map[string]any); ok {
		for k, v := range lm {
			if sv := strings.TrimSpace(stringifyAny(v)); sv != "" {
				labels[k] = sv
			}
		}
	}
	if comp := getField("component"); comp != "" && labels["component"] == "" {
		labels["component"] = comp
	}
	if grp := getField("group"); grp != "" && labels["service"] == "" {
		labels["service"] = grp
	}

	target := firstPresent(getField("target", "host", "source", "service"), extractTargetFromLabels(labels))
	if target != "" && labels["target"] == "" {
		labels["target"] = target
	}

	annotations := map[string]string{}
	if am, ok := raw["annotations"].(map[string]any); ok {
		for k, v := range am {
			if sv := strings.TrimSpace(stringifyAny(v)); sv != "" {
				annotations[k] = sv
			}
		}
	}

	ruleName := firstPresent(getField("rule_name", "rule", "class", "title", "summary"), "generic_webhook")
	ruleKey := sanitizeRuleKey(firstPresent(getField("rule_key", "rule", "class"), ruleName), "generic_webhook")
	title := firstPresent(getField("title", "summary"), formatRuleAndTarget(ruleName, target))
	summary := firstPresent(getField("summary", "message", "description"), title)

	return &AdapterResult{
		Events: []NormalizedAlertEvent{{
			RawDedupeKey: getField("dedup_key", "dedupe_key", "fingerprint", "id"),
			Status:       normalizeEventStatus(firstPresent(rawAction, "firing")),
			RuleKey:      ruleKey,
			RuleName:     ruleName,
			Severity:     normalizeExternalSeverity(getField("severity", "level", "priority")),
			Title:        title,
			Summary:      summary,
			Description:  getField("description", "message"),
			Target:       target,
			Value:        parseNumericStr(getField("value")),
			Threshold:    parseNumericStr(getField("threshold")),
			Labels:       labels,
			Annotations:  annotations,
			RunbookURL:   getField("runbook_url", "client_url", "url"),
			OccurredAt:   firstParsedTime(getField("occurred_at", "timestamp")),
		}},
	}, nil
}

func (genericWebhookAdapter) SamplePayload(action string) (string, []byte) {
	eventAction := "trigger"
	summary := "[演练] 通用 Webhook 核心支付回调耗时异常"
	if strings.EqualFold(action, "resolve") {
		eventAction = "resolve"
		summary = "[演练] 通用 Webhook 支付回调耗时恢复正常"
	}
	body, _ := json.Marshal(map[string]any{
		"event_action": eventAction,
		"dedup_key":    "generic-demo-payment-01",
		"severity":     "critical",
		"rule_key":     "payment_callback_slow",
		"title":        summary,
		"summary":      summary,
		"target":       "payment-gateway-prod",
		"labels": map[string]string{
			"service": "payment",
			"env":     "prod",
		},
		"runbook_url": "https://runbook.example.com/payment-callback",
	})
	return "application/json", body
}

// ---------------------------------------------------------------------------
// Normalization & Deterministic Dedupe Helpers
// ---------------------------------------------------------------------------

func buildExternalDedupeKey(sourceID uint64, rawKey string, ev NormalizedAlertEvent) string {
	prefix := fmt.Sprintf("ext:%d:", sourceID)
	seed := strings.TrimSpace(rawKey)
	if seed == "" {
		keys := make([]string, 0, len(ev.Labels))
		for k := range ev.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString(ev.RuleKey)
		b.WriteByte('|')
		b.WriteString(ev.Target)
		for _, k := range keys {
			b.WriteByte('|')
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(ev.Labels[k])
		}
		sum := sha256.Sum256([]byte(b.String()))
		seed = fmt.Sprintf("auto:%x", sum[:16])
	}
	full := prefix + seed
	if utf8.RuneCountInString(full) <= 185 {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	return fmt.Sprintf("%ssha256:%x", prefix, sum[:20])
}

func normalizeEventStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "resolved", "resolve", "ok", "recover", "recovered", "closed", "0", "normal", "cleared":
		return "resolved"
	default:
		return "firing"
	}
}

func normalizeExternalSeverity(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "critical", "crit", "p0", "p1", "fatal", "emergency", "disaster", "high", "error", "err", "1", "2":
		return "critical"
	case "warning", "warn", "p2", "minor", "major", "average", "medium", "3":
		return "warning"
	case "info", "informational", "notice", "p3", "p4", "low", "ok", "4", "0":
		return "info"
	default:
		if strings.Contains(s, "crit") || strings.Contains(s, "fatal") || strings.Contains(s, "high") {
			return "critical"
		}
		if strings.Contains(s, "warn") || strings.Contains(s, "med") {
			return "warning"
		}
		if strings.Contains(s, "info") || strings.Contains(s, "low") {
			return "info"
		}
		return ""
	}
}

func normalizeHuaweiSeverity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "2", "critical", "major", "紧急", "重要":
		return "critical"
	case "3", "minor", "次要":
		return "warning"
	case "4", "info", "informational", "提示":
		return "info"
	default:
		return normalizeExternalSeverity(raw)
	}
}

func normalizeZabbixSeverity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "5", "4", "disaster", "high", "灾难", "严重":
		return "critical"
	case "3", "2", "average", "warning", "一般严重", "警告":
		return "warning"
	case "1", "0", "information", "not classified", "info", "信息", "未分类":
		return "info"
	default:
		return normalizeExternalSeverity(raw)
	}
}

func sanitizeRuleKey(raw, fallback string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback
	}
	return truncateRunes(s, 128)
}

func extractTargetFromLabels(labels map[string]string) string {
	for _, key := range []string{"target", "host", "hostname", "instance", "node", "service", "pod", "cluster"} {
		if v := strings.TrimSpace(labels[key]); v != "" {
			return v
		}
	}
	return ""
}

func formatRuleAndTarget(ruleName, target string) string {
	if target == "" {
		return ruleName
	}
	if strings.Contains(ruleName, target) {
		return ruleName
	}
	return fmt.Sprintf("%s - %s", ruleName, target)
}

func cloneAndMergeMaps(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			kt := strings.TrimSpace(k)
			vt := strings.TrimSpace(v)
			if kt != "" && vt != "" {
				out[kt] = vt
			}
		}
	}
	return out
}

func extractZabbixTags(v any, out map[string]string) {
	if v == nil {
		return
	}
	switch typed := v.(type) {
	case map[string]any:
		for k, val := range typed {
			if s := strings.TrimSpace(stringifyAny(val)); s != "" {
				out[k] = s
			}
		}
	case []any:
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				tk := strings.TrimSpace(stringifyAny(m["tag"]))
				tv := strings.TrimSpace(stringifyAny(m["value"]))
				if tk != "" {
					out[tk] = firstPresent(tv, "true")
				}
			}
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return
		}
		var parsedAny any
		if json.Unmarshal([]byte(trimmed), &parsedAny) == nil {
			extractZabbixTags(parsedAny, out)
		}
	}
}

func parseAnyTimestamp(raw string) time.Time {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	if iv, err := strconv.ParseInt(s, 10, 64); err == nil && iv > 0 {
		if iv > 1e12 {
			return time.UnixMilli(iv).UTC()
		}
		return time.Unix(iv, 0).UTC()
	}
	return time.Time{}
}

func firstParsedTime(values ...string) time.Time {
	for _, v := range values {
		if t := parseAnyTimestamp(v); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func parseNumericAny(v any) *float64 {
	if v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		val := n
		return &val
	case int64:
		val := float64(n)
		return &val
	case int:
		val := float64(n)
		return &val
	default:
		return parseNumericStr(stringifyAny(v))
	}
}

func parseNumericStr(raw string) *float64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	// Strip trailing units like "98.1 %" or "42.0ms".
	parts := strings.Fields(s)
	if len(parts) > 0 {
		candidate := strings.TrimRight(parts[0], "%ms")
		if v, err := strconv.ParseFloat(candidate, 64); err == nil {
			return &v
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return &v
	}
	return nil
}

func stringifyAny(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case float64:
		if s == float64(int64(s)) {
			return strconv.FormatInt(int64(s), 10)
		}
		return strconv.FormatFloat(s, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(s)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, sep)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	if max == 1 {
		return string([]rune(s)[:1])
	}
	return string([]rune(s)[:max-1]) + "…"
}

func firstPresent(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
