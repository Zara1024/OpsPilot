package voice

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

type AliyunVoiceProvider struct {
	client *http.Client
}

func NewAliyunVoiceProvider() *AliyunVoiceProvider {
	return &AliyunVoiceProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *AliyunVoiceProvider) Name() string {
	return "aliyun"
}

type aliyunSingleCallResponse struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	CallId    string `json:"CallId"`
	RequestId string `json:"RequestId"`
}

func (a *AliyunVoiceProvider) SendVoiceCall(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest) (*CallResult, error) {
	if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
		return nil, errors.New("阿里云语音网关未配置 AccessKey ID 或 AccessKey Secret")
	}
	if req.PhoneNumber == "" {
		return nil, errors.New("呼叫目标手机号不能为空")
	}
	if cfg.AliyunTtsCode == "" {
		return nil, errors.New("未配置阿里云 TTS 模版代码 (TtsCode)")
	}

	region := cfg.AliyunRegion
	if region == "" {
		region = "cn-hangzhou"
	}

	// Prepare TTS parameters
	ttsParamMap := make(map[string]string)
	if req.Params != nil {
		for k, v := range req.Params {
			ttsParamMap[k] = v
		}
	}
	if _, ok := ttsParamMap["urgency"]; !ok {
		ttsParamMap["urgency"] = req.Urgency
	}
	if _, ok := ttsParamMap["title"]; !ok {
		ttsParamMap["title"] = req.Title
	}
	ttsParamJSON, _ := json.Marshal(ttsParamMap)

	// Build query parameters for POP signature
	params := url.Values{}
	params.Set("Action", "SingleCallByTts")
	params.Set("Version", "2017-05-25")
	params.Set("Format", "JSON")
	params.Set("RegionId", region)
	params.Set("AccessKeyId", cfg.AliyunAccessKeyID)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", fmt.Sprintf("%d-%s", time.Now().UnixNano(), req.CallID))
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("CalledNumber", req.PhoneNumber)
	params.Set("TtsCode", cfg.AliyunTtsCode)
	params.Set("TtsParam", string(ttsParamJSON))

	if cfg.AliyunCalledShowNumber != "" {
		params.Set("CalledShowNumber", cfg.AliyunCalledShowNumber)
	}

	signature := a.sign("POST", params, cfg.AliyunAccessKeySecret)
	params.Set("Signature", signature)

	endpoint := "https://dyvmsapi.aliyuncs.com"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create aliyun request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("aliyun dyvmsapi request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read aliyun response: %w", err)
	}

	var res aliyunSingleCallResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("invalid aliyun json response: %s", string(bodyBytes))
	}

	if res.Code != "OK" {
		return &CallResult{
			CallID:       req.CallID,
			Provider:     "aliyun",
			Status:       "failed",
			PhoneNumber:  req.PhoneNumber,
			Message:      fmt.Sprintf("阿里云外呼失败: [%s] %s", res.Code, res.Message),
			RawResponse:  string(bodyBytes),
		}, fmt.Errorf("aliyun dyvmsapi error: [%s] %s", res.Code, res.Message)
	}

	return &CallResult{
		CallID:       req.CallID,
		Provider:     "aliyun",
		Status:       "calling",
		OutCallID:    res.CallId,
		PhoneNumber:  req.PhoneNumber,
		TTSContent:   fmt.Sprintf("TTS 模版 [%s]，参数: %s", cfg.AliyunTtsCode, string(ttsParamJSON)),
		Message:      fmt.Sprintf("已成功通过阿里云语音服务发起外呼 (CallId: %s)", res.CallId),
		RawResponse:  string(bodyBytes),
	}, nil
}

func (a *AliyunVoiceProvider) sign(method string, params url.Values, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalized []string
	for _, k := range keys {
		v := params.Get(k)
		canonicalized = append(canonicalized, aliyunSpecialUrlEncode(k)+"="+aliyunSpecialUrlEncode(v))
	}
	canonicalQuery := strings.Join(canonicalized, "&")

	stringToSign := method + "&" + aliyunSpecialUrlEncode("/") + "&" + aliyunSpecialUrlEncode(canonicalQuery)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func aliyunSpecialUrlEncode(s string) string {
	res := url.QueryEscape(s)
	res = strings.ReplaceAll(res, "+", "%20")
	res = strings.ReplaceAll(res, "*", "%2A")
	res = strings.ReplaceAll(res, "%7E", "~")
	return res
}
