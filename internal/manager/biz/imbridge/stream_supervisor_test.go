package imbridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/imbridge"
)

type streamRepoFunc func(context.Context) ([]*model.ImApp, error)

func (f streamRepoFunc) ListEnabledStreamApps(ctx context.Context) ([]*model.ImApp, error) {
	return f(ctx)
}

type streamClientFunc func(context.Context) error

func (f streamClientFunc) ProviderName() string          { return "test" }
func (f streamClientFunc) Run(ctx context.Context) error { return f(ctx) }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStreamSupervisorContinuesAfterStopTimeout(t *testing.T) {
	for _, tc := range []struct {
		name string
		next model.ImApp
	}{
		{"removed_or_disabled", model.ImApp{ID: 2, Provider: "telegram", AppID: "other"}},
		{"app_id_rotated", model.ImApp{ID: 1, Provider: "feishu", AppID: "new", AppSecret: "old-secret"}},
		{"secret_rotated", model.ImApp{ID: 1, Provider: "feishu", AppID: "old", AppSecret: "new-secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				old := &model.ImApp{ID: 1, Provider: "feishu", AppID: "old", AppSecret: "old-secret"}
				apps := []*model.ImApp{old}
				s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
					return apps, nil
				}), nil, nil, discardLogger())
				ctx, cancel := context.WithCancel(context.Background())
				release := make(chan struct{})
				defer func() { cancel(); s.shutdown() }()
				defer close(release)
				started := make(chan *model.ImApp, 2)
				factory := func(app *model.ImApp, _ *Bridge) (StreamClient, error) {
					return streamClientFunc(func(ctx context.Context) error {
						started <- app
						<-ctx.Done()
						if app == old {
							<-release // A provider that ignores cancellation until externally released.
						}
						return ctx.Err()
					}), nil
				}
				s.RegisterFactory("feishu", factory)
				s.RegisterFactory("telegram", factory)
				s.reconcileOnce(ctx)
				synctest.Wait()
				if got := <-started; got != old {
					t.Fatalf("started app = %v, want old app", got)
				}
				oldSlot := s.running[old.ID]
				apps = []*model.ImApp{&tc.next}
				begin := time.Now()
				s.reconcileOnce(ctx)
				if elapsed := time.Since(begin); elapsed != streamStopTimeout {
					t.Fatalf("reconcile took %s, want %s", elapsed, streamStopTimeout)
				}
				synctest.Wait()
				select {
				case got := <-started:
					if got != &tc.next {
						t.Fatalf("started app = %v, want replacement", got)
					}
				default:
					t.Fatal("replacement did not start after stop timeout")
				}
				newSlot := s.running[tc.next.ID]
				// Late exit of the old client must not remove the replacement slot.
				release <- struct{}{}
				<-oldSlot.done
				s.reconcileOnce(ctx)
				if len(s.running) != 1 || s.running[tc.next.ID] != newSlot {
					t.Fatal("late exit changed the replacement slot")
				}
			})
		})
	}
}

func TestStreamSupervisorShutdownCancelsAllBeforeWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewStreamSupervisor(nil, nil, nil, discardLogger())
		release := make(chan struct{})
		defer close(release)
		var contexts []context.Context
		for id := uint64(1); id <= 3; id++ {
			ctx, cancel := context.WithCancel(context.Background())
			contexts = append(contexts, ctx)
			s.running[id] = &streamSlot{cancel: cancel, done: release}
		}
		done := make(chan struct{})
		begin := time.Now()
		go func() { s.shutdown(); close(done) }()
		synctest.Wait()
		for i, ctx := range contexts {
			if ctx.Err() == nil {
				t.Errorf("client %d was not cancelled before waiting", i)
			}
		}
		<-done
		if elapsed := time.Since(begin); elapsed != streamStopTimeout {
			t.Fatalf("shutdown took %s, want one shared %s deadline", elapsed, streamStopTimeout)
		}
		if len(s.running) != 0 {
			t.Fatal("shutdown retained running slots")
		}
	})
}

func TestStreamSupervisorCancellationDuringReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		apps := []*model.ImApp{{ID: 1, Provider: "test"}, {ID: 2, Provider: "test"}}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return apps, nil
		}), nil, nil, discardLogger())
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()
		release := make(chan struct{})
		defer close(release)
		contexts := make(map[uint64]context.Context)
		// Each client records its context before synctest.Wait returns.
		started := make(chan struct {
			id  uint64
			ctx context.Context
		}, 3)
		s.RegisterFactory("test", func(app *model.ImApp, _ *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				started <- struct {
					id  uint64
					ctx context.Context
				}{app.ID, ctx}
				<-ctx.Done()
				if app.ID == 1 {
					<-release
				}
				return ctx.Err()
			}), nil
		})
		s.reconcileOnce(ctx)
		for range 2 {
			client := <-started
			contexts[client.id] = client.ctx
		}
		apps = []*model.ImApp{apps[1], {ID: 3, Provider: "test"}}
		done := make(chan struct{})
		go func() { s.reconcileOnce(ctx); close(done) }()
		synctest.Wait()
		begin := time.Now()
		cancel()
		<-done
		synctest.Wait()
		if elapsed := time.Since(begin); elapsed != 0 {
			t.Fatalf("cancelled reconcile waited %s", elapsed)
		}
		if contexts[2].Err() == nil {
			t.Fatal("parent cancellation did not reach the unchanged client")
		}
		select {
		case <-started:
			t.Fatal("started a client after supervisor cancellation")
		default:
		}
	})
}

func TestStreamSupervisorRunStopsCooperativeClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{{ID: 1, Provider: "test"}}, nil
		}), nil, nil, discardLogger())
		started, stopped := make(chan struct{}), make(chan struct{})
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { s.Run(ctx); close(done) }()
		<-started
		begin := time.Now()
		cancel()
		<-done
		<-stopped
		if elapsed := time.Since(begin); elapsed != 0 {
			t.Fatalf("cooperative shutdown waited %s", elapsed)
		}
	})
}

// While an attempt is in flight the app must read as running, so an enabled
// app with no live client stands out instead of looking healthy.
func TestStreamSupervisor_WhenClientAttempts_RecordsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		started := make(chan struct{})
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		<-started
		synctest.Wait()

		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded for a running client")
		}
		if got.State != StreamStateRunning {
			t.Fatalf("state = %q, want %q", got.State, StreamStateRunning)
		}
	})
}

// A client that returns keeps its reason, so the UI can show why the
// reconnect loop is spinning.
func TestStreamSupervisor_WhenClientReturns_RecordsRetryingWithError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(context.Context) error {
				return errors.New("dial tcp: connection refused")
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		synctest.Wait()

		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded")
		}
		// A fresh slot records running once before the loop; the client then fails
		// immediately, so the state observed here is retrying.
		if got.State != StreamStateRetrying {
			t.Fatalf("state = %q, want %q", got.State, StreamStateRetrying)
		}
		if got.LastError != "dial tcp: connection refused" {
			t.Fatalf("last_error = %q", got.LastError)
		}
	})
}

// A client that fails faster than connectGrace must never flap to running. It
// used to: the supervisor recorded running at the top of every attempt, so a
// fast-failing client (bad token, Socket Mode disabled) alternated
// running -> retrying on each retry. Because Record re-stamped updated_at each
// time, the Channels badge read "alive" half the time and its "how long has this
// been broken" timestamp kept resetting to "just now".
func TestStreamSupervisor_WhenClientFailsFast_DoesNotFlapToRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		var attempts atomic.Int64
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				attempts.Add(1)
				// Give up well inside connectGrace but long enough that this test's
				// sampling point lands mid-attempt.
				select {
				case <-time.After(100 * time.Millisecond):
					return errors.New("dial tcp: connection refused")
				case <-ctx.Done():
					return ctx.Err()
				}
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		synctest.Wait()

		// Attempt #1 runs from t=0 to t=100ms, then the supervisor waits out its
		// 1s backoff (t=200ms is inside that wait).
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		failed, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded")
		}
		if failed.State != StreamStateRetrying {
			t.Fatalf("state after the first fast failure = %q, want %q", failed.State, StreamStateRetrying)
		}

		// Attempt #2 starts at t=1.1s. Halfway through it the state must still read
		// retrying: the attempt has not outlived connectGrace, so it has not earned
		// "running". Recording it up front is what made the badge flap.
		time.Sleep(950 * time.Millisecond)
		synctest.Wait()
		if mid, _ := health.Get(app.ID); mid.State != StreamStateRetrying {
			t.Fatalf("state mid-retry = %q, want %q", mid.State, StreamStateRetrying)
		}

		// After many retries the timestamp still marks when the state began, which
		// is what the Channels UI renders as "how long has this been broken".
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if n := attempts.Load(); n < 3 {
			t.Fatalf("attempts = %d, want >= 3 (test did not exercise several retries)", n)
		}
		after, _ := health.Get(app.ID)
		if after.State != StreamStateRetrying {
			t.Fatalf("state after retries = %q, want %q", after.State, StreamStateRetrying)
		}
		if !after.UpdatedAt.Equal(failed.UpdatedAt) {
			t.Fatalf("updated_at moved from %v to %v across retries; it must mark when the state changed", failed.UpdatedAt, after.UpdatedAt)
		}
	})
}

// The other half of the contract: an attempt that outlives connectGrace is what
// "running" is supposed to mean, otherwise a healthy client would be stuck on
// retrying forever.
func TestStreamSupervisor_WhenAttemptOutlivesConnectGrace_RecordsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				select {
				case <-time.After(30 * time.Second):
					return errors.New("idle timeout")
				case <-ctx.Done():
					return ctx.Err()
				}
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		synctest.Wait()

		// Still inside the attempt, past connectGrace (5s but well before 30s).
		time.Sleep(6 * time.Second)
		synctest.Wait()

		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded")
		}
		if got.State != StreamStateRunning {
			t.Fatalf("state = %q, want %q: an attempt that outlived connectGrace is the closest thing to connected we can observe", got.State, StreamStateRunning)
		}
	})
}

// A panicking client unwinds past the join, so the grace timer armed for that
// attempt is still running when the recover branch reports retrying and drops the
// slot. It must not resurrect "running" for an app the supervisor no longer owns:
// the slot is gone and no client exists.
func TestStreamSupervisor_WhenClientPanics_GraceTimerDoesNotResurrectRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(context.Context) error {
				panic("provider client exploded")
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		synctest.Wait()

		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded")
		}
		if got.State != StreamStateRetrying {
			t.Fatalf("state after the panic = %q, want %q", got.State, StreamStateRetrying)
		}

		// Well past connectGrace: the abandoned timer must not claim the app back.
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if after, _ := health.Get(app.ID); after.State != StreamStateRetrying {
			t.Fatalf("state = %q after the grace window, want %q: a dropped slot must not come back as running", after.State, StreamStateRetrying)
		}
	})
}

func TestStreamSupervisor_WhenAppRemoved_RecordsStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		apps := []*model.ImApp{app}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return apps, nil
		}), nil, health, discardLogger())
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		synctest.Wait()
		if got, ok := health.Get(app.ID); !ok || got.State != StreamStateRunning {
			t.Fatalf("before removal: state = %q ok = %v, want running", got.State, ok)
		}

		apps = nil
		s.reconcileOnce(ctx)
		synctest.Wait()

		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("health dropped instead of being marked stopped")
		}
		if got.State != StreamStateStopped {
			t.Fatalf("state = %q, want %q", got.State, StreamStateStopped)
		}
	})
}

// An app whose provider has no factory never starts a client, so it must stay
// unknown rather than be reported as running.
func TestStreamSupervisor_WhenProviderHasNoFactory_LeavesHealthUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{{ID: 1, Provider: "unregistered"}}, nil
		}), nil, health, discardLogger())

		s.reconcileOnce(context.Background())
		synctest.Wait()

		if got, ok := health.Get(1); ok {
			t.Fatalf("health = %+v recorded without a factory", got)
		}
	})
}

// A panicking provider must not take the manager down: the slot is dropped and
// the reason is recorded, so the next reconcile retries the app.
func TestStreamSupervisor_WhenClientPanics_RecoversAndRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		attempts := make(chan struct{}, 2)
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(context.Context) error {
				attempts <- struct{}{}
				panic("provider exploded")
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s.reconcileOnce(ctx)
		synctest.Wait()
		<-attempts

		if len(s.running) != 0 {
			t.Fatalf("running slots = %d, want the panicked slot dropped", len(s.running))
		}
		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded after a panic")
		}
		if got.State != StreamStateRetrying || !strings.Contains(got.LastError, "provider exploded") {
			t.Fatalf("health = %+v, want retrying with the panic reason", got)
		}

		s.reconcileOnce(ctx)
		synctest.Wait()
		select {
		case <-attempts:
		default:
			t.Fatal("reconcile did not retry after a panicking client")
		}
	})
}

// A provider error can embed the app secret (Telegram puts the bot token in
// the request URL, so an HTTP transport error echoes it). last_error is
// user-visible, so the supervisor must scrub the credential before recording.
func TestStreamSupervisor_WhenClientReturns_RedactsSecretFromError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		app := &model.ImApp{ID: 1, Provider: "test", AppID: "app", AppSecret: "sekret-token"}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return []*model.ImApp{app}, nil
		}), nil, health, discardLogger())
		s.RegisterFactory("test", func(*model.ImApp, *Bridge) (StreamClient, error) {
			return streamClientFunc(func(context.Context) error {
				return errors.New(`Post "https://api.telegram.org/botsekret-token/getUpdates": dial tcp: connection refused`)
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		synctest.Wait()

		got, ok := health.Get(app.ID)
		if !ok {
			t.Fatal("no health recorded")
		}
		if got.State != StreamStateRetrying {
			t.Fatalf("state = %q, want %q", got.State, StreamStateRetrying)
		}
		if strings.Contains(got.LastError, "sekret-token") {
			t.Fatalf("last_error leaked the secret: %q", got.LastError)
		}
		if !strings.Contains(got.LastError, "[redacted]") {
			t.Fatalf("last_error = %q, want the secret redacted", got.LastError)
		}
	})
}

// After a credential rotation the stale client can exceed the stop timeout and
// panic once its replacement is already running. The recover path must not
// clobber the replacement's healthy "running" with "retrying".
func TestStreamSupervisor_WhenStaleClientPanicsAfterReplacement_KeepsReplacementHealth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		health := NewStreamHealthStore()
		old := &model.ImApp{ID: 1, Provider: "feishu", AppID: "old", AppSecret: "old-secret"}
		next := &model.ImApp{ID: 1, Provider: "feishu", AppID: "old", AppSecret: "new-secret"}
		apps := []*model.ImApp{old}
		s := NewStreamSupervisor(streamRepoFunc(func(context.Context) ([]*model.ImApp, error) {
			return apps, nil
		}), nil, health, discardLogger())
		panicNow := make(chan struct{})
		oldStarted := make(chan struct{})
		newStarted := make(chan struct{})
		s.RegisterFactory("feishu", func(app *model.ImApp, _ *Bridge) (StreamClient, error) {
			return streamClientFunc(func(ctx context.Context) error {
				if app == old {
					close(oldStarted)
					<-ctx.Done()
					<-panicNow // stale client ignores cancel until told to panic
					panic("stale client exploded")
				}
				close(newStarted)
				<-ctx.Done()
				return ctx.Err()
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer func() { cancel(); s.shutdown() }()

		s.reconcileOnce(ctx)
		<-oldStarted
		synctest.Wait()
		oldSlot := s.running[old.ID] // captured before the rotation replaces it

		apps = []*model.ImApp{next}
		s.reconcileOnce(ctx)
		<-newStarted
		synctest.Wait()

		if got, ok := health.Get(next.ID); !ok || got.State != StreamStateRunning {
			t.Fatalf("replacement state = %q ok = %v, want running", got.State, ok)
		}

		close(panicNow)
		// Waiting on the old slot proves the panic actually fired. Without this
		// the assertions below would also pass if the stale client never ran.
		<-oldSlot.done
		synctest.Wait()

		got, ok := health.Get(next.ID)
		if !ok || got.State != StreamStateRunning {
			t.Fatalf("stale panic clobbered replacement health: %+v (ok = %v)", got, ok)
		}
	})
}
