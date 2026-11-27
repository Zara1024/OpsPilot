// Package aicost 提供 AI 模型成本核算、多维度配额控制与熔断保护服务。
package aicost

import (
	"sort"
	"strings"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/aicost"
)

// PriceEntry 描述单条模型定价信息（每百万 Token 美元价格）。
type PriceEntry struct {
	ID             uint64  `json:"id,omitempty"`
	Provider       string  `json:"provider"`
	ModelPattern   string  `json:"model_pattern"`
	InputUSDPer1M  float64 `json:"input_usd_per_1m"`
	OutputUSDPer1M float64 `json:"output_usd_per_1m"`
	Currency       string  `json:"currency"`
	Custom         bool    `json:"custom"`
}

var builtinModelPrices = []PriceEntry{
	// OpenAI
	{Provider: "openai", ModelPattern: "gpt-4o-mini*", InputUSDPer1M: 0.15, OutputUSDPer1M: 0.60, Currency: "USD"},
	{Provider: "openai", ModelPattern: "gpt-4o*", InputUSDPer1M: 2.50, OutputUSDPer1M: 10.00, Currency: "USD"},
	{Provider: "openai", ModelPattern: "gpt-4.1-mini*", InputUSDPer1M: 0.40, OutputUSDPer1M: 1.60, Currency: "USD"},
	{Provider: "openai", ModelPattern: "gpt-4.1*", InputUSDPer1M: 2.00, OutputUSDPer1M: 8.00, Currency: "USD"},
	{Provider: "openai", ModelPattern: "gpt-5*", InputUSDPer1M: 2.50, OutputUSDPer1M: 10.00, Currency: "USD"},
	{Provider: "openai", ModelPattern: "o4-mini*", InputUSDPer1M: 1.10, OutputUSDPer1M: 4.40, Currency: "USD"},
	{Provider: "openai", ModelPattern: "o3*", InputUSDPer1M: 2.00, OutputUSDPer1M: 8.00, Currency: "USD"},

	// Anthropic
	{Provider: "anthropic", ModelPattern: "claude-opus-4*", InputUSDPer1M: 15.00, OutputUSDPer1M: 75.00, Currency: "USD"},
	{Provider: "anthropic", ModelPattern: "claude-sonnet-4*", InputUSDPer1M: 3.00, OutputUSDPer1M: 15.00, Currency: "USD"},
	{Provider: "anthropic", ModelPattern: "claude-3-7-sonnet*", InputUSDPer1M: 3.00, OutputUSDPer1M: 15.00, Currency: "USD"},
	{Provider: "anthropic", ModelPattern: "claude-3-5-sonnet*", InputUSDPer1M: 3.00, OutputUSDPer1M: 15.00, Currency: "USD"},
	{Provider: "anthropic", ModelPattern: "claude-haiku-4*", InputUSDPer1M: 0.80, OutputUSDPer1M: 4.00, Currency: "USD"},
	{Provider: "anthropic", ModelPattern: "claude-3-5-haiku*", InputUSDPer1M: 0.80, OutputUSDPer1M: 4.00, Currency: "USD"},

	// Google Gemini
	{Provider: "gemini", ModelPattern: "gemini-2.5-pro*", InputUSDPer1M: 1.25, OutputUSDPer1M: 10.00, Currency: "USD"},
	{Provider: "gemini", ModelPattern: "gemini-3*pro*", InputUSDPer1M: 1.25, OutputUSDPer1M: 10.00, Currency: "USD"},
	{Provider: "gemini", ModelPattern: "gemini-2.5-flash*", InputUSDPer1M: 0.15, OutputUSDPer1M: 0.60, Currency: "USD"},
	{Provider: "gemini", ModelPattern: "gemini-3*flash*", InputUSDPer1M: 0.15, OutputUSDPer1M: 0.60, Currency: "USD"},
	{Provider: "gemini", ModelPattern: "gemini-2.0-flash*", InputUSDPer1M: 0.10, OutputUSDPer1M: 0.40, Currency: "USD"},

	// DeepSeek
	{Provider: "deepseek", ModelPattern: "deepseek-reasoner*", InputUSDPer1M: 0.55, OutputUSDPer1M: 2.19, Currency: "USD"},
	{Provider: "deepseek", ModelPattern: "deepseek-chat*", InputUSDPer1M: 0.27, OutputUSDPer1M: 1.10, Currency: "USD"},

	// Domestic mainstream models (USD equivalent estimate per 1M tokens)
	{Provider: "zhipu", ModelPattern: "glm-5*", InputUSDPer1M: 0.70, OutputUSDPer1M: 2.80, Currency: "USD"},
	{Provider: "zhipu", ModelPattern: "glm-4*", InputUSDPer1M: 0.50, OutputUSDPer1M: 1.50, Currency: "USD"},
	{Provider: "kimi", ModelPattern: "kimi-*", InputUSDPer1M: 0.60, OutputUSDPer1M: 2.40, Currency: "USD"},
	{Provider: "kimi", ModelPattern: "moonshot-*", InputUSDPer1M: 0.60, OutputUSDPer1M: 2.40, Currency: "USD"},
	{Provider: "minimax", ModelPattern: "MiniMax-*", InputUSDPer1M: 0.40, OutputUSDPer1M: 1.60, Currency: "USD"},
	{Provider: "xiaomi", ModelPattern: "mimo-*", InputUSDPer1M: 0.30, OutputUSDPer1M: 1.20, Currency: "USD"},
}

// BuiltinPrices 返回内置模型参考价格表副本。
func BuiltinPrices() []PriceEntry {
	out := make([]PriceEntry, len(builtinModelPrices))
	copy(out, builtinModelPrices)
	return out
}

// matchGlob 支持大小写不敏感的通配符匹配（* 匹配任意长度，? 匹配单字符）。
func matchGlob(pattern, value string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	v := strings.ToLower(strings.TrimSpace(value))
	if p == "" {
		return false
	}
	if p == "*" {
		return true
	}
	pi, vi := 0, 0
	starIdx, matchIdx := -1, 0
	for vi < len(v) {
		if pi < len(p) && (p[pi] == '?' || p[pi] == v[vi]) {
			pi++
			vi++
			continue
		}
		if pi < len(p) && p[pi] == '*' {
			starIdx = pi
			matchIdx = vi
			pi++
			continue
		}
		if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			vi = matchIdx
			continue
		}
		return false
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

func patternSpecificity(pattern string) int {
	p := strings.TrimSpace(pattern)
	nonWild := 0
	for i := 0; i < len(p); i++ {
		if p[i] != '*' && p[i] != '?' {
			nonWild++
		}
	}
	exactBonus := 0
	if !strings.ContainsAny(p, "*?") {
		exactBonus = 1000
	}
	return exactBonus + nonWild
}

// ResolvePrice 按自定义单价优先、再匹配内置价格表的策略计算命中的定价条目。
func ResolvePrice(provider, modelName string, custom []*model.ModelPrice) (PriceEntry, bool) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	mod := strings.TrimSpace(modelName)

	pickBest := func(candidates []PriceEntry) (PriceEntry, bool) {
		if len(candidates) == 0 {
			return PriceEntry{}, false
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			si := patternSpecificity(candidates[i].ModelPattern)
			sj := patternSpecificity(candidates[j].ModelPattern)
			if candidates[i].Provider != "*" && candidates[j].Provider == "*" {
				return true
			}
			if candidates[i].Provider == "*" && candidates[j].Provider != "*" {
				return false
			}
			return si > sj
		})
		return candidates[0], true
	}

	var customHits []PriceEntry
	for _, cp := range custom {
		if cp == nil {
			continue
		}
		cProv := strings.TrimSpace(strings.ToLower(cp.Provider))
		if cProv != "" && cProv != "*" && cProv != prov {
			continue
		}
		if matchGlob(cp.ModelPattern, mod) {
			customHits = append(customHits, PriceEntry{
				ID:             cp.ID,
				Provider:       cp.Provider,
				ModelPattern:   cp.ModelPattern,
				InputUSDPer1M:  cp.InputUSDPer1M,
				OutputUSDPer1M: cp.OutputUSDPer1M,
				Currency:       cp.Currency,
				Custom:         true,
			})
		}
	}
	if hit, ok := pickBest(customHits); ok {
		return hit, true
	}

	var builtinHits []PriceEntry
	for _, bp := range builtinModelPrices {
		bProv := strings.ToLower(bp.Provider)
		if prov != "" && bProv != prov {
			// 允许跨网关别名按模型名兜底匹配（如 custom/openai 网关下调用 claude-sonnet-4）
			if !matchGlob(bp.ModelPattern, mod) {
				continue
			}
		} else if !matchGlob(bp.ModelPattern, mod) {
			continue
		}
		builtinHits = append(builtinHits, bp)
	}
	if hit, ok := pickBest(builtinHits); ok {
		return hit, true
	}

	// 未知模型兜底估算单价，确保新模型或本地别名也能纳入门限监控（可用自定义单价覆盖为 0 或真实价）。
	if mod != "" {
		return PriceEntry{
			Provider:       provider,
			ModelPattern:   "*",
			InputUSDPer1M:  0.50,
			OutputUSDPer1M: 1.50,
			Currency:       "USD",
		}, true
	}
	return PriceEntry{}, false
}

// ComputeCostUSD 根据输入输出 Token 与定价表估算 USD 成本。
func ComputeCostUSD(provider, modelName string, promptTokens, completionTokens int, custom []*model.ModelPrice) float64 {
	if promptTokens <= 0 && completionTokens <= 0 {
		return 0
	}
	price, ok := ResolvePrice(provider, modelName, custom)
	if !ok {
		return 0
	}
	cost := (float64(promptTokens)*price.InputUSDPer1M + float64(completionTokens)*price.OutputUSDPer1M) / 1_000_000.0
	if cost < 0 {
		return 0
	}
	return cost
}
