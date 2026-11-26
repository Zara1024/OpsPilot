package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	managerbizalert "github.com/Zara1024/OpsPilot/internal/manager/biz/alert"
	managerbizoncall "github.com/Zara1024/OpsPilot/internal/manager/biz/oncall"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	oncallstore "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	managerserveroncall "github.com/Zara1024/OpsPilot/internal/manager/server/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/config"
	"github.com/Zara1024/OpsPilot/internal/pkg/notify"
	"github.com/prometheus/client_golang/prometheus"
)

type oncallModule struct {
	handler         *managerserveroncall.Handler
	dispatcher      *managerbizoncall.Dispatcher
	worker          *managerbizoncall.EscalationWorker
	handoffReminder *managerbizoncall.HandoffReminderWorker
}

func bootstrapOncall(cfg *config.Config, log *slog.Logger, reg *prometheus.Registry, notifyRouter *notify.Router, storage *storageModule, alerts *alertsModule) *oncallModule {
	if storage == nil || storage.oncallStore == nil {
		return nil
	}
	compLog := log.With(slog.String("comp", "oncall"))
	metrics := managerbizoncall.NewMetrics(reg)
	sched := managerbizoncall.NewScheduler(storage.oncallStore)
	vg := voice.NewVoiceGateway()
	cg := channels.NewChannelGateway()
	publicURL := ""
	if cfg != nil {
		publicURL = strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	}

	var alertRepo managerbizalert.Repo
	if alerts != nil {
		alertRepo = alerts.alertRepo
	}
	deliverer := &oncallMessageDeliverer{
		notifyRouter: notifyRouter,
		alertRepo:    alertRepo,
		publicURL:    publicURL,
	}

	worker := managerbizoncall.NewEscalationWorker(managerbizoncall.EscalationWorkerOpts{
		Store:          storage.oncallStore,
		Scheduler:      sched,
		VoiceGateway:   vg,
		ChannelGateway: cg,
		Deliverer:      deliverer,
		Metrics:        metrics,
		Log:            compLog,
		PublicURL:      publicURL,
	})
	handoffReminder := managerbizoncall.NewHandoffReminderWorker(managerbizoncall.HandoffReminderWorkerOpts{
		Store:     storage.oncallStore,
		Scheduler: sched,
		Deliverer: deliverer,
		Metrics:   metrics,
		Log:       compLog,
	})

	rm := managerbizoncall.NewRouteMatcher(storage.oncallStore)
	dispatcher := managerbizoncall.NewDispatcher(storage.oncallStore, rm, worker.Wake, metrics, compLog)

	if alerts != nil && alerts.alertUC != nil {
		alerts.alertUC.SetOnCallDispatcher(dispatcher)
	}

	handler := managerserveroncall.NewHandlerWithDeps(storage.oncallStore, managerserveroncall.HandlerDeps{
		PublicURL:       publicURL,
		VoiceGateway:    vg,
		ChannelGateway:  cg,
		Deliverer:       deliverer,
		Worker:          worker,
		HandoffReminder: handoffReminder,
	})

	return &oncallModule{
		handler:         handler,
		dispatcher:      dispatcher,
		worker:          worker,
		handoffReminder: handoffReminder,
	}
}

type oncallMessageDeliverer struct {
	notifyRouter *notify.Router
	alertRepo    managerbizalert.Repo
	publicURL    string
}

func (d *oncallMessageDeliverer) Deliver(ctx context.Context, channel string, _ *oncallstore.UserSummary, subject, body, severity string, labels map[string]string) (string, error) {
	enrichedLabels := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		enrichedLabels[k] = v
	}
	if d.publicURL != "" && enrichedLabels["console_url"] == "" {
		enrichedLabels["console_url"] = d.publicURL
	}
	msg := notify.Message{
		Subject:    subject,
		Body:       body,
		Severity:   mapNotifySeverity(severity),
		Source:     "oncall:" + channel,
		Labels:     enrichedLabels,
		OccurredAt: time.Now().UTC(),
	}

	// Prefer enabled DB-configured notification channels (Feishu / DingTalk / WeCom / Slack / Webhook).
	if d.alertRepo != nil {
		dbChannels, err := d.alertRepo.ListEnabledChannels(ctx)
		if err == nil && len(dbChannels) > 0 {
			var succeeded []string
			var errs []error
			for _, ch := range dbChannels {
				if ch == nil || !ch.Enabled {
					continue
				}
				sender, buildErr := managerbizalert.BuildSenderFromChannel(ch)
				if buildErr != nil {
					errs = append(errs, buildErr)
					continue
				}
				sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				sendErr := sender.Send(sendCtx, msg)
				cancel()
				if sendErr != nil {
					errs = append(errs, fmt.Errorf("%s: %w", ch.Name, sendErr))
					continue
				}
				succeeded = append(succeeded, ch.ChannelType)
			}
			if len(succeeded) > 0 {
				return strings.Join(succeeded, ","), nil
			}
			if len(errs) > 0 {
				return "db_channel", errors.Join(errs...)
			}
		}
	}

	// Fallback to environment-configured notify.Router defaults when available.
	if d.notifyRouter != nil && len(d.notifyRouter.ChannelNames()) > 0 {
		if err := d.notifyRouter.Send(ctx, msg); err != nil {
			return "notify_router", err
		}
		return strings.Join(d.notifyRouter.ChannelNames(), ","), nil
	}

	return "unconfigured", errors.New("no notification channel configured")
}

func mapNotifySeverity(severity string) notify.Severity {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "p0", "p1":
		return notify.SeverityCritical
	case "warning", "p2":
		return notify.SeverityWarning
	default:
		return notify.SeverityInfo
	}
}
