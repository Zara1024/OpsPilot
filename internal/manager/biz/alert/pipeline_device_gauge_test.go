package alert

import (
	"context"
	"testing"
	"time"

	edgemodel "github.com/Zara1024/OpsPilot/internal/manager/model/edge"
)

// Issue #329：批量录入会先建出 edge 行，等主机真正安装 edge 之后才开始
// 上报心跳。这类行既没有 last_seen_at 也没有绑定的 device，旧实现会把
// created_at 当作"最后一次可见时间"、把 edge.id 当作 device_id，于是
// 立刻产生一条指向不存在设备的 device_offline 告警。
func TestRefreshDeviceStalenessGaugeSkipsRowsWithoutHeartbeatOrDevice(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	seen := now.Add(-3 * time.Minute)
	neverSeen := now.Add(-7 * 24 * time.Hour)
	linkedID := uint64(21)
	otherID := uint64(5)

	edges := []*edgemodel.Edge{
		// 现场里的那条影子行：edge 22，无 name、无心跳、无设备。
		{ID: 22, CreatedAt: neverSeen},
		// 已绑定设备但从未上报。
		{ID: 23, Name: "worker-node-017", DeviceID: &otherID, CreatedAt: neverSeen},
		// 上报过心跳但尚未绑定设备。
		{ID: 24, Name: "worker-node-018", LastSeenAt: &seen, CreatedAt: neverSeen},
		// 正常行。
		{ID: 25, Name: "worker-node-016", DeviceID: &linkedID, LastSeenAt: &seen},
	}

	repo, notifier := newFakeRepo(), &fakeNotifier{}
	eval := newPipelineEvaluator(t, repo, notifier, NewStaticRulesProvider(), PipelineEvaluatorOpts{
		EdgeLister: &fakeEdgeLister{edges: edges},
	})

	eval.refreshDeviceStalenessGauge(context.Background(), now)

	got := gaugeSnapshotOf(t, eval)
	if len(got) != 1 {
		t.Fatalf("gauge series = %v, want exactly the one fully-registered device", got)
	}
	if name, ok := got["21"]; !ok || name != "worker-node-016" {
		t.Fatalf("device 21 series = %q, present=%v", name, ok)
	}
	for _, fabricated := range []string{"22", "23", "24"} {
		if _, ok := got[fabricated]; ok {
			t.Fatalf("edge without a heartbeat or a linked device still produced device_id=%s: %v", fabricated, got)
		}
	}
}

// 设备彻底离开清单后，上一轮的序列必须被回收，否则 metric_raw 会一直对
// 已删除的设备告警。同时验证 device_id 与 edge.id 不同时用的是设备号。
func TestRefreshDeviceStalenessGaugeDropsSeriesThatLeaveInventory(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	seen := now.Add(-30 * time.Second)
	edgeOnlyID := uint64(9)

	lister := &fakeEdgeLister{edges: []*edgemodel.Edge{
		{ID: 40, Name: "hpc-admin-001", DeviceID: &edgeOnlyID, LastSeenAt: &seen},
	}}
	repo, notifier := newFakeRepo(), &fakeNotifier{}
	eval := newPipelineEvaluator(t, repo, notifier, NewStaticRulesProvider(), PipelineEvaluatorOpts{
		EdgeLister: lister,
	})

	eval.refreshDeviceStalenessGauge(context.Background(), now)
	got := gaugeSnapshotOf(t, eval)
	if _, ok := got["9"]; !ok {
		t.Fatalf("series should be keyed by device_id 9, not edge id 40: %v", got)
	}
	if _, ok := got["40"]; ok {
		t.Fatalf("edge.id leaked into the device_id label: %v", got)
	}

	// 设备被删除后这一轮不再返回它。
	lister.edges = []*edgemodel.Edge{}
	eval.refreshDeviceStalenessGauge(context.Background(), now)
	got = gaugeSnapshotOf(t, eval)
	if len(got) != 0 {
		t.Fatalf("removed device should drop out of the snapshot, got %v", got)
	}
}

// 读取上一轮快照需要在 gaugeMu 下进行：Loop 是单协程，但测试会并发调用。
func gaugeSnapshotOf(t *testing.T, eval *PipelineEvaluator) map[string]string {
	t.Helper()
	eval.gaugeMu.Lock()
	defer eval.gaugeMu.Unlock()
	out := make(map[string]string, len(eval.gaugeSnapshot))
	for k, v := range eval.gaugeSnapshot {
		out[k] = v
	}
	return out
}
