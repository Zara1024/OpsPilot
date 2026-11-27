package aicost

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/manager/data/aicost/store"
	model "github.com/Zara1024/OpsPilot/internal/manager/model/aicost"
	"github.com/Zara1024/OpsPilot/internal/pkg/llm"
)

type stubOrgReader map[uint64][]uint64

func (m stubOrgReader) ListOrgIDsByUser(_ context.Context, userID uint64) ([]uint64, error) {
	return m[userID], nil
}

func newTestService(t *testing.T, orgs OrgMembershipReader) (*Service, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "svc.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, store.Migrate(db))

	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	svc := New(store.New(db), orgs, nil)
	svc.now = func() time.Time { return now }
	return svc, &now
}

func TestPricingComputationAndCustomOverride(t *testing.T) {
	// gpt-4o builtin: $2.50 in / $10.00 out per 1M tokens
	c1 := ComputeCostUSD("openai", "gpt-4o", 1_000_000, 500_000, nil)
	require.InDelta(t, 7.50, c1, 0.0001)

	custom := []*model.ModelPrice{
		{Provider: "openai", ModelPattern: "gpt-4o*", InputUSDPer1M: 1.0, OutputUSDPer1M: 2.0, Currency: "USD"},
	}
	c2 := ComputeCostUSD("openai", "gpt-4o-2024", 1_000_000, 500_000, custom)
	require.InDelta(t, 2.00, c2, 0.0001)
}

func TestCostGuard_WarnVsBlockAndDayRolloverReset(t *testing.T) {
	ctx := context.Background()
	svc, now := newTestService(t, stubOrgReader{7: {3}})

	// Create org-level block quota
	q, err := svc.CreateQuota(ctx, 1, QuotaInput{
		Name:             "Org 3 Cap",
		ScopeType:        model.ScopeOrg,
		ScopeKey:         "3",
		Enabled:          true,
		DailyTokenLimit:  1000,
		WarnThresholdPct: 80,
		ActionOnExceed:   model.ActionBlock,
	})
	require.NoError(t, err)

	// Record 900 tokens -> warn threshold reached, still under limit
	require.NoError(t, svc.Record(ctx, llm.GuardRecordInput{
		Provider: "openai", Model: "gpt-4o", Source: llm.SourceChat,
		UserID: 7, PromptTokens: 600, CompletionTokens: 300, TotalTokens: 900, Status: "ok",
	}))
	require.NoError(t, svc.Check(ctx, llm.GuardCheckInput{
		Provider: "openai", Model: "gpt-4o", UserID: 7, EstPromptTokens: 50,
	}))

	// Another 200 est tokens exceeds 1000 -> trips circuit & returns ErrBudgetExceeded
	err = svc.Check(ctx, llm.GuardCheckInput{
		Provider: "openai", Model: "gpt-4o", UserID: 7, EstPromptTokens: 200,
	})
	require.True(t, errors.Is(err, llm.ErrBudgetExceeded))

	quotas, err := svc.ListQuotas(ctx)
	require.NoError(t, err)
	require.Len(t, quotas, 1)
	require.True(t, quotas[0].CircuitOpen)
	require.Equal(t, "circuit_open", quotas[0].HealthState)

	// Next UTC day -> auto-resets auto-tripped circuit
	*now = now.Add(24 * time.Hour)
	require.NoError(t, svc.Check(ctx, llm.GuardCheckInput{
		Provider: "openai", Model: "gpt-4o", UserID: 7, EstPromptTokens: 200,
	}))
	quotas2, err := svc.ListQuotas(ctx)
	require.NoError(t, err)
	require.False(t, quotas2[0].CircuitOpen)

	// Manual trip stays closed across days until manual reset
	_, err = svc.TripCircuit(ctx, q.ID, 1, "maintenance")
	require.NoError(t, err)
	*now = now.Add(24 * time.Hour)
	err = svc.Check(ctx, llm.GuardCheckInput{
		Provider: "openai", Model: "gpt-4o", UserID: 7, EstPromptTokens: 10,
	})
	require.True(t, errors.Is(err, llm.ErrBudgetExceeded))

	_, err = svc.ResetCircuit(ctx, q.ID, 1)
	require.NoError(t, err)
	require.NoError(t, svc.Check(ctx, llm.GuardCheckInput{
		Provider: "openai", Model: "gpt-4o", UserID: 7, EstPromptTokens: 10,
	}))
}

func TestCostGuard_ModelWildcardQuotaAndWarnAction(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)

	_, err := svc.CreateQuota(ctx, 1, QuotaInput{
		Name:              "Expensive Opus Soft Cap",
		ScopeType:         model.ScopeModel,
		ScopeKey:          "claude-opus-*",
		Enabled:           true,
		DailyCostLimitUSD: 1.0,
		ActionOnExceed:    model.ActionWarn,
	})
	require.NoError(t, err)

	// Record $1.50 usage on claude-opus-4-1
	require.NoError(t, svc.Record(ctx, llm.GuardRecordInput{
		Provider: "anthropic", Model: "claude-opus-4-1", Source: llm.SourceAlertRCA,
		PromptTokens: 100_000, CompletionTokens: 0, TotalTokens: 100_000, Status: "ok",
	}))

	// Warn-only quota allows request without tripping
	require.NoError(t, svc.Check(ctx, llm.GuardCheckInput{
		Provider: "anthropic", Model: "claude-opus-4-1", EstPromptTokens: 100,
	}))

	sum, err := svc.Summary(ctx, 7)
	require.NoError(t, err)
	require.Len(t, sum.Quotas, 1)
	require.InDelta(t, 1.50, sum.Quotas[0].DailyCostUsedUSD, 0.001)
	require.Equal(t, "warn", sum.Quotas[0].HealthState)
}
