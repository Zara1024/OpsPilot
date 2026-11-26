package oncall

import (
	"crypto/subtle"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	bizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// quickActionConfirm renders a read-only confirmation page for signed ChatOps links so GET link previews never mutate state.
// @Summary Render signed quick-action confirmation page for ChatOps cards and emails
// @Router /api/v1/oncall/chatops/quick-action [get]
// @Success 200 {string} string "HTML confirmation page"
func (h *Handler) quickActionConfirm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	incStr := strings.TrimSpace(q.Get("inc"))
	action := strings.ToLower(strings.TrimSpace(q.Get("action")))
	expStr := strings.TrimSpace(q.Get("exp"))
	sig := strings.TrimSpace(q.Get("sig"))

	cfg, err := h.store.GetChannelConfig(r.Context())
	if err != nil || cfg == nil {
		http.Error(w, "channel config unavailable", http.StatusInternalServerError)
		return
	}
	if verifyErr := bizoncall.VerifyQuickAction(cfg.ChatOpsCallbackToken, incStr, action, expStr, sig, time.Now()); verifyErr != nil {
		http.Error(w, "链接签名无效或已过期", http.StatusUnauthorized)
		return
	}

	actionLabel := quickActionLabelCN(action)
	page := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>OpsPilot On-Call 操作确认</title></head>
<body style="font-family:system-ui,-apple-system,sans-serif;background:#f8fafc;color:#0f172a;padding:32px 16px;">
<div style="max-width:440px;margin:0 auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:24px;">
<h2 style="margin:0 0 8px;font-size:18px;">确认执行：%s</h2>
<p style="margin:0 0 20px;font-size:14px;color:#475569;">告警编号 <strong>INC-%s</strong>。为防止即时通讯预览误触发，请点击下方按钮确认。</p>
<form method="POST" action="/api/v1/oncall/chatops/quick-action">
<input type="hidden" name="inc" value="%s"/>
<input type="hidden" name="action" value="%s"/>
<input type="hidden" name="exp" value="%s"/>
<input type="hidden" name="sig" value="%s"/>
<button type="submit" style="width:100%%;padding:10px 16px;background:#4f46e5;color:#fff;border:none;border-radius:8px;font-size:14px;font-weight:600;cursor:pointer;">确认执行</button>
</form></div></body></html>`,
		html.EscapeString(actionLabel),
		html.EscapeString(incStr),
		html.EscapeString(incStr),
		html.EscapeString(action),
		html.EscapeString(expStr),
		html.EscapeString(sig),
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

// quickActionExecute verifies the signed payload and performs the ChatOps alert transition via POST.
// @Summary Execute signed quick-action for an On-Call alert
// @Router /api/v1/oncall/chatops/quick-action [post]
// @Success 200 {object} bizoncall.ChatOpsActionResult
func (h *Handler) quickActionExecute(w http.ResponseWriter, r *http.Request) {
	var incStr, action, expStr, sig string
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var body struct {
			Inc    string `json:"inc"`
			Action string `json:"action"`
			Exp    string `json:"exp"`
			Sig    string `json:"sig"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&body); err != nil {
			writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
			return
		}
		incStr, action, expStr, sig = body.Inc, body.Action, body.Exp, body.Sig
	} else {
		if err := r.ParseForm(); err != nil {
			writeErr(w, fmt.Errorf("%w: %v", errs.ErrInvalid, err))
			return
		}
		incStr = r.FormValue("inc")
		action = r.FormValue("action")
		expStr = r.FormValue("exp")
		sig = r.FormValue("sig")
	}

	incStr = strings.TrimSpace(incStr)
	action = strings.ToLower(strings.TrimSpace(action))
	expStr = strings.TrimSpace(expStr)
	sig = strings.TrimSpace(sig)

	cfg, err := h.store.GetChannelConfig(r.Context())
	if err != nil || cfg == nil {
		http.Error(w, "channel config unavailable", http.StatusInternalServerError)
		return
	}
	if verifyErr := bizoncall.VerifyQuickAction(cfg.ChatOpsCallbackToken, incStr, action, expStr, sig, time.Now()); verifyErr != nil {
		http.Error(w, "invalid or expired quick action signature", http.StatusUnauthorized)
		return
	}

	res, handleErr := h.escalationEngine.HandleAction(r.Context(), bizoncall.ChatOpsActionRequest{
		Action:     action,
		IncidentID: incStr,
		Note:       "Via ChatOps signed quick-action link",
	})
	if handleErr != nil {
		writeErr(w, handleErr)
		return
	}

	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		page := fmt.Sprintf(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>OpsPilot On-Call 已执行</title></head>
<body style="font-family:system-ui,-apple-system,sans-serif;background:#f8fafc;color:#0f172a;padding:32px 16px;">
<div style="max-width:440px;margin:0 auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:24px;">
<h2 style="margin:0 0 8px;font-size:18px;">%s</h2>
<p style="margin:0;font-size:14px;color:#475569;">%s</p>
</div></body></html>`, html.EscapeString(res.StatusText), html.EscapeString(res.Message))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(page))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// @Summary Receive Feishu/Lark interactive alert card action callback
// @Router /api/v1/oncall/chatops/callback/feishu [post]
// @Success 200 {object} map[string]any
func (h *Handler) feishuChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
	if err != nil {
		writeErr(w, fmt.Errorf("%w: read feishu body", errs.ErrInvalid))
		return
	}

	cfg, cfgErr := h.store.GetChannelConfig(r.Context())
	if cfgErr != nil || cfg == nil {
		http.Error(w, "channel config unavailable", http.StatusInternalServerError)
		return
	}

	queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
	queryTokenValid := cfg.ChatOpsCallbackToken != "" && subtle.ConstantTimeCompare([]byte(queryToken), []byte(cfg.ChatOpsCallbackToken)) == 1

	// 1. Decrypt AES-256-CBC envelope if present, validating X-Lark-Signature when encryptKey is set.
	payloadBytes := rawBody
	var envelope struct {
		Encrypt string `json:"encrypt"`
	}
	if json.Unmarshal(rawBody, &envelope) == nil && envelope.Encrypt != "" {
		ts := r.Header.Get("X-Lark-Request-Timestamp")
		nonce := r.Header.Get("X-Lark-Request-Nonce")
		sig := r.Header.Get("X-Lark-Signature")
		sigValid := cfg.FeishuEncryptKey != "" && sig != "" &&
			bizoncall.VerifyFeishuEventSignature(ts, nonce, cfg.FeishuEncryptKey, rawBody, sig) == nil
		if !sigValid && !queryTokenValid {
			http.Error(w, "invalid feishu signature", http.StatusUnauthorized)
			return
		}
		if cfg.FeishuEncryptKey == "" {
			http.Error(w, "feishu encrypt key is not configured", http.StatusUnauthorized)
			return
		}
		decrypted, decErr := bizoncall.DecryptFeishuEvent(cfg.FeishuEncryptKey, envelope.Encrypt)
		if decErr != nil {
			http.Error(w, "failed to decrypt feishu callback", http.StatusUnauthorized)
			return
		}
		payloadBytes = decrypted
	} else {
		// Unencrypted callback must carry valid verification token in payload OR valid ?token= query param.
		var tokProbe struct {
			Token  string `json:"token"`
			Header struct {
				Token string `json:"token"`
			} `json:"header"`
		}
		_ = json.Unmarshal(payloadBytes, &tokProbe)
		bodyToken := tokProbe.Token
		if bodyToken == "" {
			bodyToken = tokProbe.Header.Token
		}
		verifyTokenValid := cfg.FeishuVerificationToken != "" &&
			subtle.ConstantTimeCompare([]byte(bodyToken), []byte(cfg.FeishuVerificationToken)) == 1
		if !queryTokenValid && !verifyTokenValid {
			http.Error(w, "unauthorized feishu callback", http.StatusUnauthorized)
			return
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		writeErr(w, fmt.Errorf("%w: invalid feishu json", errs.ErrInvalid))
		return
	}

	if msgType, _ := payload["type"].(string); msgType == "url_verification" {
		challenge, _ := payload["challenge"].(string)
		writeJSON(w, http.StatusOK, map[string]any{"challenge": challenge})
		return
	}

	action, incID, actor := extractCardActionFields(payload)
	if action == "" || incID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]any{"type": "info", "content": "未识别到可执行的告警操作"},
		})
		return
	}

	note := "Via Feishu Interactive Card"
	if actor != "" {
		note = fmt.Sprintf("Via Feishu Card (%s)", actor)
	}
	res, err := h.escalationEngine.HandleAction(r.Context(), bizoncall.ChatOpsActionRequest{
		Action:     action,
		IncidentID: incID,
		Note:       note,
	})
	if err != nil {
		slog.Warn("feishu chatops action failed", slog.String("action", action), slog.String("incident_id", incID), slog.Any("err", err))
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]any{"type": "error", "content": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"toast": map[string]any{
			"type":    "success",
			"content": res.StatusText + "：" + res.Message,
		},
		"result": res,
	})
}

// @Summary Receive DingTalk interactive ActionCard callback
// @Router /api/v1/oncall/chatops/callback/dingtalk [post]
// @Success 200 {object} map[string]any
func (h *Handler) dingTalkChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
	if err != nil {
		writeErr(w, fmt.Errorf("%w: read dingtalk body", errs.ErrInvalid))
		return
	}

	cfg, cfgErr := h.store.GetChannelConfig(r.Context())
	if cfgErr != nil || cfg == nil {
		http.Error(w, "channel config unavailable", http.StatusInternalServerError)
		return
	}

	queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
	queryTokenValid := cfg.ChatOpsCallbackToken != "" && subtle.ConstantTimeCompare([]byte(queryToken), []byte(cfg.ChatOpsCallbackToken)) == 1
	ts := r.Header.Get("timestamp")
	sign := r.Header.Get("sign")
	hmacValid := cfg.DingTalkAppSecret != "" &&
		bizoncall.VerifyDingTalkCallbackSignature(ts, cfg.DingTalkAppSecret, sign, time.Now()) == nil

	if !queryTokenValid && !hmacValid {
		http.Error(w, "unauthorized dingtalk callback", http.StatusUnauthorized)
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		writeErr(w, fmt.Errorf("%w: invalid dingtalk json", errs.ErrInvalid))
		return
	}

	action, incID, actor := extractCardActionFields(payload)
	if action == "" || incID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"code":    0,
			"message": "no executable card action",
		})
		return
	}

	note := "Via DingTalk ActionCard"
	if actor != "" {
		note = fmt.Sprintf("Via DingTalk Card (%s)", actor)
	}
	res, err := h.escalationEngine.HandleAction(r.Context(), bizoncall.ChatOpsActionRequest{
		Action:     action,
		IncidentID: incID,
		Note:       note,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"code":    0,
		"message": res.Message,
		"toast":   res.StatusText,
		"result":  res,
	})
}

func extractCardActionFields(payload map[string]any) (action, incID, actor string) {
	if payload == nil {
		return "", "", ""
	}
	readValueMap := func(m map[string]any) {
		if m == nil {
			return
		}
		if a, _ := m["opspilot_action"].(string); a != "" {
			action = a
		} else if a, _ := m["action"].(string); a != "" {
			action = a
		}
		if inc, ok := m["incident_id"]; ok && inc != nil {
			incID = fmt.Sprintf("%v", inc)
		}
	}

	readValueMap(payload)

	if actObj, ok := payload["action"].(map[string]any); ok {
		if val, ok2 := actObj["value"].(map[string]any); ok2 {
			readValueMap(val)
		}
	}
	if evObj, ok := payload["event"].(map[string]any); ok {
		if actObj, ok2 := evObj["action"].(map[string]any); ok2 {
			if val, ok3 := actObj["value"].(map[string]any); ok3 {
				readValueMap(val)
			}
		}
		if opObj, ok2 := evObj["operator"].(map[string]any); ok2 {
			if oid, _ := opObj["open_id"].(string); oid != "" {
				actor = oid
			}
		}
	}
	if oid, _ := payload["open_id"].(string); actor == "" && oid != "" {
		actor = oid
	}
	if uid, _ := payload["userId"].(string); actor == "" && uid != "" {
		actor = uid
	}
	if contentStr, _ := payload["content"].(string); contentStr != "" && (action == "" || incID == "") {
		var nested map[string]any
		if json.Unmarshal([]byte(contentStr), &nested) == nil {
			readValueMap(nested)
			if cardParams, ok := nested["cardPrivateData"].(map[string]any); ok {
				if params, ok2 := cardParams["params"].(map[string]any); ok2 {
					readValueMap(params)
				}
			}
		}
	}
	return strings.ToLower(strings.TrimSpace(action)), strings.TrimSpace(incID), strings.TrimSpace(actor)
}

func quickActionLabelCN(action string) string {
	switch action {
	case "ack":
		return "认领告警 (Acknowledge)"
	case "silence":
		return "静音告警 30 分钟 (Silence 30m)"
	case "escalate":
		return "立即升级至二线 (Escalate)"
	default:
		return action
	}
}

// weComChatOpsVerify validates WeCom CorpApp URL signature and decrypts echostr on GET callback setup.
// @Summary Verify WeCom CorpApp callback URL signature and decrypt echostr
// @Router /api/v1/oncall/chatops/callback/wecom [get]
// @Success 200 {string} string "Decrypted echostr"
func (h *Handler) weComChatOpsVerify(w http.ResponseWriter, r *http.Request) {
	cfg, cfgErr := h.store.GetChannelConfig(r.Context())
	if cfgErr != nil || cfg == nil {
		http.Error(w, "channel config unavailable", http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	msgSig := q.Get("msg_signature")
	ts := q.Get("timestamp")
	nonce := q.Get("nonce")
	echoStr := q.Get("echostr")

	if err := bizoncall.VerifyWeComSignature(cfg.WeComToken, ts, nonce, echoStr, msgSig); err != nil {
		http.Error(w, "unauthorized wecom url verification", http.StatusUnauthorized)
		return
	}
	plainEcho, err := bizoncall.DecryptWeComMessage(cfg.WeComEncodingAESKey, echoStr, cfg.WeComCorpID)
	if err != nil {
		http.Error(w, "failed to decrypt wecom echostr", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// HTTP 200 响应头已发送，客户端断开等写入错误无法再改写状态码
	_, _ = w.Write(plainEcho)
}

// weComChatOpsCallback handles encrypted CorpApp XML callbacks and signed JSON webhook actions for WeCom.
// @Summary Receive WeCom CorpApp or interactive card callback
// @Router /api/v1/oncall/chatops/callback/wecom [post]
// @Success 200 {object} map[string]any
func (h *Handler) weComChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
	if err != nil {
		writeErr(w, fmt.Errorf("%w: read wecom body", errs.ErrInvalid))
		return
	}

	cfg, cfgErr := h.store.GetChannelConfig(r.Context())
	if cfgErr != nil || cfg == nil {
		http.Error(w, "channel config unavailable", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	queryToken := strings.TrimSpace(q.Get("token"))
	queryTokenValid := cfg.ChatOpsCallbackToken != "" && subtle.ConstantTimeCompare([]byte(queryToken), []byte(cfg.ChatOpsCallbackToken)) == 1

	var action, incID, actor string
	trimmed := strings.TrimSpace(string(rawBody))
	if strings.HasPrefix(trimmed, "<") {
		var outer struct {
			Encrypt string `xml:"Encrypt"`
		}
		if xmlErr := xml.Unmarshal(rawBody, &outer); xmlErr != nil || strings.TrimSpace(outer.Encrypt) == "" {
			http.Error(w, "invalid wecom xml envelope", http.StatusBadRequest)
			return
		}
		msgSig := q.Get("msg_signature")
		ts := q.Get("timestamp")
		nonce := q.Get("nonce")
		if sigErr := bizoncall.VerifyWeComSignature(cfg.WeComToken, ts, nonce, outer.Encrypt, msgSig); sigErr != nil {
			http.Error(w, "unauthorized wecom signature", http.StatusUnauthorized)
			return
		}
		decrypted, decErr := bizoncall.DecryptWeComMessage(cfg.WeComEncodingAESKey, outer.Encrypt, cfg.WeComCorpID)
		if decErr != nil {
			http.Error(w, "failed to decrypt wecom callback", http.StatusUnauthorized)
			return
		}
		var xmlEvent struct {
			FromUserName string `xml:"FromUserName"`
			Event        string `xml:"Event"`
			EventKey     string `xml:"EventKey"`
			TaskID       string `xml:"TaskId"`
		}
		if xmlErr := xml.Unmarshal(decrypted, &xmlEvent); xmlErr != nil {
			writeErr(w, fmt.Errorf("%w: invalid decrypted wecom xml", errs.ErrInvalid))
			return
		}
		actor = strings.TrimSpace(xmlEvent.FromUserName)
		action, incID = parseWeComActionKey(xmlEvent.EventKey, xmlEvent.TaskID)
	} else {
		if !queryTokenValid {
			http.Error(w, "unauthorized wecom callback", http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if jsonErr := json.Unmarshal(rawBody, &payload); jsonErr != nil {
			writeErr(w, fmt.Errorf("%w: invalid wecom json", errs.ErrInvalid))
			return
		}
		action, incID, actor = extractCardActionFields(payload)
		if (action == "" || incID == "") && payload != nil {
			ek, _ := payload["EventKey"].(string)
			tk, _ := payload["TaskId"].(string)
			a2, i2 := parseWeComActionKey(ek, tk)
			if action == "" {
				action = a2
			}
			if incID == "" {
				incID = i2
			}
		}
	}

	if action == "" || incID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"errcode": 0,
			"errmsg":  "ignored",
		})
		return
	}

	note := "Via WeCom Card"
	if actor != "" {
		note = fmt.Sprintf("Via WeCom Card (%s)", actor)
	}
	res, handleErr := h.escalationEngine.HandleAction(r.Context(), bizoncall.ChatOpsActionRequest{
		Action:     action,
		IncidentID: incID,
		Note:       note,
	})
	if handleErr != nil {
		slog.Warn("wecom chatops action failed", slog.String("action", action), slog.String("incident_id", incID), slog.Any("err", handleErr))
		writeJSON(w, http.StatusOK, map[string]any{
			"errcode": 1,
			"errmsg":  handleErr.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"errcode": 0,
		"errmsg":  "ok",
		"result":  res,
	})
}

func parseWeComActionKey(eventKey, taskID string) (action, incID string) {
	raw := strings.TrimSpace(eventKey)
	if raw == "" {
		raw = strings.TrimSpace(taskID)
	}
	if raw == "" {
		return "", ""
	}
	// Support "ack:17", "silence:17", "escalate:17", "ack_17", or EventKey="ack" + TaskId="17".
	for _, sep := range []string{":", "_", "|"} {
		if idx := strings.Index(raw, sep); idx > 0 {
			act := strings.ToLower(strings.TrimSpace(raw[:idx]))
			idPart := strings.TrimSpace(raw[idx+1:])
			if act != "" && idPart != "" {
				return act, idPart
			}
		}
	}
	if tid := strings.TrimSpace(taskID); tid != "" && tid != raw {
		return strings.ToLower(raw), tid
	}
	return "", ""
}
