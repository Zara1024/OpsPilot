package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/aicost"
	"github.com/Zara1024/OpsPilot/internal/pkg/dbx"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// LogFilter 描述用量流水列表过滤条件。
type LogFilter struct {
	Since  *time.Time
	Source string
	Model  string
	Status string
	UserID uint64
	OrgID  uint64
	Limit  int
	Offset int
}

// WindowTotals 保存某时间窗内的 Token、费用与调用/拦截次数。
type WindowTotals struct {
	Requests         int64   `json:"requests"`
	BlockedCount     int64   `json:"blocked_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// DailyBucket 表示按 UTC 日期聚合的用量趋势点。
type DailyBucket struct {
	Date         string  `json:"date"`
	Requests     int64   `json:"requests"`
	BlockedCount int64   `json:"blocked_count"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// GroupStat 表示按模型、业务来源、组织或用户维度的统计明细。
type GroupStat struct {
	Key              string  `json:"key"`
	Secondary        string  `json:"secondary,omitempty"`
	Requests         int64   `json:"requests"`
	BlockedCount     int64   `json:"blocked_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// OverviewStats 聚合一段时间内各维度的成本与调用统计。
type OverviewStats struct {
	Totals   WindowTotals  `json:"totals"`
	Daily    []DailyBucket `json:"daily"`
	ByModel  []GroupStat   `json:"by_model"`
	BySource []GroupStat   `json:"by_source"`
	ByOrg    []GroupStat   `json:"by_org"`
	ByUser   []GroupStat   `json:"by_user"`
}

// ModelUsageSlice 用于按模型通配符做精确或模式求和。
type ModelUsageSlice struct {
	Model       string
	TotalTokens int64
	CostUSD     float64
}

// Store 提供 AI 用量流水、配额/熔断状态与自定义单价持久化操作。
type Store struct {
	db *gorm.DB
}

// New 构造 aicost Store。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) conn(ctx context.Context) *gorm.DB {
	return dbx.WithContext(ctx, s.db)
}

func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var myErr *driver.MySQLError
	if errors.As(err, &myErr) && myErr.Number == 1062 {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "duplicate key value violates unique constraint")
}

// InsertUsage 写入单条 AI 请求记账日志。
func (s *Store) InsertUsage(ctx context.Context, row *model.UsageLog) error {
	if row == nil {
		return errs.ErrInvalid
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	} else {
		row.CreatedAt = row.CreatedAt.UTC()
	}
	return s.conn(ctx).Create(row).Error
}

// ListUsageLogs 分页查询 AI 请求日志与总条数。
func (s *Store) ListUsageLogs(ctx context.Context, f LogFilter) ([]*model.UsageLog, int64, error) {
	q := s.conn(ctx).Model(&model.UsageLog{})
	if f.Since != nil {
		q = q.Where("created_at >= ?", f.Since.UTC())
	}
	if src := strings.TrimSpace(f.Source); src != "" {
		q = q.Where("source = ?", src)
	}
	if m := strings.TrimSpace(f.Model); m != "" {
		q = q.Where("model = ?", m)
	}
	if st := strings.TrimSpace(f.Status); st != "" {
		q = q.Where("status = ?", st)
	}
	if f.UserID > 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.OrgID > 0 {
		q = q.Where("org_id = ?", f.OrgID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	var rows []*model.UsageLog
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// SumUsageWindow 统计给定作用域在 since 之后的累计 Token、费用及拦截数（作用域 global/org/user 用 SQL 精确过滤；model 返回分模型切片由上层做通配符匹配）。
func (s *Store) SumUsageWindow(ctx context.Context, scopeType, scopeKey string, since time.Time) (WindowTotals, error) {
	var out WindowTotals
	q := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			"COUNT(*) AS requests, "+
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, "+
				"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, "+
				"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, "+
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, "+
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", since.UTC())

	switch scopeType {
	case model.ScopeGlobal:
	case model.ScopeOrg:
		q = q.Where("org_id = ?", scopeKey)
	case model.ScopeUser:
		q = q.Where("user_id = ?", scopeKey)
	case model.ScopeModel:
		q = q.Where("model = ?", scopeKey)
	default:
		return out, fmt.Errorf("%w: invalid scope type %q", errs.ErrInvalid, scopeType)
	}
	err := q.Scan(&out).Error
	return out, err
}

// ListUsageByModelSince 按模型聚合 since 之后的用量，用于支持含通配符的模型配额匹配。
func (s *Store) ListUsageByModelSince(ctx context.Context, since time.Time) ([]ModelUsageSlice, error) {
	var rows []ModelUsageSlice
	err := s.conn(ctx).Model(&model.UsageLog{}).
		Select("model, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(cost_usd), 0) AS cost_usd").
		Where("created_at >= ? AND status <> ?", since.UTC(), model.UsageStatusBlocked).
		Group("model").
		Scan(&rows).Error
	return rows, err
}

// AggregateOverview 统计 since 之后的总览、每日趋势及模型/来源/组织/用户分布。
func (s *Store) AggregateOverview(ctx context.Context, since time.Time) (*OverviewStats, error) {
	utcSince := since.UTC()
	out := &OverviewStats{}

	if err := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			"COUNT(*) AS requests, "+
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, "+
				"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, "+
				"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, "+
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, "+
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", utcSince).
		Scan(&out.Totals).Error; err != nil {
		return nil, err
	}

	dateExpr := "substr(created_at, 1, 10)"
	if s.db != nil && s.db.Dialector != nil && s.db.Dialector.Name() == "mysql" {
		dateExpr = "DATE_FORMAT(created_at, '%Y-%m-%d')"
	}
	if err := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			dateExpr + " AS date, " +
				"COUNT(*) AS requests, " +
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, " +
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, " +
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", utcSince).
		Group(dateExpr).
		Order("date ASC").
		Scan(&out.Daily).Error; err != nil {
		return nil, err
	}

	if err := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			"model AS `key`, provider AS secondary, COUNT(*) AS requests, " +
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, " +
				"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, " +
				"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, " +
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, " +
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", utcSince).
		Group("model, provider").
		Order("cost_usd DESC, total_tokens DESC").
		Limit(20).
		Scan(&out.ByModel).Error; err != nil {
		return nil, err
	}

	if err := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			"source AS `key`, COUNT(*) AS requests, " +
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, " +
				"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, " +
				"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, " +
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, " +
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", utcSince).
		Group("source").
		Order("cost_usd DESC, total_tokens DESC").
		Scan(&out.BySource).Error; err != nil {
		return nil, err
	}

	type numericGroupRow struct {
		ID               uint64
		Requests         int64
		BlockedCount     int64
		PromptTokens     int64
		CompletionTokens int64
		TotalTokens      int64
		CostUSD          float64
	}
	var orgRows []numericGroupRow
	if err := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			"org_id AS id, COUNT(*) AS requests, " +
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, " +
				"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, " +
				"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, " +
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, " +
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", utcSince).
		Group("org_id").
		Order("cost_usd DESC, total_tokens DESC").
		Limit(20).
		Scan(&orgRows).Error; err != nil {
		return nil, err
	}
	for _, r := range orgRows {
		out.ByOrg = append(out.ByOrg, GroupStat{
			Key:              fmt.Sprintf("%d", r.ID),
			Requests:         r.Requests,
			BlockedCount:     r.BlockedCount,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.TotalTokens,
			CostUSD:          r.CostUSD,
		})
	}

	var userRows []numericGroupRow
	if err := s.conn(ctx).Model(&model.UsageLog{}).
		Select(
			"user_id AS id, COUNT(*) AS requests, " +
				"COALESCE(SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END), 0) AS blocked_count, " +
				"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, " +
				"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, " +
				"COALESCE(SUM(total_tokens), 0) AS total_tokens, " +
				"COALESCE(SUM(cost_usd), 0) AS cost_usd",
		).
		Where("created_at >= ?", utcSince).
		Group("user_id").
		Order("cost_usd DESC, total_tokens DESC").
		Limit(20).
		Scan(&userRows).Error; err != nil {
		return nil, err
	}
	for _, r := range userRows {
		out.ByUser = append(out.ByUser, GroupStat{
			Key:              fmt.Sprintf("%d", r.ID),
			Requests:         r.Requests,
			BlockedCount:     r.BlockedCount,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.TotalTokens,
			CostUSD:          r.CostUSD,
		})
	}

	return out, nil
}

// ListQuotas 列出配额规则。
func (s *Store) ListQuotas(ctx context.Context, onlyEnabled bool) ([]*model.CostQuota, error) {
	q := s.conn(ctx).Model(&model.CostQuota{})
	if onlyEnabled {
		q = q.Where("enabled = ?", true)
	}
	var rows []*model.CostQuota
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 保持稳定排序：global 优先展示
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ScopeType == model.ScopeGlobal && rows[j].ScopeType != model.ScopeGlobal {
			return true
		}
		if rows[j].ScopeType == model.ScopeGlobal && rows[i].ScopeType != model.ScopeGlobal {
			return false
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}

// GetQuotaByID 按主键读取配额。
func (s *Store) GetQuotaByID(ctx context.Context, id uint64) (*model.CostQuota, error) {
	var row model.CostQuota
	err := s.conn(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateQuota 新增一条配额规则。
func (s *Store) CreateQuota(ctx context.Context, q *model.CostQuota) error {
	if q == nil {
		return errs.ErrInvalid
	}
	err := s.conn(ctx).Create(q).Error
	if isDuplicateKeyErr(err) {
		return fmt.Errorf("%w: quota already exists for (%s, %s)", errs.ErrConflict, q.ScopeType, q.ScopeKey)
	}
	return err
}

// UpdateQuota 更新配额规则配置。
func (s *Store) UpdateQuota(ctx context.Context, q *model.CostQuota) error {
	if q == nil || q.ID == 0 {
		return errs.ErrInvalid
	}
	res := s.conn(ctx).Model(&model.CostQuota{}).Where("id = ?", q.ID).Updates(map[string]any{
		"name":                   q.Name,
		"scope_type":             q.ScopeType,
		"scope_key":              q.ScopeKey,
		"enabled":                q.Enabled,
		"daily_token_limit":      q.DailyTokenLimit,
		"monthly_token_limit":    q.MonthlyTokenLimit,
		"daily_cost_limit_usd":   q.DailyCostLimitUSD,
		"monthly_cost_limit_usd": q.MonthlyCostLimitUSD,
		"warn_threshold_pct":     q.WarnThresholdPct,
		"action_on_exceed":       q.ActionOnExceed,
		"description":            q.Description,
		"updated_by":             q.UpdatedBy,
	})
	if res.Error != nil {
		if isDuplicateKeyErr(res.Error) {
			return fmt.Errorf("%w: quota already exists for (%s, %s)", errs.ErrConflict, q.ScopeType, q.ScopeKey)
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// SetCircuitState 更新指定配额的熔断开关与原因。
func (s *Store) SetCircuitState(ctx context.Context, id uint64, open bool, openedAt *time.Time, reason string) error {
	res := s.conn(ctx).Model(&model.CostQuota{}).Where("id = ?", id).Updates(map[string]any{
		"circuit_open":      open,
		"circuit_opened_at": openedAt,
		"circuit_reason":    reason,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// DeleteQuota 删除配额规则。
func (s *Store) DeleteQuota(ctx context.Context, id uint64) error {
	res := s.conn(ctx).Delete(&model.CostQuota{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// ListPrices 列出全部自定义模型单价配置。
func (s *Store) ListPrices(ctx context.Context) ([]*model.ModelPrice, error) {
	var rows []*model.ModelPrice
	err := s.conn(ctx).Order("provider ASC, model_pattern ASC").Find(&rows).Error
	return rows, err
}

// UpsertPrice 新增或更新 (provider, model_pattern) 模型单价。
func (s *Store) UpsertPrice(ctx context.Context, p *model.ModelPrice) (*model.ModelPrice, error) {
	if p == nil {
		return nil, errs.ErrInvalid
	}
	var existing model.ModelPrice
	err := s.conn(ctx).
		Where("provider = ? AND model_pattern = ?", p.Provider, p.ModelPattern).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if cErr := s.conn(ctx).Create(p).Error; cErr != nil {
			return nil, cErr
		}
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if uErr := s.conn(ctx).Model(&model.ModelPrice{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"input_usd_per_1m":  p.InputUSDPer1M,
		"output_usd_per_1m": p.OutputUSDPer1M,
		"currency":          p.Currency,
	}).Error; uErr != nil {
		return nil, uErr
	}
	if gErr := s.conn(ctx).First(&existing, existing.ID).Error; gErr != nil {
		return nil, gErr
	}
	return &existing, nil
}

// DeletePrice 删除一条自定义模型单价覆盖。
func (s *Store) DeletePrice(ctx context.Context, id uint64) error {
	res := s.conn(ctx).Delete(&model.ModelPrice{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}
