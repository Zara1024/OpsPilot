package channels

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultAliyunSMSEndpoint = "https://dysmsapi.aliyuncs.com"

type aliyunSMSResponse struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	BizId     string `json:"BizId"`
	RequestId string `json:"RequestId"`
}

func sendAliyunSMS(ctx context.Context, client *http.Client, endpoint string, cfg *ChannelGatewayConfig, req *SMSRequest) (*SMSResult, error) {
	if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
		return nil, errors.New("阿里云短信未配置 AccessKey ID 或 AccessKey Secret")
	}
	if cfg.AliyunSignName == "" || cfg.AliyunTemplateCode == "" {
		return nil, errors.New("阿里云短信未配置短信签名 (SignName) 或模版代码 (TemplateCode)")
	}
	if strings.TrimSpace(req.PhoneNumber) == "" {
		return nil, errors.New("接收短信的目标手机号不能为空")
	}

	region := cfg.AliyunRegion
	if region == "" {
		region = "cn-hangzhou"
	}
	tplParamJSON, err := json.Marshal(map[string]string{
		"urgency": nonEmptyStr(req.Urgency, "high"),
		"title":   truncateRunes(req.Title, 40),
	})
	if err != nil {
		return nil, fmt.Errorf("encode aliyun sms params: %w", err)
	}

	params := url.Values{}
	params.Set("Action", "SendSms")
	params.Set("Version", "2017-05-25")
	params.Set("Format", "JSON")
	params.Set("RegionId", region)
	params.Set("AccessKeyId", cfg.AliyunAccessKeyID)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", fmt.Sprintf("%d-%d", time.Now().UnixNano(), req.IncidentID))
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("PhoneNumbers", strings.TrimSpace(req.PhoneNumber))
	params.Set("SignName", cfg.AliyunSignName)
	params.Set("TemplateCode", cfg.AliyunTemplateCode)
	params.Set("TemplateParam", string(tplParamJSON))
	params.Set("Signature", signAliyunPOP("POST", params, cfg.AliyunAccessKeySecret))

	if endpoint == "" {
		endpoint = defaultAliyunSMSEndpoint
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create aliyun sms request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("aliyun sms request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // response body already read or discarded on close

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read aliyun sms response: %w", err)
	}

	var parsed aliyunSMSResponse
	if jsonErr := json.Unmarshal(bodyBytes, &parsed); jsonErr != nil {
		if resp.StatusCode >= 400 {
			return &SMSResult{
				Provider:    "aliyun",
				Status:      "failed",
				PhoneNumber: req.PhoneNumber,
				Message:     fmt.Sprintf("阿里云短信服务 HTTP %d: %s", resp.StatusCode, truncateRunes(string(bodyBytes), 200)),
			}, fmt.Errorf("aliyun sms http %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("invalid aliyun sms json response: %w", jsonErr)
	}

	if resp.StatusCode >= 400 || parsed.Code != "OK" {
		msg := fmt.Sprintf("阿里云短信发送失败: [%s] %s", parsed.Code, parsed.Message)
		return &SMSResult{
			Provider:    "aliyun",
			Status:      "failed",
			RequestID:   parsed.RequestId,
			PhoneNumber: req.PhoneNumber,
			Message:     msg,
		}, fmt.Errorf("aliyun sms error [%s]: %s", parsed.Code, parsed.Message)
	}

	return &SMSResult{
		Provider:    "aliyun",
		Status:      "sent",
		RequestID:   parsed.RequestId,
		BizID:       parsed.BizId,
		PhoneNumber: req.PhoneNumber,
		Message:     fmt.Sprintf("已通过阿里云短信发送成功 (BizId: %s)", parsed.BizId),
	}, nil
}

func signAliyunPOP(method string, params url.Values, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	canonicalized := make([]string, 0, len(keys))
	for _, k := range keys {
		canonicalized = append(canonicalized, aliyunPercentEncode(k)+"="+aliyunPercentEncode(params.Get(k)))
	}
	stringToSign := method + "&" + aliyunPercentEncode("/") + "&" + aliyunPercentEncode(strings.Join(canonicalized, "&"))
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func aliyunPercentEncode(s string) string {
	res := url.QueryEscape(s)
	res = strings.ReplaceAll(res, "+", "%20")
	res = strings.ReplaceAll(res, "*", "%2A")
	res = strings.ReplaceAll(res, "%7E", "~")
	return res
}
