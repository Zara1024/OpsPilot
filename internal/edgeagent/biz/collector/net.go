package collector

import (
	"context"

	"github.com/Zara1024/OpsPilot/internal/edgeagent/model"
)

// CollectNet samples network throughput from /proc/net/dev.
// Phase 1 returns a zero value.
func CollectNet(ctx context.Context) (model.HostMetric, error) {
	_ = ctx
	return model.HostMetric{}, nil
}
