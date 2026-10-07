package voice

import (
	"context"
	"fmt"
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

func (vg *VoiceGateway) SendVoiceCall(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest) (*CallResult, error) {
	providerName := "mock"
	if cfg != nil && cfg.Provider != "" {
		providerName = cfg.Provider
	}
	if req.ForceMock {
		providerName = "mock"
	}

	provider, ok := vg.providers[providerName]
	if !ok {
		provider = vg.providers["mock"]
	}
	if provider == nil {
		return nil, fmt.Errorf("voice provider %s not found and mock is unavailable", providerName)
	}

	return provider.SendVoiceCall(ctx, cfg, req)
}
