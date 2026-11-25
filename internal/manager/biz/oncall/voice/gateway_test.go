package voice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func testDualCloudConfig() *VoiceGatewayConfig {
	return &VoiceGatewayConfig{
		Provider:               "aliyun",
		FallbackProvider:       "tencent",
		FailoverTimeoutSeconds: 8,
		AliyunAccessKeyID:      "LTAI5tTest",
		AliyunAccessKeySecret:  "AliyunSecret",
		AliyunTtsCode:          "TTS_999",
		AliyunRegion:           "cn-hangzhou",
		TencentSecretID:        "AKIDTest",
		TencentSecretKey:       "TencentSecret",
		TencentSdkAppID:        "1400001234",
		TencentTemplateID:      "88888",
		TencentRegion:          "ap-guangzhou",
	}
}

func TestVoiceGateway_FailoverAliyunToTencentOnHTTP500(t *testing.T) {
	var aliyunHits, tencentHits int32
	aliyun := &AliyunVoiceProvider{
		client: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				atomic.AddInt32(&aliyunHits, 1)
				return jsonHTTPResponse(http.StatusInternalServerError, `{"Code":"InternalError","Message":"upstream gateway 500"}`), nil
			}),
		},
	}
	tencent := &TencentVoiceProvider{
		client: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				atomic.AddInt32(&tencentHits, 1)
				return jsonHTTPResponse(http.StatusOK, `{"Response":{"SendStatus":{"CallId":"tx_call_777"},"RequestId":"req_1"}}`), nil
			}),
		},
	}

	vg := &VoiceGateway{providers: make(map[string]VoiceProvider)}
	vg.RegisterProvider(NewMockVoiceProvider())
	vg.RegisterProvider(aliyun)
	vg.RegisterProvider(tencent)

	res, err := vg.SendVoiceCall(context.Background(), testDualCloudConfig(), &CallRequest{
		CallID:      "call_failover_1",
		PhoneNumber: "13800000001",
		Urgency:     "high",
		Title:       "Core DB down",
	})
	if err != nil {
		t.Fatalf("expected successful failover, got error: %v", err)
	}
	if atomic.LoadInt32(&aliyunHits) != 1 || atomic.LoadInt32(&tencentHits) != 1 {
		t.Fatalf("expected 1 hit each on aliyun/tencent, got %d/%d", aliyunHits, tencentHits)
	}
	if res.Provider != "tencent" || res.OutCallID != "tx_call_777" {
		t.Fatalf("expected tencent result, got %+v", res)
	}
	if res.FallbackFrom != "aliyun" || res.PrimaryError == "" || len(res.Attempts) != 2 {
		t.Fatalf("expected dual attempt metadata on failover, got %+v", res)
	}
}

func TestVoiceGateway_NetworkTimeoutTriggersFailoverButCallerCancelDoesNot(t *testing.T) {
	var tencentHits int32
	aliyun := &AliyunVoiceProvider{
		client: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return nil, &url.Error{Op: "Post", URL: "https://dyvmsapi.aliyuncs.com", Err: context.DeadlineExceeded}
			}),
		},
	}
	tencent := &TencentVoiceProvider{
		client: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				atomic.AddInt32(&tencentHits, 1)
				return jsonHTTPResponse(http.StatusOK, `{"Response":{"SendStatus":{"CallId":"tx_timeout_ok"},"RequestId":"req_2"}}`), nil
			}),
		},
	}

	vg := &VoiceGateway{providers: make(map[string]VoiceProvider)}
	vg.RegisterProvider(aliyun)
	vg.RegisterProvider(tencent)

	// 1. Provider HTTP timeout with live parent context triggers failover cleanly.
	res, err := vg.SendVoiceCall(context.Background(), testDualCloudConfig(), &CallRequest{
		CallID:      "call_timeout_ok",
		PhoneNumber: "13800000001",
		Urgency:     "high",
		Title:       "Redis timeout",
	})
	if err != nil || res.Provider != "tencent" || atomic.LoadInt32(&tencentHits) != 1 {
		t.Fatalf("expected failover on provider HTTP timeout, got res=%+v err=%v hits=%d", res, err, tencentHits)
	}

	// 2. Cancelled caller context must NOT attempt backup provider.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errCancelled := vg.SendVoiceCall(cancelledCtx, testDualCloudConfig(), &CallRequest{
		CallID:      "call_cancelled",
		PhoneNumber: "13800000001",
	})
	if errCancelled == nil || atomic.LoadInt32(&tencentHits) != 1 {
		t.Fatalf("expected no backup call after parent context cancellation, hits=%d err=%v", tencentHits, errCancelled)
	}
}

func TestVoiceGateway_NoFailoverGuards(t *testing.T) {
	var tencentHits int32
	aliyun := &AliyunVoiceProvider{
		client: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return nil, errors.New("unexpected aliyun call")
			}),
		},
	}
	tencent := &TencentVoiceProvider{
		client: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				atomic.AddInt32(&tencentHits, 1)
				return jsonHTTPResponse(http.StatusOK, `{}`), nil
			}),
		},
	}

	vg := &VoiceGateway{providers: make(map[string]VoiceProvider)}
	vg.RegisterProvider(NewMockVoiceProvider())
	vg.RegisterProvider(aliyun)
	vg.RegisterProvider(tencent)

	// Missing phone number must NOT fail over.
	cfgMissingPhone := testDualCloudConfig()
	_, err := vg.SendVoiceCall(context.Background(), cfgMissingPhone, &CallRequest{CallID: "c1", PhoneNumber: ""})
	if err == nil || atomic.LoadInt32(&tencentHits) != 0 {
		t.Fatalf("expected no failover on missing phone number, got err=%v hits=%d", err, tencentHits)
	}

	// Failover to "mock" must be blocked even if configured manually.
	cfgMockFallback := testDualCloudConfig()
	cfgMockFallback.FallbackProvider = "mock"
	res, err := vg.SendVoiceCall(context.Background(), cfgMockFallback, &CallRequest{CallID: "c2", PhoneNumber: "13800000001"})
	if err == nil || (res != nil && res.Provider == "mock") {
		t.Fatalf("expected failover to mock to be rejected, got res=%+v err=%v", res, err)
	}

	// Same provider as fallback must be rejected.
	cfgSame := testDualCloudConfig()
	cfgSame.FallbackProvider = "aliyun"
	if vg.canFailover(context.Background(), cfgSame, &CallRequest{PhoneNumber: "13800000001"}, "aliyun", "aliyun") {
		t.Fatal("expected canFailover=false when fallback == primary")
	}

	// Missing backup credentials must block failover cleanly.
	cfgNoCred := testDualCloudConfig()
	cfgNoCred.TencentSecretKey = ""
	if vg.canFailover(context.Background(), cfgNoCred, &CallRequest{PhoneNumber: "13800000001"}, "aliyun", "tencent") {
		t.Fatal("expected canFailover=false when backup credentials are incomplete")
	}
	if !strings.Contains(NewTencentVoiceProvider().Name(), "tencent") {
		t.Fatal("sanity check")
	}
}
