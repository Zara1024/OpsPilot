package oncall

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/voice"
	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	alertmodel "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
)

const (
	defaultWorkerTick    = 10 * time.Second
	defaultWorkerLease   = 30 * time.Second
	defaultVoiceCooldown = 3 * time.Minute
	maxStepRetries       = 2
)

// EscalationWorkerOpts configures EscalationWorker dependencies and timing.
type EscalationWorkerOpts struct {
	Store          *store.Store
	Scheduler      *Scheduler
	VoiceGateway   *voice.VoiceGateway
	ChannelGateway *channels.ChannelGateway
	Deliverer      MessageDeliverer
	Metrics        *Metrics
	Log            *slog.Logger
	PublicURL      string
	TickInterval   time.Duration
	LeaseDuration  time.Duration
	VoiceCooldown  time.Duration
	Now            func() time.Time
}

// EscalationWorker executes due On-Call escalation tasks with DB lease locking.
type EscalationWorker struct {
	store          *store.Store
	scheduler      *Scheduler
	routeMatcher   *RouteMatcher
	voiceGateway   *voice.VoiceGateway
	channelGateway *channels.ChannelGateway
	deliverer      MessageDeliverer
	metrics        *Metrics
	log            *slog.Logger
	publicURL      string
	tickInterval   time.Duration
	leaseDuration  time.Duration
	voiceCooldown  time.Duration
	now            func() time.Time
	wakeCh         chan struct{}
}

// NewEscalationWorker constructs an EscalationWorker with sensible production defaults.
func NewEscalationWorker(opts EscalationWorkerOpts) *EscalationWorker {
	tick := opts.TickInterval
	if tick <= 0 {
		tick = defaultWorkerTick
	}
	lease := opts.LeaseDuration
	if lease <= 0 {
		lease = defaultWorkerLease
	}
	cooldown := opts.VoiceCooldown
	if cooldown <= 0 {
		cooldown = defaultVoiceCooldown
	}
	sched := opts.Scheduler
	if sched == nil {
		sched = NewScheduler(opts.Store)
	}
	vg := opts.VoiceGateway
	if vg == nil {
		vg = voice.NewVoiceGateway()
	}
	cg := opts.ChannelGateway
	if cg == nil {
		cg = channels.NewChannelGateway()
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &EscalationWorker{
		store:          opts.Store,
		scheduler:      sched,
		routeMatcher:   NewRouteMatcher(opts.Store),
		voiceGateway:   vg,
		channelGateway: cg,
		deliverer:      opts.Deliverer,
		metrics:        opts.Metrics,
		log:            log,
		publicURL:      strings.TrimRight(strings.TrimSpace(opts.PublicURL), "/"),
		tickInterval:   tick,
		leaseDuration:  lease,
		voiceCooldown:  cooldown,
		now:            nowFn,
		wakeCh:         make(chan struct{}, 1),
	}
}

// Wake nudges the worker to process due tasks immediately without waiting for the next ticker.
func (w *EscalationWorker) Wake() {
	if w == nil {
		return
	}
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

// Loop periodically claims and processes due On-Call escalation tasks until ctx is cancelled.
func (w *EscalationWorker) Loop(ctx context.Context) error {
	if w == nil || w.store == nil {
		return nil
	}
	w.ProcessBatch(ctx)

	ticker := time.NewTicker(w.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.ProcessBatch(ctx)
		case <-w.wakeCh:
			w.ProcessBatch(ctx)
		}
	}
}

// ProcessBatch claims due pending tasks once and advances each state machine step.
func (w *EscalationWorker) ProcessBatch(ctx context.Context) int {
	if w == nil || w.store == nil {
		return 0
	}
	now := w.now().UTC()
	tasks, err := w.store.ClaimDueEscalationTasks(ctx, now, w.leaseDuration, 20)
	if err != nil {
		w.log.Warn("oncall claim due escalation tasks failed", slog.Any("err", err))
		return 0
	}
	for _, task := range tasks {
		w.processTask(ctx, task, now)
	}
	return len(tasks)
}

type openGroupContext struct {
	openIncidents []*alertmodel.Incident
	alertCount    int
	title         string
	detailBody    string
}

func (w *EscalationWorker) resolveOpenGroup(ctx context.Context, task *model.EscalationTask, now time.Time) (*openGroupContext, bool) {
	ids := model.ParseMergedIncidentIDs(task)
	incidents, err := w.store.GetAlertIncidentsByIDs(ctx, ids)
	if err != nil {
		task.LockedUntil = nil
		task.LastError = truncateString(err.Error(), 500)
		task.NextRunAt = now.Add(30 * time.Second)
		if saveErr := w.store.SaveEscalationTask(ctx, task); saveErr != nil {
			w.log.Warn("oncall save retry task after incident lookup error failed", slog.Uint64("task_id", task.ID), slog.Any("err", saveErr))
		}
		return nil, false
	}
	if len(incidents) == 0 {
		task.Status = model.TaskStatusCancelled
		task.LockedUntil = nil
		task.LastError = "incident not found"
		w.finishSave(ctx, task, model.TaskStatusCancelled)
		return nil, false
	}

	var openList []*alertmodel.Incident
	anyAcked := false
	for _, inc := range incidents {
		if inc == nil {
			continue
		}
		if inc.Status == alertmodel.StatusOpen {
			openList = append(openList, inc)
		} else if inc.Status == alertmodel.StatusAcknowledged {
			anyAcked = true
		}
	}
	if len(openList) == 0 {
		finalStatus := model.TaskStatusCancelled
		if anyAcked {
			finalStatus = model.TaskStatusAcked
		}
		task.Status = finalStatus
		task.LockedUntil = nil
		w.finishSave(ctx, task, finalStatus)
		return nil, false
	}

	openIDs := make([]uint64, 0, len(openList))
	for _, inc := range openList {
		openIDs = append(openIDs, inc.ID)
	}
	task.IncidentID = openIDs[0]
	task.AlertCount = len(openIDs)
	task.MergedIncidentIDsJSON = model.EncodeMergedIncidentIDs(openIDs)

	title := openList[0].Title
	if title == "" {
		title = task.Title
	}
	if title == "" {
		title = fmt.Sprintf("Incident #%d", openList[0].ID)
	}
	var body string
	if len(openList) > 1 {
		title = fmt.Sprintf("[合并 %d 条告警] %s 等", len(openList), title)
		var b strings.Builder
		limit := len(openList)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			fmt.Fprintf(&b, "- INC-%04d: %s (%s)\n", openList[i].ID, openList[i].Title, openList[i].Severity)
		}
		if len(openList) > limit {
			fmt.Fprintf(&b, "... 共 %d 条未关闭告警", len(openList))
		}
		body = strings.TrimSpace(b.String())
	} else {
		body = fmt.Sprintf("告警单号: INC-%04d | 级别: %s", openList[0].ID, openList[0].Severity)
	}
	if task.RepeatCount > 0 {
		title = fmt.Sprintf("[第 %d 轮提醒] %s", task.RepeatCount+1, title)
	}

	return &openGroupContext{
		openIncidents: openList,
		alertCount:    len(openList),
		title:         title,
		detailBody:    body,
	}, true
}

func (w *EscalationWorker) processTask(ctx context.Context, task *model.EscalationTask, now time.Time) {
	grp, ok := w.resolveOpenGroup(ctx, task, now)
	if !ok {
		return
	}

	cfg, err := w.store.GetVoiceConfig(ctx)
	if err != nil {
		cfg = store.DefaultVoiceGatewayConfig()
	}
	if !cfg.DispatchEnabled {
		task.Status = model.TaskStatusCancelled
		task.LockedUntil = nil
		task.LastError = "oncall dispatch disabled"
		w.finishSave(ctx, task, model.TaskStatusCancelled)
		return
	}

	sched, _ := w.store.GetSchedule(ctx, task.ScheduleID)
	isNight := isNightInSchedule(now, sched)

	var suppressVoiceMW *model.MaintenanceWindow
	if mw, mwErr := MatchMaintenanceWindow(ctx, w.store, w.routeMatcher, task.ScheduleID, w.incidentMatchLabels(grp.openIncidents[0], task), now); mwErr != nil {
		w.log.Warn("oncall maintenance check failed during task processing", slog.Uint64("task_id", task.ID), slog.Any("err", mwErr))
	} else if mw != nil {
		if mw.Action == model.MaintenanceActionSuppressAll {
			if incErr := w.store.IncrementMaintenanceSuppressed(ctx, mw.ID); incErr != nil {
				w.log.Warn("increment maintenance suppressed failed", slog.Uint64("mw_id", mw.ID), slog.Any("err", incErr))
			}
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				Target:         mw.Name,
				Channel:        "maintenance",
				Provider:       "maintenance_window",
				Status:         model.NotifyStatusSuppressed,
				Message:        truncateString(fmt.Sprintf("维护窗口「%s」(#%d) 生效中，升级任务已延后至 %s", mw.Name, mw.ID, mw.EndsAt.Local().Format("15:04")), 500),
				IsNight:        isNight,
				CreatedAt:      now,
			})
			nextAt := mw.EndsAt.UTC()
			if !nextAt.After(now) {
				nextAt = now.Add(30 * time.Second)
			}
			task.NextRunAt = nextAt
			task.LockedUntil = nil
			if saveErr := w.store.SaveEscalationTask(ctx, task); saveErr != nil {
				w.log.Warn("save delayed maintenance task failed", slog.Uint64("task_id", task.ID), slog.Any("err", saveErr))
			}
			return
		}
		if mw.Action == model.MaintenanceActionSuppressVoice {
			suppressVoiceMW = mw
		}
	}

	policies := w.loadPolicies(ctx, task.ScheduleID)
	if task.StepStartedAt.IsZero() {
		task.StepStartedAt = now
	}
	idx := int(task.CurrentStep) - 1
	if idx < 0 {
		idx = 0
		task.CurrentStep = 1
	}
	if idx >= len(policies) {
		w.completeOrRepeat(ctx, task, sched, cfg, grp, isNight, now, false)
		return
	}
	if task.LastVoiceStep != 0 && task.CurrentStep != task.LastVoiceStep {
		task.VoiceRedialCount = 0
		task.AwaitingVoiceGrace = false
	}

	policy := policies[idx]

	var targetUser *store.UserSummary
	var resolveErr error
	if task.PinnedAssigneeID > 0 {
		if usersMap, uErr := w.store.GetUsersByIDs(ctx, []uint64{task.PinnedAssigneeID}); uErr == nil {
			targetUser = usersMap[task.PinnedAssigneeID]
		}
	}
	if targetUser == nil && policy.TargetType == "webhook" {
		w.executeWebhookBroadcastStep(ctx, task, grp, policies, idx, sched, cfg, now, isNight)
		return
	}
	if targetUser == nil {
		targetUser, resolveErr = w.resolveTargetUser(ctx, task.ScheduleID, policy, sched, now)
	}
	if resolveErr != nil || targetUser == nil {
		msg := "no active on-call engineer for target"
		if resolveErr != nil {
			msg = resolveErr.Error()
		}
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     grp.alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: task.CurrentStep,
			Target:         policy.TargetType,
			Channel:        "im_dm",
			Provider:       "none",
			Status:         model.NotifyStatusFailed,
			Message:        truncateString(msg, 500),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		w.advanceTeamStepImmediately(ctx, task, policies, idx, sched, cfg, grp, isNight, now, msg)
		return
	}

	task.AssigneeUserID = targetUser.ID
	rules := w.loadUserLadder(ctx, targetUser.ID, task.Urgency)
	if int(task.LadderStep) <= 0 {
		task.LadderStep = 1
	}
	if int(task.LadderStep) > len(rules) {
		w.advanceAfterLadderComplete(ctx, task, policies, idx, sched, cfg, grp, isNight, now, false)
		return
	}

	rule := rules[int(task.LadderStep)-1]
	voicePlacedWaiting, transientErr := w.executeRuleStep(ctx, task, rule, targetUser, cfg, grp, isNight, now, suppressVoiceMW)
	if transientErr != nil && task.AttemptCount < maxStepRetries {
		task.AttemptCount++
		task.LastError = truncateString(transientErr.Error(), 500)
		backoff := time.Duration(30*(1<<(task.AttemptCount-1))) * time.Second
		task.NextRunAt = now.Add(backoff)
		task.LockedUntil = nil
		if err := w.store.SaveEscalationTask(ctx, task); err != nil {
			w.log.Warn("oncall save retry task failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
		}
		return
	}

	nowCopy := now
	task.LastDispatchedAt = &nowCopy
	task.AttemptCount = 0
	if transientErr != nil {
		task.LastError = truncateString(transientErr.Error(), 500)
	} else {
		task.LastError = ""
	}
	task.LockedUntil = nil

	w.scheduleNextRun(ctx, task, policies, idx, rules, sched, cfg, grp, isNight, now, voicePlacedWaiting)
}

func (w *EscalationWorker) executeWebhookBroadcastStep(ctx context.Context, task *model.EscalationTask, grp *openGroupContext, policies []*model.EscalationPolicy, idx int, sched *model.Schedule, cfg *voice.VoiceGatewayConfig, now time.Time, isNight bool) {
	subject := fmt.Sprintf("[OpsPilot On-Call Step %d] %s", task.CurrentStep, grp.title)
	body := fmt.Sprintf("%s\n升级步骤: %d", grp.detailBody, task.CurrentStep)
	labels := map[string]string{
		"incident_id": fmt.Sprintf("%d", task.IncidentID),
		"severity":    task.Severity,
		"rule":        task.RuleKey,
		"alert_count": fmt.Sprintf("%d", grp.alertCount),
	}
	w.enrichQuickActionLabels(ctx, task.IncidentID, labels)

	var provider string
	var sendErr error
	if w.deliverer == nil {
		provider = "unconfigured"
		sendErr = errors.New("notification deliverer not configured")
	} else {
		provider, sendErr = w.deliverer.Deliver(ctx, "webhook", nil, subject, body, task.Severity, labels)
	}

	status := model.NotifyStatusSent
	msg := "webhook broadcast sent"
	if sendErr != nil {
		status = model.NotifyStatusFailed
		msg = sendErr.Error()
	}
	w.recordLog(ctx, &model.NotificationLog{
		TaskID:         task.ID,
		IncidentID:     task.IncidentID,
		AlertCount:     grp.alertCount,
		ScheduleID:     task.ScheduleID,
		EscalationStep: task.CurrentStep,
		Target:         "webhook",
		Channel:        "webhook",
		Provider:       provider,
		Status:         status,
		Message:        truncateString(msg, 500),
		IsNight:        isNight,
		CreatedAt:      now,
	})

	nowCopy := now
	task.LastDispatchedAt = &nowCopy
	task.LockedUntil = nil
	task.AttemptCount = 0
	w.advanceAfterLadderComplete(ctx, task, policies, idx, sched, cfg, grp, isNight, now, false)
}

func (w *EscalationWorker) executeRuleStep(
	ctx context.Context,
	task *model.EscalationTask,
	rule *model.UserNotificationRule,
	user *store.UserSummary,
	cfg *voice.VoiceGatewayConfig,
	grp *openGroupContext,
	isNight bool,
	now time.Time,
	suppressVoiceMW *model.MaintenanceWindow,
) (bool, error) {
	userName := displayNameOrEmail(user)
	title := grp.title

	switch rule.Channel {
	case "voice_call":
		if suppressVoiceMW != nil {
			if incErr := w.store.IncrementMaintenanceSuppressed(ctx, suppressVoiceMW.ID); incErr != nil {
				w.log.Warn("increment maintenance voice suppressed failed", slog.Uint64("mw_id", suppressVoiceMW.ID), slog.Any("err", incErr))
			}
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         maskPhone(user.Phone),
				Channel:        "voice_call",
				Provider:       "maintenance_window",
				Status:         model.NotifyStatusSuppressed,
				Message:        truncateString(fmt.Sprintf("维护窗口「%s」(#%d) 生效中，已静默本次语音外呼", suppressVoiceMW.Name, suppressVoiceMW.ID), 500),
				IsNight:        isNight,
				CreatedAt:      now,
			})
			return false, nil
		}

		isRedial := task.VoiceRedialCount > 0 &&
			task.LastVoiceStep == task.CurrentStep &&
			task.LastVoiceLadderStep == task.LadderStep &&
			task.LastVoiceUserID == user.ID
		if !isRedial {
			cooldownSince := now.Add(-w.voiceCooldown)
			throttled, tErr := w.store.HasRecentVoiceCall(ctx, user.ID, cooldownSince)
			if tErr == nil && throttled {
				w.recordLog(ctx, &model.NotificationLog{
					TaskID:         task.ID,
					IncidentID:     task.IncidentID,
					AlertCount:     grp.alertCount,
					ScheduleID:     task.ScheduleID,
					EscalationStep: task.CurrentStep,
					UserID:         user.ID,
					UserName:       userName,
					Target:         maskPhone(user.Phone),
					Channel:        "voice_call",
					Provider:       cfg.Provider,
					Status:         model.NotifyStatusThrottled,
					Message:        fmt.Sprintf("throttled: voice call cooldown (%v) active", w.voiceCooldown),
					IsNight:        isNight,
					CreatedAt:      now,
				})
				return false, nil
			}
		}

		if strings.TrimSpace(user.Phone) == "" {
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         user.Email,
				Channel:        "voice_call",
				Provider:       cfg.Provider,
				Status:         model.NotifyStatusFailed,
				Message:        "user phone number not configured",
				IsNight:        isNight,
				CreatedAt:      now,
			})
			return false, nil
		}

		callTitle := title
		if isRedial {
			callTitle = fmt.Sprintf("[第 %d 次重拨] %s", task.VoiceRedialCount, title)
		}
		callID := fmt.Sprintf("call_%d_%d", task.IncidentID, now.UnixNano())
		res, err := w.voiceGateway.SendVoiceCall(ctx, cfg, &voice.CallRequest{
			CallID:      callID,
			PhoneNumber: user.Phone,
			Urgency:     task.Urgency,
			Title:       callTitle,
			Severity:    task.Severity,
		})
		if res != nil && res.FallbackFrom != "" {
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         maskPhone(user.Phone),
				Channel:        "voice_call",
				Provider:       res.FallbackFrom,
				CallID:         callID,
				Status:         model.NotifyStatusFailed,
				Message:        truncateString(fmt.Sprintf("[容灾切换至 %s] %s", res.Provider, res.PrimaryError), 500),
				IsNight:        isNight,
				CreatedAt:      now,
			})
			outcome := "success"
			if err != nil {
				outcome = "failed"
			}
			w.metrics.incVoiceFailover(res.FallbackFrom, res.Provider, outcome)
		}
		if err != nil {
			provider := cfg.Provider
			if res != nil && res.Provider != "" {
				provider = res.Provider
			}
			msg := err.Error()
			if res != nil && res.Message != "" {
				msg = res.Message
			}
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         maskPhone(user.Phone),
				Channel:        "voice_call",
				Provider:       provider,
				CallID:         callID,
				Status:         model.NotifyStatusFailed,
				Message:        truncateString(msg, 500),
				IsNight:        isNight,
				CreatedAt:      now,
			})
			if isPermanentVoiceConfigErr(msg) && (res == nil || res.FallbackFrom == "") {
				return false, nil
			}
			return false, err
		}

		// Record call log. Note: mock provider returns dtmf_confirmed in simulation,
		// while automated pipeline NEVER auto-acks real alert incidents from mock mode;
		// real ack comes from signed Aliyun/Tencent callbacks or human action.
		status := model.NotifyStatusCalling
		provider := cfg.Provider
		outCallID := ""
		dtmf := ""
		dur := 0
		msg := ""
		if res != nil {
			if res.Status != "" {
				status = res.Status
			}
			if res.Provider != "" {
				provider = res.Provider
			}
			outCallID = res.OutCallID
			dtmf = res.DTMFCode
			dur = res.DurationSecs
			msg = res.Message
		}
		if isRedial {
			msg = fmt.Sprintf("[第 %d 次自动重拨] %s", task.VoiceRedialCount, msg)
		}
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     grp.alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: task.CurrentStep,
			UserID:         user.ID,
			UserName:       userName,
			Target:         maskPhone(user.Phone),
			Channel:        "voice_call",
			Provider:       provider,
			CallID:         callID,
			OutCallID:      outCallID,
			Status:         status,
			DTMFCode:       dtmf,
			DurationSecs:   dur,
			Message:        truncateString(msg, 500),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		task.LastVoiceCallID = callID
		task.LastVoiceStep = task.CurrentStep
		task.LastVoiceLadderStep = task.LadderStep
		task.LastVoiceUserID = user.ID
		task.AwaitingVoiceGrace = false
		return status == model.NotifyStatusCalling, nil

	case "im_dm", "im_group_at", "webhook":
		subject := fmt.Sprintf("[OpsPilot On-Call] %s (%s)", title, strings.ToUpper(task.Severity))
		body := fmt.Sprintf("%s\n值班人: %s | 升级步骤: %d", grp.detailBody, userName, task.CurrentStep)
		labels := map[string]string{
			"incident_id": fmt.Sprintf("%d", task.IncidentID),
			"severity":    task.Severity,
			"rule":        task.RuleKey,
			"assignee":    userName,
			"alert_count": fmt.Sprintf("%d", grp.alertCount),
		}
		w.enrichQuickActionLabels(ctx, task.IncidentID, labels)
		if w.deliverer == nil {
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         userName,
				Channel:        rule.Channel,
				Provider:       "unconfigured",
				Status:         model.NotifyStatusFailed,
				Message:        "notification channel not configured",
				IsNight:        isNight,
				CreatedAt:      now,
			})
			return false, nil
		}
		provider, err := w.deliverer.Deliver(ctx, rule.Channel, user, subject, body, task.Severity, labels)
		if err != nil {
			if provider == "" {
				provider = "notify_router"
			}
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         userName,
				Channel:        rule.Channel,
				Provider:       provider,
				Status:         model.NotifyStatusFailed,
				Message:        truncateString(err.Error(), 500),
				IsNight:        isNight,
				CreatedAt:      now,
			})
			if strings.Contains(err.Error(), "no notification channel configured") {
				return false, nil
			}
			return false, err
		}
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     grp.alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: task.CurrentStep,
			UserID:         user.ID,
			UserName:       userName,
			Target:         userName,
			Channel:        rule.Channel,
			Provider:       provider,
			Status:         model.NotifyStatusSent,
			Message:        fmt.Sprintf("dispatched via %s", provider),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		return false, nil

	case "sms":
		if strings.TrimSpace(user.Phone) == "" {
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         user.Email,
				Channel:        "sms",
				Provider:       "unconfigured",
				Status:         model.NotifyStatusFailed,
				Message:        "user phone number not configured",
				IsNight:        isNight,
				CreatedAt:      now,
			})
			return false, nil
		}
		chCfg, cfgErr := w.store.GetChannelConfig(ctx)
		if cfgErr != nil || chCfg == nil {
			chCfg = store.DefaultChannelGatewayConfig()
		}
		cg := w.channelGateway
		if cg == nil {
			cg = channels.NewChannelGateway()
		}
		smsRes, smsErr := cg.SendSMS(ctx, chCfg, &channels.SMSRequest{
			PhoneNumber: user.Phone,
			Urgency:     task.Urgency,
			Title:       title,
			IncidentID:  task.IncidentID,
		})
		provider := chCfg.SMSProvider
		if provider == "" {
			provider = "unconfigured"
		}
		msg := ""
		status := model.NotifyStatusSent
		if smsRes != nil {
			if smsRes.Provider != "" {
				provider = smsRes.Provider
			}
			msg = smsRes.Message
		}
		if smsErr != nil {
			status = model.NotifyStatusFailed
			if msg == "" {
				msg = smsErr.Error()
			}
		}
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     grp.alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: task.CurrentStep,
			UserID:         user.ID,
			UserName:       userName,
			Target:         maskPhone(user.Phone),
			Channel:        "sms",
			Provider:       provider,
			OutCallID:      func() string { if smsRes != nil { return smsRes.BizID }; return "" }(),
			Status:         status,
			Message:        truncateString(msg, 500),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		if smsErr != nil && !channels.IsPermanentConfigErr(msg) {
			return false, smsErr
		}
		return false, nil

	case "email":
		if strings.TrimSpace(user.Email) == "" {
			w.recordLog(ctx, &model.NotificationLog{
				TaskID:         task.ID,
				IncidentID:     task.IncidentID,
				AlertCount:     grp.alertCount,
				ScheduleID:     task.ScheduleID,
				EscalationStep: task.CurrentStep,
				UserID:         user.ID,
				UserName:       userName,
				Target:         maskPhone(user.Phone),
				Channel:        "email",
				Provider:       "unconfigured",
				Status:         model.NotifyStatusFailed,
				Message:        "user email not configured",
				IsNight:        isNight,
				CreatedAt:      now,
			})
			return false, nil
		}
		chCfg, cfgErr := w.store.GetChannelConfig(ctx)
		if cfgErr != nil || chCfg == nil {
			chCfg = store.DefaultChannelGatewayConfig()
		}
		cg := w.channelGateway
		if cg == nil {
			cg = channels.NewChannelGateway()
		}
		actionLinks := map[string]string{}
		if w.publicURL != "" {
			labels := map[string]string{}
			w.enrichQuickActionLabels(ctx, task.IncidentID, labels)
			if u := labels["oncall_ack_url"]; u != "" {
				actionLinks["认领告警"] = u
			}
			if u := labels["oncall_silence_url"]; u != "" {
				actionLinks["静音 30 分钟"] = u
			}
			if u := labels["oncall_escalate_url"]; u != "" {
				actionLinks["升级二线"] = u
			}
		}
		subject := fmt.Sprintf("[OpsPilot On-Call] %s (%s)", title, strings.ToUpper(task.Severity))
		textBody := fmt.Sprintf("%s\n值班人: %s | 升级步骤: %d", grp.detailBody, userName, task.CurrentStep)
		emailRes, emailErr := cg.SendEmail(ctx, chCfg, &channels.EmailRequest{
			To:          user.Email,
			Subject:     subject,
			TextBody:    textBody,
			Urgency:     task.Urgency,
			IncidentID:  task.IncidentID,
			ActionLinks: actionLinks,
		})
		provider := "smtp"
		msg := ""
		status := model.NotifyStatusSent
		if emailRes != nil {
			if emailRes.Provider != "" {
				provider = emailRes.Provider
			}
			msg = emailRes.Message
		}
		if emailErr != nil {
			status = model.NotifyStatusFailed
			if msg == "" {
				msg = emailErr.Error()
			}
		}
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     grp.alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: task.CurrentStep,
			UserID:         user.ID,
			UserName:       userName,
			Target:         user.Email,
			Channel:        "email",
			Provider:       provider,
			Status:         status,
			Message:        truncateString(msg, 500),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		if emailErr != nil && !channels.IsPermanentConfigErr(msg) {
			return false, emailErr
		}
		return false, nil

	default:
		// Future channels without an active external gateway: fail honestly rather than fabricating success.
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     grp.alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: task.CurrentStep,
			UserID:         user.ID,
			UserName:       userName,
			Target:         fallbackTarget(user),
			Channel:        rule.Channel,
			Provider:       "unconfigured",
			Status:         model.NotifyStatusFailed,
			Message:        fmt.Sprintf("provider for channel %s is not configured", rule.Channel),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		return false, nil
	}
}

func (w *EscalationWorker) enrichQuickActionLabels(ctx context.Context, incidentID uint64, labels map[string]string) {
	if w == nil || labels == nil || w.publicURL == "" {
		return
	}
	labels["console_url"] = w.publicURL
	if incidentID == 0 || w.store == nil {
		return
	}
	tok, err := w.store.EnsureChatOpsCallbackToken(ctx)
	if err != nil {
		w.log.Warn("ensure chatops callback token failed", slog.Uint64("incident_id", incidentID), slog.Any("err", err))
		return
	}
	if tok == "" {
		return
	}
	if u := SignQuickActionURL(w.publicURL, tok, incidentID, "ack", 24*time.Hour); u != "" {
		labels["oncall_ack_url"] = u
	}
	if u := SignQuickActionURL(w.publicURL, tok, incidentID, "silence", 24*time.Hour); u != "" {
		labels["oncall_silence_url"] = u
	}
	if u := SignQuickActionURL(w.publicURL, tok, incidentID, "escalate", 24*time.Hour); u != "" {
		labels["oncall_escalate_url"] = u
	}
}

func (w *EscalationWorker) loadPolicies(ctx context.Context, scheduleID uint64) []*model.EscalationPolicy {
	policies, err := w.store.ListEscalationPolicies(ctx, scheduleID)
	if err != nil || len(policies) == 0 {
		return []*model.EscalationPolicy{
			{ScheduleID: scheduleID, Name: "一线主值班即时通知", StepNumber: 1, WaitSeconds: 0, TargetType: "primary_oncall"},
			{ScheduleID: scheduleID, Name: "未响应升级至二线技术专家", StepNumber: 2, WaitSeconds: 300, TargetType: "secondary_oncall"},
			{ScheduleID: scheduleID, Name: "仍未响应升级至SRE应急负责人", StepNumber: 3, WaitSeconds: 900, TargetType: "fallback_lead"},
		}
	}
	sort.Slice(policies, func(i, j int) bool {
		if policies[i].StepNumber == policies[j].StepNumber {
			return policies[i].ID < policies[j].ID
		}
		return policies[i].StepNumber < policies[j].StepNumber
	})
	return policies
}

func (w *EscalationWorker) loadUserLadder(ctx context.Context, userID uint64, urgency string) []*model.UserNotificationRule {
	all, err := w.store.ListUserNotificationRules(ctx, userID, urgency)
	var enabled []*model.UserNotificationRule
	if err == nil {
		for _, r := range all {
			if r != nil && r.Enabled {
				enabled = append(enabled, r)
			}
		}
	}
	if len(enabled) > 0 {
		return enabled
	}
	if urgency == "high" {
		return []*model.UserNotificationRule{
			{UserID: userID, Urgency: "high", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
			{UserID: userID, Urgency: "high", StepNumber: 2, DelayMinutes: 2, Channel: "voice_call", Enabled: true},
		}
	}
	return []*model.UserNotificationRule{
		{UserID: userID, Urgency: "low", StepNumber: 1, DelayMinutes: 0, Channel: "im_dm", Enabled: true},
	}
}

func (w *EscalationWorker) resolveTargetUser(ctx context.Context, scheduleID uint64, policy *model.EscalationPolicy, sched *model.Schedule, now time.Time) (*store.UserSummary, error) {
	var userID uint64
	switch policy.TargetType {
	case "primary_oncall":
		live, err := w.scheduler.GetLiveStatus(ctx, scheduleID, now)
		if err != nil {
			return nil, fmt.Errorf("resolve live primary on-call: %w", err)
		}
		if live != nil && live.PrimaryUser != nil {
			userID = live.PrimaryUser.UserID
		}
	case "secondary_oncall":
		live, err := w.scheduler.GetLiveStatus(ctx, scheduleID, now)
		if err != nil {
			return nil, fmt.Errorf("resolve live secondary on-call: %w", err)
		}
		if live != nil && live.SecondaryUser != nil {
			userID = live.SecondaryUser.UserID
		}
	case "specific_user":
		if policy.TargetID != nil {
			userID = *policy.TargetID
		}
	case "fallback_lead":
		if sched != nil && sched.CreatedBy > 0 {
			userID = sched.CreatedBy
		} else {
			users, err := w.store.ListAllUsers(ctx)
			if err == nil && len(users) > 0 {
				return users[0], nil
			}
		}
	default:
		return nil, fmt.Errorf("unsupported escalation target type %q", policy.TargetType)
	}

	if userID == 0 {
		return nil, fmt.Errorf("no user on shift for %s", policy.TargetType)
	}
	usersMap, err := w.store.GetUsersByIDs(ctx, []uint64{userID})
	if err != nil {
		return nil, err
	}
	user, ok := usersMap[userID]
	if !ok || user == nil {
		return nil, fmt.Errorf("user %d not found", userID)
	}
	return user, nil
}

func (w *EscalationWorker) scheduleNextRun(
	ctx context.Context,
	task *model.EscalationTask,
	policies []*model.EscalationPolicy,
	idx int,
	rules []*model.UserNotificationRule,
	sched *model.Schedule,
	cfg *voice.VoiceGatewayConfig,
	grp *openGroupContext,
	isNight bool,
	now time.Time,
	voicePlacedWaiting bool,
) {
	hasNextTeam := idx+1 < len(policies)
	var teamEscalateAt time.Time
	if hasNextTeam {
		wait := time.Duration(policies[idx+1].WaitSeconds) * time.Second
		if wait <= 0 {
			wait = 5 * time.Minute
		}
		teamEscalateAt = task.StepStartedAt.Add(wait)
		if teamEscalateAt.Before(now) {
			teamEscalateAt = now
		}
	}

	if int(task.LadderStep) < len(rules) {
		nextRule := rules[int(task.LadderStep)]
		nextLadderAt := task.StepStartedAt.Add(time.Duration(nextRule.DelayMinutes) * time.Minute)
		if nextLadderAt.Before(now) {
			nextLadderAt = now
		}
		if hasNextTeam && nextLadderAt.After(teamEscalateAt) {
			nextAt := teamEscalateAt
			if voicePlacedWaiting && cfg != nil && cfg.VoiceRedialMaxAttempts > 0 && int(task.VoiceRedialCount) < cfg.VoiceRedialMaxAttempts && nextAt.Before(now.Add(90*time.Second)) {
				nextAt = now.Add(90 * time.Second)
				task.AwaitingVoiceGrace = true
			} else {
				task.AwaitingVoiceGrace = false
			}
			task.CurrentStep++
			task.LadderStep = 1
			task.PinnedAssigneeID = 0
			task.StepStartedAt = nextAt
			task.NextRunAt = nextAt
		} else {
			task.LadderStep++
			task.NextRunAt = nextLadderAt
		}
		if err := w.store.SaveEscalationTask(ctx, task); err != nil {
			w.log.Warn("oncall save next ladder task failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
		}
		return
	}

	w.advanceAfterLadderComplete(ctx, task, policies, idx, sched, cfg, grp, isNight, now, voicePlacedWaiting)
}

func (w *EscalationWorker) advanceAfterLadderComplete(
	ctx context.Context,
	task *model.EscalationTask,
	policies []*model.EscalationPolicy,
	idx int,
	sched *model.Schedule,
	cfg *voice.VoiceGatewayConfig,
	grp *openGroupContext,
	isNight bool,
	now time.Time,
	voicePlacedWaiting bool,
) {
	if idx+1 < len(policies) {
		wait := time.Duration(policies[idx+1].WaitSeconds) * time.Second
		if wait <= 0 {
			wait = 5 * time.Minute
		}
		nextTeamAt := task.StepStartedAt.Add(wait)
		if nextTeamAt.Before(now) {
			nextTeamAt = now
		}
		if voicePlacedWaiting && cfg != nil && cfg.VoiceRedialMaxAttempts > 0 && int(task.VoiceRedialCount) < cfg.VoiceRedialMaxAttempts && nextTeamAt.Before(now.Add(90*time.Second)) {
			nextTeamAt = now.Add(90 * time.Second)
			task.AwaitingVoiceGrace = true
		} else {
			task.AwaitingVoiceGrace = false
		}
		task.CurrentStep++
		task.LadderStep = 1
		task.PinnedAssigneeID = 0
		task.StepStartedAt = nextTeamAt
		task.NextRunAt = nextTeamAt
		task.LockedUntil = nil
		if err := w.store.SaveEscalationTask(ctx, task); err != nil {
			w.log.Warn("oncall save next escalation step failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
		}
		return
	}
	w.completeOrRepeat(ctx, task, sched, cfg, grp, isNight, now, voicePlacedWaiting)
}

func (w *EscalationWorker) advanceTeamStepImmediately(
	ctx context.Context,
	task *model.EscalationTask,
	policies []*model.EscalationPolicy,
	idx int,
	sched *model.Schedule,
	cfg *voice.VoiceGatewayConfig,
	grp *openGroupContext,
	isNight bool,
	now time.Time,
	reason string,
) {
	task.AttemptCount = 0
	task.LastError = truncateString(reason, 500)
	task.LockedUntil = nil
	task.VoiceRedialCount = 0
	task.AwaitingVoiceGrace = false
	task.LastVoiceCallID = ""
	if idx+1 < len(policies) {
		task.CurrentStep++
		task.LadderStep = 1
		task.PinnedAssigneeID = 0
		task.StepStartedAt = now
		task.NextRunAt = now
		if err := w.store.SaveEscalationTask(ctx, task); err != nil {
			w.log.Warn("oncall save skipped step failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
		}
		return
	}
	w.completeOrRepeat(ctx, task, sched, cfg, grp, isNight, now, false)
}

func (w *EscalationWorker) completeOrRepeat(
	ctx context.Context,
	task *model.EscalationTask,
	sched *model.Schedule,
	cfg *voice.VoiceGatewayConfig,
	grp *openGroupContext,
	isNight bool,
	now time.Time,
	voicePlacedWaiting bool,
) {
	// 1. Final-step voice callback grace window so unanswered calls still get a chance to redial before cycling or completing.
	if voicePlacedWaiting && !task.AwaitingVoiceGrace && cfg != nil && cfg.VoiceRedialMaxAttempts > 0 && int(task.VoiceRedialCount) < cfg.VoiceRedialMaxAttempts {
		task.AwaitingVoiceGrace = true
		task.CurrentStep++
		task.NextRunAt = now.Add(90 * time.Second)
		task.LockedUntil = nil
		if err := w.store.SaveEscalationTask(ctx, task); err != nil {
			w.log.Warn("oncall save voice grace window failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
		}
		return
	}

	// 2. Escalation chain repeat when configured on the schedule and repeat budget remains.
	if sched != nil && sched.EscalationRepeatCount > 0 && task.RepeatCount < sched.EscalationRepeatCount {
		repeatInterval := time.Duration(sched.EscalationRepeatIntervalSeconds) * time.Second
		if sched.EscalationRepeatIntervalSeconds == 0 {
			repeatInterval = 5 * time.Minute
		} else if repeatInterval < 30*time.Second {
			repeatInterval = 30 * time.Second
		}
		task.RepeatCount++
		task.CurrentStep = 1
		task.LadderStep = 1
		task.PinnedAssigneeID = 0
		task.AssigneeUserID = 0
		task.VoiceRedialCount = 0
		task.LastVoiceCallID = ""
		task.AwaitingVoiceGrace = false
		task.AttemptCount = 0
		nextAt := now.Add(repeatInterval)
		task.StepStartedAt = nextAt
		task.NextRunAt = nextAt
		task.LockedUntil = nil
		if err := w.store.SaveEscalationTask(ctx, task); err != nil {
			w.log.Warn("oncall save repeat escalation task failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
			return
		}
		alertCount := 1
		if grp != nil && grp.alertCount > 0 {
			alertCount = grp.alertCount
		}
		w.recordLog(ctx, &model.NotificationLog{
			TaskID:         task.ID,
			IncidentID:     task.IncidentID,
			AlertCount:     alertCount,
			ScheduleID:     task.ScheduleID,
			EscalationStep: 1,
			Target:         sched.Name,
			Channel:        "escalation",
			Provider:       "repeat",
			Status:         model.NotifyStatusSent,
			Message:        truncateString(fmt.Sprintf("升级链第 %d 轮未获认领，将于 %d 秒后开启第 %d 轮循环通知", task.RepeatCount, int(repeatInterval.Seconds()), task.RepeatCount+1), 500),
			IsNight:        isNight,
			CreatedAt:      now,
		})
		return
	}

	// 3. Exhausted escalation chain -> mark completed.
	task.Status = model.TaskStatusCompleted
	task.AwaitingVoiceGrace = false
	task.LockedUntil = nil
	w.finishSave(ctx, task, model.TaskStatusCompleted)
}

func (w *EscalationWorker) finishSave(ctx context.Context, task *model.EscalationTask, outcome string) {
	if err := w.store.SaveEscalationTask(ctx, task); err != nil {
		w.log.Warn("oncall save completed task failed", slog.Uint64("task_id", task.ID), slog.Any("err", err))
		return
	}
	w.metrics.incEscalation(outcome)
}

func (w *EscalationWorker) recordLog(ctx context.Context, entry *model.NotificationLog) {
	if err := w.store.CreateNotificationLog(ctx, entry); err != nil {
		w.log.Warn("oncall create notification log failed", slog.Uint64("incident_id", entry.IncidentID), slog.Any("err", err))
	}
	w.metrics.incNotification(entry.Channel, entry.Provider, entry.Status)
}

func isNightInSchedule(now time.Time, sched *model.Schedule) bool {
	tz := "Asia/Shanghai"
	if sched != nil && sched.Timezone != "" {
		tz = sched.Timezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	h := now.In(loc).Hour()
	return h >= 22 || h < 8
}

func isPermanentVoiceConfigErr(msg string) bool {
	return strings.Contains(msg, "未配置") || strings.Contains(msg, "不能为空") || strings.Contains(msg, "not configured")
}

func maskPhone(p string) string {
	p = strings.TrimSpace(p)
	if len(p) < 7 {
		return p
	}
	return p[:3] + "****" + p[len(p)-4:]
}

func displayNameOrEmail(u *store.UserSummary) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Email
}

func fallbackTarget(u *store.UserSummary) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.Phone) != "" {
		return maskPhone(u.Phone)
	}
	return u.Email
}

func truncateString(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (w *EscalationWorker) incidentMatchLabels(inc *alertmodel.Incident, task *model.EscalationTask) map[string]string {
	out := make(map[string]string)
	if inc != nil {
		if parsed, err := inc.Labels(); err == nil {
			for k, v := range parsed {
				out[k] = v
			}
		}
		if inc.Severity != "" && out["severity"] == "" {
			out["severity"] = inc.Severity
		}
		if inc.Rule != "" && out["rule"] == "" {
			out["rule"] = inc.Rule
		}
	}
	if task != nil {
		if task.Severity != "" && out["severity"] == "" {
			out["severity"] = task.Severity
		}
		if task.RuleKey != "" && out["rule"] == "" {
			out["rule"] = task.RuleKey
		}
	}
	return out
}
