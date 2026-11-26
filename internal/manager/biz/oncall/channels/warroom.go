package channels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultFeishuAPIBase = "https://open.feishu.cn"

func createWarRoom(
	ctx context.Context,
	client *http.Client,
	feishuBaseURL string,
	cfg *ChannelGatewayConfig,
	req *WarRoomCreateRequest,
) (*WarRoomCreateResult, error) {
	if req == nil || req.IncidentID == 0 {
		return nil, errors.New("incident_id is required to create war room")
	}
	roomName := fmt.Sprintf("[INC-%04d] %s", req.IncidentID, nonEmptyStr(req.Title, "应急作战室"))
	roomName = truncateRunes(roomName, 80)
	fallbackURL := buildConsoleIncidentURL(req.ConsoleURL, req.IncidentID)

	if cfg == nil {
		return consoleWarRoomFallback(roomName, fallbackURL), nil
	}

	switch strings.ToLower(strings.TrimSpace(cfg.WarRoomProvider)) {
	case "feishu":
		if strings.TrimSpace(cfg.FeishuAppID) == "" || strings.TrimSpace(cfg.FeishuAppSecret) == "" {
			return nil, errors.New("飞书建群模式未配置 AppID 或 AppSecret")
		}
		return createFeishuWarRoom(ctx, client, feishuBaseURL, cfg, req, "🚨 "+roomName)

	case "webhook":
		if strings.TrimSpace(cfg.WarRoomWebhookURL) == "" {
			return nil, errors.New("Webhook 建群模式未配置 WarRoomWebhookURL")
		}
		return createWebhookWarRoom(ctx, client, cfg, req, roomName, fallbackURL)

	default:
		return consoleWarRoomFallback(roomName, fallbackURL), nil
	}
}

func consoleWarRoomFallback(roomName, fallbackURL string) *WarRoomCreateResult {
	return &WarRoomCreateResult{
		Provider: "console",
		RoomName: roomName,
		RoomURL:  fallbackURL,
		Message:  "已生成 OpsPilot 控制台应急处置协同入口（如需自动拉飞书应急群，请在通道网关配置飞书或 Webhook 建群参数）。",
	}
}

func buildConsoleIncidentURL(consoleURL string, incidentID uint64) string {
	base := strings.TrimRight(strings.TrimSpace(consoleURL), "/")
	if base == "" {
		return fmt.Sprintf("/alerts/incidents/%d", incidentID)
	}
	return fmt.Sprintf("%s/alerts/incidents/%d", base, incidentID)
}

func createFeishuWarRoom(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	cfg *ChannelGatewayConfig,
	req *WarRoomCreateRequest,
	roomName string,
) (*WarRoomCreateResult, error) {
	if baseURL == "" {
		baseURL = defaultFeishuAPIBase
	}
	baseURL = strings.TrimRight(baseURL, "/")

	token, err := fetchFeishuTenantToken(ctx, client, baseURL, cfg.FeishuAppID, cfg.FeishuAppSecret)
	if err != nil {
		return nil, err
	}

	createBody, err := json.Marshal(map[string]any{
		"name":        roomName,
		"description": fmt.Sprintf("OpsPilot On-Call WarRoom | INC-%04d | 级别: %s", req.IncidentID, nonEmptyStr(req.Severity, "critical")),
		"chat_mode":   "group",
		"chat_type":   "private",
	})
	if err != nil {
		return nil, fmt.Errorf("encode feishu create chat payload: %w", err)
	}

	chatReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/open-apis/im/v1/chats", bytes.NewReader(createBody))
	if err != nil {
		return nil, fmt.Errorf("create feishu chat request: %w", err)
	}
	chatReq.Header.Set("Authorization", "Bearer "+token)
	chatReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(chatReq)
	if err != nil {
		return nil, fmt.Errorf("feishu create chat request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // body read completely below
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read feishu create chat response: %w", err)
	}

	var chatResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ChatID string `json:"chat_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		return nil, fmt.Errorf("decode feishu create chat response: %w", err)
	}
	if resp.StatusCode >= 400 || chatResp.Code != 0 || chatResp.Data.ChatID == "" {
		return nil, fmt.Errorf("feishu create chat failed (status=%d code=%d): %s", resp.StatusCode, chatResp.Code, chatResp.Msg)
	}

	chatID := chatResp.Data.ChatID
	_ = sendFeishuWarRoomBrief(ctx, client, baseURL, token, chatID, req) // best-effort welcome card after group creation

	return &WarRoomCreateResult{
		Provider:       "feishu",
		ExternalChatID: chatID,
		RoomName:       roomName,
		RoomURL:        "https://applink.feishu.cn/client/chat/open?openChatId=" + url.QueryEscape(chatID),
		Message:        fmt.Sprintf("已创建飞书应急作战室群（%s），请通过群链接进入协同排障。", chatID),
	}, nil
}

func fetchFeishuTenantToken(ctx context.Context, client *http.Client, baseURL, appID, appSecret string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("feishu auth request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // body read completely below
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read feishu auth response: %w", err)
	}
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode feishu auth response: %w", err)
	}
	if resp.StatusCode >= 400 || out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu tenant_access_token error (status=%d code=%d): %s", resp.StatusCode, out.Code, out.Msg)
	}
	return out.TenantAccessToken, nil
}

func sendFeishuWarRoomBrief(ctx context.Context, client *http.Client, baseURL, token, chatID string, req *WarRoomCreateRequest) error {
	consoleLink := buildConsoleIncidentURL(req.ConsoleURL, req.IncidentID)
	contentMap := map[string]string{
		"text": fmt.Sprintf("🚨 应急作战室已就绪\n• 事件编号: INC-%04d\n• 告警标题: %s\n• 严重级别: %s\n• 发起人: %s\n• 控制台详情: %s",
			req.IncidentID, req.Title, nonEmptyStr(req.Severity, "critical"), nonEmptyStr(req.Operator, "On-Call"), consoleLink),
	}
	contentJSON, err := json.Marshal(contentMap)
	if err != nil {
		return err
	}
	msgBody, err := json.Marshal(map[string]string{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(contentJSON),
	})
	if err != nil {
		return err
	}
	msgReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/open-apis/im/v1/messages?receive_id_type=chat_id", bytes.NewReader(msgBody))
	if err != nil {
		return err
	}
	msgReq.Header.Set("Authorization", "Bearer "+token)
	msgReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(msgReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // close unused welcome response body
	return nil
}

func createWebhookWarRoom(
	ctx context.Context,
	client *http.Client,
	cfg *ChannelGatewayConfig,
	req *WarRoomCreateRequest,
	roomName, fallbackURL string,
) (*WarRoomCreateResult, error) {
	payloadMap := map[string]any{
		"event":       "warroom.create",
		"incident_id": req.IncidentID,
		"title":       req.Title,
		"severity":    nonEmptyStr(req.Severity, "critical"),
		"rule_name":   req.RuleName,
		"operator":    req.Operator,
		"room_name":   roomName,
		"console_url": fallbackURL,
		"created_at":  time.Now().UTC().Format(time.RFC3339),
	}
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("encode webhook warroom payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(cfg.WarRoomWebhookURL), bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("create webhook warroom request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if cfg.WarRoomWebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.WarRoomWebhookSecret))
		mac.Write(payloadBytes)
		httpReq.Header.Set("X-OpsPilot-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("webhook warroom request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // body read completely below
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read webhook warroom response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webhook warroom HTTP %d: %s", resp.StatusCode, truncateRunes(string(raw), 200))
	}

	var parsed struct {
		WarRoomURL string `json:"war_room_url"`
		URL        string `json:"url"`
		ChatID     string `json:"chat_id"`
		RoomName   string `json:"room_name"`
	}
	_ = json.Unmarshal(raw, &parsed) // optional structured response body; fall back to console URL on empty/plain response

	roomURL := strings.TrimSpace(parsed.WarRoomURL)
	if roomURL == "" {
		roomURL = strings.TrimSpace(parsed.URL)
	}
	if roomURL == "" {
		roomURL = fallbackURL
	}
	finalName := nonEmptyStr(parsed.RoomName, roomName)

	return &WarRoomCreateResult{
		Provider:       "webhook",
		ExternalChatID: strings.TrimSpace(parsed.ChatID),
		RoomName:       finalName,
		RoomURL:        roomURL,
		Message:        fmt.Sprintf("已通过 Webhook 触发创建应急作战室「%s」。", finalName),
	}, nil
}
