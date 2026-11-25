package oncall

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// MessageDeliverer abstracts non-voice IM/Webhook channel delivery for On-Call escalations and tests.
type MessageDeliverer interface {
	Deliver(ctx context.Context, channel string, targetUser *store.UserSummary, subject, body, severity string, labels map[string]string) (provider string, err error)
}

// Metrics holds low-cardinality Prometheus collectors for On-Call dispatch and escalation outcomes.
type Metrics struct {
	NotificationsTotal *prometheus.CounterVec
	EscalationsTotal   *prometheus.CounterVec
}

// NewMetrics constructs On-Call Prometheus metrics and registers them with reg when non-nil.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	notifications := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "opspilot",
		Subsystem: "oncall",
		Name:      "notifications_total",
		Help:      "Total On-Call notifications attempted by channel, provider, and status.",
	}, []string{"channel", "provider", "status"})

	escalations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "opspilot",
		Subsystem: "oncall",
		Name:      "escalations_total",
		Help:      "Total On-Call escalation task lifecycle transitions by outcome.",
	}, []string{"outcome"})

	if reg != nil {
		if err := reg.Register(notifications); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
					notifications = existing
				}
			}
		}
		if err := reg.Register(escalations); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
					escalations = existing
				}
			}
		}
	}

	return &Metrics{
		NotificationsTotal: notifications,
		EscalationsTotal:   escalations,
	}
}

func (m *Metrics) incNotification(channel, provider, status string) {
	if m == nil || m.NotificationsTotal == nil {
		return
	}
	if channel == "" {
		channel = "unknown"
	}
	if provider == "" {
		provider = "none"
	}
	if status == "" {
		status = "unknown"
	}
	m.NotificationsTotal.WithLabelValues(channel, provider, status).Inc()
}

func (m *Metrics) incEscalation(outcome string) {
	if m == nil || m.EscalationsTotal == nil || outcome == "" {
		return
	}
	m.EscalationsTotal.WithLabelValues(outcome).Inc()
}

// Dispatcher implements alert.OnCallDispatcher using non-blocking bounded async dispatch.
type Dispatcher struct {
	store        *store.Store
	routeMatcher *RouteMatcher
	wake         func()
	metrics      *Metrics
	log          *slog.Logger
	now          func() time.Time
	sem          chan struct{}
	wg           sync.WaitGroup
}

// NewDispatcher builds an On-Call dispatcher connected to store, routeMatcher, and optional worker wake hook.
func NewDispatcher(s *store.Store, rm *RouteMatcher, wake func(), metrics *Metrics, log *slog.Logger) *Dispatcher {
	if rm == nil {
		rm = NewRouteMatcher(s)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		store:        s,
		routeMatcher: rm,
		wake:         wake,
		metrics:      metrics,
		log:          log,
		now:          time.Now,
		sem:          make(chan struct{}, 32),
	}
}

// SetClock overrides the Dispatcher clock function (primarily for deterministic tests).
func (d *Dispatcher) SetClock(fn func() time.Time) {
	if d != nil && fn != nil {
		d.now = fn
	}
}

// OnAlertFired enqueues an On-Call escalation task asynchronously without blocking alert evaluation.
func (d *Dispatcher) OnAlertFired(incidentID uint64, ruleKey, title, severity string, deviceID uint64, labels map[string]string, firedAt time.Time) {
	if d == nil || d.store == nil || incidentID == 0 {
		return
	}
	labelsCopy := make(map[string]string, len(labels)+2)
	for k, v := range labels {
		labelsCopy[k] = v
	}

	select {
	case d.sem <- struct{}{}:
		d.wg.Add(1)
		go func() {
			defer func() {
				<-d.sem
				d.wg.Done()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			d.DispatchFiredSync(ctx, incidentID, ruleKey, title, severity, deviceID, labelsCopy, firedAt)
		}()
	default:
		d.log.Warn("oncall dispatch queue full, dropping async alert trigger", slog.Uint64("incident_id", incidentID))
	}
}

// DispatchFiredSync synchronously evaluates routing and persists an escalation task.
func (d *Dispatcher) DispatchFiredSync(ctx context.Context, incidentID uint64, ruleKey, title, severity string, deviceID uint64, labels map[string]string, firedAt time.Time) {
	if d == nil || d.store == nil || incidentID == 0 {
		return
	}
	cfg, err := d.store.GetVoiceConfig(ctx)
	if err != nil {
		d.log.Warn("oncall read config failed", slog.Uint64("incident_id", incidentID), slog.Any("err", err))
		return
	}
	if !cfg.DispatchEnabled {
		d.log.Debug("oncall dispatch disabled, skipping alert", slog.Uint64("incident_id", incidentID))
		return
	}

	matchLabels := make(map[string]string, len(labels)+3)
	for k, v := range labels {
		matchLabels[k] = v
	}
	if severity != "" {
		matchLabels["severity"] = severity
	}
	if ruleKey != "" {
		matchLabels["rule"] = ruleKey
	}

	matchRes, err := d.routeMatcher.MatchLabels(ctx, 0, matchLabels)
	if err != nil {
		d.log.Warn("oncall match route failed", slog.Uint64("incident_id", incidentID), slog.Any("err", err))
		return
	}
	if matchRes == nil || matchRes.ScheduleID == 0 {
		d.log.Debug("oncall no enabled schedule matched alert", slog.Uint64("incident_id", incidentID), slog.String("rule", ruleKey))
		return
	}

	var routeID uint64
	if matchRes.MatchedRoute != nil {
		routeID = matchRes.MatchedRoute.ID
	}

	now := time.Now()
	if d.now != nil {
		now = d.now()
	}
	if firedAt.IsZero() {
		firedAt = now
	}
	urgency := mapSeverityToUrgency(severity)
	task := &model.EscalationTask{
		IncidentID:    incidentID,
		RuleKey:       ruleKey,
		Title:         title,
		Severity:      severity,
		Urgency:       urgency,
		ScheduleID:    matchRes.ScheduleID,
		RouteID:       routeID,
		CurrentStep:   1,
		LadderStep:    1,
		StepStartedAt: now,
		Status:        model.TaskStatusPending,
		NextRunAt:     now,
	}
	if err := d.store.EnqueueEscalationTask(ctx, task); err != nil {
		d.log.Warn("oncall enqueue escalation task failed", slog.Uint64("incident_id", incidentID), slog.Any("err", err))
		return
	}
	d.metrics.incEscalation("enqueued")
	if d.wake != nil {
		d.wake()
	}
}

// OnAlertClosed cancels active On-Call escalation tasks asynchronously when an incident leaves open status.
func (d *Dispatcher) OnAlertClosed(incidentID uint64, status string) {
	if d == nil || d.store == nil || incidentID == 0 {
		return
	}
	select {
	case d.sem <- struct{}{}:
		d.wg.Add(1)
		go func() {
			defer func() {
				<-d.sem
				d.wg.Done()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			d.DispatchClosedSync(ctx, incidentID, status)
		}()
	default:
		// Best-effort bounded fallback if semaphore is full during burst closes.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d.DispatchClosedSync(ctx, incidentID, status)
	}
}

// DispatchClosedSync synchronously cancels pending escalation tasks for the incident.
func (d *Dispatcher) DispatchClosedSync(ctx context.Context, incidentID uint64, status string) {
	if d == nil || d.store == nil || incidentID == 0 {
		return
	}
	finalStatus := model.TaskStatusCancelled
	if strings.EqualFold(status, "acknowledged") {
		finalStatus = model.TaskStatusAcked
	}
	if err := d.store.CancelEscalationTasksByIncident(ctx, incidentID, finalStatus); err != nil {
		d.log.Warn("oncall cancel escalation tasks failed", slog.Uint64("incident_id", incidentID), slog.Any("err", err))
		return
	}
	d.metrics.incEscalation(finalStatus)
}

// Flush waits for all in-flight asynchronous dispatch goroutines (used primarily in tests).
func (d *Dispatcher) Flush() {
	if d == nil {
		return
	}
	d.wg.Wait()
}

func mapSeverityToUrgency(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "p0", "p1":
		return "high"
	default:
		return "low"
	}
}
