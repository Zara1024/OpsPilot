package llm

import "context"

// CallSource 标识一次 LLM 调用的业务来源维度，便于成本拆分与排查。
const (
	SourceChat           = "chat"
	SourceIMBridge       = "imbridge"
	SourceAlertRCA       = "alert_rca"
	SourceReport         = "report"
	SourceFlow           = "flow"
	SourceQueryTranslate = "query_translate"
	SourceCompaction     = "compaction"
	SourceOther          = "other"
)

// CallMeta 在 context 中传递调用方归属与业务追踪字段，供全局 CostGuard 使用。
type CallMeta struct {
	Source    string
	UserID    uint64
	OrgID     uint64
	SessionID string
	RefID     string
}

type callMetaKey struct{}

// WithCallMeta 将调用元信息合并写入 ctx（已存在字段不会被零值覆盖）。
func WithCallMeta(ctx context.Context, meta CallMeta) context.Context {
	cur := CallMetaFrom(ctx)
	if meta.Source != "" {
		cur.Source = meta.Source
	}
	if meta.UserID > 0 {
		cur.UserID = meta.UserID
	}
	if meta.OrgID > 0 {
		cur.OrgID = meta.OrgID
	}
	if meta.SessionID != "" {
		cur.SessionID = meta.SessionID
	}
	if meta.RefID != "" {
		cur.RefID = meta.RefID
	}
	return context.WithValue(ctx, callMetaKey{}, cur)
}

// CallMetaFrom 从 ctx 读取 LLM 调用元信息。
func CallMetaFrom(ctx context.Context) CallMeta {
	if ctx == nil {
		return CallMeta{}
	}
	meta, _ := ctx.Value(callMetaKey{}).(CallMeta)
	return meta
}
