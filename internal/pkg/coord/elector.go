package coord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultLeaseTTL           = 30 * time.Second
	defaultLeaseRenewInterval = 10 * time.Second
	defaultLeaseRetryInterval = 5 * time.Second
)

// ElectorConfig 配置选主租约的有效期、续约周期与争抢重试间隔。
type ElectorConfig struct {
	TTL           time.Duration
	RenewInterval time.Duration
	RetryInterval time.Duration
}

// Elector 负责以排他租约保护任意单实例任务（如定时调度器、对账器、长连接托管器）。
type Elector struct {
	store       Store
	holderID    string
	cfg         ElectorConfig
	log         *slog.Logger
	heldGauge   *prometheus.GaugeVec
	transitions *prometheus.CounterVec
}

// NewElector 创建租约选主控制器并注册低基数可观测指标（仅以 lease 名区分）。
func NewElector(store Store, holderID string, cfg ElectorConfig, reg prometheus.Registerer, log *slog.Logger) *Elector {
	if cfg.TTL <= 0 {
		cfg.TTL = defaultLeaseTTL
	}
	if cfg.RenewInterval <= 0 || cfg.RenewInterval >= cfg.TTL {
		cfg.RenewInterval = max(cfg.TTL/3, time.Millisecond)
		if cfg.TTL <= 3*time.Millisecond {
			cfg.RenewInterval = defaultLeaseRenewInterval
			if cfg.RenewInterval >= cfg.TTL {
				cfg.RenewInterval = cfg.TTL / 2
			}
		}
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = defaultLeaseRetryInterval
	}
	if log == nil {
		log = slog.Default()
	}

	heldGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "opspilot_cluster_lease_held",
		Help: "Whether this OpsPilot manager replica currently holds the distributed lease (1=leader, 0=standby).",
	}, []string{"lease"})

	transitions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "opspilot_cluster_lease_transitions_total",
		Help: "Total distributed lease state transitions observed by this replica.",
	}, []string{"lease", "event"})

	if reg != nil {
		heldGauge = registerGaugeVec(reg, heldGauge)
		transitions = registerCounterVec(reg, transitions)
	}

	return &Elector{
		store:       store,
		holderID:    holderID,
		cfg:         cfg,
		log:         log.With(slog.String("comp", "coord")),
		heldGauge:   heldGauge,
		transitions: transitions,
	}
}

func registerGaugeVec(reg prometheus.Registerer, g *prometheus.GaugeVec) *prometheus.GaugeVec {
	if err := reg.Register(g); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(*prometheus.GaugeVec); ok {
				return existing
			}
		}
	}
	return g
}

func registerCounterVec(reg prometheus.Registerer, c *prometheus.CounterVec) *prometheus.CounterVec {
	if err := reg.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
	}
	return c
}

// RunWithLeadership 持续竞争指定 `key` 的租约；仅在持有有效租约期间执行 `fn`。
// 采用 fail-closed 机制：当续约失败超过安全边界或租约被接管时，先取消 `leaderCtx` 并等待
// `fn` 完全退出后再释放租约，杜绝双主并行。仅当外部 `ctx` 被取消时返回 nil。
func (e *Elector) RunWithLeadership(ctx context.Context, key string, fn func(leaderCtx context.Context) error) error {
	if e == nil || e.store == nil {
		return fmt.Errorf("coord elector: nil store for lease %q", key)
	}
	if fn == nil {
		return fmt.Errorf("coord elector: nil leader callback for lease %q", key)
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		lease, acquired, err := e.store.TryAcquireOrRenew(ctx, key, e.holderID, e.cfg.TTL)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			e.recordTransition(key, "error")
			e.log.Warn("acquire distributed lease failed", slog.String("lease_key", key), slog.Any("err", err))
			if !waitContext(ctx, e.cfg.RetryInterval) {
				return nil
			}
			continue
		}
		if !acquired {
			e.setHeld(key, 0)
			if !waitContext(ctx, e.cfg.RetryInterval) {
				return nil
			}
			continue
		}

		e.setHeld(key, 1)
		e.recordTransition(key, "acquired")
		e.log.Info("acquired distributed lease",
			slog.String("lease_key", key),
			slog.String("holder_id", e.holderID),
			slog.Uint64("fencing_token", lease.FencingToken),
			slog.Time("expires_at", lease.ExpiresAt),
		)

		e.runLeaderSession(ctx, key, lease, fn)

		if ctx.Err() != nil {
			return nil
		}
		if !waitContext(ctx, e.cfg.RetryInterval) {
			return nil
		}
	}
}

func (e *Elector) runLeaderSession(ctx context.Context, key string, initial Lease, fn func(leaderCtx context.Context) error) {
	leaderCtx, cancelLeader := context.WithCancel(ctx)
	defer cancelLeader()

	doneCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("leader task panicked", slog.String("lease_key", key), slog.Any("panic", r))
				doneCh <- fmt.Errorf("leader task %q panicked: %v", key, r)
			}
		}()
		doneCh <- fn(leaderCtx)
	}()

	leaseExpiresAt := initial.ExpiresAt
	ticker := time.NewTicker(e.cfg.RenewInterval)
	defer ticker.Stop()

	lost := false
	workerFinished := false

loop:
	for {
		select {
		case <-ctx.Done():
			cancelLeader()
			break loop
		case workerErr := <-doneCh:
			workerFinished = true
			if workerErr != nil && ctx.Err() == nil && leaderCtx.Err() == nil {
				e.log.Warn("leader task exited unexpectedly", slog.String("lease_key", key), slog.Any("err", workerErr))
			}
			break loop
		case <-ticker.C:
			renewed, ok, err := e.store.TryAcquireOrRenew(ctx, key, e.holderID, e.cfg.TTL)
			if err != nil {
				if ctx.Err() != nil {
					cancelLeader()
					break loop
				}
				e.recordTransition(key, "error")
				e.log.Warn("renew distributed lease failed", slog.String("lease_key", key), slog.Any("err", err))
				if time.Now().UTC().Add(e.cfg.RenewInterval / 2).After(leaseExpiresAt) {
					lost = true
					cancelLeader()
					break loop
				}
				continue
			}
			if !ok {
				lost = true
				cancelLeader()
				break loop
			}
			leaseExpiresAt = renewed.ExpiresAt
		}
	}

	if !workerFinished {
		if workerErr := <-doneCh; workerErr != nil && ctx.Err() == nil && !lost {
			e.log.Warn("leader task exited during step-down", slog.String("lease_key", key), slog.Any("err", workerErr))
		}
	}

	e.setHeld(key, 0)
	if lost {
		e.recordTransition(key, "lost")
		e.log.Warn("lost distributed lease; stepped down", slog.String("lease_key", key), slog.String("holder_id", e.holderID))
	}

	// 使用独立超时上下文释放租约，确保收到 SIGTERM 或工作提前退出后备副本能秒级接管。
	relCtx, relCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := e.store.Release(relCtx, key, e.holderID); err != nil {
		e.log.Warn("release distributed lease failed", slog.String("lease_key", key), slog.Any("err", err))
	} else if !lost {
		e.recordTransition(key, "released")
	}
	relCancel()
}

func (e *Elector) setHeld(key string, val float64) {
	if e.heldGauge != nil {
		e.heldGauge.WithLabelValues(key).Set(val)
	}
}

func (e *Elector) recordTransition(key, event string) {
	if e.transitions != nil {
		e.transitions.WithLabelValues(key, event).Inc()
	}
}

func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
