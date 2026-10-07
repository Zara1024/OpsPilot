package voice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TencentVoiceProvider struct {
	client *http.Client
}

func NewTencentVoiceProvider() *TencentVoiceProvider {
	return &TencentVoiceProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TencentVoiceProvider) Name() string {
	return "tencent"
}

type tencentSendTtsResponse struct {
	Response struct {
		SendStatus struct {
			CallId         string `json:"CallId"`
			SessionContext string `json:"SessionContext"`
		} `json:"SendStatus"`
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
		RequestId string `json:"RequestId"`
	} `json:"Response"`
}

func (t *TencentVoiceProvider) SendVoiceCall(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest) (*CallResult, error) {
	if cfg.TencentSecretID == "" || cfg.TencentSecretKey == "" {
		return nil, errors.New("腾讯云语音服务未配置 SecretId 或 SecretKey")
	}
	if cfg.TencentSdkAppID == "" {
		return nil, errors.New("未配置腾讯云语音应用 SdkAppId")
	}
	if cfg.TencentTemplateID == "" {
		return nil, errors.New("未配置腾讯云语音模版 TemplateId")
	}
	if req.PhoneNumber == "" {
		return nil, errors.New("呼叫目标手机号不能为空")
	}

	region := cfg.TencentRegion
	if region == "" {
		region = "ap-guangzhou"
	}

	phone := req.PhoneNumber
	if !strings.HasPrefix(phone, "+") {
		phone = "+86" + phone
	}

	// Prepare template params
	paramSet := []string{req.Urgency, req.Title}
	if req.Params != nil {
		if p1, ok := req.Params["param1"]; ok {
			paramSet = []string{p1}
			if p2, ok2 := req.Params["param2"]; ok2 {
				paramSet = append(paramSet, p2)
			}
		}
	}

	payloadMap := map[string]any{
		"TemplateId":       cfg.TencentTemplateID,
		"TemplateParamSet": paramSet,
		"CalledNumber":     phone,
		"VoiceSdkAppid":    cfg.TencentSdkAppID,
		"SessionContext":   req.CallID,
	}
	if cfg.TencentCalledShowNumber != "" {
		payloadMap["CalledShowNumber"] = cfg.TencentCalledShowNumber
	}

	payloadBytes, _ := json.Marshal(payloadMap)

	// TC3-HMAC-SHA256 Signature
	host := "vms.tencentcloudapi.com"
	service := "vms"
	action := "SendTtsVoice"
	version := "2020-09-02"
	now := time.Now().UTC()
	timestamp := now.Unix()
	date := now.Format("2006-01-02")

	canonicalHeaders := fmt.Sprintf("content-type:application/json; charset=utf-8\nhost:%s\nx-tc-action:%s\n", host, strings.ToLower(action))
	signedHeaders := "content-type;host;x-tc-action"

	hPayload := sha256.Sum256(payloadBytes)
	hashedPayload := hex.EncodeToString(hPayload[:])

	canonicalRequest := fmt.Sprintf("POST\n/\n\n%s\n%s\n%s", canonicalHeaders, signedHeaders, hashedPayload)
	hCanonical := sha256.Sum256([]byte(canonicalRequest))
	hashedCanonicalRequest := hex.EncodeToString(hCanonical[:])

	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := fmt.Sprintf("TC3-HMAC-SHA256\n%d\n%s\n%s", timestamp, credentialScope, hashedCanonicalRequest)

	secretDate := hmacSHA256([]byte("TC3"+cfg.TencentSecretKey), []byte(date))
	secretService := hmacSHA256(secretDate, []byte(service))
	secretSigning := hmacSHA256(secretService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(secretSigning, []byte(stringToSign)))

	authHeader := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		cfg.TencentSecretID, credentialScope, signedHeaders, signature)

	endpoint := "https://" + host
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create tencent request: %w", err)
	}

	httpReq.Header.Set("Authorization", authHeader)
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Host", host)
	httpReq.Header.Set("X-TC-Action", action)
	httpReq.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-TC-Version", version)
	httpReq.Header.Set("X-TC-Region", region)

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tencent vms request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tencent response: %w", err)
	}

	var res tencentSendTtsResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("invalid tencent json response: %s", string(bodyBytes))
	}

	if res.Response.Error != nil {
		return &CallResult{
			CallID:       req.CallID,
			Provider:     "tencent",
			Status:       "failed",
			PhoneNumber:  phone,
			Message:      fmt.Sprintf("腾讯云语音外呼失败: [%s] %s", res.Response.Error.Code, res.Response.Error.Message),
			RawResponse:  string(bodyBytes),
		}, fmt.Errorf("tencent vms error: [%s] %s", res.Response.Error.Code, res.Response.Error.Message)
	}

	callID := res.Response.SendStatus.CallId
	return &CallResult{
		CallID:       req.CallID,
		Provider:     "tencent",
		Status:       "calling",
		OutCallID:    callID,
		PhoneNumber:  phone,
		TTSContent:   fmt.Sprintf("模版 [%s]，参数: %v", cfg.TencentTemplateID, paramSet),
		Message:      fmt.Sprintf("已成功通过腾讯云语音服务发起外呼 (CallId: %s)", callID),
		RawResponse:  string(bodyBytes),
	}, nil
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
