// Package logs is the edge-side `logs` plugin.
//
// It wraps an otelcol-contrib subprocess: opspilot-edge writes a validated
// Collector config derived from manager-pushed PluginConfig and lets the
// Collector write directly to built-in Loki or external Elasticsearch.
// opspilot-edge does not touch the log byte stream.
package logs

import (
	"log/slog"
	"path/filepath"

	"github.com/Zara1024/OpsPilot/internal/edgeagent/plugins"
)

// Name is the OTel signal name used as plugin identifier and as the
// directory key under <workDir>/plugins/.
const Name = "logs"

// New constructs the logs plugin. binDir is where opspilot-edge looks for
// the bundled otelcol-contrib binary; workDir is where rendered config,
// persistent receiver checkpoints/export queues, and subprocess logs live
// (typically /var/lib/opspilot-edge/plugins).
//
// The returned *plugins.SubprocessPlugin satisfies plugins.Plugin and is
// registered with the Supervisor by opspilot-edge main.
func New(binDir, workDir string, log *slog.Logger) plugins.Plugin {
	binary := filepath.Join(binDir, "otelcol-contrib")
	return plugins.NewSubprocess(plugins.SubprocessOpts{
		Name:            Name,
		Binary:          binary,
		WorkDir:         filepath.Join(workDir, Name),
		ConfigFile:      filepath.Join(workDir, Name, "otelcol.yaml"),
		ConfigRender:    render,
		ConfigValidator: plugins.OTelConfigValidator(binary, "--feature-gates=transform.flatten.logs"),
		Args: func(_ plugins.PluginConfig, configFile string) []string {
			return []string{"--config=" + configFile, "--feature-gates=transform.flatten.logs"}
		},
		Log: log,
	})
}
