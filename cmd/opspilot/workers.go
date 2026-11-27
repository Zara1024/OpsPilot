package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	managerbizalert "github.com/Zara1024/OpsPilot/internal/manager/biz/alert"
	managersvcaiops "github.com/Zara1024/OpsPilot/internal/manager/service/aiops"
	managersvcalert "github.com/Zara1024/OpsPilot/internal/manager/service/alert"
	managersvcedge "github.com/Zara1024/OpsPilot/internal/manager/service/edge"
	managersvck8s "github.com/Zara1024/OpsPilot/internal/manager/service/k8s"
	managersvctopology "github.com/Zara1024/OpsPilot/internal/manager/service/topology"
	"github.com/Zara1024/OpsPilot/internal/pkg/config"
	"github.com/Zara1024/OpsPilot/internal/pkg/coord"
	"github.com/Zara1024/OpsPilot/internal/pkg/notify"
	"github.com/Zara1024/OpsPilot/internal/pkg/prom"
	"golang.org/x/sync/errgroup"
)

func runWorkers(rootCtx context.Context, cfg *config.Config, log *slog.Logger, elector *coord.Elector, notifyRouter *notify.Router, storage *storageModule, settings *settingsModule, devices *devicesModule, alerts *alertsModule, telemetry *telemetryModule, aiops *aiopsModule, runtime *runtimeModule, automation *automationModule, extensions *extensionsModule, imbridge *imbridgeModule, http *httpModule, external *externalMCPModule, oncall *oncallModule) error {
	if elector == nil {
		return errors.New("coord elector is required")
	}

	eg, egCtx := errgroup.WithContext(rootCtx)
	if external != nil && external.engine != nil {
		eg.Go(func() error { return external.engine.Run(egCtx) })
	}
	// 无状态接口、监控采样、规则本地缓存及值班路由仍随每个副本运行。
	eg.Go(func() error { return http.apiServer.Start(egCtx) })
	eg.Go(func() error { return http.metricsServer.Start(egCtx) })
	eg.Go(func() error { return prom.RunDBPoolSampler(egCtx, storage.sqlDB, storage.errDB, log) })
	if runtime != nil && runtime.aiopsRuntime != nil {
		eg.Go(func() error { return managersvcaiops.RunWorkerMetrics(egCtx, runtime.aiopsRuntime) })
	}
	if automation != nil && automation.rcaInvConcrete != nil {
		eg.Go(func() error { return managersvcalert.RunInvestigatorMetrics(egCtx, automation.rcaInvConcrete) })
	}

	// 全局单例定时调度器：通过数据库租约选主确保同一时刻仅由单副本执行。
	if automation != nil && automation.flowScheduler != nil {
		eg.Go(func() error {
			return elector.RunWithLeadership(egCtx, "flow-scheduler", automation.flowScheduler.Run)
		})
	}
	if automation != nil && automation.reportScheduler != nil {
		eg.Go(func() error {
			return elector.RunWithLeadership(egCtx, "report-scheduler", automation.reportScheduler.Run)
		})
	}
	if imbridge != nil && imbridge.streamSupervisor != nil {
		eg.Go(func() error {
			return elector.RunWithLeadership(egCtx, "imbridge-stream", func(leaderCtx context.Context) error {
				imbridge.streamSupervisor.Run(leaderCtx)
				return nil
			})
		})
	}
	if extensions != nil && extensions.networkPollScheduler != nil {
		eg.Go(func() error {
			return elector.RunWithLeadership(egCtx, "network-poll", extensions.networkPollScheduler.Run)
		})
	}

	// 集群级状态对账、保留清理与超期租约回收收敛至单一主控租约组。
	var reconcilers []func(context.Context) error
	if devices != nil && devices.edgeUpgradeJobUC != nil {
		reconcilers = append(reconcilers, func(ctx context.Context) error {
			devices.edgeUpgradeJobUC.Run(ctx)
			return nil
		})
	}
	if settings != nil && settings.auditUC != nil {
		reconcilers = append(reconcilers, func(ctx context.Context) error {
			return settings.auditUC.RunRetention(ctx, settings.auditRetentionDays)
		})
	}
	if devices != nil && devices.k8sUC != nil {
		reconcilers = append(reconcilers,
			func(ctx context.Context) error { return managersvck8s.RunEventRetention(ctx, devices.k8sUC, log) },
			func(ctx context.Context) error { return managersvck8s.RunTopologyReconcile(ctx, devices.k8sUC, log) },
		)
	}
	if telemetry != nil && telemetry.apmProm != nil {
		reconcilers = append(reconcilers, func(ctx context.Context) error {
			return managersvctopology.RunServiceReconcile(ctx, telemetry.apmService, devices.topologyUC, log)
		})
	}
	if aiops != nil && aiops.operationUC != nil {
		reconcilers = append(reconcilers, func(ctx context.Context) error {
			return managersvcaiops.RunPacketCaptureOperations(ctx, aiops.operationUC, aiops.packetCaptureUC, aiops.aiopsRepo, log)
		})
	}
	if devices != nil && devices.edgeUC != nil && devices.deviceUC != nil {
		reconcilers = append(reconcilers, func(ctx context.Context) error {
			return managersvcedge.RunPresenceReconcile(ctx, devices.edgeUC, devices.deviceUC, devices.edgeOfflineThreshold, log)
		})
	}
	if automation != nil && automation.flowUC != nil {
		reconcilers = append(reconcilers, automation.flowUC.RunStaleRunReconciler)
	}
	if extensions != nil && extensions.approvalUC != nil {
		reconcilers = append(reconcilers, func(ctx context.Context) error {
			extensions.approvalUC.RunExecutionLeaseReconciler(ctx)
			return nil
		})
	}
	reconcilers = append(reconcilers, func(ctx context.Context) error {
		return runClusterMaintenanceLoop(ctx, cfg, log, aiops, automation, alerts)
	})
	eg.Go(func() error {
		return elector.RunWithLeadership(egCtx, "cluster-reconcilers", func(leaderCtx context.Context) error {
			return runLeaderGroup(leaderCtx, reconcilers...)
		})
	})

	// 告警评估与重试按租约互斥执行；rules 本地缓存保持每副本刷新。
	var alertPromQuerier managerbizalert.PromQuerier
	if telemetry != nil && telemetry.promQueryClient != nil {
		alertPromQuerier = telemetry.promQueryClient
	}
	if cfg.Alert.Enabled {
		eg.Go(func() error { return alerts.alertRules.Loop(egCtx) })
		pipelineEval := managerbizalert.NewPipelineEvaluator(managerbizalert.PipelineEvaluatorOpts{
			Usecase:                alerts.alertUC,
			Rules:                  alerts.alertRules,
			Notifier:               notifyRouter,
			Resolver:               alerts.alertResolver,
			Inhibitor:              alerts.alertInhibitor,
			DefaultChannels:        cfg.Notification.DefaultChannels,
			Cooldown:               cfg.Alert.Cooldown,
			Interval:               cfg.Alert.EvaluatorInterval,
			EdgeLister:             devices.edgeUC,
			PromQuerier:            alertPromQuerier,
			LogQuerier:             telemetry.lokiLogClient,
			LogSearcher:            telemetry.logsBackendSvc,
			DeviceIdentityResolver: managersvcalert.DeviceIdentityResolver(devices.deviceUC),
			ConsoleURL:             cfg.PublicURL,
			Log:                    log.With(slog.String("comp", "alert-pipeline")),
		})
		eg.Go(func() error {
			return elector.RunWithLeadership(egCtx, "alert-evaluator", func(leaderCtx context.Context) error {
				if alerts != nil && alerts.heartbeatWorker != nil {
					return runLeaderGroup(leaderCtx, pipelineEval.Loop, alerts.heartbeatWorker.Loop)
				}
				return pipelineEval.Loop(leaderCtx)
			})
		})

		retryWorker := managerbizalert.NewRetryWorker(managerbizalert.RetryWorkerOpts{
			Repo:        alerts.alertRepo,
			Notifier:    notifyRouter,
			Resolver:    alerts.alertResolver,
			Usecase:     alerts.alertUC,
			MaxAttempts: 5,
			Tick:        cfg.Alert.EvaluatorInterval,
			ConsoleURL:  cfg.PublicURL,
			Log:         log.With(slog.String("comp", "alert-retry")),
		})
		eg.Go(func() error {
			return elector.RunWithLeadership(egCtx, "alert-retry", retryWorker.Loop)
		})
	}

	if oncall != nil && oncall.worker != nil {
		eg.Go(func() error { return oncall.worker.Loop(egCtx) })
	}
	if oncall != nil && oncall.handoffReminder != nil {
		eg.Go(func() error { return oncall.handoffReminder.Loop(egCtx) })
	}

	err := eg.Wait()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = shutCtx

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Error("shutdown with error", slog.Any("err", err))
		return err
	}
	log.Info("opspilot shutdown complete")

	return nil
}

// runLeaderGroup 在单一 Leader 租约上下文下并发托管多个子循环，并在租约失去前确保全部子循环完全退出。
func runLeaderGroup(ctx context.Context, runners ...func(context.Context) error) error {
	if len(runners) == 0 {
		<-ctx.Done()
		return nil
	}
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	for _, fn := range runners {
		if fn == nil {
			continue
		}
		runFn := fn
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runFn(subCtx); err != nil && !errors.Is(err, context.Canceled) {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				cancel()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// runClusterMaintenanceLoop 由 cluster-reconcilers 主节点周期性清理超时 Worker、恢复溢出工具结果并执行 RCA 历史补偿。
func runClusterMaintenanceLoop(ctx context.Context, cfg *config.Config, log *slog.Logger, aiops *aiopsModule, automation *automationModule, alerts *alertsModule) error {
	if automation != nil && automation.rcaInvConcrete != nil && alerts != nil && alerts.alertUC != nil {
		// 仅在接管对账主控权时异步补偿历史未开始调查的告警，防止多副本并发轰炸 LLM。
		go managersvcalert.BackfillInvestigations(automation.rcaInvConcrete, alerts.alertUC, log)
	}
	staleTimeout := cfg.HA.StaleWorkerTimeout
	if staleTimeout <= 0 {
		staleTimeout = 30 * time.Minute
	}
	runPass := func() {
		passCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		now := time.Now().UTC()
		if aiops != nil && aiops.aiopsRepo != nil {
			if n, err := aiops.aiopsRepo.ReconcileInterruptedWorkers(passCtx, now.Add(-staleTimeout)); err != nil {
				log.Error("stale worker reconciler failed", slog.Any("err", err))
			} else if n > 0 {
				log.Info("stale workers closed by cluster reconciler", slog.Int64("sessions", n))
			}
		}
		if aiops != nil && aiops.resultRepo != nil {
			if _, err := aiops.resultRepo.Recover(passCtx); err != nil {
				log.Error("tool results recovery failed", slog.Any("err", err))
			}
			if _, err := aiops.resultRepo.Cleanup(passCtx, now); err != nil {
				log.Error("tool results cleanup failed", slog.Any("err", err))
			}
		}
	}

	runPass()
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			runPass()
		}
	}
}
