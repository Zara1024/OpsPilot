package authzmw

import (
	"context"
	"log/slog"

	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

// 资源级 RBAC 资源类别与动作常量（与 internal/iam/model 中定义对齐，避免 manager 依赖 iam）。
const (
	ResourceDevice     = "device"
	ResourceDeviceEnv  = "device_env"
	ResourceK8sCluster = "k8s_cluster"
	ResourceService    = "service"

	ActionRead   = "read"
	ActionWrite  = "write"
	ActionExec   = "exec"
	ActionManage = "manage"
)

// ResourceAuthorizer 定义跨域资源级访问判定契约，由 iam/biz/rbac.Service 实现并在 cmd/opspilot 装配。
type ResourceAuthorizer interface {
	AllowResource(ctx context.Context, userID uint64, isAdmin bool, resourceType string, resourceKeys []string, action string) (bool, error)
}

// SetResourceAuthorizer 注入资源级 RBAC 判定器。
func (m *Middleware) SetResourceAuthorizer(ra ResourceAuthorizer) {
	if m != nil {
		m.ra = ra
	}
}

// AllowResource 基于请求上下文中的 tenant 校验当前用户是否可访问目标资源；未装配 ResourceAuthorizer 时放行以保持向后兼容，评估出错时 fail-closed 拒绝。
func (m *Middleware) AllowResource(ctx context.Context, resourceType string, resourceKeys []string, action string) bool {
	if m == nil || m.ra == nil {
		return true
	}
	t, ok := tenantctx.From(ctx)
	if !ok {
		return false
	}
	isAdmin := t.IsSuperuser || t.Role == "admin"
	if isAdmin {
		return true
	}
	allowed, err := m.ra.AllowResource(ctx, t.UserID, false, resourceType, resourceKeys, action)
	if err != nil {
		m.log.Warn("authz: resource access check failed; failing closed",
			slog.Uint64("user_id", t.UserID),
			slog.String("resource_type", resourceType),
			slog.Any("keys", resourceKeys),
			slog.String("action", action),
			slog.Any("err", err))
		return false
	}
	return allowed
}
