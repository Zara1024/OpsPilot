package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type reasoningRoundTripFunc func(*http.Request) (*http.Response, error)

func (f reasoningRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type reasoningTrackedBody struct {
	io.Reader
	closed   bool
	readErr  error
	closeErr error
}

func (b *reasoningTrackedBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.Reader.Read(p)
}

func (b *reasoningTrackedBody) Close() error {
	b.closed = true
	return b.closeErr
}

func TestReasoningTransport_ClosesOriginalBodyOnSuccessAndFailure(t *testing.T) {
	sentinel := errors.New("synthetic IO failure")
	for _, scenario := range []string{"success", "no replay", "copy error", "read error", "close error", "invalid json", "invalid messages", "upstream error", "original close error"} {
		t.Run(scenario, func(t *testing.T) {
			const payload = `{"messages":[{"role":"assistant","content":"old"}],"model":"deepseek-v4-flash"}`
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader(payload))
			require.NoError(t, err)
			req = req.WithContext(context.WithValue(req.Context(), deepSeekReasoningKey{}, true))
			original := &reasoningTrackedBody{Reader: strings.NewReader(payload)}
			copyBody := &reasoningTrackedBody{Reader: strings.NewReader(payload)}
			req.Body = original
			req.GetBody = func() (io.ReadCloser, error) { return copyBody, nil }
			switch scenario {
			case "no replay":
				req.GetBody = nil
			case "copy error":
				req.GetBody = func() (io.ReadCloser, error) { return nil, sentinel }
			case "read error":
				copyBody.readErr = sentinel
			case "close error":
				copyBody.closeErr = sentinel
			case "invalid json":
				copyBody.Reader = strings.NewReader("{")
			case "invalid messages":
				copyBody.Reader = strings.NewReader(`{"messages":1}`)
			case "original close error":
				original.closeErr = sentinel
				req.GetBody = nil
			}
			called := false
			transport := &deepSeekReasoningTransport{base: reasoningRoundTripFunc(func(sent *http.Request) (*http.Response, error) {
				called = true
				require.NotSame(t, req, sent)
				require.Same(t, original, req.Body)
				require.Equal(t, req.Context(), sent.Context())
				data, readErr := io.ReadAll(sent.Body)
				require.NoError(t, readErr)
				require.NoError(t, sent.Body.Close())
				require.Contains(t, string(data), `"reasoning_content":""`)
				require.EqualValues(t, len(data), sent.ContentLength)
				replay, replayErr := sent.GetBody()
				require.NoError(t, replayErr)
				replayData, replayErr := io.ReadAll(replay)
				require.NoError(t, replayErr)
				require.NoError(t, replay.Close())
				require.Equal(t, data, replayData)
				if scenario == "upstream error" {
					return nil, sentinel
				}
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			})}
			_, err = transport.RoundTrip(req)
			require.True(t, original.closed)
			require.Equal(t, scenario == "success" || scenario == "upstream error", called)
			if scenario == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			switch scenario {
			case "copy error", "read error", "close error", "upstream error", "original close error":
				require.ErrorIs(t, err, sentinel)
			}
			if scenario != "no replay" && scenario != "copy error" && scenario != "original close error" {
				require.True(t, copyBody.closed)
			}
		})
	}
}

func FuzzReasoningTransport_DoesNotPanicOrLeakBody(f *testing.F) {
	for _, seed := range []string{
		`{"messages":[{"role":"assistant","content":"hello"}]}`,
		`{"messages":[{"role":"assistant","reasoning_content":"原文"}]}`,
		`{"messages":null}`, `{"messages":1}`, `null`, `{`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload string) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", strings.NewReader(payload))
		require.NoError(t, err)
		req = req.WithContext(context.WithValue(req.Context(), deepSeekReasoningKey{}, true))
		body := &reasoningTrackedBody{Reader: strings.NewReader(payload)}
		req.Body = body
		transport := &deepSeekReasoningTransport{base: reasoningRoundTripFunc(func(sent *http.Request) (*http.Response, error) {
			require.NoError(t, sent.Body.Close())
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})}
		resp, err := transport.RoundTrip(req)
		// 任意输入可以被拒绝；成功响应必须有效，两种路径均须关闭原请求体。
		if err == nil {
			require.NotNil(t, resp)
		}
		require.True(t, body.closed)
	})
}
