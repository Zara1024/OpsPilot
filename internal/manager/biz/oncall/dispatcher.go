package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

// MessageDeliverer abstracts non-voice IM/Webhook channel delivery for On-Call escalations, reminders, and tests.
type MessageDeliverer interface {
	Deliver(ctx context.Context, channel string, targetUser *store.UserSummary, subject, body, severity string, labels map[string]string) (provider string, err error)
}

// Metrics holds low-cardinality Prometheus collectors for On-Call dispatch, grouping, failover, and escalation outcomes.
type Metrics struct {
	NotificationsTotal  *prometheus.CounterVec
	EscalationsTotal    *prometheus.CounterVec
	AlertsMergedTotal   prometheus.Counter
	VoiceFailoversTotal *prometheus.CounterVec
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

	merged := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "opspilot",
		Subsystem: "oncall",
		Name:      "alerts_merged_total",
		Help:      "Total firing alerts merged into existing grouped escalation tasks.",
	})

	failovers := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "opspilot",
		Subsystem: "oncall",
		Name:      "voice_failovers_total",
		Help:      "Total voice gateway failovers from primary to backup provider by outcome.",
	}, []string{"from_provider", "to_provider", "outcome"})

	if reg != nil {
		notifications = registerCounterVec(reg, notifications)
		escalations = registerCounterVec(reg, escalations)
		merged = registerCounter(reg, merged)
		failovers = registerCounterVec(reg, failovers)
	}

	return &Metrics{
		NotificationsTotal:  notifications,
		EscalationsTotal:    escalations,
		AlertsMergedTotal:   merged,
		VoiceFailoversTotal: failovers,
	}
}

func registerCounterVec(reg prometheus.Registerer, c *prometheus.CounterVec) *prometheus.CounterVec {
	if err := reg.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
	}
	return c
}

func registerCounter(reg prometheus.Registerer, c prometheus.Counter) prometheus.Counter {
	if err := reg.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := are.ExistingCollector.(prometheus.Counter); ok {
				return existing
			}
		}
	}
	return c
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

func (m *Metrics) incMerged() {
	if m == nil || m.AlertsMergedTotal == nil {
		return
	}
	m.AlertsMergedTotal.Inc()
}

func (m *Metrics) incVoiceFailover(fromProvider, toProvider, outcome string) {
	if m == nil || m.VoiceFailoversTotal == nil || fromProvider == "" || toProvider == "" {
		return
	}
	if outcome == "" {
		outcome = "unknown"
	}
	m.VoiceFailoversTotal.WithLabelValues(fromProvider, toProvider, outcome).Inc()
}

// Dispatcher implements alert.OnCallDispatcher using non-blocking bounded async dispatch and route-based grouping.
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

// OnAlertFired enqueues or merges an On-Call escalation task asynchronously without blocking alert evaluation.
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

// DispatchFiredSync synchronously evaluates routing, grouping, and persists or merges an escalation task.
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

	now := time.Now().UTC()
	if d.now != nil {
		now = d.now().UTC()
	}
	if firedAt.IsZero() {
		firedAt = now
	}

	if mw, mwErr := MatchMaintenanceWindow(ctx, d.store, d.routeMatcher, matchRes.ScheduleID, matchLabels, now); mwErr != nil {
		d.log.Warn("oncall maintenance window match failed", slog.Uint64("incident_id", incidentID), slog.Any("err", mwErr))
	} else if mw != nil && mw.Action == model.MaintenanceActionSuppressAll {
		if incErr := d.store.IncrementMaintenanceSuppressed(ctx, mw.ID); incErr != nil {
			d.log.Warn("oncall increment maintenance suppressed failed", slog.Uint64("mw_id", mw.ID), slog.Any("err", incErr))
		}
		if logErr := d.store.CreateNotificationLog(ctx, &model.NotificationLog{
			IncidentID: incidentID,
			AlertCount: 1,
			ScheduleID: matchRes.ScheduleID,
			Channel:    "maintenance",
			Provider:   "maintenance_window",
			Status:     model.NotifyStatusSuppressed,
			Target:     mw.Name,
			Message:    fmt.Sprintf("维护窗口「%s」(#%d) 生效中，已抑制告警派发", mw.Name, mw.ID),
			CreatedAt:  now,
		}); logErr != nil {
			d.log.Warn("oncall log maintenance suppression failed", slog.Uint64("incident_id", incidentID), slog.Any("err", logErr))
		}
		d.metrics.incNotification("maintenance", "maintenance_window", model.NotifyStatusSuppressed)
		d.metrics.incEscalation("maintenance_suppressed")
		return
	}

	var routeID uint64
	if matchRes.MatchedRoute != nil {
		routeID = matchRes.MatchedRoute.ID
	}
	urgency := mapSeverityToUrgency(severity)
	groupKey, waitDur, intervalDur := buildGroupKey(matchRes, ruleKey, urgency, matchLabels)

	task := &model.EscalationTask{
		IncidentID:  incidentID,
		GroupKey:    groupKey,
		RuleKey:     ruleKey,
		Title:       title,
		Severity:    severity,
		Urgency:     urgency,
		ScheduleID:  matchRes.ScheduleID,
		RouteID:     routeID,
		CurrentStep: 1,
		LadderStep:  1,
		Status:      model.TaskStatusPending,
	}
	merged, err := d.store.EnqueueOrMergeEscalationTask(ctx, task, waitDur, intervalDur, now)
	if err != nil {
		d.log.Warn("oncall enqueue or merge escalation task failed", slog.Uint64("incident_id", incidentID), slog.Any("err", err))
		return
	}
	if merged {
		d.metrics.incMerged()
		return
	}
	d.metrics.incEscalation("enqueued")
	if waitDur == 0 && d.wake != nil {
		d.wake()
	}
}

func buildGroupKey(matchRes *MatchResult, ruleKey, urgency string, labels map[string]string) (string, time.Duration, time.Duration) {
	if matchRes == nil || matchRes.MatchedRoute == nil {
		return "", 0, 0
	}
	route := matchRes.MatchedRoute
	if route.GroupWaitSeconds <= 0 {
		return "", 0, 0
	}
	waitDur := time.Duration(route.GroupWaitSeconds) * time.Second
	intervalSecs := route.GroupIntervalSeconds
	if intervalSecs < 0 {
		intervalSecs = 0
	}
	intervalDur := time.Duration(intervalSecs) * time.Second

	var groupKeys []string
	if strings.TrimSpace(route.GroupByJSON) != "" {
		_ = json.Unmarshal([]byte(route.GroupByJSON), &groupKeys)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("r%d:s%d", route.ID, matchRes.ScheduleID))
	if len(groupKeys) > 0 {
		for _, k := range groupKeys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			v := strings.TrimSpace(labels[k])
			if k == "rule" && v == "" {
				v = ruleKey
			}
			if k == "urgency" && v == "" {
				v = urgency
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	} else {
		parts = append(parts, fmt.Sprintf("rule=%s", ruleKey), fmt.Sprintf("urgency=%s", urgency))
	}

	raw := strings.Join(parts, ":")
	if len(raw) > 120 {
		raw = raw[:120]
	}
	return raw, waitDur, intervalDur
}

// OnAlertClosed cancels or shrinks active On-Call escalation tasks asynchronously when an incident leaves open status.
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
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d.DispatchClosedSync(ctx, incidentID, status)
	}
}

// DispatchClosedSync synchronously cancels or shrinks pending escalation tasks for the incident.
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
