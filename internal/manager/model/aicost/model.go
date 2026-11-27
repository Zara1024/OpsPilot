// Package aicost 定义 AI 模型调用流水、成本配额、熔断状态及模型单价领域实体。
package aicost

import (
	"strings"
	"time"
)

// 配额作用域类型枚举。
const (
	ScopeGlobal = "global"
	ScopeOrg    = "org"
	ScopeUser   = "user"
	ScopeModel  = "model"
)

// 配额超限处理动作枚举。
const (
	ActionBlock = "block"
	ActionWarn  = "warn"
)

// 调用记录状态枚举。
const (
	UsageStatusOK      = "ok"
	UsageStatusError   = "error"
	UsageStatusBlocked = "blocked"
)

// IsValidScopeType 判断配额作用域是否合法。
func IsValidScopeType(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case ScopeGlobal, ScopeOrg, ScopeUser, ScopeModel:
		return true
	default:
		return false
	}
}

// IsValidActionOnExceed 判断超限动作是否合法。
func IsValidActionOnExceed(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case ActionBlock, ActionWarn:
		return true
	default:
		return false
	}
}

// IsValidUsageStatus 判断调用记录状态是否合法。
func IsValidUsageStatus(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case UsageStatusOK, UsageStatusError, UsageStatusBlocked:
		return true
	default:
		return false
	}
}

// UsageLog 记录每一次真实发起或被熔断拦截的 LLM 请求账本。
type UsageLog struct {
	ID               uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID           uint64    `gorm:"not null;index" json:"user_id"`
	OrgID            uint64    `gorm:"not null;index" json:"org_id"`
	Provider         string    `gorm:"type:varchar(64);not null;index" json:"provider"`
	Model            string    `gorm:"type:varchar(128);not null;index" json:"model"`
	Source           string    `gorm:"type:varchar(32);not null;index" json:"source"`
	SessionID        string    `gorm:"type:varchar(64);index" json:"session_id,omitempty"`
	RefID            string    `gorm:"type:varchar(64)" json:"ref_id,omitempty"`
	PromptTokens     int       `gorm:"not null;default:0" json:"prompt_tokens"`
	CompletionTokens int       `gorm:"not null;default:0" json:"completion_tokens"`
	TotalTokens      int       `gorm:"not null;default:0" json:"total_tokens"`
	CostUSD          float64   `gorm:"not null;default:0" json:"cost_usd"`
	LatencyMS        int64     `gorm:"not null;default:0" json:"latency_ms"`
	Status           string    `gorm:"type:varchar(24);not null;index" json:"status"`
	ErrorMessage     string    `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt        time.Time `gorm:"not null;index" json:"created_at"`
}

func (UsageLog) TableName() string { return "ai_usage_logs" }

// CostQuota 定义按全局、组织、用户或模型匹配的 Token/美元配额与熔断器状态。
type CostQuota struct {
	ID                  uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                string     `gorm:"type:varchar(128);not null" json:"name"`
	ScopeType           string     `gorm:"type:varchar(24);not null;uniqueIndex:uniq_ai_quota_scope,priority:1" json:"scope_type"`
	ScopeKey            string     `gorm:"type:varchar(128);not null;uniqueIndex:uniq_ai_quota_scope,priority:2" json:"scope_key"`
	Enabled             bool       `gorm:"not null;index" json:"enabled"`
	DailyTokenLimit     int64      `gorm:"not null;default:0" json:"daily_token_limit"`
	MonthlyTokenLimit   int64      `gorm:"not null;default:0" json:"monthly_token_limit"`
	DailyCostLimitUSD   float64    `gorm:"not null;default:0" json:"daily_cost_limit_usd"`
	MonthlyCostLimitUSD float64    `gorm:"not null;default:0" json:"monthly_cost_limit_usd"`
	WarnThresholdPct    int        `gorm:"not null;default:80" json:"warn_threshold_pct"`
	ActionOnExceed      string     `gorm:"type:varchar(24);not null;default:block" json:"action_on_exceed"`
	CircuitOpen         bool       `gorm:"not null" json:"circuit_open"`
	CircuitOpenedAt     *time.Time `json:"circuit_opened_at,omitempty"`
	CircuitReason       string     `gorm:"type:varchar(255)" json:"circuit_reason,omitempty"`
	Description         string     `gorm:"type:varchar(255)" json:"description,omitempty"`
	UpdatedBy           uint64     `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (CostQuota) TableName() string { return "ai_cost_quotas" }

// ModelPrice 存储管理员自定义覆盖的每百万 Token（USD）定价规则。
type ModelPrice struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Provider       string    `gorm:"type:varchar(64);not null;uniqueIndex:uniq_ai_price_pm,priority:1" json:"provider"`
	ModelPattern   string    `gorm:"type:varchar(128);not null;uniqueIndex:uniq_ai_price_pm,priority:2" json:"model_pattern"`
	InputUSDPer1M  float64   `gorm:"column:input_usd_per_1m;not null;default:0" json:"input_usd_per_1m"`
	OutputUSDPer1M float64   `gorm:"column:output_usd_per_1m;not null;default:0" json:"output_usd_per_1m"`
	Currency       string    `gorm:"type:varchar(16);not null;default:USD" json:"currency"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (ModelPrice) TableName() string { return "ai_model_prices" }
