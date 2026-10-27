package imbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/imbridge"
)

// StreamClient is the interface every per-provider long-connection
// client implements. Run blocks until the supplied context is
// cancelled or a fatal error occurs (the supervisor retries with
// backoff). ProviderName is used in logs.
type StreamClient interface {
	ProviderName() string
	Run(ctx context.Context) error
}

// StreamClientFactory builds a StreamClient for one ImApp row. The
// supervisor calls this when it observes a new (enabled, stream-mode)
// app, and lazily replaces the client when credentials rotate
// (handled by stopping + reconstructing on app row update).
type StreamClientFactory func(app *model.ImApp, bridge *Bridge) (StreamClient, error)

// StreamRepo is the narrow data-layer surface the supervisor needs —
// just listing enabled stream apps periodically. Decoupled from
// imbridge.Repo so the supervisor doesn't drag in the full CRUD.
type StreamRepo interface {
	ListEnabledStreamApps(ctx context.Context) ([]*model.ImApp, error)
}

const streamStopTimeout = 10 * time.Second

// connectGrace is how long one attempt must outlive before it is recorded as
// running. StreamClient has no "connected" callback — Run blocks for as long as
// the long connection is healthy — so surviving the grace window is the only
// observable proxy for "it came up". See runClientWithRestart.
const connectGrace = 5 * time.Second

// StreamSupervisor owns one long-running goroutine per
// (enabled, stream-mode) ImApp. On a reconcile interval (default 30s)
// it diffs DB state vs. running clients:
//   - new app row appears   → spawn client
//   - existing app disabled → cancel client
//   - secret rotated        → cancel then respawn
//
// Lifecycle of an individual client: it lives inside a context whose
// parent is the supervisor's own context. Cancelling the supervisor
// (manager shutdown) propagates a graceful Stop to every client.
//
// Every transition it observes is mirrored into a StreamHealthStore so an
// operator can see whether a client is attached at all — the credentials
// probe on Settings → Channels cannot answer that.
type StreamSupervisor struct {
	repo      StreamRepo
	bridge    *Bridge
	health    *StreamHealthStore
	factories map[string]StreamClientFactory // provider -> factory
	log       *slog.Logger
	mu        sync.Mutex
	running   map[uint64]*streamSlot // im_app.id -> running slot
	reconcile time.Duration
}

type streamSlot struct {
	app    *model.ImApp // Immutable snapshot for this client's lifetime.
	cancel context.CancelFunc
	done   chan struct{}
}

func NewStreamSupervisor(repo StreamRepo, bridge *Bridge, health *StreamHealthStore, log *slog.Logger) *StreamSupervisor {
	if log == nil {
		log = slog.Default()
	}
	return &StreamSupervisor{
		repo:      repo,
		bridge:    bridge,
		health:    health,
		factories: map[string]StreamClientFactory{},
		log:       log.With(slog.String("comp", "imbridge.stream")),
		running:   map[uint64]*streamSlot{},
		reconcile: 30 * time.Second,
	}
}

// RegisterFactory wires a provider implementation (Feishu / DingTalk
// SDK adapter) into the supervisor. Called at boot before Run.
func (s *StreamSupervisor) RegisterFactory(provider string, f StreamClientFactory) {
	s.factories[provider] = f
}

// Run starts the reconcile loop and stops clients when ctx is done.
func (s *StreamSupervisor) Run(ctx context.Context) {
	s.reconcileOnce(ctx) // initial apply

	tick := time.NewTicker(s.reconcile)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return
		case <-tick.C:
			s.reconcileOnce(ctx)
		}
	}
}

func (s *StreamSupervisor) reconcileOnce(ctx context.Context) {
	want, err := s.repo.ListEnabledStreamApps(ctx)
	if err != nil {
		s.log.Warn("list enabled stream apps", slog.Any("err", err))
		return
	}
	wantByID := make(map[uint64]*model.ImApp, len(want))
	for _, a := range want {
		wantByID[a.ID] = a
	}

	s.mu.Lock()
	stopping := make(map[uint64]*streamSlot)
	// Stop slots whose app row vanished, got disabled, or had its
	// credentials rotated.
	for id, slot := range s.running {
		nxt, ok := wantByID[id]
		stale := !ok || nxt.AppID != slot.app.AppID || nxt.AppSecret != slot.app.AppSecret
		if stale {
			s.log.Info("stopping stream client", slog.Uint64("im_app_id", id))
			stopping[id] = slot
			delete(s.running, id)
		}
	}
	s.mu.Unlock()
	s.stopClients(ctx, stopping)
	// Record outside the lock: the store carries its own lock, and calling
	// into it while holding s.mu would couple the two.
	for id := range stopping {
		s.health.Record(id, StreamHealth{State: StreamStateStopped})
	}
	if ctx.Err() != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Spawn slots for newly-arriving apps.
	for id, app := range wantByID {
		if _, ok := s.running[id]; ok {
			continue
		}
		factory, ok := s.factories[app.Provider]
		if !ok {
			s.log.Warn("no factory for provider — skipping",
				slog.String("provider", app.Provider),
				slog.Uint64("im_app_id", id))
			continue
		}
		client, err := factory(app, s.bridge)
		if err != nil {
			s.log.Warn("factory failed",
				slog.String("provider", app.Provider),
				slog.Uint64("im_app_id", id),
				slog.Any("err", err))
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		slot := &streamSlot{app: app, cancel: cancel, done: make(chan struct{})}
		s.running[id] = slot

		s.log.Info("starting stream client",
			slog.Uint64("im_app_id", id),
			slog.String("provider", app.Provider),
			slog.String("app_id", app.AppID))

		go s.runClientWithRestart(runCtx, slot, client)
	}
}

// redactSensitive scrubs credential values from a provider error before it is
// recorded in StreamHealth.LastError. The app row carries the plaintext
// credentials, and providers can embed them in error text — Telegram puts the
// bot token in the request URL, so an HTTP transport error echoes it. The app
// DTO hides the secret, but last_error is user-visible, so it must be scrubbed
// here too.
func redactSensitive(s string, app *model.ImApp) string {
	for _, secret := range []string{app.AppSecret, app.VerifyToken, app.EncryptKey} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

// runClientWithRestart wraps the client.Run call with exponential
// backoff. Reconnect logic inside the SDK handles transient network
// errors; we only see Run return on terminal errors or supervisor
// cancellation.
func (s *StreamSupervisor) runClientWithRestart(ctx context.Context, slot *streamSlot, client StreamClient) {
	defer close(slot.done)
	// A provider client that panics must not take the manager down with it.
	// The recovered goroutine leaves nothing behind, so the slot is dropped
	// and reported as retrying: keeping it would leave an entry that reads
	// as running while no client exists.
	defer func() {
		if r := recover(); r != nil {
			// Redact once, reuse twice: the panic value can carry a credential
			// (providers embed request URLs in their errors), and this text ends
			// up in the log as well as in the user-visible LastError.
			reason := redactSensitive(fmt.Sprintf("panic: %v", r), slot.app)
			s.log.Error("stream client panic",
				slog.String("provider", client.ProviderName()),
				slog.Uint64("im_app_id", slot.app.ID),
				slog.String("panic", reason))
			// Record the panic and drop the slot only if this goroutine still
			// owns the app. After a credential rotation a stale client can
			// panic *after* its replacement has started (the SDK exceeds the
			// stop timeout and keeps spinning); recording unconditionally here
			// would flip the replacement's healthy "running" back to "retrying".
			// Holding s.mu keeps the ownership check and the health write
			// atomic with respect to reconcile replacing the slot. The store is
			// an in-memory map under its own lock — no I/O, no callback into
			// the supervisor — so this neither deadlocks nor couples the locks.
			s.mu.Lock()
			if s.running[slot.app.ID] == slot {
				s.health.Record(slot.app.ID, StreamHealth{
					State:     StreamStateRetrying,
					LastError: reason,
				})
				delete(s.running, slot.app.ID)
			}
			s.mu.Unlock()
		}
	}()
	backoff := time.Second
	const maxBackoff = 60 * time.Second
	// A freshly spawned slot has nothing better to report than "an attempt is in
	// flight". Every later attempt has to earn it — see connectGrace below.
	s.health.Record(slot.app.ID, StreamHealth{State: StreamStateRunning})
	for ctx.Err() == nil {
		// Record running only once the attempt has outlived connectGrace. A client
		// that fails immediately (bad token, Socket Mode disabled) would otherwise
		// flap running -> retrying on every retry, and because Record re-stamps
		// UpdatedAt the Channels UI could never show how long the app had been
		// broken: it kept resetting to "just now".
		attemptDone := make(chan struct{})
		graceDone := make(chan struct{})
		go func() {
			defer close(graceDone)
			timer := time.NewTimer(connectGrace)
			defer timer.Stop()
			select {
			case <-timer.C:
				// Process-level state: the attempt is still in flight after the
				// grace window, which is as close to "connected" as a
				// StreamClient lets us observe.
				//
				// Claim it only while this goroutine still owns the app. A panic
				// unwinds past the join below without cancelling ctx, so an armed
				// timer would otherwise resurrect "running" five seconds after the
				// recovered panic had already reported "retrying" and dropped the
				// slot. Holding s.mu keeps the ownership check and the write atomic
				// with respect to reconcile, exactly like the recover branch.
				s.mu.Lock()
				if s.running[slot.app.ID] == slot && ctx.Err() == nil {
					s.health.Record(slot.app.ID, StreamHealth{State: StreamStateRunning})
				}
				s.mu.Unlock()
			case <-attemptDone:
			case <-ctx.Done():
			}
		}()
		err := client.Run(ctx)
		close(attemptDone)
		// The grace goroutine must be gone before retrying is recorded, otherwise
		// a timer firing concurrently could overwrite it with running.
		<-graceDone
		if err != nil && ctx.Err() == nil {
			// Same text feeds LastError and the log, so redact once.
			safeErr := redactSensitive(err.Error(), slot.app)
			s.health.Record(slot.app.ID, StreamHealth{
				State:     StreamStateRetrying,
				LastError: safeErr,
			})
			s.log.Warn("stream client exited; reconnect",
				slog.String("provider", client.ProviderName()),
				slog.Uint64("im_app_id", slot.app.ID),
				slog.Duration("backoff", backoff),
				slog.String("err", safeErr))
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (s *StreamSupervisor) shutdown() {
	s.mu.Lock()
	stopping := s.running
	s.running = make(map[uint64]*streamSlot)
	s.mu.Unlock()
	// Shutdown gets its own grace period after the parent context is cancelled.
	s.stopClients(context.Background(), stopping)
}

func (s *StreamSupervisor) stopClients(ctx context.Context, slots map[uint64]*streamSlot) {
	if len(slots) == 0 {
		return
	}
	// Cancel the entire batch before waiting; one broken provider must not
	// delay cancellation of the others or multiply the shutdown deadline.
	for _, slot := range slots {
		slot.cancel()
	}
	stopCtx, cancel := context.WithTimeout(ctx, streamStopTimeout)
	defer cancel()
	for id, slot := range slots {
		select {
		case <-slot.done:
			continue
		default:
		}
		select {
		case <-slot.done:
		case <-stopCtx.Done():
			// The client ignored cancellation past the deadline. Go cannot kill a
			// goroutine, so reconcile abandons this one on purpose: a leaked
			// goroutine is better than a dead reconcile loop (issue #517). The
			// consequences are spelled out in the log because they are invisible
			// anywhere else — this path is the only evidence that a provider
			// client is not honouring its context.
			provider := ""
			if slot.app != nil {
				provider = slot.app.Provider
			}
			s.log.Warn("stream client did not stop within the stop timeout; abandoning it",
				slog.Uint64("im_app_id", id),
				slog.String("provider", provider),
				slog.Duration("timeout", streamStopTimeout),
				slog.Any("err", stopCtx.Err()),
				slog.String("impact", "the goroutine and its connection are leaked until the manager restarts; a replacement is started in the same reconcile pass, so the old client can briefly run alongside it - duplicate events are possible (the bridge dedups them, it does not prevent them) and poll-based providers such as Telegram getUpdates reject the new poller while the old one still polls; a repeating message here means the provider client is not honouring context cancellation and needs fixing at the source"),
			)
		}
	}
}
