package alert

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
)

const defaultHeartbeatTick = 15 * time.Second

// HeartbeatWorker periodically scans active heartbeats and fires dead-man's switch alerts on overdue intervals.
type HeartbeatWorker struct {
	repo    IngestRepo
	alertUC *Usecase
	tick    time.Duration
	clock   Clock
	log     *slog.Logger
}

// NewHeartbeatWorker constructs a HeartbeatWorker. Nil clock/logger or non-positive tick fall back to safe defaults.
func NewHeartbeatWorker(repo IngestRepo, alertUC *Usecase, tick time.Duration, log *slog.Logger) *HeartbeatWorker {
	if tick <= 0 {
		tick = defaultHeartbeatTick
	}
	if log == nil {
		log = slog.Default()
	}
	return &HeartbeatWorker{
		repo:    repo,
		alertUC: alertUC,
		tick:    tick,
		clock:   realClock{},
		log:     log,
	}
}

func (w *HeartbeatWorker) setClock(c Clock) {
	if c != nil {
		w.clock = c
	}
}

// Loop evaluates overdue heartbeats on every tick until ctx is canceled.
func (w *HeartbeatWorker) Loop(ctx context.Context) error {
	if err := w.EvaluateOnce(ctx); err != nil && ctx.Err() == nil {
		w.log.Warn("initial heartbeat evaluation failed", slog.Any("err", err))
	}
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.EvaluateOnce(ctx); err != nil && ctx.Err() == nil {
				w.log.Warn("heartbeat evaluation failed", slog.Any("err", err))
			}
		}
	}
}

// EvaluateOnce checks all enabled healthy heartbeats and opens/reopens incidents for any overdue switches.
func (w *HeartbeatWorker) EvaluateOnce(ctx context.Context) error {
	if w.repo == nil || w.alertUC == nil {
		return nil
	}
	items, err := w.repo.ListEnabledHeartbeats(ctx)
	if err != nil {
		return fmt.Errorf("list enabled heartbeats: %w", err)
	}

	now := w.clock.Now().UTC()
	for _, hb := range items {
		if hb == nil || !hb.Enabled || hb.State != model.HeartbeatStateHealthy || hb.LastPingAt == nil {
			continue
		}
		timeout := time.Duration(hb.IntervalSeconds+hb.GraceSeconds) * time.Second
		if timeout <= 0 {
			timeout = 180 * time.Second
		}
		deadline := hb.LastPingAt.UTC().Add(timeout)
		if !now.After(deadline) {
			continue
		}

		labels := decodeStringMapJSON(hb.LabelsJSON)
		labels["source_type"] = model.RuleSourceHeartbeat
		labels["heartbeat_id"] = fmt.Sprintf("%d", hb.ID)
		labels["heartbeat_name"] = hb.Name
		labels["target"] = hb.Name

		overdue := now.Sub(hb.LastPingAt.UTC()).Round(time.Second)
		dedupeKey := fmt.Sprintf("heartbeat:%d", hb.ID)
		summary := fmt.Sprintf(
			"死信心跳「%s」已超过 %s 未上报（期望周期 %ds + 宽限期 %ds，上次心跳：%s）",
			hb.Name,
			overdue,
			hb.IntervalSeconds,
			hb.GraceSeconds,
			hb.LastPingAt.UTC().Format(time.RFC3339),
		)

		fr, fireErr := w.alertUC.RecordFiring(ctx, FiringInput{
			ScopeType:   model.RuleScopeExternal,
			Scope:       truncateRunes(hb.Name, 32),
			Rule:        "heartbeat_missed",
			RuleName:    "死信心跳失联",
			Severity:    firstPresent(hb.Severity, "critical"),
			OccurredAt:  now,
			DedupeKey:   dedupeKey,
			SourceType:  model.RuleSourceHeartbeat,
			Title:       truncateRunes(fmt.Sprintf("心跳监控失联: %s", hb.Name), 256),
			Summary:     summary,
			Description: hb.Description,
			Labels:      labels,
		})
		if fireErr != nil {
			w.log.Warn("fire overdue heartbeat incident failed", slog.Uint64("heartbeat_id", hb.ID), slog.Any("err", fireErr))
			continue
		}

		var incID uint64
		if fr != nil && fr.Incident != nil {
			incID = fr.Incident.ID
		}
		if _, markErr := w.repo.MarkHeartbeatAlerting(ctx, hb.ID, now, incID); markErr != nil {
			w.log.Warn("mark heartbeat alerting failed", slog.Uint64("heartbeat_id", hb.ID), slog.Any("err", markErr))
		}
	}
	return nil
}
