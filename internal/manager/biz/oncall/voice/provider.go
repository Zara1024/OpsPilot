package voice

import "context"

// VoiceProvider defines the contract for sending voice alert calls.
type VoiceProvider interface {
	Name() string
	SendVoiceCall(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest) (*CallResult, error)
}
