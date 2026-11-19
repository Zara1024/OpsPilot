package slack

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/imbridge"
)

// A Socket Mode socket is idle most of the time, which parks Run inside
// conn.ReadMessage. ReadMessage takes no context, so cancelling alone cannot
// unblock it: Run never returns, slot.done never closes, and the supervisor is
// forced to abandon the goroutine (and its WebSocket) after the stop timeout —
// the same failure mode described in #517.
func TestStreamClient_RunReturnsWhenContextCanceledWhileIdle(t *testing.T) {
	var upgrader websocket.Upgrader
	connected := make(chan struct{}) // server saw the upgrade
	sawClose := make(chan struct{})  // server saw the socket close
	testDone := make(chan struct{})  // unblocks the handler once the test ends

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps.connections.open":
			wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/link/?ticket=test"
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": wsURL})
		case "/link/":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			close(connected)
			// Keep srv.Close() from waiting on a handler that is still parked if
			// the client never closes the socket.
			go func() {
				<-testDone
				_ = conn.Close()
			}()
			// Stay deliberately silent: this is the idle socket where the
			// client's ReadMessage parks.
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					close(sawClose)
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer close(testDone)

	client := NewClient("xapp-test", "xoxb-test")
	client.SetBaseURL(srv.URL) // test seam, see client.go
	sc := &StreamClient{
		app:     &model.ImApp{ID: 1, Provider: model.ProviderSlack},
		client:  client,
		allowed: map[string]struct{}{},
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sc.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("client never dialed the websocket")
	}
	// Let the client park in ReadMessage: cancelling earlier would exercise the
	// loop's context check instead of the bug under test.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation: the idle ReadMessage never unblocked")
	}
	select {
	case <-sawClose:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not close the websocket")
	}
}
