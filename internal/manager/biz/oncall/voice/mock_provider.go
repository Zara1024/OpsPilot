package voice

import (
	"context"
	"fmt"
	"time"
)

type MockVoiceProvider struct{}

func NewMockVoiceProvider() *MockVoiceProvider {
	return &MockVoiceProvider{}
}

func (m *MockVoiceProvider) Name() string {
	return "mock"
}

func (m *MockVoiceProvider) SendVoiceCall(ctx context.Context, cfg *VoiceGatewayConfig, req *CallRequest) (*CallResult, error) {
	callID := req.CallID
	if callID == "" {
		callID = fmt.Sprintf("mock_call_%d", time.Now().UnixNano())
	}
	phone := req.PhoneNumber
	if phone == "" {
		phone = "13800000000"
	}
	urgency := req.Urgency
	if urgency == "" {
		urgency = "high"
	}

	return &CallResult{
		CallID:       callID,
		Provider:     "mock",
		Status:       "dtmf_confirmed",
		OutCallID:    fmt.Sprintf("mock_out_%d", time.Now().Unix()),
		PhoneNumber:  phone,
		TTSContent:   fmt.Sprintf("【OpsPilot值班生命线】您好，生产环境发生 %s 紧急告警：%s，请在滴声后按 1 确认接单，按 2 升级到二线专家。", urgency, req.Title),
		DTMFCode:     "1",
		DurationSecs: 18,
		Message:      "沙箱模拟呼叫成功，模拟用户已按键 1 (Ack) 确认认领告警。",
	}, nil
}
