package llm

import (
	"context"
	"errors"
	"testing"
)

type stubCostGuard struct {
	checkErr error
	checks   []GuardCheckInput
	records  []GuardRecordInput
}

func (g *stubCostGuard) Check(_ context.Context, in GuardCheckInput) error {
	g.checks = append(g.checks, in)
	return g.checkErr
}

func (g *stubCostGuard) Record(_ context.Context, in GuardRecordInput) error {
	g.records = append(g.records, in)
	return nil
}

type stubTargetClient struct {
	called   int
	resp     *ChatResp
	err      error
	provider string
	model    string
}

func (c *stubTargetClient) ResolveTarget(_ context.Context, req ChatReq) (string, string) {
	p := req.Provider
	if p == "" {
		p = c.provider
	}
	m := req.Model
	if m == "" {
		m = c.model
	}
	return p, m
}

func (c *stubTargetClient) Chat(_ context.Context, _ ChatReq) (*ChatResp, error) {
	c.called++
	return c.resp, c.err
}

func TestWithCostGuard_AllowAndRecord(t *testing.T) {
	inner := &stubTargetClient{
		provider: "openai",
		model:    "gpt-4o",
		resp: &ChatResp{
			Assistant: Message{Role: "assistant", Content: "ok"},
			Usage:     Usage{PromptTokens: 120, CompletionTokens: 30, TotalTokens: 150},
			Provider:  "openai",
			Model:     "gpt-4o-2024-11-20",
		},
	}
	guard := &stubCostGuard{}
	c := WithCostGuard(inner, guard)

	ctx := WithCallMeta(context.Background(), CallMeta{UserID: 7, OrgID: 2, SessionID: "s-1"})
	ctx = WithCallMeta(ctx, CallMeta{Source: SourceAlertRCA, RefID: "42"})

	resp, err := c.Chat(ctx, ChatReq{
		Messages: []Message{{Role: "user", Content: "check pod status"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Model != "gpt-4o-2024-11-20" || inner.called != 1 {
		t.Fatalf("unexpected resp=%+v called=%d", resp, inner.called)
	}
	if len(guard.checks) != 1 || guard.checks[0].Provider != "openai" || guard.checks[0].Model != "gpt-4o" {
		t.Fatalf("unexpected check input: %+v", guard.checks)
	}
	if guard.checks[0].UserID != 7 || guard.checks[0].OrgID != 2 || guard.checks[0].Source != SourceAlertRCA {
		t.Fatalf("expected merged CallMeta preserved, got %+v", guard.checks[0])
	}
	if len(guard.records) != 1 || guard.records[0].Status != "ok" || guard.records[0].TotalTokens != 150 {
		t.Fatalf("unexpected records: %+v", guard.records)
	}
	if guard.records[0].Model != "gpt-4o-2024-11-20" {
		t.Fatalf("expected resolved response model in ledger, got %s", guard.records[0].Model)
	}
}

func TestWithCostGuard_BlockedSkipsInnerAndLogsBlocked(t *testing.T) {
	inner := &stubTargetClient{provider: "deepseek", model: "deepseek-chat"}
	guard := &stubCostGuard{checkErr: ErrBudgetExceeded}
	c := WithCostGuard(inner, guard)

	ctx := WithCallMeta(context.Background(), CallMeta{Source: SourceChat, UserID: 9})
	_, err := c.Chat(ctx, ChatReq{Messages: []Message{{Role: "user", Content: "hi"}}})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
	if inner.called != 0 {
		t.Fatalf("expected inner not called when blocked, got %d", inner.called)
	}
	if len(guard.records) != 1 || guard.records[0].Status != "blocked" || guard.records[0].UserID != 9 {
		t.Fatalf("expected blocked audit record, got %+v", guard.records)
	}
}
