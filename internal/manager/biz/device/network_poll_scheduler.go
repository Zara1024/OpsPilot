package device

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// NetworkPollScheduler scans for due, explicitly configured SNMP targets.
// Polling is deliberately bounded and sequential because every request is a
// reverse-tunnel RPC through an Edge rather than a local database operation.
type NetworkPollScheduler struct {
	uc       *NetworkDiscoveryUsecase
	log      *slog.Logger
	interval time.Duration
}

func NewNetworkPollScheduler(uc *NetworkDiscoveryUsecase, log *slog.Logger) *NetworkPollScheduler {
	if log == nil {
		log = slog.Default()
	}
	return &NetworkPollScheduler{uc: uc, log: log.With(slog.String("comp", "network-poll-scheduler")), interval: 30 * time.Second}
}

func (s *NetworkPollScheduler) Start(ctx context.Context) {
	if s == nil || s.uc == nil {
		return
	}
	go func() {
		// 异步兼容入口；阻塞语义与 panic 保护统一收敛到 Run。
		_ = s.Run(ctx)
	}()
}

// Run 在当前 goroutine 内阻塞执行网络采集拓扑对账与到期 SNMP 轮询，直至 ctx 取消。
func (s *NetworkPollScheduler) Run(ctx context.Context) (retErr error) {
	if s == nil || s.uc == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.log.Error("network poll scheduler panic", slog.Any("panic", recovered))
			retErr = fmt.Errorf("network poll scheduler panic: %v", recovered)
		}
	}()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.log.Info("network poll scheduler started", slog.Duration("interval", s.interval))
	var topologyCursor uint64
	for {
		select {
		case <-ctx.Done():
			s.log.Info("network poll scheduler stopped")
			return nil
		case now := <-ticker.C:
			next, err := s.uc.ReconcileCollectionTopology(ctx, topologyCursor)
			topologyCursor = next
			if err != nil {
				s.log.Warn("network topology reconciliation failed", slog.Any("err", err))
			}
			if _, err := s.uc.PollDueNetworkDevices(ctx, now.UTC(), 10); err != nil {
				s.log.Warn("network poll tick failed", slog.Any("err", err))
			}
		}
	}
}
