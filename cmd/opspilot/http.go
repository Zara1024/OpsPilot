package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	managerserveraudit "github.com/Zara1024/OpsPilot/internal/manager/server/audit"
	managermiddleware "github.com/Zara1024/OpsPilot/internal/manager/server/middleware"
	pagesserver "github.com/Zara1024/OpsPilot/internal/manager/server/pages"
	managerserverprom "github.com/Zara1024/OpsPilot/internal/manager/server/prometheus"
	managersvcprom "github.com/Zara1024/OpsPilot/internal/manager/service/prometheus"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
	"github.com/Zara1024/OpsPilot/internal/pkg/config"
	"github.com/Zara1024/OpsPilot/internal/pkg/httpserver"
	"github.com/Zara1024/OpsPilot/internal/pkg/prom"
	chi "github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type httpModule struct {
	apiServer     *httpserver.Server
	metricsServer *httpserver.Server
}

func buildHTTPServers(rootCtx context.Context, cfg *config.Config, log *slog.Logger, reg *prometheus.Registry, storage *storageModule, iam *iamModule, settings *settingsModule, devices *devicesModule, telemetry *telemetryModule, tunnel *tunnelModule, aiops *aiopsModule, runtime *runtimeModule, imbridge *imbridgeModule, automation *automationModule, system *systemModule, extensions *extensionsModule, external *externalMCPModule, oncall *oncallModule) (*httpModule, error) {
	promProxySvc := managersvcprom.New(iam.signer)
	// Wire the cloud-Prom query client into the proxy handler so the
	// SPA's Monitor page can issue range queries through the same
	// /v1/prometheus auth gate launch + auth already use. promQueryClient
	// is nil when OPSPILOT_PROM_ENABLED=false; the handler 503s in that
	// case rather than crashing.
	var promProxyQuerier managerserverprom.PromQuerier
	if telemetry.promQueryClient != nil {
		promProxyQuerier = telemetry.promQueryClient
	}
	promProxyHandler := managerserverprom.NewHandlerWithProm(promProxySvc, promProxyQuerier)

	// otelhttpmw is the OTel HTTP middleware factory. Each request gets
	// a span named after its method + matched chi route. Built once and
	// reused; nil-safe even when tracing.Init returned a no-op tracer
	// provider (otel global stays as the default no-op).
	otelhttpmw := func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "opspilot-manager",
			otelhttp.WithFilter(managermiddleware.ShouldTraceHTTPRequest),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if route := chi.RouteContext(r.Context()).RoutePattern(); route != "" {
					return r.Method + " " + route
				}
				return r.Method + " " + r.URL.Path
			}),
		)
	}

	pagesHandler := pagesserver.NewHandler(extensions.pageStore, cfg.JWT.Secret)
	registerProtected := func(protected chi.Router) {
		// /v1/version — manager binary version, used by the SPA to
		// flag edges whose agent_version drifts from the manager's.
		// Inline rather than its own handler package because the
		// payload is one field; growing this past version + maybe a
		// build SHA would warrant lifting it out.
		protected.Get("/v1/version", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"manager_version":"` + version + `"}`))
		})
		pagesHandler.RegisterProtected(protected)
		iam.iamHandler.RegisterProtected(protected)
		devices.edgeHandler.Register(protected)
		devices.edgeEnrollmentHandler.RegisterProtected(protected)
		extensions.k8sHandler.RegisterProtected(protected)
		tunnel.webshellHandler.Register(protected)
		devices.deviceHandler.Register(protected)
		devices.topologyHandler.Register(protected)
		telemetry.metricHandler.Register(protected)
		settings.monitorHandler.Register(protected)
		telemetry.logsHandler.Register(protected)
		telemetry.tracesHandler.Register(protected)
		telemetry.apmHandler.Register(protected)
		telemetry.agentTraceHandler.Register(protected)
		telemetry.profilesHandler.Register(protected)
		runtime.aiopsHandler.Register(protected)
		system.alertHandler.Register(protected)
		system.systemHealthHandler.Register(protected)
		system.systemUpgradeHandler.Register(protected)
		imbridge.imbridgeHandler.RegisterProtected(protected)
		extensions.skillHandler.Register(protected)
		extensions.operatorRunHandler.Register(protected)
		if system.knowledgeHandler != nil {
			system.knowledgeHandler.Register(protected)
		}
		settings.settingHandler.Register(protected)
		telemetry.integrationHandler.Register(protected)
		extensions.marketplaceHandler.Register(protected)
		extensions.secretHandler.Register(protected)
		extensions.mcpHandler.Register(protected)
		extensions.approvalHandler.Register(protected)
		promProxyHandler.RegisterProtected(protected)
		managerserveraudit.NewHandler(settings.auditUC).Register(protected)
		automation.reportHandler.Register(protected)
		automation.flowHandler.Register(protected)
		aiops.packetCaptureHandler.Register(protected)
		if oncall != nil && oncall.handler != nil {
			oncall.handler.Register(protected)
		}
	}
	if external.engine != nil {
		apiRouter := chi.NewRouter()
		registerProtected(apiRouter)
		if err := external.addAPITools(apiRouter); err != nil {
			return nil, err
		}
	}

	// Top-level mux.
	mux := chi.NewRouter()
	// OTel HTTP middleware — wraps every request in a span named
	// "{METHOD} {ROUTE_PATTERN}" so Tempo's spanmetrics generator can
	// derive traces_spanmetrics_latency_bucket per route. Routes added
	// after this middleware get traced. Health probes are excluded by the
	// filter above so they cannot crowd useful traces out of Tempo search.
	mux.Use(otelhttpmw)
	// ADR-026 self-obs HTTP metrics — runs after OTel so chi has populated
	// RouteContext before we read RoutePattern for the histogram label.
	mux.Use(managermiddleware.MetricsMiddleware)
	// HLD-010 audit middleware — captures mutating requests + auth failures.
	// Handlers can supersede the heuristic by calling middleware.SetAuditEvent.
	mux.Use(managermiddleware.AuditMiddleware(settings.auditUC))
	mux.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	var sqlDB *sql.DB
	if storage != nil {
		sqlDB = storage.sqlDB
	}
	mux.Get("/readyz", buildReadyHandler(rootCtx, sqlDB))
	// Data plane auth verify lives outside /api so nginx can reach it
	// without JWT. Network policy (docker-internal only) is the gate;
	// nginx must NOT proxy_pass external traffic to /internal/auth/*.
	devices.dataPlaneAuthHandler.Register(mux)
	devices.edgeOnlyAuthHandler.RegisterAt(mux, "/internal/auth/edge-verify")
	devices.telemetryOnlyAuthHandler.RegisterAt(mux, "/internal/auth/telemetry-verify")
	extensions.k8sHandler.RegisterInternal(mux)
	devices.edgeEnrollmentHandler.RegisterInternal(mux)

	// All BC HTTP lives under /api. Public iam routes (login / refresh)
	// skip the auth middleware; everything else goes through it via
	// chi.Router.Group.
	mux.Route("/api", func(api chi.Router) {
		iam.iamHandler.RegisterPublic(api)
		if external.handler != nil {
			api.Handle("/v1/integrations/mcp", external.handler)
		}
		tunnel.webshellHandler.RegisterPublic(api)
		promProxyHandler.RegisterPublic(api)
		// IM webhooks live OUTSIDE the bearer group — Feishu / DingTalk
		// can't carry our manager JWT. Auth comes from the platform
		// signature scheme inside the handler.
		imbridge.imbridgeHandler.RegisterPublic(api)
		aiops.packetCaptureHandler.RegisterInternal(api)
		// Public share route: a minted, TTL-bounded token grants login-free
		// read. Pages themselves are NOT public — in-app viewing is authed
		// (GET /api/pages/{id} in the protected group); only an explicit share
		// exposes a page off-platform, mirroring the report /r/{token} model.
		pagesHandler.RegisterPublic(api)
		if system.alertHandler != nil {
			system.alertHandler.RegisterPublic(api)
		}
		if oncall != nil && oncall.handler != nil {
			oncall.handler.RegisterPublic(api)
		}
		// (admin endpoints registered inside the protected group below)

		api.Group(func(protected chi.Router) {
			protected.Use(auth.Middleware(iam.signer))
			external.tokens.Register(protected)
			registerProtected(protected)
		})
	})

	// Public (unauthenticated) report share route: /r/{token}. Mounted
	// on the root mux outside the auth group so a shared report opens
	// without a login (30-day TTL enforced in the biz layer).
	automation.reportHandler.RegisterPublic(mux)

	apiServer := httpserver.New(cfg.HTTPAddr, mux, log.With(slog.String("listener", "api")))

	// Dedicated metrics listener on a separate port.
	metricsMux := chi.NewRouter()
	metricsMux.Handle("/metrics", prom.Handler(reg))
	metricsServer := httpserver.New(cfg.MetricsAddr, metricsMux, log.With(slog.String("listener", "metrics")))

	return &httpModule{
		apiServer:     apiServer,
		metricsServer: metricsServer,
	}, nil
}

// buildReadyHandler 返回 /readyz 就绪探针处理器：
// 当实例处于优雅退出 draining 状态或底层数据库连接不可用时返回 503，摘除流量。
func buildReadyHandler(rootCtx context.Context, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if rootCtx != nil && rootCtx.Err() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		if sqlDB == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db_unavailable"))
			return
		}
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(pingCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db_unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}
