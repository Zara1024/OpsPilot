package aicost

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Zara1024/OpsPilot/internal/manager/data/aicost/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/aicost"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/llm"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

// Store 定义 aicost 业务层所需的持久化契约。
type Store interface {
	InsertUsage(ctx context.Context, row *model.UsageLog) error
	ListUsageLogs(ctx context.Context, f store.LogFilter) ([]*model.UsageLog, int64, error)
	SumUsageWindow(ctx context.Context, scopeType, scopeKey string, since time.Time) (store.WindowTotals, error)
	ListUsageByModelSince(ctx context.Context, since time.Time) ([]store.ModelUsageSlice, error)
	AggregateOverview(ctx context.Context, since time.Time) (*store.OverviewStats, error)
	ListQuotas(ctx context.Context, onlyEnabled bool) ([]*model.CostQuota, error)
	GetQuotaByID(ctx context.Context, id uint64) (*model.CostQuota, error)
	CreateQuota(ctx context.Context, q *model.CostQuota) error
	UpdateQuota(ctx context.Context, q *model.CostQuota) error
	SetCircuitState(ctx context.Context, id uint64, open bool, openedAt *time.Time, reason string) error
	DeleteQuota(ctx context.Context, id uint64) error
	ListPrices(ctx context.Context) ([]*model.ModelPrice, error)
	UpsertPrice(ctx context.Context, p *model.ModelPrice) (*model.ModelPrice, error)
	DeletePrice(ctx context.Context, id uint64) error
}

// OrgMembershipReader 在 manager 层消费，仅通过适配器注入用户所属组织 ID。
type OrgMembershipReader interface {
	ListOrgIDsByUser(ctx context.Context, userID uint64) ([]uint64, error)
}

// QuotaInput 为创建或更新配额规则的入参。
type QuotaInput struct {
	Name                string  `json:"name"`
	ScopeType           string  `json:"scope_type"`
	ScopeKey            string  `json:"scope_key"`
	Enabled             bool    `json:"enabled"`
	DailyTokenLimit     int64   `json:"daily_token_limit"`
	MonthlyTokenLimit   int64   `json:"monthly_token_limit"`
	DailyCostLimitUSD   float64 `json:"daily_cost_limit_usd"`
	MonthlyCostLimitUSD float64 `json:"monthly_cost_limit_usd"`
	WarnThresholdPct    int     `json:"warn_threshold_pct"`
	ActionOnExceed      string  `json:"action_on_exceed"`
	Description         string  `json:"description"`
}

// PriceInput 为设置自定义模型单价的入参。
type PriceInput struct {
	Provider       string  `json:"provider"`
	ModelPattern   string  `json:"model_pattern"`
	InputUSDPer1M  float64 `json:"input_usd_per_1m"`
	OutputUSDPer1M float64 `json:"output_usd_per_1m"`
	Currency       string  `json:"currency"`
}

// QuotaView 在配额配置基础上附加当日与当月实时消耗进度及健康状态。
type QuotaView struct {
	ID                  uint64     `json:"id"`
	Name                string     `json:"name"`
	ScopeType           string     `json:"scope_type"`
	ScopeKey            string     `json:"scope_key"`
	Enabled             bool       `json:"enabled"`
	DailyTokenLimit     int64      `json:"daily_token_limit"`
	MonthlyTokenLimit   int64      `json:"monthly_token_limit"`
	DailyCostLimitUSD   float64    `json:"daily_cost_limit_usd"`
	MonthlyCostLimitUSD float64    `json:"monthly_cost_limit_usd"`
	WarnThresholdPct    int        `json:"warn_threshold_pct"`
	ActionOnExceed      string     `json:"action_on_exceed"`
	CircuitOpen         bool       `json:"circuit_open"`
	CircuitOpenedAt     *time.Time `json:"circuit_opened_at,omitempty"`
	CircuitReason       string     `json:"circuit_reason,omitempty"`
	Description         string     `json:"description,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DailyTokensUsed     int64      `json:"daily_tokens_used"`
	DailyCostUsedUSD    float64    `json:"daily_cost_used_usd"`
	MonthlyTokensUsed   int64      `json:"monthly_tokens_used"`
	MonthlyCostUsedUSD  float64    `json:"monthly_cost_used_usd"`
	MaxUtilizationPct   float64    `json:"max_utilization_pct"`
	HealthState         string     `json:"health_state"` // ok | warn | circuit_open
}

// SummaryView 汇总今日、本月与近 N 天成本、拦截统计及所有配额状态。
type SummaryView struct {
	Days      int                 `json:"days"`
	Today     store.WindowTotals  `json:"today"`
	Month     store.WindowTotals  `json:"month"`
	Overview  store.OverviewStats `json:"overview"`
	Quotas    []QuotaView         `json:"quotas"`
	OpenCount int                 `json:"open_circuit_count"`
}

// Service 实现 llm.CostGuard 并提供配额管理、手动/自动熔断、模型定价与成本看板服务。
type Service struct {
	store Store
	orgs  OrgMembershipReader
	log   *slog.Logger
	now   func() time.Time
}

// New 构造 aicost Service。
func New(st Store, orgs OrgMembershipReader, log *slog.Logger) *Service {
	return &Service{
		store: st,
		orgs:  orgs,
		log:   log,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func utcDayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func utcMonthStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (s *Service) resolveUserAndOrgs(ctx context.Context, userID, orgID uint64) (uint64, []uint64) {
	if userID == 0 {
		if t, ok := tenantctx.From(ctx); ok {
			userID = t.UserID
		}
	}
	var orgIDs []uint64
	if orgID > 0 {
		orgIDs = append(orgIDs, orgID)
	}
	if userID > 0 && s.orgs != nil {
		if list, err := s.orgs.ListOrgIDsByUser(ctx, userID); err == nil {
			seen := make(map[uint64]struct{}, len(orgIDs)+len(list))
			for _, id := range orgIDs {
				seen[id] = struct{}{}
			}
			for _, id := range list {
				if id == 0 {
					continue
				}
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					orgIDs = append(orgIDs, id)
				}
			}
		}
	}
	return userID, orgIDs
}

func quotaMatchesRequest(q *model.CostQuota, userID uint64, orgIDs []uint64, modelName string) bool {
	if q == nil {
		return false
	}
	switch q.ScopeType {
	case model.ScopeGlobal:
		return true
	case model.ScopeUser:
		if userID == 0 {
			return false
		}
		return strings.TrimSpace(q.ScopeKey) == strconv.FormatUint(userID, 10)
	case model.ScopeOrg:
		target := strings.TrimSpace(q.ScopeKey)
		for _, oid := range orgIDs {
			if strconv.FormatUint(oid, 10) == target {
				return true
			}
		}
		return false
	case model.ScopeModel:
		return matchGlob(q.ScopeKey, modelName)
	default:
		return false
	}
}

func (s *Service) measureScopeWindow(ctx context.Context, scopeType, scopeKey string, since time.Time) (int64, float64, error) {
	if scopeType != model.ScopeModel {
		tot, err := s.store.SumUsageWindow(ctx, scopeType, scopeKey, since)
		return tot.TotalTokens, tot.CostUSD, err
	}
	if !strings.ContainsAny(scopeKey, "*?") {
		tot, err := s.store.SumUsageWindow(ctx, model.ScopeModel, scopeKey, since)
		return tot.TotalTokens, tot.CostUSD, err
	}
	slices, err := s.store.ListUsageByModelSince(ctx, since)
	if err != nil {
		return 0, 0, err
	}
	var tokens int64
	var cost float64
	for _, sl := range slices {
		if matchGlob(scopeKey, sl.Model) {
			tokens += sl.TotalTokens
			cost += sl.CostUSD
		}
	}
	return tokens, cost, nil
}

// Check 实现 llm.CostGuard：在发起真实 LLM HTTP 请求前执行配额与熔断器检查。
func (s *Service) Check(ctx context.Context, in llm.GuardCheckInput) error {
	if s.store == nil {
		return nil
	}
	quotas, err := s.store.ListQuotas(ctx, true)
	if err != nil {
		// 统计/配置库抖动不阻断线上紧急排障，但记录警告日志
		if s.log != nil {
			s.log.Warn("aicost: list quotas failed", slog.Any("err", err))
		}
		return nil
	}
	if len(quotas) == 0 {
		return nil
	}

	userID, orgIDs := s.resolveUserAndOrgs(ctx, in.UserID, in.OrgID)
	now := s.now()
	dayStart := utcDayStart(now)
	monthStart := utcMonthStart(now)

	for _, q := range quotas {
		if !quotaMatchesRequest(q, userID, orgIDs, in.Model) {
			continue
		}
		if q.CircuitOpen && strings.HasPrefix(q.CircuitReason, "manual:") {
			return fmt.Errorf("%w: quota %q circuit open (%s)", llm.ErrBudgetExceeded, q.Name, q.CircuitReason)
		}

		var dailyTokens int64
		var dailyCost float64
		if q.DailyTokenLimit > 0 || q.DailyCostLimitUSD > 0 {
			dt, dc, err := s.measureScopeWindow(ctx, q.ScopeType, q.ScopeKey, dayStart)
			if err != nil {
				if s.log != nil {
					s.log.Warn("aicost: measure daily window failed", slog.Uint64("quota_id", q.ID), slog.Any("err", err))
				}
				continue
			}
			dailyTokens, dailyCost = dt, dc
		}

		var monthlyTokens int64
		var monthlyCost float64
		if q.MonthlyTokenLimit > 0 || q.MonthlyCostLimitUSD > 0 {
			mt, mc, err := s.measureScopeWindow(ctx, q.ScopeType, q.ScopeKey, monthStart)
			if err != nil {
				if s.log != nil {
					s.log.Warn("aicost: measure monthly window failed", slog.Uint64("quota_id", q.ID), slog.Any("err", err))
				}
				continue
			}
			monthlyTokens, monthlyCost = mt, mc
		}

		reason := ""
		est := int64(in.EstPromptTokens)
		if est < 0 {
			est = 0
		}
		switch {
		case q.DailyTokenLimit > 0 && dailyTokens+est > q.DailyTokenLimit:
			reason = fmt.Sprintf("daily token limit exceeded (%d/%d)", dailyTokens+est, q.DailyTokenLimit)
		case q.MonthlyTokenLimit > 0 && monthlyTokens+est > q.MonthlyTokenLimit:
			reason = fmt.Sprintf("monthly token limit exceeded (%d/%d)", monthlyTokens+est, q.MonthlyTokenLimit)
		case q.DailyCostLimitUSD > 0 && dailyCost >= q.DailyCostLimitUSD:
			reason = fmt.Sprintf("daily cost limit exceeded ($%.4f/$%.2f)", dailyCost, q.DailyCostLimitUSD)
		case q.MonthlyCostLimitUSD > 0 && monthlyCost >= q.MonthlyCostLimitUSD:
			reason = fmt.Sprintf("monthly cost limit exceeded ($%.4f/$%.2f)", monthlyCost, q.MonthlyCostLimitUSD)
		}

		if reason == "" {
			// 自动熔断在进入新周期（或管理员提高配额后当前低于阈值）时自动恢复闭合。
			if q.CircuitOpen && !strings.HasPrefix(q.CircuitReason, "manual:") {
				_ = s.store.SetCircuitState(context.WithoutCancel(ctx), q.ID, false, nil, "")
			}
			continue
		}

		if q.ActionOnExceed == model.ActionWarn {
			if s.log != nil {
				s.log.Warn("aicost: quota soft threshold exceeded (warn only)",
					slog.String("quota", q.Name),
					slog.String("reason", reason),
				)
			}
			continue
		}

		if !q.CircuitOpen {
			openedAt := now
			_ = s.store.SetCircuitState(context.WithoutCancel(ctx), q.ID, true, &openedAt, reason)
		}
		return fmt.Errorf("%w: quota %q tripped (%s)", llm.ErrBudgetExceeded, q.Name, reason)
	}
	return nil
}

// Record 实现 llm.CostGuard：计算本次调用费用并写入账本。
func (s *Service) Record(ctx context.Context, in llm.GuardRecordInput) error {
	if s.store == nil {
		return nil
	}
	userID, orgIDs := s.resolveUserAndOrgs(ctx, in.UserID, in.OrgID)
	primaryOrgID := in.OrgID
	if primaryOrgID == 0 && len(orgIDs) > 0 {
		primaryOrgID = orgIDs[0]
	}

	status := strings.TrimSpace(strings.ToLower(in.Status))
	if !model.IsValidUsageStatus(status) {
		status = model.UsageStatusOK
	}

	var costUSD float64
	if status != model.UsageStatusBlocked {
		customPrices, _ := s.store.ListPrices(ctx)
		costUSD = ComputeCostUSD(in.Provider, in.Model, in.PromptTokens, in.CompletionTokens, customPrices)
	}

	total := in.TotalTokens
	if total == 0 {
		total = in.PromptTokens + in.CompletionTokens
	}
	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = "unknown"
	}
	modelName := strings.TrimSpace(in.Model)
	if modelName == "" {
		modelName = "unknown"
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = llm.SourceOther
	}

	return s.store.InsertUsage(ctx, &model.UsageLog{
		UserID:           userID,
		OrgID:            primaryOrgID,
		Provider:         provider,
		Model:            modelName,
		Source:           source,
		SessionID:        strings.TrimSpace(in.SessionID),
		RefID:            strings.TrimSpace(in.RefID),
		PromptTokens:     in.PromptTokens,
		CompletionTokens: in.CompletionTokens,
		TotalTokens:      total,
		CostUSD:          costUSD,
		LatencyMS:        in.LatencyMS,
		Status:           status,
		ErrorMessage:     strings.TrimSpace(in.ErrorMessage),
		CreatedAt:        s.now(),
	})
}

func validateQuotaInput(in *QuotaInput) error {
	if in == nil {
		return errs.ErrInvalid
	}
	in.Name = strings.TrimSpace(in.Name)
	in.ScopeType = strings.TrimSpace(strings.ToLower(in.ScopeType))
	in.ScopeKey = strings.TrimSpace(in.ScopeKey)
	in.ActionOnExceed = strings.TrimSpace(strings.ToLower(in.ActionOnExceed))
	in.Description = strings.TrimSpace(in.Description)

	if in.Name == "" || !model.IsValidScopeType(in.ScopeType) {
		return fmt.Errorf("%w: invalid quota name or scope_type", errs.ErrInvalid)
	}
	if in.ScopeType == model.ScopeGlobal {
		in.ScopeKey = "*"
	}
	if in.ScopeKey == "" {
		return fmt.Errorf("%w: scope_key is required", errs.ErrInvalid)
	}
	if in.ScopeType == model.ScopeUser || in.ScopeType == model.ScopeOrg {
		if id, err := strconv.ParseUint(in.ScopeKey, 10, 64); err != nil || id == 0 {
			return fmt.Errorf("%w: scope_key must be numeric ID for user/org scope", errs.ErrInvalid)
		}
	}
	if in.ActionOnExceed == "" {
		in.ActionOnExceed = model.ActionBlock
	}
	if !model.IsValidActionOnExceed(in.ActionOnExceed) {
		return fmt.Errorf("%w: invalid action_on_exceed", errs.ErrInvalid)
	}
	if in.WarnThresholdPct <= 0 || in.WarnThresholdPct > 100 {
		in.WarnThresholdPct = 80
	}
	if in.DailyTokenLimit < 0 || in.MonthlyTokenLimit < 0 || in.DailyCostLimitUSD < 0 || in.MonthlyCostLimitUSD < 0 {
		return fmt.Errorf("%w: limits cannot be negative", errs.ErrInvalid)
	}
	if in.DailyTokenLimit == 0 && in.MonthlyTokenLimit == 0 && in.DailyCostLimitUSD == 0 && in.MonthlyCostLimitUSD == 0 {
		return fmt.Errorf("%w: at least one daily or monthly limit must be positive", errs.ErrInvalid)
	}
	return nil
}

func (s *Service) enrichQuota(ctx context.Context, q *model.CostQuota, dayStart, monthStart time.Time) QuotaView {
	dt, dc, _ := s.measureScopeWindow(ctx, q.ScopeType, q.ScopeKey, dayStart)
	mt, mc, _ := s.measureScopeWindow(ctx, q.ScopeType, q.ScopeKey, monthStart)

	var maxPct float64
	checkRatio := func(used, limit float64) {
		if limit > 0 {
			pct := (used / limit) * 100
			if pct > maxPct {
				maxPct = pct
			}
		}
	}
	checkRatio(float64(dt), float64(q.DailyTokenLimit))
	checkRatio(float64(mt), float64(q.MonthlyTokenLimit))
	checkRatio(dc, q.DailyCostLimitUSD)
	checkRatio(mc, q.MonthlyCostLimitUSD)
	maxPct = math.Round(maxPct*10) / 10

	health := "ok"
	if q.CircuitOpen {
		health = "circuit_open"
	} else if maxPct >= float64(q.WarnThresholdPct) {
		health = "warn"
	}

	return QuotaView{
		ID:                  q.ID,
		Name:                q.Name,
		ScopeType:           q.ScopeType,
		ScopeKey:            q.ScopeKey,
		Enabled:             q.Enabled,
		DailyTokenLimit:     q.DailyTokenLimit,
		MonthlyTokenLimit:   q.MonthlyTokenLimit,
		DailyCostLimitUSD:   q.DailyCostLimitUSD,
		MonthlyCostLimitUSD: q.MonthlyCostLimitUSD,
		WarnThresholdPct:    q.WarnThresholdPct,
		ActionOnExceed:      q.ActionOnExceed,
		CircuitOpen:         q.CircuitOpen,
		CircuitOpenedAt:     q.CircuitOpenedAt,
		CircuitReason:       q.CircuitReason,
		Description:         q.Description,
		UpdatedAt:           q.UpdatedAt,
		DailyTokensUsed:     dt,
		DailyCostUsedUSD:    dc,
		MonthlyTokensUsed:   mt,
		MonthlyCostUsedUSD:  mc,
		MaxUtilizationPct:   maxPct,
		HealthState:         health,
	}
}

// Summary 返回总览看板数据及各配额实时进度。
func (s *Service) Summary(ctx context.Context, days int) (*SummaryView, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	if days <= 0 || days > 90 {
		days = 7
	}
	now := s.now()
	dayStart := utcDayStart(now)
	monthStart := utcMonthStart(now)
	overviewSince := dayStart.AddDate(0, 0, -(days - 1))

	today, err := s.store.SumUsageWindow(ctx, model.ScopeGlobal, "*", dayStart)
	if err != nil {
		return nil, err
	}
	month, err := s.store.SumUsageWindow(ctx, model.ScopeGlobal, "*", monthStart)
	if err != nil {
		return nil, err
	}
	ov, err := s.store.AggregateOverview(ctx, overviewSince)
	if err != nil {
		return nil, err
	}
	quotas, err := s.store.ListQuotas(ctx, false)
	if err != nil {
		return nil, err
	}

	views := make([]QuotaView, 0, len(quotas))
	openCount := 0
	for _, q := range quotas {
		v := s.enrichQuota(ctx, q, dayStart, monthStart)
		if v.CircuitOpen {
			openCount++
		}
		views = append(views, v)
	}

	return &SummaryView{
		Days:      days,
		Today:     today,
		Month:     month,
		Overview:  *ov,
		Quotas:    views,
		OpenCount: openCount,
	}, nil
}

// ListLogs 分页返回调用流水。
func (s *Service) ListLogs(ctx context.Context, f store.LogFilter) ([]*model.UsageLog, int64, error) {
	if s.store == nil {
		return nil, 0, errs.ErrNotWiredYet
	}
	return s.store.ListUsageLogs(ctx, f)
}

// ListQuotas 列出含实时利用率的全部配额规则。
func (s *Service) ListQuotas(ctx context.Context) ([]QuotaView, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	quotas, err := s.store.ListQuotas(ctx, false)
	if err != nil {
		return nil, err
	}
	now := s.now()
	dayStart := utcDayStart(now)
	monthStart := utcMonthStart(now)
	out := make([]QuotaView, 0, len(quotas))
	for _, q := range quotas {
		out = append(out, s.enrichQuota(ctx, q, dayStart, monthStart))
	}
	return out, nil
}

// CreateQuota 创建新配额。
func (s *Service) CreateQuota(ctx context.Context, actorID uint64, in QuotaInput) (*QuotaView, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	if err := validateQuotaInput(&in); err != nil {
		return nil, err
	}
	q := &model.CostQuota{
		Name:                in.Name,
		ScopeType:           in.ScopeType,
		ScopeKey:            in.ScopeKey,
		Enabled:             in.Enabled,
		DailyTokenLimit:     in.DailyTokenLimit,
		MonthlyTokenLimit:   in.MonthlyTokenLimit,
		DailyCostLimitUSD:   in.DailyCostLimitUSD,
		MonthlyCostLimitUSD: in.MonthlyCostLimitUSD,
		WarnThresholdPct:    in.WarnThresholdPct,
		ActionOnExceed:      in.ActionOnExceed,
		Description:         in.Description,
		UpdatedBy:           actorID,
	}
	if err := s.store.CreateQuota(ctx, q); err != nil {
		return nil, err
	}
	now := s.now()
	v := s.enrichQuota(ctx, q, utcDayStart(now), utcMonthStart(now))
	return &v, nil
}

// UpdateQuota 更新配额规则。
func (s *Service) UpdateQuota(ctx context.Context, id uint64, actorID uint64, in QuotaInput) (*QuotaView, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	if err := validateQuotaInput(&in); err != nil {
		return nil, err
	}
	existing, err := s.store.GetQuotaByID(ctx, id)
	if err != nil {
		return nil, err
	}
	existing.Name = in.Name
	existing.ScopeType = in.ScopeType
	existing.ScopeKey = in.ScopeKey
	existing.Enabled = in.Enabled
	existing.DailyTokenLimit = in.DailyTokenLimit
	existing.MonthlyTokenLimit = in.MonthlyTokenLimit
	existing.DailyCostLimitUSD = in.DailyCostLimitUSD
	existing.MonthlyCostLimitUSD = in.MonthlyCostLimitUSD
	existing.WarnThresholdPct = in.WarnThresholdPct
	existing.ActionOnExceed = in.ActionOnExceed
	existing.Description = in.Description
	existing.UpdatedBy = actorID

	if err := s.store.UpdateQuota(ctx, existing); err != nil {
		return nil, err
	}
	updated, err := s.store.GetQuotaByID(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	v := s.enrichQuota(ctx, updated, utcDayStart(now), utcMonthStart(now))
	return &v, nil
}

// DeleteQuota 删除配额规则。
func (s *Service) DeleteQuota(ctx context.Context, id uint64) error {
	if s.store == nil {
		return errs.ErrNotWiredYet
	}
	return s.store.DeleteQuota(ctx, id)
}

// TripCircuit 管理员手动强制拉开熔断器。
func (s *Service) TripCircuit(ctx context.Context, id uint64, actorID uint64, reason string) (*QuotaView, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	_ = actorID
	r := strings.TrimSpace(reason)
	if r == "" {
		r = "tripped by admin"
	}
	if !strings.HasPrefix(r, "manual:") {
		r = "manual: " + r
	}
	now := s.now()
	if err := s.store.SetCircuitState(ctx, id, true, &now, r); err != nil {
		return nil, err
	}
	q, err := s.store.GetQuotaByID(ctx, id)
	if err != nil {
		return nil, err
	}
	v := s.enrichQuota(ctx, q, utcDayStart(now), utcMonthStart(now))
	return &v, nil
}

// ResetCircuit 管理员手动复位熔断器。
func (s *Service) ResetCircuit(ctx context.Context, id uint64, actorID uint64) (*QuotaView, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	_ = actorID
	if err := s.store.SetCircuitState(ctx, id, false, nil, ""); err != nil {
		return nil, err
	}
	q, err := s.store.GetQuotaByID(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	v := s.enrichQuota(ctx, q, utcDayStart(now), utcMonthStart(now))
	return &v, nil
}

// PricesCatalog 返回自定义覆盖价与内置参考价集合。
type PricesCatalog struct {
	Custom  []PriceEntry `json:"custom"`
	Builtin []PriceEntry `json:"builtin"`
}

// ListPrices 列出自定义定价和内置参考价。
func (s *Service) ListPrices(ctx context.Context) (*PricesCatalog, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	rows, err := s.store.ListPrices(ctx)
	if err != nil {
		return nil, err
	}
	custom := make([]PriceEntry, 0, len(rows))
	for _, r := range rows {
		custom = append(custom, PriceEntry{
			ID:             r.ID,
			Provider:       r.Provider,
			ModelPattern:   r.ModelPattern,
			InputUSDPer1M:  r.InputUSDPer1M,
			OutputUSDPer1M: r.OutputUSDPer1M,
			Currency:       r.Currency,
			Custom:         true,
		})
	}
	return &PricesCatalog{
		Custom:  custom,
		Builtin: BuiltinPrices(),
	}, nil
}

// UpsertPrice 保存一条自定义模型定价覆盖。
func (s *Service) UpsertPrice(ctx context.Context, in PriceInput) (*PriceEntry, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	prov := strings.TrimSpace(strings.ToLower(in.Provider))
	if prov == "" {
		prov = "*"
	}
	pat := strings.TrimSpace(in.ModelPattern)
	if pat == "" || in.InputUSDPer1M < 0 || in.OutputUSDPer1M < 0 {
		return nil, fmt.Errorf("%w: invalid model price input", errs.ErrInvalid)
	}
	cur := strings.TrimSpace(strings.ToUpper(in.Currency))
	if cur == "" {
		cur = "USD"
	}
	saved, err := s.store.UpsertPrice(ctx, &model.ModelPrice{
		Provider:       prov,
		ModelPattern:   pat,
		InputUSDPer1M:  in.InputUSDPer1M,
		OutputUSDPer1M: in.OutputUSDPer1M,
		Currency:       cur,
	})
	if err != nil {
		return nil, err
	}
	return &PriceEntry{
		ID:             saved.ID,
		Provider:       saved.Provider,
		ModelPattern:   saved.ModelPattern,
		InputUSDPer1M:  saved.InputUSDPer1M,
		OutputUSDPer1M: saved.OutputUSDPer1M,
		Currency:       saved.Currency,
		Custom:         true,
	}, nil
}

// DeletePrice 删除自定义模型定价。
func (s *Service) DeletePrice(ctx context.Context, id uint64) error {
	if s.store == nil {
		return errs.ErrNotWiredYet
	}
	return s.store.DeletePrice(ctx, id)
}
