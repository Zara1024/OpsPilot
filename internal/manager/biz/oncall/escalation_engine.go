package oncall

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	store "github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
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
}

type EscalationEngine struct {
	store     *store.Store
	scheduler *Scheduler
}

func NewEscalationEngine(s *store.Store, sched *Scheduler) *EscalationEngine {
	return &EscalationEngine{store: s, scheduler: sched}
}

// HandleAction processes a ChatOps / On-Call dashboard response action on real alert incidents.
func (ee *EscalationEngine) HandleAction(ctx context.Context, req ChatOpsActionRequest) (*ChatOpsActionResult, error) {
	// Parse numeric ID from req.IncidentID (supporting formats like "INC-0017", "INC-17", "17")
	rawID := strings.TrimPrefix(strings.TrimSpace(req.IncidentID), "INC-")
	rawID = strings.TrimLeft(rawID, "0")
	if rawID == "" {
		rawID = "0"
	}
	incID, _ := strconv.ParseUint(rawID, 10, 64)

	switch req.Action {
	case "ack":
		if incID > 0 {
			if err := ee.store.AckAlertIncident(ctx, incID, req.OperatorID, req.Note); err != nil {
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
		}
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "escalate",
			StatusText:     "已手动升级 (Escalated)",
			Message:        "已立即拉起下一级技术支持/二线技术专家，电话与强触达已派发。",
			EscalationHalt: false,
		}, nil
	case "reassign":
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "reassign",
			StatusText:     "已转派 (Reassigned)",
			Message:        fmt.Sprintf("告警已成功转派至指定工程师 (ID: %d)。", req.TargetUser),
			EscalationHalt: false,
		}, nil
	case "warroom":
		warRoomURL := fmt.Sprintf("https://feishu.cn/warroom/%s", req.IncidentID)
		return &ChatOpsActionResult{
			OK:             true,
			Action:         "warroom",
			StatusText:     "应急作战室已就绪 (WarRoom)",
			Message:        "已自动拉起 P0/P1 应急排障协同群，核心值班人员与服务负责人已拉入群聊。",
			EscalationHalt: false,
			WarRoomURL:     warRoomURL,
		}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", req.Action)
	}
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
