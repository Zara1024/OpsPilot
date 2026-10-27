package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dedupeReproSpawner struct {
	result *WorkerHandle
	calls  atomic.Int64
}

func (s *dedupeReproSpawner) SpawnWorker(context.Context, SpawnWorkerRequest) (*WorkerHandle, error) {
	s.calls.Add(1)
	return s.result, nil
}

func (*dedupeReproSpawner) SendToWorker(context.Context, string, string) error { return nil }
func (*dedupeReproSpawner) StopWorker(context.Context, string) error           { return nil }
func (*dedupeReproSpawner) GetWorker(string) (*WorkerHandle, bool)             { return nil, false }

func TestAgentToolExpiredDedupeEntriesAreReleased(t *testing.T) {
	spawner := &dedupeReproSpawner{result: &WorkerHandle{
		ID:     "worker",
		Status: "completed",
		Result: strings.Repeat("evidence ", 512),
	}}
	tool := NewAgentTool(spawner, nil, nil)
	const tasks = 64
	for i := range tasks {
		args := fmt.Sprintf(`{"description":"test","subagent_type":"specialist","prompt":"task-%d"}`, i)
		if _, err := tool.InvokableRun(context.Background(), args); err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
	}

	// Advance the entry lifetimes without waiting 90 seconds.
	tool.dedupe.Range(func(_, value any) bool {
		value.(*dedupeEntry).expiry = time.Now().Add(-time.Second)
		return true
	})
	if _, err := tool.InvokableRun(context.Background(), `{"description":"test","subagent_type":"specialist","prompt":"next task"}`); err != nil {
		t.Fatalf("next dispatch: %v", err)
	}

	expired := 0
	tool.dedupe.Range(func(_, value any) bool {
		if time.Now().After(value.(*dedupeEntry).expiry) {
			expired++
		}
		return true
	})
	if expired != 0 {
		t.Fatalf("expired dedupe results retained after another dispatch: %d of %d", expired, tasks)
	}
}

func TestAgentToolDedupeKeepsRecentResult(t *testing.T) {
	spawner := &dedupeReproSpawner{result: &WorkerHandle{ID: "worker", Status: "completed", Result: "evidence"}}
	tool := NewAgentTool(spawner, nil, nil)
	args := `{"description":"test","subagent_type":"specialist","prompt":"same task"}`
	for range 2 {
		if _, err := tool.InvokableRun(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	if got := spawner.calls.Load(); got != 1 {
		t.Fatalf("worker spawns = %d, want 1 for a recent duplicate", got)
	}
}

func TestAgentToolDedupeCapacity(t *testing.T) {
	spawner := &dedupeReproSpawner{result: &WorkerHandle{ID: "worker", Status: "completed", Result: "evidence"}}
	tool := NewAgentTool(spawner, nil, nil)
	const overflow = 64
	for i := range dedupeMaxEntries + overflow {
		args := fmt.Sprintf(`{"description":"test","subagent_type":"specialist","prompt":"task-%d"}`, i)
		if _, err := tool.InvokableRun(context.Background(), args); err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
	}
	entries := 0
	tool.dedupe.Range(func(_, _ any) bool { entries++; return true })
	if entries != dedupeMaxEntries {
		t.Fatalf("dedupe entries = %d, want %d", entries, dedupeMaxEntries)
	}
	for _, prompt := range []string{fmt.Sprintf("task-%d", dedupeMaxEntries+overflow-1), "task-0"} {
		args := fmt.Sprintf(`{"description":"test","subagent_type":"specialist","prompt":%q}`, prompt)
		if _, err := tool.InvokableRun(context.Background(), args); err != nil {
			t.Fatalf("dispatch %s: %v", prompt, err)
		}
	}
	if got := spawner.calls.Load(); got != dedupeMaxEntries+overflow+1 {
		t.Fatalf("worker spawns = %d, want recent task cached and oldest task evicted", got)
	}
}

func TestAgentToolDedupeConcurrentCapacity(t *testing.T) {
	spawner := &dedupeReproSpawner{result: &WorkerHandle{ID: "worker", Status: "completed", Result: "evidence"}}
	tool := NewAgentTool(spawner, nil, nil)
	const tasks = dedupeMaxEntries + 64
	var workers sync.WaitGroup
	errs := make(chan error, tasks)
	for i := range tasks {
		workers.Add(1)
		go func() {
			defer workers.Done()
			args := fmt.Sprintf(`{"description":"test","subagent_type":"specialist","prompt":"concurrent-%d"}`, i)
			_, err := tool.InvokableRun(context.Background(), args)
			errs <- err
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries := 0
	tool.dedupe.Range(func(_, _ any) bool { entries++; return true })
	if entries != dedupeMaxEntries {
		t.Fatalf("dedupe entries = %d, want %d", entries, dedupeMaxEntries)
	}
}
