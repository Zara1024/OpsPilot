package oncall

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/manager/biz/oncall/channels"
	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/oncall"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type ChatOpsActionRequest struct {
	Action     string `json:"action"` // ack, silence, escalate, reassign, warroom
	IncidentID string `json:"incident_id"`
	OperatorID uint64 `json:"operator_id"`
	Note       string `json:"note,omitempty"`
	TargetUser uint64 `json:"target_user,omitempty"`
}

type ChatOpsActionResult struct {
	OK             bool   `json:"ok"`
	Action         string `json:"action"`
	StatusText     string `json:"status_text"`
	Message        string `json:"message"`
	EscalationHalt bool   `json:"escalation_halt"`
	WarRoomURL     string `json:"war_room_url,omitempty"`
}

type IncidentSummary struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Severity        string     `json:"severity"` // P0, P1, P2, P3
	ScheduleID      uint64     `json:"schedule_id"`
	ScheduleName    string     `json:"schedule_name"`
	CurrentAssignee string     `json:"current_assignee"`
	CurrentTier     uint8      `json:"current_tier"`
	Status          string     `json:"status"` // firing, acknowledged, resolved, silenced
	StartedAt       time.Time  `json:"started_at"`
	AckedAt         *time.Time `json:"acked_at,omitempty"`
	AckedBy         string     `json:"acked_by,omitempty"`
	EscalationStep  uint8      `json:"escalation_step"`
	WarRoomURL      string     `json:"war_room_url,omitempty"`
}

type EscalationEngine struct {
	store          *store.Store
	scheduler      *Scheduler
	worker         *EscalationWorker
	channelGateway *channels.ChannelGateway
	deliverer      MessageDeliverer
	publicURL      string
}

func NewEscalationEngine(s *store.Store, sched *Scheduler) *EscalationEngine {
	return &EscalationEngine{
		store:          s,
		scheduler:      sched,
		channelGateway: channels.NewChannelGateway(),
	}
}

// SetWorker attaches the background escalation worker so manual escalations and reassignments wake immediately.
func (ee *EscalationEngine) SetWorker(w *EscalationWorker) {
	ee.worker = w
}

// SetChannelGateway attaches the multi-channel gateway for real War Room provisioning.
func (ee *EscalationEngine) SetChannelGateway(cg *channels.ChannelGateway) {
	if cg != nil {
		ee.channelGateway = cg
	}
}

// SetDeliverer attaches the IM/Webhook deliverer so created War Rooms can be announced in active channels.
func (ee *EscalationEngine) SetDeliverer(d MessageDeliverer) {
	ee.deliverer = d
}

// SetPublicURL configures the console base URL used for War Room links and fallback links.
func (ee *EscalationEngine) SetPublicURL(publicURL string) {
	ee.publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
}

// HandleAction processes a ChatOps / On-Call dashboard / IM card response action on real alert incidents.
func (ee *EscalationEngine) HandleAction(ctx context.Context, req ChatOpsActionRequest) (*ChatOpsActionResult, error) {
	rawID := strings.TrimPrefix(strings.TrimSpace(req.IncidentID), "INC-")
	rawID = strings.TrimLeft(rawID, "0")
	if rawID == "" {
		rawID = "0"
	}
	incID, _ := strconv.ParseUint(rawID, 10, 64)

	switch req.Action {
	case "ack":
		if incID > 0 {
			ackNote := req.Note
			if task, taskErr := ee.store.FindPendingTaskByIncident(ctx, incID); taskErr == nil && task != nil && task.AlertCount > 1 {
				ids := model.ParseMergedIncidentIDs(task)
				if err := ee.store.AckAlertIncidentsBatch(ctx, ids, req.OperatorID, ackNote); err != nil {
					return nil, fmt.Errorf("batch ack alert incidents for %d: %w", incID, err)
				}
			} else if err := ee.store.AckAlertIncident(ctx, incID, req.OperatorID, ackNote); err != nil {
				return nil, fmt.Errorf("ack alert incident %d: %w", incID, err)
			}
		}
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "ack",
			StatusText:     "已认领 (Acknowledged)",
			Message:        "值班工程师已认领接单，超时升级与后续外呼已自动阻断。",
			EscalationHalt: true,
		}, nil

	case "silence":
		if incID > 0 {
			if err := ee.store.SilenceAlertIncident(ctx, incID, req.OperatorID, 30*time.Minute, req.Note); err != nil {
				return nil, fmt.Errorf("silence alert incident %d: %w", incID, err)
			}
		}
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "silence",
			StatusText:     "已静音 30 分钟 (Silenced)",
			Message:        "该告警已进入静音保护期 (30m)，期间不再触发新通知推送。",
			EscalationHalt: true,
		}, nil

	case "escalate":
		if incID > 0 {
			if err := ee.store.EscalateAlertIncident(ctx, incID, req.OperatorID, req.Note); err != nil {
				return nil, fmt.Errorf("escalate alert incident %d: %w", incID, err)
			}
			if advanced, err := ee.store.TriggerImmediateEscalationByIncident(ctx, incID, time.Now().UTC()); err == nil && advanced && ee.worker != nil {
				ee.worker.Wake()
			}
		}
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "escalate",
			StatusText:     "已手动升级 (Escalated)",
			Message:        "已立即拉起下一级技术支持/二线技术专家，电话与强触达已派发。",
			EscalationHalt: false,
		}, nil

	case "reassign":
		if incID == 0 || req.TargetUser == 0 {
			return nil, fmt.Errorf("%w: incident_id and target_user are required for reassign", errs.ErrInvalid)
		}
		targetName, err := ee.store.ReassignAlertIncident(ctx, incID, req.OperatorID, req.TargetUser, req.Note)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errs.ErrNotFound
			}
			return nil, fmt.Errorf("reassign alert incident %d: %w", incID, err)
		}
		if ee.worker != nil {
			ee.worker.Wake()
		}
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "reassign",
			StatusText:     "已转派 (Reassigned)",
			Message:        fmt.Sprintf("告警已成功转派至值班工程师 %s，已立即唤醒通知流水线触达新责任人。", targetName),
			EscalationHalt: false,
		}, nil

	case "warroom":
		if incID == 0 {
			return nil, fmt.Errorf("%w: incident_id is required to create war room", errs.ErrInvalid)
		}
		return ee.handleWarRoom(ctx, incID, req.OperatorID)

	default:
		return nil, fmt.Errorf("%w: unknown action %q", errs.ErrInvalid, req.Action)
	}
}

func (ee *EscalationEngine) handleWarRoom(ctx context.Context, incID, operatorID uint64) (*ChatOpsActionResult, error) {
	if existing, err := ee.store.GetWarRoomByIncident(ctx, incID); err == nil && existing != nil && existing.RoomURL != "" {
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "warroom",
			StatusText:     "应急作战室已就绪 (WarRoom)",
			Message:        fmt.Sprintf("应急作战室「%s」已存在，可直接打开入口链接进入协同排障。", existing.RoomName),
			EscalationHalt: false,
			WarRoomURL:     existing.RoomURL,
		}, nil
	}

	inc, err := ee.store.GetAlertIncident(ctx, incID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, fmt.Errorf("load incident %d for war room: %w", incID, err)
	}

	operatorName := "On-Call"
	if operatorID > 0 {
		if usersMap, uErr := ee.store.GetUsersByIDs(ctx, []uint64{operatorID}); uErr == nil {
			if u := usersMap[operatorID]; u != nil {
				operatorName = displayNameOrEmail(u)
			}
		}
	}

	cfg, cfgErr := ee.store.GetChannelConfig(ctx)
	if cfgErr != nil || cfg == nil {
		cfg = store.DefaultChannelGatewayConfig()
	}
	cg := ee.channelGateway
	if cg == nil {
		cg = channels.NewChannelGateway()
	}

	res, err := cg.CreateWarRoom(ctx, cfg, &channels.WarRoomCreateRequest{
		IncidentID: incID,
		Title:      inc.Title,
		Severity:   inc.Severity,
		RuleName:   inc.RuleName,
		Operator:   operatorName,
		ConsoleURL: ee.publicURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create war room for incident %d: %w", incID, err)
	}

	saved, err := ee.store.UpsertWarRoom(ctx, &model.WarRoom{
		IncidentID:     incID,
		Provider:       res.Provider,
		ExternalChatID: res.ExternalChatID,
		RoomName:       res.RoomName,
		RoomURL:        res.RoomURL,
		CreatedBy:      operatorID,
		Status:         "active",
	})
	if err != nil {
		return nil, err
	}

	if ee.deliverer != nil && saved.RoomURL != "" {
		subject := fmt.Sprintf("[应急作战室] INC-%04d %s", incID, inc.Title)
		body := fmt.Sprintf("发起人: %s | 协同房间: %s\n入口链接: %s", operatorName, saved.RoomName, saved.RoomURL)
		labels := map[string]string{
			"incident_id": strconv.FormatUint(incID, 10),
			"console_url": ee.publicURL,
		}
		if _, dErr := ee.deliverer.Deliver(ctx, "im_group_at", nil, subject, body, inc.Severity, labels); dErr != nil {
			slog.Warn("broadcast war room announcement failed", slog.Uint64("incident_id", incID), slog.Any("err", dErr))
		}
	}

	return &ChatOpsActionResult{
		OK:             true,
		Action:         "warroom",
		StatusText:     "应急作战室已就绪 (WarRoom)",
		Message:        res.Message,
		EscalationHalt: false,
		WarRoomURL:     saved.RoomURL,
	}, nil
}

// VoiceCallSimulationResult simulates voice calls for SRE phone alerting.
type VoiceCallSimulationResult struct {
	CallID       string `json:"call_id"`
	PhoneNumber  string `json:"phone_number"`
	TTSContent   string `json:"tts_content"`
	Provider     string `json:"provider"`
	Status       string `json:"status"` // answered, dtmf_confirmed, busy
	DTMFCode     string `json:"dtmf_code"` // "1" = Ack, "2" = Escalate
	DurationSecs int    `json:"duration_secs"`
}

func (ee *EscalationEngine) SimulateVoiceCall(phone, urgency string) *VoiceCallSimulationResult {
	return &VoiceCallSimulationResult{
		CallID:       fmt.Sprintf("call_%d", time.Now().UnixNano()),
		PhoneNumber:  phone,
		TTSContent:   fmt.Sprintf("【OpsPilot值班生命线】您好，生产环境发生 %s 紧急告警，请在滴声后按 1 确认接单，按 2 请求升级至二线技术专家。", urgency),
		Provider:     "Aliyun/Tencent Voice Cloud SDK (Emulated)",
		Status:       "dtmf_confirmed",
		DTMFCode:     "1",
		DurationSecs: 18,
	}
}
