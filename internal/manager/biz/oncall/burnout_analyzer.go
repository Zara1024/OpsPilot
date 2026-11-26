package oncall

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Zara1024/OpsPilot/internal/manager/data/oncall/store"
)

type EngineerLoad struct {
	UserID           uint64  `json:"user_id"`
	Name             string  `json:"name"`
	Email            string  `json:"email"`
	OnCallHours      int     `json:"on_call_hours"`
	IncidentsHandled int     `json:"incidents_handled"`
	ResolvedCount    int     `json:"resolved_count"`
	AvgMTTASeconds   float64 `json:"avg_mtta_seconds"`
	NightCalls       int     `json:"night_calls"`
	FatigueScore     int     `json:"fatigue_score"` // 0-100 (higher = more tired)
	HealthStatus     string  `json:"health_status"` // "healthy", "moderate", "overloaded"
}

type TrendPoint struct {
	Date          string `json:"date"`
	IncidentCount int    `json:"incident_count"`
	NightCount    int    `json:"night_count"`
}

type SeveritySummary struct {
	Severity    string  `json:"severity"`
	Count       int     `json:"count"`
	MTTASeconds float64 `json:"mtta_seconds"`
	MTTRSeconds float64 `json:"mttr_seconds"`
}

type BurnoutAnalytics struct {
	TimeRange             string            `json:"time_range"`
	WindowStart           string            `json:"window_start"`
	WindowEnd             string            `json:"window_end"`
	MTTASeconds           float64           `json:"mtta_seconds"`
	MTTAP50Seconds        float64           `json:"mtta_p50_seconds"`
	MTTAP90Seconds        float64           `json:"mtta_p90_seconds"`
	MTTAP95Seconds        float64           `json:"mtta_p95_seconds"`
	MTTARating            string            `json:"mtta_rating"`
	MTTRSeconds           float64           `json:"mttr_seconds"`
	MTTRP50Seconds        float64           `json:"mttr_p50_seconds"`
	MTTRP90Seconds        float64           `json:"mttr_p90_seconds"`
	MTTRP95Seconds        float64           `json:"mttr_p95_seconds"`
	MTTRRating            string            `json:"mttr_rating"`
	NightCallCount        int               `json:"night_call_count"`
	TotalIncidents        int               `json:"total_incidents"`
	AcknowledgedCount     int               `json:"acknowledged_count"`
	ResolvedCount         int               `json:"resolved_count"`
	OpenCount             int               `json:"open_count"`
	AckRate               float64           `json:"ack_rate"`
	ResolveRate           float64           `json:"resolve_rate"`
	BurnoutIndex          int               `json:"burnout_index"` // 0-100 SRE health score
	MergedAlertsCount     int               `json:"merged_alerts_count"`
	SuppressedAlertsCount int               `json:"suppressed_alerts_count"`
	NoiseReductionRatio   float64           `json:"noise_reduction_ratio"`
	SeverityBreakdown     []SeveritySummary `json:"severity_breakdown"`
	EngineerLoads         []EngineerLoad    `json:"engineer_loads"`
	Trends                []TrendPoint      `json:"trends"`
}

type NoisyAlertItem struct {
	Fingerprint       string  `json:"fingerprint"`
	AlertName         string  `json:"alert_name"`
	Service           string  `json:"service"`
	Severity          string  `json:"severity"`
	TriggerCount      int     `json:"trigger_count"`
	AvgDurationSec    int     `json:"avg_duration_sec"`
	FlappingScore     float64 `json:"flapping_score"`
	NoiseLevel        string  `json:"noise_level"` // "extreme", "high", "medium"
	RecommendedAction string  `json:"recommended_action"`
}

type BurnoutAnalyzer struct {
	store     *store.Store
	scheduler *Scheduler
}

// NewBurnoutAnalyzer constructs a BurnoutAnalyzer using store and optional Scheduler for real shift-hour computation.
func NewBurnoutAnalyzer(s *store.Store, scheduler *Scheduler) *BurnoutAnalyzer {
	if scheduler == nil && s != nil {
		scheduler = NewScheduler(s)
	}
	return &BurnoutAnalyzer{store: s, scheduler: scheduler}
}

// ParseAnalyticsWindow converts timeRange ("7d", "14d", "30d", "90d") into UTC [start, end] bounds.
func ParseAnalyticsWindow(timeRange string, now time.Time) (string, time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	days := 7
	normalized := strings.ToLower(strings.TrimSpace(timeRange))
	switch normalized {
	case "14d":
		days = 14
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		normalized = "7d"
		days = 7
	}
	start := now.AddDate(0, 0, -days)
	return normalized, start, now
}

// GetBurnoutAnalytics computes real MTTA/MTTR percentiles, shift hours, fatigue scores, noise reduction, and trends.
func (a *BurnoutAnalyzer) GetBurnoutAnalytics(ctx context.Context, scheduleID uint64, timeRange string) (*BurnoutAnalytics, error) {
	trNormalized, start, end := ParseAnalyticsWindow(timeRange, time.Now().UTC())

	users, _ := a.store.ListAllUsers(ctx)
	detailed, detErr := a.store.GetDetailedAlertAnalytics(ctx, scheduleID, start, end)
	if detErr != nil {
		slog.Warn("query detailed alert analytics failed", slog.Any("err", detErr))
		detailed = &store.DetailedAlertAnalytics{
			SeverityMetrics:  map[string]*store.SeverityMetric{},
			UserHandleCount:  map[uint64]int64{},
			UserResolveCount: map[uint64]int64{},
			UserMTTASamples:  map[uint64][]float64{},
			NightCallByUser:  map[uint64]int{},
		}
	}

	onCallHoursByUser := a.computeOnCallHoursByUser(ctx, scheduleID, start, end)

	avgMTTA := roundOneDecimal(meanFloat(detailed.MTTASamples))
	p50MTTA := roundOneDecimal(percentileFloat(detailed.MTTASamples, 50))
	p90MTTA := roundOneDecimal(percentileFloat(detailed.MTTASamples, 90))
	p95MTTA := roundOneDecimal(percentileFloat(detailed.MTTASamples, 95))

	avgMTTR := roundOneDecimal(meanFloat(detailed.MTTRSamples))
	p50MTTR := roundOneDecimal(percentileFloat(detailed.MTTRSamples, 50))
	p90MTTR := roundOneDecimal(percentileFloat(detailed.MTTRSamples, 90))
	p95MTTR := roundOneDecimal(percentileFloat(detailed.MTTRSamples, 95))

	var ackRate, resolveRate float64
	if detailed.TotalIncidents > 0 {
		ackRate = roundOneDecimal(float64(detailed.AcknowledgedCount) / float64(detailed.TotalIncidents) * 100)
		resolveRate = roundOneDecimal(float64(detailed.ResolvedCount) / float64(detailed.TotalIncidents) * 100)
	}

	noiseReduced := detailed.MergedAlertsCount + detailed.SuppressedAlertsCount
	rawSignals := detailed.TotalIncidents + noiseReduced
	var noiseReductionRatio float64
	if rawSignals > 0 && noiseReduced > 0 {
		noiseReductionRatio = roundOneDecimal(float64(noiseReduced) / float64(rawSignals) * 100)
		if noiseReductionRatio > 99.9 {
			noiseReductionRatio = 99.9
		}
	}

	engineerLoads := make([]EngineerLoad, 0, len(users))
	fatigueSum := 0
	for _, u := range users {
		name := strings.TrimSpace(u.DisplayName)
		if name == "" {
			name = u.Email
		}
		hours := onCallHoursByUser[u.ID]
		handled := int(detailed.UserHandleCount[u.ID])
		resolved := int(detailed.UserResolveCount[u.ID])
		nightCalls := detailed.NightCallByUser[u.ID]
		userMTTA := roundOneDecimal(meanFloat(detailed.UserMTTASamples[u.ID]))

		fatigue := computeEngineerFatigue(hours, handled, nightCalls)
		status := "healthy"
		if fatigue >= 70 {
			status = "overloaded"
		} else if fatigue >= 40 {
			status = "moderate"
		}
		fatigueSum += fatigue

		engineerLoads = append(engineerLoads, EngineerLoad{
			UserID:           u.ID,
			Name:             name,
			Email:            u.Email,
			OnCallHours:      hours,
			IncidentsHandled: handled,
			ResolvedCount:    resolved,
			AvgMTTASeconds:   userMTTA,
			NightCalls:       nightCalls,
			FatigueScore:     fatigue,
			HealthStatus:     status,
		})
	}
	sort.Slice(engineerLoads, func(i, j int) bool {
		if engineerLoads[i].FatigueScore == engineerLoads[j].FatigueScore {
			return engineerLoads[i].UserID < engineerLoads[j].UserID
		}
		return engineerLoads[i].FatigueScore > engineerLoads[j].FatigueScore
	})

	burnoutIndex := 100
	if len(engineerLoads) > 0 {
		burnoutIndex = 100 - int(math.Round(float64(fatigueSum)/float64(len(engineerLoads))))
		if burnoutIndex < 0 {
			burnoutIndex = 0
		}
	}

	severityOrder := map[string]int{"critical": 1, "p0": 1, "p1": 2, "warning": 3, "p2": 4, "info": 5, "p3": 6}
	severityBreakdown := make([]SeveritySummary, 0, len(detailed.SeverityMetrics))
	for _, sm := range detailed.SeverityMetrics {
		if sm == nil {
			continue
		}
		severityBreakdown = append(severityBreakdown, SeveritySummary{
			Severity:    sm.Severity,
			Count:       sm.Count,
			MTTASeconds: roundOneDecimal(meanFloat(sm.MTTASamples)),
			MTTRSeconds: roundOneDecimal(meanFloat(sm.MTTRSamples)),
		})
	}
	sort.Slice(severityBreakdown, func(i, j int) bool {
		oi := severityOrder[severityBreakdown[i].Severity]
		oj := severityOrder[severityBreakdown[j].Severity]
		if oi == 0 {
			oi = 99
		}
		if oj == 0 {
			oj = 99
		}
		if oi == oj {
			return severityBreakdown[i].Severity < severityBreakdown[j].Severity
		}
		return oi < oj
	})

	trends := make([]TrendPoint, 0, len(detailed.DayTrends))
	for _, dt := range detailed.DayTrends {
		trends = append(trends, TrendPoint{
			Date:          dt.Date,
			IncidentCount: dt.IncidentCount,
			NightCount:    dt.NightCount,
		})
	}

	mttaRating := rateLatencySeconds(avgMTTA, len(detailed.MTTASamples) > 0, 300, 900)
	mttrRating := rateLatencySeconds(avgMTTR, len(detailed.MTTRSamples) > 0, 1800, 7200)

	return &BurnoutAnalytics{
		TimeRange:             trNormalized,
		WindowStart:           start.Format(time.RFC3339),
		WindowEnd:             end.Format(time.RFC3339),
		MTTASeconds:           avgMTTA,
		MTTAP50Seconds:        p50MTTA,
		MTTAP90Seconds:        p90MTTA,
		MTTAP95Seconds:        p95MTTA,
		MTTARating:            mttaRating,
		MTTRSeconds:           avgMTTR,
		MTTRP50Seconds:        p50MTTR,
		MTTRP90Seconds:        p90MTTR,
		MTTRP95Seconds:        p95MTTR,
		MTTRRating:            mttrRating,
		NightCallCount:        detailed.TotalNightCalls,
		TotalIncidents:        detailed.TotalIncidents,
		AcknowledgedCount:     detailed.AcknowledgedCount,
		ResolvedCount:         detailed.ResolvedCount,
		OpenCount:             detailed.OpenCount,
		AckRate:               ackRate,
		ResolveRate:           resolveRate,
		BurnoutIndex:          burnoutIndex,
		MergedAlertsCount:     detailed.MergedAlertsCount,
		SuppressedAlertsCount: detailed.SuppressedAlertsCount,
		NoiseReductionRatio:   noiseReductionRatio,
		SeverityBreakdown:     severityBreakdown,
		EngineerLoads:         engineerLoads,
		Trends:                trends,
	}, nil
}

// GetNoisyAlerts returns real flapping/high-frequency alert rules in the requested schedule and window.
func (a *BurnoutAnalyzer) GetNoisyAlerts(ctx context.Context, scheduleID uint64, timeRange string) ([]*NoisyAlertItem, error) {
	_, start, end := ParseAnalyticsWindow(timeRange, time.Now().UTC())
	detailed, err := a.store.GetDetailedAlertAnalytics(ctx, scheduleID, start, end)
	if err != nil {
		return nil, err
	}
	if detailed == nil || len(detailed.TopRules) == 0 {
		return []*NoisyAlertItem{}, nil
	}

	items := make([]*NoisyAlertItem, 0, len(detailed.TopRules))
	for _, r := range detailed.TopRules {
		service := "被控主机基础监控 (Host Infrastructure)"
		action := "调整告警持续时间与防抖阈值，避免瞬时毛刺频繁告警"
		noiseLevel := "medium"
		if r.TriggerCount >= 10 {
			noiseLevel = "extreme"
		} else if r.TriggerCount >= 4 {
			noiseLevel = "high"
		}

		ruleLower := strings.ToLower(r.RuleName + " " + r.RuleKey)
		switch {
		case strings.Contains(ruleLower, "offline"):
			service = "边缘接入与心跳检测 (Edge Agent Heartbeat)"
			action = "检查边缘节点连通性与 Agent 心跳周期；针对弱网环境放宽离线超时容限 (如 180s)"
		case strings.Contains(ruleLower, "cpu"):
			service = "主机 CPU 资源监控 (Host Node Exporter)"
			action = "排查高负载容器或进程；配置 5m PromQL 持续时间过滤 CPU 瞬时抖动"
		case strings.Contains(ruleLower, "scrape"):
			service = "Prometheus 采集管道 (Prometheus Scraper Pipeline)"
			action = "排查被控机器 node-exporter 存活状态与 9100 端口网络连通性"
		case strings.Contains(ruleLower, "mem"):
			service = "主机内存资源监控 (Host Memory Exporter)"
			action = "排查进程内存泄漏与 Cgroup 配额限制，增加平滑均值告警窗口"
		case strings.Contains(ruleLower, "disk"):
			service = "磁盘存储健康监控 (Disk Capacity Monitor)"
			action = "清理历史日志并设置分组收敛与分级预警阈值"
		}

		flappingScore := roundOneDecimal(math.Min(0.99, float64(r.TriggerCount)/math.Max(float64(r.IncidentCount)*4.0, 4.0)))
		items = append(items, &NoisyAlertItem{
			Fingerprint:       "rule:" + r.RuleKey,
			AlertName:         r.RuleName,
			Service:           service,
			Severity:          r.Severity,
			TriggerCount:      r.TriggerCount,
			AvgDurationSec:    r.AvgDurationSec,
			FlappingScore:     flappingScore,
			NoiseLevel:        noiseLevel,
			RecommendedAction: action,
		})
	}
	return items, nil
}

// BuildAnalyticsCSV formats BurnoutAnalytics and NoisyAlertItems into UTF-8 BOM CSV with formula-injection neutralization.
func BuildAnalyticsCSV(analytics *BurnoutAnalytics, noisy []*NoisyAlertItem) ([]byte, error) {
	var buf bytes.Buffer
	// Write UTF-8 BOM for seamless Chinese Excel rendering.
	buf.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(&buf)
	writeRow := func(cols ...string) error {
		safe := make([]string, len(cols))
		for i, c := range cols {
			safe[i] = sanitizeCSVCell(c)
		}
		return cw.Write(safe)
	}

	if err := writeRow("# OpsPilot On-Call SLA & Burnout Report"); err != nil {
		return nil, err
	}
	if analytics != nil {
		if err := writeRow(
			"TimeRange", "WindowStart", "WindowEnd",
			"TotalIncidents", "AckCount", "ResolveCount", "OpenCount",
			"AckRate(%)", "ResolveRate(%)",
			"MTTA_Avg(s)", "MTTA_P50(s)", "MTTA_P90(s)", "MTTA_P95(s)",
			"MTTR_Avg(s)", "MTTR_P50(s)", "MTTR_P90(s)", "MTTR_P95(s)",
			"NightCallCount", "MergedAlerts", "SuppressedAlerts", "NoiseReductionRatio(%)", "BurnoutIndex",
		); err != nil {
			return nil, err
		}
		if err := writeRow(
			analytics.TimeRange,
			analytics.WindowStart,
			analytics.WindowEnd,
			fmt.Sprintf("%d", analytics.TotalIncidents),
			fmt.Sprintf("%d", analytics.AcknowledgedCount),
			fmt.Sprintf("%d", analytics.ResolvedCount),
			fmt.Sprintf("%d", analytics.OpenCount),
			fmt.Sprintf("%.1f", analytics.AckRate),
			fmt.Sprintf("%.1f", analytics.ResolveRate),
			fmt.Sprintf("%.1f", analytics.MTTASeconds),
			fmt.Sprintf("%.1f", analytics.MTTAP50Seconds),
			fmt.Sprintf("%.1f", analytics.MTTAP90Seconds),
			fmt.Sprintf("%.1f", analytics.MTTAP95Seconds),
			fmt.Sprintf("%.1f", analytics.MTTRSeconds),
			fmt.Sprintf("%.1f", analytics.MTTRP50Seconds),
			fmt.Sprintf("%.1f", analytics.MTTRP90Seconds),
			fmt.Sprintf("%.1f", analytics.MTTRP95Seconds),
			fmt.Sprintf("%d", analytics.NightCallCount),
			fmt.Sprintf("%d", analytics.MergedAlertsCount),
			fmt.Sprintf("%d", analytics.SuppressedAlertsCount),
			fmt.Sprintf("%.1f", analytics.NoiseReductionRatio),
			fmt.Sprintf("%d", analytics.BurnoutIndex),
		); err != nil {
			return nil, err
		}

		if err := writeRow(); err != nil {
			return nil, err
		}
		if err := writeRow("# Engineer On-Call Load"); err != nil {
			return nil, err
		}
		if err := writeRow("UserID", "Name", "Email", "OnCallHours", "IncidentsHandled", "ResolvedCount", "AvgMTTA(s)", "NightCalls", "FatigueScore", "HealthStatus"); err != nil {
			return nil, err
		}
		for _, eng := range analytics.EngineerLoads {
			if err := writeRow(
				fmt.Sprintf("%d", eng.UserID),
				eng.Name,
				eng.Email,
				fmt.Sprintf("%d", eng.OnCallHours),
				fmt.Sprintf("%d", eng.IncidentsHandled),
				fmt.Sprintf("%d", eng.ResolvedCount),
				fmt.Sprintf("%.1f", eng.AvgMTTASeconds),
				fmt.Sprintf("%d", eng.NightCalls),
				fmt.Sprintf("%d", eng.FatigueScore),
				eng.HealthStatus,
			); err != nil {
				return nil, err
			}
		}

		if len(analytics.SeverityBreakdown) > 0 {
			if err := writeRow(); err != nil {
				return nil, err
			}
			if err := writeRow("# Severity SLA Breakdown"); err != nil {
				return nil, err
			}
			if err := writeRow("Severity", "Count", "AvgMTTA(s)", "AvgMTTR(s)"); err != nil {
				return nil, err
			}
			for _, sb := range analytics.SeverityBreakdown {
				if err := writeRow(
					sb.Severity,
					fmt.Sprintf("%d", sb.Count),
					fmt.Sprintf("%.1f", sb.MTTASeconds),
					fmt.Sprintf("%.1f", sb.MTTRSeconds),
				); err != nil {
					return nil, err
				}
			}
		}
	}

	if len(noisy) > 0 {
		if err := writeRow(); err != nil {
			return nil, err
		}
		if err := writeRow("# Top Noisy Alerts"); err != nil {
			return nil, err
		}
		if err := writeRow("Fingerprint", "AlertName", "Service", "Severity", "TriggerCount", "AvgDurationSec", "FlappingScore", "NoiseLevel", "RecommendedAction"); err != nil {
			return nil, err
		}
		for _, item := range noisy {
			if item == nil {
				continue
			}
			if err := writeRow(
				item.Fingerprint,
				item.AlertName,
				item.Service,
				item.Severity,
				fmt.Sprintf("%d", item.TriggerCount),
				fmt.Sprintf("%d", item.AvgDurationSec),
				fmt.Sprintf("%.1f", item.FlappingScore),
				item.NoiseLevel,
				item.RecommendedAction,
			); err != nil {
				return nil, err
			}
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return nil, fmt.Errorf("flush analytics csv: %w", err)
	}
	return buf.Bytes(), nil
}

func sanitizeCSVCell(v string) string {
	if v == "" {
		return ""
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

func (a *BurnoutAnalyzer) computeOnCallHoursByUser(ctx context.Context, scheduleID uint64, start, end time.Time) map[uint64]int {
	hoursFloat := make(map[uint64]float64)
	if a.scheduler == nil || a.store == nil {
		return map[uint64]int{}
	}

	var scheduleIDs []uint64
	if scheduleID > 0 {
		scheduleIDs = []uint64{scheduleID}
	} else if scheds, err := a.store.ListSchedules(ctx, 0, true); err == nil {
		for _, s := range scheds {
			if s != nil {
				scheduleIDs = append(scheduleIDs, s.ID)
			}
		}
	}

	for _, sid := range scheduleIDs {
		shifts, err := a.scheduler.ComputeScheduleShifts(ctx, sid, start, end)
		if err != nil {
			continue
		}
		for _, sh := range shifts {
			if sh == nil || sh.UserID == 0 {
				continue
			}
			sTime := sh.StartTime
			if sTime.Before(start) {
				sTime = start
			}
			eTime := sh.EndTime
			if eTime.After(end) {
				eTime = end
			}
			if eTime.After(sTime) {
				hoursFloat[sh.UserID] += eTime.Sub(sTime).Hours()
			}
		}
	}

	res := make(map[uint64]int, len(hoursFloat))
	for uid, h := range hoursFloat {
		res[uid] = int(math.Round(h))
	}
	return res
}

func computeEngineerFatigue(onCallHours, incidentsHandled, nightCalls int) int {
	score := int(math.Round(float64(onCallHours)/12.0)) + incidentsHandled*4 + nightCalls*15
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func rateLatencySeconds(avg float64, hasSamples bool, goodMax, moderateMax float64) string {
	if !hasSamples {
		return "good"
	}
	if avg > moderateMax {
		return "poor"
	}
	if avg > goodMax {
		return "moderate"
	}
	return "good"
}

func meanFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func percentileFloat(vals []float64, pct float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	if len(cp) == 1 {
		return cp[0]
	}
	rank := (pct / 100.0) * float64(len(cp)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return cp[lower]
	}
	frac := rank - float64(lower)
	return cp[lower]*(1-frac) + cp[upper]*frac
}

func roundOneDecimal(v float64) float64 {
	return math.Round(v*10) / 10
}
