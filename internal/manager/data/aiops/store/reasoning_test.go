package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/aiops"
)

func TestMessages_ReasoningDatabaseRoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	s := &model.Session{UserID: 1, Title: "reasoning fixture"}
	require.NoError(t, repo.CreateSession(ctx, s))
	// 覆盖超过普通 TEXT 容量的协议字段与旧记录 NULL。
	for i, reason := range []*string{nil, strp(strings.Repeat("x", 70000))} {
		row := &model.Message{CreatedAt: time.Unix(int64(i), 0).UTC(), SessionID: s.ID, Role: model.RoleAssistant, Content: strp("visible"), ReasoningContent: reason}
		require.NoError(t, repo.AppendMessage(ctx, row))
	}
	rows, err := repo.ListMessages(ctx, s.ID, 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Nil(t, rows[0].ReasoningContent)
	require.NotNil(t, rows[1].ReasoningContent)
	require.Len(t, *rows[1].ReasoningContent, 70000)
}
