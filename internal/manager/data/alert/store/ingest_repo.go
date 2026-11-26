package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	"github.com/Zara1024/OpsPilot/internal/pkg/dbx"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// IngestRepo implements persistence for external alert ingest sources and dead-man's switch heartbeats.
type IngestRepo struct {
	db *gorm.DB
}

// NewIngestRepo creates a GORM-backed IngestRepo.
func NewIngestRepo(db *gorm.DB) *IngestRepo {
	return &IngestRepo{db: db}
}

func (r *IngestRepo) ListIngestSources(ctx context.Context) ([]*model.IngestSource, error) {
	var out []*model.IngestSource
	if err := dbx.WithContext(ctx, r.db).Order("id desc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list ingest sources: %w", err)
	}
	return out, nil
}

func (r *IngestRepo) GetIngestSourceByID(ctx context.Context, id uint64) (*model.IngestSource, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: ingest source id required", errs.ErrInvalid)
	}
	var src model.IngestSource
	err := dbx.WithContext(ctx, r.db).First(&src, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ingest source %d: %w", id, err)
	}
	return &src, nil
}

func (r *IngestRepo) GetIngestSourceByTokenHash(ctx context.Context, tokenHash string) (*model.IngestSource, error) {
	if tokenHash == "" {
		return nil, errs.ErrUnauthorized
	}
	var src model.IngestSource
	err := dbx.WithContext(ctx, r.db).Where("token_hash = ?", tokenHash).First(&src).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup ingest source by token: %w", err)
	}
	return &src, nil
}

func (r *IngestRepo) CreateIngestSource(ctx context.Context, src *model.IngestSource) error {
	if src == nil {
		return fmt.Errorf("%w: ingest source is nil", errs.ErrInvalid)
	}
	now := time.Now().UTC()
	src.CreatedAt = now
	src.UpdatedAt = now
	if err := dbx.WithContext(ctx, r.db).Create(src).Error; err != nil {
		return fmt.Errorf("create ingest source: %w", err)
	}
	return nil
}

func (r *IngestRepo) UpdateIngestSource(ctx context.Context, src *model.IngestSource) error {
	if src == nil || src.ID == 0 {
		return fmt.Errorf("%w: ingest source id required", errs.ErrInvalid)
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"name":                     src.Name,
		"description":              src.Description,
		"provider":                 src.Provider,
		"signing_secret_encrypted": src.SigningSecretEncrypted,
		"default_severity":         src.DefaultSeverity,
		"default_labels_json":      src.DefaultLabelsJSON,
		"auto_resolve":             src.AutoResolve,
		"enabled":                  src.Enabled,
		"rate_limit_per_min":       src.RateLimitPerMin,
		"updated_at":               now,
	}
	res := dbx.WithContext(ctx, r.db).Model(&model.IngestSource{}).Where("id = ?", src.ID).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("update ingest source %d: %w", src.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *IngestRepo) DeleteIngestSource(ctx context.Context, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: ingest source id required", errs.ErrInvalid)
	}
	res := dbx.WithContext(ctx, r.db).Delete(&model.IngestSource{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete ingest source %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *IngestRepo) UpdateIngestSourceToken(ctx context.Context, id uint64, tokenHash, tokenEnc string) error {
	if id == 0 || tokenHash == "" {
		return fmt.Errorf("%w: id and token hash required", errs.ErrInvalid)
	}
	res := dbx.WithContext(ctx, r.db).Model(&model.IngestSource{}).Where("id = ?", id).Updates(map[string]any{
		"token_hash":      tokenHash,
		"token_encrypted": tokenEnc,
		"updated_at":      time.Now().UTC(),
	})
	if res.Error != nil {
		return fmt.Errorf("rotate ingest source token %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *IngestRepo) RecordIngestOutcome(ctx context.Context, id uint64, at time.Time, status, errMsg, payloadPreview string, isError bool) error {
	if id == 0 {
		return nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	updates := map[string]any{
		"received_count":   gorm.Expr("received_count + ?", 1),
		"last_received_at": at.UTC(),
		"last_status":      limitRunes(status, 32),
		"last_error":       limitRunes(errMsg, 512),
		"updated_at":       at.UTC(),
	}
	if isError {
		updates["error_count"] = gorm.Expr("error_count + ?", 1)
	}
	if payloadPreview != "" {
		updates["last_payload_preview"] = limitRunes(payloadPreview, 8192)
	}
	if err := dbx.WithContext(ctx, r.db).Model(&model.IngestSource{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("record ingest outcome %d: %w", id, err)
	}
	return nil
}

func (r *IngestRepo) ListHeartbeats(ctx context.Context) ([]*model.Heartbeat, error) {
	var out []*model.Heartbeat
	if err := dbx.WithContext(ctx, r.db).Order("id desc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list heartbeats: %w", err)
	}
	return out, nil
}

func (r *IngestRepo) ListEnabledHeartbeats(ctx context.Context) ([]*model.Heartbeat, error) {
	var out []*model.Heartbeat
	if err := dbx.WithContext(ctx, r.db).Where("enabled = ?", true).Order("id asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list enabled heartbeats: %w", err)
	}
	return out, nil
}

func (r *IngestRepo) GetHeartbeatByID(ctx context.Context, id uint64) (*model.Heartbeat, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: heartbeat id required", errs.ErrInvalid)
	}
	var hb model.Heartbeat
	err := dbx.WithContext(ctx, r.db).First(&hb, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get heartbeat %d: %w", id, err)
	}
	return &hb, nil
}

func (r *IngestRepo) GetHeartbeatByTokenHash(ctx context.Context, tokenHash string) (*model.Heartbeat, error) {
	if tokenHash == "" {
		return nil, errs.ErrUnauthorized
	}
	var hb model.Heartbeat
	err := dbx.WithContext(ctx, r.db).Where("token_hash = ?", tokenHash).First(&hb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup heartbeat by token: %w", err)
	}
	return &hb, nil
}

func (r *IngestRepo) CreateHeartbeat(ctx context.Context, hb *model.Heartbeat) error {
	if hb == nil {
		return fmt.Errorf("%w: heartbeat is nil", errs.ErrInvalid)
	}
	now := time.Now().UTC()
	hb.CreatedAt = now
	hb.UpdatedAt = now
	if err := dbx.WithContext(ctx, r.db).Create(hb).Error; err != nil {
		return fmt.Errorf("create heartbeat: %w", err)
	}
	return nil
}

func (r *IngestRepo) UpdateHeartbeat(ctx context.Context, hb *model.Heartbeat) error {
	if hb == nil || hb.ID == 0 {
		return fmt.Errorf("%w: heartbeat id required", errs.ErrInvalid)
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"name":             hb.Name,
		"description":      hb.Description,
		"interval_seconds": hb.IntervalSeconds,
		"grace_seconds":    hb.GraceSeconds,
		"severity":         hb.Severity,
		"labels_json":      hb.LabelsJSON,
		"enabled":          hb.Enabled,
		"state":            hb.State,
		"updated_at":       now,
	}
	res := dbx.WithContext(ctx, r.db).Model(&model.Heartbeat{}).Where("id = ?", hb.ID).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("update heartbeat %d: %w", hb.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *IngestRepo) DeleteHeartbeat(ctx context.Context, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: heartbeat id required", errs.ErrInvalid)
	}
	res := dbx.WithContext(ctx, r.db).Delete(&model.Heartbeat{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete heartbeat %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *IngestRepo) UpdateHeartbeatToken(ctx context.Context, id uint64, tokenHash, tokenEnc string) error {
	if id == 0 || tokenHash == "" {
		return fmt.Errorf("%w: id and token hash required", errs.ErrInvalid)
	}
	res := dbx.WithContext(ctx, r.db).Model(&model.Heartbeat{}).Where("id = ?", id).Updates(map[string]any{
		"token_hash":      tokenHash,
		"token_encrypted": tokenEnc,
		"updated_at":      time.Now().UTC(),
	})
	if res.Error != nil {
		return fmt.Errorf("rotate heartbeat token %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *IngestRepo) RecordHeartbeatPing(ctx context.Context, id uint64, at time.Time, clientIP, message string) (*model.Heartbeat, string, error) {
	if id == 0 {
		return nil, "", fmt.Errorf("%w: heartbeat id required", errs.ErrInvalid)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var updated model.Heartbeat
	var prevState string
	err := dbx.WithContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if loadErr := tx.First(&updated, id).Error; loadErr != nil {
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return errs.ErrNotFound
			}
			return loadErr
		}
		prevState = updated.State
		pingAt := at.UTC()
		nextState := model.HeartbeatStateHealthy
		if !updated.Enabled {
			nextState = model.HeartbeatStatePaused
		}
		updates := map[string]any{
			"state":             nextState,
			"last_ping_at":      pingAt,
			"last_ping_ip":      limitRunes(clientIP, 64),
			"last_ping_message": limitRunes(message, 255),
			"alerting_since":    nil,
			"incident_id":       nil,
			"ping_count":        gorm.Expr("ping_count + ?", 1),
			"updated_at":        pingAt,
		}
		if err := tx.Model(&model.Heartbeat{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&updated, id).Error
	})
	if err != nil {
		return nil, "", fmt.Errorf("record heartbeat ping %d: %w", id, err)
	}
	return &updated, prevState, nil
}

// MarkHeartbeatAlerting performs a compare-and-set transition from healthy to alerting so
// concurrent multi-replica workers never emit duplicate overdue alerts.
func (r *IngestRepo) MarkHeartbeatAlerting(ctx context.Context, id uint64, at time.Time, incidentID uint64) (bool, error) {
	if id == 0 {
		return false, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	utcAt := at.UTC()
	updates := map[string]any{
		"state":          model.HeartbeatStateAlerting,
		"alerting_since": utcAt,
		"updated_at":     utcAt,
	}
	if incidentID > 0 {
		updates["incident_id"] = incidentID
	}
	res := dbx.WithContext(ctx, r.db).Model(&model.Heartbeat{}).
		Where("id = ? AND state = ? AND enabled = ?", id, model.HeartbeatStateHealthy, true).
		Updates(updates)
	if res.Error != nil {
		return false, fmt.Errorf("mark heartbeat %d alerting: %w", id, res.Error)
	}
	return res.RowsAffected > 0, nil
}

func limitRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
