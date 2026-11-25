package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
)

type oncallCall struct {
	kind       string
	incidentID uint64
	status     string
	title      string
}

type spyOnCallDispatcher struct {
	mu    sync.Mutex
	calls []oncallCall
}

func (s *spyOnCallDispatcher) OnAlertFired(incidentID uint64, _, title, _ string, _ uint64, _ map[string]string, _ time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, oncallCall{kind: "fired", incidentID: incidentID, title: title})
}

func (s *spyOnCallDispatcher) OnAlertClosed(incidentID uint64, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, oncallCall{kind: "closed", incidentID: incidentID, status: status})
}

func TestAlertUsecase_OnCallBridgeLifecycle(t *testing.T) {
	repo := newFakeRepo()
	uc := NewUsecase(repo, nil)
	spy := &spyOnCallDispatcher{}
	uc.SetOnCallDispatcher(spy)

	ctx := context.Background()
	now := time.Now().UTC()
	devID := uint64(9)

	// 1. Brand-new incident triggers OnAlertFired.
	res, err := uc.RecordFiring(ctx, FiringInput{
		ScopeType:  model.RuleScopeHost,
		Scope:      "node-1",
		Rule:       "cpu_high",
		RuleName:   "CPU High",
		Severity:   "critical",
		DeviceID:   &devID,
		OccurredAt: now,
		Title:      "node-1 CPU > 95%",
	})
	if err != nil {
		t.Fatalf("RecordFiring new: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0].kind != "fired" || spy.calls[0].incidentID != res.Incident.ID {
		t.Fatalf("expected 1 OnAlertFired call, got %+v", spy.calls)
	}

	// 2. Subsequent firing of same open incident (bump) does NOT re-trigger OnAlertFired.
	if _, err := uc.RecordFiring(ctx, FiringInput{
		ScopeType:  model.RuleScopeHost,
		Scope:      "node-1",
		Rule:       "cpu_high",
		Severity:   "critical",
		DeviceID:   &devID,
		OccurredAt: now.Add(time.Minute),
		Title:      "node-1 CPU > 95%",
	}); err != nil {
		t.Fatalf("RecordFiring bump: %v", err)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("expected bump NOT to fire again, got %+v", spy.calls)
	}

	// 3. Ack & Resolve both notify OnAlertClosed.
	if err := uc.AckIncident(ctx, res.Incident.ID, 1, "investigating"); err != nil {
		t.Fatalf("AckIncident: %v", err)
	}
	if err := uc.ResolveIncident(ctx, res.Incident.ID, 1, "fixed"); err != nil {
		t.Fatalf("ResolveIncident: %v", err)
	}
	if len(spy.calls) != 3 ||
		spy.calls[1].kind != "closed" || spy.calls[1].status != model.IncidentStatusAcknowledged ||
		spy.calls[2].kind != "closed" || spy.calls[2].status != model.IncidentStatusResolved {
		t.Fatalf("expected closed calls on ack & resolve, got %+v", spy.calls)
	}

	// 4. Reopening resolved incident fires OnAlertFired again.
	if _, err := uc.RecordFiring(ctx, FiringInput{
		ScopeType:  model.RuleScopeHost,
		Scope:      "node-1",
		Rule:       "cpu_high",
		Severity:   "critical",
		DeviceID:   &devID,
		OccurredAt: now.Add(2 * time.Minute),
		Title:      "node-1 CPU > 95%",
	}); err != nil {
		t.Fatalf("RecordFiring reopen: %v", err)
	}
	if len(spy.calls) != 4 || spy.calls[3].kind != "fired" {
		t.Fatalf("expected reopen to call OnAlertFired, got %+v", spy.calls)
	}

	// 5. SilenceIncident notifies OnAlertClosed with silenced status.
	if err := uc.SilenceIncident(ctx, res.Incident.ID, 1, "30m", "maintenance window"); err != nil {
		t.Fatalf("SilenceIncident: %v", err)
	}
	if len(spy.calls) != 5 || spy.calls[4].kind != "closed" || spy.calls[4].status != model.IncidentStatusSilenced {
		t.Fatalf("expected silence to call OnAlertClosed(silenced), got %+v", spy.calls)
	}

	// 6. SystemResolveIncident notifies OnAlertClosed(resolved).
	if resolved, err := uc.SystemResolveIncident(ctx, res.Incident.DedupeKey, "recovered", now.Add(5*time.Minute)); err != nil || !resolved {
		t.Fatalf("SystemResolveIncident: resolved=%v err=%v", resolved, err)
	}
	if len(spy.calls) != 6 || spy.calls[5].kind != "closed" || spy.calls[5].status != model.IncidentStatusResolved {
		t.Fatalf("expected SystemResolveIncident to call OnAlertClosed(resolved), got %+v", spy.calls)
	}
}
