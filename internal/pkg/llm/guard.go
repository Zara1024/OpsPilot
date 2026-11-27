package llm

import (
	"context"
	"strings"
	"time"
)

// GuardCheckInput 描述请求发出前的配额/熔断检查上下文。
type GuardCheckInput struct {
	Provider        string
	Model           string
	Source          string
	UserID          uint64
	OrgID           uint64
	SessionID       string
	RefID           string
	EstPromptTokens int
}

// GuardRecordInput 描述一次 LLM 调用（成功、失败或被熔断拦截）后的计费流水。
type GuardRecordInput struct {
	Provider         string
	Model            string
	Source           string
	UserID           uint64
	OrgID            uint64
	SessionID        string
	RefID            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	LatencyMS        int64
	Status           string // ok | error | blocked
	ErrorMessage     string
}

// CostGuard 是挂在共享 llm.Client 上的成本配额检查与使用量记账接口。
type CostGuard interface {
	Check(ctx context.Context, in GuardCheckInput) error
	Record(ctx context.Context, in GuardRecordInput) error
}

// WithCostGuard 在 LLM Client 最外层挂载配额/熔断守卫与用量落账中间件。
func WithCostGuard(inner Client, guard CostGuard) Client {
	if inner == nil || guard == nil {
		return inner
	}
	return &costGuardClient{inner: inner, guard: guard}
}

type costGuardClient struct {
	inner Client
	guard CostGuard
}

func (c *costGuardClient) ResolveTarget(ctx context.Context, req ChatReq) (string, string) {
	if tr, ok := c.inner.(TargetResolver); ok {
		return tr.ResolveTarget(ctx, req)
	}
	return strings.TrimSpace(req.Provider), strings.TrimSpace(req.Model)
}

func (c *costGuardClient) Chat(ctx context.Context, req ChatReq) (*ChatResp, error) {
	provider, model := c.ResolveTarget(ctx, req)
	meta := CallMetaFrom(ctx)
	userID := meta.UserID
	if userID == 0 {
		userID = req.UserID
	}
	source := meta.Source
	if source == "" {
		source = SourceOther
	}

	estPrompt := estimatePromptTokens(req.Messages)
	checkIn := GuardCheckInput{
		Provider:        provider,
		Model:           model,
		Source:          source,
		UserID:          userID,
		OrgID:           meta.OrgID,
		SessionID:       meta.SessionID,
		RefID:           meta.RefID,
		EstPromptTokens: estPrompt,
	}
	if err := c.guard.Check(ctx, checkIn); err != nil {
		recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		_ = c.guard.Record(recCtx, GuardRecordInput{
			Provider:     provider,
			Model:        model,
			Source:       source,
			UserID:       userID,
			OrgID:        meta.OrgID,
			SessionID:    meta.SessionID,
			RefID:        meta.RefID,
			Status:       "blocked",
			ErrorMessage: truncateErrorMsg(err.Error(), 500),
		})
		cancel()
		return nil, err
	}

	start := time.Now()
	resp, err := c.inner.Chat(ctx, req)
	latencyMS := time.Since(start).Milliseconds()

	recIn := GuardRecordInput{
		Provider:  provider,
		Model:     model,
		Source:    source,
		UserID:    userID,
		OrgID:     meta.OrgID,
		SessionID: meta.SessionID,
		RefID:     meta.RefID,
		LatencyMS: latencyMS,
		Status:    "ok",
	}
	if err != nil {
		recIn.Status = "error"
		recIn.ErrorMessage = truncateErrorMsg(err.Error(), 500)
	} else if resp != nil {
		if resp.Provider != "" {
			recIn.Provider = resp.Provider
		}
		if resp.Model != "" {
			recIn.Model = resp.Model
		}
		recIn.PromptTokens = resp.Usage.PromptTokens
		recIn.CompletionTokens = resp.Usage.CompletionTokens
		recIn.TotalTokens = resp.Usage.TotalTokens
		if recIn.TotalTokens == 0 {
			recIn.TotalTokens = recIn.PromptTokens + recIn.CompletionTokens
		}
	}

	recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	_ = c.guard.Record(recCtx, recIn)
	cancel()
	return resp, err
}

func truncateErrorMsg(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
