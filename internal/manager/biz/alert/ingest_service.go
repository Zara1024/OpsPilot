package alert

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	model "github.com/Zara1024/OpsPilot/internal/manager/model/alert"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/secretbox"
)

// ErrRateLimited signals per-source webhook ingestion throttling (HTTP 429).
var ErrRateLimited = errors.New("ingest rate limit exceeded")

// IngestRepo is the consumer-defined persistence contract for external alert ingest sources and heartbeats.
type IngestRepo interface {
	ListIngestSources(ctx context.Context) ([]*model.IngestSource, error)
	GetIngestSourceByID(ctx context.Context, id uint64) (*model.IngestSource, error)
	GetIngestSourceByTokenHash(ctx context.Context, tokenHash string) (*model.IngestSource, error)
	CreateIngestSource(ctx context.Context, src *model.IngestSource) error
	UpdateIngestSource(ctx context.Context, src *model.IngestSource) error
	DeleteIngestSource(ctx context.Context, id uint64) error
	UpdateIngestSourceToken(ctx context.Context, id uint64, tokenHash, tokenEnc string) error
	RecordIngestOutcome(ctx context.Context, id uint64, at time.Time, status, errMsg, payloadPreview string, isError bool) error

	ListHeartbeats(ctx context.Context) ([]*model.Heartbeat, error)
	ListEnabledHeartbeats(ctx context.Context) ([]*model.Heartbeat, error)
	GetHeartbeatByID(ctx context.Context, id uint64) (*model.Heartbeat, error)
	GetHeartbeatByTokenHash(ctx context.Context, tokenHash string) (*model.Heartbeat, error)
	CreateHeartbeat(ctx context.Context, hb *model.Heartbeat) error
	UpdateHeartbeat(ctx context.Context, hb *model.Heartbeat) error
	DeleteHeartbeat(ctx context.Context, id uint64) error
	UpdateHeartbeatToken(ctx context.Context, id uint64, tokenHash, tokenEnc string) error
	RecordHeartbeatPing(ctx context.Context, id uint64, at time.Time, clientIP, message string) (*model.Heartbeat, string, error)
	MarkHeartbeatAlerting(ctx context.Context, id uint64, at time.Time, incidentID uint64) (bool, error)
}

// IngestSourceInput validates create/update requests for external ingest sources.
type IngestSourceInput struct {
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Provider           string            `json:"provider"`
	SigningSecret      string            `json:"signing_secret,omitempty"`
	ClearSigningSecret bool              `json:"clear_signing_secret,omitempty"`
	DefaultSeverity    string            `json:"default_severity"`
	DefaultLabels      map[string]string `json:"default_labels"`
	AutoResolve        *bool             `json:"auto_resolve"`
	Enabled            *bool             `json:"enabled"`
	RateLimitPerMin    int               `json:"rate_limit_per_min"`
}

// IngestSourceView is the API-safe representation of an IngestSource.
type IngestSourceView struct {
	ID                      uint64            `json:"id"`
	Name                    string            `json:"name"`
	Description             string            `json:"description"`
	Provider                string            `json:"provider"`
	WebhookURL              string            `json:"webhook_url"`
	Token                   string            `json:"token,omitempty"`
	SigningSecretConfigured bool              `json:"signing_secret_configured"`
	DefaultSeverity         string            `json:"default_severity"`
	DefaultLabels           map[string]string `json:"default_labels"`
	AutoResolve             bool              `json:"auto_resolve"`
	Enabled                 bool              `json:"enabled"`
	RateLimitPerMin         int               `json:"rate_limit_per_min"`
	ReceivedCount           uint64            `json:"received_count"`
	ErrorCount              uint64            `json:"error_count"`
	LastReceivedAt          *time.Time        `json:"last_received_at,omitempty"`
	LastStatus              string            `json:"last_status"`
	LastError               string            `json:"last_error"`
	LastPayloadPreview      string            `json:"last_payload_preview,omitempty"`
	SubscribeURL            string            `json:"subscribe_url,omitempty"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
}

// HeartbeatInput validates create/update requests for dead-man's switch monitors.
type HeartbeatInput struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	IntervalSeconds int               `json:"interval_seconds"`
	GraceSeconds    int               `json:"grace_seconds"`
	Severity        string            `json:"severity"`
	Labels          map[string]string `json:"labels"`
	Enabled         *bool             `json:"enabled"`
}

// HeartbeatView is the API-safe representation of a Heartbeat monitor.
type HeartbeatView struct {
	ID              uint64            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	PingURL         string            `json:"ping_url"`
	Token           string            `json:"token,omitempty"`
	IntervalSeconds int               `json:"interval_seconds"`
	GraceSeconds    int               `json:"grace_seconds"`
	Severity        string            `json:"severity"`
	Labels          map[string]string `json:"labels"`
	Enabled         bool              `json:"enabled"`
	State           string            `json:"state"`
	LastPingAt      *time.Time        `json:"last_ping_at,omitempty"`
	LastPingIP      string            `json:"last_ping_ip"`
	LastPingMessage string            `json:"last_ping_message"`
	AlertingSince   *time.Time        `json:"alerting_since,omitempty"`
	IncidentID      *uint64           `json:"incident_id,omitempty"`
	PingCount       uint64            `json:"ping_count"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// IngestWebhookInput carries an external HTTP alert payload and request metadata.
type IngestWebhookInput struct {
	Provider    string
	Token       string
	ContentType string
	Headers     http.Header
	Body        []byte
}

// IngestResult summarizes the outcome of processing an external alert webhook or simulated test.
type IngestResult struct {
	SourceID        uint64   `json:"source_id"`
	Provider        string   `json:"provider"`
	Status          string   `json:"status"`
	FiredCount      int      `json:"fired_count"`
	ResolvedCount   int      `json:"resolved_count"`
	IgnoredCount    int      `json:"ignored_count"`
	IncidentIDs     []uint64 `json:"incident_ids,omitempty"`
	SubscribeURL    string   `json:"subscribe_url,omitempty"`
	Message         string   `json:"message"`
}

// HeartbeatPingResult summarizes the state transition caused by an incoming ping.
type HeartbeatPingResult struct {
	Heartbeat       *HeartbeatView `json:"heartbeat"`
	PreviousState   string         `json:"previous_state"`
	AutoResolved    bool           `json:"auto_resolved"`
	Message         string         `json:"message"`
}

// IngestService coordinates external webhook protocol parsing, rate limiting, and heartbeat pings.
type IngestService struct {
	repo      IngestRepo
	alertUC   *Usecase
	registry  *AdapterRegistry
	publicURL string
	clock     Clock
	log       *slog.Logger

	rateMu  sync.Mutex
	windows map[uint64][]time.Time
}

// NewIngestService constructs an IngestService.
func NewIngestService(repo IngestRepo, alertUC *Usecase, publicURL string, log *slog.Logger) *IngestService {
	if log == nil {
		log = slog.Default()
	}
	return &IngestService{
		repo:      repo,
		alertUC:   alertUC,
		registry:  NewDefaultAdapterRegistry(),
		publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"),
		clock:     realClock{},
		log:       log,
		windows:   make(map[uint64][]time.Time),
	}
}

func (s *IngestService) setClock(c Clock) {
	if c != nil {
		s.clock = c
	}
}

// ---------------------------------------------------------------------------
// Ingest Sources CRUD & Token Rotation
// ---------------------------------------------------------------------------

func (s *IngestService) ListIngestSources(ctx context.Context, includeSecret bool) ([]*IngestSourceView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	rows, err := s.repo.ListIngestSources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*IngestSourceView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toIngestSourceView(row, includeSecret))
	}
	return out, nil
}

func (s *IngestService) CreateIngestSource(ctx context.Context, userID uint64, in IngestSourceInput) (*IngestSourceView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", errs.ErrInvalid)
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if !s.registry.Supports(provider) {
		return nil, fmt.Errorf("%w: unsupported provider %q", errs.ErrInvalid, in.Provider)
	}
	sev := normalizeIngestSeveritySetting(in.DefaultSeverity, "warning")
	labelsJSON := encodeStringMapJSON(in.DefaultLabels)

	plainToken, tokenHash, tokenEnc, err := mintToken()
	if err != nil {
		return nil, err
	}
	signingEnc := ""
	if sec := strings.TrimSpace(in.SigningSecret); sec != "" {
		enc, encErr := secretbox.Encrypt(sec)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt signing secret: %w", encErr)
		}
		signingEnc = enc
	}

	autoResolve := true
	if in.AutoResolve != nil {
		autoResolve = *in.AutoResolve
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	rateLimit := in.RateLimitPerMin
	if rateLimit <= 0 {
		rateLimit = 120
	} else if rateLimit > 5000 {
		rateLimit = 5000
	}

	row := &model.IngestSource{
		Name:                   truncateRunes(name, 128),
		Description:            truncateRunes(strings.TrimSpace(in.Description), 255),
		Provider:               provider,
		TokenHash:              tokenHash,
		TokenEncrypted:         tokenEnc,
		SigningSecretEncrypted: signingEnc,
		DefaultSeverity:        sev,
		DefaultLabelsJSON:      labelsJSON,
		AutoResolve:            autoResolve,
		Enabled:                enabled,
		RateLimitPerMin:        rateLimit,
		CreatedBy:              userID,
	}
	if err := s.repo.CreateIngestSource(ctx, row); err != nil {
		return nil, err
	}
	view := s.toIngestSourceView(row, true)
	view.Token = plainToken
	view.WebhookURL = s.buildWebhookURL(row.Provider, plainToken)
	return view, nil
}

func (s *IngestService) UpdateIngestSource(ctx context.Context, id uint64, in IngestSourceInput) (*IngestSourceView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	existing, err := s.repo.GetIngestSourceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		existing.Name = truncateRunes(name, 128)
	}
	existing.Description = truncateRunes(strings.TrimSpace(in.Description), 255)
	if provider := strings.ToLower(strings.TrimSpace(in.Provider)); provider != "" {
		if !s.registry.Supports(provider) {
			return nil, fmt.Errorf("%w: unsupported provider %q", errs.ErrInvalid, in.Provider)
		}
		existing.Provider = provider
	}
	if in.DefaultSeverity != "" {
		existing.DefaultSeverity = normalizeIngestSeveritySetting(in.DefaultSeverity, existing.DefaultSeverity)
	}
	if in.DefaultLabels != nil {
		existing.DefaultLabelsJSON = encodeStringMapJSON(in.DefaultLabels)
	}
	if in.AutoResolve != nil {
		existing.AutoResolve = *in.AutoResolve
	}
	if in.Enabled != nil {
		existing.Enabled = *in.Enabled
	}
	if in.RateLimitPerMin > 0 {
		rl := in.RateLimitPerMin
		if rl > 5000 {
			rl = 5000
		}
		existing.RateLimitPerMin = rl
	}
	if in.ClearSigningSecret {
		existing.SigningSecretEncrypted = ""
	} else if sec := strings.TrimSpace(in.SigningSecret); sec != "" {
		enc, encErr := secretbox.Encrypt(sec)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt signing secret: %w", encErr)
		}
		existing.SigningSecretEncrypted = enc
	}

	if err := s.repo.UpdateIngestSource(ctx, existing); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetIngestSourceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.toIngestSourceView(updated, true), nil
}

func (s *IngestService) DeleteIngestSource(ctx context.Context, id uint64) error {
	if s.repo == nil {
		return errs.ErrNotWiredYet
	}
	return s.repo.DeleteIngestSource(ctx, id)
}

func (s *IngestService) RotateIngestSourceToken(ctx context.Context, id uint64) (*IngestSourceView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	if _, err := s.repo.GetIngestSourceByID(ctx, id); err != nil {
		return nil, err
	}
	_, tokenHash, tokenEnc, err := mintToken()
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateIngestSourceToken(ctx, id, tokenHash, tokenEnc); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetIngestSourceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.toIngestSourceView(updated, true), nil
}

// ---------------------------------------------------------------------------
// Webhook Ingestion & Simulation
// ---------------------------------------------------------------------------

func (s *IngestService) IngestWebhook(ctx context.Context, in IngestWebhookInput) (*IngestResult, error) {
	if s.repo == nil || s.alertUC == nil {
		return nil, errs.ErrNotWiredYet
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if !s.registry.Supports(provider) {
		return nil, fmt.Errorf("%w: unsupported provider %q", errs.ErrInvalid, in.Provider)
	}
	token := strings.TrimSpace(in.Token)
	if token == "" {
		return nil, errs.ErrUnauthorized
	}

	src, err := s.repo.GetIngestSourceByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}
	if src.Provider != provider {
		return nil, fmt.Errorf("%w: endpoint provider %q mismatches source provider %q", errs.ErrUnauthorized, provider, src.Provider)
	}
	if !src.Enabled {
		return nil, fmt.Errorf("%w: ingest source is disabled", errs.ErrForbidden)
	}

	now := s.clock.Now().UTC()
	preview := truncateRunes(string(in.Body), 8192)

	if !s.allowRequest(src.ID, src.RateLimitPerMin, now) {
		s.recordOutcomeBestEffort(ctx, src.ID, now, "rate_limited", ErrRateLimited.Error(), preview, true)
		return nil, ErrRateLimited
	}

	if strings.TrimSpace(src.SigningSecretEncrypted) != "" {
		secret, decErr := secretbox.Decrypt(src.SigningSecretEncrypted)
		if decErr != nil {
			s.recordOutcomeBestEffort(ctx, src.ID, now, "error", "decrypt signing secret failed", preview, true)
			return nil, fmt.Errorf("decrypt signing secret: %w", decErr)
		}
		if !verifyWebhookHMAC(secret, in.Headers, in.Body) {
			s.recordOutcomeBestEffort(ctx, src.ID, now, "unauthorized", "invalid webhook HMAC signature", preview, true)
			return nil, fmt.Errorf("%w: invalid webhook signature", errs.ErrUnauthorized)
		}
	}

	parsed, err := s.registry.Parse(src.Provider, in.ContentType, in.Headers, in.Body)
	if err != nil {
		s.recordOutcomeBestEffort(ctx, src.ID, now, "parse_error", err.Error(), preview, true)
		return nil, fmt.Errorf("%w: %v", errs.ErrInvalid, err)
	}

	return s.dispatchParsedResult(ctx, src, parsed, now, preview)
}

func (s *IngestService) TestIngestSource(ctx context.Context, id uint64, action string) (*IngestResult, error) {
	if s.repo == nil || s.alertUC == nil {
		return nil, errs.ErrNotWiredYet
	}
	src, err := s.repo.GetIngestSourceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	normAction := "fire"
	if strings.EqualFold(strings.TrimSpace(action), "resolve") {
		normAction = "resolve"
	}
	ct, body, ok := s.registry.SamplePayload(src.Provider, normAction)
	if !ok {
		return nil, fmt.Errorf("%w: no sample payload for %q", errs.ErrInvalid, src.Provider)
	}
	now := s.clock.Now().UTC()
	parsed, err := s.registry.Parse(src.Provider, ct, nil, body)
	if err != nil {
		return nil, fmt.Errorf("parse sample payload: %w", err)
	}
	return s.dispatchParsedResult(ctx, src, parsed, now, truncateRunes(string(body), 8192))
}

func (s *IngestService) dispatchParsedResult(ctx context.Context, src *model.IngestSource, parsed *AdapterResult, now time.Time, rawPreview string) (*IngestResult, error) {
	if parsed.SubscribeURL != "" {
		storedPreview := rawPreview
		if b, err := json.Marshal(map[string]string{
			"type":          "SubscriptionConfirmation",
			"subscribe_url": parsed.SubscribeURL,
			"raw":           truncateRunes(rawPreview, 4096),
		}); err == nil {
			storedPreview = string(b)
		}
		s.recordOutcomeBestEffort(ctx, src.ID, now, "subscription_confirmation", "", storedPreview, false)
		return &IngestResult{
			SourceID:     src.ID,
			Provider:     src.Provider,
			Status:       "subscription_confirmation",
			SubscribeURL: parsed.SubscribeURL,
			Message:      firstPresent(parsed.InfoMessage, "Subscription confirmation captured"),
		}, nil
	}

	defaultLabels := decodeStringMapJSON(src.DefaultLabelsJSON)
	res := &IngestResult{
		SourceID: src.ID,
		Provider: src.Provider,
		Status:   "processed",
	}

	for _, ev := range parsed.Events {
		mergedLabels := cloneAndMergeMaps(defaultLabels, ev.Labels)
		mergedLabels["ingest_source_id"] = fmt.Sprintf("%d", src.ID)
		mergedLabels["ingest_source"] = src.Name
		mergedLabels["ingest_provider"] = src.Provider
		if ev.Target != "" && mergedLabels["target"] == "" {
			mergedLabels["target"] = ev.Target
		}

		dedupeKey := buildExternalDedupeKey(src.ID, ev.RawDedupeKey, ev)
		occ := ev.OccurredAt
		if occ.IsZero() {
			occ = now
		}

		if ev.Status == "resolved" {
			if !src.AutoResolve {
				res.IgnoredCount++
				continue
			}
			resolved, resolveErr := s.alertUC.SystemResolveIncident(ctx, dedupeKey, fmt.Sprintf("外部告警源「%s」推送恢复通知", src.Name), occ)
			if resolveErr != nil {
				s.recordOutcomeBestEffort(ctx, src.ID, now, "error", resolveErr.Error(), rawPreview, true)
				return nil, resolveErr
			}
			if resolved {
				res.ResolvedCount++
			} else {
				res.IgnoredCount++
			}
			continue
		}

		sev := firstPresent(ev.Severity, src.DefaultSeverity, "warning")
		targetScope := truncateRunes(firstPresent(ev.Target, src.Name), 32)
		ruleKey := truncateRunes(firstPresent(ev.RuleKey, src.Provider), 128)
		ruleName := truncateRunes(firstPresent(ev.RuleName, ev.Title, ruleKey), 128)

		fr, fireErr := s.alertUC.RecordFiring(ctx, FiringInput{
			ScopeType:   model.RuleScopeExternal,
			Scope:       targetScope,
			Rule:        ruleKey,
			RuleName:    ruleName,
			Severity:    sev,
			OccurredAt:  occ,
			DedupeKey:   dedupeKey,
			SourceType:  model.RuleSourceExternalWebhook,
			Title:       truncateRunes(firstPresent(ev.Title, ruleName), 256),
			Summary:     firstPresent(ev.Summary, ev.Description, ev.Title, ruleName),
			Description: ev.Description,
			Value:       ev.Value,
			Threshold:   ev.Threshold,
			Labels:      mergedLabels,
			Annotations: ev.Annotations,
			RunbookURL:  truncateRunes(ev.RunbookURL, 512),
		})
		if fireErr != nil {
			s.recordOutcomeBestEffort(ctx, src.ID, now, "error", fireErr.Error(), rawPreview, true)
			return nil, fireErr
		}
		res.FiredCount++
		if fr != nil && fr.Incident != nil {
			res.IncidentIDs = append(res.IncidentIDs, fr.Incident.ID)
		}
	}

	res.Message = fmt.Sprintf("已处理 %d 条触发、%d 条恢复、%d 条忽略", res.FiredCount, res.ResolvedCount, res.IgnoredCount)
	s.recordOutcomeBestEffort(ctx, src.ID, now, "processed", "", rawPreview, false)
	return res, nil
}

func (s *IngestService) recordOutcomeBestEffort(ctx context.Context, id uint64, at time.Time, status, errMsg, payloadPreview string, isError bool) {
	if err := s.repo.RecordIngestOutcome(ctx, id, at, status, errMsg, payloadPreview, isError); err != nil {
		s.log.Warn("record ingest outcome failed", slog.Uint64("source_id", id), slog.String("status", status), slog.Any("err", err))
	}
}

// ---------------------------------------------------------------------------
// Heartbeat CRUD, Token Rotation & Ping Handling
// ---------------------------------------------------------------------------

func (s *IngestService) ListHeartbeats(ctx context.Context, includeSecret bool) ([]*HeartbeatView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	rows, err := s.repo.ListHeartbeats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*HeartbeatView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toHeartbeatView(row, includeSecret))
	}
	return out, nil
}

func (s *IngestService) CreateHeartbeat(ctx context.Context, userID uint64, in HeartbeatInput) (*HeartbeatView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", errs.ErrInvalid)
	}
	interval := in.IntervalSeconds
	if interval <= 0 {
		interval = 60
	} else if interval < 10 {
		return nil, fmt.Errorf("%w: interval_seconds must be >= 10", errs.ErrInvalid)
	}
	grace := in.GraceSeconds
	if grace <= 0 {
		grace = 120
	} else if grace < 10 {
		return nil, fmt.Errorf("%w: grace_seconds must be >= 10", errs.ErrInvalid)
	}
	sev := normalizeIngestSeveritySetting(in.Severity, "critical")
	enabled := true
	state := model.HeartbeatStateWaiting
	if in.Enabled != nil && !*in.Enabled {
		enabled = false
		state = model.HeartbeatStatePaused
	}

	plainToken, tokenHash, tokenEnc, err := mintToken()
	if err != nil {
		return nil, err
	}

	row := &model.Heartbeat{
		Name:            truncateRunes(name, 128),
		Description:     truncateRunes(strings.TrimSpace(in.Description), 255),
		TokenHash:       tokenHash,
		TokenEncrypted:  tokenEnc,
		IntervalSeconds: interval,
		GraceSeconds:    grace,
		Severity:        sev,
		LabelsJSON:      encodeStringMapJSON(in.Labels),
		Enabled:         enabled,
		State:           state,
		CreatedBy:       userID,
	}
	if err := s.repo.CreateHeartbeat(ctx, row); err != nil {
		return nil, err
	}
	view := s.toHeartbeatView(row, true)
	view.Token = plainToken
	view.PingURL = s.buildPingURL(plainToken)
	return view, nil
}

func (s *IngestService) UpdateHeartbeat(ctx context.Context, id uint64, in HeartbeatInput) (*HeartbeatView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	existing, err := s.repo.GetHeartbeatByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		existing.Name = truncateRunes(name, 128)
	}
	existing.Description = truncateRunes(strings.TrimSpace(in.Description), 255)
	if in.IntervalSeconds > 0 {
		if in.IntervalSeconds < 10 {
			return nil, fmt.Errorf("%w: interval_seconds must be >= 10", errs.ErrInvalid)
		}
		existing.IntervalSeconds = in.IntervalSeconds
	}
	if in.GraceSeconds > 0 {
		if in.GraceSeconds < 10 {
			return nil, fmt.Errorf("%w: grace_seconds must be >= 10", errs.ErrInvalid)
		}
		existing.GraceSeconds = in.GraceSeconds
	}
	if in.Severity != "" {
		existing.Severity = normalizeIngestSeveritySetting(in.Severity, existing.Severity)
	}
	if in.Labels != nil {
		existing.LabelsJSON = encodeStringMapJSON(in.Labels)
	}
	if in.Enabled != nil {
		existing.Enabled = *in.Enabled
		if !existing.Enabled {
			existing.State = model.HeartbeatStatePaused
		} else if existing.State == model.HeartbeatStatePaused {
			if existing.LastPingAt == nil {
				existing.State = model.HeartbeatStateWaiting
			} else {
				existing.State = model.HeartbeatStateHealthy
			}
		}
	}

	if err := s.repo.UpdateHeartbeat(ctx, existing); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetHeartbeatByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.toHeartbeatView(updated, true), nil
}

func (s *IngestService) DeleteHeartbeat(ctx context.Context, id uint64) error {
	if s.repo == nil {
		return errs.ErrNotWiredYet
	}
	return s.repo.DeleteHeartbeat(ctx, id)
}

func (s *IngestService) RotateHeartbeatToken(ctx context.Context, id uint64) (*HeartbeatView, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	if _, err := s.repo.GetHeartbeatByID(ctx, id); err != nil {
		return nil, err
	}
	_, tokenHash, tokenEnc, err := mintToken()
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateHeartbeatToken(ctx, id, tokenHash, tokenEnc); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetHeartbeatByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.toHeartbeatView(updated, true), nil
}

func (s *IngestService) PingHeartbeatByToken(ctx context.Context, token, clientIP, message string) (*HeartbeatPingResult, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	t := strings.TrimSpace(token)
	if t == "" {
		return nil, errs.ErrUnauthorized
	}
	hb, err := s.repo.GetHeartbeatByTokenHash(ctx, hashToken(t))
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}
	return s.applyHeartbeatPing(ctx, hb.ID, clientIP, message, false)
}

func (s *IngestService) ManualPingHeartbeat(ctx context.Context, id uint64, message string) (*HeartbeatPingResult, error) {
	if s.repo == nil {
		return nil, errs.ErrNotWiredYet
	}
	return s.applyHeartbeatPing(ctx, id, "console", firstPresent(strings.TrimSpace(message), "控制台手动 Ping 激活"), true)
}

func (s *IngestService) applyHeartbeatPing(ctx context.Context, id uint64, clientIP, message string, includeSecret bool) (*HeartbeatPingResult, error) {
	now := s.clock.Now().UTC()
	updated, prevState, err := s.repo.RecordHeartbeatPing(ctx, id, now, clientIP, message)
	if err != nil {
		return nil, err
	}
	autoResolved := false
	if prevState == model.HeartbeatStateAlerting && s.alertUC != nil {
		resolved, resolveErr := s.alertUC.SystemResolveIncident(
			ctx,
			fmt.Sprintf("heartbeat:%d", updated.ID),
			fmt.Sprintf("心跳监控「%s」已重新收到心跳上报", updated.Name),
			now,
		)
		if resolveErr != nil {
			s.log.Warn("resolve heartbeat incident on ping failed", slog.Uint64("heartbeat_id", updated.ID), slog.Any("err", resolveErr))
		} else {
			autoResolved = resolved
		}
	}
	return &HeartbeatPingResult{
		Heartbeat:     s.toHeartbeatView(updated, includeSecret),
		PreviousState: prevState,
		AutoResolved:  autoResolved,
		Message:       "heartbeat ok",
	}, nil
}

// ---------------------------------------------------------------------------
// Internal View Builders, Auth, Rate Limiting & Crypto Helpers
// ---------------------------------------------------------------------------

func (s *IngestService) toIngestSourceView(row *model.IngestSource, includeSecret bool) *IngestSourceView {
	if row == nil {
		return nil
	}
	tokenPlain := ""
	if row.TokenEncrypted != "" {
		if dec, err := secretbox.Decrypt(row.TokenEncrypted); err == nil {
			tokenPlain = dec
		}
	}
	webhookURL := ""
	tokenDisplay := ""
	if includeSecret && tokenPlain != "" {
		tokenDisplay = tokenPlain
		webhookURL = s.buildWebhookURL(row.Provider, tokenPlain)
	} else if tokenPlain != "" {
		masked := maskToken(tokenPlain)
		tokenDisplay = masked
		webhookURL = s.buildWebhookURL(row.Provider, masked)
	}

	subscribeURL := ""
	if strings.Contains(row.LastPayloadPreview, `"subscribe_url"`) {
		var parsed struct {
			SubscribeURL string `json:"subscribe_url"`
		}
		if json.Unmarshal([]byte(row.LastPayloadPreview), &parsed) == nil {
			subscribeURL = parsed.SubscribeURL
		}
	}

	return &IngestSourceView{
		ID:                      row.ID,
		Name:                    row.Name,
		Description:             row.Description,
		Provider:                row.Provider,
		WebhookURL:              webhookURL,
		Token:                   tokenDisplay,
		SigningSecretConfigured: strings.TrimSpace(row.SigningSecretEncrypted) != "",
		DefaultSeverity:         row.DefaultSeverity,
		DefaultLabels:           decodeStringMapJSON(row.DefaultLabelsJSON),
		AutoResolve:             row.AutoResolve,
		Enabled:                 row.Enabled,
		RateLimitPerMin:         row.RateLimitPerMin,
		ReceivedCount:           row.ReceivedCount,
		ErrorCount:              row.ErrorCount,
		LastReceivedAt:          row.LastReceivedAt,
		LastStatus:              row.LastStatus,
		LastError:               row.LastError,
		LastPayloadPreview:      row.LastPayloadPreview,
		SubscribeURL:            subscribeURL,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

func (s *IngestService) toHeartbeatView(row *model.Heartbeat, includeSecret bool) *HeartbeatView {
	if row == nil {
		return nil
	}
	tokenPlain := ""
	if row.TokenEncrypted != "" {
		if dec, err := secretbox.Decrypt(row.TokenEncrypted); err == nil {
			tokenPlain = dec
		}
	}
	tokenDisplay := ""
	pingURL := ""
	if includeSecret && tokenPlain != "" {
		tokenDisplay = tokenPlain
		pingURL = s.buildPingURL(tokenPlain)
	} else if tokenPlain != "" {
		masked := maskToken(tokenPlain)
		tokenDisplay = masked
		pingURL = s.buildPingURL(masked)
	}

	return &HeartbeatView{
		ID:              row.ID,
		Name:            row.Name,
		Description:     row.Description,
		PingURL:         pingURL,
		Token:           tokenDisplay,
		IntervalSeconds: row.IntervalSeconds,
		GraceSeconds:    row.GraceSeconds,
		Severity:        row.Severity,
		Labels:          decodeStringMapJSON(row.LabelsJSON),
		Enabled:         row.Enabled,
		State:           row.State,
		LastPingAt:      row.LastPingAt,
		LastPingIP:      row.LastPingIP,
		LastPingMessage: row.LastPingMessage,
		AlertingSince:   row.AlertingSince,
		IncidentID:      row.IncidentID,
		PingCount:       row.PingCount,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func (s *IngestService) buildWebhookURL(provider, token string) string {
	path := fmt.Sprintf("/api/v1/alerts/ingest/%s/%s", provider, token)
	if s.publicURL == "" {
		return path
	}
	return s.publicURL + path
}

func (s *IngestService) buildPingURL(token string) string {
	path := fmt.Sprintf("/api/v1/alerts/heartbeat/%s", token)
	if s.publicURL == "" {
		return path
	}
	return s.publicURL + path
}

func (s *IngestService) allowRequest(sourceID uint64, limitPerMin int, now time.Time) bool {
	if limitPerMin <= 0 {
		limitPerMin = 120
	}
	cutoff := now.Add(-1 * time.Minute)
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	hits := s.windows[sourceID]
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= limitPerMin {
		s.windows[sourceID] = kept
		return false
	}
	kept = append(kept, now)
	s.windows[sourceID] = kept
	return true
}

func verifyWebhookHMAC(secret string, headers http.Header, body []byte) bool {
	if secret == "" || headers == nil {
		return false
	}
	rawSig := strings.TrimSpace(firstPresent(
		headers.Get("X-OpsPilot-Signature"),
		headers.Get("X-Hub-Signature-256"),
	))
	if rawSig == "" {
		return false
	}
	rawSig = strings.TrimPrefix(strings.ToLower(rawSig), "sha256=")
	sigBytes, err := hex.DecodeString(rawSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(sigBytes, expected)
}

func mintToken() (plain, hash, encrypted string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("generate random token: %w", err)
	}
	plain = hex.EncodeToString(buf)
	hash = hashToken(plain)
	encrypted, err = secretbox.Encrypt(plain)
	if err != nil {
		return "", "", "", fmt.Errorf("encrypt token: %w", err)
	}
	return plain, hash, encrypted, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func maskToken(token string) string {
	if len(token) <= 10 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

func normalizeIngestSeveritySetting(raw, fallback string) string {
	if sev := normalizeExternalSeverity(raw); sev != "" {
		return sev
	}
	if fb := normalizeExternalSeverity(fallback); fb != "" {
		return fb
	}
	return "warning"
}

func encodeStringMapJSON(m map[string]string) string {
	clean := cloneAndMergeMaps(m)
	b, err := json.Marshal(clean)
	if err != nil {
		return "{}"
	}
	s := string(b)
	if len(s) > 1000 {
		return "{}"
	}
	return s
}

func decodeStringMapJSON(raw string) map[string]string {
	out := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	// Best-effort: fallback to empty map on malformed persisted JSON.
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
