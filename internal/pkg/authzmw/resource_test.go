package authzmw

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

type fakeResourceAuthorizer struct {
	allow bool
	err   error
	calls int
}

func (f *fakeResourceAuthorizer) AllowResource(_ context.Context, _ uint64, _ bool, _ string, _ []string, _ string) (bool, error) {
	f.calls++
	return f.allow, f.err
}

func TestAllowResourceBehavior(t *testing.T) {
	mw := New(nil, nil)
	ctxUser := tenantctx.With(context.Background(), tenantctx.Tenant{UserID: 7, Role: "user"})
	ctxAdmin := tenantctx.With(context.Background(), tenantctx.Tenant{UserID: 1, Role: "admin", IsSuperuser: true})

	// Unwired ResourceAuthorizer remains permissive for legacy callers
	require.True(t, mw.AllowResource(ctxUser, ResourceDevice, []string{"1"}, ActionRead))

	// Fail-closed on unauthenticated context once wired
	denyRA := &fakeResourceAuthorizer{allow: false}
	mw.SetResourceAuthorizer(denyRA)
	require.False(t, mw.AllowResource(context.Background(), ResourceDevice, []string{"1"}, ActionRead))

	// Denied user
	require.False(t, mw.AllowResource(ctxUser, ResourceDeviceEnv, []string{"prod"}, ActionExec))
	require.Equal(t, 1, denyRA.calls)

	// Admin short-circuits cleanly
	require.True(t, mw.AllowResource(ctxAdmin, ResourceDeviceEnv, []string{"prod"}, ActionExec))
	require.Equal(t, 1, denyRA.calls)

	// Fail-closed on internal error
	errRA := &fakeResourceAuthorizer{allow: true, err: errors.New("db down")}
	mw.SetResourceAuthorizer(errRA)
	require.False(t, mw.AllowResource(ctxUser, ResourceK8sCluster, []string{"prod-eks"}, ActionRead))
}
