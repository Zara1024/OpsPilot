package voice

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	defaultFailoverTimeoutSecs = 8
	minFailoverTimeoutSecs     = 3
	maxFailoverTimeoutSecs     = 30
)

type VoiceGateway struct {
	providers map[string]VoiceProvider
}

func NewVoiceGateway() *VoiceGateway {
	vg := &VoiceGateway{
		providers: make(map[string]VoiceProvider),
	}
	vg.RegisterProvider(NewMockVoiceProvider())
	vg.RegisterProvider(NewAliyunVoiceProvider())
	vg.RegisterProvider(NewTencentVoiceProvider())
	return vg
}

func (vg *VoiceGateway) RegisterProvider(p VoiceProvider) {
	vg.providers[p.Name()] = p
}

// NormalizeFailoverTimeoutSeconds clamps per-provider call timeout within [3, 30] seconds (default 8s).
func NormalizeFailoverTimeoutSeconds(secs int) int {
	if secs <= 0 {
		return defaultFailoverTimeoutSecs
	}
	if secs < minFailoverTimeoutSecs {
		return minFailoverTimeoutSecs
	}
	if secs > maxFailoverTimeoutSecs {
		return maxFailoverTimeoutSecs
	}
	return secs
}

func (vg *VoiceGateway) SendVoiceCall(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest) (*CallResult, error) {
	if req == nil {
		return nil, fmt.Errorf("call request is nil")
	}

	primaryName := "mock"
	if cfg != nil && cfg.Provider != "" {
		primaryName = cfg.Provider
	}
	if req.ForceMock {
		primaryName = "mock"
	}

	provider, ok := vg.providers[primaryName]
	if !ok {
		provider = vg.providers["mock"]
		primaryName = "mock"
	}
	if provider == nil {
		return nil, fmt.Errorf("voice provider %s not found and mock is unavailable", primaryName)
	}

	if primaryName == "mock" || req.ForceMock {
		return provider.SendVoiceCall(ctx, cfg, req)
	}

	timeout := time.Duration(NormalizeFailoverTimeoutSeconds(cfg.FailoverTimeoutSeconds)) * time.Second
	pCtx, pCancel := context.WithTimeout(ctx, timeout)
	primaryRes, primaryErr := provider.SendVoiceCall(pCtx, cfg, req)
	pCancel()
	if primaryErr == nil {
		return primaryRes, nil
	}

	primaryMsg := primaryErr.Error()
	if primaryRes != nil && primaryRes.Message != "" {
		primaryMsg = primaryRes.Message
	}
	primaryAttempt := CallAttempt{
		Provider: primaryName,
		Status:   "failed",
		Message:  primaryMsg,
	}

	fallbackName := strings.TrimSpace(cfg.FallbackProvider)
	if !vg.canFailover(ctx, cfg, req, primaryName, fallbackName) {
		return primaryRes, primaryErr
	}

	fallbackProvider := vg.providers[fallbackName]
	fCtx, fCancel := context.WithTimeout(ctx, timeout)
	fbRes, fbErr := fallbackProvider.SendVoiceCall(fCtx, cfg, req)
	fCancel()

	if fbErr == nil && fbRes != nil {
		attempts := []CallAttempt{
			primaryAttempt,
			{
				Provider:  fbRes.Provider,
				Status:    fbRes.Status,
				OutCallID: fbRes.OutCallID,
				Message:   fbRes.Message,
			},
		}
		fbRes.FallbackFrom = primaryName
		fbRes.PrimaryError = primaryMsg
		fbRes.Attempts = attempts
		fbRes.Message = fmt.Sprintf("[主通道 %s 失败，已自动容灾切换至 %s] %s", primaryName, fbRes.Provider, fbRes.Message)
		return fbRes, nil
	}

	fbMsg := ""
	if fbErr != nil {
		fbMsg = fbErr.Error()
	}
	if fbRes != nil && fbRes.Message != "" {
		fbMsg = fbRes.Message
	}
	attempts := []CallAttempt{
		primaryAttempt,
		{
			Provider: fallbackName,
			Status:   "failed",
			Message:  fbMsg,
		},
	}
	combined := &CallResult{
		CallID:       req.CallID,
		Provider:     fallbackName,
		Status:       "failed",
		PhoneNumber:  req.PhoneNumber,
		FallbackFrom: primaryName,
		PrimaryError: primaryMsg,
		Attempts:     attempts,
		Message:      fmt.Sprintf("主通道 %s 失败 (%s)；备用通道 %s 亦失败 (%s)", primaryName, primaryMsg, fallbackName, fbMsg),
	}
	return combined, fmt.Errorf("primary %s failed (%w); fallback %s failed: %s", primaryName, primaryErr, fallbackName, fbMsg)
}

func (vg *VoiceGateway) canFailover(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest, primaryName, fallbackName string) bool {
	if ctx.Err() != nil {
		return false
	}
	if strings.TrimSpace(req.PhoneNumber) == "" {
		return false
	}
	if cfg == nil || fallbackName == "" || fallbackName == "mock" || fallbackName == primaryName {
		return false
	}
	if _, exists := vg.providers[fallbackName]; !exists {
		return false
	}
	return hasProviderCredentials(cfg, fallbackName)
}

func hasProviderCredentials(cfg *VoiceGatewayConfig, provider string) bool {
	if cfg == nil {
		return false
	}
	switch provider {
	case "aliyun":
		return cfg.AliyunAccessKeyID != "" && cfg.AliyunAccessKeySecret != "" && cfg.AliyunTtsCode != ""
	case "tencent":
		return cfg.TencentSecretID != "" && cfg.TencentSecretKey != "" && cfg.TencentSdkAppID != "" && cfg.TencentTemplateID != ""
	default:
		return false
	}
}
