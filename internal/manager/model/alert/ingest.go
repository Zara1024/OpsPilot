package alert

import "time"

const (
	IngestProviderAlertmanager   = "alertmanager"
	IngestProviderGrafana        = "grafana"
	IngestProviderAliyunCMS      = "aliyun_cms"
	IngestProviderTencentCM      = "tencent_cm"
	IngestProviderHuaweiCES      = "huawei_ces"
	IngestProviderZabbix         = "zabbix"
	IngestProviderGenericWebhook = "generic_webhook"
)

const (
	HeartbeatStateWaiting  = "waiting"
	HeartbeatStateHealthy  = "healthy"
	HeartbeatStateAlerting = "alerting"
	HeartbeatStatePaused   = "paused"
)

// IngestSource stores an external monitoring webhook endpoint configuration and its ingest telemetry.
type IngestSource struct {
	ID                     uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name                   string     `gorm:"column:name;size:128;not null" json:"name"`
	Description            string     `gorm:"column:description;size:255;not null;default:''" json:"description"`
	Provider               string     `gorm:"column:provider;size:32;not null;index:idx_alert_ingest_provider" json:"provider"`
	TokenHash              string     `gorm:"column:token_hash;size:64;not null;uniqueIndex:uk_alert_ingest_token_hash" json:"-"`
	TokenEncrypted         string     `gorm:"column:token_encrypted;type:text" json:"-"`
	SigningSecretEncrypted string     `gorm:"column:signing_secret_encrypted;type:text" json:"-"`
	DefaultSeverity        string     `gorm:"column:default_severity;size:16;not null;default:'warning'" json:"default_severity"`
	DefaultLabelsJSON      string     `gorm:"column:default_labels_json;size:1024;not null;default:'{}'" json:"default_labels_json"`
	AutoResolve            bool       `gorm:"column:auto_resolve;not null;default:true" json:"auto_resolve"`
	Enabled                bool       `gorm:"column:enabled;not null;default:true" json:"enabled"`
	RateLimitPerMin        int        `gorm:"column:rate_limit_per_min;not null;default:120" json:"rate_limit_per_min"`
	ReceivedCount          uint64     `gorm:"column:received_count;not null;default:0" json:"received_count"`
	ErrorCount             uint64     `gorm:"column:error_count;not null;default:0" json:"error_count"`
	LastReceivedAt         *time.Time `gorm:"column:last_received_at" json:"last_received_at,omitempty"`
	LastStatus             string     `gorm:"column:last_status;size:32;not null;default:''" json:"last_status"`
	LastError              string     `gorm:"column:last_error;size:512;not null;default:''" json:"last_error"`
	LastPayloadPreview     string     `gorm:"column:last_payload_preview;type:text" json:"last_payload_preview"`
	CreatedBy              uint64     `gorm:"column:created_by;not null;default:0" json:"created_by"`
	CreatedAt              time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt              time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName returns the database table name for external alert ingest sources.
func (IngestSource) TableName() string {
	return "alert_ingest_sources"
}

// Heartbeat represents a dead-man's switch monitor that fires if periodic pings stop arriving.
type Heartbeat struct {
	ID              uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"column:name;size:128;not null" json:"name"`
	Description     string     `gorm:"column:description;size:255;not null;default:''" json:"description"`
	TokenHash       string     `gorm:"column:token_hash;size:64;not null;uniqueIndex:uk_alert_hb_token_hash" json:"-"`
	TokenEncrypted  string     `gorm:"column:token_encrypted;type:text" json:"-"`
	IntervalSeconds int        `gorm:"column:interval_seconds;not null;default:60" json:"interval_seconds"`
	GraceSeconds    int        `gorm:"column:grace_seconds;not null;default:120" json:"grace_seconds"`
	Severity        string     `gorm:"column:severity;size:16;not null;default:'critical'" json:"severity"`
	LabelsJSON      string     `gorm:"column:labels_json;size:1024;not null;default:'{}'" json:"labels_json"`
	Enabled         bool       `gorm:"column:enabled;not null;default:true" json:"enabled"`
	State           string     `gorm:"column:state;size:16;not null;default:'waiting';index:idx_alert_hb_state" json:"state"`
	LastPingAt      *time.Time `gorm:"column:last_ping_at" json:"last_ping_at,omitempty"`
	LastPingIP      string     `gorm:"column:last_ping_ip;size:64;not null;default:''" json:"last_ping_ip"`
	LastPingMessage string     `gorm:"column:last_ping_message;size:255;not null;default:''" json:"last_ping_message"`
	AlertingSince   *time.Time `gorm:"column:alerting_since" json:"alerting_since,omitempty"`
	IncidentID      *uint64    `gorm:"column:incident_id" json:"incident_id,omitempty"`
	PingCount       uint64     `gorm:"column:ping_count;not null;default:0" json:"ping_count"`
	CreatedBy       uint64     `gorm:"column:created_by;not null;default:0" json:"created_by"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName returns the database table name for heartbeat monitors.
func (Heartbeat) TableName() string {
	return "alert_heartbeats"
}
