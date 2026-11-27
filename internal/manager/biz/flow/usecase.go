// Package flow is the workflow-orchestration biz tier (HLD-016).
// Definitions are user-authored DAGs (graph.go); the engine
// (engine.go) executes them through seams over the existing agent /
// tool / notify subsystems (nodes.go).
package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/flow"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// Repo is the flow-definition persistence contract.
type Repo interface {
	Create(ctx context.Context, f *model.Flow) error
	Update(ctx context.Context, f *model.Flow) error
	Get(ctx context.Context, id uint64) (*model.Flow, error)
	List(ctx context.Context, limit, offset int) ([]*model.Flow, int64, error)
	// ListEnabled returns every enabled flow (no paging) — the alert
	// dispatcher and cron scheduler scan these for matching triggers.
	ListEnabled(ctx context.Context) ([]*model.Flow, error)
	Delete(ctx context.Context, id uint64) error
}

// RunRepo persists runs + their node rows.
type RunRepo interface {
	CreateRun(ctx context.Context, r *model.FlowRun) error
	UpdateRun(ctx context.Context, r *model.FlowRun) error
	GetRun(ctx context.Context, id string) (*model.FlowRun, error)
	ListRuns(ctx context.Context, flowID uint64, limit int) ([]*model.FlowRun, error)
	CreateNode(ctx context.Context, n *model.FlowRunNode) error
	UpdateNode(ctx context.Context, n *model.FlowRunNode) error
	ListNodes(ctx context.Context, runID string) ([]*model.FlowRunNode, error)
	// SweepStaleRunning flips running/pending rows to failed — called
	// once at boot; the engine is in-process so runs don't survive a
	// restart.
	SweepStaleRunning(ctx context.Context, reason string) (int64, error)
	// PruneRuns deletes finished runs (and their node rows) created before
	// `before`, bounding history growth.
	PruneRuns(ctx context.Context, before time.Time) (int64, error)
}

// RunLeaseRepo 为多副本部署提供条件式租约续期与仅清理过期遗留运行的持久化契约。
type RunLeaseRepo interface {
	RenewRunLease(ctx context.Context, runID, owner string, until time.Time) error
	SweepExpiredRuns(ctx context.Context, now, legacyCutoff time.Time, reason string) (int64, error)
}

// runRetention is how long finished runs are kept before PruneOldRuns
// reaps them. Overridable via OPSPILOT_FLOW_RUN_RETENTION_DAYS.
const defaultRunRetentionDays = 14

const (
	flowRunLeaseTTL    = 60 * time.Second
	flowRunLeaseRenew  = 20 * time.Second
	flowLegacyStaleAge = 5 * time.Minute
)

// Usecase wires definitions, runs and the engine.
type Usecase struct {
	repo       Repo
	runs       RunRepo
	engine     *Engine
	catalog    ToolCatalog
	llm        GenLLM
	log        *slog.Logger
	instanceID string
}

// NewUsecase constructs the biz facade. engine may be nil in tests
// that only exercise CRUD.
func NewUsecase(repo Repo, runs RunRepo, engine *Engine, log *slog.Logger) *Usecase {
	if log == nil {
		log = slog.Default()
	}
	return &Usecase{repo: repo, runs: runs, engine: engine, log: log, instanceID: uuid.NewString()}
}

// WithInstanceID 绑定当前 Manager 副本 ID，作为异步工作流执行的租约持有者标识。
func (u *Usecase) WithInstanceID(id string) *Usecase {
	if trimmed := strings.TrimSpace(id); trimmed != "" {
		u.instanceID = trimmed
	}
	return u
}

// CreateInput / UpdateInput are the write payloads.
type CreateInput struct {
	Name        string
	Description string
	GraphJSON   string
	CreatedBy   *uint64
}

// Create validates the graph and inserts the definition.
func (u *Usecase) Create(ctx context.Context, in CreateInput) (*model.Flow, error) {
	if u.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name required", errs.ErrInvalid)
	}
	graph := strings.TrimSpace(in.GraphJSON)
	if graph == "" {
		graph = "{}"
	}
	if _, err := ParseGraph(graph); err != nil {
		return nil, fmt.Errorf("%w: %v", errs.ErrInvalid, err)
	}
	f := &model.Flow{
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		GraphJSON:   graph,
		Enabled:     true,
		Version:     1,
		CreatedBy:   in.CreatedBy,
	}
	if err := u.repo.Create(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Update replaces name/description/graph and bumps Version when the
// graph actually changed.
func (u *Usecase) Update(ctx context.Context, id uint64, in CreateInput) (*model.Flow, error) {
	if u.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	f, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		f.Name = name
	}
	f.Description = strings.TrimSpace(in.Description)
	if graph := strings.TrimSpace(in.GraphJSON); graph != "" && graph != f.GraphJSON {
		if _, err := ParseGraph(graph); err != nil {
			return nil, fmt.Errorf("%w: %v", errs.ErrInvalid, err)
		}
		f.GraphJSON = graph
		f.Version++
	}
	if err := u.repo.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// SetEnabled toggles a flow.
func (u *Usecase) SetEnabled(ctx context.Context, id uint64, enabled bool) error {
	if u.repo == nil {
		return errs.ErrNotWiredYet
	}
	f, err := u.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	f.Enabled = enabled
	return u.repo.Update(ctx, f)
}

// Get / List / Delete are thin passthroughs.
func (u *Usecase) Get(ctx context.Context, id uint64) (*model.Flow, error) {
	if u.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	return u.repo.Get(ctx, id)
}

func (u *Usecase) List(ctx context.Context, limit, offset int) ([]*model.Flow, int64, error) {
	if u.repo == nil {
		return nil, 0, errs.ErrNotWiredYet
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return u.repo.List(ctx, limit, offset)
}

func (u *Usecase) Delete(ctx context.Context, id uint64) error {
	if u.repo == nil {
		return errs.ErrNotWiredYet
	}
	return u.repo.Delete(ctx, id)
}

// Trigger starts a manual run (the HTTP "Run" button). Thin wrapper over
// triggerRun scoped to manual trigger nodes.
func (u *Usecase) Trigger(ctx context.Context, id uint64, input map[string]any, by *uint64) (*model.FlowRun, error) {
	return u.triggerRun(ctx, id, NodeTriggerManual, nil, input, by, true)
}

// TestNode executes a single node in isolation and returns its output —
// the editor's "test run this node" affordance, so the user sees a node's
// real output (and thus its referenceable fields) before wiring it into
// the flow. Upstream {{nodes.*}} refs resolve against the flow's most
// recent run outputs; triggerInput backs {{trigger.*}}. The error is
// returned in-band (caller surfaces it in the UI, not as an HTTP failure).
func (u *Usecase) TestNode(ctx context.Context, flowID uint64, nodeType string, configJSON json.RawMessage, triggerInput map[string]any) (any, error) {
	if u.engine == nil {
		return nil, errs.ErrNotWiredYet
	}
	// Seed upstream context from the latest run so refs to already-run
	// nodes resolve; best-effort.
	nodesCtx := map[string]any{}
	if u.runs != nil && flowID > 0 {
		if runs, err := u.runs.ListRuns(ctx, flowID, 1); err == nil && len(runs) > 0 {
			if rnodes, err := u.runs.ListNodes(ctx, runs[0].ID); err == nil {
				for _, rn := range rnodes {
					var out any
					if rn.OutputJSON != "" {
						_ = json.Unmarshal([]byte(rn.OutputJSON), &out)
					}
					nodesCtx[rn.NodeID] = out
				}
			}
		}
	}
	if reason := u.testRunSideEffect(nodeType, configJSON); reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	rc := &RunContext{Trigger: triggerInput, Nodes: nodesCtx, Vars: map[string]any{}}
	node := GraphNode{ID: "test", Type: nodeType, Config: configJSON}
	res, err := u.engine.RunSingle(ctx, node, rc)
	if err != nil {
		return nil, err
	}
	return res.Output, nil
}

// testRunSideEffect returns a non-empty reason when a node type must NOT
// be test-run because doing so causes a real external side effect (a
// notification actually delivered, a service actually restarted). Test-run
// exists to reveal a node's OUTPUT SHAPE for downstream references, not to
// mutate the world — side-effecting nodes are validated only when the whole
// flow runs. Read-class tool / agent / llm / condition / set / transform
// nodes stay test-runnable.
func (u *Usecase) testRunSideEffect(nodeType string, configJSON json.RawMessage) string {
	switch nodeType {
	case NodeNotify:
		return "notify node delivers a real message and cannot be test-run; run the whole flow to validate it"
	case NodeTool:
		var cfg struct {
			Tool string `json:"tool"`
		}
		_ = json.Unmarshal(configJSON, &cfg)
		if cfg.Tool == "" {
			return ""
		}
		for _, t := range u.ListTools() {
			if t.Name != cfg.Tool {
				continue
			}
			if t.Class == "write" || t.Class == "destructive" {
				return fmt.Sprintf("tool %q is %s-class and cannot be test-run because it changes real state; run the whole flow to execute it", cfg.Tool, t.Class)
			}
			break
		}
	}
	return ""
}

// ListEnabledFlows returns every enabled flow — the dispatcher /
// scheduler scan source.
func (u *Usecase) ListEnabledFlows(ctx context.Context) ([]*model.Flow, error) {
	if u.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	return u.repo.ListEnabled(ctx)
}

// TriggerEvent starts a run from a non-manual source (alert dispatcher /
// cron scheduler). entryType and entryNodeID identify the matched trigger;
// payload becomes {{trigger.*}}.
// Returns ErrInvalid when the selected node was removed or changed after
// the caller scanned it, so another branch is never started instead.
func (u *Usecase) TriggerEvent(ctx context.Context, flowID uint64, entryType, entryNodeID string, payload map[string]any) (*model.FlowRun, error) {
	return u.TriggerEvents(ctx, flowID, entryType, []string{entryNodeID}, payload)
}

// TriggerEvents starts one run containing every matched entry in the flow.
// The engine's execute-once state is shared across the selected branches.
func (u *Usecase) TriggerEvents(ctx context.Context, flowID uint64, entryType string, entryNodeIDs []string, payload map[string]any) (*model.FlowRun, error) {
	if len(entryNodeIDs) == 0 {
		return nil, fmt.Errorf("%w: trigger node id required", errs.ErrInvalid)
	}
	for _, id := range entryNodeIDs {
		if id == "" {
			return nil, fmt.Errorf("%w: trigger node id required", errs.ErrInvalid)
		}
	}
	return u.triggerRun(ctx, flowID, entryType, append([]string(nil), entryNodeIDs...), payload, nil, false)
}

// triggerRun is the shared run-launch core. requireEnabled distinguishes
// the manual path (surfaces "flow disabled" as a user error) from event
// paths (caller already filtered to enabled flows; a disabled flow here
// is a benign race → skip).
func (u *Usecase) triggerRun(ctx context.Context, id uint64, entryType string, entryNodeIDs []string, input map[string]any, by *uint64, requireEnabled bool) (*model.FlowRun, error) {
	if u.repo == nil || u.runs == nil || u.engine == nil {
		return nil, errs.ErrNotWiredYet
	}
	f, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !f.Enabled {
		if requireEnabled {
			return nil, fmt.Errorf("%w: flow disabled", errs.ErrInvalid)
		}
		return nil, nil // event path: benign skip
	}
	g, err := ParseGraph(f.GraphJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errs.ErrInvalid, err)
	}
	// Confirm every selected entry still exists in the current graph.
	matching := make(map[string]bool)
	for _, t := range g.Triggers() {
		if t.Type == entryType {
			matching[t.ID] = true
		}
	}
	if len(entryNodeIDs) == 0 && len(matching) == 0 {
		return nil, fmt.Errorf("%w: graph has no %s trigger", errs.ErrInvalid, entryType)
	}
	for _, nodeID := range entryNodeIDs {
		if !matching[nodeID] {
			return nil, fmt.Errorf("%w: graph has no %s trigger node %q", errs.ErrInvalid, entryType, nodeID)
		}
	}

	tb, _ := json.Marshal(input)
	if input == nil {
		tb = []byte("{}")
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(flowRunLeaseTTL)
	run := &model.FlowRun{
		ID:             uuid.NewString(),
		FlowID:         f.ID,
		FlowVersion:    f.Version,
		Status:         model.RunStatusRunning,
		TriggerType:    entryType,
		TriggerJSON:    string(tb),
		CreatedBy:      by,
		ExecutionOwner: u.instanceID,
		LeaseUntil:     &leaseUntil,
		StartedAt:      &now,
	}
	if err := u.runs.CreateRun(ctx, run); err != nil {
		return nil, err
	}

	// Detach from the request context — the run must outlive it (same
	// rationale as the chat workCtx fix: a closed connection must not
	// cancel in-flight work).
	go func() {
		bg := context.Background()
		renewCtx, stopRenew := context.WithCancel(bg)
		var renewWG sync.WaitGroup
		if lr, ok := u.runs.(RunLeaseRepo); ok && u.instanceID != "" {
			renewWG.Add(1)
			go func() {
				defer renewWG.Done()
				t := time.NewTicker(flowRunLeaseRenew)
				defer t.Stop()
				for {
					select {
					case <-renewCtx.Done():
						return
					case <-t.C:
						callCtx, cancel := context.WithTimeout(renewCtx, 5*time.Second)
						if err := lr.RenewRunLease(callCtx, run.ID, u.instanceID, time.Now().UTC().Add(flowRunLeaseTTL)); err != nil && renewCtx.Err() == nil {
							u.log.Warn("flow run lease renew failed", slog.String("run_id", run.ID), slog.Any("err", err))
						}
						cancel()
					}
				}
			}()
		}

		status, execErr := u.engine.Execute(bg, run, g, entryType, entryNodeIDs...)
		stopRenew()
		renewWG.Wait()

		fin := time.Now().UTC()
		run.Status = status
		run.FinishedAt = &fin
		if execErr != nil {
			run.Error = truncate(execErr.Error(), 2000)
		}
		if err := u.runs.UpdateRun(bg, run); err != nil {
			u.log.Warn("flow run finalize failed", slog.String("run_id", run.ID), slog.Any("err", err))
		}
	}()
	return run, nil
}

// GetRun returns a run plus its node rows.
func (u *Usecase) GetRun(ctx context.Context, runID string) (*model.FlowRun, []*model.FlowRunNode, error) {
	if u.runs == nil {
		return nil, nil, errs.ErrNotWiredYet
	}
	run, err := u.runs.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := u.runs.ListNodes(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	return run, nodes, nil
}

// ListRuns returns the latest runs of one flow.
func (u *Usecase) ListRuns(ctx context.Context, flowID uint64, limit int) ([]*model.FlowRun, error) {
	if u.runs == nil {
		return nil, errs.ErrNotWiredYet
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return u.runs.ListRuns(ctx, flowID, limit)
}

// HealStaleRuns sweeps running rows left by a previous process. Call
// once from main after migration.
func (u *Usecase) HealStaleRuns(ctx context.Context) {
	if u.runs == nil {
		return
	}
	n, err := u.runs.SweepStaleRunning(ctx, "manager restarted while run was in flight")
	if err != nil {
		u.log.Warn("flow stale-run sweep failed", slog.Any("err", err))
		return
	}
	if n > 0 {
		u.log.Info("flow stale runs swept", slog.Int64("count", n))
	}
}

// SweepExpiredRuns 清理租约已过期（或旧版无 owner 超时）的孤立运行记录，保护其他活跃副本上的运行。
func (u *Usecase) SweepExpiredRuns(ctx context.Context) int64 {
	if u.runs == nil {
		return 0
	}
	now := time.Now().UTC()
	const reason = "flow execution lease expired before completion"
	if lr, ok := u.runs.(RunLeaseRepo); ok {
		n, err := lr.SweepExpiredRuns(ctx, now, now.Add(-flowLegacyStaleAge), reason)
		if err != nil {
			u.log.Warn("flow expired-run sweep failed", slog.Any("err", err))
			return 0
		}
		if n > 0 {
			u.log.Info("flow expired runs swept", slog.Int64("count", n))
		}
		return n
	}
	n, err := u.runs.SweepStaleRunning(ctx, reason)
	if err != nil {
		u.log.Warn("flow stale-run fallback sweep failed", slog.Any("err", err))
		return 0
	}
	return n
}

// RunStaleRunReconciler 在主节点租约上下文内定期回收租约过期的遗留工作流运行，直至 ctx 取消。
func (u *Usecase) RunStaleRunReconciler(ctx context.Context) error {
	if u == nil || u.runs == nil {
		return nil
	}
	u.SweepExpiredRuns(ctx)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			u.SweepExpiredRuns(ctx)
		}
	}
}

// PruneOldRuns reaps finished runs older than the retention window. Safe
// to call periodically (the scheduler does) and at boot.
func (u *Usecase) PruneOldRuns(ctx context.Context) {
	if u.runs == nil {
		return
	}
	days := defaultRunRetentionDays
	if v := strings.TrimSpace(os.Getenv("OPSPILOT_FLOW_RUN_RETENTION_DAYS")); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			days = d
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -days)
	n, err := u.runs.PruneRuns(ctx, before)
	if err != nil {
		u.log.Warn("flow run prune failed", slog.Any("err", err))
		return
	}
	if n > 0 {
		u.log.Info("flow old runs pruned", slog.Int64("count", n), slog.Int("retention_days", days))
	}
}
