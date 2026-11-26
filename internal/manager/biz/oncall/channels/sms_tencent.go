package channels

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

const defaultTencentSMSEndpoint = "https://sms.tencentcloudapi.com"

type tencentSMSResponse struct {
	Response struct {
		SendStatusSet []struct {
			SerialNo string `json:"SerialNo"`
			Code     string `json:"Code"`
			Message  string `json:"Message"`
		} `json:"SendStatusSet"`
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
		RequestId string `json:"RequestId"`
	} `json:"Response"`
}

func sendTencentSMS(ctx context.Context, client *http.Client, endpoint string, cfg *ChannelGatewayConfig, req *SMSRequest) (*SMSResult, error) {
	if cfg.TencentSecretID == "" || cfg.TencentSecretKey == "" {
		return nil, errors.New("腾讯云短信未配置 SecretId 或 SecretKey")
	}
	if cfg.TencentSdkAppID == "" || cfg.TencentSignName == "" || cfg.TencentTemplateID == "" {
		return nil, errors.New("腾讯云短信未配置 SdkAppId、短信签名 (SignName) 或模版 ID (TemplateId)")
	}
	phone := strings.TrimSpace(req.PhoneNumber)
	if phone == "" {
		return nil, errors.New("接收短信的目标手机号不能为空")
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+86" + phone
	}

	region := cfg.TencentRegion
	if region == "" {
		region = "ap-guangzhou"
	}

	payloadMap := map[string]any{
		"PhoneNumberSet":   []string{phone},
		"SmsSdkAppId":      cfg.TencentSdkAppID,
		"SignName":         cfg.TencentSignName,
		"TemplateId":       cfg.TencentTemplateID,
		"TemplateParamSet": []string{nonEmptyStr(req.Urgency, "high"), truncateRunes(req.Title, 40)},
		"SessionContext":   fmt.Sprintf("inc_%d", req.IncidentID),
	}
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("encode tencent sms payload: %w", err)
	}

	const (
		host    = "sms.tencentcloudapi.com"
		service = "sms"
		action  = "SendSms"
		version = "2021-01-11"
	)
	now := time.Now().UTC()
	timestamp := now.Unix()
	date := now.Format("2006-01-02")

	canonicalHeaders := fmt.Sprintf("content-type:application/json; charset=utf-8\nhost:%s\nx-tc-action:%s\n", host, strings.ToLower(action))
	signedHeaders := "content-type;host;x-tc-action"
	payloadDigest := sha256.Sum256(payloadBytes)
	canonicalRequest := fmt.Sprintf("POST\n/\n\n%s\n%s\n%s", canonicalHeaders, signedHeaders, hex.EncodeToString(payloadDigest[:]))
	canonicalDigest := sha256.Sum256([]byte(canonicalRequest))
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := fmt.Sprintf("TC3-HMAC-SHA256\n%d\n%s\n%s", timestamp, credentialScope, hex.EncodeToString(canonicalDigest[:]))

	secretDate := hmacSHA256Bytes([]byte("TC3"+cfg.TencentSecretKey), []byte(date))
	secretService := hmacSHA256Bytes(secretDate, []byte(service))
	secretSigning := hmacSHA256Bytes(secretService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256Bytes(secretSigning, []byte(stringToSign)))

	authHeader := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		cfg.TencentSecretID, credentialScope, signedHeaders, signature)

	if endpoint == "" {
		endpoint = defaultTencentSMSEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, fmt.Errorf("create tencent sms request: %w", err)
	}
	httpReq.Header.Set("Authorization", authHeader)
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Host", host)
	httpReq.Header.Set("X-TC-Action", action)
	httpReq.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-TC-Version", version)
	httpReq.Header.Set("X-TC-Region", region)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tencent sms request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // response body already read or discarded on close

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read tencent sms response: %w", err)
	}

	var parsed tencentSMSResponse
	if jsonErr := json.Unmarshal(bodyBytes, &parsed); jsonErr != nil {
		if resp.StatusCode >= 400 {
			return &SMSResult{
				Provider:    "tencent",
				Status:      "failed",
				PhoneNumber: phone,
				Message:     fmt.Sprintf("腾讯云短信 HTTP %d: %s", resp.StatusCode, truncateRunes(string(bodyBytes), 200)),
			}, fmt.Errorf("tencent sms http %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("invalid tencent sms json response: %w", jsonErr)
	}

	if resp.StatusCode >= 400 || parsed.Response.Error != nil {
		code := fmt.Sprintf("HTTP%d", resp.StatusCode)
		errMsg := string(bodyBytes)
		if parsed.Response.Error != nil {
			code = parsed.Response.Error.Code
			errMsg = parsed.Response.Error.Message
		}
		msg := fmt.Sprintf("腾讯云短信发送失败: [%s] %s", code, errMsg)
		return &SMSResult{
			Provider:    "tencent",
			Status:      "failed",
			RequestID:   parsed.Response.RequestId,
			PhoneNumber: phone,
			Message:     msg,
		}, fmt.Errorf("tencent sms error [%s]: %s", code, errMsg)
	}

	serialNo := ""
	if len(parsed.Response.SendStatusSet) > 0 {
		st := parsed.Response.SendStatusSet[0]
		serialNo = st.SerialNo
		if !strings.EqualFold(st.Code, "Ok") {
			msg := fmt.Sprintf("腾讯云短信下发被拒: [%s] %s", st.Code, st.Message)
			return &SMSResult{
				Provider:    "tencent",
				Status:      "failed",
				RequestID:   parsed.Response.RequestId,
				PhoneNumber: phone,
				Message:     msg,
			}, fmt.Errorf("tencent sms status error [%s]: %s", st.Code, st.Message)
		}
	}

	return &SMSResult{
		Provider:    "tencent",
		Status:      "sent",
		RequestID:   parsed.Response.RequestId,
		BizID:       serialNo,
		PhoneNumber: phone,
		Message:     fmt.Sprintf("已通过腾讯云短信发送成功 (SerialNo: %s)", serialNo),
	}, nil
}

func hmacSHA256Bytes(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
