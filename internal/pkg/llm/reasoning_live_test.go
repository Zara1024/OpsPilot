package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// 显式启用的真实上游冒烟；只使用合成测试工具，不访问设备或业务数据。
func TestReasoningLive_LegacyHistoryThenToolContinuation(t *testing.T) {
	if os.Getenv("OPSPILOT_REASONING_SMOKE") != "1" {
		t.Skip("opt-in live provider smoke")
	}
	client := New(Config{APIKey: os.Getenv("SMOKE_API_KEY"), BaseURL: os.Getenv("SMOKE_BASE_URL"), Model: os.Getenv("SMOKE_MODEL"), TLSInsecure: os.Getenv("SMOKE_TLS_INSECURE") == "true"}, nil, prometheus.NewRegistry())
	cm, err := NewClientChatModel(ClientChatModelConfig{Client: client})
	require.NoError(t, err)
	require.NoError(t, cm.BindTools([]*schema.ToolInfo{{Name: "test_value", Desc: "Returns a synthetic test value. Call once when asked to read the test value."}}))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	history := []*schema.Message{{Role: schema.User, Content: "Hello"}, {Role: schema.Assistant, Content: "Hello."}, {Role: schema.User, Content: "Call test_value once now, then report its value briefly. This is a synthetic protocol test."}}
	msg, err := cm.Generate(ctx, history)
	require.NoError(t, err)
	require.NotEmpty(t, msg.ReasoningContent)
	require.Len(t, msg.ToolCalls, 1)
	require.Equal(t, "test_value", msg.ToolCalls[0].Function.Name)
	history = append(history, msg, &schema.Message{Role: schema.Tool, ToolCallID: msg.ToolCalls[0].ID, Content: `{"value":7}`})
	msg, err = cm.Generate(ctx, history)
	require.NoError(t, err)
	require.Empty(t, msg.ToolCalls)
	require.Contains(t, msg.Content, "7")
	history = append(history, msg, &schema.Message{Role: schema.User, Content: "What was the synthetic value? Answer briefly from history; do not call tools."})
	msg, err = cm.Generate(ctx, history)
	require.NoError(t, err)
	require.Empty(t, msg.ToolCalls)
	require.Contains(t, msg.Content, "7")
	t.Log("legacy history, tool continuation, and next user turn succeeded")
}
